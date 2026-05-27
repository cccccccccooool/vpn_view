// ddns_service.go 实现动态 DNS（DDNS）自动更新功能。
// 定时检测公网 IP 变化，并在 IP 变更时自动更新 DNS 记录。

package service

import (
	"context"
	"log/slog"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/port"
)

// DDNSService 提供 DDNS 自动更新服务。
// 它通过 DDNSProvider 检测当前公网 IP 并在发生变化时更新对应的 DNS 记录。
type DDNSService struct {
	provider port.DDNSProvider  // DDNS 服务提供商（如 Cloudflare）
	cfg      *config.DDNSConfig // DDNS 相关配置
	interval time.Duration      // 检测间隔
	cachedIP string             // 上一次检测到的公网 IP，用于判断是否发生变化
}

// NewDDNSService 创建并返回一个新的 DDNSService 实例。
// 若 provider 或 cfg 为 nil，Start 方法将直接返回不执行任何操作。
func NewDDNSService(provider port.DDNSProvider, cfg *config.DDNSConfig, interval time.Duration) *DDNSService {
	return &DDNSService{
		provider: provider,
		cfg:      cfg,
		interval: interval,
	}
}

// Start 启动 DDNS 检测的后台循环。该方法会阻塞直到 ctx 被取消。
// 若 provider 或 cfg 为 nil，方法立即返回。
func (s *DDNSService) Start(ctx context.Context) {
	if s.provider == nil || s.cfg == nil {
		slog.Info("DDNS disabled or provider nil")
		return
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 初始检查
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

// check 执行一次 DDNS 检查：获取当前公网 IP，若与缓存值不同则更新 DNS 记录。
func (s *DDNSService) check(ctx context.Context) {
	ip, err := s.provider.GetCurrentIP(ctx)
	if err != nil {
		slog.Error("ddns failed to get current IP", "err", err)
		return
	}

	if ip == s.cachedIP {
		return
	}

	slog.Info("IP changed, updating DNS record", "old", s.cachedIP, "new", ip)
	if err := s.provider.UpdateDNSRecord(ctx, ip); err != nil {
		slog.Error("ddns failed to update record", "err", err)
		return
	}

	s.cachedIP = ip
	slog.Info("DDNS update successful")
}
