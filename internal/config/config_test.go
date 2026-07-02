package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesLegacyAdapterToCore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`
adapter:
  type: "stub"
  mock_users: 2
store:
  sqlite:
    path: "./test.db"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cores.Default != "legacy" {
		t.Fatalf("default core = %q, want legacy", cfg.Cores.Default)
	}
	item, ok := cfg.Cores.Items["legacy"]
	if !ok {
		t.Fatalf("legacy core missing")
	}
	if !item.Enabled {
		t.Fatalf("legacy core should be enabled")
	}
	if item.Type != "stub" {
		t.Fatalf("legacy type = %q, want stub", item.Type)
	}
	if got := item.Config["mock_users"]; got != 2 {
		t.Fatalf("mock_users = %#v, want 2", got)
	}
	if _, exists := item.Config["type"]; exists {
		t.Fatalf("legacy core config should not keep adapter type")
	}
}

func TestLoadKeepsExplicitCores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`
cores:
  default: "main"
  enabled: ["main"]
  items:
    main:
      type: "stub"
      enabled: true
      role: "primary"
      config:
        mock_users: 1
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cores.Default != "main" {
		t.Fatalf("default core = %q, want main", cfg.Cores.Default)
	}
	if cfg.Cores.Items["main"].Type != "stub" {
		t.Fatalf("main type = %q, want stub", cfg.Cores.Items["main"].Type)
	}
}

func TestLoadMigratesLegacyXrayAdapterToCore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`
adapter:
  type: "xray"
  xray_config_path: "/etc/xray/config.json"
  config_template_path: "/etc/xray/config.template.json"
  api_address: "127.0.0.1:10085"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	item := cfg.Cores.Items["legacy"]
	if item.Type != "xray" {
		t.Fatalf("legacy type = %q, want xray", item.Type)
	}
	if got := item.Config["xray_config_path"]; got != "/etc/xray/config.json" {
		t.Fatalf("xray_config_path = %#v", got)
	}
	if got := item.Config["api_address"]; got != "127.0.0.1:10085" {
		t.Fatalf("api_address = %#v", got)
	}
	if _, exists := item.Config["type"]; exists {
		t.Fatalf("legacy core config should not keep adapter type")
	}
}

func TestLoadNormalizesSecurityAndDDNS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`
adapter:
  type: "stub"
security:
  hsts_enabled: true
ddns:
  provider: "CloudFlare"
  domain: "vpn.example.com"
  zone_id: "zone"
  record_id: "record"
  api_token: "token"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Security.CSP == "" {
		t.Fatalf("security CSP should default")
	}
	if !cfg.Security.HSTSEnabled {
		t.Fatalf("explicit HSTS setting should be preserved")
	}
	if cfg.Security.HSTSMaxAge != 31536000 {
		t.Fatalf("HSTS max-age = %d, want 31536000", cfg.Security.HSTSMaxAge)
	}
	if cfg.DDNS.Provider != "cloudflare" {
		t.Fatalf("DDNS provider = %q, want cloudflare", cfg.DDNS.Provider)
	}
	if cfg.DDNS.RecordType != "A" {
		t.Fatalf("DDNS record type = %q, want A", cfg.DDNS.RecordType)
	}
	if cfg.DDNS.TTL != 1 {
		t.Fatalf("DDNS TTL = %d, want 1", cfg.DDNS.TTL)
	}
	if len(cfg.DDNS.IPCheckURLs) == 0 {
		t.Fatalf("DDNS IP resolvers should default")
	}
}
