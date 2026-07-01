package core

import (
	"fmt"
	"sort"

	"vpnview/internal/adapter/registry"
	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

type Runtime struct {
	ID           string
	Type         string
	Enabled      bool
	Role         string
	Status       string
	Adapter      port.VPNAdapter
	Capabilities domain.Capability
}

type Manager struct {
	defaultID string
	runtimes  map[string]Runtime
}

func NewManager(cfg config.CoreConfig, subscriptionDomain string) (*Manager, error) {
	m := &Manager{
		defaultID: cfg.Default,
		runtimes:  make(map[string]Runtime, len(cfg.Items)),
	}

	ids := make([]string, 0, len(cfg.Items))
	for id := range cfg.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		item := cfg.Items[id]
		if !item.Enabled {
			m.runtimes[id] = Runtime{
				ID:      id,
				Type:    item.Type,
				Enabled: false,
				Role:    item.Role,
				Status:  "disabled",
			}
			continue
		}

		raw := copyMap(item.Config)
		raw["type"] = item.Type
		if raw["subscription_domain"] == nil && subscriptionDomain != "" {
			raw["subscription_domain"] = subscriptionDomain
		}
		adapter, err := registry.Create(raw)
		if err != nil {
			_ = m.Close()
			return nil, fmt.Errorf("load core %q (%s): %w", id, item.Type, err)
		}
		m.runtimes[id] = Runtime{
			ID:           id,
			Type:         item.Type,
			Enabled:      true,
			Role:         item.Role,
			Status:       "ready",
			Adapter:      adapter,
			Capabilities: adapter.Capabilities(),
		}
	}

	if m.defaultID == "" && len(ids) > 0 {
		m.defaultID = ids[0]
	}
	if _, ok := m.runtimes[m.defaultID]; !ok {
		_ = m.Close()
		return nil, fmt.Errorf("default core %q is not configured", m.defaultID)
	}
	if rt := m.runtimes[m.defaultID]; !rt.Enabled || rt.Adapter == nil {
		_ = m.Close()
		return nil, fmt.Errorf("default core %q is not enabled", m.defaultID)
	}
	return m, nil
}

func (m *Manager) DefaultID() string {
	return m.defaultID
}

func (m *Manager) Default() port.VPNAdapter {
	rt := m.runtimes[m.defaultID]
	return rt.Adapter
}

func (m *Manager) DefaultRuntime() Runtime {
	return m.runtimes[m.defaultID]
}

func (m *Manager) Get(coreID string) (Runtime, bool) {
	rt, ok := m.runtimes[coreID]
	return rt, ok
}

func (m *Manager) List() []Runtime {
	out := make([]Runtime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Manager) SelectForUser(user *domain.User) (port.VPNAdapter, Runtime, error) {
	coreID := ""
	if user != nil {
		coreID = user.CoreID
	}
	if coreID == "" {
		coreID = m.defaultID
	}
	rt, ok := m.runtimes[coreID]
	if !ok {
		return nil, Runtime{}, fmt.Errorf("core %q is not configured", coreID)
	}
	if !rt.Enabled || rt.Adapter == nil {
		return nil, Runtime{}, fmt.Errorf("core %q is not enabled", coreID)
	}
	return rt.Adapter, rt, nil
}

func (m *Manager) Close() error {
	var firstErr error
	for _, rt := range m.runtimes {
		if rt.Adapter == nil {
			continue
		}
		if err := rt.Adapter.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
