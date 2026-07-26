package middleware

import (
	"net/http"
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string]*rateEntry
	limit    int
	window   time.Duration
	cleanup  time.Duration
}

type rateEntry struct {
	count    int
	windowStart time.Time
}

func NewRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		requests: make(map[string]*rateEntry),
		limit:    limit,
		window:   window,
		cleanup:  window * 2,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *rateLimiter) allow(key string) bool {
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

func (rl *rateLimiter) cleanupLoop() {
	for {
		time.Sleep(rl.cleanup)
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.window)
		for k, v := range rl.requests {
			if v.windowStart.Before(cutoff) {
				delete(rl.requests, k)
			}
		}
		rl.mu.Unlock()
	}
}

func RateLimitMiddleware(limiter *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.RemoteAddr
			if !limiter.allow(key) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
