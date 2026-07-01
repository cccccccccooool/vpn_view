package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

type TrafficService struct {
	store    port.UserStore
	cores    *core.Manager
	interval time.Duration

	mu            sync.RWMutex
	speeds        map[string][2]int64
	lastTraffic   map[string][2]int64
	lastGlobalUp  int64
	lastGlobalDn  int64
	lastPollError error
}

func NewTrafficService(store port.UserStore, cores *core.Manager, interval time.Duration) *TrafficService {
	return &TrafficService{
		store:       store,
		cores:       cores,
		interval:    interval,
		speeds:      make(map[string][2]int64),
		lastTraffic: make(map[string][2]int64),
	}
}

func (s *TrafficService) Start(ctx context.Context) {
	if !s.anyTrafficProvider() {
		slog.Info("no enabled core supports traffic queries; traffic polling disabled")
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

func (s *TrafficService) PollOnce(ctx context.Context) {
	if s.anyTrafficProvider() {
		s.pollTraffic(ctx)
	}
}

func (s *TrafficService) GetUserSpeeds() map[string][2]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][2]int64, len(s.speeds))
	for k, v := range s.speeds {
		out[k] = v
	}
	return out
}

func (s *TrafficService) GetGlobalStats(ctx context.Context) (Stats, error) {
	upload, download, err := s.store.GetTotalTraffic(ctx)
	if err != nil {
		return Stats{}, err
	}
	users, err := s.store.List(ctx)
	if err != nil {
		return Stats{}, err
	}

	defaultAdapter := s.cores.Default()
	stats := Stats{
		TotalUpload:    upload,
		TotalDownload:  download,
		ActiveUsers:    countEnabled(users),
		Capabilities:   defaultAdapter.Capabilities().ToMap(),
		RealtimeNative: defaultAdapter.Capabilities().Has(domain.CapRealtimeSpeed),
	}
	s.mu.RLock()
	stats.LastPollError = ""
	if s.lastPollError != nil {
		stats.LastPollError = s.lastPollError.Error()
	}
	stats.SpeedUp = s.lastGlobalUp
	stats.SpeedDown = s.lastGlobalDn
	s.mu.RUnlock()

	if up, down, ok := s.nativeGlobalSpeed(ctx); ok {
		stats.SpeedUp = up
		stats.SpeedDown = down
		stats.RealtimeNative = true
	}
	if conns, err := s.GetActiveConnections(ctx); err == nil {
		stats.ActiveConnections = len(conns)
	}
	return stats, nil
}

func (s *TrafficService) GetActiveConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	var out []port.ActiveConnection
	supported := false
	for _, rt := range s.cores.List() {
		if !rt.Enabled || rt.Adapter == nil || !rt.Adapter.Capabilities().Has(domain.CapActiveConns) {
			continue
		}
		connProvider, ok := rt.Adapter.(port.ConnectionProvider)
		if !ok {
			continue
		}
		supported = true
		conns, err := connProvider.GetActiveConnections(ctx)
		if err != nil {
			slog.Warn("failed to list active connections", "core_id", rt.ID, "err", err)
			continue
		}
		out = append(out, conns...)
	}
	if !supported {
		return nil, domain.ErrNotSupported
	}
	return out, nil
}

func (s *TrafficService) KillConnection(ctx context.Context, connID string) error {
	for _, rt := range s.cores.List() {
		if !rt.Enabled || rt.Adapter == nil || !rt.Adapter.Capabilities().Has(domain.CapKillConn) {
			continue
		}
		connProvider, ok := rt.Adapter.(port.ConnectionProvider)
		if !ok {
			continue
		}
		if err := connProvider.KillConnection(ctx, connID); err == nil {
			return nil
		}
	}
	return domain.ErrNotSupported
}

func (s *TrafficService) pollTraffic(ctx context.Context) {
	intervalSecs := int64(s.interval.Seconds())
	if intervalSecs <= 0 {
		intervalSecs = 1
	}

	nextSpeeds := map[string][2]int64{}
	var globalUp, globalDown int64
	var lastErr error

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, rt := range s.cores.List() {
		if !rt.Enabled || rt.Adapter == nil || !rt.Adapter.Capabilities().Has(domain.CapQueryTraffic) {
			continue
		}
		trafficProvider, ok := rt.Adapter.(port.TrafficProvider)
		if !ok {
			continue
		}
		snaps, err := trafficProvider.QueryTraffic(ctx)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", rt.ID, err)
			slog.Error("failed to query core traffic", "core_id", rt.ID, "err", err)
			continue
		}
		for _, snap := range snaps {
			key := rt.ID + "/" + snap.UserID
			prev, exists := s.lastTraffic[key]
			if !exists {
				s.lastTraffic[key] = [2]int64{snap.Upload, snap.Download}
				continue
			}
			deltaUp := snap.Upload - prev[0]
			deltaDown := snap.Download - prev[1]
			if deltaUp < 0 {
				deltaUp = 0
			}
			if deltaDown < 0 {
				deltaDown = 0
			}
			s.lastTraffic[key] = [2]int64{snap.Upload, snap.Download}
			if deltaUp != 0 || deltaDown != 0 {
				if err := s.store.AddTraffic(ctx, snap.UserID, deltaUp, deltaDown); err != nil {
					slog.Warn("failed to persist traffic delta", "core_id", rt.ID, "user", snap.UserID, "err", err)
				}
			}
			up := deltaUp / intervalSecs
			down := deltaDown / intervalSecs
			nextSpeeds[snap.UserID] = [2]int64{up, down}
			globalUp += up
			globalDown += down
		}
	}

	s.speeds = nextSpeeds
	s.lastGlobalUp = globalUp
	s.lastGlobalDn = globalDown
	s.lastPollError = lastErr
}

func (s *TrafficService) anyTrafficProvider() bool {
	for _, rt := range s.cores.List() {
		if rt.Enabled && rt.Adapter != nil && rt.Adapter.Capabilities().Has(domain.CapQueryTraffic) {
			if _, ok := rt.Adapter.(port.TrafficProvider); ok {
				return true
			}
		}
	}
	return false
}

func (s *TrafficService) nativeGlobalSpeed(ctx context.Context) (int64, int64, bool) {
	var up, down int64
	okAny := false
	for _, rt := range s.cores.List() {
		if !rt.Enabled || rt.Adapter == nil || !rt.Adapter.Capabilities().Has(domain.CapRealtimeSpeed) {
			continue
		}
		speedProvider, ok := rt.Adapter.(port.GlobalSpeedProvider)
		if !ok {
			continue
		}
		speed, err := speedProvider.GetGlobalSpeed(ctx)
		if err != nil || speed == nil {
			slog.Warn("failed to get native global speed", "core_id", rt.ID, "err", err)
			continue
		}
		up += speed.Up
		down += speed.Down
		okAny = true
	}
	return up, down, okAny
}

type Stats struct {
	SpeedUp           int64           `json:"speed_up"`
	SpeedDown         int64           `json:"speed_down"`
	TotalUpload       int64           `json:"total_upload"`
	TotalDownload     int64           `json:"total_download"`
	ActiveUsers       int             `json:"active_users"`
	ActiveConnections int             `json:"active_connections"`
	RealtimeNative    bool            `json:"realtime_native"`
	LastPollError     string          `json:"last_poll_error,omitempty"`
	Capabilities      map[string]bool `json:"capabilities"`
}

func countEnabled(users []*domain.User) int {
	total := 0
	for _, user := range users {
		if user.Enabled {
			total++
		}
	}
	return total
}
