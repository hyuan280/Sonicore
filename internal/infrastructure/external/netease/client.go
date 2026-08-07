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

	anonMu      sync.Mutex
	anonInFlight bool
	anonDone    bool
	anonRetryAt time.Time
	anonErr     error
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		deviceID: randomDeviceID(),
	}
}

func (c *Client) SetCookie(cookie string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cookie == "" {
		return
	}
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
	if err != nil {
		c.anonErr = err
		if strings.Contains(err.Error(), "code 404") || strings.Contains(err.Error(), "HTTP 404") {
			c.anonDone = true
		} else {
			c.anonRetryAt = time.Now().Add(5 * time.Minute)
			log.Printf("[netease] anonymous registration failed (will retry): %v", err)
		}
		return
	}
	c.anonDone = true
	c.anonErr = nil
	if musicA := resp.setCookie["MUSIC_A"]; musicA != "" {
		c.mu.Lock()
		c.musicA = musicA
		c.deviceID = deviceID
		c.mu.Unlock()
	}
}

type apiResponse struct {
	body     map[string]any
	setCookie map[string]string
}

// request performs a single API call with the given crypto mode
// ("weapi", "eapi", "api") and returns the parsed JSON body.
func (c *Client) request(ctx context.Context, uri string, data map[string]any, mode string, allowAnon bool) (*apiResponse, error) {
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

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("netease request %s: %w", uri, err)
	}
	defer resp.Body.Close()

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
