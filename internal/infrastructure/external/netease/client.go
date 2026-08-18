package netease

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiDomain = "https://interface.music.163.com"
	domain    = "https://music.163.com"
)

var successCodes = map[int]bool{
	200: true, 201: true, 302: true, 400: true, 502: true,
	800: true, 801: true, 802: true, 803: true,
}

var osPC = map[string]string{
	"os":      "pc",
	"appver":  "3.1.17.204416",
	"osver":   "Microsoft-Windows-10-Professional-build-19045-64bit",
	"channel": "netease",
}

const userAgentWeAPI = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0"
const userAgentAPI = "NeteaseMusic 9.0.90/5038 (iPhone; iOS 16.2; zh_CN)"

// Client wraps NetEase Cloud Music's private API. All requests use the
// reverse-engineered weapi/eapi/api encryption schemes and anonymous access.
type Client struct {
	http *http.Client

	mu       sync.Mutex
	deviceID string
	musicA   string
	musicU   string

	// Global request pacing so scans/enrichment bursts do not trip NetEase's
	// server-side throttling (code 405 操作频繁). 0 or negative disables.
	// rateLimitProvider, when set, supplies the effective requests/second at
	// request time (runtime admin setting with a config fallback).
	rateLimitPerSec   int
	rateLimitProvider func() int
	lastReq           time.Time

	// appliedCookie is the last effective cookie value applied (from the
	// runtime provider or SetCookie), so a change (including a clear to "")
	// is detected and applied exactly once.
	appliedCookie string

	// cookieGen increments on every applied cookie change. ensureAnon records
	// it when a registration starts and validates it before writing back the
	// anonymous credentials, so an in-flight registration can never clobber
	// account credentials that were applied (or cleared) in the meantime.
	cookieGen uint64

	// staticCookie is the statically configured cookie (SetCookie). It is the
	// fallback when the runtime cookieProvider yields an empty value, so a
	// cleared runtime override reverts to config instead of silently dropping
	// credentials until restart.
	staticCookie string

	// cookieProvider supplies the account cookie at request time
	// (runtime-configurable via admin settings); overrides SetCookie.
	cookieProvider func() string

	anonMu       sync.Mutex
	anonInFlight bool
	anonDone     bool
	anonRetryAt  time.Time
	anonErr      error
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		deviceID: randomDeviceID(),
	}
}

// SetRateLimit sets the client-wide request pacing (requests per second;
// <= 0 disables). Applied from config; a scan that fires many requests is
// paced so bursts do not trip NetEase's 405 "操作频繁" throttle.
func (c *Client) SetRateLimit(perSec int) {
	c.mu.Lock()
	c.rateLimitPerSec = perSec
	c.mu.Unlock()
}

// SetRateLimitProvider registers a callback that supplies the effective
// requests-per-second at request time (e.g. from a runtime admin setting with
// a config fallback). It overrides the static SetRateLimit value.
func (c *Client) SetRateLimitProvider(f func() int) {
	c.mu.Lock()
	c.rateLimitProvider = f
	c.mu.Unlock()
}

// rateLimit paces outbound requests ticket-style: each request claims the
// next slot (lastReq + interval) so concurrent callers get distinct slots,
// and waits OUTSIDE the lock so pacing never blocks credential handling. The
// wait is cancellable via ctx. <= 0 per second disables pacing.
func (c *Client) rateLimit(ctx context.Context) error {
	c.mu.Lock()
	perSec := c.rateLimitPerSec
	provider := c.rateLimitProvider
	c.mu.Unlock()
	if provider != nil {
		perSec = provider()
	}
	if perSec <= 0 {
		return nil
	}
	interval := time.Second / time.Duration(perSec)
	c.mu.Lock()
	now := time.Now()
	if c.lastReq.IsZero() {
		c.lastReq = now.Add(-interval)
	} else if c.lastReq.Before(now) {
		c.lastReq = now
	}
	next := c.lastReq.Add(interval)
	c.lastReq = next
	c.mu.Unlock()

	wait := next.Sub(now)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		if c.lastReq.Equal(next) {
			c.lastReq = next.Add(-interval)
		}
		c.mu.Unlock()
		return ctx.Err()
	}
}

// SetCookie records the statically configured account cookie and applies it
// immediately (used for the config value at startup). Clearing the runtime
// cookie via the provider later falls back to this value.
func (c *Client) SetCookie(cookie string) {
	c.anonMu.Lock()
	defer c.anonMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.staticCookie = cookie
	c.applyCookieLockedState(cookie)
}

