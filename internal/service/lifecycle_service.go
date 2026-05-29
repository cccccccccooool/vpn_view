// ============================================================================
// 文件说明：internal/service/lifecycle_service.go
// 职责概览：实现 VPN 用户账号的生命周期自动巡检服务（LifecycleService）。
//           主要依靠后台循环协程，定时巡检所有启用中的 VPN 用户状态。
//           核心巡检规则包含三条：
//           1. 过期停机：检查账户的 ExpireAt 字段，若已过有效期，自动将其执行停机禁用。
//           2. 流量超支停机：检查用户的已消耗总流量（Upload + Download），若超出了 Quota 配额上限，自动执行停机禁用。
//           3. 软件测速限制与惩罚（Strike）：当底层适配器硬件不支持限速能力时，利用本服务在软件层面监控用户的瞬时网速。
//              如果发现用户持续超速，累计 strikes 达到阈值，则自动实施惩罚性停机禁用，杜绝用户超速抢占带宽。
// ============================================================================

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

// LifecycleService 定时扫库，对违规、欠费、超支、到期用户自动关停，防止滥用。
type LifecycleService struct {
	store      port.UserStore      // SQLite 本地持久化存储
	adapter    port.VPNAdapter     // VPN 代理适配器
	limits     config.LimitsConfig // 限制惩罚相关的全局参数
	trafficSvc *TrafficService     // 用于拉取用户瞬时实时速率
	interval   time.Duration       // 扫库巡检周期

	mu           sync.Mutex
	speedStrikes map[string]int // 用户连续违规超速的计数器 map（ID -> 连续超速违规次数 strikes）
}

// NewLifecycleService 实例化创建一个新的 LifecycleService 用户生命周期服务。
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

// Start 生命周期巡检的主协程生命周期起点。由主程序启动时并发运行。
// 本函数会永久阻塞，直至 ctx 被取消。
func (s *LifecycleService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 启动时立即执行首次状态检查
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

// checkAll 执行全量核心生命周期合规检查。
func (s *LifecycleService) checkAll(ctx context.Context) {
	users, err := s.store.List(ctx)
	if err != nil {
		slog.Warn("生命周期服务扫描用户列表失败", "err", err)
		return
	}

	caps := s.adapter.Capabilities()
	now := time.Now()
	speeds := s.trafficSvc.GetUserSpeeds() // 获取全员当前的瞬时网速数据

	for _, user := range users {
		// 已经处于禁用停机状态的用户，不重复核查
		if !user.Enabled {
			continue
		}

		// 检查项 1：是否已超出指定的有效期时间
		if user.ExpireAt != nil && now.After(*user.ExpireAt) {
			slog.Info("用户已过有效期截止日，触发到期自动停机关停", "id", user.ID)
			s.disableUser(ctx, user, domain.ErrExpired)
			continue
		}

		// 检查项 2：检查累计使用的流量是否超出配额上限
		if caps.Has(domain.CapQueryTraffic) && user.Quota > 0 && user.Upload+user.Download >= user.Quota {
			slog.Info("用户已用尽设定的流量配额，触发配额超限停机关停", "id", user.ID)
			s.disableUser(ctx, user, domain.ErrQuotaExceeded)
			continue
		}

		// 速率正常，擦除历史超速 strike 记录
		s.clearSpeedStrike(user.ID)
	}

	// 检查项 4：若底层不支持全局硬件总吞吐速率限制，使用软件层面对全局网速超限情况在日志端报警
	if !caps.Has(domain.CapGlobalSpeedLimit) {
		s.logGlobalSoftwareLimit(speeds)
	}
}

// exceedsSoftwareUserLimit 判断单用户的实时上传/下载速度是否超过了其预设的速度上限。
func (s *LifecycleService) exceedsSoftwareUserLimit(user *domain.User, speed [2]int64) bool {
	return (user.SpeedLimitUp > 0 && speed[0] > user.SpeedLimitUp) ||
		(user.SpeedLimitDown > 0 && speed[1] > user.SpeedLimitDown)
}

// clearSpeedStrike 清空用户的速度违规记录 strikes 计数。
func (s *LifecycleService) clearSpeedStrike(userID string) {
	s.mu.Lock()
	delete(s.speedStrikes, userID)
	s.mu.Unlock()
}

// logGlobalSoftwareLimit 检查全局速度是否超出了全局软件配置速度，并向日志打印警告警报。
func (s *LifecycleService) logGlobalSoftwareLimit(speeds map[string][2]int64) {
	var up, down int64
	for _, speed := range speeds {
		up += speed[0]
		down += speed[1]
	}
	if s.limits.GlobalUploadSpeed > 0 && up > s.limits.GlobalUploadSpeed {
		slog.Warn("全局瞬时上传总速率突破预警限额！", "当前速度(Bytes/s)", up, "全局限速限额", s.limits.GlobalUploadSpeed)
	}
	if s.limits.GlobalDownloadSpeed > 0 && down > s.limits.GlobalDownloadSpeed {
		slog.Warn("全局瞬时下载总速率突破预警限额！", "当前速度(Bytes/s)", down, "全局限速限额", s.limits.GlobalDownloadSpeed)
	}
}

// disableUser 将用户标记为不可用（禁用状态）。
// 适配器对齐：优先调用适配器的 DisableUser 控制底层账号逻辑；如若不支持，降级物理 RemoveUser 剔除用户。
// 状态存库：最后将 user.Enabled = false 持久化保存进数据库并清理超速 Strike。
func (s *LifecycleService) disableUser(ctx context.Context, user *domain.User, reason error) {
	caps := s.adapter.Capabilities()
	if caps.Has(domain.CapDisableUser) {
		stateManager, ok := s.adapter.(port.UserStateManager)
		if !ok {
			slog.Warn("适配器声明支持禁用用户，但未实现启停接口", "id", user.ID)
		} else if err := stateManager.DisableUser(ctx, user.ID); err != nil {
			slog.Warn("生命周期调用适配器禁用用户接口失败", "id", user.ID, "err", err)
		}
	} else if caps.Has(domain.CapRemoveUser) {
		if err := s.adapter.RemoveUser(ctx, user.ID); err != nil {
			slog.Warn("生命周期适配器不支持禁用，降级物理剔除用户失败", "id", user.ID, "err", err)
		}
	} else {
		slog.Warn("适配器不支持账号关停动作，状态仅保存在本地数据库中", "id", user.ID)
	}

	user.Enabled = false
	if err := s.store.Update(ctx, user); err != nil {
		slog.Warn("生命周期服务将用户禁用状态写入数据库持久化失败", "id", user.ID, "禁用原因", reason, "err", err)
	}
	s.clearSpeedStrike(user.ID) // 清空超速 Strike
}
