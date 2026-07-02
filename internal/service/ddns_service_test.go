package service

import (
	"context"
	"testing"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/port"
)

type fakeDDNSProvider struct {
	currentIP string
	record    port.DDNSRecord
	updates   int
}

func (p *fakeDDNSProvider) GetCurrentIP(ctx context.Context) (string, error) {
	return p.currentIP, nil
}

func (p *fakeDDNSProvider) GetDNSRecord(ctx context.Context) (port.DDNSRecord, error) {
	return p.record, nil
}

func (p *fakeDDNSProvider) UpdateDNSRecord(ctx context.Context, ip string) (port.DDNSRecord, error) {
	p.updates++
	p.record.Content = ip
	return p.record, nil
}

func TestDDNSCheckSkipsUpdateWhenRemoteRecordMatches(t *testing.T) {
	provider := &fakeDDNSProvider{
		currentIP: "8.8.8.8",
		record:    port.DDNSRecord{Type: "A", Name: "vpn.example.com", Content: "8.8.8.8"},
	}
	service := NewDDNSService(provider, &config.DDNSConfig{}, time.Minute)

	service.check(context.Background())

	if provider.updates != 0 {
		t.Fatalf("updates = %d, want 0", provider.updates)
	}
	if service.cachedIP != "8.8.8.8" {
		t.Fatalf("cachedIP = %q", service.cachedIP)
	}
}

func TestDDNSCheckUpdatesWhenRemoteRecordDiffers(t *testing.T) {
	provider := &fakeDDNSProvider{
		currentIP: "8.8.4.4",
		record:    port.DDNSRecord{Type: "A", Name: "vpn.example.com", Content: "8.8.8.8"},
	}
	service := NewDDNSService(provider, &config.DDNSConfig{}, time.Minute)

	service.check(context.Background())

	if provider.updates != 1 {
		t.Fatalf("updates = %d, want 1", provider.updates)
	}
	if service.cachedIP != "8.8.4.4" {
		t.Fatalf("cachedIP = %q", service.cachedIP)
	}
}
