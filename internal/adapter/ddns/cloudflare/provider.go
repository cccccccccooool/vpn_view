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
	"vpnview/internal/port"
)

const defaultAPIBaseURL = "https://api.cloudflare.com/client/v4"

type Provider struct {
	cfg        *config.DDNSConfig
	client     *http.Client
	apiBaseURL string
}

func New(cfg *config.DDNSConfig) *Provider {
	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		apiBaseURL: defaultAPIBaseURL,
	}
}

type cloudflareRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareRecordResponse struct {
	Success bool              `json:"success"`
	Errors  []cloudflareError `json:"errors"`
	Result  cloudflareRecord  `json:"result"`
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

func (p *Provider) GetDNSRecord(ctx context.Context) (port.DDNSRecord, error) {
	if err := p.validateConfig(); err != nil {
		return port.DDNSRecord{}, err
	}
	recordType, err := normalizeRecordType(p.cfg.RecordType)
	if err != nil {
		return port.DDNSRecord{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.recordURL(), nil)
	if err != nil {
		return port.DDNSRecord{}, err
	}
	p.setHeaders(req)
	record, err := p.doRecordRequest(req)
	if err != nil {
		return port.DDNSRecord{}, err
	}
	if err := p.validateRecord(record, recordType); err != nil {
		return port.DDNSRecord{}, err
	}
	return record, nil
}

func (p *Provider) UpdateDNSRecord(ctx context.Context, ip string) (port.DDNSRecord, error) {
	if err := p.validateConfig(); err != nil {
		return port.DDNSRecord{}, err
	}
	recordType, err := normalizeRecordType(p.cfg.RecordType)
	if err != nil {
		return port.DDNSRecord{}, err
	}
	if err := validateIPForRecord(ip, recordType); err != nil {
		return port.DDNSRecord{}, err
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
		return port.DDNSRecord{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, p.recordURL(), bytes.NewReader(raw))
	if err != nil {
		return port.DDNSRecord{}, err
	}
	p.setHeaders(req)
	record, err := p.doRecordRequest(req)
	if err != nil {
		return port.DDNSRecord{}, err
	}
	if err := p.validateRecord(record, recordType); err != nil {
		return port.DDNSRecord{}, err
	}
	if record.Content != strings.TrimSpace(ip) {
		return port.DDNSRecord{}, fmt.Errorf("cloudflare record content mismatch after update: got %q, want %q", record.Content, strings.TrimSpace(ip))
	}
	return record, nil
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

func (p *Provider) validateConfig() error {
	if p.cfg == nil || p.cfg.ZoneID == "" || p.cfg.RecordID == "" || p.cfg.APIToken == "" || p.cfg.Domain == "" {
		return fmt.Errorf("cloudflare DDNS config requires zone_id, record_id, api_token, and domain")
	}
	return nil
}

func (p *Provider) recordURL() string {
	base := strings.TrimRight(p.apiBaseURL, "/")
	return fmt.Sprintf("%s/zones/%s/dns_records/%s", base, p.cfg.ZoneID, p.cfg.RecordID)
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
}

func (p *Provider) doRecordRequest(req *http.Request) (port.DDNSRecord, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return port.DDNSRecord{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return port.DDNSRecord{}, err
	}
	var parsed cloudflareRecordResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return port.DDNSRecord{}, fmt.Errorf("cloudflare API returned invalid JSON: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return port.DDNSRecord{}, fmt.Errorf("cloudflare API returned %s: %s", resp.Status, cloudflareErrorSummary(parsed.Errors))
	}
	if !parsed.Success {
		return port.DDNSRecord{}, fmt.Errorf("cloudflare API success=false: %s", cloudflareErrorSummary(parsed.Errors))
	}
	return port.DDNSRecord{
		Type:    strings.ToUpper(strings.TrimSpace(parsed.Result.Type)),
		Name:    strings.TrimSpace(parsed.Result.Name),
		Content: strings.TrimSpace(parsed.Result.Content),
		TTL:     parsed.Result.TTL,
		Proxied: parsed.Result.Proxied,
	}, nil
}

func (p *Provider) validateRecord(record port.DDNSRecord, recordType string) error {
	if !strings.EqualFold(record.Type, recordType) {
		return fmt.Errorf("cloudflare record type mismatch: got %q, want %q", record.Type, recordType)
	}
	if !strings.EqualFold(record.Name, p.cfg.Domain) {
		return fmt.Errorf("cloudflare record name mismatch: got %q, want %q", record.Name, p.cfg.Domain)
	}
	return validateIPForRecord(record.Content, recordType)
}

func cloudflareErrorSummary(errors []cloudflareError) string {
	if len(errors) == 0 {
		return "no error details"
	}
	parts := make([]string, 0, len(errors))
	for _, item := range errors {
		msg := strings.TrimSpace(item.Message)
		if msg == "" {
			msg = "unknown error"
		}
		if item.Code != 0 {
			parts = append(parts, fmt.Sprintf("%d: %s", item.Code, msg))
		} else {
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}

func validateIPForRecord(ip string, recordType string) error {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() {
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
