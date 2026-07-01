// ============================================================================
// 文件说明：internal/adapter/xray/subscription.go
// 职责概览：实现针对 Xray / V2Ray 代理环境的多协议节点订阅生成器（SubscriptionBuilder）。
//           依据用户 Credentials 自动识别协议类型（VLESS, VMess, Trojan, Shadowsocks），
//           按通用客户端 URI 规范拼接并转义节点链接，最终 Base64 标准编码下发，
//           保障 v2rayN / Shadowrocket / Clash 等主流客户端一键拉取。
// ============================================================================

package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
)

// SubscriptionBuilder 基于适配器全局配置为用户生成多协议 URI 格式订阅链接。
type SubscriptionBuilder struct {
	cfg Config
}

// NewSubscriptionBuilder 实例化创建一个 SubscriptionBuilder。
func NewSubscriptionBuilder(cfg Config) *SubscriptionBuilder {
	return &SubscriptionBuilder{cfg: cfg}
}

// Enabled 返回订阅下发功能是否具备激活条件（必须配置 SubscriptionDomain）。
func (b *SubscriptionBuilder) Enabled() bool {
	return b.cfg.SubscriptionDomain != ""
}

// Build 生成特定用户专属的节点链接，支持 VLESS / VMess / Trojan / Shadowsocks 自动组装，
// 最终整体 Base64 标准编码，符合业界订阅服务器返回规范。
func (b *SubscriptionBuilder) Build(userID string, credentials map[string]string) ([]byte, string, error) {
	proto := credentials["protocol"]
	if proto == "" {
		proto = "vless" // 默认退回主流 vless
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
		if b.cfg.SubscriptionType != "" {
			q.Set("type", b.cfg.SubscriptionType)
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
		userInfo := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", method, password)))
		uri = fmt.Sprintf("ss://%s@%s:%d#%s",
			userInfo,
			b.cfg.SubscriptionDomain,
			b.cfg.SubscriptionPort,
			url.PathEscape(userID),
		)

	case "vmess":
		// VMess 采用 v2rayN 定义的 Base64(JSON) 节点格式。
		uuid := credentials["uuid"]
		if uuid == "" {
			uuid = userID
		}
		net := b.cfg.SubscriptionType
		if net == "" {
			net = "tcp"
		}
		tls := ""
		if b.cfg.SubscriptionTLS {
			tls = "tls"
		}
		node := map[string]any{
			"v":    "2",
			"ps":   userID,
			"add":  b.cfg.SubscriptionDomain,
			"port": fmt.Sprintf("%d", b.cfg.SubscriptionPort),
			"id":   uuid,
			"aid":  "0",
			"scy":  "auto",
			"net":  net,
			"type": "none",
			"host": "",
			"path": "",
			"tls":  tls,
			"sni":  b.cfg.SubscriptionDomain,
		}
		payload, err := json.Marshal(node)
		if err != nil {
			return nil, "", err
		}
		uri = "vmess://" + base64.StdEncoding.EncodeToString(payload)

	default: // vless 协议组装
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
