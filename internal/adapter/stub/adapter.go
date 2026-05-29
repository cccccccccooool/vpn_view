// ============================================================================
// 文件说明：internal/adapter/stub/adapter.go
// 职责概览：提供一个完全基于内存运行的模拟测试桩 VPN 适配器（Adapter）。
//           对外完整实现 port.VPNAdapter 与 port.ProfileProvider 接口。
//           通过在内存中维护用户字典、限速缓存以及随机数模拟流量增量，
//           支持在脱离真实 VPN 网络环境（如开发调试、UI 测试）时，
//           让主程序流程无缝闭环运转，用于完整检验管理面板的所有功能。
// ============================================================================

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

// Adapter 基于内存的高保真 VPN 后端测试桩适配器。
// 所有创建的用户、证书、网速控制指标等均只记录在内存 map 中，服务重启后即清空消失。
type Adapter struct {
	mu            sync.RWMutex
	users         map[string]map[string]string // 模拟的底层用户凭据表 userID -> credentials
	speedLimitsUp map[string]int64             // 模拟的用户单人上传限速，单位：字节/秒
	speedLimitsDn map[string]int64             // 模拟的用户单人下载限速，单位：字节/秒
	traffic       map[string][2]int64          // 模拟的用户累计消耗流量 [uploadBytes, downloadBytes]
	globalUp      int64                        // 模拟的整机全局上传限速，单位：字节/秒
	globalDn      int64                        // 模拟的整机全局下载限速，单位：字节/秒
}

// New 初始化并实例化测试桩适配器。
// 支持在全局配置 cfg["mock_users"] 中，指定系统初始模拟自动批量生成的测试用户数量。
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

// Capabilities 声明该测试桩适配器拥有的全部能力位掩码。
// 桩为了配合主流程测试，特意声明支持所有的 Capabilities，以便进行全面的功能闭环校验。
func (a *Adapter) Capabilities() domain.Capability {
	return domain.CapListUsers | domain.CapAddUser | domain.CapRemoveUser |
		domain.CapDisableUser | domain.CapEnableUser | domain.CapQueryTraffic |
		domain.CapRealtimeSpeed | domain.CapUserSpeed | domain.CapActiveConns |
		domain.CapKillConn | domain.CapSubscription | domain.CapCredentialDefs |
		domain.CapSpeedLimit | domain.CapGlobalSpeedLimit
}

// Profile 实现 port.ProfileProvider 接口，返回该桩的特征配置描述。
func (a *Adapter) Profile() domain.AdapterProfile {
	return domain.AdapterProfile{
		Name:              "stub-mock-adapter",
		UserProvisionMode: domain.UserProvisionAPI,
		TrafficMode:       domain.TrafficModeCumulative,
		TrafficScope:      domain.TrafficScopeUser,
		ReloadMode:        domain.ReloadModeAPI,
		ConfigFormat:      domain.ConfigFormatNone,
		Identity: domain.IdentityProfile{
			MetadataKeys:    []string{"user_id"},
			AllowIPFallback: true,
		},
	}
}

// CredentialFields 返回测试桩模拟的凭据输入框字段定义（仅 UUID 和 Flow），
// 供前端动态渲染表单进行添加测试。
func (a *Adapter) CredentialFields() []port.CredentialField {
	return []port.CredentialField{
		{Key: "uuid", Label: "测试 UUID", Type: "text", Required: true, AutoGenerate: true},
		{Key: "flow", Label: "流控 Flow", Type: "select", Options: []string{"", "xtls-rprx-vision"}},
	}
}

// ListUsers 返回当前内存中记录的全部用户 ID 列表。
func (a *Adapter) ListUsers(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ids := make([]string, 0, len(a.users))
	for id := range a.users {
		ids = append(ids, id)
	}
	return ids, nil
}

// AddUser 将账号及其凭据保存到内存中，并自动初始化其模拟流量累加器。
func (a *Adapter) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.users[userID] = cloneMap(credentials)
	if _, ok := a.traffic[userID]; !ok {
		a.traffic[userID] = [2]int64{}
	}
	return nil
}

// RemoveUser 从内存中彻底抹除该用户，并清理其关联的模拟限速与流量缓存。
func (a *Adapter) RemoveUser(ctx context.Context, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.users, userID)
	delete(a.speedLimitsUp, userID)
	delete(a.speedLimitsDn, userID)
	delete(a.traffic, userID)
	return nil
}

