// Package stub 提供了一个基于内存的 VPN 适配器桩实现，用于开发和测试。
// In-memory stub adapter for development and testing.
package stub

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// Adapter 是一个基于内存的 VPN 后端桩实现。
// 所有用户数据和限速配置都保存在内存中，进程重启后丢失。
// 主要用于在没有真实 VPN 后端时进行 UI 开发和功能测试。
type Adapter struct {
	mu            sync.RWMutex
	users         map[string]map[string]string // userID -> credentials
	speedLimitsUp map[string]int64             // 每用户上传限速 (bytes/s)
	speedLimitsDn map[string]int64             // 每用户下载限速 (bytes/s)
	traffic       map[string][2]int64          // 每用户累计流量 [upload, download]
	globalUp      int64                        // 全局上传限速 (bytes/s)
	globalDn      int64                        // 全局下载限速 (bytes/s)
}

// New 创建一个新的桩适配器实例。
// 可通过 cfg["mock_users"] 指定预生成的模拟用户数量。
func New(cfg map[string]any) *Adapter {
	a := &Adapter{
		users:         make(map[string]map[string]string),
		speedLimitsUp: make(map[string]int64),
		speedLimitsDn: make(map[string]int64),
		traffic:       make(map[string][2]int64),
	}

	count := intFromConfig(cfg, "mock_users", 0)
	for i := 1; i <= count; i++ {
		id := fmt.Sprintf("demo-%02d", i)
		a.users[id] = map[string]string{
			"uuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			"flow": "",
		}
	}
	return a
}

// Capabilities 返回该桩适配器支持的全部能力标志位。
// 桩实现声明支持所有能力，以便完整测试 UI 功能。
func (a *Adapter) Capabilities() domain.Capability {
	return domain.CapListUsers | domain.CapAddUser | domain.CapRemoveUser |
		domain.CapDisableUser | domain.CapEnableUser | domain.CapQueryTraffic |
		domain.CapRealtimeSpeed | domain.CapUserSpeed | domain.CapActiveConns |
		domain.CapKillConn | domain.CapSubscription | domain.CapCredentialDefs |
		domain.CapSpeedLimit | domain.CapGlobalSpeedLimit
}

// CredentialFields 返回桩适配器所需的凭据字段定义（UUID 和 Flow）。
func (a *Adapter) CredentialFields() []port.CredentialField {
	return []port.CredentialField{
		{Key: "uuid", Label: "UUID", Type: "text", Required: true, AutoGenerate: true},
		{Key: "flow", Label: "Flow", Type: "select", Options: []string{"", "xtls-rprx-vision"}},
	}
}

// ListUsers 返回当前内存中所有用户的 ID 列表。
func (a *Adapter) ListUsers(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ids := make([]string, 0, len(a.users))
	for id := range a.users {
		ids = append(ids, id)
	}
	return ids, nil
}

// AddUser 将指定用户及其凭据添加到内存存储中。
func (a *Adapter) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.users[userID] = cloneMap(credentials)
	if _, ok := a.traffic[userID]; !ok {
		a.traffic[userID] = [2]int64{}
	}
	return nil
}

// RemoveUser 从内存中移除指定用户及其关联的限速配置。
func (a *Adapter) RemoveUser(ctx context.Context, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.users, userID)
	delete(a.speedLimitsUp, userID)
	delete(a.speedLimitsDn, userID)
	delete(a.traffic, userID)
	return nil
}

// DisableUser 禁用指定用户。桩实现中等同于 RemoveUser。
func (a *Adapter) DisableUser(ctx context.Context, userID string) error {
	return a.RemoveUser(ctx, userID)
}

// EnableUser 启用指定用户。桩实现中等同于 AddUser。
func (a *Adapter) EnableUser(ctx context.Context, userID string, credentials map[string]string) error {
	return a.AddUser(ctx, userID, credentials)
}

// QueryTraffic 返回所有用户的模拟累计流量快照。
// 数据通过随机数生成，并受用户限速约束。
func (a *Adapter) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	snaps := make([]port.TrafficSnapshot, 0, len(a.users))
	for id := range a.users {
		up := rand.Int64N(350*1024) + 4*1024
		down := rand.Int64N(1200*1024) + 16*1024
		current := a.traffic[id]
		current[0] += clampPositive(up, a.speedLimitsUp[id])
		current[1] += clampPositive(down, a.speedLimitsDn[id])
		a.traffic[id] = current
		snaps = append(snaps, port.TrafficSnapshot{
			UserID:   id,
			Upload:   current[0],
			Download: current[1],
		})
	}
	return snaps, nil
}

