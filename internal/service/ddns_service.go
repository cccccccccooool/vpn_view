package service

import (
	"context"
	"log/slog"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/port"
)

type DDNSService struct {
	provider port.DDNSProvider
	cfg      *config.DDNSConfig
	interval time.Duration
	cachedIP string
}

func NewDDNSService(provider port.DDNSProvider, cfg *config.DDNSConfig, interval time.Duration) *DDNSService {
	return &DDNSService{
		provider: provider,
		cfg:      cfg,
		interval: interval,
	}
}

func (s *DDNSService) Start(ctx context.Context) {
	if s.provider == nil || s.cfg == nil {
		slog.Info("DDNS disabled")
		return
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.check(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

func (s *DDNSService) check(ctx context.Context) {
	ip, err := s.provider.GetCurrentIP(ctx)
	if err != nil {
		slog.Error("DDNS public IP check failed", "err", err)
		return
	}

	record, err := s.provider.GetDNSRecord(ctx)
	if err != nil {
		slog.Error("DDNS remote record check failed", "err", err)
		return
	}
	if record.Content == ip {
		s.cachedIP = ip
		return
	}

	slog.Info("DDNS remote record differs from current public IP", "old_ip", record.Content, "new_ip", ip)
	updated, err := s.provider.UpdateDNSRecord(ctx, ip)
	if err != nil {
		slog.Error("DDNS remote record update failed", "err", err)
		return
	}

	s.cachedIP = updated.Content
	slog.Info("DDNS remote record updated", "domain", updated.Name, "record_type", updated.Type, "ip", updated.Content)
}
