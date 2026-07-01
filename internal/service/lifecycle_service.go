package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

type LifecycleService struct {
	store      port.UserStore
	cores      *core.Manager
	limits     config.LimitsConfig
	trafficSvc *TrafficService
	interval   time.Duration

	mu           sync.Mutex
	speedStrikes map[string]int
}

func NewLifecycleService(store port.UserStore, cores *core.Manager, limits config.LimitsConfig, trafficSvc *TrafficService, interval time.Duration) *LifecycleService {
	return &LifecycleService{
		store:        store,
		cores:        cores,
		limits:       limits,
		trafficSvc:   trafficSvc,
		interval:     interval,
		speedStrikes: make(map[string]int),
	}
}

func (s *LifecycleService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.checkAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAll(ctx)
		}
	}
}

func (s *LifecycleService) checkAll(ctx context.Context) {
	users, err := s.store.List(ctx)
	if err != nil {
		slog.Warn("failed to scan users for lifecycle checks", "err", err)
		return
	}

	now := time.Now()
	speeds := s.trafficSvc.GetUserSpeeds()
	for _, user := range users {
		if !user.Enabled {
			continue
		}
		if user.ExpireAt != nil && now.After(*user.ExpireAt) {
			slog.Info("user expired; disabling", "id", user.ID)
			s.disableUser(ctx, user, domain.ErrExpired)
			continue
		}
		if user.Quota > 0 && user.Upload+user.Download >= user.Quota {
			slog.Info("user quota exceeded; disabling", "id", user.ID)
			s.disableUser(ctx, user, domain.ErrQuotaExceeded)
			continue
		}
		if speed, ok := speeds[user.ID]; ok && s.exceedsSoftwareUserLimit(user, speed) {
			s.addSpeedStrike(ctx, user)
			continue
		}
		s.clearSpeedStrike(user.ID)
	}

	defaultCaps := s.cores.Default().Capabilities()
	if !defaultCaps.Has(domain.CapGlobalSpeedLimit) {
		s.logGlobalSoftwareLimit(speeds)
	}
}

func (s *LifecycleService) exceedsSoftwareUserLimit(user *domain.User, speed [2]int64) bool {
	return (user.SpeedLimitUp > 0 && speed[0] > user.SpeedLimitUp) ||
		(user.SpeedLimitDown > 0 && speed[1] > user.SpeedLimitDown)
}

func (s *LifecycleService) addSpeedStrike(ctx context.Context, user *domain.User) {
	s.mu.Lock()
	s.speedStrikes[user.ID]++
	strikes := s.speedStrikes[user.ID]
	s.mu.Unlock()
	if s.limits.SoftwareLimitStrikes > 0 && strikes >= s.limits.SoftwareLimitStrikes {
		slog.Info("user exceeded software speed limit repeatedly; disabling", "id", user.ID, "strikes", strikes)
		s.disableUser(ctx, user, domain.ErrSpeedLimitExceeded)
	}
}

func (s *LifecycleService) clearSpeedStrike(userID string) {
	s.mu.Lock()
	delete(s.speedStrikes, userID)
	s.mu.Unlock()
}

func (s *LifecycleService) logGlobalSoftwareLimit(speeds map[string][2]int64) {
	var up, down int64
	for _, speed := range speeds {
		up += speed[0]
		down += speed[1]
	}
	if s.limits.GlobalUploadSpeed > 0 && up > s.limits.GlobalUploadSpeed {
		slog.Warn("global upload speed exceeds software warning limit", "current", up, "limit", s.limits.GlobalUploadSpeed)
	}
	if s.limits.GlobalDownloadSpeed > 0 && down > s.limits.GlobalDownloadSpeed {
		slog.Warn("global download speed exceeds software warning limit", "current", down, "limit", s.limits.GlobalDownloadSpeed)
	}
}

func (s *LifecycleService) disableUser(ctx context.Context, user *domain.User, reason error) {
	adapter, rt, err := s.cores.SelectForUser(user)
	if err != nil {
		slog.Warn("failed to select user core for lifecycle disable", "id", user.ID, "err", err)
		return
	}
	caps := adapter.Capabilities()
	if caps.Has(domain.CapDisableUser) {
		if stateManager, ok := adapter.(port.UserStateManager); ok {
			if err := stateManager.DisableUser(ctx, user.ID); err != nil {
				slog.Warn("core failed to disable user", "core_id", rt.ID, "id", user.ID, "err", err)
			}
		}
	} else if caps.Has(domain.CapRemoveUser) {
		if err := adapter.RemoveUser(ctx, user.ID); err != nil {
			slog.Warn("core failed to remove user during lifecycle disable", "core_id", rt.ID, "id", user.ID, "err", err)
		}
	} else {
		slog.Warn("core does not support account disable; state is local only", "core_id", rt.ID, "id", user.ID)
	}

	user.Enabled = false
	if user.CoreID == "" {
		user.CoreID = rt.ID
	}
	if user.AdapterType == "" {
		user.AdapterType = rt.Type
	}
	if err := s.store.Update(ctx, user); err != nil {
		slog.Warn("failed to persist lifecycle disable", "id", user.ID, "reason", reason, "err", err)
	}
	s.clearSpeedStrike(user.ID)
}
