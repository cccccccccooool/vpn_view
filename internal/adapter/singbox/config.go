// ============================================================================
// 文件说明：internal/adapter/singbox/config.go
// 职责概览：定义了 Sing-box 客户端适配器的配置属性结构体（Config）及其解析逻辑。
//           提供从系统的原始无结构万能配置 map 字典中提取、类型转换并安全填充 Config 属性。
//           支持合理的系统推荐参数兜底，保障了 Sing-box API 客户端在各种复杂网络配置下的健壮性。
// ============================================================================

package singbox

import (
	"fmt"
	"strconv"
)

// Config 封装了 Sing-box 后端代理适配器运行所需的全部必要与可选控制配置项。
type Config struct {
	ConfigPath         string // 最终运行生成的 Sing-box JSON 配置文件生成路径（物理输出目标）
	ConfigTemplatePath string // 起始的 Sing-box 配置底座模板路径（输入源），若为空默认对齐 ConfigPath
	InboundTag         string // 限定需要通过面板注入并管理的 Inbound 标识 tag（如果为空则解析并管理底座内全部 inbounds 模块）
	ReloadCommand      string // 配置文件更新后，系统执行热重载的外部 Shell 命令（如 "systemctl reload sing-box"）
	ClashAPI           string // 底层内置 Clash 兼容 API 服务的 HTTP 控制端地址（如 "http://127.0.0.1:9090"）
	ClashSecret        string // 访问 Clash API 所需的鉴权 Token
	V2RayAPI           string // 底层内置 V2Ray Stats gRPC API 服务的通信端口与地址（如 "127.0.0.1:10085"）

	SubscriptionDomain string // 自动拼接生成用户订阅节点时，给客户端直连使用的公网解析域名
	SubscriptionPort   int    // 拼接订阅节点时的公网连接端口
	SubscriptionType   string // 订阅节点传输协议网络层载体类型（如 "tcp", "ws" 等）
	SubscriptionTLS    bool   // 订阅节点是否强行开启并匹配 TLS 安全隧道
}

// ParseConfig 输入一个通用的无类型配置属性字典 map，反序列化并自动对齐翻译填充为一个类型安全的 Config 结构体。
// 支持针对布尔值、整数、字符串的自动安全防崩溃转换，不存在的键自动赋系统缺省参数。
func ParseConfig(raw map[string]any) Config {
	cfg := Config{
		ConfigPath:       stringValue(raw, "singbox_config_path", ""),
		InboundTag:       stringValue(raw, "inbound_tag", ""),
		ReloadCommand:    stringValue(raw, "reload_command", ""),
		ClashAPI:         stringValue(raw, "clash_api", ""),
		ClashSecret:      stringValue(raw, "clash_secret", ""),
		V2RayAPI:         stringValue(raw, "v2ray_api", ""),
		SubscriptionPort: intValue(raw, "subscription_port", 443), // 默认 443 端口
		SubscriptionType: stringValue(raw, "subscription_type", "tcp"),
		SubscriptionTLS:  boolValue(raw, "subscription_tls", true),
	}
	cfg.ConfigTemplatePath = stringValue(raw, "config_template_path", cfg.ConfigPath)
	cfg.SubscriptionDomain = stringValue(raw, "subscription_domain", "")
	return cfg
}

// stringValue 从 map 中提取字符串属性值，为 nil 或空时安全采用 fallback 兜底。
func stringValue(raw map[string]any, key, fallback string) string {
	if raw == nil {
		return fallback
	}
	if v, ok := raw[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// intValue 从 map 中提取整数值，防御性地兼容 string、int64、float64 类型自动强转。
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

// boolValue 从 map 中提取布尔值，支持 string 类型解析转换。
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

// ValidateForConfigWrites 校验当前配置参数是否具备合规的配置文件写磁盘权限。
func (c Config) ValidateForConfigWrites() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("必须在配置文件中指定 singbox_config_path 运行配置文件物理保存路径")
	}
	return nil
}
