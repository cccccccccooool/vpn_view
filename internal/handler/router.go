// Package handler 提供 HTTP 请求处理器，包括路由注册、中间件、认证和各类业务 API 的实现。
// HTTP handlers for VPNView management panel API.
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

// NewRouter 创建并返回完整的 HTTP 路由树。
// 根据 VPN adapter 支持的能力动态注册路由，不支持的操作返回 501；
// 同时挂载 SPA 前端静态文件服务和 JWT / IP 黑名单中间件。
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
	caps := adapter.Capabilities()

	mux.HandleFunc("POST /api/auth/login", authH.Login)
	mux.HandleFunc("GET /api/capabilities", capH.GetCapabilities)
	mux.HandleFunc("GET /api/stats/global", statsH.GetGlobalStats)
	mux.HandleFunc("GET /api/stats/connections", statsH.GetConnections)
	mux.HandleFunc("DELETE /api/stats/connections/{id}", statsH.KillConnection)
	mux.HandleFunc("GET /api/users", userH.ListUsers)
	mux.HandleFunc("PATCH /api/users/{id}", userH.UpdateUser)
	mux.HandleFunc("GET /api/sub/{id}", subH.GetSubscription)

	if caps.Has(domain.CapAddUser) {
		mux.HandleFunc("POST /api/users", userH.CreateUser)
	} else {
		mux.HandleFunc("POST /api/users", notSupported)
	}
	if caps.Has(domain.CapRemoveUser) {
		mux.HandleFunc("DELETE /api/users/{id}", userH.DeleteUser)
	} else {
		mux.HandleFunc("DELETE /api/users/{id}", notSupported)
	}

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "api endpoint not found")
	})
	mux.Handle("/", spaFileServer(webFS))

	return IPBlockMiddleware(blocker, JWTMiddleware(authSvc, mux))
}

// notSupported 是能力未启用时的占位处理器，返回 501 Not Implemented。
func notSupported(w http.ResponseWriter, r *http.Request) {
	writeDomainError(w, domain.ErrNotSupported)
}

// spaFileServer 返回支持单页应用 (SPA) 的静态文件服务器。
// 对于存在的静态资源直接返回文件内容；不存在的路径一律回退到 index.html，
// 以便前端路由正常工作。
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

		serveIndex(w, r, webFS)
	})
}

// serveIndex 从 webFS 中读取 index.html 并以 text/html 响应。
func serveIndex(w http.ResponseWriter, r *http.Request, webFS fs.FS) {
	content, err := fs.ReadFile(webFS, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
