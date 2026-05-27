// traffic_service.go 实现流量轮询、速率计算与全局统计功能。

package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// TrafficService 负责定时从 VPN 适配器拉取流量数据，计算每用户的实时速率，
// 并将增量流量持久化到存储中。它同时提供全局统计信息的查询接口。
type TrafficService struct {
	store    port.UserStore
	adapter  port.VPNAdapter
	interval time.Duration

	mu            sync.RWMutex
	speeds        map[string][2]int64 // 各用户当前速率 [upload, download]（字节/秒）
	lastTraffic   map[string][2]int64 // 上一次轮询时各用户的累积流量，用于计算增量
	lastGlobalUp  int64               // 基于轮询计算的全局上行速率（字节/秒）
	lastGlobalDn  int64               // 基于轮询计算的全局下行速率（字节/秒）
	lastPollError error               // 最近一次轮询的错误
}

// NewTrafficService 创建并返回一个新的 TrafficService 实例。
// interval 指定流量轮询的时间间隔。
func NewTrafficService(store port.UserStore, adapter port.VPNAdapter, interval time.Duration) *TrafficService {
	return &TrafficService{
		store:       store,
		adapter:     adapter,
		interval:    interval,
		speeds:      make(map[string][2]int64),
		lastTraffic: make(map[string][2]int64),
	}
}

// Start 启动流量轮询的后台循环。
// 若适配器不支持 CapQueryTraffic 能力，则不会启动轮询。
// 该方法会阻塞直到 ctx 被取消。
func (s *TrafficService) Start(ctx context.Context) {
	if !s.adapter.Capabilities().Has(domain.CapQueryTraffic) {
		slog.Info("adapter does not support traffic query; traffic polling disabled")
		return
	}

	s.pollTraffic(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollTraffic(ctx)
		}
	}
}

// PollOnce immediately samples adapter traffic and persists the calculated deltas.
func (s *TrafficService) PollOnce(ctx context.Context) {
	if !s.adapter.Capabilities().Has(domain.CapQueryTraffic) {
		return
	}
	s.pollTraffic(ctx)
}

// GetUserSpeeds 返回各用户当前的实时速率快照。
// 返回值的 key 为用户 ID，value 为 [上行速率, 下行速率]（字节/秒）。
func (s *TrafficService) GetUserSpeeds() map[string][2]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][2]int64, len(s.speeds))
	for k, v := range s.speeds {
		out[k] = v
	}
	return out
}

// GetGlobalStats 汇总并返回全局流量统计信息，包括总流量、活跃用户数、
// 实时速率和活跃连接数。速率数据优先使用适配器原生接口，否则回退到轮询计算值。
func (s *TrafficService) GetGlobalStats(ctx context.Context) (Stats, error) {
	upload, download, err := s.store.GetTotalTraffic(ctx)
	if err != nil {
		return Stats{}, err
	}

	users, err := s.store.List(ctx)
	if err != nil {
		return Stats{}, err
	}

	stats := Stats{
		TotalUpload:    upload,
		TotalDownload:  download,
		ActiveUsers:    countEnabled(users),
		Capabilities:   s.adapter.Capabilities().ToMap(),
		RealtimeNative: s.adapter.Capabilities().Has(domain.CapRealtimeSpeed),
	}
	s.mu.RLock()
	if s.lastPollError != nil {
		stats.LastPollError = s.lastPollError.Error()
	}
	s.mu.RUnlock()

	if s.adapter.Capabilities().Has(domain.CapRealtimeSpeed) {
		speed, err := s.adapter.GetGlobalSpeed(ctx)
		if err == nil && speed != nil {
			stats.SpeedUp = speed.Up
			stats.SpeedDown = speed.Down
		} else if err != nil {
			slog.Warn("failed to get native global speed", "err", err)
		}
	} else {
		s.mu.RLock()
		stats.SpeedUp = s.lastGlobalUp
		stats.SpeedDown = s.lastGlobalDn
		s.mu.RUnlock()
	}

	if s.adapter.Capabilities().Has(domain.CapActiveConns) {
		if conns, err := s.adapter.GetActiveConnections(ctx); err == nil {
			stats.ActiveConnections = len(conns)
		}
	}

	return stats, nil
}

