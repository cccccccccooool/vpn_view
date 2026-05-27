// Package domain 定义了 VPNView 的核心领域模型和业务规则。
// Core domain models and business rules for VPNView.
package domain

import "strings"

// Capability 使用位掩码（bitmask）表示 VPN 适配器所支持的功能集合。
// 每个能力对应一个独立的位，可通过按位或组合多个能力。
type Capability uint32

const (
	CapListUsers    Capability = 1 << iota // 列出所有用户
	CapAddUser                             // 添加用户
	CapRemoveUser                          // 删除用户
	CapDisableUser                         // 禁用用户
	CapEnableUser                          // 启用用户
	CapQueryTraffic                        // 查询流量统计
	CapRealtimeSpeed                       // 获取全局实时速度
	CapUserSpeed                           // 获取单用户实时速度
	CapActiveConns                         // 查看活跃连接
	CapKillConn                            // 断开指定连接
	CapSubscription                        // 生成订阅链接
	CapCredentialDefs                      // 提供凭据字段定义
	CapSpeedLimit                          // 设置单用户限速
	CapGlobalSpeedLimit                    // 设置全局限速
)

var capNames = map[Capability]string{
	CapListUsers:        "list_users",
	CapAddUser:          "add_user",
	CapRemoveUser:       "remove_user",
	CapDisableUser:      "disable_user",
	CapEnableUser:       "enable_user",
	CapQueryTraffic:     "query_traffic",
	CapRealtimeSpeed:    "realtime_speed",
	CapUserSpeed:        "user_speed",
	CapActiveConns:      "active_conns",
	CapKillConn:         "kill_conn",
	CapSubscription:     "subscription",
	CapCredentialDefs:   "credential_defs",
	CapSpeedLimit:       "speed_limit",
	CapGlobalSpeedLimit: "global_speed_limit",
}

// AllCapabilities 返回所有已定义的能力常量列表，用于遍历和展示。
func AllCapabilities() []Capability {
	return []Capability{
		CapListUsers,
		CapAddUser,
		CapRemoveUser,
		CapDisableUser,
		CapEnableUser,
		CapQueryTraffic,
		CapRealtimeSpeed,
		CapUserSpeed,
		CapActiveConns,
		CapKillConn,
		CapSubscription,
		CapCredentialDefs,
		CapSpeedLimit,
		CapGlobalSpeedLimit,
	}
}

// Has 判断当前能力集合中是否包含指定的 flag 能力位。
func (c Capability) Has(flag Capability) bool {
	return c&flag != 0
}

// Name 返回单个能力位对应的可读名称；若未知则返回 "unknown"。
func (c Capability) Name() string {
	if name, ok := capNames[c]; ok {
		return name
	}
	return "unknown"
}

// String 将能力集合格式化为逗号分隔的名称字符串，实现 fmt.Stringer 接口。
func (c Capability) String() string {
	names := make([]string, 0)
	for _, cap := range AllCapabilities() {
		if c.Has(cap) {
			names = append(names, cap.Name())
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// ToMap 将能力集合转换为 map[能力名称]是否启用 的映射，便于 JSON 序列化或前端展示。
func (c Capability) ToMap() map[string]bool {
	out := make(map[string]bool, len(capNames))
	for _, cap := range AllCapabilities() {
		out[cap.Name()] = c.Has(cap)
	}
	return out
}
