// ============================================================================
// 文件说明：internal/adapter/singbox/adapter.go
// 职责概览：Sing-box 后端网络代理内核适配器的主控类文件（Adapter）。
//           对外实现 port.VPNAdapter 与 port.ProfileProvider 接口。
//           内部通过多模块协作（ConfigManager 维护 JSON、ClashClient 连接 REST API、
//           GRPCTrafficReader 连接 V2Ray Stats API、SubscriptionBuilder 渲染配置），
//           实现高度模块化的业务调度。
//           通过在 Capabilities() 中根据各 API 的可联通性动态位或组合，
//           向上暴露系统能力集，保障了系统的高可用动态自适应降级。
// ============================================================================

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

// Adapter 是 Sing-box 后端的顶级适配层主控类，高度统筹子功能组件的调度运作。
type Adapter struct {
	cfg           Config               // 解析后的运行参数
	manager       *ConfigManager       // 配置文件 JSON 读写注入及自愈热重载管理器
	clash         *ClashClient         // 内置 Clash 兼容 REST API 客户端（获取全局速度、活跃连接、阻断连接）
	subscription  *SubscriptionBuilder // 多协议客户端节点配置订阅构建器
	trafficReader TrafficReader        // 流量计数器读取器接口，可以是 gRPC API 客户端或其它降级源

	reloadTimer *time.Timer // 延迟防抖热重载定时器
	reloadMu    sync.Mutex  // 并发热更控制锁
}

// TrafficReader 定义了本适配器内部用于抓取用户级别流量数据的标准接口。
type TrafficReader interface {
	QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) // 拉取流量快照
	Close() error                                                     // 安全断开数据通信
}

// New 根据原始参数字典创建、加载并初始化一个 Sing-box 顶级适配器实例。
// 启动逻辑：
//  - 转换并加载 Config 运行参数。
//  - 实例化 ConfigManager，自动核算冷启动，建立底座 users 并同步 stats 激活指标。
//  - 若启用了 ClashAPI，实例化 Clash API 专用客户端。
//  - 若启用了 V2RayAPI，直连初始化 gRPC 流量抓取读取驱动。
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
			return nil, fmt.Errorf("连接并初始化 V2Ray Stats gRPC 控制端失败: %w", err)
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

// Capabilities 🏆 核心动态降级分发器。
// 根据配置文件中各项接口的可连接状态与可用性，动态组合、生成位掩码。
//  - 账号管理生命周期（查询、添加、移除、禁用、启用）：Sing-box 原生支持通过 Manager 修改 JSON 配置，默认支持。
//  - 活跃连接与切断连接、全局速度：在 Clash 兼容接口激活时开启。
//  - 流量数据与网速度量：在 gRPC 统计接口直连成功时开启。
//  - 订阅链接下发：在 Subscription 域名配置有效时开启。
// 这极大地避免了后台接口崩溃导致前台页面整体卡死的尴尬。
func (a *Adapter) Capabilities() domain.Capability {
	// 本地 JSON 可写，即可支持基本的账号生命周期和前端动态表单
	caps := domain.CapListUsers | domain.CapAddUser | domain.CapRemoveUser |
		domain.CapDisableUser | domain.CapEnableUser | domain.CapCredentialDefs

	// 挂载 Clash 接口后激活大屏监控
	if a.clash != nil {
		caps |= domain.CapRealtimeSpeed | domain.CapActiveConns | domain.CapKillConn
	}
	// 挂载 gRPC 接口后激活高精度流量累计度量与速度限制支持
	if a.trafficReader != nil {
		caps |= domain.CapQueryTraffic | domain.CapUserSpeed
	}
	// 订阅功能
	if a.subscription.Enabled() {
		caps |= domain.CapSubscription
	}
	return caps
}

