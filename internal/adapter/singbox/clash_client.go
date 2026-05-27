// clash_client.go 封装了 Clash RESTful API 的 HTTP 客户端，
// 提供实时速率查询、活跃连接管理、配置重载和流量聚合等功能。
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
	"vpnview/internal/port"
)

// ClashClient 是 Clash RESTful API 的 HTTP 客户端。
// 用于与 sing-box 内置的 Clash 兼容 API 进行交互。
type ClashClient struct {
	baseURL string       // Clash API 基础地址，例如 http://127.0.0.1:9090
	secret  string       // API 鉴权密钥（Bearer Token）
	client  *http.Client // 底层 HTTP 客户端
}

// NewClashClient 创建一个新的 ClashClient 实例。
// baseURL 为 Clash API 地址，secret 为鉴权密钥（可为空）。
func NewClashClient(baseURL, secret string) *ClashClient {
	return &ClashClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetTraffic 获取全局实时上传和下载速率 (来自 clash API /traffic)
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
	// /traffic 接口是一个 SSE (Server-Sent Events) 流或者分块 JSON 流
	// 我们只需要读取第一条数据即可代表当前这一秒的实时速率
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

// GetConnections 获取当前所有活跃连接列表（来自 Clash API GET /connections）。
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

// KillConnection 通过 ID 切断一个已建立的活跃连接 (来自 clash API DELETE /connections/{id})
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

// ReloadConfig 调用 clash API 重载底层 sing-box 配置文件
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

// newRequest 创建一个带 context 和鉴权 header 的 HTTP 请求。
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

// statusError 从非 2xx 响应中提取错误信息，返回格式化的错误。
func (c *ClashClient) statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("clash api returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

// clashConnection 是 Clash API /connections 端点返回的单个连接的 JSON 结构映射。
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

// QueryTrafficFromConnections 通过聚合 /connections 端点的所有连接数据，
// 按用户汇总流量快照。这是当 V2Ray Stats gRPC API 不可用时的降级备选方案。
// 注意：这些是累积值（非增量），调用方需要自行计算差值。
func (c *ClashClient) QueryTrafficFromConnections(ctx context.Context) ([]port.TrafficSnapshot, error) {
	conns, err := c.GetConnections(ctx)
	if err != nil {
		return nil, err
	}

	byUser := make(map[string]*port.TrafficSnapshot)
	for _, conn := range conns {
		uid := conn.UserID
		if uid == "" || strings.HasPrefix(uid, "IP:") {
			continue // 跳过无法识别或仅以 IP 标记的临时用户，防止污染流量记录数据库
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

// toPort 将 Clash API 的连接结构转换为 port.ActiveConnection 通用模型。
func (c clashConnection) toPort() port.ActiveConnection {
	sourceIP := stringFromMeta(c.Metadata, "sourceIP")
	network := stringFromMeta(c.Metadata, "network")
	source := joinHostPort(sourceIP, stringFromMeta(c.Metadata, "sourcePort"))
	host := stringFromMeta(c.Metadata, "host")
	if host == "" {
		host = stringFromMeta(c.Metadata, "destinationIP")
	}
	destination := joinHostPort(host, stringFromMeta(c.Metadata, "destinationPort"))

	slog.Debug("clash connection metadata", "connID", c.ID, "metadata", c.Metadata)

	// sing-box Clash API 使用 metadata.auth_user 来标识认证用户
	userID := stringFromMeta(c.Metadata, "inbound_user")
	if userID == "" {
		userID = stringFromMeta(c.Metadata, "auth_user")
	}
	if userID == "" {
		userID = stringFromMeta(c.Metadata, "user")
	}
	if userID == "" {
		userID = stringFromMeta(c.Metadata, "name")
	}
	if userID == "" {
		userID = userIDFromChains(c.Chains)
	}
	// 🏆 降级方案一：通过已记录的 IP-User 面板用户映射动态反查
	if userID == "" && sourceIP != "" {
		userID = domain.GetUserByIP(sourceIP)
	}
	// 🏆 降级方案二：若仍完全无法匹配，则通过 IP 分组，使大盘能够按 IP 进行物理隔离展示
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

// stringFromMeta 从 metadata map 中安全提取指定 key 的字符串值。
// 支持 string、float64、int 类型的自动转换。
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

func userIDFromChains(chains []string) string {
	for _, chain := range chains {
		if userID := userIDFromRouteTag(chain); userID != "" {
			return userID
		}
	}
	return ""
}

// joinHostPort 将主机名和端口拼接为 host:port 格式，空值时优雅处理。
func joinHostPort(host, port string) string {
	if host == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}
