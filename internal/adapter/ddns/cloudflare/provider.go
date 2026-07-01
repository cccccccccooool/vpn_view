package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"vpnview/internal/config"
)

type Provider struct {
	cfg    *config.DDNSConfig
	client *http.Client
}

func New(cfg *config.DDNSConfig) *Provider {
	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (p *Provider) GetCurrentIP(ctx context.Context) (string, error) {
	if p.cfg == nil {
		return "", fmt.Errorf("cloudflare DDNS config is nil")
	}
	var lastErr error
	for _, url := range p.cfg.IPCheckURLs {
		ip, err := p.queryIP(ctx, url)
		if err == nil {
			return ip, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no DDNS public IP resolver is configured")
}

func (p *Provider) UpdateDNSRecord(ctx context.Context, ip string) error {
	if p.cfg == nil || p.cfg.ZoneID == "" || p.cfg.RecordID == "" || p.cfg.APIToken == "" || p.cfg.Domain == "" {
		return fmt.Errorf("cloudflare DDNS config requires zone_id, record_id, api_token, and domain")
	}
	recordType, err := normalizeRecordType(p.cfg.RecordType)
	if err != nil {
		return err
	}
	if err := validateIPForRecord(ip, recordType); err != nil {
		return err
	}

	payload := map[string]any{
		"type":    recordType,
		"name":    p.cfg.Domain,
		"content": strings.TrimSpace(ip),
		"ttl":     p.cfg.TTL,
		"proxied": p.cfg.Proxied,
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
		return fmt.Errorf("cloudflare API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *Provider) queryIP(ctx context.Context, url string) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("empty DDNS public IP resolver URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("public IP resolver %s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	recordType, err := normalizeRecordType(p.cfg.RecordType)
	if err != nil {
		return "", err
	}
	if err := validateIPForRecord(ip, recordType); err != nil {
		return "", fmt.Errorf("public IP resolver %s: %w", url, err)
	}
	return ip, nil
}

func normalizeRecordType(raw string) (string, error) {
	recordType := strings.ToUpper(strings.TrimSpace(raw))
	if recordType == "" {
		recordType = "A"
	}
	if recordType != "A" && recordType != "AAAA" {
		return "", fmt.Errorf("unsupported Cloudflare DDNS record type: %s", recordType)
	}
	return recordType, nil
}

func validateIPForRecord(ip string, recordType string) error {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.IsGlobalUnicast() {
		return fmt.Errorf("invalid public IP %q", ip)
	}
	if recordType == "A" && !addr.Is4() {
		return fmt.Errorf("record type A requires an IPv4 address, got %s", ip)
	}
	if recordType == "AAAA" && !addr.Is6() {
		return fmt.Errorf("record type AAAA requires an IPv6 address, got %s", ip)
	}
	return nil
}