// Profile 实现 port.ProfileProvider 接口。
// 返回 Sing-box 结构化行为特征，允许主服务根据其 UserProvisionConfigPatch 和 TrafficModeCumulative 策略
// 自动对齐调度逻辑，解除代码死锁硬编码绑定。
func (a *Adapter) Profile() domain.AdapterProfile {
	reloadMode := domain.ReloadModeManual
	if a.cfg.ReloadCommand != "" {
		reloadMode = domain.ReloadModeCommand
	} else if a.clash != nil {
		reloadMode = domain.ReloadModeAPI
	}

	trafficMode := domain.TrafficModeUnsupported
	trafficScope := domain.TrafficScopeUnsupported
	if a.trafficReader != nil {
		trafficMode = domain.TrafficModeCumulative
		trafficScope = domain.TrafficScopeUser
	}

	return domain.AdapterProfile{
		Name:              "sing-box",
		UserProvisionMode: domain.UserProvisionConfigPatch,
		TrafficMode:       trafficMode,
		TrafficScope:      trafficScope,
		ReloadMode:        reloadMode,
		ConfigFormat:      domain.ConfigFormatJSON,
		Identity: domain.IdentityProfile{
			MetadataKeys:      []string{"inbound_user", "auth_user", "user", "name"},
			RouteMarkerPrefix: userRouteTagPrefix,
			StatsNameFormat:   "user>>>{user_id}>>>traffic>>>{direction}",
			AllowIPFallback:   true,
		},
	}
}

// CredentialFields 返回创建用户时所需的凭据字段定义列表。
// 支持 VLESS（含 Flow 选择）、Trojan、Shadowsocks（含加密算法选择）的动态表单结构描述。
func (a *Adapter) CredentialFields() []port.CredentialField {
	return []port.CredentialField{
		{
			Key:      "protocol",
			Label:    "网络协议类型",
			Type:     "select",
			Required: true,
			Default:  "vless",
			Options:  []string{"vless", "trojan", "shadowsocks", "hysteria2", "tuic"},
		},
		// --- VLESS / TUIC 共享 UUID 字段 ---
		{
			Key:          "uuid",
			Label:        "UUID 安全凭据",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "vless,tuic",
		},
		{
			Key:          "flow",
			Label:        "流控控制 (Flow)",
			Type:         "select",
			Options:      []string{"", "xtls-rprx-vision"},
			DependsOnKey: "protocol",
			DependsOnVal: "vless",
		},
		// --- Trojan / Hysteria 2 / TUIC 共享密码字段 ---
		{
			Key:          "password",
			Label:        "连接密码 (Password)",
			Type:         "text",
			Required:     true,
			AutoGenerate: true,
			DependsOnKey: "protocol",
			DependsOnVal: "trojan,hysteria2,tuic",
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
			Options:      []string{"256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"},
			DependsOnKey: "protocol",
			DependsOnVal: "shadowsocks",
		},
	}
}

// ListUsers 获取底层 JSON 配置文件中已登记的用户。
func (a *Adapter) ListUsers(ctx context.Context) ([]string, error) {
	return a.manager.ListUsers(ctx)
}

// AddUser 添加新用户元数据，自动同步写入 JSON 并执行热重载防抖更新。
func (a *Adapter) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	if err := a.manager.AddUser(ctx, userID, credentials); err != nil {
		return err
	}
	return a.reload(ctx)
}

// RemoveUser 从底层配置文件物理注销删除用户，并执行热重载。
func (a *Adapter) RemoveUser(ctx context.Context, userID string) error {
	if err := a.manager.RemoveUser(ctx, userID); err != nil {
		return err
	}
	return a.reload(ctx)
}

// DisableUser 禁用用户，当前在配置文件修改机制下，等同于从 inbound.users 中暂时物理剔除。
func (a *Adapter) DisableUser(ctx context.Context, userID string) error {
	return a.RemoveUser(ctx, userID)
}

// EnableUser 恢复启用用户，等同于将账号凭据重新同步注入 inbound.users。
func (a *Adapter) EnableUser(ctx context.Context, userID string, credentials map[string]string) error {
	return a.AddUser(ctx, userID, credentials)
}

// QueryTraffic 流量统计拉取入口。
// 优先使用 V2Ray Stats gRPC API 抓取精准流量；如果因特殊情况不支持，回退调用 Clash 活跃连接反查聚合算法。
func (a *Adapter) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	if a.trafficReader != nil {
		return a.trafficReader.QueryTraffic(ctx)
	}
	// 🏆 备选降级回退：从 Clash 存活长连接中聚合反查
	if a.clash != nil {
		return a.clash.QueryTrafficFromConnections(ctx)
	}
	return nil, domain.ErrNotSupported
}

