// ============================================================================
// 文件说明：internal/domain/user.go
// 职责概览：定义了 VPNView 的核心领域模型——User（用户）结构体。
//           User 描述了 VPN 用户的完整身份、凭据字典、累计消耗流量、限速规则、
//           配额及到期策略，是整个系统的核心数据和业务规则载体。
// ============================================================================

package domain

import "time"

// User 表示一个 VPN 用户在系统中的完整信息，包括身份标识、认证凭据、流量指标和账户状态。
type User struct {
	ID          string            `json:"id" bson:"_id"`                  // 用户唯一 ID 标识（常作为系统主键，例如用户名）
	Name        string            `json:"name" bson:"name"`                // 用户的中文显示昵称名称
	Credentials map[string]string `json:"credentials,omitempty" bson:"creds"` // 动态凭据字典（根据具体协议存放如 uuid, password, flow 等）

	Upload   int64 `json:"upload" bson:"upload"`     // 从该账户创建至今，累计已消耗的上传流量总和（字节）
	Download int64 `json:"download" bson:"download"` // 从该账户创建至今，累计已消耗的下载流量总和（字节）

	Quota          int64      `json:"quota" bson:"quota"`                  // 流量配额总量限制上限（字节），设为 0 表示不做限制
	SpeedLimitUp   int64      `json:"speed_limit_up" bson:"slup"`         // 账户最大允许上传限速速度（字节/秒），设为 0 表示不做限制
	SpeedLimitDown int64      `json:"speed_limit_down" bson:"sldown"`     // 账户最大允许下载限速速度（字节/秒），设为 0 表示不做限制
	ExpireAt       *time.Time `json:"expire_at,omitempty" bson:"expire_at"` // 账户到期强制失效时间。为 nil 时代表永久有效

	Enabled   bool      `json:"enabled" bson:"enabled"`       // 账户是否启用。如果为 false，用户将被切断且无法建立新连接
	CreatedAt time.Time `json:"created_at" bson:"created_at"` // 该用户记录在数据库中的创建生成时间

	SpeedUp   int64 `json:"speed_up" bson:"-"`   // 当前运行时的瞬时实时上传网速（字节/秒），不作持久化落库
	SpeedDown int64 `json:"speed_down" bson:"-"` // 当前运行时的瞬时实时下载网速（字节/秒），不作持久化落库
}
