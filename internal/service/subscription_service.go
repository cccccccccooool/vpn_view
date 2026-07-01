package service

import (
	"context"

	"vpnview/internal/config"
	"vpnview/internal/core"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

type SubscriptionService struct {
	cores *core.Manager
	cfg   config.SubscriptionConfig
}

func NewSubscriptionService(cores *core.Manager, cfg config.SubscriptionConfig) *SubscriptionService {
	return &SubscriptionService{cores: cores, cfg: cfg}
}

func (s *SubscriptionService) Generate(ctx context.Context, user *domain.User) ([]byte, string, error) {
	adapter, _, err := s.cores.SelectForUser(user)
	if err != nil {
		return nil, "", err
	}
	if !adapter.Capabilities().Has(domain.CapSubscription) {
		return nil, "", domain.ErrNotSupported
	}
	provider, ok := adapter.(port.SubscriptionProvider)
	if !ok {
		return nil, "", domain.ErrNotSupported
	}
	return provider.GenerateSubscription(ctx, user.ID, user.Credentials)
}
