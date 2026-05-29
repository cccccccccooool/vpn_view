// ============================================================================
// 文件说明：internal/domain/capability.go
// 职责概览：定义了 VPN 适配器所能支持的各项功能标志位（位掩码类型）。
//           主程序依靠此能力位掩码在运行时动态感知适配器是否具有诸如用户限速、
//           活跃连接查询、生成订阅等特定能力，从而动态开合 API 路由、进行页面降级渲染。
// ============================================================================

package domain

import "strings"

// Capability 使用 32 位无符号整数的位掩码形式，标识适配器所支持的特定能力项子集。
type Capability uint32

// 各项特定能力对应的二进制位掩码常量定义。
const (
	CapListUsers    Capability = 1 << iota // 支持列出所有注册的用户
	CapAddUser                             // 支持向后端添加新用户
	CapRemoveUser                          // 支持从后端彻底删除用户
	CapDisableUser                         // 支持临时禁用用户连接
	CapEnableUser                          // 支持恢复启用用户连接
	CapQueryTraffic                        // 支持查询用户的累计流量数据
	CapRealtimeSpeed                       // 支持获取服务器全局的实时速度
	CapUserSpeed                           // 支持获取特定单个用户的实时速度
	CapActiveConns                         // 支持查看服务器的当前活动 TCP/UDP 连接
	CapKillConn                            // 支持强制阻断并断开指定的活动网络连接
	CapSubscription                        // 支持为用户配置渲染输出订阅配置文件
	CapCredentialDefs                      // 支持返回各协议凭据的表单定义，支持前端动态表单
	CapSpeedLimit                          // 支持设置单个用户的上传/下载速度上限
	CapGlobalSpeedLimit                    // 支持设置服务器全局的上传/下载速度上限
)

// 各种能力标志位与其对应的人类可读的 JSON/API 接口标识名称的映射表。
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

// AllCapabilities 返回系统中全部已定义的能力项列表，常用于能力列表的循环扫描。
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

// Has 运算判断当前 Capability 位掩码中是否集成了指定的单个能力项 flag。
func (c Capability) Has(flag Capability) bool {
	return c&flag != 0
}

// Name 将给定的单个能力常量翻译成英文唯一标识串，用于数据序列化；未知则返回 "unknown"。
func (c Capability) Name() string {
	if name, ok := capNames[c]; ok {
		return name
	}
	return "unknown"
}

// String 将当前的能力集合格式化为以逗号分隔的可读英文字符串，实现了 Go 标准库的 fmt.Stringer 接口。
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

// ToMap 将当前的全部能力集按名字平铺转换为 map 键值对映射，方便 API 直接返回 JSON 格式给前端渲染。
func (c Capability) ToMap() map[string]bool {
	out := make(map[string]bool, len(capNames))
	for _, cap := range AllCapabilities() {
		out[cap.Name()] = c.Has(cap)
	}
	return out
}
