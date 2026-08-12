package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetTrustedProxies(t *testing.T) {
	SetTrustedProxies([]string{"10.0.0.0/8", "172.16.0.1", "invalid-cidr", "", "bad/ip"})
	assert.Equal(t, 2, len(trustedProxies), "valid entries: /8 CIDR, bare IP; three invalid skipped")

	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})
}

func TestIsTrusted(t *testing.T) {
	SetTrustedProxies([]string{"10.0.0.0/8", "192.168.1.5", "2001:db8::/32"})
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"inside ipv4 cidr", "10.1.2.3:4000", true},
		{"cidr network address", "10.0.0.0:80", true},
		{"bare ip inside cidr", "10.9.9.9", true},
		{"exact ip match", "192.168.1.5:8080", true},
		{"outside cidr", "11.0.0.1:80", false},
		{"different private range", "172.16.0.1:80", false},
		{"ipv6 inside cidr", "[2001:db8:1::1]:443", true},
		{"ipv6 outside cidr", "[2001:db9::1]:443", false},
		{"invalid ip", "not-an-ip", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isTrusted(tt.addr))
		})
	}
}

func TestIsTrustedNoProxiesConfigured(t *testing.T) {
	SetTrustedProxies(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})
	assert.False(t, isTrusted("10.0.0.1:80"))
	assert.False(t, isTrusted("127.0.0.1:80"))
}

func TestClientIPFromRemoteAddr(t *testing.T) {
	SetTrustedProxies(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	req := httptestNewRequest()
	req.RemoteAddr = "203.0.113.7:5678"
	assert.Equal(t, "203.0.113.7", ClientIP(req))
}

func TestClientIPUntrustedIgnoresForwardedHeaders(t *testing.T) {
	SetTrustedProxies(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	req := httptestNewRequest()
	req.RemoteAddr = "203.0.113.7:5678"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("X-Forwarded-For", "5.6.7.8, 6.6.6.6")
	assert.Equal(t, "203.0.113.7", ClientIP(req))
}

func TestClientIPTrustedProxyUsesXRealIP(t *testing.T) {
	SetTrustedProxies([]string{"172.16.0.0/16"})
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	req := httptestNewRequest()
	req.RemoteAddr = "172.16.0.2:8080"
	req.Header.Set("X-Real-IP", "9.9.9.9")
	assert.Equal(t, "9.9.9.9", ClientIP(req))
}

func TestClientIPTrustedProxyUsesForwardedFor(t *testing.T) {
	SetTrustedProxies([]string{"172.16.0.0/16"})
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	tests := []struct {
		name string
		xff  string
		want string
	}{
		{"single untrusted", "8.8.8.8", "8.8.8.8"},
		{"chain rightmost untrusted", "8.8.8.8, 172.16.0.1", "8.8.8.8"},
		{"chain of trusted then untrusted", "1.2.3.4, 172.16.0.2, 172.16.0.1", "1.2.3.4"},
		{"all trusted falls through to remote", "172.16.0.2, 172.16.0.1", "172.16.0.2"},
		{"garbage entries skipped", "not-an-ip, 1.2.3.4", "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptestNewRequest()
			req.RemoteAddr = "172.16.0.2:8080"
			req.Header.Set("X-Forwarded-For", tt.xff)
			assert.Equal(t, tt.want, ClientIP(req))
		})
	}
}

func TestClientIPMalformedRemoteAddr(t *testing.T) {
	SetTrustedProxies(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	req := httptestNewRequest()
	req.RemoteAddr = "no-port-here"
	assert.Equal(t, "no-port-here", ClientIP(req))
}

func httptestNewRequest() *http.Request {
	return &http.Request{Header: http.Header{}}
}
