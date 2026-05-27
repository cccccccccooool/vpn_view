// user.go 定义了 VPN 用户的核心领域模型。

package domain

import "time"

// User 表示一个 VPN 用户的完整信息，包含身份、凭据、流量统计、限速策略和账户状态。
type User struct {
	ID          string            `json:"id" bson:"_id"`          // 用户唯一标识
	Name        string            `json:"name" bson:"name"`        // 显示名称
	Credentials map[string]string `json:"credentials,omitempty" bson:"creds"` // 认证凭据键值对（如密码、UUID 等）

	Upload   int64 `json:"upload" bson:"upload"`     // 累计上传流量（字节）
	Download int64 `json:"download" bson:"download"` // 累计下载流量（字节）

	Quota          int64      `json:"quota" bson:"quota"`                  // 流量配额（字节），0 表示不限制
	SpeedLimitUp   int64      `json:"speed_limit_up" bson:"slup"`         // 上传限速（字节/秒），0 表示不限制
	SpeedLimitDown int64      `json:"speed_limit_down" bson:"sldown"`     // 下载限速（字节/秒），0 表示不限制
	ExpireAt       *time.Time `json:"expire_at,omitempty" bson:"expire_at"` // 账户过期时间，nil 表示永不过期

	Enabled   bool      `json:"enabled" bson:"enabled"`       // 账户是否启用
	CreatedAt time.Time `json:"created_at" bson:"created_at"` // 账户创建时间

	SpeedUp   int64 `json:"speed_up" bson:"-"`   // 当前实时上传速度（字节/秒），仅运行时使用，不持久化
	SpeedDown int64 `json:"speed_down" bson:"-"` // 当前实时下载速度（字节/秒），仅运行时使用，不持久化
}
