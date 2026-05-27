// capability_handler.go 实现 VPN adapter 能力查询的 HTTP 处理器。
package handler

import (
	"net/http"

	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// CapabilityHandler 处理 VPN adapter 能力及系统配置的查询请求。
type CapabilityHandler struct {
	adapter port.VPNAdapter
	cfg     *config.Config
}

// NewCapabilityHandler 创建 CapabilityHandler 实例。
func NewCapabilityHandler(adapter port.VPNAdapter, cfg *config.Config) *CapabilityHandler {
	return &CapabilityHandler{adapter: adapter, cfg: cfg}
}

// GetCapabilities 处理 GET /api/capabilities 请求。
// 返回当前 adapter 支持的能力集合、凭据字段定义、限速/配额的默认配置以及订阅配置。
func (h *CapabilityHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	caps := h.adapter.Capabilities()
	fields := []port.CredentialField{}
	if caps.Has(domain.CapCredentialDefs) {
		fields = h.adapter.CredentialFields()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"capabilities":      caps.ToMap(),
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
