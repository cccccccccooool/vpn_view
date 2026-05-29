// ============================================================================
// 文件说明：internal/adapter/singbox/clash_client.go
// 职责概览：实现连接并通信 Sing-box 内置 Clash API 兼容服务的 HTTP 客户端（ClashClient）。
//           提供利用 Clash /traffic 实时吞吐推送流（SSE）读取全局瞬时速度的方法。
//           提供从 `/connections` 活动长连接端点，拉取当前服务器所有活跃 TCP/UDP 网络连接明细、
//           以及向指定 ID 发起 DELETE 剔除网络连接的方法。
//           降级兜底算法（QueryTrafficFromConnections）：
//           当 gRPC 流量统计组件完全未配置或端口挂掉时，此组件可作为坚实的降级方案。
//           它会拉取 /connections，结合 identity.Resolver 模块，反向汇总还原出所有用户的
//           实时累计流量数据，保障流量落库和超额停机功能的健壮运行。
// ============================================================================

package singbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vpnview/internal/domain"
	"vpnview/internal/identity"
	"vpnview/internal/port"
)

// ClashClient 负责调度 Sing-box 内置的 Clash 兼容 API 终点进行全局状态度量。
type ClashClient struct {
	baseURL string       // API 控制根入口（如 http://127.0.0.1:9090）
	secret  string       // Bearer Token 访问凭证
	client  *http.Client // 带超时控制的 HTTP 发起端
}

// NewClashClient 实例化创建一个 ClashClient。
func NewClashClient(baseURL, secret string) *ClashClient {
	return &ClashClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetTraffic 获取全局实时的上传和下载速率。
// 利用 Clash 的 SSE (Server-Sent Events) 单向推送流，读取第一条推送数据即算得当秒速率。
func (c *ClashClient) GetTraffic(ctx context.Context) (*port.GlobalSpeed, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/traffic", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.statusError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	// 读取流首行，代表一秒内的速率指标
	if scanner.Scan() {
		var item struct {
			Up   int64 `json:"up"`
			Down int64 `json:"down"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, err
		}
		return &port.GlobalSpeed{Up: item.Up, Down: item.Down}, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &port.GlobalSpeed{}, nil
}

// GetConnections 获取当前正在运行的全部活跃连接记录，调用 Clash 接口 `GET /connections`。
func (c *ClashClient) GetConnections(ctx context.Context) ([]port.ActiveConnection, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/connections", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.statusError(resp)
	}

	var payload struct {
		Connections []clashConnection `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]port.ActiveConnection, 0, len(payload.Connections))
	for _, conn := range payload.Connections {
		out = append(out, conn.toPort())
	}
	return out, nil
}

// KillConnection 阻断切断指定的网络连接。调用 `DELETE /connections/{id}`。
func (c *ClashClient) KillConnection(ctx context.Context, connID string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/connections/"+url.PathEscape(connID), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp)
	}
	return nil
}

// ReloadConfig 热更新重载底层代理配置文件。调用 `PUT /configs?force=true`。
func (c *ClashClient) ReloadConfig(ctx context.Context, path string) error {
	body, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPut, "/configs?force=true", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp)
	}
	return nil
}

// newRequest 创建一个注入 Bearer Token 鉴权的 HTTP 请求包。
func (c *ClashClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return req, nil
}

func (c *ClashClient) statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("Clash API 响应异常: %s, 详情: %s", resp.Status, strings.TrimSpace(string(body)))
}

// clashConnection 映射自 Clash `/connections` 返回的单个网络连接的 JSON 对象。
type clashConnection struct {
	ID          string         `json:"id"`
	Upload      int64          `json:"upload"`
	Download    int64          `json:"download"`
	Start       time.Time      `json:"start"`
	Metadata    map[string]any `json:"metadata"`
	Chains      []string       `json:"chains"`
	Rule        string         `json:"rule"`
	RulePayload string         `json:"rulePayload"`
}

