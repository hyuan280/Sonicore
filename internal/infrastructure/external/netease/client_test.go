package netease

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// neteaseRewriter redirects both hardcoded NetEase domains to the test server.
type neteaseRewriter struct {
	target string
	inner  http.RoundTripper
}

func (rt *neteaseRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "music.163.com" || req.URL.Host == "interface.music.163.com" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.target
	}
	return rt.inner.RoundTrip(req)
}

func newFakeClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewClient()
	c.http = &http.Client{Transport: &neteaseRewriter{
		target: strings.TrimPrefix(srv.URL, "http://"),
		inner:  http.DefaultTransport,
	}}
	return c
}

// withAnonWrapper answers the anonymous registration endpoint first, then
// delegates everything else to inner.
func withAnonWrapper(inner http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/weapi/register/anonimous" {
			w.Header().Add("Set-Cookie", "MUSIC_A=anon-token; Path=/; HttpOnly")
			w.Write([]byte(`{"code":200}`))
			return
		}
		inner(w, r)
	}
}

func TestEncodeURIComponent(t *testing.T) {
	assert.Equal(t, "abcXYZ019-_.!~*'()", encodeURIComponent("abcXYZ019-_.!~*'()"))
	assert.Equal(t, "%20%2F%3D%25", encodeURIComponent(" /=%"))
	assert.Equal(t, "%E4%B8%AD", encodeURIComponent("中"))
	assert.Equal(t, "", encodeURIComponent(""))
}

func TestCookieString(t *testing.T) {
	s := cookieString(map[string]string{"a": "1", "b": "x y"})
	assert.Contains(t, s, "a=1")
	assert.Contains(t, s, "b=x%20y")
	assert.Contains(t, s, "; ")
}

func TestToValues(t *testing.T) {
	v := toValues(map[string]any{
		"s":    "query",
		"n":    42,
		"big":  int64(1234567890123),
		"f":    1.5,
		"flag": true,
		"obj":  map[string]any{"k": "v"},
	})
	assert.Equal(t, "query", v.Get("s"))
	assert.Equal(t, "42", v.Get("n"))
	assert.Equal(t, "1234567890123", v.Get("big"))
	assert.Equal(t, "1.5", v.Get("f"))
	assert.Equal(t, "true", v.Get("flag"))
	assert.Equal(t, `{"k":"v"}`, v.Get("obj"))
}

func TestParseSetCookie(t *testing.T) {
	h := http.Header{}
	h.Add("Set-Cookie", "MUSIC_A=token1; Path=/; HttpOnly")
	h.Add("Set-Cookie", "MUSIC_U=token2; Max-Age=3600")
	h.Add("Set-Cookie", "=empty-name")

	out := parseSetCookie(h)
	assert.Equal(t, "token1", out["MUSIC_A"])
	assert.Equal(t, "token2", out["MUSIC_U"])
	assert.NotContains(t, out, "")
}

func TestToFloat64AndHelpers(t *testing.T) {
	f, ok := toFloat64(3.5)
	assert.True(t, ok)
	assert.Equal(t, 3.5, f)

	f, ok = toFloat64(json.Number("7"))
	assert.True(t, ok)
	assert.Equal(t, float64(7), f)

	_, ok = toFloat64("nope")
	assert.False(t, ok)

	assert.Equal(t, float64(9), asFloat64(json.Number("9")))
	assert.Equal(t, "s", asString("s"))
	assert.Equal(t, "", asString(123))
}

func TestIdString(t *testing.T) {
	assert.Equal(t, "12345678901234567", idString(json.Number("12345678901234567")), "exact precision preserved")
	assert.Equal(t, "42", idString(float64(42)))
	assert.Equal(t, "abc", idString("abc"))
	assert.Equal(t, "", idString(nil))
	assert.Equal(t, "", idString(true))
}

func TestIsNumericID(t *testing.T) {
	assert.True(t, isNumericID("123"))
	assert.False(t, isNumericID(""))
	assert.False(t, isNumericID("12a"))
	assert.False(t, isNumericID("-1"))
}

func TestRandomGenerators(t *testing.T) {
	assert.Len(t, randomDeviceID(), 52)
	assert.NotEqual(t, randomDeviceID(), randomDeviceID())
	assert.Len(t, randomHex(16), 16)
	assert.Contains(t, randomWNMCID(), ".01.0")
}

func TestSetCookie(t *testing.T) {
	c := NewClient()
	c.SetCookie("MUSIC_A=aaa; MUSIC_U=uuu; other=ignored")
	assert.Equal(t, "aaa", c.getMusicA())
	assert.Equal(t, "uuu", c.getMusicU())

	c.SetCookie("") // no-op
	assert.Equal(t, "aaa", c.getMusicA())

	c.SetCookie("garbage-without-equals")
	assert.Equal(t, "aaa", c.getMusicA())
}

