// ============================================================================
// 文件说明：internal/domain/errors.go
// 职责概览：定义了 VPNView 系统级以及核心业务领域中，通用的标准哨兵错误对象（Sentinel Errors）。
//           各层业务组件（如 Handler, Service, Store, Adapter）在发生逻辑中断时，应抛出
//           并使用标准库 errors.Is 来精确判断具体错误类型，实现规范的异常处理与 HTTP 响应对齐。
// ============================================================================

package domain

import "errors"

var (
	// ErrNotSupported 表示当前加载的底层 VPN 适配器在硬件或逻辑上不支持此特定操作（如 Singbox 不支持对单个用户限速）。
	ErrNotSupported = errors.New("operation not supported by adapter")

	// ErrUserNotFound 表示所请求或操作的 VPN 用户账户 ID 在系统（数据库/适配器）中不存在。
	ErrUserNotFound = errors.New("user not found")

	// ErrUserExists 表示在创建新用户时发生 ID 重复冲突，目标用户已重复存在。
	ErrUserExists = errors.New("user already exists")

	// ErrQuotaExceeded 表示某用户的累计已用流量已经超过了其分配的流量配额上限。
	ErrQuotaExceeded = errors.New("traffic quota exceeded")

	// ErrSpeedLimitExceeded 表示某瞬时网速已经超过了速度限制。
	ErrSpeedLimitExceeded = errors.New("speed limit exceeded")

	// ErrExpired 表示该 VPN 用户的账户生命周期已经超过了设定的到期截止时间，强制过期失效。
	ErrExpired = errors.New("user expired")

	// ErrUnauthorized 表示未通过管理员 JWT 身份验证，访问安全受限资源被拒。
	ErrUnauthorized = errors.New("unauthorized")

	// ErrInvalidConfig 表示系统读取或加载的 YAML 核心配置中存在格式、数值错误或缺失关键必填字段。
	ErrInvalidConfig = errors.New("invalid configuration")
)
