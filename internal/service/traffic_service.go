// ============================================================================
// 文件说明：internal/service/traffic_service.go
// 职责概览：实现系统的流量轮询监控与网速统计服务（TrafficService）。
//           主要依靠后台循环协程，定时（如每 10 秒）从 VPN 适配器拉取最新的累计流量快照。
//           通过与上轮基准数据做减法得出流量增量，并将增量数据累加落库到持久化 SQLite 数据库中。
//           根据两次轮询之间的时间差计算出每位用户瞬时实时的上行、下行网速（速度数据）。
//           提供系统首页所需的全局吞吐统计（包含瞬时全局速度、活跃用户、活跃连接数等看板数据）。
// ============================================================================

package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// TrafficService 控制系统核心流量数据轮询、数据累加持久化与瞬时网速计算。
type TrafficService struct {
	store    port.UserStore  // 本地 SQLite 存储句柄
	adapter  port.VPNAdapter // VPN 内核适配器
	interval time.Duration   // 流量拉取定时轮询间隔

	mu            sync.RWMutex
	speeds        map[string][2]int64 // 各用户当前实时的上传与下载网速 [uploadSpeed, downloadSpeed]（字节/秒）
	lastTraffic   map[string][2]int64 // 上一次轮询拉取到的各用户历史累计流量计数基准 [uploadAccum, downloadAccum]（字节），用于得出相对增量
	lastGlobalUp  int64               // 轮询累加算出的全局实时上行吞吐速率（字节/秒）
	lastGlobalDn  int64               // 轮询累加算出的全局实时下行吞吐速率（字节/秒）
	lastPollError error               // 记录最近一次拉取流量发生的异常错误信息（便于监控报错）
}

// NewTrafficService 实例化并创建一个新的 TrafficService 服务。
func NewTrafficService(store port.UserStore, adapter port.VPNAdapter, interval time.Duration) *TrafficService {
	return &TrafficService{
		store:       store,
		adapter:     adapter,
		interval:    interval,
		speeds:      make(map[string][2]int64),
		lastTraffic: make(map[string][2]int64),
	}
}

// Start 后台轮询的主协程逻辑入口。当主程序 `main.go` 启动时，开启此守护协程。
// 事务保护：如果底层的适配器完全声明不支持 CapQueryTraffic 能力，则此定时轮询将自动跳过并关闭，防止异常空转。
// 本函数会永久阻塞，直到 ctx 被取消停止。
func (s *TrafficService) Start(ctx context.Context) {
	if !s.adapter.Capabilities().Has(domain.CapQueryTraffic) {
		slog.Info("VPN 适配器不支持流量数据查询，流量定时轮询监控已关闭")
		return
	}
	if _, ok := s.adapter.(port.TrafficProvider); !ok {
		slog.Warn("VPN 适配器声明支持流量查询，但未实现流量接口，流量定时轮询监控已关闭")
		return
	}

	// 启动时优先主动执行一次拉取，铺垫基础基准数据
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

// PollOnce 用于强行执行一次流量抓取更新，通常在删除用户或者重大停机节点被调用以确保流量不缺失。
func (s *TrafficService) PollOnce(ctx context.Context) {
	if !s.adapter.Capabilities().Has(domain.CapQueryTraffic) {
		return
	}
	if _, ok := s.adapter.(port.TrafficProvider); !ok {
		return
	}
	s.pollTraffic(ctx)
}

// GetUserSpeeds 获取系统当前所有活动用户的实时瞬时网速。
// 返回的 map 以用户 ID 为 Key，Value 为双元数组 [上行速度(Bytes/s), 下行速度(Bytes/s)]。
func (s *TrafficService) GetUserSpeeds() map[string][2]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][2]int64, len(s.speeds))
	for k, v := range s.speeds {
		out[k] = v
	}
	return out
}

// GetGlobalStats 汇总计算并返回系统全局管理面板看板所需的汇总运行指标数据包。
// 算法对齐：全局速度数据优先从 VPN 代理原生提供的实时速度接口拉取，如果适配器不支持，则回退使用本轮询服务累加用户速度算得的值。
func (s *TrafficService) GetGlobalStats(ctx context.Context) (Stats, error) {
	// 从数据库读取历史至今所有用户的总计流量
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

	// 全局瞬时速度获取策略
	if speedProvider, ok := s.adapter.(port.GlobalSpeedProvider); s.adapter.Capabilities().Has(domain.CapRealtimeSpeed) && ok {
		speed, err := speedProvider.GetGlobalSpeed(ctx)
		if err == nil && speed != nil {
			stats.SpeedUp = speed.Up
			stats.SpeedDown = speed.Down
		} else if err != nil {
			slog.Warn("无法获取底层适配器的原生全局速度", "err", err)
		}
	} else {
		// 回退方案：使用本服务统计的各个用户网速总和累加值
		s.mu.RLock()
		stats.SpeedUp = s.lastGlobalUp
		stats.SpeedDown = s.lastGlobalDn
		s.mu.RUnlock()
	}

	// 统计活跃连接数
	if connProvider, ok := s.adapter.(port.ConnectionProvider); s.adapter.Capabilities().Has(domain.CapActiveConns) && ok {
		if conns, err := connProvider.GetActiveConnections(ctx); err == nil {
			stats.ActiveConnections = len(conns)
		}
	}

	return stats, nil
}

