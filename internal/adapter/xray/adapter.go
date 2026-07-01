// ============================================================================
// 文件说明：internal/adapter/xray/adapter.go
// 职责概览：Xray-core / V2Ray-core 后端网络代理内核适配器的主控类文件（Adapter）。
//           对外实现 port.VPNAdapter 与 port.ProfileProvider 接口。
//           内部通过 ConfigManager 以 config_patch 模式维护 JSON 配置的用户下发，
//           通过 GRPCTrafficReader 直连核心 Stats gRPC API 抓取用户级累计流量，
//           通过 SubscriptionBuilder 渲染多协议客户端订阅。
//           在 Capabilities() 中依据各 API 的可联通性动态位或组合，向上暴露能力集，
//           保障后端接口缺位时前端自动降级而非整体不可用。
//           Xray 是 V2Ray 的分叉，二者配置与统计接口同构，故共用本包，仅以 Variant 区分
//           gRPC 服务包路径（见 config.go）。init() 中一并注册两类核心及其常见别名。
// ============================================================================

package xray

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/adapter/registry"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

func init() {
	// Xray-core 变体及别名
	registry.Register("xray", newFactory("xray"))
	registry.Register("xray-core", newFactory("xray"))
	// V2Ray-core 变体及别名
	registry.Register("v2ray", newFactory("v2ray"))
	registry.Register("v2ray-core", newFactory("v2ray"))
	registry.Register("v2fly", newFactory("v2ray"))
}

// newFactory 返回一个绑定了固定核心变体的注册工厂，使同一份实现服务于 xray 与 v2ray 两类核心。
func newFactory(variant string) registry.Factory {
	return func(raw map[string]any) (port.VPNAdapter, error) {
		return New(raw, variant)
	}
}

// Adapter 是 Xray / V2Ray 后端的顶级适配层主控类，统筹子功能组件的调度运作。
type Adapter struct {
	cfg           Config
	manager       *ConfigManager       // 配置文件 JSON 读写注入与用户下发管理器
	subscription  *SubscriptionBuilder // 多协议客户端订阅构建器
	trafficReader TrafficReader        // 流量计数器读取器（gRPC Stats API），未配置时为 nil

	reloadTimer *time.Timer // 延迟防抖热重载定时器
	reloadMu    sync.Mutex  // 并发热更控制锁
}

// TrafficReader 定义本适配器内部抓取用户级流量数据的标准接口。
type TrafficReader interface {
	QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error)
	Close() error
}

// New 根据原始参数字典与核心变体创建、加载并初始化一个 Xray / V2Ray 顶级适配器实例。
//   - 转换并加载 Config 运行参数。
//   - 实例化 ConfigManager，冷启动建立底座 users 并激活 stats 指标。
//   - 若配置了 Stats gRPC API 地址，直连初始化流量抓取读取驱动。
func New(raw map[string]any, variant string) (*Adapter, error) {
	cfg := ParseConfig(raw, variant)

	manager, err := NewConfigManager(cfg)
	if err != nil {
		return nil, err
	}

	var trafficReader TrafficReader
	if cfg.APIAddress != "" {
		trafficReader, err = NewGRPCTrafficReader(cfg.APIAddress, cfg.StatsQueryMethod)
		if err != nil {
			return nil, fmt.Errorf("连接并初始化 %s Stats gRPC 控制端失败: %w", cfg.ProfileName(), err)
		}
	}

	return &Adapter{
		cfg:           cfg,
		manager:       manager,
		subscription:  NewSubscriptionBuilder(cfg),
		trafficReader: trafficReader,
	}, nil
}

// Capabilities 核心动态降级分发器，根据各接口可用性动态组合能力位掩码。
//   - 账号生命周期（查询、添加、移除、禁用、启用）与前端动态表单：config_patch 本地可写，默认支持。
//   - 用户级累计流量与用户测速：在 Stats gRPC 接口直连成功时开启。
//   - 订阅链接下发：在订阅域名配置有效时开启。
//
// 注意：Xray / V2Ray 原生不提供 Clash 式活跃连接管理与全局测速接口，故不虚报这些能力。
func (a *Adapter) Capabilities() domain.Capability {
	caps := domain.CapListUsers | domain.CapAddUser | domain.CapRemoveUser |
		domain.CapDisableUser | domain.CapEnableUser | domain.CapCredentialDefs

	if a.trafficReader != nil {
		caps |= domain.CapQueryTraffic | domain.CapUserSpeed
	}
	if a.subscription.Enabled() {
		caps |= domain.CapSubscription
	}
	return caps
}

// Profile 实现 port.ProfileProvider 接口，返回结构化行为特征，供主服务对齐调度逻辑。
func (a *Adapter) Profile() domain.AdapterProfile {
	reloadMode := domain.ReloadModeManual
	if a.cfg.ReloadCommand != "" {
		reloadMode = domain.ReloadModeCommand
	}

	trafficMode := domain.TrafficModeUnsupported
	trafficScope := domain.TrafficScopeUnsupported
	if a.trafficReader != nil {
		trafficMode = domain.TrafficModeCumulative
		trafficScope = domain.TrafficScopeUser
	}

	return domain.AdapterProfile{
		Name:              a.cfg.ProfileName(),
		UserProvisionMode: domain.UserProvisionConfigPatch,
		TrafficMode:       trafficMode,
		TrafficScope:      trafficScope,
		ReloadMode:        reloadMode,
		ConfigFormat:      domain.ConfigFormatJSON,
		Identity: domain.IdentityProfile{
			// Xray / V2Ray 以 email 字段承载用户标识，统计与日志均以此为锚点。
			MetadataKeys:    []string{"email", "user"},
			StatsNameFormat: "user>>>{user_id}>>>traffic>>>{direction}",
			AllowIPFallback: true,
		},
	}
}