// DisableUser 禁用用户，桩实现中为了测试闭环，效果等同于 RemoveUser 物理移除连接。
func (a *Adapter) DisableUser(ctx context.Context, userID string) error {
	return a.RemoveUser(ctx, userID)
}

// EnableUser 恢复启用用户，效果等同于重新 AddUser 登记凭据。
func (a *Adapter) EnableUser(ctx context.Context, userID string, credentials map[string]string) error {
	return a.AddUser(ctx, userID, credentials)
}

// QueryTraffic 核心模拟生成算法。
// 依据随机数发生器，每次被轮询时，为内存中的每个活跃用户随机产生一定的上传、下载流量字节数，
// 累加到累计计数器中，模拟用户的真实上网过程。
// 测速惩罚对齐：产生的流量增量严格受内存限速 `speedLimits` 约束，若超限则强行截断（用于完整测试 Strike 罚下机制）。
func (a *Adapter) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	snaps := make([]port.TrafficSnapshot, 0, len(a.users))
	for id := range a.users {
		// 随机生成 4KB - 350KB 的上传增量，16KB - 1.2MB 的下载增量
		up := rand.Int64N(350*1024) + 4*1024
		down := rand.Int64N(1200*1024) + 16*1024

		current := a.traffic[id]
		// 结合限速进行流量截断
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

// GetGlobalSpeed 返回模拟的整机当前瞬时全局上下行实时速率。
func (a *Adapter) GetGlobalSpeed(ctx context.Context) (*port.GlobalSpeed, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	userCount := maxInt(len(a.users), 1)
	// 根据人数随机折算实时速度
	up := rand.Int64N(int64(userCount)*512*1024) + int64(userCount)*32*1024
	down := rand.Int64N(int64(userCount)*2*1024*1024) + int64(userCount)*96*1024
	return &port.GlobalSpeed{
		Up:   clampPositive(up, a.globalUp),
		Down: clampPositive(down, a.globalDn),
	}, nil
}

// GetActiveConnections 随机从内存用户中挑选，生成并虚构出一批包含 IP、建立时间、
// 当前流量、TCP/UDP 类型的仿真活动连接列表，用于大屏连接看板和 Kill 连接的交互测试。
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

	count := minInt(len(ids)+2, 8) // 随机生成最多 8 个连接
	conns := make([]port.ActiveConnection, 0, count)
	for i := 0; i < count; i++ {
		userID := ids[rand.IntN(len(ids))]
		conns = append(conns, port.ActiveConnection{
			ID:          fmt.Sprintf("stub-conn-%d-%d", time.Now().Unix()%1000, i),
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

// KillConnection 阻断连接。桩测试模拟中仅做空操作放行。
func (a *Adapter) KillConnection(ctx context.Context, connID string) error {
	return nil
}

// SetUserSpeedLimit 对单个用户设置模拟限速数值。
func (a *Adapter) SetUserSpeedLimit(ctx context.Context, userID string, uploadBPS, downloadBPS int64) error {
	a.mu.Lock()
	a.speedLimitsUp[userID] = uploadBPS
	a.speedLimitsDn[userID] = downloadBPS
	a.mu.Unlock()
	return nil
}

// SetGlobalSpeedLimit 设置全局模拟限速数值。
func (a *Adapter) SetGlobalSpeedLimit(ctx context.Context, uploadBPS, downloadBPS int64) error {
	a.mu.Lock()
	a.globalUp = uploadBPS
	a.globalDn = downloadBPS
	a.mu.Unlock()
	return nil
}

// GenerateSubscription 为用户虚构生成一条可用的 VLESS 测试订阅链接，MIME 类型为 plain/text。
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
	// 拼接仿真订阅链接
	return []byte(fmt.Sprintf("vless://%s@stub.example.com:443?%s#%s", uuid, query, userID)), "text/plain; charset=utf-8", nil
}

// Close 关闭释放资源。
func (a *Adapter) Close() error {
	return nil
}

// cloneMap 深拷贝 credentials 字典，防止并发冲突和脏更改。
func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// intFromConfig 解析 cfg 中配置的 int 属性，支持 float64 类型断言兼容。
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

// clampPositive 数值限截断：如果 limit 阈值有效且 value 超过阈值，返回 limit 限制值。
func clampPositive(value, limit int64) int64 {
	if limit > 0 && value > limit {
		return limit
	}
	return value
}

// maxInt 快速求最大 int。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt 快速求最小 int。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
