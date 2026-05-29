// ============================================================================
// 文件说明：internal/service/user_service.go
// 职责概览：提供用户账号管理的核心业务层服务（UserService）。
//           实现用户的增删改查及启用/禁用操作，并在底层实现数据一致性闭环：
//           协调持久化数据库（port.UserStore）与 VPN 代理适配器（port.VPNAdapter）的同步写入。
//           支持用户禁用时，自动强行切断其在代理后端现存的活动连接实现即时断流。
// ============================================================================

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

// UserService 管理整个系统的 VPN 用户账户业务逻辑与数据对齐。
type UserService struct {
	store      port.UserStore      // 关系型数据库存储句柄（SQLite 适配器）
	adapter    port.VPNAdapter     // 底层网络代理适配器句柄（如 Singbox 适配器）
	limits     config.LimitsConfig // 全局系统限制参数配置
	trafficSvc *TrafficService     // 用于在删除用户前将残余网速流量持久化落库
}

// NewUserService 实例化并返回一个新的 UserService 服务。
func NewUserService(store port.UserStore, adapter port.VPNAdapter, limits config.LimitsConfig) *UserService {
	return &UserService{store: store, adapter: adapter, limits: limits}
}

// AttachTrafficService 用于连接装配流量轮询服务，在最后注销用户前，将未统计完的流量归入库中。
func (s *UserService) AttachTrafficService(trafficSvc *TrafficService) {
	s.trafficSvc = trafficSvc
}

// CreateUser 创建并保存一个全新的 VPN 用户，并即时同步到 VPN 代理底层。
// 如果 quota（流量配额）或速率限制参数为零，将自动采用系统 Limits 设定的全局默认值兜底。
// 事务守卫：一旦 VPN 代理适配器同步注册失败，会自动回滚（Rollback）删除 SQLite 中的用户数据，保障一致性。
func (s *UserService) CreateUser(ctx context.Context, id, name string, creds map[string]string, quota, speedUp, speedDown int64, expireAt *time.Time) error {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return fmt.Errorf("用户唯一标识 ID 不能为空")
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
		Enabled:        true, // 默认创建时开启
		CreatedAt:      time.Now(),
	}

	// 首先尝试在本地 SQLite 数据库中创建记录
	if err := s.store.Create(ctx, user); err != nil {
		return err
	}

	caps := s.adapter.Capabilities()
	// 如果适配器有 AddUser 能力，则同步写入底层代理内核配置
	if caps.Has(domain.CapAddUser) {
		if err := s.adapter.AddUser(ctx, id, creds); err != nil {
			// 【事务回滚】适配器同步失败，删除数据库已建用户
			_ = s.store.Delete(ctx, id)
			return fmt.Errorf("同步用户至 VPN 适配器失败: %w", err)
		}
	} else {
		slog.Warn("适配器不支持 CapAddUser 能力，用户仅保存在本地数据库中", "id", id)
	}

	// 如果适配器支持硬件级别单用户限速，则设置限速值
	if caps.Has(domain.CapSpeedLimit) && (speedUp > 0 || speedDown > 0) {
		limiter, ok := s.adapter.(port.SpeedLimiter)
		if ok {
			if err := limiter.SetUserSpeedLimit(ctx, id, speedUp, speedDown); err != nil {
				slog.Warn("适配器应用单用户硬件限速失败", "id", id, "err", err)
			}
		} else {
			slog.Warn("适配器声明支持单用户限速，但未实现限速接口", "id", id)
		}
	}
	return nil
}

// UpdateUser 更新已有 VPN 用户的显示名称、到期有效期时间、配额总量以及限速速率上限参数。
// 数据持久化保存到数据库中。如果用户处于 Enabled 状态且代理原生支持限速，则即时同步更新代理中的限速限制。
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

	// 保存修改到 SQLite 数据库中
	if err := s.store.Update(ctx, user); err != nil {
		return err
	}

	// 若处于启用状态且适配器支持单人限速，更新网络代理端配置
	if user.Enabled && s.adapter.Capabilities().Has(domain.CapSpeedLimit) {
		if limiter, ok := s.adapter.(port.SpeedLimiter); ok {
			if err := limiter.SetUserSpeedLimit(ctx, id, speedUp, speedDown); err != nil {
				slog.Warn("适配器同步更新单用户硬件限速失败", "id", id, "err", err)
			}
		} else {
			slog.Warn("适配器声明支持单用户限速，但未实现限速接口", "id", id)
		}
	}
	return nil
}

// DeleteUser 彻底销毁删除一个 VPN 用户。
// 优雅防漏：删除前会先调用一次【流量排空机制（drainTraffic）】抓取一次最新的流量增量保存，防止用户最后几秒消耗的流量丢失，
// 然后将其从网络代理和 SQLite 数据库中物理清除。
func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	s.drainTraffic(ctx) // 执行最后一次流量统计归仓

	if s.adapter.Capabilities().Has(domain.CapRemoveUser) {
		if err := s.adapter.RemoveUser(ctx, id); err != nil {
			return fmt.Errorf("从 VPN 适配器删除用户失败: %w", err)
		}
	}
	return s.store.Delete(ctx, id)
}

