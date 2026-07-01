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
