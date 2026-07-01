package handler

import (
	"net/http"

	"vpnview/internal/config"
	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

type CapabilityHandler struct {
	cores *core.Manager
	cfg   *config.Config
}

func NewCapabilityHandler(cores *core.Manager, cfg *config.Config) *CapabilityHandler {
	return &CapabilityHandler{cores: cores, cfg: cfg}
}

func (h *CapabilityHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	defaultRT := h.cores.DefaultRuntime()
	defaultAdapter := defaultRT.Adapter
	caps := defaultAdapter.Capabilities()
	fields := credentialFields(defaultAdapter, caps)
	profile := adapterProfile(defaultAdapter)

	corePayload := make([]map[string]any, 0)
	for _, rt := range h.cores.List() {
		item := map[string]any{
			"id":           rt.ID,
			"type":         rt.Type,
			"enabled":      rt.Enabled,
			"role":         rt.Role,
			"status":       rt.Status,
			"capabilities": rt.Capabilities.ToMap(),
		}
		if rt.Adapter != nil {
			item["credential_fields"] = credentialFields(rt.Adapter, rt.Adapter.Capabilities())
			item["adapter_profile"] = adapterProfile(rt.Adapter)
		}
		corePayload = append(corePayload, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"default_core":      defaultRT.ID,
		"cores":             corePayload,
		"capabilities":      caps.ToMap(),
		"adapter_profile":   profile,
		"credential_fields": fields,
		"limits": map[string]any{
			"global_upload_speed":         h.cfg.Limits.GlobalUploadSpeed,
			"global_download_speed":       h.cfg.Limits.GlobalDownloadSpeed,
			"default_user_upload_speed":   h.cfg.Limits.DefaultUserUploadSpeed,
			"default_user_download_speed": h.cfg.Limits.DefaultUserDownloadSpeed,
			"default_quota":               h.cfg.Limits.DefaultQuota,
			"software_limit_strikes":      h.cfg.Limits.SoftwareLimitStrikes,
		},
		"subscription": h.cfg.Subscription,
	})
}

func credentialFields(adapter port.VPNAdapter, caps domain.Capability) []port.CredentialField {
	if caps.Has(domain.CapCredentialDefs) {
		return adapter.CredentialFields()
	}
	return []port.CredentialField{}
}

func adapterProfile(adapter port.VPNAdapter) *domain.AdapterProfile {
	if provider, ok := adapter.(port.ProfileProvider); ok {
		p := provider.Profile()
		return &p
	}
	return nil
}
