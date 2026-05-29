// ============================================================================
// 文件说明：internal/port/vpnadapter.go
// 职责概览：定义了 VPNView 与底层 VPN 代理客户端（如 Sing-box、Xray）交互的统一公共
//           核心端口接口（port.VPNAdapter）。规定了所有适配器必须实现的固定方法，包括
//           用户增删改查生命周期、流量累计数据查询、实时网速查询、活跃连接管理和订阅生成，
//           提供了系统底层的强一致性约定，保证主程序能完美适配任何具体代理后端。
// ============================================================================

package port

import (
	"context"
	"time"

	"vpnview/internal/domain"
)

// CredentialField 描述创建或修改用户凭据时，某个字段在前端动态渲染和输入的属性。
// 避免了前端写死不同 VPN 协议所需的表单字段（如 VLESS 需要 UUID，Trojan 需要 Password 等）。
type CredentialField struct {
	Key          string   `json:"key"`                      // 字段在凭据 map 中的 Key 标识
	Label        string   `json:"label"`                    // 前端显示的中文标签/字段名称
	Type         string   `json:"type"`                     // 字段渲染输入框类型（"text" 文本, "password" 密码框, "select" 下拉单选）
	Required     bool     `json:"required"`                 // 是否必填项
	Default      string   `json:"default,omitempty"`        // 默认填充值
	Options      []string `json:"options,omitempty"`        // 仅在 select 类型下生效的备选选项值列表
	AutoGenerate bool     `json:"auto_generate"`            // 是否允许一键自动生成（如自动生成 UUID）
	DependsOnKey string   `json:"depends_on_key,omitempty"` // 级联依赖条件字段的 Key（用于前端逻辑）
	DependsOnVal string   `json:"depends_on_val,omitempty"` // 级联依赖条件字段满足时的特定匹配值，只有当 DependsOnKey 字段的值等于 DependsOnVal 时此字段才显示
}

// TrafficSnapshot 表示某一瞬时时刻，单个用户的总流量快照信息。
type TrafficSnapshot struct {
	UserID   string `json:"user_id"`  // 用户唯一 ID 标识
	Upload   int64  `json:"upload"`   // 当前时刻累计的上传流量（字节数）
	Download int64  `json:"download"` // 当前时刻累计的下载流量（字节数）
}

// GlobalSpeed 表示整个 VPN 后端在当前时刻的全局实时上传、下载速率（速度包）。
type GlobalSpeed struct {
	Up   int64 `json:"up"`   // 当前实时上传速度（字节/秒）
	Down int64 `json:"down"` // 当前实时下载速度（字节/秒）
}

// ActiveConnection 描述当前正在运行的单个活跃的网络 TCP/UDP 连接详情。
type ActiveConnection struct {
	ID          string    `json:"id"`                // 连接唯一生成的 UUID 或序列标识
	UserID      string    `json:"user_id,omitempty"` // 关联的 VPN 用户 ID（如果无法解析或非认证连接，则为空）
	Upload      int64     `json:"upload"`            // 该活跃连接目前已发送的上传流量（字节数）
	Download    int64     `json:"download"`          // 该活跃连接目前已接收的下载流量（字节数）
	Start       time.Time `json:"start"`             // 连接建立的准确时间戳
	Network     string    `json:"network"`           // 连接网络协议类型（如 "tcp", "udp"）
	Source      string    `json:"source"`            // 连接发起方的 IP 和源端口
	Destination string    `json:"destination"`       // 连接的目标请求服务器 IP 和目的端口
}

// ProfileProvider 是一个可选的高级接口，当底层适配器除了返回位掩码之外，还能
// 结构化描述其内部更高级的特性分布（如配置加载模式、流量计算粒度、认证配置属性等）时实现该接口。
type ProfileProvider interface {
	// Profile 返回该适配器的结构化特征行为描述配置。
	Profile() domain.AdapterProfile
}

