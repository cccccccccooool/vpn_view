package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"vpnview/internal/auth"
	"vpnview/internal/config"
	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/service"
)

func NewRouter(
	authSvc *auth.JWTService,
	userSvc *service.UserService,
	trafficSvc *service.TrafficService,
	subSvc *service.SubscriptionService,
	cores *core.Manager,
	cfg *config.Config,
	webFS fs.FS,
	blocker *auth.IPBlocker,
) http.Handler {
	mux := http.NewServeMux()

	authH := NewAuthHandler(authSvc, blocker)
	userH := NewUserHandler(userSvc, trafficSvc)
	statsH := NewStatsHandler(trafficSvc)
	capH := NewCapabilityHandler(cores, cfg)
	subH := NewSubscriptionHandler(subSvc, userSvc)
	caps := cores.Default().Capabilities()

	mux.HandleFunc("POST /api/auth/login", authH.Login)
	mux.HandleFunc("POST /api/auth/logout", authH.Logout)
	mux.HandleFunc("GET /api/capabilities", capH.GetCapabilities)
	mux.HandleFunc("GET /api/sub/{id}", subH.GetSubscription)

	mux.HandleFunc("GET /api/stats/global", statsH.GetGlobalStats)
	mux.HandleFunc("GET /api/stats/connections", statsH.GetConnections)
	mux.HandleFunc("DELETE /api/stats/connections/{id}", statsH.KillConnection)
	mux.HandleFunc("GET /api/users", userH.ListUsers)
	mux.HandleFunc("PATCH /api/users/{id}", userH.UpdateUser)

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

	return SecurityHeadersMiddleware(IPBlockMiddleware(blocker, CSRFMiddleware(JWTMiddleware(authSvc, mux))))
}

func notSupported(w http.ResponseWriter, r *http.Request) {
	writeDomainError(w, domain.ErrNotSupported)
}

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
