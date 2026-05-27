// Package singbox 实现了 sing-box 代理后端的适配器。
// 通过读写 sing-box JSON 配置文件、Clash RESTful API 以及 V2Ray Stats gRPC API，
// 提供用户管理、流量查询、连接管理和订阅生成等功能。
//
// Adapter for sing-box proxy backend.
package singbox

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// Adapter 是 sing-box 后端的适配器实现，封装了配置管理、Clash API 客户端、
// 流量统计读取和订阅链接生成等子组件，对外实现 port.BackendAdapter 接口。
type Adapter struct {
	cfg           Config
	manager       *ConfigManager
	clash         *ClashClient
	subscription  *SubscriptionBuilder
	trafficReader TrafficReader

	reloadTimer *time.Timer
	reloadMu    sync.Mutex
}

// TrafficReader 定义了流量统计数据的读取接口。
// 具体实现可以是 V2Ray Stats gRPC 客户端或其他流量数据源。
type TrafficReader interface {
	// QueryTraffic 查询并返回各用户的流量快照。
	QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error)
	// Close 释放底层资源（如 gRPC 连接）。
	Close() error
}

// New 根据原始配置字典创建并初始化一个 Adapter 实例。
// 会根据配置自动初始化 ConfigManager、ClashClient 和 GRPCTrafficReader 等子组件。
func New(raw map[string]any) (*Adapter, error) {
	cfg := ParseConfig(raw)
	manager, err := NewConfigManager(cfg)
	if err != nil {
		return nil, err
	}

	var clash *ClashClient
	if cfg.ClashAPI != "" {
		clash = NewClashClient(cfg.ClashAPI, cfg.ClashSecret)
	}

	var trafficReader TrafficReader
	if cfg.V2RayAPI != "" {
		trafficReader, err = NewGRPCTrafficReader(cfg.V2RayAPI)
		if err != nil {
			return nil, fmt.Errorf("initialize v2ray stats client: %w", err)
		}
	}

	a := &Adapter{
		cfg:           cfg,
		manager:       manager,
		clash:         clash,
		subscription:  NewSubscriptionBuilder(cfg),
		trafficReader: trafficReader,
	}
	return a, nil
}

// Capabilities 返回当前适配器支持的功能位集合。
// 根据 Clash API、V2Ray Stats API 和订阅功能的可用性动态组合。
func (a *Adapter) Capabilities() domain.Capability {
	caps := domain.CapListUsers | domain.CapAddUser | domain.CapRemoveUser |
		domain.CapDisableUser | domain.CapEnableUser | domain.CapCredentialDefs

	if a.clash != nil {
		caps |= domain.CapRealtimeSpeed | domain.CapActiveConns | domain.CapKillConn
	}
	if a.trafficReader != nil {
		caps |= domain.CapQueryTraffic | domain.CapUserSpeed
	}
	if a.subscription.Enabled() {
		caps |= domain.CapSubscription
	}
	return caps
}

// CredentialFields 返回创建用户时所需的凭证字段定义列表。
// 包含协议选择（vless/trojan/shadowsocks）及各协议专属字段，前端据此动态渲染表单。
func (a *Adapter) CredentialFields() []port.CredentialField {
	return []port.CredentialField{
		{
			Key:      "protocol",
			Label:    "协议类型",
			Type:     "select",
			Required: true,
			Default:  "vless",
			Options:  []string{"vless", "trojan", "shadowsocks", "hysteria2", "tuic"},
		},
		// --- VLESS / TUIC 共享 UUID 字段 ---
		{
			Key:          "uuid",
			Label:        "UUID",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "vless,tuic",
		},
		{
			Key:          "flow",
			Label:        "流控 (Flow)",
			Type:         "select",
			Options:      []string{"", "xtls-rprx-vision"},
			DependsOnKey: "protocol",
			DependsOnVal: "vless",
		},
		// --- Trojan / Hysteria 2 / TUIC 共享密码字段 ---
		{
			Key:          "password",
			Label:        "密码 (Password)",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "trojan,hysteria2,tuic",
		},
		// --- Shadowsocks 专属字段 ---
		{
			Key:          "ss_password",
			Label:        "加密密码 (Password)",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "shadowsocks",
		},
		{
			Key:          "ss_method",
			Label:        "加密方法 (Method)",
			Type:         "select",
			Default:      "256-gcm",
			Options:      []string{"256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"},
			DependsOnKey: "protocol",
			DependsOnVal: "shadowsocks",
		},
	}
}

// ListUsers 返回当前配置中所有已注册用户的 ID 列表。
func (a *Adapter) ListUsers(ctx context.Context) ([]string, error) {
	return a.manager.ListUsers(ctx)
}

