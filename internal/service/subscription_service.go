// subscription_service.go 实现用户订阅链接的生成功能。

package service

import (
	"context"

	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// SubscriptionService 负责为用户生成 VPN 客户端订阅内容。
// 它依赖适配器的 CapSubscription 能力来生成对应协议的订阅链接。
type SubscriptionService struct {
	adapter port.VPNAdapter
	cfg     config.SubscriptionConfig
}

// NewSubscriptionService 创建并返回一个新的 SubscriptionService 实例。
func NewSubscriptionService(adapter port.VPNAdapter, cfg config.SubscriptionConfig) *SubscriptionService {
	return &SubscriptionService{adapter: adapter, cfg: cfg}
}

// Generate 为指定用户生成订阅内容。
// 返回值依次为：订阅数据（[]byte）、Content-Type 和可能的错误。
// 若适配器不支持 CapSubscription 能力，返回 domain.ErrNotSupported。
func (s *SubscriptionService) Generate(ctx context.Context, user *domain.User) ([]byte, string, error) {
	if s.adapter.Capabilities().Has(domain.CapSubscription) {
		return s.adapter.GenerateSubscription(ctx, user.ID, user.Credentials)
	}
	return nil, "", domain.ErrNotSupported
}
