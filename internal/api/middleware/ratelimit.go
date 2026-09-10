package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/sonicore/server/internal/core/domain"
)

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string]*rateEntry
	limit    int
	window   time.Duration
}

type rateEntry struct {
	count       int
	windowStart time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string]*rateEntry),
		limit:    limit,
		window:   window,
	}
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.requests[key]

	if !exists || now.Sub(entry.windowStart) > rl.window {
		rl.requests[key] = &rateEntry{
			count:       1,
			windowStart: now,
		}
		return true
	}

	entry.count++
	return entry.count <= rl.limit
}

// CleanupOnce performs a single pass of request-map pruning, dropping
// entries whose window has expired. Scheduled by the central task system
// instead of a self-managed loop.
func (rl *RateLimiter) CleanupOnce() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.window)
	for k, v := range rl.requests {
		if v.windowStart.Before(cutoff) {
			delete(rl.requests, k)
		}
	}
}

func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ClientIP verifies the TCP peer against trusted_proxies before
			// honoring forwarded headers; untrusted peers use RemoteAddr.
			key := ClientIP(r)
			if !limiter.allow(key) {
				w.Header().Set("Retry-After", "60")
				writeCodedError(w, http.StatusTooManyRequests, domain.ErrTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
