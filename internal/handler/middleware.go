// middleware.go 定义 HTTP 中间件，包括 JWT 鉴权和 IP 封禁。
package handler

import (
	"net/http"
	"strings"

	"vpnview/internal/auth"
)

// JWTMiddleware 对 /api/ 路径下的请求执行 JWT Bearer Token 校验。
// 登录接口 (/api/auth/login) 和订阅接口 (/api/sub/) 不需要鉴权，直接放行。
func JWTMiddleware(authSvc *auth.JWTService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/auth/login" ||
			strings.HasPrefix(r.URL.Path, "/api/sub/") {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := authSvc.Verify(strings.TrimPrefix(authHeader, "Bearer ")); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// IPBlockMiddleware 在最外层拦截黑名单中的恶意 IP 请求，返回 403 阻断一切访问
func IPBlockMiddleware(blocker *auth.IPBlocker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := blocker.GetClientIP(r)
		if blocker.IsBlocked(ip) {
			http.Error(w, "Access Denied: Your IP has been blocked due to multiple failed login attempts.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
