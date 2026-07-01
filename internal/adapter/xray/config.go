// ============================================================================
// 文件说明：internal/adapter/xray/config.go
// 职责概览：定义了 Xray-core / V2Ray-core 网络代理适配器的配置属性结构体（Config）及其解析逻辑。
//           Xray 是 V2Ray 的一个分支（fork），两者在配置结构（inbounds/settings/users）和
//           gRPC Stats 统计接口上高度同构，仅在 gRPC 服务的 protobuf 包路径上存在差异
//           （xray.app.stats.command vs v2ray.core.app.stats.command）。因此本适配器采用
//           "单包多别名" 模式，通过 Variant 字段区分核心变体，从系统的无结构万能配置 map
//           中安全提取、类型转换并兜底填充配置，保障两类核心在各种部署环境下的健壮启动。
// ============================================================================

package xray

import (
	"fmt"
	"strconv"
	"strings"
)

// gRPC Stats 服务 QueryStats 方法的全限定调用路由常量。
// Xray-core 在 fork 时将 protobuf 包名从 v2ray.core.* 重命名为 xray.*，两条路径的消息结构完全一致。
const (
	xrayStatsQueryMethod  = "/xray.app.stats.command.StatsService/QueryStats"       // Xray-core 变体
	v2rayStatsQueryMethod = "/v2ray.core.app.stats.command.StatsService/QueryStats" // V2Ray-core 变体
)

// Config 封装了 Xray / V2Ray 后端代理适配器运行所需的全部必要与可选控制配置项。
type Config struct {
	Variant            string // 核心变体归一化名（"xray" 或 "v2ray"），决定 gRPC 统计服务路径与 Profile 展示名
	ConfigPath         string // 最终运行生成的核心 JSON 配置文件物理输出路径
	ConfigTemplatePath string // 起始配置底座模板路径（输入源），若为空默认对齐 ConfigPath
	InboundTag         string // 限定需要通过面板注入并管理的 Inbound 标识 tag（为空则管理底座内全部可识别 inbounds）
	ReloadCommand      string // 配置文件更新后执行热重载的外部 Shell 命令（如 "systemctl reload xray"）

	APIAddress       string // 核心内置 Stats gRPC API 服务的通信地址（如 "127.0.0.1:10085"），空则不启用流量统计
	StatsQueryMethod string // gRPC QueryStats 方法全路径，默认按 Variant 推导，可被配置显式覆盖

	SubscriptionDomain string // 生成用户订阅节点时给客户端直连使用的公网解析域名
	SubscriptionPort   int    // 拼接订阅节点时的公网连接端口
	SubscriptionType   string // 订阅节点传输层网络类型（如 "tcp", "ws", "grpc"）
	SubscriptionTLS    bool   // 订阅节点是否开启并匹配 TLS 安全隧道
}

// ParseConfig 输入通用无类型配置字典与核心变体名，反序列化并对齐填充为类型安全的 Config。
// 为兼容不同书写习惯与从 singbox 平滑迁移，多个语义相同的键名支持别名归一化。
func ParseConfig(raw map[string]any, variant string) Config {
	variant = normalizeVariant(variant)

	cfg := Config{
		Variant:          variant,
		ConfigPath:       firstStringValue(raw, "", "xray_config_path", "v2ray_config_path", "config_path"),
		InboundTag:       stringValue(raw, "inbound_tag", ""),
		ReloadCommand:    stringValue(raw, "reload_command", ""),
		APIAddress:       firstStringValue(raw, "", "api_address", "stats_api", "xray_api", "v2ray_api"),
		SubscriptionPort: intValue(raw, "subscription_port", 443),
		SubscriptionType: stringValue(raw, "subscription_type", "tcp"),
		SubscriptionTLS:  boolValue(raw, "subscription_tls", true),
	}
	cfg.ConfigTemplatePath = stringValue(raw, "config_template_path", cfg.ConfigPath)
	cfg.SubscriptionDomain = stringValue(raw, "subscription_domain", "")
	cfg.StatsQueryMethod = stringValue(raw, "stats_query_method", defaultStatsQueryMethod(variant))
	return cfg
}

// normalizeVariant 将注册名/配置类型归一化为内部稳定的核心变体标识。
// v2ray / v2ray-core / v2fly 归入 "v2ray"，其余（xray / xray-core）归入 "xray"。
func normalizeVariant(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(n, "v2ray") || strings.Contains(n, "v2fly") {
		return "v2ray"
	}
	return "xray"
}

// defaultStatsQueryMethod 依据核心变体返回其原生 gRPC Stats 服务的 QueryStats 默认调用路径。
func defaultStatsQueryMethod(variant string) string {
	if normalizeVariant(variant) == "v2ray" {
		return v2rayStatsQueryMethod
	}
	return xrayStatsQueryMethod
}

// ProfileName 返回用于 AdapterProfile 展示的核心名称。
func (c Config) ProfileName() string {
	if c.Variant == "v2ray" {
		return "v2ray"
	}
	return "xray"
}

// stringValue 从 map 中提取字符串属性值，为 nil 或空时安全采用 fallback 兜底。
func stringValue(raw map[string]any, key, fallback string) string {
	if raw == nil {
		return fallback
	}
	if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// firstStringValue 依序尝试多个别名键，返回首个非空字符串值，全部缺失时返回 fallback。
func firstStringValue(raw map[string]any, fallback string, keys ...string) string {
	for _, key := range keys {
		if v := stringValue(raw, key, ""); v != "" {
			return v
		}
	}
	return fallback
}

// intValue 从 map 中提取整数值，防御性地兼容 string、int、int64、float64 类型自动强转。
func intValue(raw map[string]any, key string, fallback int) int {
	if raw == nil {
		return fallback
	}
	switch v := raw[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// boolValue 从 map 中提取布尔值，支持 string 类型解析转换。
func boolValue(raw map[string]any, key string, fallback bool) bool {
	if raw == nil {
		return fallback
	}
	switch v := raw[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

// ValidateForConfigWrites 校验当前配置参数是否具备合规的配置文件写磁盘能力。
func (c Config) ValidateForConfigWrites() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("必须在配置中指定 xray_config_path（或 config_path）运行配置文件物理保存路径")
	}
	return nil
}
