package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiterAllowWithinLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	assert.True(t, rl.allow("client-a"))
	assert.True(t, rl.allow("client-a"))
	assert.True(t, rl.allow("client-a"))
}

func TestRateLimiterRejectsOverLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	assert.True(t, rl.allow("client-a"))
	assert.True(t, rl.allow("client-a"))
	assert.False(t, rl.allow("client-a"))
	assert.False(t, rl.allow("client-a"))
}

func TestRateLimiterIndependentKeys(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	assert.True(t, rl.allow("client-a"))
	assert.False(t, rl.allow("client-a"))
	assert.True(t, rl.allow("client-b"), "different key should not be affected")
}

func TestRateLimiterWindowReset(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	assert.True(t, rl.allow("client-a"))
	assert.False(t, rl.allow("client-a"))

	entry := rl.requests["client-a"]
	assert.NotNil(t, entry)
	entry.windowStart = time.Now().Add(-2 * time.Minute)

	assert.True(t, rl.allow("client-a"), "new window should reset the counter")
	assert.False(t, rl.allow("client-a"))
}

func TestRateLimiterLimitZero(t *testing.T) {
	rl := NewRateLimiter(0, time.Minute)
	assert.True(t, rl.allow("client-a"), "first request always opens a new window")
	assert.False(t, rl.allow("client-a"), "subsequent requests exceed limit 0")
}

func TestRateLimitMiddlewareAllowsAndRejects(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	SetTrustedProxies(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:4000"

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	assert.Equal(t, http.StatusOK, rec1.Code)

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
	assert.Equal(t, "60", rec2.Header().Get("Retry-After"))
}
