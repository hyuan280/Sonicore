// Package ssrf guards outbound HTTP requests against server-side request
// forgery: targets are restricted to http/https URLs whose host resolves only
// to globally routable addresses, and redirects are re-vetted on every hop.
package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// nonGlobalNetworks lists non-global unicast blocks excluded by IsPublicIP in
// addition to net.IP's built-in loopback/private/link-local/unspecified/
// multicast predicates. 0.0.0.0/8 is the important one: Linux routes it
// locally, so a host like 0.0.0.1 bypasses the IsLoopback/IsUnspecified
// checks and hits the local machine.
var nonGlobalNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
	// IPv6 translation/mapping prefixes that can carry or map to arbitrary
	// IPv4 targets (e.g. 2002:7f00:1:: → 127.0.0.1, 64:ff9b::a00:1 →
	// 10.0.0.1); net.IP's predicates do not cover them.
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
	// RFC 8215 NAT64 local-use prefix (distinct from 64:ff9b::/96; maps
	// private IPv4, e.g. 64:ff9b:1::a00:1 → 10.0.0.1).
	netip.MustParsePrefix("64:ff9b:1::/48"),
	// Deprecated special-use blocks kept closed for completeness: ::/96
	// (IPv4-compatible), fec0::/10 (site-local, RFC 3879) and the 6to4
	// relay anycast 192.88.99.0/24 (RFC 7526).
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("192.88.99.0/24"),
}

// IsPublicIP reports whether ip is a globally routable address.
func IsPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	// Non-global unicast ranges the stdlib predicate misses — CGNAT, the
	// TEST-NET documentation blocks and the reserved 240.0.0.0/4. They are
	// not globally routable, so treating them as reachable would widen the
	// SSRF white-list into carrier-grade or reserved space.
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	for _, p := range nonGlobalNetworks {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// SafeURL reports whether raw is a safe target for an outbound request using a
// background-bounded resolver. Prefer SafeURLCtx when a request context is
// available so cancellation propagates into the DNS lookup.
func SafeURL(raw string) bool {
	return SafeURLCtx(context.Background(), raw)
}

// SafeURLCtx reports whether raw is a safe target for an outbound request:
// http/https only, a non-empty host with no userinfo, and every resolved
// address public. The check is a cheap pre-filter; the resolution is bounded
// and inherits the caller's cancellation so an aborted request does not block
// on a stuck resolver.
func SafeURLCtx(ctx context.Context, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return IsPublicIP(ip)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if !IsPublicIP(a.IP) {
			return false
		}
	}
	return true
}

type allowPrivateKey struct{}

// WithAllowPrivate marks a context so DialContext permits private addresses.
// Used for self-hosted plugin repositories on a private network.
func WithAllowPrivate(ctx context.Context) context.Context {
	return context.WithValue(ctx, allowPrivateKey{}, true)
}

// DialContext returns a DialContext function that re-resolves each host at
// connect time and pins the connection to the resolved addresses, closing the
// DNS-rebinding window between a SafeURL pre-check and the actual dial. Only
// public addresses are allowed unless the request context carries the
// WithAllowPrivate marker.
func DialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		allowPrivate, _ := ctx.Value(allowPrivateKey{}).(bool)
		ips := resolveIPs(ctx, host, allowPrivate)
		if len(ips) == 0 {
			return nil, fmt.Errorf("host %q resolves to no allowed address", host)
		}
		// Try every allowed address (CDNs commonly return several A/AAAA
		// records); a dead first hop must not fail the request when another
		// address works.
		var lastErr error
		for _, ip := range ips {
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
		}
		return nil, lastErr
	}
}

// PublicDialContext is DialContext with private addresses always disallowed,
// regardless of any WithAllowPrivate marker on the request context.
func PublicDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := DialContext(dialer)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d(context.WithValue(ctx, allowPrivateKey{}, false), network, addr)
	}
}

// resolveIPs resolves host to the set of addresses allowed by the policy: a
// literal IP is validated directly; otherwise the host is resolved and
// non-public addresses are dropped unless allowPrivate. The resolution is
// bounded and inherits the caller's cancellation.
func resolveIPs(ctx context.Context, host string, allowPrivate bool) []net.IP {
	if ip := net.ParseIP(host); ip != nil {
		if allowPrivate || IsPublicIP(ip) {
			return []net.IP{ip}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		if allowPrivate || IsPublicIP(a.IP) {
			out = append(out, a.IP)
		}
	}
	return out
}

// RedirectGuard constrains outbound redirects: a bounded hop count, no
// https→http downgrade (protects any Authorization header from being sent in
// the clear) and every hop re-vetted. When the request context carries the
// WithAllowPrivate marker the hop is trusted as long as the chain has not
// passed through a public address (self-hosted private repos); otherwise the
// target must be a safe public URL. Use as http.Client.CheckRedirect.
func RedirectGuard(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("https→http redirect downgrade blocked")
	}
	allow, _ := req.Context().Value(allowPrivateKey{}).(bool)
	if allow {
		// Once any prior hop is public the chain has left the trusted private
		// network, so the private allowance no longer applies.
		for _, prev := range via {
			if SafeURLCtx(req.Context(), prev.URL.String()) {
				allow = false
				break
			}
		}
	}
	if allow {
		return nil
	}
	if !SafeURLCtx(req.Context(), req.URL.String()) {
		return errors.New("redirect to disallowed host")
	}
	return nil
}