// GetGlobalSpeed 返回模拟的全局实时速率，受全局限速约束。
func (a *Adapter) GetGlobalSpeed(ctx context.Context) (*port.GlobalSpeed, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	userCount := maxInt(len(a.users), 1)
	up := rand.Int64N(int64(userCount)*512*1024) + int64(userCount)*32*1024
	down := rand.Int64N(int64(userCount)*2*1024*1024) + int64(userCount)*96*1024
	return &port.GlobalSpeed{
		Up:   clampPositive(up, a.globalUp),
		Down: clampPositive(down, a.globalDn),
	}, nil
}

// GetActiveConnections 返回模拟的活跃连接列表。
// 随机从现有用户中选取，生成伪造的连接信息用于 UI 展示测试。
func (a *Adapter) GetActiveConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ids := make([]string, 0, len(a.users))
	for id := range a.users {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	count := minInt(len(ids)+2, 8)
	conns := make([]port.ActiveConnection, 0, count)
	for i := 0; i < count; i++ {
		userID := ids[rand.IntN(len(ids))]
		conns = append(conns, port.ActiveConnection{
			ID:          fmt.Sprintf("stub-%d-%d", time.Now().Unix()%1000, i),
			UserID:      userID,
			Upload:      rand.Int64N(4 * 1024 * 1024),
			Download:    rand.Int64N(16 * 1024 * 1024),
			Start:       time.Now().Add(-time.Duration(rand.IntN(7200)) * time.Second),
			Network:     []string{"tcp", "udp"}[rand.IntN(2)],
			Source:      fmt.Sprintf("192.168.1.%d:%d", rand.IntN(250)+2, rand.IntN(50000)+10000),
			Destination: fmt.Sprintf("203.0.113.%d:443", rand.IntN(250)+1),
		})
	}
	return conns, nil
}

// KillConnection 终止指定连接。桩实现中为空操作，始终返回 nil。
func (a *Adapter) KillConnection(ctx context.Context, connID string) error {
	return nil
}

// SetUserSpeedLimit 设置指定用户的上传/下载限速（单位：bytes/s）。
// 设为 0 表示不限速。
func (a *Adapter) SetUserSpeedLimit(ctx context.Context, userID string, uploadBPS, downloadBPS int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.speedLimitsUp[userID] = uploadBPS
	a.speedLimitsDn[userID] = downloadBPS
	return nil
}

// SetGlobalSpeedLimit 设置全局上传/下载限速（单位：bytes/s）。
// 设为 0 表示不限速。
func (a *Adapter) SetGlobalSpeedLimit(ctx context.Context, uploadBPS, downloadBPS int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.globalUp = uploadBPS
	a.globalDn = downloadBPS
	return nil
}

// GenerateSubscription 为指定用户生成 VLESS 订阅链接。
// 返回订阅内容字节、Content-Type 和可能的错误。
func (a *Adapter) GenerateSubscription(ctx context.Context, userID string, credentials map[string]string) ([]byte, string, error) {
	uuid := credentials["uuid"]
	if uuid == "" {
		uuid = userID
	}
	flow := credentials["flow"]
	query := "encryption=none&security=tls&type=tcp"
	if flow != "" {
		query += "&flow=" + flow
	}
	return []byte(fmt.Sprintf("vless://%s@stub.example.com:443?%s#%s", uuid, query, userID)), "text/plain; charset=utf-8", nil
}

// Close 释放适配器资源。桩实现中为空操作。
func (a *Adapter) Close() error {
	return nil
}

// cloneMap 深拷贝一个 map[string]string，避免外部引用修改内部数据。
func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// intFromConfig 从配置 map 中安全地提取整数值，支持 int/int64/float64 类型转换。
func intFromConfig(cfg map[string]any, key string, fallback int) int {
	if cfg == nil {
		return fallback
	}
	switch v := cfg[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return fallback
	}
}

// clampPositive 将 value 限制在 limit 以内；limit <= 0 时不做限制。
func clampPositive(value, limit int64) int64 {
	if limit > 0 && value > limit {
		return limit
	}
	return value
}

// maxInt 返回两个 int 中的较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt 返回两个 int 中的较小值。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
