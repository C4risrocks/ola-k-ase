package main

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "request-unknown"
	}
	return hex.EncodeToString(b)
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	if r.wroteHeader {
		return
	}
	r.statusCode = statusCode
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(data)
}

func (r *responseRecorder) Flush() {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func standardMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" || len(reqID) > 128 || strings.ContainsAny(reqID, "\r\n") {
			reqID = generateRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'")

		rr := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic recovered",
					slog.String("request_id", reqID),
					slog.Any("error", recovered),
					slog.String("stack", string(debug.Stack())),
				)
				if !rr.wroteHeader {
					http.Error(rr, "Internal Server Error", http.StatusInternalServerError)
				}
			}

			slog.Info("request completed",
				slog.String("request_id", reqID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("forwarded_for", r.Header.Get("X-Forwarded-For")),
				slog.String("forwarded_proto", r.Header.Get("X-Forwarded-Proto")),
				slog.Int("status", rr.statusCode),
				slog.Duration("duration", time.Since(start)),
				slog.String("user_agent", truncateLogValue(r.UserAgent(), 256)),
			)
		}()

		next.ServeHTTP(rr, r)
	})
}

func cacheMiddleware(next http.Handler, assetETags map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			if etag := assetETags[r.URL.Path]; etag != "" {
				w.Header().Set("ETag", etag)
				if matchesETag(r.Header.Get("If-None-Match"), etag) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func matchesETag(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

func compressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || !acceptsGzip(r.Header.Get("Accept-Encoding")) || r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}

		gzipWriter := &gzipResponseWriter{ResponseWriter: w, request: r}
		defer func() { _ = gzipWriter.Close() }()
		next.ServeHTTP(gzipWriter, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	request      *http.Request
	gzipWriter   *gzip.Writer
	compressBody bool
	wroteHeader  bool
}

func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.compressBody = isCompressible(w.request, w.Header(), statusCode)
	if w.compressBody {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.compressBody {
		return w.ResponseWriter.Write(data)
	}
	if w.gzipWriter == nil {
		w.gzipWriter = gzip.NewWriter(w.ResponseWriter)
	}
	return w.gzipWriter.Write(data)
}

func (w *gzipResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.compressBody {
		if w.gzipWriter == nil {
			w.gzipWriter = gzip.NewWriter(w.ResponseWriter)
		}
		_ = w.gzipWriter.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) Close() error {
	if w.gzipWriter == nil {
		return nil
	}
	return w.gzipWriter.Close()
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func acceptsGzip(header string) bool {
	for _, token := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(token), ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		for _, parameter := range parts[1:] {
			if strings.EqualFold(strings.TrimSpace(parameter), "q=0") {
				return false
			}
		}
		return true
	}
	return false
}

func isCompressible(r *http.Request, header http.Header, statusCode int) bool {
	if statusCode == http.StatusNoContent || statusCode == http.StatusNotModified || header.Get("Content-Encoding") != "" {
		return false
	}
	contentType := header.Get("Content-Type")
	return strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "application/json") ||
		strings.HasPrefix(contentType, "application/javascript") ||
		strings.HasPrefix(contentType, "application/xml") ||
		strings.HasPrefix(contentType, "image/svg+xml") ||
		(strings.HasPrefix(r.URL.Path, "/static/") && strings.HasSuffix(r.URL.Path, ".css"))
}

func truncateLogValue(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
