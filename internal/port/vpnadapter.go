// Package port 定义了 VPNView 的端口接口（ports），遵循六边形架构。
// 包含 VPN 适配器、数据存储和 DDNS 等外部依赖的抽象接口。
package port

import (
	"context"
	"time"

	"vpnview/internal/domain"
)

// CredentialField 描述用户凭据的一个字段定义，用于前端动态渲染表单。
type CredentialField struct {
	Key          string   `json:"key"`                      // 字段标识键
	Label        string   `json:"label"`                    // 显示标签
	Type         string   `json:"type"`                     // 字段类型（如 "text", "password", "select"）
	Required     bool     `json:"required"`                 // 是否必填
	Default      string   `json:"default,omitempty"`        // 默认值
	Options      []string `json:"options,omitempty"`        // 可选值列表（仅 select 类型使用）
	AutoGenerate bool     `json:"auto_generate"`            // 是否支持自动生成（如 UUID）
	DependsOnKey string   `json:"depends_on_key,omitempty"` // 条件依赖的字段键名
	DependsOnVal string   `json:"depends_on_val,omitempty"` // 条件依赖的字段值
}

// TrafficSnapshot 表示某一时刻单个用户的流量快照。
type TrafficSnapshot struct {
	UserID   string `json:"user_id"`  // 用户 ID
	Upload   int64  `json:"upload"`   // 上传流量（字节）
	Download int64  `json:"download"` // 下载流量（字节）
}

// GlobalSpeed 表示全局实时上下行速度。
type GlobalSpeed struct {
	Up   int64 `json:"up"`   // 上传速度（字节/秒）
	Down int64 `json:"down"` // 下载速度（字节/秒）
}

// ActiveConnection 表示一条正在进行的活跃连接详情。
type ActiveConnection struct {
	ID          string    `json:"id"`                // 连接唯一标识
	UserID      string    `json:"user_id,omitempty"` // 关联用户 ID（可能为空）
	Upload      int64     `json:"upload"`            // 该连接已上传字节数
	Download    int64     `json:"download"`          // 该连接已下载字节数
	Start       time.Time `json:"start"`             // 连接建立时间
	Network     string    `json:"network"`           // 网络协议（如 "tcp", "udp"）
	Source      string    `json:"source"`            // 来源地址
	Destination string    `json:"destination"`       // 目标地址
}

// VPNAdapter 定义了与底层 VPN 代理（如 Xray、Sing-box）交互的统一接口。
// 不同的 VPN 后端通过实现此接口来接入系统，适配器可通过 Capabilities 声明自身支持的功能子集。
type VPNAdapter interface {
	// Capabilities 返回该适配器支持的能力位掩码。
	Capabilities() domain.Capability
	// CredentialFields 返回创建用户时所需的凭据字段定义列表。
	CredentialFields() []CredentialField

	// ListUsers 列出适配器中所有已注册的用户 ID。
	ListUsers(ctx context.Context) ([]string, error)
	// AddUser 向适配器添加一个用户及其凭据。
	AddUser(ctx context.Context, userID string, credentials map[string]string) error
	// RemoveUser 从适配器中移除指定用户。
	RemoveUser(ctx context.Context, userID string) error
	// DisableUser 禁用指定用户，使其无法连接但保留配置。
	DisableUser(ctx context.Context, userID string) error
	// EnableUser 重新启用被禁用的用户，需要提供凭据以恢复配置。
	EnableUser(ctx context.Context, userID string, credentials map[string]string) error

	// QueryTraffic 查询所有用户的累计流量快照，由主程序计算相邻快照差值。
	QueryTraffic(ctx context.Context) ([]TrafficSnapshot, error)
	// GetGlobalSpeed 获取全局实时上下行速度。
	GetGlobalSpeed(ctx context.Context) (*GlobalSpeed, error)
	// GetActiveConnections 获取当前所有活跃连接列表。
	GetActiveConnections(ctx context.Context) ([]ActiveConnection, error)
	// KillConnection 断开指定 ID 的活跃连接。
	KillConnection(ctx context.Context, connID string) error

	// SetUserSpeedLimit 设置指定用户的上传和下载限速（字节/秒）。
	SetUserSpeedLimit(ctx context.Context, userID string, uploadBytesPerSec, downloadBytesPerSec int64) error
	// SetGlobalSpeedLimit 设置全局上传和下载限速（字节/秒）。
	SetGlobalSpeedLimit(ctx context.Context, uploadBytesPerSec, downloadBytesPerSec int64) error

	// GenerateSubscription 为指定用户生成订阅内容，返回订阅数据、MIME 类型和可能的错误。
	GenerateSubscription(ctx context.Context, userID string, credentials map[string]string) ([]byte, string, error)

	// Close 关闭适配器连接并释放资源。
	Close() error
}
