// ============================================================================
// 文件说明：internal/handler/middleware.go
// 职责概览：定义 HTTP 中间件，负责系统拦截鉴权。
//           包含两大中间件：
//           1. JWTMiddleware：管理员 JWT Bearer Token 验证，保护机密路由。
//              自动放行登录路由与免登录下发的订阅路由。
//           2. IPBlockMiddleware：最外层的安全防线，探测来路客户端 IP。
//              若 IP 已在黑名单（被 IPBlocker 封禁），直接阻断并返回 403 Forbidden，
//              防止恶意网络扫描或暴力破解尝试。
// ============================================================================

package handler

import (
	"net/http"
	"strings"

	"vpnview/internal/auth"
)

// JWTMiddleware 负责截获 `/api/` 路由下的鉴权，解析 Authorization 头中的 Bearer 签名。
// 免除鉴权白名单：
//  - 非 `/api/` 的静态文件资源路由放行。
//  - 管理员密码登录接口 `/api/auth/login` 放行。
//  - 用户订阅配置文件下发接口 `/api/sub/` 必须免鉴权放行，方便客户端随时自动拉取节点。
func JWTMiddleware(authSvc *auth.JWTService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 豁免白名单检查
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/auth/login" ||
			strings.HasPrefix(r.URL.Path, "/api/sub/") {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "未授权访问：授信凭证缺失")
			return
		}

		// 剔除 Bearer 前缀并执行校验
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if err := authSvc.Verify(token); err != nil {
			writeError(w, http.StatusUnauthorized, "未授权访问：无效或已过期的管理员令牌")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// IPBlockMiddleware 网络安全最外层物理防线。
// 会利用 IPBlocker 抓取请求客户端真实 IP，若是黑名单受封禁主机，
// 直接打回并阻断请求（返回 403 Forbidden），不浪费后台 CPU 和数据库开销。
func IPBlockMiddleware(blocker *auth.IPBlocker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := blocker.GetClientIP(r)
		if blocker.IsBlocked(ip) {
			http.Error(w, "访问拒绝：检测到您的 IP 因密码多次尝试失败，已被系统防火墙及面板拦截阻断，已进入黑名单。", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
