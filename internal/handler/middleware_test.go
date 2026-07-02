package handler

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnview/internal/config"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	sec := config.SecurityConfig{
		CSP:                   "default-src 'self'",
		HSTSEnabled:           true,
		HSTSMaxAge:            123,
		HSTSIncludeSubDomains: true,
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "https://vpnview.local/", nil)
	req.TLS = &tls.ConnectionState{}
	rr := httptest.NewRecorder()
	SecurityHeadersMiddleware(sec, next).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("CSP = %q", got)
	}
	if got := rr.Header().Get("Strict-Transport-Security"); got != "max-age=123; includeSubDomains" {
		t.Fatalf("HSTS = %q", got)
	}
}

func TestSecurityHeadersMiddlewareDoesNotEmitHSTSOnHTTP(t *testing.T) {
	sec := config.SecurityConfig{
		CSP:         "default-src 'self'",
		HSTSEnabled: true,
		HSTSMaxAge:  123,
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "http://vpnview.local/", nil)
	rr := httptest.NewRecorder()
	SecurityHeadersMiddleware(sec, next).ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS on HTTP = %q, want empty", got)
	}
}
