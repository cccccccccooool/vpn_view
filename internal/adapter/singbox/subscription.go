// subscription.go 负责为用户生成代理客户端订阅链接。
// 支持 vless、trojan 和 shadowsocks 三种协议的 URI 格式。
package singbox

import (
	"encoding/base64"
	"fmt"
	"net/url"
)

// SubscriptionBuilder 根据适配器配置为用户构建订阅链接。
// 支持 vless://、trojan:// 和 ss:// 三种 URI 格式。
type SubscriptionBuilder struct {
	cfg Config // 用于获取域名、端口、TLS 等订阅相关配置
}

// NewSubscriptionBuilder 创建一个新的 SubscriptionBuilder 实例。
func NewSubscriptionBuilder(cfg Config) *SubscriptionBuilder {
	return &SubscriptionBuilder{cfg: cfg}
}

// Enabled 返回订阅功能是否已启用。需要配置 SubscriptionDomain 才会启用。
func (b *SubscriptionBuilder) Enabled() bool {
	return b.cfg.SubscriptionDomain != ""
}

// Build 根据用户凭证生成订阅链接。
// 返回值依次为：订阅 URI 内容（字节）、MIME 类型（text/plain）、错误。
// 根据 credentials["protocol"] 自动选择 vless/trojan/shadowsocks URI 格式。
func (b *SubscriptionBuilder) Build(userID string, credentials map[string]string) ([]byte, string, error) {
	proto := credentials["protocol"]
	if proto == "" {
		proto = "vless" // 默认降级为 vless
	}

	var uri string
	switch proto {
	case "trojan":
		password := credentials["password"]
		if password == "" {
			password = userID
		}
		q := url.Values{}
		if b.cfg.SubscriptionTLS {
			q.Set("security", "tls")
		}
		uri = fmt.Sprintf("trojan://%s@%s:%d?%s#%s",
			url.PathEscape(password),
			b.cfg.SubscriptionDomain,
			b.cfg.SubscriptionPort,
			q.Encode(),
			url.PathEscape(userID),
		)

	case "shadowsocks":
		method := credentials["ss_method"]
		if method == "" {
			method = "256-gcm"
		}
		password := credentials["ss_password"]
		if password == "" {
			password = userID
		}
		// ss://BASE64(method:password)@domain:port#tag
		userInfo := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", method, password)))
		uri = fmt.Sprintf("ss://%s@%s:%d#%s",
			userInfo,
			b.cfg.SubscriptionDomain,
			b.cfg.SubscriptionPort,
			url.PathEscape(userID),
		)

	default: // vless
		uuid := credentials["uuid"]
		if uuid == "" {
			uuid = userID
		}
		q := url.Values{}
		q.Set("encryption", "none")
		if b.cfg.SubscriptionTLS {
			q.Set("security", "tls")
		}
		if b.cfg.SubscriptionType != "" {
			q.Set("type", b.cfg.SubscriptionType)
		}
		if flow := credentials["flow"]; flow != "" {
			q.Set("flow", flow)
		}
		uri = fmt.Sprintf("vless://%s@%s:%d?%s#%s",
			url.PathEscape(uuid),
			b.cfg.SubscriptionDomain,
			b.cfg.SubscriptionPort,
			q.Encode(),
			url.PathEscape(userID),
		)
	}

	b64 := base64.StdEncoding.EncodeToString([]byte(uri))
	return []byte(b64), "text/plain; charset=utf-8", nil
}