// AddUser 向 sing-box 配置中添加一个新用户，写入配置文件后自动触发热重载。
func (a *Adapter) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	if err := a.manager.AddUser(ctx, userID, credentials); err != nil {
		return err
	}
	return a.reload(ctx)
}

// RemoveUser 从 sing-box 配置中移除指定用户，写入配置文件后自动触发热重载。
func (a *Adapter) RemoveUser(ctx context.Context, userID string) error {
	if err := a.manager.RemoveUser(ctx, userID); err != nil {
		return err
	}
	return a.reload(ctx)
}

// DisableUser 禁用指定用户。当前实现等同于 RemoveUser，直接从配置中移除该用户。
func (a *Adapter) DisableUser(ctx context.Context, userID string) error {
	return a.RemoveUser(ctx, userID)
}

// EnableUser 启用指定用户。当前实现等同于 AddUser，将用户重新添加到配置中。
func (a *Adapter) EnableUser(ctx context.Context, userID string, credentials map[string]string) error {
	return a.AddUser(ctx, userID, credentials)
}

// QueryTraffic 查询各用户的流量统计数据。
// 优先使用 V2Ray Stats gRPC API（精确增量统计），不可用时降级为 Clash API 聚合方案。
func (a *Adapter) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	// 优先使用 V2Ray Stats gRPC API（精确用户级累计统计）
	if a.trafficReader != nil {
		return a.trafficReader.QueryTraffic(ctx)
	}
	return nil, domain.ErrNotSupported
}

// GetGlobalSpeed 获取全局实时上传/下载速率（字节/秒），通过 Clash API 实现。
func (a *Adapter) GetGlobalSpeed(ctx context.Context) (*port.GlobalSpeed, error) {
	if a.clash == nil {
		return nil, domain.ErrNotSupported
	}
	return a.clash.GetTraffic(ctx)
}

// GetActiveConnections 获取当前所有活跃连接列表，通过 Clash API 实现。
func (a *Adapter) GetActiveConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	if a.clash == nil {
		return nil, domain.ErrNotSupported
	}
	return a.clash.GetConnections(ctx)
}

// KillConnection 强制断开指定 ID 的活跃连接，通过 Clash API 实现。
func (a *Adapter) KillConnection(ctx context.Context, connID string) error {
	if a.clash == nil {
		return domain.ErrNotSupported
	}
	return a.clash.KillConnection(ctx, connID)
}

// SetUserSpeedLimit 设置指定用户的速率限制。sing-box 原生不支持此功能，始终返回 ErrNotSupported。
func (a *Adapter) SetUserSpeedLimit(ctx context.Context, userID string, uploadBytesPerSec, downloadBytesPerSec int64) error {
	return domain.ErrNotSupported
}

// SetGlobalSpeedLimit 设置全局速率限制。sing-box 原生不支持此功能，始终返回 ErrNotSupported。
func (a *Adapter) SetGlobalSpeedLimit(ctx context.Context, uploadBytesPerSec, downloadBytesPerSec int64) error {
	return domain.ErrNotSupported
}

// GenerateSubscription 为指定用户生成订阅链接内容。
// 返回值依次为：订阅内容（字节）、MIME 类型、错误。
func (a *Adapter) GenerateSubscription(ctx context.Context, userID string, credentials map[string]string) ([]byte, string, error) {
	if !a.subscription.Enabled() {
		return nil, "", domain.ErrNotSupported
	}
	return a.subscription.Build(userID, credentials)
}

// Close 释放适配器持有的资源，如关闭 gRPC 连接。
func (a *Adapter) Close() error {
	if a.trafficReader != nil {
		return a.trafficReader.Close()
	}
	return nil
}

// reload 在配置变更后触发 sing-box 热重载（使用5秒延迟与防抖防断流机制）。
func (a *Adapter) reload(ctx context.Context) error {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	if a.reloadTimer != nil {
		a.reloadTimer.Stop()
	}

	a.reloadTimer = time.AfterFunc(5*time.Second, func() {
		bgCtx := context.Background()
		slog.Info("executing delayed vpn reload...")
		if err := a.executeReload(bgCtx); err != nil {
			slog.Error("failed to reload vpn configuration", "err", err)
		} else {
			slog.Info("vpn configuration reloaded successfully")
		}
	})

	slog.Info("scheduled vpn reload in 5 seconds to prevent immediate connection cut")
	return nil
}

func (a *Adapter) executeReload(ctx context.Context) error {
	if a.manager == nil {
		return fmt.Errorf("config manager is nil")
	}
	if err := a.manager.Reload(ctx); err != nil {
		return err
	}
	if a.cfg.ReloadCommand == "" && a.clash != nil && a.cfg.ConfigPath != "" {
		return a.clash.ReloadConfig(ctx, a.cfg.ConfigPath)
	}
	return nil
}
