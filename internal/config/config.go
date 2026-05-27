// Package config 负责加载、解析和管理 VPNView 的 YAML 配置文件。
// 支持默认值回退，涵盖服务器监听、认证、存储、限速、订阅及 DDNS 等配置项。
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是应用程序的顶层配置结构体，对应 YAML 配置文件的根节点。
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Auth         AuthConfig         `yaml:"auth"`
	Adapter      map[string]any     `yaml:"adapter"`
	Store        StoreConfig        `yaml:"store"`
	Limits       LimitsConfig       `yaml:"limits"`
	Subscription SubscriptionConfig `yaml:"subscription"`
	DDNS         *DDNSConfig        `yaml:"ddns,omitempty"`
	PollInterval string             `yaml:"poll_interval"`
}

// ServerConfig 定义 HTTP 服务器的监听地址等网络配置。
type ServerConfig struct {
	Listen string `yaml:"listen"`
}

// AuthConfig 定义认证相关配置，包括 JWT 签名密钥和令牌有效期。
type AuthConfig struct {
	Secret   string `yaml:"secret"`
	TokenTTL string `yaml:"token_ttl"`
}

// StoreConfig 定义持久化存储的配置，当前仅支持 SQLite 后端。
type StoreConfig struct {
	SQLite  SQLiteConfig `yaml:"sqlite"`
}

// SQLiteConfig 定义 SQLite 数据库的连接参数。
type SQLiteConfig struct {
	Path string `yaml:"path"`
}

// LimitsConfig 定义全局及用户级别的流量限速、配额和软件限制策略。
type LimitsConfig struct {
	GlobalUploadSpeed        int64 `yaml:"global_upload_speed"`
	GlobalDownloadSpeed      int64 `yaml:"global_download_speed"`
	DefaultUserUploadSpeed   int64 `yaml:"default_user_upload_speed"`
	DefaultUserDownloadSpeed int64 `yaml:"default_user_download_speed"`
	DefaultQuota             int64 `yaml:"default_quota"`
	SoftwareLimitStrikes     int   `yaml:"software_limit_strikes"`
}

// SubscriptionConfig 定义订阅链接的生成模式、域名及模板路径。
type SubscriptionConfig struct {
	Mode         string `yaml:"mode"`
	Domain       string `yaml:"domain"`
	TemplatePath string `yaml:"template_path"`
}

// DDNSConfig 定义动态 DNS 更新的配置，包括 DNS 提供商凭据和检查间隔。
type DDNSConfig struct {
	Provider      string `yaml:"provider"`
	Domain        string `yaml:"domain"`
	ZoneID        string `yaml:"zone_id"`
	RecordID      string `yaml:"record_id"`
	APIToken      string `yaml:"api_token"`
	CheckInterval string `yaml:"check_interval"`
}

// Load 从指定路径读取 YAML 配置文件并反序列化为 Config 结构体。
// 加载完成后会自动填充缺省默认值。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// MustLoad 与 Load 功能相同，但在加载失败时直接 panic。
// 适用于程序启动阶段，配置文件缺失应视为致命错误的场景。
func MustLoad(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		panic("failed to load config: " + err.Error())
	}
	return cfg
}

// applyDefaults 为未显式设置的配置项填充合理的默认值。
func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = "0.0.0.0:19463"
	}
	if c.Auth.Secret == "" {
		c.Auth.Secret = "change-me"
	}
	if c.Store.SQLite.Path == "" {
		c.Store.SQLite.Path = "./data/vpnview.db"
	}
	if c.PollInterval == "" {
		c.PollInterval = "10s"
	}
	if c.Limits.SoftwareLimitStrikes <= 0 {
		c.Limits.SoftwareLimitStrikes = 3
	}
	if c.Adapter == nil {
		c.Adapter = map[string]any{"type": "stub"}
	}
}

// GetPollInterval 返回数据轮询间隔，解析失败或未配置时默认为 10 秒。
func (c *Config) GetPollInterval() time.Duration {
	return parseDuration(c.PollInterval, 10*time.Second)
}

// GetTokenTTL 返回 JWT 令牌的有效期，解析失败或未配置时默认为 24 小时。
func (c *Config) GetTokenTTL() time.Duration {
	return parseDuration(c.Auth.TokenTTL, 24*time.Hour)
}

// GetDDNSCheckInterval 返回 DDNS 记录检查间隔，未配置 DDNS 或解析失败时默认为 5 分钟。
func (c *Config) GetDDNSCheckInterval() time.Duration {
	if c.DDNS == nil {
		return 5 * time.Minute
	}
	return parseDuration(c.DDNS.CheckInterval, 5*time.Minute)
}

// parseDuration 将字符串解析为 time.Duration，空值或解析失败时返回 fallback。
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
