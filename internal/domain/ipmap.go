// ============================================================================
// 文件说明：internal/domain/ipmap.go
// 职责概览：提供一个全局线程安全的运行时 IP 到用户 UUID 的内存高速缓存映射表（IPUserMap）。
//           专门用作当底层网络协议（如 VLESS）在建立网络连接时，不直接在连接元数据中携带
//           用户标识的场景，主程序可以通过此映射，使用客户端来源 IP 反查并精确定位对应的 VPN 用户账户。
// ============================================================================

package domain

import (
	"strings"
	"sync"
)

var (
	// IPUserMap 动态存储网络客户端的来源真实 IP 地址到面板用户唯一标识（如 UUID 或 ID）的映射关系。
	// 使用并发安全的 sync.Map，防止高并发连接写入和反查时的读写锁竞争。
	IPUserMap sync.Map
)

// RecordUserIP 记录或更新客户端 IP 到面板用户 ID 标识的绑定映射关系。
func RecordUserIP(ip, userID string) {
	ip = strings.TrimSpace(ip)
	userID = strings.TrimSpace(userID)
	if ip != "" && userID != "" {
		IPUserMap.Store(ip, userID)
	}
}

// GetUserByIP 通过客户端请求的具体来源 IP 地址反向查找并返回其归宿绑定的面板用户唯一 ID 标识。
// 如果查找不到绑定或传入参数无效，则直接返回空字符串 ""。
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
