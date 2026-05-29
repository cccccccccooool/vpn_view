// ============================================================================
// 文件说明：internal/handler/subscription_handler.go
// 职责概览：实现 VPN 用户节点配置文件（订阅链接）生成的 HTTP 处理器（SubscriptionHandler）。
//           此接口（GET /api/sub/{id}）为完全公开免鉴权的，允许客户端（Clash, Shadowrocket 等）
//           通过配置的唯一用户 ID 随时随地发起请求拉取连接凭证。
//           即时 IP 身份登记（RecordUserIP）：在此接口被成功请求的瞬间，
//           主程序会自动捕获来路客户端的真实 IP 地址，并即刻在 IPUserMap 中记录该 IP 与用户 ID
//           的映射关系。这一精巧的设计瞬间打通了像 VLESS 这种在握手包中不暴露用户 ID 的协议，
//           使 Clash API 后续监控网络连接时，可以通过客户端 IP 瞬间反查出对应的 VPN 用户 ID。
// ============================================================================

package handler

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"vpnview/internal/domain"
	"vpnview/internal/service"
)

// SubscriptionHandler 响应客户端直接联络拉取订阅节点链接的 HTTP 处理器。
type SubscriptionHandler struct {
	subSvc  *service.SubscriptionService // 订阅链接配置下发服务
	userSvc *service.UserService         // 用户状态与属性验证服务
}

// NewSubscriptionHandler 实例化创建一个 SubscriptionHandler 处理器。
func NewSubscriptionHandler(subSvc *service.SubscriptionService, userSvc *service.UserService) *SubscriptionHandler {
	return &SubscriptionHandler{subSvc: subSvc, userSvc: userSvc}
}

// GetSubscription 响应 GET /api/sub/{id} 路由请求。
// 安全防御与流程：
//  1. 接收到请求，根据传入的 {id} 查询本地数据库中该 VPN 账户元数据。
//  2. 权限校验：若发现该用户不存在，或者已被管理员禁用标记为 Disabled，拒绝服务返回 403 Forbidden。
//  3. 🏆 捕获来路客户端的 IP 地址，主动触发 IPUserMap.Store，记录当前客户端公网 IP 到该用户 ID 的强绑定映射，
//     以便后续进行长连接身份识别与反查。
//  4. 委托订阅生成服务层 `subSvc.Generate` 呼叫具体适配器编译出通用客户端协议格式数据，
//     自动填补 Content-Type MIME 响应头，直接返回订阅内容字节流。
func (h *SubscriptionHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "用户订阅 ID 缺失")
		return
	}

	user, err := h.userSvc.GetUser(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// 安全防守：已被禁用关停停机的用户，剥夺拉取订阅节点的资格
	if !user.Enabled {
		writeError(w, http.StatusForbidden, "该用户账户已被系统禁用关停，拒绝拉取订阅节点")
		return
	}

	// 🏆【核心 IP 反查铺垫】获取请求订阅的客户端真实出口 IP，存入全局 IP 映射表
	clientIP := getClientIP(r)
	domain.RecordUserIP(clientIP, user.ID)

	content, mime, err := h.subSvc.Generate(r.Context(), user)
	if err != nil {
		if errors.Is(err, domain.ErrNotSupported) {
			writeDomainError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "自动拼接生成客户端订阅内容失败")
		return
	}

	// 安全兜底，确保 MIME 响应头合规
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// getClientIP 安全提取客户端的出口广域网 IP，全面防御和兼容各种反向代理级联场景。
func getClientIP(r *http.Request) string {
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
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return strings.TrimSpace(host)
}