// SetEnabled 将目标用户的可用连接状态设定为【开启/关闭】。
// 如果要从禁用状态开启：将重新注册并加载用户及证书到 VPN 代理中（恢复连接）；
// 如果要将其禁用：将从代理适配器中剔除或禁用该账号。
// 🏆 即时阻断机制：如果用户被禁用，主程序会异步扫描当前所有的活动网络 TCP/UDP 连接，
//
//	发现属于该禁用用户的活动连接，则执行强制 Kill 阻断，保证流量即刻关停，拒绝白嫖！
func (s *UserService) SetEnabled(ctx context.Context, id string, enabled bool) error {
	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if user.Enabled == enabled {
		return nil // 状态未变更，直接对齐返回
	}

	caps := s.adapter.Capabilities()
	if enabled {
		// 恢复启用时：如果适配器有 EnableUser 接口则调用；否则退回到 AddUser 重新注册进代理
		if caps.Has(domain.CapEnableUser) {
			stateManager, ok := s.adapter.(port.UserStateManager)
			if !ok {
				return domain.ErrNotSupported
			}
			if err := stateManager.EnableUser(ctx, id, user.Credentials); err != nil {
				return fmt.Errorf("适配器恢复启用用户失败: %w", err)
			}
		} else if caps.Has(domain.CapAddUser) {
			if err := s.adapter.AddUser(ctx, id, user.Credentials); err != nil {
				return fmt.Errorf("适配器重新添加注册用户失败: %w", err)
			}
		} else {
			slog.Warn("适配器不支持账号控制，启用标志仅持久化在本地数据库中", "id", id)
		}
		// 恢复原有的速率限速指标
		if caps.Has(domain.CapSpeedLimit) {
			if limiter, ok := s.adapter.(port.SpeedLimiter); ok {
				_ = limiter.SetUserSpeedLimit(ctx, id, user.SpeedLimitUp, user.SpeedLimitDown)
			}
		}
	} else {
		// 临时禁用时：优先调用适配器自带的禁用接口，无则降级从配置中 Remove 删除该账号
		if caps.Has(domain.CapDisableUser) {
			stateManager, ok := s.adapter.(port.UserStateManager)
			if !ok {
				return domain.ErrNotSupported
			}
			if err := stateManager.DisableUser(ctx, id); err != nil {
				return fmt.Errorf("适配器禁用用户失败: %w", err)
			}
		} else if caps.Has(domain.CapRemoveUser) {
			if err := s.adapter.RemoveUser(ctx, id); err != nil {
				return fmt.Errorf("适配器降级物理剔除用户失败: %w", err)
			}
		} else {
			slog.Warn("适配器不支持账号控制，禁用标志仅持久化在本地数据库中", "id", id)
		}

		// 🏆【核心即时阻断】强行切断当前该禁用用户正在进行的全部活动网络 TCP/UDP 连接，达到即刻停网效果
		if caps.Has(domain.CapActiveConns) && caps.Has(domain.CapKillConn) {
			connProvider, ok := s.adapter.(port.ConnectionProvider)
			if ok {
				go func() {
					// 异步进行，延迟 100ms 避开底层操作的配置重载瞬时锁定
					time.Sleep(100 * time.Millisecond)
					conns, err := connProvider.GetActiveConnections(context.Background())
					if err != nil {
						slog.Warn("踢除用户失败：无法从适配器获取活跃连接列表", "err", err)
						return
					}
					killedCount := 0
					for _, conn := range conns {
						if conn.UserID == id {
							// 只要属于此禁用用户的网络连接，执行物理阻断
							if err := connProvider.KillConnection(context.Background(), conn.ID); err == nil {
								killedCount++
							}
						}
					}
					if killedCount > 0 {
						slog.Info("已成功强制阻断禁用用户的全部网络活动连接", "user_id", id, "阻断连接数", killedCount)
					}
				}()
			}
		}
	}

	user.Enabled = enabled
	return s.store.Update(ctx, user)
}

// ListUsers 获取本地数据库中保存的完整用户列表数据。
func (s *UserService) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return s.store.List(ctx)
}

// GetUser 根据 ID 单独提取单个用户的属性配置与流量数据。
func (s *UserService) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return s.store.GetByID(ctx, id)
}

// drainTraffic 用于在删除或重大变更前触发一次流量收缴，防止由于异步轮询时间差造成流量统计缺失漏记。
func (s *UserService) drainTraffic(ctx context.Context) {
	if s.trafficSvc == nil {
		return
	}
	s.trafficSvc.PollOnce(ctx)
}
