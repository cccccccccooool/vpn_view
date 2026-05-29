// ============================================================================
// 文件说明：internal/handler/capability_handler.go
// 职责概览：实现系统与适配器能力查询的 HTTP 接口处理器（CapabilityHandler）。
//           系统在启动时，前端会优先访问此接口（GET /api/capabilities）。
//           获取当前适配器所支持的全部能力字典映射、适配器特征属性描述、
//           以及创建用户时所需的凭据动态表单定义、限速配额全局配置。
//           前端以此动态开闭面板按钮，动态控制用户凭据输入表单的渲染。
// ============================================================================

package handler

import (
	"net/http"

	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// CapabilityHandler 用于对外暴露面板运行时能力参数与前端表单约束条件。
type CapabilityHandler struct {
	adapter port.VPNAdapter // VPN 内核适配器
	cfg     *config.Config  // 全局 YAML 配置，用于获取 limits 和 subscription 默认值
}

// NewCapabilityHandler 实例化 CapabilityHandler 处理器。
func NewCapabilityHandler(adapter port.VPNAdapter, cfg *config.Config) *CapabilityHandler {
	return &CapabilityHandler{adapter: adapter, cfg: cfg}
}

// GetCapabilities 响应 GET /api/capabilities 路由请求。
// 流程：
//  1. 取得 VPN 适配器拥有的 Capabilities 位掩码，平铺为 map 映射。
//  2. 若适配器支持自定义表单协议（CapCredentialDefs），调用其 CredentialFields()，返回表单字段数组。
//  3. 检测并提取适配器的特征 Profile（Config 模式、Reload 命令、流量维度等），供前端高阶渲染。
//  4. 整合全局系统 Limits 速率设置和默认 Subscription 订阅模板属性一并返回。
func (h *CapabilityHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	caps := h.adapter.Capabilities()
	fields := []port.CredentialField{}

	// 如果适配器声明了能够返回具体凭据表单字段的定义
	if caps.Has(domain.CapCredentialDefs) {
		fields = h.adapter.CredentialFields()
	}

	// 动态检测适配器是否实现 ProfileProvider 高级特性描述接口
	var profile *domain.AdapterProfile
	if provider, ok := h.adapter.(port.ProfileProvider); ok {
		p := provider.Profile()
		profile = &p
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"capabilities":      caps.ToMap(),          // 代理底层能力分布 map
		"adapter_profile":   profile,               // 适配器运行模式 Profile 特征
		"credential_fields": fields,                // 动态凭据渲染表单模型数据
		"limits": map[string]any{                   // 全局参数与超速 Strikes 限制
			"global_upload_speed":         h.cfg.Limits.GlobalUploadSpeed,
			"global_download_speed":       h.cfg.Limits.GlobalDownloadSpeed,
			"default_user_upload_speed":   h.cfg.Limits.DefaultUserUploadSpeed,
			"default_user_download_speed": h.cfg.Limits.DefaultUserDownloadSpeed,
			"default_quota":               h.cfg.Limits.DefaultQuota,
			"software_limit_strikes":      h.cfg.Limits.SoftwareLimitStrikes,
		},
		"subscription": h.cfg.Subscription, // 订阅的基础配置映射
	})
}
