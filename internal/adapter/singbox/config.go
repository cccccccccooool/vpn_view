// config.go 定义了 sing-box 适配器的配置结构及其解析逻辑。
package singbox

import (
	"fmt"
	"strconv"
)

// Config 保存 sing-box 适配器运行所需的全部配置项。
type Config struct {
	ConfigPath         string // sing-box 运行配置文件路径（输出目标）
	ConfigTemplatePath string // 配置模板文件路径（输入源），为空时与 ConfigPath 相同
	InboundTag         string // 指定要管理的 inbound tag，为空则管理所有 inbound
	ReloadCommand      string // 重载 sing-box 的外部命令，如 "systemctl reload sing-box"
	ClashAPI           string // Clash 兼容 API 地址，如 http://127.0.0.1:9090
	ClashSecret        string // Clash API 鉴权密钥
	V2RayAPI           string // V2Ray Stats gRPC API 地址，如 127.0.0.1:10085

	SubscriptionDomain string // 订阅链接中的服务器域名
	SubscriptionPort   int    // 订阅链接中的服务器端口
	SubscriptionType   string // 订阅链接中的传输类型（tcp/ws 等）
	SubscriptionTLS    bool   // 订阅链接是否启用 TLS
}

// ParseConfig 从原始键值对字典中解析并构造 Config 实例。
// 各字段均有合理的默认值，调用方无需保证所有键都存在。
func ParseConfig(raw map[string]any) Config {
	cfg := Config{
		ConfigPath:       stringValue(raw, "singbox_config_path", ""),
		InboundTag:       stringValue(raw, "inbound_tag", ""),
		ReloadCommand:    stringValue(raw, "reload_command", ""),
		ClashAPI:         stringValue(raw, "clash_api", ""),
		ClashSecret:      stringValue(raw, "clash_secret", ""),
		V2RayAPI:         stringValue(raw, "v2ray_api", ""),
		SubscriptionPort: intValue(raw, "subscription_port", 443),
		SubscriptionType: stringValue(raw, "subscription_type", "tcp"),
		SubscriptionTLS:  boolValue(raw, "subscription_tls", true),
	}
	cfg.ConfigTemplatePath = stringValue(raw, "config_template_path", cfg.ConfigPath)
	cfg.SubscriptionDomain = stringValue(raw, "subscription_domain", "")
	return cfg
}

// stringValue 从 map 中提取字符串值，不存在或为空时返回 fallback。
func stringValue(raw map[string]any, key, fallback string) string {
	if raw == nil {
		return fallback
	}
	if v, ok := raw[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// intValue 从 map 中提取整数值，支持 int/int64/float64/string 类型的自动转换。
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
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

// boolValue 从 map 中提取布尔值，支持 bool 和 string（"true"/"false"）类型的自动转换。
func boolValue(raw map[string]any, key string, fallback bool) bool {
	if raw == nil {
		return fallback
	}
	switch v := raw[key].(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return fallback
}

// ValidateForConfigWrites 校验配置是否满足写入配置文件的前提条件。
// 目前要求 ConfigPath 不为空。
func (c Config) ValidateForConfigWrites() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("singbox_config_path is required")
	}
	return nil
}