// GetActiveConnections 获取当前代理服务器中正在连接的所有 TCP/UDP 的详细记录列表。
func (s *TrafficService) GetActiveConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	if !s.adapter.Capabilities().Has(domain.CapActiveConns) {
		return nil, domain.ErrNotSupported
	}
	connProvider, ok := s.adapter.(port.ConnectionProvider)
	if !ok {
		return nil, domain.ErrNotSupported
	}
	conns, err := connProvider.GetActiveConnections(ctx)
	if err != nil {
		slog.Warn("从适配器拉取活动网络连接失败，返回空列表", "err", err)
		return []port.ActiveConnection{}, nil
	}
	return conns, nil
}

// KillConnection 强制踢出并掐断某个正在通信的特定活跃连接。
func (s *TrafficService) KillConnection(ctx context.Context, connID string) error {
	if !s.adapter.Capabilities().Has(domain.CapKillConn) {
		return domain.ErrNotSupported
	}
	connProvider, ok := s.adapter.(port.ConnectionProvider)
	if !ok {
		return domain.ErrNotSupported
	}
	return connProvider.KillConnection(ctx, connID)
}

// pollTraffic 底层流量轮询与核心数据差值运算算法函数。
// 逻辑流程：
//  1. 从适配器查询获得当前时刻所有注册用户的累计总流量快照（每个 snap 包含 userID, 累计 upload/download 字节数）。
//  2. 提取上轮历史基准流量（lastTraffic），两者做差得出此时间段的增量流量。
//  3. 若为负数则代表底层连接发生过关闭重置，增量做归零保护。
//  4. 首次观测的用户做基准铺设，不计入首期突发增量（防数据陡增）。
//  5. 将计算出的增量流量（deltaUp/deltaDown）即刻异步调用 `store.AddTraffic` 累加落库 SQLite，保证数据库流量数据的真实准确。
//  6. 依据两次定时轮询的时间跨度（Seconds）计算得出各用户的实时网速（上传与下载），累加求出全局速度指标。
func (s *TrafficService) pollTraffic(ctx context.Context) {
	trafficProvider, ok := s.adapter.(port.TrafficProvider)
	if !ok {
		return
	}
	snaps, err := trafficProvider.QueryTraffic(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastPollError = err
		s.mu.Unlock()
		slog.Error("从 VPN 适配器抓取用户流量数据失败", "err", err)
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
		prev, exists := s.lastTraffic[snap.UserID]
		if !exists {
			// 【冷启动安全】首次感知该用户，只作基准录入，不将历史累计流量误判为当期激增增量
			s.lastTraffic[snap.UserID] = [2]int64{snap.Upload, snap.Download}
			continue
		}

		// 做差计算出在此轮巡检间隔内消耗的绝对流量增量
		deltaUp := snap.Upload - prev[0]
		deltaDown := snap.Download - prev[1]

		// 边界重置防护：如果用户重建、底层计数清零出现负增量，做清零兜底
		if deltaUp < 0 {
			deltaUp = 0
		}
		if deltaDown < 0 {
			deltaDown = 0
		}

		// 覆写当前流量数值，作为下一轮轮询的基准值
		s.lastTraffic[snap.UserID] = [2]int64{snap.Upload, snap.Download}

		// 如果发生了实际流量消耗，调用数据库增量累加落库
		if deltaUp != 0 || deltaDown != 0 {
			if err := s.store.AddTraffic(ctx, snap.UserID, deltaUp, deltaDown); err != nil {
				slog.Warn("流量消费增量同步写入数据库失败", "user", snap.UserID, "err", err)
			}
		}

		// 结合耗时折算出实时速率（网速）
		up := deltaUp / intervalSecs
		down := deltaDown / intervalSecs
		nextSpeeds[snap.UserID] = [2]int64{up, down}
		globalUp += up
		globalDown += down
	}

	// 状态更新并解除轮询错误记录
	s.speeds = nextSpeeds
	s.lastGlobalUp = globalUp
	s.lastGlobalDn = globalDown
	s.lastPollError = nil
}

// Stats 描述管理面板大屏全局统计的基础看板数据格式。
type Stats struct {
	SpeedUp           int64           `json:"speed_up"`                  // 全局当前的实时上传网速（字节/秒）
	SpeedDown         int64           `json:"speed_down"`                // 全局当前的实时下载网速（字节/秒）
	TotalUpload       int64           `json:"total_upload"`              // 历史至今整机累计总上传已用流量（字节）
	TotalDownload     int64           `json:"total_download"`            // 历史至今整机累计总下载已用流量（字节）
	ActiveUsers       int             `json:"active_users"`              // 当前系统正处于启用连接状态的 VPN 用户总数
	ActiveConnections int             `json:"active_connections"`        // 当前代理内核中正在发生的活跃网络连接连接数
	RealtimeNative    bool            `json:"realtime_native"`           // 标识底层适配器是否能直接原生提供全局速度数据
	LastPollError     string          `json:"last_poll_error,omitempty"` // 指示最近一次流量轮询出错详情（如果没有错误则为空）
	Capabilities      map[string]bool `json:"capabilities"`              // 当前加载适配器的系统能力字典分布
}

// countEnabled 过滤统计用户列表中开启状态的用户账户总和。
func countEnabled(users []*domain.User) int {
	total := 0
	for _, user := range users {
		if user.Enabled {
			total++
		}
	}
	return total
}
