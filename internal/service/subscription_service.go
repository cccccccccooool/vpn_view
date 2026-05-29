// ============================================================================
// 文件说明：internal/service/subscription_service.go
// 职责概览：实现 VPN 用户客户端订阅文件的生成下发服务（SubscriptionService）。
//           当客户端请求订阅链接时，此服务负责读取并组装用户模型，
//           通过调用 VPN 适配器的 GenerateSubscription 生成特定后端支持的通用协议订阅文本，
//           提供客户端（如 Clash, Shadowrocket, V2ray）直接解析并拉取节点的能力。
// ============================================================================

package service

import (
	"context"

	"vpnview/internal/config"
	"vpnview/internal/domain"
	"vpnview/internal/port"
)

// SubscriptionService 提供安全而快速的用户订阅分发和多协议链接构建。
type SubscriptionService struct {
	adapter port.VPNAdapter           // VPN 底层适配器句柄
	cfg     config.SubscriptionConfig // 全局订阅相关的配置参数（如默认订阅域名等）
}

// NewSubscriptionService 创建并实例化一个新的 SubscriptionService 订阅服务。
func NewSubscriptionService(adapter port.VPNAdapter, cfg config.SubscriptionConfig) *SubscriptionService {
	return &SubscriptionService{adapter: adapter, cfg: cfg}
}

// Generate 根据用户的认证数据与凭据参数，生成该用户专属可接入的客户端订阅配置正文与文件响应类型。
// 流程：
//  1. 检验适配器当前是否拥有 CapSubscription 订阅生成能力。
//  2. 满足后，直接委托具体适配器执行其专有的订阅协议拼接，返回订阅字节流与 MIME 类型。
//  3. 不支持则直接向调用端返回 domain.ErrNotSupported 错误。
func (s *SubscriptionService) Generate(ctx context.Context, user *domain.User) ([]byte, string, error) {
	if s.adapter.Capabilities().Has(domain.CapSubscription) {
		provider, ok := s.adapter.(port.SubscriptionProvider)
		if !ok {
			return nil, "", domain.ErrNotSupported
		}
		return provider.GenerateSubscription(ctx, user.ID, user.Credentials)
	}
	return nil, "", domain.ErrNotSupported
}
