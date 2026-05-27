// errors.go 定义了 VPNView 业务层通用的哨兵错误（sentinel errors）。
// 各层可通过 errors.Is 判断具体错误类型。

package domain

import "errors"

var (
	ErrNotSupported       = errors.New("operation not supported by adapter") // 适配器不支持该操作
	ErrUserNotFound       = errors.New("user not found")                     // 用户不存在
	ErrUserExists         = errors.New("user already exists")               // 用户已存在（ID 冲突）
	ErrQuotaExceeded      = errors.New("traffic quota exceeded")            // 流量配额已用尽
	ErrSpeedLimitExceeded = errors.New("speed limit exceeded")              // 超出限速阈值
	ErrExpired            = errors.New("user expired")                      // 用户账户已过期
	ErrUnauthorized       = errors.New("unauthorized")                      // 未授权访问
	ErrInvalidConfig      = errors.New("invalid configuration")             // 配置无效
)
