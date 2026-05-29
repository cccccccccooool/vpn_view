// ============================================================================
// 文件说明：internal/adapter/singbox/subscription.go
// 职责概览：实现针对 Sing-box 代理环境的多协议节点订阅生成器（SubscriptionBuilder）。
//           依据传递的用户 Credentials，自动识别协议类型（VLESS, Trojan, Shadowsocks），
//           按照通用的 URI 节点规范（如 `vless://uuid@domain:port?params#userID` 等）
//           进行字符串转义与格式拼接，最后进行标准化 Base64 安全编码下发，
//           保障各种客户端（Shadowrocket, Clash, V2rayN）一键拉取。
// ============================================================================

package singbox

import (
	"encoding/base64"
	"fmt"
	"net/url"
)

// SubscriptionBuilder 基于适配器全局配置为用户提供安全的多协议 URI 格式订阅链接下发。
type SubscriptionBuilder struct {
	cfg Config // 获取订阅绑定的连接域名、公网端口、TLS 指标及传输层属性
}

// NewSubscriptionBuilder 实例化创建一个 SubscriptionBuilder。
func NewSubscriptionBuilder(cfg Config) *SubscriptionBuilder {
	return &SubscriptionBuilder{cfg: cfg}
}

// Enabled 返回当前订阅链接下发功能是否具备激活条件（必须配置了 SubscriptionDomain 才算激活）。
func (b *SubscriptionBuilder) Enabled() bool {
	return b.cfg.SubscriptionDomain != ""
}

// Build 生成特定用户专属的节点链接。
// 支持以下三种连接协议的自动组装：
//  1. Trojan 协议：`trojan://[Password]@[Domain]:[Port]?security=tls#[UserID]`
//  2. Shadowsocks 协议：`ss://[Base64Url(Method:Password)]@[Domain]:[Port]#[UserID]`
//  3. VLESS 协议：`vless://[UUID]@[Domain]:[Port]?encryption=none&security=tls&type=tcp&flow=xtls-rprx-vision#[UserID]`
// 生成后，全量使用 Base64 进行 Standard 编码，符合业界订阅服务器返回规范。
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
			method = "256-gcm" // 缺省加密
		}
		password := credentials["ss_password"]
		if password == "" {
			password = userID
		}
		// SS 节点标准定义
		userInfo := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", method, password)))
		uri = fmt.Sprintf("ss://%s@%s:%d#%s",
			userInfo,
			b.cfg.SubscriptionDomain,
			b.cfg.SubscriptionPort,
			url.PathEscape(userID),
		)

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

	// 最终进行 Base64 编码，适配客户端订阅格式
	b64 := base64.StdEncoding.EncodeToString([]byte(uri))
	return []byte(b64), "text/plain; charset=utf-8", nil
}
