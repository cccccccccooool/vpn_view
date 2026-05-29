// ============================================================================
// 文件说明：internal/config/config.go
// 职责概览：负责加载、解密和管理整个 VPNView 系统全局 YAML 配置文件（Config）。
//           包含各层子配置映射（HTTP 监听、JWT 密钥、底层适配器私有字典、
//           SQLite 数据库路径、限速阈值及 strike 惩罚、订阅分发及 DDNS 定时解析），
//           并提供合理的硬编码系统缺省默认值进行防御性覆盖填充。
// ============================================================================

package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是整个面板服务进程最顶层的配置统筹数据模型，对应 YAML 配置文件的根结构。
type Config struct {
	Server       ServerConfig       `yaml:"server"`        // HTTP 面板监听端口配置
	Auth         AuthConfig         `yaml:"auth"`          // 安全与管理员 JWT 令牌配置
	Adapter      map[string]any     `yaml:"adapter"`       // 开放给 VPN 底层适配器的无结构私有参数字典映射（类型、命令、端口等）
	Store        StoreConfig        `yaml:"store"`         // 持久化存储引擎配置
	Limits       LimitsConfig       `yaml:"limits"`        // 限制、默认配额和超速惩罚 Strike 规则配置
	Subscription SubscriptionConfig `yaml:"subscription"`  // 订阅分发域名及参数配置
	DDNS         *DDNSConfig        `yaml:"ddns,omitempty"` // 动态公网 IP 定时解析更新服务配置（可选）
	PollInterval string             `yaml:"poll_interval"` // 流量及网速计算后台协程拉取轮询的间隔时间字符串（如 "10s", "5s"）
}

// ServerConfig 控制 HTTP API 面板的网络绑定与监听端口。
type ServerConfig struct {
	Listen string `yaml:"listen"` // 格式如 "0.0.0.0:19463"
}

// AuthConfig 存放管理员后台安全校验的参数。
type AuthConfig struct {
	Secret   string `yaml:"secret"`    // 签名 JWT 所用对称密钥密文
	TokenTTL string `yaml:"token_ttl"` // 签发出的管理员令牌的免登录生存有效期（如 "24h"）
}

// StoreConfig 决定系统持久化引擎，目前主打 SQLite。
type StoreConfig struct {
	SQLite SQLiteConfig `yaml:"sqlite"`
}

// SQLiteConfig 存放 SQLite 嵌入式轻量数据库的文件路径。
type SQLiteConfig struct {
	Path string `yaml:"path"` // 数据库文件路径（如 "./data/vpnview.db"）
}

// LimitsConfig 定义用户默认配额、限速、连续超速 Strike 关停策略。
type LimitsConfig struct {
	GlobalUploadSpeed        int64 `yaml:"global_upload_speed"`          // 全局服务器最大上传速率配额（字节/秒），0 代表不封顶
	GlobalDownloadSpeed      int64 `yaml:"global_download_speed"`        // 全局服务器最大下载速率配额（字节/秒），0 代表不封顶
	DefaultUserUploadSpeed   int64 `yaml:"default_user_upload_speed"`    // 新建用户默认填充的最高上传网速，0 代表不限制
	DefaultUserDownloadSpeed int64 `yaml:"default_user_download_speed"`  // 新建用户默认填充的最高下载网速，0 代表不限制
	DefaultQuota             int64 `yaml:"default_quota"`                // 新建用户默认填充的流量配额总量（字节），0 代表不限制
	SoftwareLimitStrikes     int   `yaml:"software_limit_strikes"`       // 若底层不支持原生硬件限速，主程序通过软件检测发现用户连续超速几次后执行停机禁用惩罚
}

// SubscriptionConfig 控制用户多协议节点配置文件下发的路由和展现规格。
type SubscriptionConfig struct {
	Mode         string `yaml:"mode"`          // 订阅生成器内置模式
	Domain       string `yaml:"domain"`        // 拼接生成节点链接时使用的外部可访问域名
	TemplatePath string `yaml:"template_path"` // 订阅下发的额外模板路径
}

// DDNSConfig 定义通过 Cloudflare 动态更新本机公网 IP 解析指标所需的鉴权令牌与域名记录指标。
type DDNSConfig struct {
	Provider      string `yaml:"provider"`       // DDNS 厂商，当前仅支持 "cloudflare"
	Domain        string `yaml:"domain"`         // 需要自动更新指向的 DNS 解析域名（如 "vpn.example.com"）
	ZoneID        string `yaml:"zone_id"`        // Cloudflare 解析域名的托管区域唯一 Zone ID
	RecordID      string `yaml:"record_id"`      // 解析域名对应的 DNS 条目唯一 Record ID
	APIToken      string `yaml:"api_token"`      // 拥有 DNS 修改读写权限的 Cloudflare API 授权 Token
	CheckInterval string `yaml:"check_interval"`  // 动态 IP 定时巡检探测的检查频率（如 "5m", "10m"）
}

// Load 从给定的磁盘绝对/相对路径加载并解析 YAML 全局配置文件，并执行 applyDefaults 进行安全缺省值覆盖。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 YAML 失败: %w", err)
	}
	cfg.applyDefaults() // 注入默认值
	return &cfg, nil
}

// MustLoad 同 Load 作用一致，若读取或解析出错，直接触发 Panic 中断服务。
// 适用于程序启动首要初始化阶段。
func MustLoad(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		panic("配置文件缺失或解析损坏，程序致命退出: " + err.Error())
	}
	return cfg
}

// applyDefaults 扫描配置项，对未配置或者配置为空的字段，强行填入系统推荐的稳定默认参数。
func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = "0.0.0.0:19463" // 默认监听 19463 端口
	}
	if c.Auth.Secret == "" {
		c.Auth.Secret = "change-me" // 默认管理员签名密钥
	}
	if c.Store.SQLite.Path == "" {
		c.Store.SQLite.Path = "./data/vpnview.db" // 默认嵌入式数据库存放点
	}
	if c.PollInterval == "" {
		c.PollInterval = "10s" // 默认 10 秒拉取一次流量
	}
	if c.Limits.SoftwareLimitStrikes <= 0 {
		c.Limits.SoftwareLimitStrikes = 3 // 默认连续 3 次测速超限封禁
	}
	if c.Adapter == nil {
		// 默认加载 Stub 开发用内存桩测试适配器
		c.Adapter = map[string]any{"type": "stub"}
	}
}

// GetPollInterval 将流量拉取轮询参数字符串安全解析为 time.Duration 类型。若解析失败，默认返回 10 秒。
func (c *Config) GetPollInterval() time.Duration {
	return parseDuration(c.PollInterval, 10*time.Second)
}

// GetTokenTTL 将 JWT 生存期解析为 time.Duration。解析失败或未配置默认返回 24 小时。
func (c *Config) GetTokenTTL() time.Duration {
	return parseDuration(c.Auth.TokenTTL, 24*time.Hour)
}

// GetDDNSCheckInterval 将 DDNS 检测周期字符串转换为 time.Duration。未配置或解析失败默认返回 5 分钟。
func (c *Config) GetDDNSCheckInterval() time.Duration {
	if c.DDNS == nil {
		return 5 * time.Minute
	}
	return parseDuration(c.DDNS.CheckInterval, 5*time.Minute)
}

// parseDuration 通用时间段解析辅助工具。若出错或数值不合规自动采用给定的 fallback 默认值。
func parseDuration(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
