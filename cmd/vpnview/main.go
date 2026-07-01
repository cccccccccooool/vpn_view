package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"vpnview/internal/adapter/ddns/cloudflare"
	_ "vpnview/internal/adapter/singbox"
	"vpnview/internal/adapter/store/sqlite"
	_ "vpnview/internal/adapter/stub"
	"vpnview/internal/auth"
	"vpnview/internal/config"
	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/handler"
	"vpnview/internal/port"
	"vpnview/internal/service"
	"vpnview/web"
)

func main() {
	configPath := flag.String("config", "config.yaml", "config YAML path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	store, err := buildStore(cfg)
	if err != nil {
		slog.Error("failed to initialize SQLite store", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	coreManager, err := buildCoreManager(cfg)
	if err != nil {
		slog.Error("failed to initialize VPN cores", "err", err)
		os.Exit(1)
	}
	defer coreManager.Close()
	logLoadedCores(coreManager)

	authSvc := auth.NewJWTService(cfg.Auth.Secret, cfg.GetTokenTTL())
	blocker := auth.NewIPBlocker()
	userSvc := service.NewUserService(store, coreManager, cfg.Limits)

	syncEnabledUsers(store, coreManager)

	trafficSvc := service.NewTrafficService(store, coreManager, cfg.GetPollInterval())
	userSvc.AttachTrafficService(trafficSvc)
	lifecycleSvc := service.NewLifecycleService(store, coreManager, cfg.Limits, trafficSvc, cfg.GetPollInterval())
	subSvc := service.NewSubscriptionService(coreManager, cfg.Subscription)
	ddnsSvc := buildDDNSService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go trafficSvc.Start(ctx)
	go lifecycleSvc.Start(ctx)
	if ddnsSvc != nil {
		go ddnsSvc.Start(ctx)
	}

	router := handler.NewRouter(authSvc, userSvc, trafficSvc, subSvc, coreManager, cfg, web.FS, blocker)
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("VPNView HTTP server started", "listen", cfg.Server.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server exited", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown failed", "err", err)
	}
}

func buildStore(cfg *config.Config) (port.UserStore, error) {
	if dir := filepath.Dir(cfg.Store.SQLite.Path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	return sqlite.New(cfg.Store.SQLite.Path)
}

func buildCoreManager(cfg *config.Config) (*core.Manager, error) {
	return core.NewManager(cfg.Cores, cfg.Subscription.Domain)
}

func logLoadedCores(manager *core.Manager) {
	for _, rt := range manager.List() {
		slog.Info("VPN core loaded", "core_id", rt.ID, "type", rt.Type, "enabled", rt.Enabled, "status", rt.Status, "capabilities", rt.Capabilities.String())
	}
}

func syncEnabledUsers(store port.UserStore, manager *core.Manager) {
	slog.Info("starting user self-healing sync")
	users, err := store.List(context.Background())
	if err != nil {
		slog.Error("failed to read users for startup sync", "err", err)
		return
	}
	synced := 0
	for _, user := range users {
		if !user.Enabled {
			continue
		}
		adapter, rt, err := manager.SelectForUser(user)
		if err != nil {
			slog.Warn("skipping user sync because target core is unavailable", "id", user.ID, "core_id", user.CoreID, "err", err)
			continue
		}
		if !adapter.Capabilities().Has(domain.CapAddUser) {
			slog.Warn("skipping user sync because core cannot add users", "id", user.ID, "core_id", rt.ID)
			continue
		}
		if err := adapter.AddUser(context.Background(), user.ID, user.Credentials); err != nil {
			slog.Error("failed to sync user to core", "id", user.ID, "core_id", rt.ID, "err", err)
			continue
		}
		synced++
	}
	slog.Info("user self-healing sync finished", "synced", synced)
}

func buildDDNSService(cfg *config.Config) *service.DDNSService {
	if cfg.DDNS == nil || cfg.DDNS.Provider == "" {
		return nil
	}
	switch strings.ToLower(cfg.DDNS.Provider) {
	case "cloudflare":
		return service.NewDDNSService(cloudflare.New(cfg.DDNS), cfg.DDNS, cfg.GetDDNSCheckInterval())
	default:
		slog.Warn("unsupported DDNS provider; DDNS disabled", "provider", cfg.DDNS.Provider)
		return nil
	}
}
