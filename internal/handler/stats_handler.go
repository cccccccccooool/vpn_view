// ============================================================================
// 文件说明：internal/handler/stats_handler.go
// 职责概览：实现系统全局监控与活动连接控制的 HTTP 接口处理器（StatsHandler）。
//           提供大屏看板所需的整机运行汇总统计（GetGlobalStats），
//           查询当前代理底层的活动网络 TCP/UDP 连接明细（GetConnections），
//           以及强制踢除阻断特定异常网络活动连接的控制 API（KillConnection）。
// ============================================================================

package handler

import (
	"errors"
	"net/http"

	"vpnview/internal/domain"
	"vpnview/internal/service"
)

// StatsHandler 负责处理全局数据看板监控与底层网络活动连接的管控动作。
type StatsHandler struct {
	trafficSvc *service.TrafficService // 结合流量定时轮询器进行速度和连接调度
}

// NewStatsHandler 实例化创建一个 StatsHandler 处理器。
func NewStatsHandler(trafficSvc *service.TrafficService) *StatsHandler {
	return &StatsHandler{trafficSvc: trafficSvc}
}

// GetGlobalStats 响应 GET /api/stats/global 路由请求。
// 吐出全局累计流量、瞬时总网速吞吐、在线活跃连接数等全局大看板数据 JSON。
func (h *StatsHandler) GetGlobalStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.trafficSvc.GetGlobalStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取全局监控大屏数据失败")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// GetConnections 响应 GET /api/stats/connections 路由请求。
// 抓取并返回当前所有维持中的活动 TCP/UDP 连接列表。
// 如果加载的具体 VPN 适配器声明无此查询能力，统一自动返回 501 Not Implemented 降级。
func (h *StatsHandler) GetConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.trafficSvc.GetActiveConnections(r.Context())
	if err != nil {
		if errors.Is(err, domain.ErrNotSupported) {
			writeDomainError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "获取当前活动网络连接列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

// KillConnection 响应 DELETE /api/stats/connections/{id} 强制网络连接阻断接口。
func (h *StatsHandler) KillConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "活动连接 ID 参数缺失")
		return
	}
	if err := h.trafficSvc.KillConnection(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