// applyCookie applies the runtime provider's cookie value: a non-empty value
// overrides the statically configured one, an empty one falls back to the
// static cookie (config wins back when the admin override is cleared).
// Holds anonMu then mu (the same order ensureAnon uses) so no concurrent
// request can slip out with cleared credentials while anonDone is still true.
func (c *Client) applyCookie(raw string) {
	c.anonMu.Lock()
	defer c.anonMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if raw == "" {
		raw = c.staticCookie
	}
	c.applyCookieLockedState(raw)
}

// applyCookieLockedState parses and stores the credentials, records the value
// as last applied, and — on a clear — resets the anonymous-registration state
// so the client immediately falls back to anonymous access instead of sending
// requests with empty credentials. Caller must hold anonMu and mu. A no-op
// when the value equals the last applied one.
func (c *Client) applyCookieLockedState(cookie string) {
	if cookie == c.appliedCookie {
		return
	}
	c.applyCookieLocked(cookie)
	c.appliedCookie = cookie
	c.cookieGen++
	if cookie == "" || (c.musicU == "" && c.musicA == "") {
		// A cleared cookie, or a non-empty value that parsed to no usable
		// credential (e.g. "foo=bar"), leaves the client with empty
		// credentials. Re-enable anonymous registration immediately, ignoring
		// any earlier cooldown/failure, so the client never runs long with
		// empty credentials. anonInFlight is deliberately left alone: it
		// belongs to the in-flight registration, which resets it on
		// completion — clearing it here would let a second registration start
		// while the first is still running, and the first's completion would
		// then clobber the second's in-flight marker, allowing a third
		// (concurrent) one.
		c.anonDone = false
		c.anonRetryAt = time.Time{}
		c.anonErr = nil
	}
}

// SetCookieProvider registers a callback that supplies the account cookie at
// request time (e.g. from runtime server settings). When the callback is
// non-nil and returns a non-empty value it overrides the statically
// configured cookie on every request.
func (c *Client) SetCookieProvider(f func() string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cookieProvider = f
}

// applyCookieLocked parses and stores the account cookie. Caller must hold
// c.mu. An empty cookie clears any previously stored credentials so the
// client falls back to anonymous access.
func (c *Client) applyCookieLocked(cookie string) {
	if cookie == "" {
		c.musicU = ""
		c.musicA = ""
		return
	}
	// Clear first: a cookie switching from "MUSIC_U + MUSIC_A" to only one of
	// them (e.g. admin configured just MUSIC_U) must not leave the other
	// field holding the previous session's stale credentials.
	c.musicU = ""
	c.musicA = ""
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "MUSIC_U":
			c.musicU = kv[1]
		case "MUSIC_A":
			c.musicA = kv[1]
		}
	}
}

// ensureAnon registers an anonymous token, mirroring the reference
// implementation's register_anonimous flow. It runs with a detached context
// so a canceled/timed-out caller request cannot poison the registration.
// Failure is non-fatal: a retired endpoint (404) is marked permanently
// skipped, while transient failures are retried after a cooldown.
func (c *Client) ensureAnon() {
	c.anonMu.Lock()
	if c.anonDone || c.anonInFlight || time.Now().Before(c.anonRetryAt) {
		c.anonMu.Unlock()
		return
	}
	c.anonInFlight = true
	c.mu.Lock()
	startGen := c.cookieGen
	c.mu.Unlock()
	c.anonMu.Unlock()

	regCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	deviceID := randomDeviceID()
	username := base64Encode(deviceID + " " + cloudmusicEncodeID(deviceID))
	resp, err := c.request(regCtx, "/api/register/anonimous",
		map[string]any{"username": username}, "weapi", false)

	c.anonMu.Lock()
	defer c.anonMu.Unlock()
	c.anonInFlight = false
	c.mu.Lock()
	genMatch := c.cookieGen == startGen
	c.mu.Unlock()
	// Finalize the anonymous state under the same generation check as the
	// token write: if a cookie was cleared while the registration was in
	// flight, applyCookie reset anonDone/anonInFlight so the next request
	// re-registers. Setting anonDone unconditionally here would leave the
	// client with anonDone=true and empty credentials, short-circuiting every
	// later request until the next cookie change.
	if err != nil {
		c.anonErr = err
		if genMatch && (strings.Contains(err.Error(), "code 404") || strings.Contains(err.Error(), "HTTP 404")) {
			c.anonDone = true
		} else if !genMatch {
			// A cookie change already reset the anonymous state; leave the
			// retry cooldown for a fresh cookie generation to re-raise.
			c.anonErr = nil
		} else {
			c.anonRetryAt = time.Now().Add(5 * time.Minute)
			log.Printf("[netease] anonymous registration failed (will retry): %v", err)
		}
		return
	}
	if genMatch {
		c.anonDone = true
		c.anonErr = nil
		if musicA := resp.setCookie["MUSIC_A"]; musicA != "" {
			c.mu.Lock()
			// Only adopt the anonymous token when no account cookie was
			// applied (or re-applied/cleared) while the registration was in
			// flight; otherwise the account credentials win and stay
			// authoritative. Credentials are judged by their parsed values, so
			// a malformed cookie (non-empty but no MUSIC_U/MUSIC_A) still lets
			// the anonymous token through.
			if c.musicU == "" && c.musicA == "" {
				c.musicA = musicA
				c.deviceID = deviceID
			}
			c.mu.Unlock()
		}
	}
}

