// Package cloudflare 提供了基于 Cloudflare API 的 DDNS 提供商实现。
// Cloudflare-based DDNS provider using the v4 API.
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

// Provider 是 Cloudflare DDNS 提供商，通过 Cloudflare API v4 更新 DNS 记录。
type Provider struct {
	cfg    *config.DDNSConfig // DDNS 配置（Zone ID、Record ID、API Token 等）
	client *http.Client       // 带超时的 HTTP 客户端
}

// New 创建一个新的 Cloudflare DDNS 提供商实例，HTTP 超时设为 15 秒。
func New(cfg *config.DDNSConfig) *Provider {
	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetCurrentIP 通过 ipify.org 服务获取当前主机的公网 IPv4 地址。
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
		return "", fmt.Errorf("ip service returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("empty public ip response")
	}
	return ip, nil
}

// UpdateDNSRecord 通过 Cloudflare API v4 更新指定域名的 A 记录为给定 IP。
// 需要配置中包含有效的 ZoneID、RecordID、APIToken 和 Domain。
func (p *Provider) UpdateDNSRecord(ctx context.Context, ip string) error {
	if p.cfg == nil || p.cfg.ZoneID == "" || p.cfg.RecordID == "" || p.cfg.APIToken == "" || p.cfg.Domain == "" {
		return fmt.Errorf("incomplete cloudflare ddns config")
	}

	payload := map[string]any{
		"type":    "A",
		"name":    p.cfg.Domain,
		"content": ip,
		"ttl":     1,
		"proxied": false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", p.cfg.ZoneID, p.cfg.RecordID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("cloudflare update failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
