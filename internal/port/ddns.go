// ============================================================================
// 文件说明：internal/port/ddns.go
// 职责概览：定义了动态 DNS（DDNS）提供商的抽象端口接口（port.DDNSProvider）。
//           任何特定的 DNS 解析服务商（如 Cloudflare, Aliyun 等）通过实现此接口
//           对接系统，从而支持定时上报更新本机公网 IP 地址。
// ============================================================================

package port

import "context"

// DDNSProvider 定义了外部动态 DNS 服务解析记录的抽象接口。
type DDNSProvider interface {
	// GetCurrentIP 探测并获取本机当前的公网广域网 IP 地址（可能是 IPv4 或 IPv6）。
	GetCurrentIP(ctx context.Context) (string, error)

	// UpdateDNSRecord 将远端域名托管服务商处对应的 A/AAAA 记录的 IP 地址修改为给定的公网 IP。
	UpdateDNSRecord(ctx context.Context, ip string) error
}
