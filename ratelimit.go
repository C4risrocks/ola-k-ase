package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a small in-memory fixed-window limiter. It keys on the direct
// peer address only: X-Forwarded-For is never trusted for rate limiting.
type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		attempts: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	var recent []time.Time
	for _, attempt := range l.attempts[key] {
		if attempt.After(cutoff) {
			recent = append(recent, attempt)
		}
	}

	if len(recent) >= l.limit {
		l.attempts[key] = recent
		return false
	}

	l.attempts[key] = append(recent, now)
	if len(l.attempts) > 10000 {
		for candidate, attempts := range l.attempts {
			if len(attempts) == 0 || attempts[len(attempts)-1].Before(cutoff) {
				delete(l.attempts, candidate)
			}
		}
	}
	return true
}

func (l *rateLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts = make(map[string][]time.Time)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var (
	loginLimiter   = newRateLimiter(5, 15*time.Minute)
	contactLimiter = newRateLimiter(5, 10*time.Minute)
)