// VPNAdapter 定义主程序不可降级的最小 VPN 模块接口。
// 只有账号同步和基础能力声明保留在核心接口；流量、连接、订阅、限速等功能通过下方可选接口按能力启用。
type VPNAdapter interface {
	// Capabilities 返回当前适配器所支持的能力位掩码集（如是否支持速度限制、活跃连接监控等）。
	Capabilities() domain.Capability

	// CredentialFields 返回该适配器所期望的凭据定义表，供前端动态创建表单界面。
	CredentialFields() []CredentialField

	// ListUsers 查询并获取底层代理后端中已经注册并存在的全部用户 ID 列表。
	ListUsers(ctx context.Context) ([]string, error)

	// AddUser 向底层代理添加一个用户账户，并传递该用户所需的具体协议凭据（如密码、流控协议等）。
	AddUser(ctx context.Context, userID string, credentials map[string]string) error

	// RemoveUser 从底层代理彻底移除某用户，防止其连接，并清理对应的全部配置资源。
	RemoveUser(ctx context.Context, userID string) error

	// Close 安全关闭与底层网络代理的数据通信或 API 长连接，归还系统资源。
	Close() error
}

// UserStateManager 表示适配器支持保留用户主体的启停控制。
type UserStateManager interface {
	// DisableUser 禁用指定用户账户，使其无法建立新的连接，但在配置中依旧保留用户主体。
	DisableUser(ctx context.Context, userID string) error

	// EnableUser 重新启用此前被禁用的用户，通常在用户续费重置时调用，并同步传入其最新的连接凭据。
	EnableUser(ctx context.Context, userID string, credentials map[string]string) error
}

// TrafficProvider 表示适配器能够提供用户级累计流量快照。
type TrafficProvider interface {
	// QueryTraffic 核心轮询接口。查询所有用户的累计流量快照。主程序通过定时器前后两次调用的差值算得实时速度。
	QueryTraffic(ctx context.Context) ([]TrafficSnapshot, error)
}

// GlobalSpeedProvider 表示适配器能够提供底层原生全局实时速度。
type GlobalSpeedProvider interface {
	// GetGlobalSpeed 获取整个代理服务器的全局瞬时实时吞吐上传与下载速度。
	GetGlobalSpeed(ctx context.Context) (*GlobalSpeed, error)
}

// ConnectionProvider 表示适配器能够列出并切断底层活跃连接。
type ConnectionProvider interface {
	// GetActiveConnections 抓取当前所有维持中的活跃连接记录列表。
	GetActiveConnections(ctx context.Context) ([]ActiveConnection, error)

	// KillConnection 强制切断某一个具体 ID 的活动网络连接。
	KillConnection(ctx context.Context, connID string) error
}

// SpeedLimiter 表示适配器支持单用户原生限速。
type SpeedLimiter interface {
	// SetUserSpeedLimit 对单个 VPN 用户实施最大上传与下载的网速上限控制（字节/秒）。如果不被底层支持，返回 ErrNotSupported。
	SetUserSpeedLimit(ctx context.Context, userID string, uploadBytesPerSec, downloadBytesPerSec int64) error
}

// GlobalSpeedLimiter 表示适配器支持全局原生限速。
type GlobalSpeedLimiter interface {
	// SetGlobalSpeedLimit 对代理服务器整机实施最大全局上传和下载吞吐网速控制（字节/秒）。如果不被支持，返回 ErrNotSupported。
	SetGlobalSpeedLimit(ctx context.Context, uploadBytesPerSec, downloadBytesPerSec int64) error
}

// SubscriptionProvider 表示适配器能够生成用户客户端订阅配置。
type SubscriptionProvider interface {
	// GenerateSubscription 为用户量身定制地生成其可直接接入的客户端订阅链接配置文件文本内容及 MIME-Type 类型。
	GenerateSubscription(ctx context.Context, userID string, credentials map[string]string) ([]byte, string, error)
}