// CredentialFields 返回创建用户时所需的凭据字段定义列表。
// 支持 VLESS（含 Flow）、VMess、Trojan、Shadowsocks（含加密算法）的动态表单结构描述。
func (a *Adapter) CredentialFields() []port.CredentialField {
	return []port.CredentialField{
		{
			Key:      "protocol",
			Label:    "网络协议类型",
			Type:     "select",
			Required: true,
			Default:  "vless",
			Options:  []string{"vless", "vmess", "trojan", "shadowsocks"},
		},
		// --- VLESS / VMess 共享 UUID 字段 ---
		{
			Key:          "uuid",
			Label:        "UUID 安全凭据",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "vless,vmess",
		},
		{
			Key:          "flow",
			Label:        "流控控制 (Flow)",
			Type:         "select",
			Options:      []string{"", "xtls-rprx-vision"},
			DependsOnKey: "protocol",
			DependsOnVal: "vless",
		},
		// --- Trojan 专属密码字段 ---
		{
			Key:          "password",
			Label:        "连接密码 (Password)",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "trojan",
		},
		// --- Shadowsocks 专属加密字段 ---
		{
			Key:          "ss_password",
			Label:        "加密密钥 (SS Password)",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "shadowsocks",
		},
		{
			Key:          "ss_method",
			Label:        "加密算法 (SS Method)",
			Type:         "select",
			Default:      "256-gcm",
			Options:      []string{"256-gcm", "128-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"},
			DependsOnKey: "protocol",
			DependsOnVal: "shadowsocks",
		},
	}
}

// ListUsers 获取底层 JSON 配置文件中已登记的用户。
func (a *Adapter) ListUsers(ctx context.Context) ([]string, error) {
	return a.manager.ListUsers(ctx)
}

// AddUser 添加新用户，写入 JSON 并执行防抖热重载。
func (a *Adapter) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	if err := a.manager.AddUser(ctx, userID, credentials); err != nil {
		return err
	}
	return a.reload(ctx)
}

// RemoveUser 从底层配置文件注销用户，并执行热重载。
func (a *Adapter) RemoveUser(ctx context.Context, userID string) error {
	if err := a.manager.RemoveUser(ctx, userID); err != nil {
		return err
	}
	return a.reload(ctx)
}

// DisableUser 禁用用户，在 config_patch 机制下等同于从 inbound 用户表中暂时剔除。
func (a *Adapter) DisableUser(ctx context.Context, userID string) error {
	return a.RemoveUser(ctx, userID)
}

// EnableUser 恢复启用用户，等同于将凭据重新注入 inbound 用户表。
func (a *Adapter) EnableUser(ctx context.Context, userID string, credentials map[string]string) error {
	return a.AddUser(ctx, userID, credentials)
}

// QueryTraffic 通过 Stats gRPC API 抓取精准用户级累计流量；未配置该 API 时返回 ErrNotSupported。
func (a *Adapter) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	if a.trafficReader != nil {
		return a.trafficReader.QueryTraffic(ctx)
	}
	return nil, domain.ErrNotSupported
}

// SetUserSpeedLimit 单用户原生限速。Xray / V2Ray 无动态单用户限速接口，返回 ErrNotSupported，
// 由主程序生命周期用软件 Strike 兜底。
func (a *Adapter) SetUserSpeedLimit(ctx context.Context, userID string, uploadBytesPerSec, downloadBytesPerSec int64) error {
	return domain.ErrNotSupported
}

// SetGlobalSpeedLimit 全局原生限速。返回 ErrNotSupported。
func (a *Adapter) SetGlobalSpeedLimit(ctx context.Context, uploadBytesPerSec, downloadBytesPerSec int64) error {
	return domain.ErrNotSupported
}

// GenerateSubscription 调用内置订阅生成器，格式化用户凭据并 Base64 编码返回。
func (a *Adapter) GenerateSubscription(ctx context.Context, userID string, credentials map[string]string) ([]byte, string, error) {
	if !a.subscription.Enabled() {
		return nil, "", domain.ErrNotSupported
	}
	return a.subscription.Build(userID, credentials)
}

// Close 关闭并安全断开与底层核心的 gRPC 通信连接。
func (a *Adapter) Close() error {
	if a.trafficReader != nil {
		return a.trafficReader.Close()
	}
	return nil
}

// reload 延迟防抖热重载机制。config_patch 模式下配置已同步写盘，此处仅负责触发核心重载生效。
//   - 未配置 reload_command 时：无自动重载手段，仅告警提示需手动重载，避免遗留空转定时器。
//   - 配置了 reload_command 时：5 秒防抖后执行，批量增删用户期间避免核心频繁重启断流。
func (a *Adapter) reload(ctx context.Context) error {
	if a.cfg.ReloadCommand == "" {
		slog.Warn("Xray/V2Ray 未配置 reload_command，配置已写盘但需手动重载核心方可生效")
		return nil
	}

	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	if a.reloadTimer != nil {
		a.reloadTimer.Stop()
	}
	a.reloadTimer = time.AfterFunc(5*time.Second, func() {
		bgCtx := context.Background()
		slog.Info("触发 5 秒定时防抖延迟热重载 Xray/V2Ray...")
		if err := a.manager.Reload(bgCtx); err != nil {
			slog.Error("Xray/V2Ray 配置文件热更失败", "err", err)
		} else {
			slog.Info("🎉 Xray/V2Ray 配置文件热重载更新成功！")
		}
	})
	slog.Info("已注册 5 秒定时防抖热更任务以保网络平稳")
	return nil
}