// GetGlobalSpeed 呼叫 Clash 接口抓取全局速率。
func (a *Adapter) GetGlobalSpeed(ctx context.Context) (*port.GlobalSpeed, error) {
	if a.clash == nil {
		return nil, domain.ErrNotSupported
	}
	return a.clash.GetTraffic(ctx)
}

// GetActiveConnections 呼叫 Clash 接口拉取活动连接明细。
func (a *Adapter) GetActiveConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	if a.clash == nil {
		return nil, domain.ErrNotSupported
	}
	return a.clash.GetConnections(ctx)
}

// KillConnection 呼叫 Clash 接口切断指定连接。
func (a *Adapter) KillConnection(ctx context.Context, connID string) error {
	if a.clash == nil {
		return domain.ErrNotSupported
	}
	return a.clash.KillConnection(ctx, connID)
}

// SetUserSpeedLimit 速度硬度限额。由于 Sing-box 原生没有动态接口提供单用户级别的速度限制，返回 ErrNotSupported，由主程序生命周期自动执行软件 Strike 防御。
func (a *Adapter) SetUserSpeedLimit(ctx context.Context, userID string, uploadBytesPerSec, downloadBytesPerSec int64) error {
	return domain.ErrNotSupported
}

// SetGlobalSpeedLimit 全局速度限制。返回 ErrNotSupported。
func (a *Adapter) SetGlobalSpeedLimit(ctx context.Context, uploadBytesPerSec, downloadBytesPerSec int64) error {
	return domain.ErrNotSupported
}

// GenerateSubscription 调用内置订阅生成器，格式化用户凭证并 Base64 编码返回 plain/text 数据。
func (a *Adapter) GenerateSubscription(ctx context.Context, userID string, credentials map[string]string) ([]byte, string, error) {
	if !a.subscription.Enabled() {
		return nil, "", domain.ErrNotSupported
	}
	return a.subscription.Build(userID, credentials)
}

// Close 关闭并安全断开与底层代理内核的 gRPC 或 API 通信连接。
func (a *Adapter) Close() error {
	if a.trafficReader != nil {
		return a.trafficReader.Close()
	}
	return nil
}

// reload 核心的延迟与防抖热重载机制。
// 为了防止在面板上批量频繁添加、删除用户时，导致底层代理软件频繁进行热更断流，
// 重载动作会在最后一次配置变更后，自动开启 5 秒钟的延迟与防抖。
// 5 秒内若有新的配置写入，旧定时器自动关闭并重启，最大程度防止代理瞬间抖动。
func (a *Adapter) reload(ctx context.Context) error {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	if a.reloadTimer != nil {
		a.reloadTimer.Stop() // 防抖
	}

	a.reloadTimer = time.AfterFunc(5*time.Second, func() {
		bgCtx := context.Background()
		slog.Info("触发 5 秒定时防抖延迟热重载...")
		if err := a.executeReload(bgCtx); err != nil {
			slog.Error("Sing-box 配置文件热更失败", "err", err)
		} else {
			slog.Info("🎉 Sing-box 配置文件热重载更新成功！")
		}
	})

	slog.Info("已成功注册 5 秒定时防抖热更任务以保网络平稳")
	return nil
}

// executeReload 执行具体的配置文件更替和热更命令。
// 若无 reload 外部命令，且开启了 Clash API，则直接利用 Clash HTTP API 传入 path 触发无缝热更（无任何断流）。
func (a *Adapter) executeReload(ctx context.Context) error {
	if a.manager == nil {
		return fmt.Errorf("ConfigManager 未初始化")
	}
	if err := a.manager.Reload(ctx); err != nil {
		return err
	}
	// 如果没有系统 reload 命令，但开启了 Clash API，调用 HTTP API PUT 热加载配置文件，做到 0 断流秒级热更
	if a.cfg.ReloadCommand == "" && a.clash != nil && a.cfg.ConfigPath != "" {
		return a.clash.ReloadConfig(ctx, a.cfg.ConfigPath)
	}
	return nil
}