type apiResponse struct {
	body      map[string]any
	setCookie map[string]string
}

// request performs a single API call with the given crypto mode
// ("weapi", "eapi", "api") and returns the parsed JSON body.
func (c *Client) request(ctx context.Context, uri string, data map[string]any, mode string, allowAnon bool) (*apiResponse, error) {
	// Pace the request so sustained scans do not trip the server-side 405
	// throttle; a cancelled context aborts the wait.
	if err := c.rateLimit(ctx); err != nil {
		return nil, err
	}
	// Apply the runtime cookie before the anon flow so user-provided
	// credentials take precedence over anonymous registration. A provider
	// change (including a clear to "") is applied exactly once. Credential
	// clearing and the anonymous-state reset are done atomically (anonMu then
	// mu, the same order ensureAnon uses) so no concurrent request can slip a
	// request out with empty credentials while anonDone is still true.
	c.mu.Lock()
	provider := c.cookieProvider
	c.mu.Unlock()

	if provider != nil {
		// Apply the runtime cookie (falling back to the static config value
		// when cleared) before the anon flow so user-provided credentials
		// take precedence over anonymous registration. A change — including a
		// clear back to anonymous — is applied exactly once and atomically
		// resets the anonymous state.
		c.applyCookie(provider())
	}
	if allowAnon {
		c.ensureAnon()
	}

	now := time.Now()
	deviceID := c.getDeviceID()
	musicA := c.getMusicA()

	cookie := c.buildBaseCookie(uri, deviceID)
	headers := http.Header{}

	var body url.Values
	var reqURL string

	switch mode {
	case "weapi":
		data["csrf_token"] = cookie["__csrf"]
		enc, err := weapi(data)
		if err != nil {
			return nil, err
		}
		body = url.Values{"params": {enc["params"]}, "encSecKey": {enc["encSecKey"]}}
		reqURL = domain + "/weapi/" + uri[5:]
		headers.Set("Referer", domain)
		headers.Set("User-Agent", userAgentWeAPI)
		headers.Set("Cookie", cookieString(cookie))
	case "eapi", "api":
		header := map[string]string{
			"osver":       cookie["osver"],
			"deviceId":    deviceID,
			"os":          cookie["os"],
			"appver":      cookie["appver"],
			"versioncode": cookie["versioncode"],
			"mobilename":  cookie["mobilename"],
			"buildver":    strconv.FormatInt(now.Unix(), 10),
			"resolution":  "1920x1080",
			"__csrf":      cookie["__csrf"],
			"channel":     cookie["channel"],
			"requestId":   fmt.Sprintf("%d_%04d", now.UnixMilli(), rand.Intn(1000)),
		}
		if musicA != "" {
			header["MUSIC_A"] = musicA
		}
		if musicU := c.getMusicU(); musicU != "" {
			header["MUSIC_U"] = musicU
		}
		headers.Set("Cookie", cookieString(header))
		headers.Set("User-Agent", userAgentAPI)
		if mode == "eapi" {
			data["header"] = header
			enc, err := eapi(uri, data)
			if err != nil {
				return nil, err
			}
			body = url.Values{"params": {enc["params"]}}
			reqURL = apiDomain + "/eapi/" + uri[5:]
		} else {
			body = toValues(data)
			reqURL = apiDomain + uri
		}
	case "linuxapi":
		enc, err := linuxapi(map[string]any{
			"method": "POST",
			"url":    domain + uri,
			"params": data,
		})
		if err != nil {
			return nil, err
		}
		body = url.Values{"eparams": {enc["eparams"]}}
		reqURL = domain + "/api/linux/forward"
		headers.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/60.0.3112.90 Safari/537.36")
		headers.Set("Cookie", cookieString(cookie))
	default:
		return nil, fmt.Errorf("unknown crypto mode: %s", mode)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header = headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Log the path only (the query string carries batch track/artist ids that
	// are noise and may be considered sensitive).
	logURL := reqURL
	if i := strings.IndexByte(logURL, '?'); i >= 0 {
		logURL = logURL[:i]
	}
	log.Printf("[netease] POST %s [%s]", logURL, mode)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("netease request %s: %w", uri, err)
	}
	defer resp.Body.Close()
	log.Printf("[netease] %d %s", resp.StatusCode, logURL)

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("netease %s: HTTP %d: %s", uri, resp.StatusCode, truncate(string(raw), 200))
	}

	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("netease %s: invalid JSON: %w", uri, err)
	}
	if code, ok := toFloat64(obj["code"]); ok {
		if !successCodes[int(code)] {
			msg, _ := obj["msg"].(string)
			return nil, fmt.Errorf("netease %s: code %d: %s", uri, int(code), msg)
		}
	}

	return &apiResponse{body: obj, setCookie: parseSetCookie(resp.Header)}, nil
}

