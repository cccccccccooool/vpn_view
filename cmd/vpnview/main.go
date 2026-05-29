// ============================================================================
// 文件说明：cmd/vpnview/main.go
// 职责概览：VPNView 管理面板的主程序入口。负责读取并解析 YAML 配置文件、初始化各个
//           分层组件（SQLite 存储适配器、VPN 内核适配器、DDNS 适配器、业务 Service、
//           API Router 等）、启动后台守护进程（流量统计、生命周期自愈、DDNS 定时更新），
//           并启动安全监听 HTTP 服务，实现优雅停机。
// ============================================================================

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

// main 是系统的主入口函数。
// 运行流程：
//  1. 解析命令行参数 `--config`（默认加载同级目录下的 `config.yaml`）
//  2. 加载并解析系统的 YAML 全局配置
//  3. 初始化底层数据存储层（使用 SQLite）
//  4. 初始化底层 VPN 内核适配层（如 Singbox 客户端适配器或 Stub 测试桩适配器）
//  5. 初始化认证模块、拦截中心与业务服务层（UserService, TrafficService, LifecycleService...）
//  6. 执行【启动自愈同步】：从 SQLite 数据库中加载所有启用状态的用户，并自动全量注册/同步到 VPN 适配器中，防止两端状态不一致
//  7. 启动三个核心后台协程：流量轮询监控、生命周期巡检、DDNS 定时更新
//  8. 装配 HTTP 路由并开启 HTTP 服务监听
//  9. 监听系统 SIGINT/SIGTERM 信号，接收到后安全终止协程并优雅关闭 HTTP 服务，释放全部数据库与适配器资源
func main() {
	// 定义并解析配置文件路径命令行参数
	configPath := flag.String("config", "config.yaml", "配置文件 YAML 路径")
	flag.Parse()

	// 1. 加载系统全局配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("全局配置文件加载失败", "err", err)
		os.Exit(1)
	}

	// 2. 初始化持久化存储引擎 (SQLite)
	store, err := buildStore(cfg)
	if err != nil {
		slog.Error("持久化存储层 SQLite 初始化失败", "err", err)
		os.Exit(1)
	}
	defer store.Close() // 延迟关闭存储引擎，保障数据刷盘

	// 若配置中没有显式设置订阅域名，但定义了全局订阅域名，则自动对齐
	if cfg.Adapter != nil && cfg.Adapter["subscription_domain"] == nil && cfg.Subscription.Domain != "" {
		cfg.Adapter["subscription_domain"] = cfg.Subscription.Domain
	}

	// 3. 构建并初始化底层 VPN 内核适配器
	adapter, err := buildAdapter(cfg.Adapter)
	if err != nil {
		slog.Error("VPN 适配器初始化失败", "err", err)
		os.Exit(1)
	}
	defer adapter.Close() // 延迟释放适配器持有连接与资源
	slog.Info("VPN 适配器初始化成功", "capabilities", adapter.Capabilities().String())

	// 4. 实例化认证服务与安全拦截中心
	authSvc := auth.NewJWTService(cfg.Auth.Secret, cfg.GetTokenTTL())
	blocker := auth.NewIPBlocker() // 初始化 IP 防暴力破解拦截中心
	userSvc := service.NewUserService(store, adapter, cfg.Limits)

	// 🏆 核心逻辑：启动自愈同步机制
	// 读取 SQLite 数据库中所有标记为 Enabled 的用户，并调用 adapter.AddUser 重新写入适配器中
	// 这避免了因代理软件重启、配置丢失导致的用户连接失效
	slog.Info("正在启动自愈同步：将数据库用户同步至 VPN 适配器配置...")
	dbUsers, err := store.List(context.Background())
	if err != nil {
		slog.Error("从数据库读取用户同步数据失败", "err", err)
	} else {
		syncCount := 0
		for _, u := range dbUsers {
			if u.Enabled {
				// 将启用用户的凭据与配置加载回 VPN 内核
				if err := adapter.AddUser(context.Background(), u.ID, u.Credentials); err != nil {
					slog.Error("同步用户至 VPN 适配器失败", "id", u.ID, "err", err)
				} else {
					syncCount++
				}
			}
		}
		slog.Info("自愈同步完成", "已恢复的有效用户数", syncCount)
	}

	// 5. 实例化各个关键业务服务
	trafficSvc := service.NewTrafficService(store, adapter, cfg.GetPollInterval())
	userSvc.AttachTrafficService(trafficSvc) // 挂载流量统计，支持速度和流量联动
	lifecycleSvc := service.NewLifecycleService(store, adapter, cfg.Limits, trafficSvc, cfg.GetPollInterval())
	subSvc := service.NewSubscriptionService(adapter, cfg.Subscription)
	ddnsSvc := buildDDNSService(cfg)

	// 定义上下文用于控制后台守护协程的生命周期
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 6. 开启后台并发守护任务
	go trafficSvc.Start(ctx)   // 启动流量定时轮询和瞬时网速计算
	go lifecycleSvc.Start(ctx) // 启动账号到期或超额自动停机监控
	if ddnsSvc != nil {
		go ddnsSvc.Start(ctx)  // 若启用，启动动态公网 IP 解析定时同步
	}

	// 7. 装配 HTTP 服务路由并配置安全参数
	router := handler.NewRouter(authSvc, userSvc, trafficSvc, subSvc, adapter, cfg, web.FS, blocker)
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second, // 设置读取请求头的超时时间，防御 Slowloris 攻击
	}

	// 8. 并发运行 HTTP 服务
	go func() {
		slog.Info("管理面板 HTTP 服务已启动", "监听地址", cfg.Server.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP 服务启动异常退出", "err", err)
			os.Exit(1)
		}
	}()

	// 9. 监听系统优雅停机信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM) // 监听键盘中断和系统终止信号
	<-quit

	slog.Info("正在执行优雅停机，回收系统资源...")
	cancel() // 广播通知，停止所有的后台守护协程

	// 给 HTTP 服务 5 秒的缓冲处理未完成的请求
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP 服务安全关闭失败", "err", err)
	}
	slog.Info("系统服务已安全退出。")
}

