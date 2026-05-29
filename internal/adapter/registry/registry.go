package registry

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"vpnview/internal/port"
)

// Factory creates a VPN adapter from the adapter section of the YAML config.
type Factory func(raw map[string]any) (port.VPNAdapter, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes an adapter implementation available by name.
func Register(name string, factory Factory) {
	key := normalize(name)
	if key == "" {
		panic("adapter registry: empty adapter name")
	}
	if factory == nil {
		panic("adapter registry: nil factory for " + key)
	}

	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[key]; exists {
		panic("adapter registry: duplicate adapter " + key)
	}
	factories[key] = factory
}

// Create builds the adapter selected by YAML config key adapter.type.
func Create(raw map[string]any) (port.VPNAdapter, error) {
	if raw == nil {
		raw = map[string]any{"type": "stub"}
	}
	adapterType := normalize(fmt.Sprint(raw["type"]))
	if adapterType == "" || adapterType == "<nil>" {
		adapterType = "stub"
	}

	mu.RLock()
	factory := factories[adapterType]
	mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("未知的 VPN 适配器类型 %q，可用类型: %s", adapterType, strings.Join(Names(), ", "))
	}
	return factory(raw)
}

// Names returns the registered adapter names for diagnostics.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
