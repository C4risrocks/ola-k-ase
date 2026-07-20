package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Generate a random Request ID
func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// responseRecorder captures the status code for logging
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// standardMiddleware wraps all requests
func standardMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = generateRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)

		// Security Headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// CSP: Very strict, allows inline styles/scripts due to HTMX/Alpine, but restricts external sources to unpkg/cdn.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://unpkg.com https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://unpkg.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:;")

		// Setup response recorder
		rr := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		// Handle Panic (Recovery)
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					slog.String("request_id", reqID),
					slog.Any("error", err),
					slog.String("stack", string(debug.Stack())),
				)
				http.Error(rr, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(rr, r)

		// Structured Logging
		duration := time.Since(start)
		slog.Info("request completed",
			slog.String("request_id", reqID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("ip", getClientIP(r)),
			slog.Int("status", rr.statusCode),
			slog.Duration("duration", duration),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

// cacheMiddleware applies specific cache rules based on path
func cacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			// Aggressive cache for static assets
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("ETag", fmt.Sprintf(`"%s"`, Commit)) // Use commit as ETag
		} else {
			// No cache for HTML / HTMX requests
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		next.ServeHTTP(w, r)
	})
}

// getClientIP extracts real IP respecting proxies (Traefik/Coolify)
func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
