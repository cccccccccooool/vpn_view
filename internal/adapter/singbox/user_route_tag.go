// ============================================================================
// 文件说明：internal/adapter/singbox/user_route_tag.go
// 职责概览：提供将用户账户 ID 编码转换为 Sing-box 可识别的专用分流出站路由标志
//           （UserRouteTag）的工具函数包。
//           为了防止用户 ID 中包含 Sing-box 不允许的特殊字符破坏路由表规则，
//           系统自动采用 `vpnview-user-` 前缀拼接用户 ID 原始字节的 Hex 十六进制，
//           提供双向转换方法，打通了匿名协议在后端反查身份时的生命通道。
// ============================================================================

package singbox

import (
	"encoding/hex"
	"strings"
)

// 专用分流规则的 Tag 前缀标志，在出站 (outbounds) 和分流路由中 (route.rules) 强制挂载
const userRouteTagPrefix = "vpnview-user-"

// userRouteTag 将原始用户 ID 转化成 Hex 编码后挂上专用前缀，构成合法的出站 Tag。
func userRouteTag(userID string) string {
	return userRouteTagPrefix + hex.EncodeToString([]byte(userID))
}

// userIDFromRouteTag 反向解码器。将给定的出站 Tag 剥离前缀并进行 Hex 逆向解密，
// 精准还原出用户原始 ID。若非本系统标记的 Tag 或 Hex 破损则返回空字符串。
func userIDFromRouteTag(tag string) string {
	encoded, ok := strings.CutPrefix(tag, userRouteTagPrefix)
	if !ok || encoded == "" {
		return ""
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(raw)
}

// isUserRouteTag 判断某个路由规则或出站 Tag 是否属于本管理面板托管注入的用户标记路由。
func isUserRouteTag(tag string) bool {
	return strings.HasPrefix(tag, userRouteTagPrefix)
}