func TestClientRequestAPIPlaintext(t *testing.T) {
	var gotMethod, gotCT, gotPath string
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		assert.Contains(t, r.Header.Get("User-Agent"), "NeteaseMusic")
		r.ParseForm()
		assert.Equal(t, "hello", r.PostForm.Get("s"))
		assert.Equal(t, "1", r.PostForm.Get("type"))
		assert.Equal(t, "true", r.PostForm.Get("flag"))
		w.Write([]byte(`{"code":200,"data":"ok"}`))
	}))

	// "api" mode posts plaintext form values and needs no anon registration
	body, err := c.request(context.Background(), "/api/foo",
		map[string]any{"s": "hello", "type": 1, "flag": true}, "api", false)
	require.NoError(t, err)
	assert.Equal(t, "ok", body.body["data"])
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Contains(t, gotCT, "application/x-www-form-urlencoded")
	assert.Equal(t, "/api/foo", gotPath)
}

func TestClientRequestEapiEncrypted(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/eapi/toplist", r.URL.Path)
		assert.Contains(t, r.Header.Get("User-Agent"), "NeteaseMusic")
		r.ParseForm()
		params := r.PostForm.Get("params")
		require.NotEmpty(t, params)
		assertUppercaseHex(t, params, "eapi params are uppercase hex")
		w.Write([]byte(`{"code":200}`))
	}))

	_, err := c.request(context.Background(), "/api/toplist", map[string]any{}, "eapi", true)
	require.NoError(t, err)
}

func TestClientRequestWeapiEncrypted(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/weapi/v3/song/detail", r.URL.Path, "weapi strips the /api prefix")
		assert.Contains(t, r.Header.Get("Referer"), "music.163.com")
		r.ParseForm()
		require.NotEmpty(t, r.PostForm.Get("params"))
		require.NotEmpty(t, r.PostForm.Get("encSecKey"))
		w.Write([]byte(`{"code":200}`))
	}))

	_, err := c.request(context.Background(), "/api/v3/song/detail",
		map[string]any{"c": "[{\"id\":1}]"}, "weapi", true)
	require.NoError(t, err)
}

func TestClientRequestLinuxapi(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/linux/forward", r.URL.Path)
		r.ParseForm()
		eparams := r.PostForm.Get("eparams")
		require.NotEmpty(t, eparams)
		assertUppercaseHex(t, eparams, "linuxapi params are uppercase hex")
		w.Write([]byte(`{"code":200}`))
	})

	_, err := c.request(context.Background(), "/api/foo", map[string]any{"k": "v"}, "linuxapi", false)
	require.NoError(t, err)
}

func TestClientRequestUnknownMode(t *testing.T) {
	c := NewClient()
	_, err := c.request(context.Background(), "/api/foo", nil, "bogus", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown crypto mode")
}

func TestClientRequestNon200(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("denied"))
	}))

	_, err := c.request(context.Background(), "/api/foo", nil, "api", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
	assert.Contains(t, err.Error(), "denied")
}

func TestClientRequestInvalidJSON(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":200,`))
	}))

	_, err := c.request(context.Background(), "/api/foo", nil, "api", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestClientRequestFailureCode(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":500,"msg":"server boom"}`))
	}))

	_, err := c.request(context.Background(), "/api/foo", nil, "api", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "code 500")
	assert.Contains(t, err.Error(), "server boom")
}

func TestClientRequestSuccessCodes(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":302}`))
	}))

	_, err := c.request(context.Background(), "/api/foo", nil, "api", false)
	require.NoError(t, err, "302 is an accepted success code")
}

func TestEnsureAnon404MarksPermanent(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":404}`))
	})

	c.ensureAnon()
	assert.True(t, c.anonDone, "404 should permanently skip anon registration")
	require.Error(t, c.anonErr)
}

func TestEnsureAnonSuccessSetsMusicA(t *testing.T) {
	c := newFakeClient(t, withAnonWrapper(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":200}`))
	}))

	c.ensureAnon()
	assert.True(t, c.anonDone)
	assert.Equal(t, "anon-token", c.getMusicA())
}

func TestEnsureAnonTransientFailureRetries(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c.ensureAnon()
	assert.False(t, c.anonDone)
	assert.False(t, c.anonRetryAt.IsZero(), "retry cooldown should be set")
	require.Error(t, c.anonErr)
}

func assertUppercaseHex(t *testing.T, s, msg string) {
	t.Helper()
	if len(s)%2 != 0 {
		t.Fatalf("%s: odd length", msg)
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'F') && !(c >= '0' && c <= '9') {
			t.Fatalf("%s: non-hex char %q", msg, c)
		}
	}
}
