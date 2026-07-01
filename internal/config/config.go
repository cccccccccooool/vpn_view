package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level YAML configuration.
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Auth         AuthConfig         `yaml:"auth"`
	Adapter      map[string]any     `yaml:"adapter"`
	Cores        CoreConfig         `yaml:"cores"`
	Store        StoreConfig        `yaml:"store"`
	Limits       LimitsConfig       `yaml:"limits"`
	Subscription SubscriptionConfig `yaml:"subscription"`
	DDNS         *DDNSConfig        `yaml:"ddns,omitempty"`
	PollInterval string             `yaml:"poll_interval"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type AuthConfig struct {
	Secret   string `yaml:"secret"`
	TokenTTL string `yaml:"token_ttl"`
}

// CoreConfig describes all VPN cores loaded by the process. Older configs
// without this section are migrated in memory from adapter.
type CoreConfig struct {
	Default string              `yaml:"default"`
	Enabled []string            `yaml:"enabled"`
	Items   map[string]CoreItem `yaml:"items"`
}

type CoreItem struct {
	Type    string         `yaml:"type"`
	Enabled bool           `yaml:"enabled"`
	Role    string         `yaml:"role"`
	Config  map[string]any `yaml:"config"`
}

type StoreConfig struct {
	SQLite SQLiteConfig `yaml:"sqlite"`
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type LimitsConfig struct {
	GlobalUploadSpeed        int64 `yaml:"global_upload_speed"`
	GlobalDownloadSpeed      int64 `yaml:"global_download_speed"`
	DefaultUserUploadSpeed   int64 `yaml:"default_user_upload_speed"`
	DefaultUserDownloadSpeed int64 `yaml:"default_user_download_speed"`
	DefaultQuota             int64 `yaml:"default_quota"`
	SoftwareLimitStrikes     int   `yaml:"software_limit_strikes"`
}

type SubscriptionConfig struct {
	Mode         string `yaml:"mode"`
	Domain       string `yaml:"domain"`
	TemplatePath string `yaml:"template_path"`
}

type DDNSConfig struct {
	Provider      string `yaml:"provider"`
	Domain        string `yaml:"domain"`
	ZoneID        string `yaml:"zone_id"`
	RecordID      string `yaml:"record_id"`
	APIToken      string `yaml:"api_token"`
	CheckInterval string `yaml:"check_interval"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config YAML: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func MustLoad(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		panic("invalid config file: " + err.Error())
	}
	return cfg
}

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
	c.normalizeCores()
}

func (c *Config) GetPollInterval() time.Duration {
	return parseDuration(c.PollInterval, 10*time.Second)
}

func (c *Config) GetTokenTTL() time.Duration {
	return parseDuration(c.Auth.TokenTTL, 24*time.Hour)
}

func (c *Config) GetDDNSCheckInterval() time.Duration {
	if c.DDNS == nil {
		return 5 * time.Minute
	}
	return parseDuration(c.DDNS.CheckInterval, 5*time.Minute)
}

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

func (c *Config) normalizeCores() {
	if len(c.Cores.Items) == 0 {
		if c.Adapter == nil {
			c.Adapter = map[string]any{"type": "stub"}
		}
		legacy := copyMap(c.Adapter)
		adapterType := strings.TrimSpace(fmt.Sprint(legacy["type"]))
		if adapterType == "" || adapterType == "<nil>" {
			adapterType = "stub"
			legacy["type"] = adapterType
		}
		delete(legacy, "type")
		c.Cores = CoreConfig{
			Default: "legacy",
			Enabled: []string{"legacy"},
			Items: map[string]CoreItem{
				"legacy": {
					Type:    adapterType,
					Enabled: true,
					Role:    "primary",
					Config:  legacy,
				},
			},
		}
		return
	}

	if c.Cores.Items == nil {
		c.Cores.Items = map[string]CoreItem{}
	}
	enabledSet := map[string]bool{}
	for _, id := range c.Cores.Enabled {
		id = strings.TrimSpace(id)
		if id != "" {
			enabledSet[id] = true
		}
	}

	keys := make([]string, 0, len(c.Cores.Items))
	hasEnabled := false
	for id, item := range c.Cores.Items {
		keys = append(keys, id)
		if len(enabledSet) > 0 {
			item.Enabled = enabledSet[id]
		}
		if item.Config == nil {
			item.Config = map[string]any{}
		}
		if item.Type == "" {
			if rawType, ok := item.Config["type"]; ok {
				item.Type = fmt.Sprint(rawType)
			}
		}
		item.Type = strings.TrimSpace(item.Type)
		if item.Role == "" {
			item.Role = "primary"
		}
		if item.Enabled {
			hasEnabled = true
		}
		c.Cores.Items[id] = item
	}
	sort.Strings(keys)
	if c.Cores.Default == "" && len(keys) > 0 {
		c.Cores.Default = keys[0]
	}
	if !hasEnabled && c.Cores.Default != "" {
		item := c.Cores.Items[c.Cores.Default]
		item.Enabled = true
		c.Cores.Items[c.Cores.Default] = item
		c.Cores.Enabled = []string{c.Cores.Default}
	}
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
