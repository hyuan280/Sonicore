package ssrf

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSafeURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"http://127.0.0.1/x", false},                       // loopback
		{"http://169.254.169.254/latest/meta-data/", false}, // cloud metadata
		{"http://10.0.0.5/x", false},                        // private
		{"http://[::1]/x", false},                           // IPv6 loopback
		{"http://user:pass@example.com/x", false},           // userinfo
	}
	for _, c := range cases {
		got := SafeURL(c.raw)
		if got != c.want {
			t.Errorf("SafeURL(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestRedirectGuardBlocksPrivateHop(t *testing.T) {
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1:1", Path: "/x"}}
	via := []*http.Request{{URL: &url.URL{Scheme: "http", Host: "example.com", Path: "/x"}}}
	if err := RedirectGuard(req, via); err == nil {
		t.Fatal("RedirectGuard allowed a redirect to a loopback host")
	}
}

func TestRedirectGuardBlocksHTTPSDowngrade(t *testing.T) {
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com", Path: "/x"}}
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/x"}}}
	if err := RedirectGuard(req, via); err == nil {
		t.Fatal("RedirectGuard allowed an https→http downgrade")
	}
}

func TestRedirectGuardTooManyHops(t *testing.T) {
	via := make([]*http.Request, 10)
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/x"}}
	if err := RedirectGuard(req, via); err == nil {
		t.Fatal("RedirectGuard allowed more than 10 hops")
	}
}