// QueryTrafficFromConnections 🏆 流量统计降级算法。
// 流程：
//  1. 通过 /connections 获取所有活动连接及其实时消耗的总流量值（Upload/Download）。
//  2. 遍历连接，通过 toPort 转换机制，抓取 identity.Resolver 并将 IP、标签等解构翻译出用户 ID。
//  3. 过滤掉无法确定归宿的临时连接（防止垃圾连接污染数据库）。
//  4. 将属于同一个用户的所有连接的累计流量在内存中求和，重构汇总为 TrafficSnapshot 快照列表返回。
func (c *ClashClient) QueryTrafficFromConnections(ctx context.Context) ([]port.TrafficSnapshot, error) {
	conns, err := c.GetConnections(ctx)
	if err != nil {
		return nil, err
	}

	byUser := make(map[string]*port.TrafficSnapshot)
	for _, conn := range conns {
		uid := conn.UserID
		if uid == "" || strings.HasPrefix(uid, "IP:") {
			continue // 丢弃无法确定归档的匿名/IP 临时绑定连接，防污染流量落库
		}
		snap := byUser[uid]
		if snap == nil {
			snap = &port.TrafficSnapshot{UserID: uid}
			byUser[uid] = snap
		}
		snap.Upload += conn.Upload
		snap.Download += conn.Download
	}

	out := make([]port.TrafficSnapshot, 0, len(byUser))
	for _, snap := range byUser {
		out = append(out, *snap)
	}
	return out, nil
}

// toPort 将 Clash API 连接结构高聚集成标准的 port.ActiveConnection 数据模型。
// 核心 identity 调度链：
//  1. 抓取连接元数据，收集 identity.Hint 线索（来自元数据、分流链、来路 IP）。
//  2. 提交给 `singboxIdentityResolver()` 按照优先级（InboundUser -> RouteTag 路由翻译 -> IP 反查映射）识别。
//  3. 解出 UserID，如无法解出，采用 `IP:192.168...` 作为标识提示，确保长连接管控无遗漏。
func (c clashConnection) toPort() port.ActiveConnection {
	sourceIP := stringFromMeta(c.Metadata, "sourceIP")
	network := stringFromMeta(c.Metadata, "network")
	source := joinHostPort(sourceIP, stringFromMeta(c.Metadata, "sourcePort"))
	host := stringFromMeta(c.Metadata, "host")
	if host == "" {
		host = stringFromMeta(c.Metadata, "destinationIP")
	}
	destination := joinHostPort(host, stringFromMeta(c.Metadata, "destinationPort"))

	slog.Debug("Clash 活动连接元数据", "connID", c.ID, "metadata", c.Metadata)

	// 🏆 呼叫身份解析器
	userID := singboxIdentityResolver().Resolve(c.identityHints(sourceIP))
	if userID == "" && sourceIP != "" {
		userID = "IP:" + sourceIP
	}

	return port.ActiveConnection{
		ID:          c.ID,
		UserID:      userID,
		Upload:      c.Upload,
		Download:    c.Download,
		Start:       c.Start,
		Network:     network,
		Source:      source,
		Destination: destination,
	}
}

func stringFromMeta(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	switch v := meta[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return ""
	}
}

// identityHints 将连接的多维信息，平铺搜集为身份识别 Hint 线索集。
func (c clashConnection) identityHints(sourceIP string) []identity.Hint {
	hints := make([]identity.Hint, 0, len(c.Metadata)+len(c.Chains)+1)
	for key, value := range c.Metadata {
		if s := metaValueString(value); s != "" {
			hints = append(hints, identity.Hint{
				Source: identity.HintMetadata,
				Key:    key,
				Value:  s,
			})
		}
	}
	for _, chain := range c.Chains {
		hints = append(hints, identity.Hint{
			Source: identity.HintChain,
			Value:  chain,
		})
	}
	if sourceIP != "" {
		hints = append(hints, identity.Hint{
			Source: identity.HintIP,
			Value:  sourceIP,
		})
	}
	return hints
}

// singboxIdentityResolver 针对 Sing-box 运行特征量身定制的身份判定器。
func singboxIdentityResolver() identity.Resolver {
	return identity.Resolver{
		// 授信的元数据 Key，Sing-box 常用 "inbound_user" 携带协议用户名
		MetadataKeys:    []string{"inbound_user", "auth_user", "user", "name"},
		RouteTagDecoder: userIDFromRouteTag, // 路由标签解码器（vpnview-user-[hex] -> UserID）
		IPResolver:      domain.GetUserByIP, // 订阅时 IP 映射缓存反查
		AllowIPFallback: true,               // 支持 IP 兜底
	}
}

func metaValueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return ""
	}
}

func joinHostPort(host, port string) string {
	if host == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}