func (c *Client) getDeviceID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deviceID
}

func (c *Client) getMusicA() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.musicA
}

func (c *Client) getMusicU() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.musicU
}

// buildBaseCookie mirrors processCookieObject: anonymous-friendly defaults.
func (c *Client) buildBaseCookie(uri, deviceID string) map[string]string {
	nuid := randomHex(32)
	cookie := map[string]string{
		"__remember_me": "true",
		"ntes_kaola_ad": "1",
		"_ntes_nuid":    nuid,
		"_ntes_nnid":    nuid + "," + strconv.FormatInt(time.Now().UnixMilli(), 10),
		"WNMCID":        randomWNMCID(),
		"WEVNSM":        "1.0.0",
		"osver":         osPC["osver"],
		"deviceId":      deviceID,
		"os":            osPC["os"],
		"channel":       osPC["channel"],
		"appver":        osPC["appver"],
		"versioncode":   "140",
		"mobilename":    "",
		"__csrf":        "",
	}
	if !strings.Contains(uri, "login") {
		cookie["NMTID"] = randomHex(16)
	}
	if a := c.getMusicA(); a != "" {
		cookie["MUSIC_A"] = a
	}
	if u := c.getMusicU(); u != "" {
		cookie["MUSIC_U"] = u
	}
	return cookie
}

func randomDeviceID() string {
	const hexChars = "0123456789ABCDEF"
	var sb strings.Builder
	for i := 0; i < 52; i++ {
		sb.WriteByte(hexChars[rand.Intn(16)])
	}
	return sb.String()
}

func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteByte(hexChars[rand.Intn(16)])
	}
	return sb.String()
}

func randomWNMCID() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	var sb strings.Builder
	for i := 0; i < 6; i++ {
		sb.WriteByte(letters[rand.Intn(26)])
	}
	return sb.String() + "." + strconv.FormatInt(time.Now().UnixMilli(), 10) + ".01.0"
}

// cookieString mirrors cookieObjToString/createHeaderCookie:
// encodeURIComponent(key)=encodeURIComponent(value) joined by "; ".
func cookieString(cookie map[string]string) string {
	parts := make([]string, 0, len(cookie))
	for k, v := range cookie {
		parts = append(parts, encodeURIComponent(k)+"="+encodeURIComponent(v))
	}
	return strings.Join(parts, "; ")
}

func encodeURIComponent(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '!' || c == '~' || c == '*' || c == '\'' || c == '(' || c == ')' {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

func toValues(data map[string]any) url.Values {
	v := url.Values{}
	for k, val := range data {
		switch t := val.(type) {
		case string:
			v.Set(k, t)
		case int:
			v.Set(k, strconv.Itoa(t))
		case int64:
			v.Set(k, strconv.FormatInt(t, 10))
		case float64:
			v.Set(k, strconv.FormatFloat(t, 'f', -1, 64))
		case bool:
			v.Set(k, strconv.FormatBool(t))
		default:
			if b, err := marshalJSON(t); err == nil {
				v.Set(k, b)
			}
		}
	}
	return v
}

func parseSetCookie(h http.Header) map[string]string {
	out := map[string]string{}
	for _, line := range h.Values("Set-Cookie") {
		parts := strings.SplitN(line, ";", 2)
		kv := strings.SplitN(strings.TrimSpace(parts[0]), "=", 2)
		if len(kv) == 2 && kv[0] != "" {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func asFloat64(v any) float64 {
	f, _ := toFloat64(v)
	return f
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
