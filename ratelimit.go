package main

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a small in-memory fixed-window limiter.
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

// clientIP returns the address used as the rate limiting key. The direct peer
// is used for public clients. When the peer is loopback, private or link-local
// (a local reverse proxy such as Traefik), the rightmost X-Forwarded-For entry
// is used instead: it is the address the trusted proxy observed, while earlier
// entries are client-controlled and never trusted.
func clientIP(r *http.Request) string {
	peer := peerIP(r.RemoteAddr)
	peerAddr, err := netip.ParseAddr(peer)
	if err != nil || !isLocalPeer(peerAddr) {
		return peer
	}
	if forwarded := rightmostForwardedFor(r.Header.Values("X-Forwarded-For")); forwarded != "" {
		return forwarded
	}
	return peer
}

func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func isLocalPeer(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

func rightmostForwardedFor(values []string) string {
	parts := strings.Split(strings.Join(values, ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		if candidate == "" {
			continue
		}
		if host, _, err := net.SplitHostPort(candidate); err == nil {
			candidate = host
		}
		parsed, err := netip.ParseAddr(candidate)
		if err != nil {
			return ""
		}
		return parsed.Unmap().String()
	}
	return ""
}

var (
	loginLimiter   = newRateLimiter(5, 15*time.Minute)
	contactLimiter = newRateLimiter(5, 10*time.Minute)
)
