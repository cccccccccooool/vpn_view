// lifecycle_service.go 实现用户生命周期管理，定时检查用户的过期、配额超限和速率违规，
// 并在满足条件时自动禁用用户。

package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// LifecycleService 负责周期性地检查所有用户的生命周期状态。
// 包括：过期自动禁用、流量配额超限禁用、软件限速违规累计 strike 后禁用。
type LifecycleService struct {
	store      port.UserStore
	adapter    port.VPNAdapter
	limits     config.LimitsConfig
	trafficSvc *TrafficService
	interval   time.Duration

	mu           sync.Mutex
	speedStrikes map[string]int // 各用户的软件限速违规计数（连续超速次数）
}

// NewLifecycleService 创建并返回一个新的 LifecycleService 实例。
// interval 指定检查周期；trafficSvc 用于获取各用户的实时速率。
func NewLifecycleService(store port.UserStore, adapter port.VPNAdapter, limits config.LimitsConfig, trafficSvc *TrafficService, interval time.Duration) *LifecycleService {
	return &LifecycleService{
		store:        store,
		adapter:      adapter,
		limits:       limits,
		trafficSvc:   trafficSvc,
		interval:     interval,
		speedStrikes: make(map[string]int),
	}
}

// Start 启动生命周期检查的后台循环。该方法会阻塞直到 ctx 被取消。
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

// checkAll 遍历所有启用中的用户，依次检查过期、配额超限和速率违规。
func (s *LifecycleService) checkAll(ctx context.Context) {
	users, err := s.store.List(ctx)
	if err != nil {
		slog.Warn("lifecycle list users failed", "err", err)
		return
	}

	caps := s.adapter.Capabilities()
	now := time.Now()
	speeds := s.trafficSvc.GetUserSpeeds()

	for _, user := range users {
		if !user.Enabled {
			continue
		}

		if user.ExpireAt != nil && now.After(*user.ExpireAt) {
			slog.Info("disabling expired user", "id", user.ID)
			s.disableUser(ctx, user, domain.ErrExpired)
			continue
		}

		if caps.Has(domain.CapQueryTraffic) && user.Quota > 0 && user.Upload+user.Download >= user.Quota {
			slog.Info("disabling user over quota", "id", user.ID)
			s.disableUser(ctx, user, domain.ErrQuotaExceeded)
			continue
		}

		if !caps.Has(domain.CapSpeedLimit) && s.exceedsSoftwareUserLimit(user, speeds[user.ID]) {
			s.recordSpeedStrike(ctx, user)
			continue
		}
		s.clearSpeedStrike(user.ID)
	}

	if !caps.Has(domain.CapGlobalSpeedLimit) {
		s.logGlobalSoftwareLimit(speeds)
	}
}

// exceedsSoftwareUserLimit 判断用户当前速率是否超过了其设定的软件限速阈值。
// 当适配器不支持原生限速（CapSpeedLimit）时使用此方法进行软件层面的检测。
func (s *LifecycleService) exceedsSoftwareUserLimit(user *domain.User, speed [2]int64) bool {
	return (user.SpeedLimitUp > 0 && speed[0] > user.SpeedLimitUp) ||
		(user.SpeedLimitDown > 0 && speed[1] > user.SpeedLimitDown)
}

// recordSpeedStrike 记录一次速率违规。当连续违规次数达到配置阈值时，自动禁用该用户。
func (s *LifecycleService) recordSpeedStrike(ctx context.Context, user *domain.User) {
	s.mu.Lock()
	s.speedStrikes[user.ID]++
	strikes := s.speedStrikes[user.ID]
	s.mu.Unlock()

	threshold := s.limits.SoftwareLimitStrikes
	if threshold <= 0 {
		threshold = 3
	}
	if strikes >= threshold {
		slog.Info("disabling user over software speed limit", "id", user.ID, "strikes", strikes)
		s.disableUser(ctx, user, domain.ErrSpeedLimitExceeded)
	}
}

// clearSpeedStrike 清除指定用户的速率违规计数（用户速率恢复正常或已被禁用时调用）。
func (s *LifecycleService) clearSpeedStrike(userID string) {
	s.mu.Lock()
	delete(s.speedStrikes, userID)
	s.mu.Unlock()
}

// logGlobalSoftwareLimit 检查全局聚合速率是否超过配置的全局限速阈值，超限时输出警告日志。
// 仅在适配器不支持原生全局限速（CapGlobalSpeedLimit）时被调用。
func (s *LifecycleService) logGlobalSoftwareLimit(speeds map[string][2]int64) {
	var up, down int64
	for _, speed := range speeds {
		up += speed[0]
		down += speed[1]
	}
	if s.limits.GlobalUploadSpeed > 0 && up > s.limits.GlobalUploadSpeed {
		slog.Warn("global upload speed exceeds software limit", "speed", up, "limit", s.limits.GlobalUploadSpeed)
	}
	if s.limits.GlobalDownloadSpeed > 0 && down > s.limits.GlobalDownloadSpeed {
		slog.Warn("global download speed exceeds software limit", "speed", down, "limit", s.limits.GlobalDownloadSpeed)
	}
}

// disableUser 禁用指定用户。优先使用适配器的 DisableUser 能力，
// 如不支持则回退到 RemoveUser。最后将用户状态持久化到存储并清除 strike 记录。
func (s *LifecycleService) disableUser(ctx context.Context, user *domain.User, reason error) {
	caps := s.adapter.Capabilities()
	if caps.Has(domain.CapDisableUser) {
		if err := s.adapter.DisableUser(ctx, user.ID); err != nil {
			slog.Warn("adapter disable failed", "id", user.ID, "err", err)
		}
	} else if caps.Has(domain.CapRemoveUser) {
		if err := s.adapter.RemoveUser(ctx, user.ID); err != nil {
			slog.Warn("adapter remove fallback failed", "id", user.ID, "err", err)
		}
	} else {
		slog.Warn("adapter cannot disable or remove user; store flag only", "id", user.ID)
	}

	user.Enabled = false
	if err := s.store.Update(ctx, user); err != nil {
		slog.Warn("failed to persist disabled user", "id", user.ID, "reason", reason, "err", err)
	}
	s.clearSpeedStrike(user.ID)
}
