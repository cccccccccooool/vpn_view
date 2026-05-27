// user_service.go 实现用户的增删改查及启用/禁用操作。

package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// UserService 提供用户管理的核心业务逻辑，包括创建、更新、删除、启用/禁用用户。
// 它同时协调持久化存储（store）与 VPN 适配器（adapter）之间的数据一致性。
type UserService struct {
	store      port.UserStore
	adapter    port.VPNAdapter
	limits     config.LimitsConfig
	trafficSvc *TrafficService
}

// NewUserService 创建并返回一个新的 UserService 实例。
// store 用于持久化用户数据，adapter 用于与 VPN 后端交互，limits 定义默认配额和速率。
func NewUserService(store port.UserStore, adapter port.VPNAdapter, limits config.LimitsConfig) *UserService {
	return &UserService{store: store, adapter: adapter, limits: limits}
}

// AttachTrafficService wires the traffic service used for final traffic drains.
func (s *UserService) AttachTrafficService(trafficSvc *TrafficService) {
	s.trafficSvc = trafficSvc
}

// CreateUser 创建一个新用户并同步到 VPN 适配器。
// 如果传入的 quota、speedUp、speedDown 为零，则使用配置中的默认值。
// 若适配器添加用户失败，会回滚已持久化的数据。
func (s *UserService) CreateUser(ctx context.Context, id, name string, creds map[string]string, quota, speedUp, speedDown int64, expireAt *time.Time) error {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return fmt.Errorf("id is required")
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
		Credentials:    creds,
		Quota:          quota,
		SpeedLimitUp:   speedUp,
		SpeedLimitDown: speedDown,
		ExpireAt:       expireAt,
		Enabled:        true,
		CreatedAt:      time.Now(),
	}

	if err := s.store.Create(ctx, user); err != nil {
		return err
	}

	caps := s.adapter.Capabilities()
	if caps.Has(domain.CapAddUser) {
		if err := s.adapter.AddUser(ctx, id, creds); err != nil {
			_ = s.store.Delete(ctx, id)
			return fmt.Errorf("add user to adapter: %w", err)
		}
	} else {
		slog.Warn("adapter lacks CapAddUser; user saved only in store", "id", id)
	}

	if caps.Has(domain.CapSpeedLimit) && (speedUp > 0 || speedDown > 0) {
		if err := s.adapter.SetUserSpeedLimit(ctx, id, speedUp, speedDown); err != nil {
			slog.Warn("failed to apply native user speed limit", "id", id, "err", err)
		}
	}
	return nil
}

// UpdateUser 更新指定用户的名称、配额、速率限制及过期时间。
// 若用户处于启用状态且适配器支持速率限制，会同步更新适配器端的速率配置。
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

	if err := s.store.Update(ctx, user); err != nil {
		return err
	}

	if user.Enabled && s.adapter.Capabilities().Has(domain.CapSpeedLimit) {
		if err := s.adapter.SetUserSpeedLimit(ctx, id, speedUp, speedDown); err != nil {
			slog.Warn("failed to update native user speed limit", "id", id, "err", err)
		}
	}
	return nil
}

// DeleteUser 删除指定用户。删除前会先排空（drain）残余流量数据并从适配器中移除用户。
func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	s.drainTraffic(ctx)
	if s.adapter.Capabilities().Has(domain.CapRemoveUser) {
		if err := s.adapter.RemoveUser(ctx, id); err != nil {
			return fmt.Errorf("remove user from adapter: %w", err)
		}
	}
	return s.store.Delete(ctx, id)
}

// SetEnabled 启用或禁用指定用户。
// 启用时会根据适配器能力调用 EnableUser 或 AddUser，并恢复速率限制；
// 禁用时会调用 DisableUser 或 RemoveUser。状态变更会持久化到存储中。
func (s *UserService) SetEnabled(ctx context.Context, id string, enabled bool) error {
	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if user.Enabled == enabled {
		return nil
	}

	caps := s.adapter.Capabilities()
	if enabled {
		if caps.Has(domain.CapEnableUser) {
			if err := s.adapter.EnableUser(ctx, id, user.Credentials); err != nil {
				return fmt.Errorf("enable user in adapter: %w", err)
			}
		} else if caps.Has(domain.CapAddUser) {
			if err := s.adapter.AddUser(ctx, id, user.Credentials); err != nil {
				return fmt.Errorf("add user in adapter: %w", err)
			}
		} else {
			slog.Warn("adapter cannot enable users; store flag only", "id", id)
		}
		if caps.Has(domain.CapSpeedLimit) {
			_ = s.adapter.SetUserSpeedLimit(ctx, id, user.SpeedLimitUp, user.SpeedLimitDown)
		}
	} else {
		if caps.Has(domain.CapDisableUser) {
			if err := s.adapter.DisableUser(ctx, id); err != nil {
				return fmt.Errorf("disable user in adapter: %w", err)
			}
		} else if caps.Has(domain.CapRemoveUser) {
			if err := s.adapter.RemoveUser(ctx, id); err != nil {
				return fmt.Errorf("remove user in adapter: %w", err)
			}
		} else {
			slog.Warn("adapter cannot disable users; store flag only", "id", id)
		}

		// 🏆 新增：强行切断被禁用用户的所有活跃 TCP 连接，实现即时断流！
		if caps.Has(domain.CapActiveConns) && caps.Has(domain.CapKillConn) {
			go func() {
				// 异步执行以避免阻塞当前的 HTTP 响应
				// 给适配器和底层内核少许时间做初始准备
				time.Sleep(100 * time.Millisecond)
				conns, err := s.adapter.GetActiveConnections(context.Background())
				if err != nil {
					slog.Warn("failed to query active connections for user kick", "err", err)
					return
				}
				killedCount := 0
				for _, conn := range conns {
					// 根据连接的 UserID 过滤属于该被禁用用户的连接，然后强制关闭！
					if conn.UserID == id {
						if err := s.adapter.KillConnection(context.Background(), conn.ID); err == nil {
							killedCount++
						}
					}
				}
				if killedCount > 0 {
					slog.Info("forcefully disconnected disabled user connections", "user_id", id, "connections_killed", killedCount)
				}
			}()
		}
	}

	user.Enabled = enabled
	return s.store.Update(ctx, user)
}

// ListUsers 返回所有用户的列表。
func (s *UserService) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return s.store.List(ctx)
}

// GetUser 根据 ID 获取单个用户信息。
func (s *UserService) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return s.store.GetByID(ctx, id)
}

// drainTraffic 在删除用户前将适配器中尚未持久化的流量数据写入存储，
// 防止删除操作导致流量统计数据丢失。
func (s *UserService) drainTraffic(ctx context.Context) {
	if s.trafficSvc == nil {
		return
	}
	s.trafficSvc.PollOnce(ctx)
}
