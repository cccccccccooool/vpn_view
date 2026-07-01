package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

type UserService struct {
	store      port.UserStore
	cores      *core.Manager
	limits     config.LimitsConfig
	trafficSvc *TrafficService
}

func NewUserService(store port.UserStore, cores *core.Manager, limits config.LimitsConfig) *UserService {
	return &UserService{store: store, cores: cores, limits: limits}
}

func (s *UserService) AttachTrafficService(trafficSvc *TrafficService) {
	s.trafficSvc = trafficSvc
}

func (s *UserService) CreateUser(ctx context.Context, id, name, coreID string, creds map[string]string, quota, speedUp, speedDown int64, expireAt *time.Time) error {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	coreID = strings.TrimSpace(coreID)
	if id == "" {
		return fmt.Errorf("user id cannot be empty")
	}
	if name == "" {
		name = id
	}
	if creds == nil {
		creds = map[string]string{}
	}
	if quota == 0 {
		quota = s.limits.DefaultQuota
	}
	if speedUp == 0 {
		speedUp = s.limits.DefaultUserUploadSpeed
	}
	if speedDown == 0 {
		speedDown = s.limits.DefaultUserDownloadSpeed
	}

	user := &domain.User{
		ID:             id,
		Name:           name,
		CoreID:         coreID,
		Credentials:    creds,
		Quota:          quota,
		SpeedLimitUp:   speedUp,
		SpeedLimitDown: speedDown,
		ExpireAt:       expireAt,
		Enabled:        true,
		CreatedAt:      time.Now(),
	}
	adapter, rt, err := s.cores.SelectForUser(user)
	if err != nil {
		return err
	}
	user.CoreID = rt.ID
	user.AdapterType = rt.Type

	if err := s.store.Create(ctx, user); err != nil {
		return err
	}

	caps := adapter.Capabilities()
	if caps.Has(domain.CapAddUser) {
		if err := adapter.AddUser(ctx, id, creds); err != nil {
			_ = s.store.Delete(ctx, id)
			return fmt.Errorf("sync user to core %q: %w", rt.ID, err)
		}
	} else {
		slog.Warn("core does not support adding users; user stored locally only", "core_id", rt.ID, "id", id)
	}

	if caps.Has(domain.CapSpeedLimit) && (speedUp > 0 || speedDown > 0) {
		if limiter, ok := adapter.(port.SpeedLimiter); ok {
			if err := limiter.SetUserSpeedLimit(ctx, id, speedUp, speedDown); err != nil {
				slog.Warn("failed to apply native user speed limit", "core_id", rt.ID, "id", id, "err", err)
			}
		}
	}
	return nil
}

func (s *UserService) UpdateUser(ctx context.Context, id, name string, quota, speedUp, speedDown int64, expireAt *time.Time) error {
	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) != "" {
		user.Name = strings.TrimSpace(name)
	}
	user.Quota = quota
	user.SpeedLimitUp = speedUp
	user.SpeedLimitDown = speedDown
	user.ExpireAt = expireAt
	s.fillRuntimeOwnership(user)

	if err := s.store.Update(ctx, user); err != nil {
		return err
	}

	adapter, rt, err := s.cores.SelectForUser(user)
	if err != nil {
		return err
	}
	if user.Enabled && adapter.Capabilities().Has(domain.CapSpeedLimit) {
		if limiter, ok := adapter.(port.SpeedLimiter); ok {
			if err := limiter.SetUserSpeedLimit(ctx, id, speedUp, speedDown); err != nil {
				slog.Warn("failed to update native user speed limit", "core_id", rt.ID, "id", id, "err", err)
			}
		}
	}
	return nil
}

func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	s.drainTraffic(ctx)
	adapter, _, err := s.cores.SelectForUser(user)
	if err != nil {
		return err
	}
	if adapter.Capabilities().Has(domain.CapRemoveUser) {
		if err := adapter.RemoveUser(ctx, id); err != nil {
			return fmt.Errorf("remove user from core: %w", err)
		}
	}
	return s.store.Delete(ctx, id)
}

func (s *UserService) SetEnabled(ctx context.Context, id string, enabled bool) error {
	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if user.Enabled == enabled {
		return nil
	}
	adapter, rt, err := s.cores.SelectForUser(user)
	if err != nil {
		return err
	}
	caps := adapter.Capabilities()

	if enabled {
		if caps.Has(domain.CapEnableUser) {
			stateManager, ok := adapter.(port.UserStateManager)
			if !ok {
				return domain.ErrNotSupported
			}
			if err := stateManager.EnableUser(ctx, id, user.Credentials); err != nil {
				return fmt.Errorf("enable user in core %q: %w", rt.ID, err)
			}
		} else if caps.Has(domain.CapAddUser) {
			if err := adapter.AddUser(ctx, id, user.Credentials); err != nil {
				return fmt.Errorf("add user in core %q: %w", rt.ID, err)
			}
		} else {
			slog.Warn("core does not support account control; enabled flag is local only", "core_id", rt.ID, "id", id)
		}
		if caps.Has(domain.CapSpeedLimit) {
			if limiter, ok := adapter.(port.SpeedLimiter); ok {
				_ = limiter.SetUserSpeedLimit(ctx, id, user.SpeedLimitUp, user.SpeedLimitDown)
			}
		}
	} else {
		if caps.Has(domain.CapDisableUser) {
			stateManager, ok := adapter.(port.UserStateManager)
			if !ok {
				return domain.ErrNotSupported
			}
			if err := stateManager.DisableUser(ctx, id); err != nil {
				return fmt.Errorf("disable user in core %q: %w", rt.ID, err)
			}
		} else if caps.Has(domain.CapRemoveUser) {
			if err := adapter.RemoveUser(ctx, id); err != nil {
				return fmt.Errorf("remove user in core %q: %w", rt.ID, err)
			}
		} else {
			slog.Warn("core does not support account control; disabled flag is local only", "core_id", rt.ID, "id", id)
		}
		s.killUserConnections(id, adapter)
	}

	user.Enabled = enabled
	s.fillRuntimeOwnership(user)
	return s.store.Update(ctx, user)
}

func (s *UserService) ListUsers(ctx context.Context) ([]*domain.User, error) {
	users, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		s.fillRuntimeOwnership(user)
	}
	return users, nil
}

func (s *UserService) GetUser(ctx context.Context, id string) (*domain.User, error) {
	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.fillRuntimeOwnership(user)
	return user, nil
}

func (s *UserService) fillRuntimeOwnership(user *domain.User) {
	if user == nil {
		return
	}
	if user.CoreID == "" {
		user.CoreID = s.cores.DefaultID()
	}
	if user.AdapterType == "" {
		if rt, ok := s.cores.Get(user.CoreID); ok {
			user.AdapterType = rt.Type
		}
	}
}

func (s *UserService) killUserConnections(userID string, adapter port.VPNAdapter) {
	if !adapter.Capabilities().Has(domain.CapActiveConns) || !adapter.Capabilities().Has(domain.CapKillConn) {
		return
	}
	connProvider, ok := adapter.(port.ConnectionProvider)
	if !ok {
		return
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		conns, err := connProvider.GetActiveConnections(context.Background())
		if err != nil {
			slog.Warn("failed to list active connections for disabled user", "err", err)
			return
		}
		for _, conn := range conns {
			if conn.UserID == userID {
				_ = connProvider.KillConnection(context.Background(), conn.ID)
			}
		}
	}()
}

func (s *UserService) drainTraffic(ctx context.Context) {
	if s.trafficSvc != nil {
		s.trafficSvc.PollOnce(ctx)
	}
}
