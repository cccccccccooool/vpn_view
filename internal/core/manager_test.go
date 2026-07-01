package core

import (
	"testing"

	_ "vpnview/internal/adapter/stub"
	"vpnview/internal/config"
	"vpnview/internal/domain"
)

func TestManagerLoadsSingleLegacyCore(t *testing.T) {
	manager, err := NewManager(config.CoreConfig{
		Default: "legacy",
		Enabled: []string{"legacy"},
		Items: map[string]config.CoreItem{
			"legacy": {
				Type:    "stub",
				Enabled: true,
				Role:    "primary",
				Config:  map[string]any{"mock_users": 0},
			},
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if manager.DefaultID() != "legacy" {
		t.Fatalf("default id = %q, want legacy", manager.DefaultID())
	}
	if manager.Default() == nil {
		t.Fatalf("default adapter is nil")
	}
	user := &domain.User{ID: "alice"}
	adapter, rt, err := manager.SelectForUser(user)
	if err != nil {
		t.Fatal(err)
	}
	if adapter != manager.Default() {
		t.Fatalf("empty user core should select default adapter")
	}
	if rt.ID != "legacy" || rt.Type != "stub" {
		t.Fatalf("runtime = %#v, want legacy stub", rt)
	}
}

func TestManagerRejectsDisabledDefaultCore(t *testing.T) {
	_, err := NewManager(config.CoreConfig{
		Default: "legacy",
		Items: map[string]config.CoreItem{
			"legacy": {Type: "stub", Enabled: false},
		},
	}, "")
	if err == nil {
		t.Fatalf("expected disabled default core to fail")
	}
}
