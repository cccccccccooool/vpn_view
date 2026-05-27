// VPNView 管理面板主入口程序。
// 负责加载配置、初始化各层组件（存储、适配器、服务、路由）、启动后台任务并监听 HTTP 服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"vpnview/internal/adapter/ddns/cloudflare"
	"vpnview/internal/adapter/singbox"
	"vpnview/internal/adapter/store/sqlite"
	"vpnview/internal/adapter/stub"
	"vpnview/internal/auth"
	"vpnview/internal/config"
	"vpnview/internal/handler"
	"vpnview/internal/port"
	"vpnview/internal/service"
	"vpnview/web"
)

// main 是程序入口函数。执行流程：
//  1. 解析命令行参数并加载 YAML 配置文件
//  2. 初始化存储层（SQLite）与 VPN 内核适配器
//  3. 构建认证、用户、流量、生命周期、订阅、DDNS 等服务
//  4. 启动流量轮询、生命周期管理、DDNS 等后台 goroutine
//  5. 创建 HTTP 路由并启动服务器
//  6. 监听系统信号（SIGINT/SIGTERM），收到后执行优雅关闭
func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	store, err := buildStore(cfg)
	if err != nil {
		slog.Error("failed to initialize store", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	if cfg.Adapter != nil && cfg.Adapter["subscription_domain"] == nil && cfg.Subscription.Domain != "" {
		cfg.Adapter["subscription_domain"] = cfg.Subscription.Domain
	}
	adapter, err := buildAdapter(cfg.Adapter)
	if err != nil {
		slog.Error("failed to initialize adapter", "err", err)
		os.Exit(1)
	}
	defer adapter.Close()
	slog.Info("adapter initialized", "capabilities", adapter.Capabilities().String())

	authSvc := auth.NewJWTService(cfg.Auth.Secret, cfg.GetTokenTTL())
	blocker := auth.NewIPBlocker() // 初始化安全拦截中心
	userSvc := service.NewUserService(store, adapter, cfg.Limits)

	// 🏆 启动自愈：将 SQLite 数据库中的所有启用用户同步到适配器/配置文件中
	slog.Info("synchronizing users from database to adapter...")
	dbUsers, err := store.List(context.Background())
	if err != nil {
		slog.Error("failed to load users from store for synchronization", "err", err)
	} else {
		syncCount := 0
		for _, u := range dbUsers {
			if u.Enabled {
				if err := adapter.AddUser(context.Background(), u.ID, u.Credentials); err != nil {
					slog.Error("failed to sync user to adapter", "id", u.ID, "err", err)
				} else {
					syncCount++
				}
			}
		}
		slog.Info("user synchronization completed", "synchronized_enabled_users", syncCount)
	}

	trafficSvc := service.NewTrafficService(store, adapter, cfg.GetPollInterval())
	userSvc.AttachTrafficService(trafficSvc)
	lifecycleSvc := service.NewLifecycleService(store, adapter, cfg.Limits, trafficSvc, cfg.GetPollInterval())
	subSvc := service.NewSubscriptionService(adapter, cfg.Subscription)
	ddnsSvc := buildDDNSService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go trafficSvc.Start(ctx)
	go lifecycleSvc.Start(ctx)
	if ddnsSvc != nil {
		go ddnsSvc.Start(ctx)
	}

	router := handler.NewRouter(authSvc, userSvc, trafficSvc, subSvc, adapter, cfg, web.FS, blocker)
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("starting vpnview", "listen", cfg.Server.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "err", err)
	}
}

// buildStore 根据配置文件初始化 SQLite 存储层引擎。
// 若数据库文件所在目录不存在，会自动创建。
func buildStore(cfg *config.Config) (port.UserStore, error) {
	if dir := filepath.Dir(cfg.Store.SQLite.Path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	return sqlite.New(cfg.Store.SQLite.Path)
}

// buildAdapter 根据配置实例化对应的 VPN 内核适配层组件。
func buildAdapter(raw map[string]any) (port.VPNAdapter, error) {
	adapterType := strings.ToLower(fmt.Sprint(raw["type"]))
	switch adapterType {
	case "", "stub":
		return stub.New(raw), nil
	case "singbox", "sing-box":
		return singbox.New(raw)
	default:
		return nil, fmt.Errorf("unknown adapter type %q", adapterType)
	}
}

// buildDDNSService 根据配置构建 DDNS 定时更新服务。
// 若配置中未指定 DDNS provider 或 provider 为空则返回 nil（不启用 DDNS）。
// 目前仅支持 Cloudflare 作为 DNS 提供商。
func buildDDNSService(cfg *config.Config) *service.DDNSService {
	if cfg.DDNS == nil || cfg.DDNS.Provider == "" {
		return nil
	}

	switch strings.ToLower(cfg.DDNS.Provider) {
	case "cloudflare":
		return service.NewDDNSService(cloudflare.New(cfg.DDNS), cfg.DDNS, cfg.GetDDNSCheckInterval())
	default:
		slog.Warn("unknown ddns provider; ddns disabled", "provider", cfg.DDNS.Provider)
		return nil
	}
}
