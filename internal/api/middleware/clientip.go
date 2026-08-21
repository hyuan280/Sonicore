package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/sonicore/server/internal/infrastructure/logger"
)

var (
	trustedMu      sync.RWMutex
	trustedProxies []*net.IPNet
)

func SetTrustedProxies(cidrs []string) {
	var parsed []*net.IPNet
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			ip := net.ParseIP(c)
			if ip == nil {
				logger.Warn("[middleware] skip invalid IP %q in trusted_proxies", c)
				continue
			}
			if ip.To4() != nil {
				c = ip.To4().String() + "/32"
			} else {
				c = ip.String() + "/128"
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			logger.Warn("[middleware] skip invalid CIDR %q in trusted_proxies: %v", c, err)
			continue
		}
		parsed = append(parsed, n)
	}
	trustedMu.Lock()
	trustedProxies = parsed
	trustedMu.Unlock()
}

func isTrusted(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	trustedMu.RLock()
	defer trustedMu.RUnlock()
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the real client IP.
//
//   - Docker: add nginx's subnet to trusted_proxies (e.g. 172.17.0.0/16)
//     so forwarded headers from the internal proxy are honored.
//   - Standalone: omit trusted_proxies, RemoteAddr is used directly.
func ClientIP(r *http.Request) string {
	if isTrusted(r.RemoteAddr) {
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			if ip := net.ParseIP(xrip); ip != nil {
				return ip.String()
			}
		}
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			trustedMu.RLock()
			defer trustedMu.RUnlock()
			for i := len(parts) - 1; i >= 0; i-- {
				ip := net.ParseIP(strings.TrimSpace(parts[i]))
				if ip == nil {
					continue
				}
				trusted := false
				for _, n := range trustedProxies {
					if n.Contains(ip) {
						trusted = true
						break
					}
				}
				if !trusted {
					return ip.String()
				}
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		logger.Info("[clientip] untrusted peer %s forwarded X-Real-IP=%s — add to trusted_proxies", peerHost(r.RemoteAddr), xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func peerHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
