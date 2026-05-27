// stats_handler.go 实现流量统计和连接管理相关的 HTTP 处理器。
package handler

import (
	"errors"
	"net/http"

	"vpnview/internal/domain"
	"vpnview/internal/service"
)

// StatsHandler 处理全局流量统计和活跃连接管理的 HTTP 请求。
type StatsHandler struct {
	trafficSvc *service.TrafficService
}

// NewStatsHandler 创建 StatsHandler 实例。
func NewStatsHandler(trafficSvc *service.TrafficService) *StatsHandler {
	return &StatsHandler{trafficSvc: trafficSvc}
}

// GetGlobalStats 处理 GET /api/stats/global 请求，返回系统全局流量统计数据。
func (h *StatsHandler) GetGlobalStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.trafficSvc.GetGlobalStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// GetConnections 处理 GET /api/stats/connections 请求，返回当前活跃连接列表。
// 若 adapter 不支持该能力，返回 501 Not Implemented。
func (h *StatsHandler) GetConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.trafficSvc.GetActiveConnections(r.Context())
	if err != nil {
		if errors.Is(err, domain.ErrNotSupported) {
			writeDomainError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

// KillConnection 处理 DELETE /api/stats/connections/{id} 请求，强制断开指定连接。
func (h *StatsHandler) KillConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing connection id")
		return
	}
	if err := h.trafficSvc.KillConnection(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