// buildStore 根据全局配置的 SQLite 路径参数，创建并实例化 SQLite3 存储适配层。
// 会自动创建数据库文件所在的父级级联目录。
func buildStore(cfg *config.Config) (port.UserStore, error) {
	if dir := filepath.Dir(cfg.Store.SQLite.Path); dir != "." && dir != "" {
		// 级联创建数据库父目录
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	return sqlite.New(cfg.Store.SQLite.Path)
}

// buildAdapter 根据配置字典中的类型，实例化具体的底层 VPN 核心适配器（Stub 或 Sing-box）。
func buildAdapter(raw map[string]any) (port.VPNAdapter, error) {
	adapterType := strings.ToLower(fmt.Sprint(raw["type"]))
	switch adapterType {
	case "", "stub":
		// 返回基于内存的测试桩适配器（便于无 VPN 环境调试）
		return stub.New(raw), nil
	case "singbox", "sing-box":
		// 返回真实的 Sing-box 网络代理适配器
		return singbox.New(raw)
	default:
		return nil, fmt.Errorf("未知的 VPN 适配器类型 %q", adapterType)
	}
}

// buildDDNSService 解析动态 DNS (DDNS) 参数，构建定时域名解析服务。
// 目前仅实现了基于 Cloudflare 的 DNS 更新器。
func buildDDNSService(cfg *config.Config) *service.DDNSService {
	if cfg.DDNS == nil || cfg.DDNS.Provider == "" {
		return nil // 未启用 DDNS
	}

	switch strings.ToLower(cfg.DDNS.Provider) {
	case "cloudflare":
		return service.NewDDNSService(cloudflare.New(cfg.DDNS), cfg.DDNS, cfg.GetDDNSCheckInterval())
	default:
		slog.Warn("未支持的 DDNS 服务商，DDNS 更新已关闭", "provider", cfg.DDNS.Provider)
		return nil
	}
}
