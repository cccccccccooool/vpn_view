// ============================================================================
// 文件说明：internal/domain/adapter_profile.go
// 职责概览：定义了 VPN 适配器的底层运行行为特征配置结构体（AdapterProfile）。
//           详细描述了适配器如何更新用户账号、流量数据统计类型是累计式还是差值式、
//           配置热重载采用何种控制模式等，允许主程序根据配置结构化智能治理而无须硬编码。
// ============================================================================

package domain

// UserProvisionMode 描述适配器底层是如何将账号修改（增删改查）应用到 VPN 内核中的模式。
type UserProvisionMode string

const (
	UserProvisionUnknown     UserProvisionMode = ""
	UserProvisionAPI         UserProvisionMode = "api"          // 基于 RESTful / gRPC API 动态下发并即时生效
	UserProvisionConfigPatch UserProvisionMode = "config_patch" // 采用修改/覆盖/Patch JSON、YAML 配置文件的方式生效
	UserProvisionStatic      UserProvisionMode = "static"       // 静态不可改写模式
)

// TrafficMode 描述适配器抓取出的流量计数器，数据格式是绝对累计值（Cumulative）还是已经差值归一化后的数据。
type TrafficMode string

const (
	TrafficModeUnknown     TrafficMode = ""
	TrafficModeUnsupported TrafficMode = "unsupported" // 不支持流量计数
	TrafficModeCumulative  TrafficMode = "cumulative"  // 经典的绝对累计数值计数器（数据单调递增，每次获取都是历史总和）
	TrafficModeDelta       TrafficMode = "delta"       // 单次查询吐出相对上次的差值增量数据
)

// TrafficScope 描述流量度量指标的精细粒度和原生的暴露统计维度。
type TrafficScope string

const (
	TrafficScopeUnknown     TrafficScope = ""
	TrafficScopeUnsupported TrafficScope = "unsupported" // 不支持流量度量
	TrafficScopeUser        TrafficScope = "user"        // 底层原生基于“用户 ID”进行累计流量统计
	TrafficScopeConnection  TrafficScope = "connection"  // 底层只能统计“网络连接级”的瞬时流量数据
	TrafficScopeMixed       TrafficScope = "mixed"       // 用户级与连接级流量同时混合统计
)

// ReloadMode 描述当生成的配置文件或者元数据更新后，主程序如何让 VPN 代理服务重新加载配置的手段。
type ReloadMode string

const (
	ReloadModeUnknown ReloadMode = ""
	ReloadModeAPI     ReloadMode = "api"     // 主动请求控制 API 触发重新热加载（无断流风险）
	ReloadModeCommand ReloadMode = "command" // 调用特定的外部 shell 命令执行服务重启/重载（如 systemctl reload）
	ReloadModeManual  ReloadMode = "manual"  // 手动操作模式，需要人为介入
)

// ConfigFormat 描述该适配器所代表的 VPN 主配置文件的主要文件编码格式。
type ConfigFormat string

const (
	ConfigFormatUnknown ConfigFormat = ""
	ConfigFormatNone    ConfigFormat = "none" // 无配置文件
	ConfigFormatJSON    ConfigFormat = "json" // JSON 格式
	ConfigFormatYAML    ConfigFormat = "yaml" // YAML 格式
	ConfigFormatTOML    ConfigFormat = "toml" // TOML 格式
)

// IdentityProfile 声明适配器暴露给外部的各种网络身份暗示、查询及流量名字解析规则。
type IdentityProfile struct {
	MetadataKeys      []string `json:"metadata_keys,omitempty"`      // 在连接元数据中可能用于提取关联用户标识的 Key 列表
	RouteMarkerPrefix string   `json:"route_marker_prefix,omitempty"` // 特定用户所关联的分流路由标记标识前缀
	StatsNameFormat   string   `json:"stats_name_format,omitempty"`   // 流量 API 返回中，用户指标名字的字符串匹配模板（如 "user>>>{user_id}>>>traffic"）
	AllowIPFallback   bool     `json:"allow_ip_fallback"`             // 当无法直接在握手协议中查出用户 ID 时，是否降级允许使用 IP 反查 IPUserMap 来定位用户
}

// AdapterProfile 是一种结构化的能力与行为特征图谱。
// 主程序利用它能够智能且动态地处理不同适配器的底层差异，极大避免了在业务逻辑层写死各种 `if type == "singbox"`。
type AdapterProfile struct {
	Name              string            `json:"name"`                // 适配器的名字标识（如 "sing-box"）
	UserProvisionMode UserProvisionMode `json:"user_provision_mode"` // 账号下发写入的工作模式
	TrafficMode       TrafficMode       `json:"traffic_mode"`        // 流量数据暴露的模式
	TrafficScope      TrafficScope      `json:"traffic_scope"`       // 流量度量的细粒度维度
	ReloadMode        ReloadMode        `json:"reload_mode"`         // 重新加载配置并热生效的模式
	ConfigFormat      ConfigFormat      `json:"config_format"`       // 配置文件的载体格式
	Identity          IdentityProfile   `json:"identity"`            // 身份匹配反查的相关规格配置
}
