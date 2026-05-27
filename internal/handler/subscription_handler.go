// subscription_handler.go 实现订阅链接生成的 HTTP 处理器。
package handler

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"vpnview/internal/domain"
	"vpnview/internal/service"
)

// SubscriptionHandler 处理客户端订阅链接的生成请求。
type SubscriptionHandler struct {
	subSvc  *service.SubscriptionService
	userSvc *service.UserService
}

// NewSubscriptionHandler 创建 SubscriptionHandler 实例。
func NewSubscriptionHandler(subSvc *service.SubscriptionService, userSvc *service.UserService) *SubscriptionHandler {
	return &SubscriptionHandler{subSvc: subSvc, userSvc: userSvc}
}

// GetSubscription 处理 GET /api/sub/{id} 请求，为指定用户生成订阅内容。
// 该接口无需 JWT 鉴权。如果用户已禁用则返回 403，
// adapter 不支持订阅生成时返回 501。
func (h *SubscriptionHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing user id")
		return
	}

	user, err := h.userSvc.GetUser(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !user.Enabled {
		writeError(w, http.StatusForbidden, "user disabled")
		return
	}

	// 🏆 捕获客户端 IP 并记录映射，用于在 Clash API 连接中动态识别用户
	clientIP := getClientIP(r)
	domain.RecordUserIP(clientIP, user.ID)

	content, mime, err := h.subSvc.Generate(r.Context(), user)
	if err != nil {
		if errors.Is(err, domain.ErrNotSupported) {
			writeDomainError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to generate subscription")
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// getClientIP 提取客户端请求的真实 IP 地址，支持常见的反向代理头部。
func getClientIP(r *http.Request) string {
	// 优先获取代理头中的真实 IP
	for _, h := range []string{"X-Forwarded-For", "X-Real-IP"} {
		if addresses := r.Header.Values(h); len(addresses) > 0 {
			for _, address := range addresses {
				for _, ip := range strings.Split(address, ",") {
					ip = strings.TrimSpace(ip)
					if ip != "" {
						return ip
					}
				}
			}
		}
	}
	// 回退到 RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return strings.TrimSpace(host)
}
