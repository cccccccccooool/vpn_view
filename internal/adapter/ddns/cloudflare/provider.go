// ============================================================================
// 文件说明：internal/adapter/ddns/cloudflare/provider.go
// 职责概览：实现基于 Cloudflare API v4 接口的动态 DNS 更新适配器（Provider）。
//           对外实现 port.DDNSProvider 接口。
//           提供通过公共服务（api.ipify.org）自动探测当前服务器出口公网 IPv4 地址的方法，
//           并利用 Cloudflare PATCH API 对指定托管域名解析记录进行安全覆盖，
//           支持管理员设定的 API Token 鉴权机制。
// ============================================================================

package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vpnview/internal/config"
)

// Provider 基于 Cloudflare 官方 V4 开放接口的 DDNS 定时域名更新驱动。
type Provider struct {
	cfg    *config.DDNSConfig // 绑定的 DDNS 全局参数（Zone ID, Record ID, APIToken, Target Domain 等）
	client *http.Client       // 具有严格超时控制的专用 HTTP 请求客服端
}

// New 实例化并创建一个 Cloudflare DDNS 解析服务提供商，设定 API 超时为 15 秒以防网络拥堵阻塞。
func New(cfg *config.DDNSConfig) *Provider {
	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetCurrentIP 呼叫外部高可用的 IP 定位检测公共 API（api.ipify.org）提取本机当前的公网 IPv4 出口地址。
func (p *Provider) GetCurrentIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("公网 IP 检测接口报错，返回状态码: %s", resp.Status)
	}

	// 限制读取字节数防 DDOS 爆内存
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("公网 IP 接口返回数据为空")
	}
	return ip, nil
}

// UpdateDNSRecord 联动 Cloudflare 官方 DNS 修改 API，更新目标域名的 A 记录解析。
// 参数 ip 传入最新探测到的公网 IP。
// 规则：
//  - 默认采用 A 记录更新。
//  - 开启 TTL = 1（即自动），Proxied = false（关闭 Cloudflare CDN 橙色云朵，防止阻断 VPN 流量通道）。
//  - 基于 Bearer APIToken 鉴权头传输，PATCH 方式局部刷新。
func (p *Provider) UpdateDNSRecord(ctx context.Context, ip string) error {
	// 参数校验
	if p.cfg == nil || p.cfg.ZoneID == "" || p.cfg.RecordID == "" || p.cfg.APIToken == "" || p.cfg.Domain == "" {
		return fmt.Errorf("Cloudflare DDNS 关键配置参数不完整，请仔细核对 ZoneID/RecordID/Token")
	}

	payload := map[string]any{
		"type":    "A",
		"name":    p.cfg.Domain,
		"content": ip,
		"ttl":     1,     // TTL = 1 代表自动（Automatic）
		"proxied": false, // VPN 流量不能走 CF 代理，必须直接路由指向 IP
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Cloudflare DNS 记录修改局部 PATCH 终点
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", p.cfg.ZoneID, p.cfg.RecordID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIToken) // Token 安全鉴权
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Cloudflare 接口返回报错，状态码: %s, 详情: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
