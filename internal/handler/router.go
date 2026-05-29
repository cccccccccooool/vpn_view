// ============================================================================
// 文件说明：internal/handler/router.go
// 职责概览：系统核心 HTTP 路由管理器（Router）。
//           负责组装管理面板的全部 HTTP API 路由（端点分发），
//           挂载各种安全控制中间件（IP 黑名单拦截、管理员 JWT 鉴权校验），
//           并且通过读取 VPN 适配器的 Capabilities 能力掩码，动态绑定/开闭相关 API 端点。
//           若适配器不支持某 API，会自动将其映射到 notSupported 统一返回 501 错误，
//           同时集成 SPA 单页应用的前端静态资源文件服务（FS 服务）。
// ============================================================================

package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"vpnview/internal/auth"
	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
	"vpnview/internal/service"
)

// NewRouter 装配并返回系统完整的 HTTP 路由处理器（http.Handler 路由树）。
func NewRouter(
	authSvc *auth.JWTService,
	userSvc *service.UserService,
	trafficSvc *service.TrafficService,
	subSvc *service.SubscriptionService,
	adapter port.VPNAdapter,
	cfg *config.Config,
	webFS fs.FS,
	blocker *auth.IPBlocker,
) http.Handler {
	mux := http.NewServeMux()

	authH := NewAuthHandler(authSvc, blocker)
	userH := NewUserHandler(userSvc, trafficSvc)
	statsH := NewStatsHandler(trafficSvc)
	capH := NewCapabilityHandler(adapter, cfg)
	subH := NewSubscriptionHandler(subSvc, userSvc)
	caps := adapter.Capabilities() // 读取当前 VPN 适配器支持的能力

	// === 无需 JWT 鉴权的公开路由 ===
	mux.HandleFunc("POST /api/auth/login", authH.Login)       // 密码校验登录
	mux.HandleFunc("GET /api/capabilities", capH.GetCapabilities) // 查询系统/适配器能力集
	mux.HandleFunc("GET /api/sub/{id}", subH.GetSubscription) // 获取客户端连接订阅配置文件（免鉴权）

	// === 强 JWT 鉴权的管理员 API 路由 ===
	mux.HandleFunc("GET /api/stats/global", statsH.GetGlobalStats)       // 抓取全局流量统计与看板大屏数据
	mux.HandleFunc("GET /api/stats/connections", statsH.GetConnections)   // 查询当前代理底层的活动连接
	mux.HandleFunc("DELETE /api/stats/connections/{id}", statsH.KillConnection) // 掐断/阻断指定活动网络连接
	mux.HandleFunc("GET /api/users", userH.ListUsers)                   // 获取用户列表
	mux.HandleFunc("PATCH /api/users/{id}", userH.UpdateUser)           // 修改更新单个用户配置

	// === 动态路由开闭安全机制 ===
	// 若底层不支持添加用户，将 POST 路由封死，统一映射到 notSupported
	if caps.Has(domain.CapAddUser) {
		mux.HandleFunc("POST /api/users", userH.CreateUser)
	} else {
		mux.HandleFunc("POST /api/users", notSupported)
	}

	// 若底层不支持删除用户，将 DELETE 路由映射到 notSupported
	if caps.Has(domain.CapRemoveUser) {
		mux.HandleFunc("DELETE /api/users/{id}", userH.DeleteUser)
	} else {
		mux.HandleFunc("DELETE /api/users/{id}", notSupported)
	}

	// 拦截未知的 API 端点请求，返回 404
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "找不到请求的 API 端点")
	})

	// 挂载 SPA 单页前端静态资源文件服务，其余路由全部回退到静态文件
	mux.Handle("/", spaFileServer(webFS))

	// 🏆 包裹安全防护中间件链条：
	// IP 爆破防护层 (IPBlockMiddleware) -> JWT 免登录鉴权层 (JWTMiddleware) -> 业务处理器 (mux)
	return IPBlockMiddleware(blocker, JWTMiddleware(authSvc, mux))
}

// notSupported 占位处理器，专门响应当前适配器不支持的能力接口，统一吐出 501 状态码。
func notSupported(w http.ResponseWriter, r *http.Request) {
	writeDomainError(w, domain.ErrNotSupported)
}

// spaFileServer 创建并返回支持 React/Vue SPA 单页应用路由的静态资源管理器。
// 当访问的静态资源真实存在时直接读取返回；如果路由属于前端内部页面路由（物理不存在），
// 则一律回滚并返回 `index.html`，保障前端路由正常渲染。
func spaFileServer(webFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			serveIndex(w, r, webFS)
			return
		}

		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "." || !fs.ValidPath(clean) {
			http.NotFound(w, r)
			return
		}

		info, err := fs.Stat(webFS, clean)
		if err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 回滚返回 index.html 交付前端 SPA 路由接管
		serveIndex(w, r, webFS)
	})
}

// serveIndex 单独读取并返回 index.html 主入口页面文件。
func serveIndex(w http.ResponseWriter, r *http.Request, webFS fs.FS) {
	content, err := fs.ReadFile(webFS, "index.html")
	if err != nil {
		http.Error(w, "未找到前端主 index.html 入口模板文件", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
