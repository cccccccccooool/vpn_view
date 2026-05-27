// ipmap.go 提供全局线程安全的 IP 到用户（面板用户 UUID）的动态映射。
// 解决底层协议（如 VLESS）在连接元数据中不携带用户名的问题。
package domain

import (
	"strings"
	"sync"
)

var (
	// IPUserMap 存储客户端真实 IP 地址到面板用户唯一标识（UUID）的映射关系。
	IPUserMap sync.Map
)

// RecordUserIP 记录或更新客户端 IP 到面板用户唯一标识的映射关系。
func RecordUserIP(ip, userID string) {
	ip = strings.TrimSpace(ip)
	userID = strings.TrimSpace(userID)
	if ip != "" && userID != "" {
		IPUserMap.Store(ip, userID)
	}
}

// GetUserByIP 获取指定 IP 地址对应的面板用户唯一标识（如果已绑定）。
func GetUserByIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	if val, ok := IPUserMap.Load(ip); ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
