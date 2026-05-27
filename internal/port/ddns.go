// ddns.go 定义了动态 DNS（DDNS）服务的端口接口。

package port

import "context"

// DDNSProvider 定义了动态 DNS 提供商的抽象接口，用于自动更新域名解析记录。
type DDNSProvider interface {
	// GetCurrentIP 获取本机当前的公网 IP 地址。
	GetCurrentIP(ctx context.Context) (string, error)
	// UpdateDNSRecord 将域名的 DNS 记录更新为指定的 IP 地址。
	UpdateDNSRecord(ctx context.Context, ip string) error
}
