package handler

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnview/internal/config"
)

func TestShouldUseSecureCookieByDeploymentMode(t *testing.T) {
	httpReq := httptest.NewRequest(http.MethodGet, "http://panel.example.com/api/auth/login", nil)
	httpsReq := httptest.NewRequest(http.MethodGet, "https://panel.example.com/api/auth/login", nil)
	httpsReq.TLS = &tls.ConnectionState{}
	proxyReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:19463/api/auth/login", nil)
	proxyReq.Header.Set("X-Forwarded-Proto", "https")

	tests := []struct {
		name string
		req  *http.Request
		sec  config.SecurityConfig
		want bool
	}{
		{
			name: "insecure http",
			req:  httpReq,
			sec:  config.SecurityConfig{DeploymentMode: "insecure", CookieSecure: "auto"},
			want: false,
		},
		{
			name: "self signed direct https",
			req:  httpsReq,
			sec:  config.SecurityConfig{DeploymentMode: "self_signed", CookieSecure: "auto"},
			want: true,
		},
		{
			name: "self signed forwarded https",
			req:  proxyReq,
			sec:  config.SecurityConfig{DeploymentMode: "self_signed", CookieSecure: "auto"},
			want: true,
		},
		{
			name: "strict http still secure",
			req:  httpReq,
			sec:  config.SecurityConfig{DeploymentMode: "strict", CookieSecure: "auto"},
			want: true,
		},
		{
			name: "manual never overrides strict",
			req:  httpsReq,
			sec:  config.SecurityConfig{DeploymentMode: "strict", CookieSecure: "never"},
			want: false,
		},
		{
			name: "manual always overrides insecure",
			req:  httpReq,
			sec:  config.SecurityConfig{DeploymentMode: "insecure", CookieSecure: "always"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseSecureCookie(tt.req, tt.sec); got != tt.want {
				t.Fatalf("shouldUseSecureCookie() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCookiePaths(t *testing.T) {
	if got := cookiePath(sessionCookieName); got != "/api/" {
		t.Fatalf("session cookie path = %q, want /api/", got)
	}
	if got := cookiePath(csrfCookieName); got != "/" {
		t.Fatalf("csrf cookie path = %q, want /", got)
	}
}
