// ============================================================================
// 文件说明：internal/handler/auth_handler.go
// 职责概览：实现管理员鉴权登录接口的 HTTP 处理器（AuthHandler）。
//           专门处理 POST /api/auth/login 登录路由。
//           密码匹配防御：校验前端输入的 Secret 是否与配置文件相符。
//           若相符，签发 JWT 并清除对应 IP 的错误计数；
//           若密码错误，登记该 IP 到安全防爆破中心（IPBlocker），连续输错 3 次
//           将直接封锁 IP 并调用系统防火墙实施网络阻断。
// ============================================================================

package handler

import (
	"encoding/json"
	"net/http"

	"vpnview/internal/auth"
)

// AuthHandler 专门提供管理员登录密码认证的 HTTP 校验控制器。
type AuthHandler struct {
	authSvc *auth.JWTService // 管理员 JWT 签发服务
	blocker *auth.IPBlocker  // IP 防爆破拦截中心
}

// NewAuthHandler 实例化创建一个 AuthHandler 处理器。
func NewAuthHandler(authSvc *auth.JWTService, blocker *auth.IPBlocker) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, blocker: blocker}
}

// Login 响应 POST /api/auth/login 请求。
// 控制流：
//  1. 探测客户端来路 IP 地址。
//  2. 解码请求体中的 secret。
//  3. 密钥核对：
//     - 若比对失败：呼叫 IPBlocker 登记一次暴破错误记录。如果触发 3 次阈值，该客户端 IP 将永久被防火墙包过滤丢弃，返回 401。
//     - 若比对成功：清空当前客户端 IP 的历史失败计数，调用 JWT 签发高强度 Bearer 令牌，返回 200 及过期截止时间。
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := h.blocker.GetClientIP(r) // 提取客户端 IP

	var req struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求参数 JSON 格式错误")
		return
	}

	// 核对配置文件中的 Auth Secret
	if !h.authSvc.MatchSecret(req.Secret) {
		h.blocker.RecordFailure(ip) // 暴破记录登记
		writeError(w, http.StatusUnauthorized, "管理员安全验证密钥错误，拒绝登录")
		return
	}

	// 🏆 登录成功，擦除爆破计数器历史
	h.blocker.ResetAttempts(ip)

	token, expiresAt, err := h.authSvc.Sign()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "管理员令牌 JWT 签名颁发失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expiresAt,
	})
}