// GetActiveConnections 返回当前所有活跃的 VPN 连接列表。
// 若适配器不支持 CapActiveConns 能力，返回 domain.ErrNotSupported。
func (s *TrafficService) GetActiveConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	if !s.adapter.Capabilities().Has(domain.CapActiveConns) {
		return nil, domain.ErrNotSupported
	}
	conns, err := s.adapter.GetActiveConnections(ctx)
	if err != nil {
		slog.Warn("failed to get active connections from adapter; returning empty list", "err", err)
		return []port.ActiveConnection{}, nil
	}
	return conns, nil
}

// KillConnection 强制断开指定的 VPN 连接。
// 若适配器不支持 CapKillConn 能力，返回 domain.ErrNotSupported。
func (s *TrafficService) KillConnection(ctx context.Context, connID string) error {
	if !s.adapter.Capabilities().Has(domain.CapKillConn) {
		return domain.ErrNotSupported
	}
	return s.adapter.KillConnection(ctx, connID)
}

// pollTraffic 从适配器拉取累计流量快照，并与上一轮基准做差得到增量。
func (s *TrafficService) pollTraffic(ctx context.Context) {
	snaps, err := s.adapter.QueryTraffic(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastPollError = err
		s.mu.Unlock()
		slog.Error("failed to query traffic", "err", err)
		return
	}

	intervalSecs := int64(s.interval.Seconds())
	if intervalSecs <= 0 {
		intervalSecs = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nextSpeeds := make(map[string][2]int64, len(snaps))
	var globalUp, globalDown int64

	for _, snap := range snaps {
		// 取上一次轮询时该用户的累积流量值
		prev, exists := s.lastTraffic[snap.UserID]
		if !exists {
			// 首次看到该面板用户，初始化其流量基准值，防止系统重启时将历史累积流量误判为增量（防止数据激增）
			s.lastTraffic[snap.UserID] = [2]int64{snap.Upload, snap.Download}
			continue
		}

		// 计算增量（当前累积值 - 上次累积值）
		deltaUp := snap.Upload - prev[0]
		deltaDown := snap.Download - prev[1]

		// 安全守卫：如果增量为负值（连接已关闭/重置），将其归零
		if deltaUp < 0 {
			deltaUp = 0
		}
		if deltaDown < 0 {
			deltaDown = 0
		}

		// 记录当前累积值，供下一次轮询计算差值
		s.lastTraffic[snap.UserID] = [2]int64{snap.Upload, snap.Download}

		// 将增量流量写入持久化数据库
		if deltaUp != 0 || deltaDown != 0 {
			if err := s.store.AddTraffic(ctx, snap.UserID, deltaUp, deltaDown); err != nil {
				slog.Warn("failed to add traffic", "user", snap.UserID, "err", err)
			}
		}

		// 计算每秒速率
		up := deltaUp / intervalSecs
		down := deltaDown / intervalSecs
		nextSpeeds[snap.UserID] = [2]int64{up, down}
		globalUp += up
		globalDown += down
	}

	s.speeds = nextSpeeds
	s.lastGlobalUp = globalUp
	s.lastGlobalDn = globalDown
	s.lastPollError = nil
}

// Stats 聚合了面板首页所需的全局统计数据。
type Stats struct {
	SpeedUp           int64           `json:"speed_up"`                  // 全局上行速率（字节/秒）
	SpeedDown         int64           `json:"speed_down"`                // 全局下行速率（字节/秒）
	TotalUpload       int64           `json:"total_upload"`              // 累计上行流量（字节）
	TotalDownload     int64           `json:"total_download"`            // 累计下行流量（字节）
	ActiveUsers       int             `json:"active_users"`              // 当前启用的用户数
	ActiveConnections int             `json:"active_connections"`        // 当前活跃连接数
	RealtimeNative    bool            `json:"realtime_native"`           // 适配器是否原生支持实时速率
	LastPollError     string          `json:"last_poll_error,omitempty"` // 最近一次流量轮询错误
	Capabilities      map[string]bool `json:"capabilities"`              // 适配器能力集合
}

// countEnabled 统计用户列表中处于启用状态的用户数量。
func countEnabled(users []*domain.User) int {
	total := 0
	for _, user := range users {
		if user.Enabled {
			total++
		}
	}
	return total
}
