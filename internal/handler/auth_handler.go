// auth_handler.go 实现管理面板登录认证的 HTTP 处理器。
package handler

import (
	"encoding/json"
	"net/http"

	"vpnview/internal/auth"
)

// AuthHandler 处理管理面板的登录认证请求。
type AuthHandler struct {
	authSvc *auth.JWTService
	blocker *auth.IPBlocker
}

// NewAuthHandler 创建 AuthHandler 实例。
func NewAuthHandler(authSvc *auth.JWTService, blocker *auth.IPBlocker) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, blocker: blocker}
}

// Login 处理 POST /api/auth/login 请求。
// 校验请求体中的 secret 密钥，成功则返回 JWT token 和过期时间；
// 失败则记录该 IP 的失败次数，连续失败过多会被 IPBlocker 封禁。
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := h.blocker.GetClientIP(r)

	var req struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.authSvc.MatchSecret(req.Secret) {
		h.blocker.RecordFailure(ip)
		writeError(w, http.StatusUnauthorized, "invalid secret")
		return
	}

	// 验证成功，清除当前 IP 的登录失败尝试历史
	h.blocker.ResetAttempts(ip)

	token, expiresAt, err := h.authSvc.Sign()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expiresAt,
	})
}
