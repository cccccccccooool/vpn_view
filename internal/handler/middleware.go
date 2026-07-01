package handler

import (
	"net/http"
	"strings"

	"vpnview/internal/auth"
)

const (
	sessionCookieName = "vpnview_session"
	csrfCookieName    = "vpnview_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

func JWTMiddleware(authSvc *auth.JWTService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/auth/login" ||
			strings.HasPrefix(r.URL.Path, "/api/sub/") {
			next.ServeHTTP(w, r)
			return
		}

		token := bearerToken(r)
		if token == "" {
			if cookie, err := r.Cookie(sessionCookieName); err == nil {
				token = cookie.Value
			}
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization credentials")
			return
		}
		if err := authSvc.Verify(token); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired authorization token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/auth/login" ||
			strings.HasPrefix(r.URL.Path, "/api/sub/") ||
			isSafeMethod(r.Method) ||
			bearerToken(r) != "" {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" || r.Header.Get(csrfHeaderName) != cookie.Value {
			writeError(w, http.StatusForbidden, "csrf token mismatch")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "clipboard-read=(), geolocation=(), microphone=(), camera=()")
		h.Set("X-Frame-Options", "DENY")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func IPBlockMiddleware(blocker *auth.IPBlocker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := blocker.GetClientIP(r)
		if blocker.IsBlocked(ip) {
			http.Error(w, "access denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return ""
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
