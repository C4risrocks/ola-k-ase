package main

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

func sessionTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie("session_token")
	if err != nil || cookie == nil {
		return ""
	}
	return cookie.Value
}

// sameOriginRequest rejects mutations that do not carry a matching Origin or
// Referer header. Browsers send at least one of them for form and fetch POSTs.
func sameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return false
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// requireAdminMutation enforces method, same-origin, session and CSRF checks
// for every state-changing admin endpoint.
func requireAdminMutation(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !sameOriginRequest(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}

	record, ok := lookupSession(db, sessionTokenFromRequest(r))
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	if !csrfMatches(record, r.Header.Get("X-CSRF-Token")) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func sessionCookie(name, value string, httpOnly bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: httpOnly,
		SameSite: http.SameSiteLaxMode,
		Secure:   appConfig.AppEnv == "production",
	}
}

func clearCookie(name string, httpOnly bool) *http.Cookie {
	cookie := sessionCookie(name, "", httpOnly)
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(0, 0)
	return cookie
}
