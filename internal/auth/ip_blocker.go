// ============================================================================
// 文件说明：internal/auth/ip_blocker.go
// 职责概览：实现系统管理面板防暴力破解的 IP 智能封禁中心（IPBlocker）。
//           主要在内存中追踪各 IP 请求登录失败的频次。如果单 IP 连续密码失败达到 3 次，
//           系统自动将此 IP 拉入面板黑名单并拒绝服务，同时跨平台联动调用系统防火墙
//           （Linux 下调用 iptables，Windows 下调用 netsh advfirewall）从物理网络层
//           强行掐断并封锁该 IP 传入的全部流量，实现硬防暴破。
//           特权免封：服务启动时会自动扫描服务器所有的本地物理网卡 IP 与 127.0.0.1 回环地址，
//           自动加入白名单豁免区，防止因管理员输错密码导致服务器远程联络自锁的悲剧发生。
// ============================================================================

package auth

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// IPBlocker 基于内存并发安全的安全防护拦截中心。
type IPBlocker struct {
	mu          sync.RWMutex
	attempts    map[string]int  // 统计每个来源 IP 连续失败的计数器表（IP -> 连续密码失败次数）
	blacklist   map[string]bool // 面板级拦截黑名单（IP -> 是否封禁）
	exemptedIPs map[string]bool // 白名单豁免名单（IP -> 是否免受拦截惩罚）
}

// NewIPBlocker 创建并实例化一个新的 IP 拦截防护拦截中心。
// 会主动通过 `net.InterfaceAddrs()` 搜寻本地网络物理地址，全量加入豁免名单以保护系统自锁。
func NewIPBlocker() *IPBlocker {
	b := &IPBlocker{
		attempts:    make(map[string]int),
		blacklist:   make(map[string]bool),
		exemptedIPs: make(map[string]bool),
	}

	// 1. 强制豁免本地环回接口，保障宿主机内部进程通讯畅通
	b.exemptedIPs["127.0.0.1"] = true
	b.exemptedIPs["::1"] = true

	// 2. 跨网卡扫描，对服务器本地所有的物理网卡 IP 进行白名单特权豁免，杜绝自锁
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				ipStr := ipNet.IP.String()
				// 去除掩码后缀以获取纯 IP 地址
				if idx := strings.Index(ipStr, "/"); idx != -1 {
					ipStr = ipStr[:idx]
				}
				b.exemptedIPs[ipStr] = true
				slog.Info("安全白名单自动豁免本机网卡物理 IP", "ip", ipStr)
			}
		}
	}
	return b
}

// GetClientIP 从 HTTP 请求报文中层层解构，提取客户端最为真实可信的公网源 IP 地址。
// 支持识别常规的反向代理头（X-Forwarded-For, X-Real-IP）。
func (b *IPBlocker) GetClientIP(r *http.Request) string {
	// 优先提取 X-Forwarded-For 代理链，取逗号分割后的第一个真实发起者 IP
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	// 其次检测 X-Real-IP 代理头
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	// 无反向代理时，退回网络底层连接直连来源 IP
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// IsBlocked 核对指定客户端 IP 当前是否已处于系统内存黑名单中。
func (b *IPBlocker) IsBlocked(ip string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.blacklist[ip]
}

// RecordFailure 登记并递增某 IP 发生的一次无效密码校验尝试。
// 流程：
//  1. 比对免锁白名单，若是白名单 IP，直接忽略不记录。
//  2. 连续失败次数达到 3 次，开启硬封禁：
//     - 放入 blacklist 内存黑名单。
//     - 跨平台自动通过命令行调用操作系统的硬件防火墙（iptables 或 netsh）添加丢弃该 IP 一切流入连接的硬规则。
func (b *IPBlocker) RecordFailure(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 特权防护：若是本机物理 IP 或特许回环地址，绝对豁免任何暴破封禁处罚
	if b.exemptedIPs[ip] {
		slog.Info("白名单特权 IP 发生密码尝试校验失败，豁免封锁拦截", "ip", ip)
		return
	}

	b.attempts[ip]++
	count := b.attempts[ip]
	slog.Warn("管理员密码校验尝试失败", "ip", ip, "连续失败次数", count)

	if count >= 3 {
		b.blacklist[ip] = true
		slog.Error("🚨 严重警告：发现外部 IP 密码连续错误达到 3 次！系统已将该 IP 封锁，并联动底层物理防火墙实施包丢弃拦截防御！", "封锁 IP", ip)

		// 调用物理防火墙进行拦截
		b.blockAtSystemFirewall(ip)
	}
}

// ResetAttempts 当客户端完成正确的密码鉴权成功登录后，清除其历史累计的连续失败计数器。
func (b *IPBlocker) ResetAttempts(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.attempts, ip)
}

// blockAtSystemFirewall 跨平台调用系统的防火墙控制命令行执行强制的物理端口流量阻断。
func (b *IPBlocker) blockAtSystemFirewall(ip string) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		slog.Error("欲封禁的 IP 地址语法格式非法，终止防火墙联动拦截", "ip", ip)
		return
	}

	var cmd *exec.Cmd
	osType := runtime.GOOS

	switch osType {
	case "linux":
		// Linux 使用经典 iptables 规则将该 IP 来源的所有流量插入到 INPUT 链首部并直接 DROP 丢弃
		cmd = exec.Command("iptables", "-I", "INPUT", "-s", ip, "-j", "DROP")
	case "windows":
		// Windows 使用 netsh advfirewall 添加强制阻断流入规则
		ruleName := fmt.Sprintf("VPNViewBlock_%s", ip)
		cmd = exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+ruleName, "dir=in", "action=block", "remoteip="+ip)
	default:
		slog.Warn("当前底层操作系统平台未支持物理防火墙联动控制，已自动降级为单纯内置内存黑名单拦截机制", "os", osType)
		return
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("物理防火墙联动拦截规则应用发生异常", "err", err, "命令行输出", strings.TrimSpace(string(out)))
	} else {
		slog.Info("物理防火墙联防阻断规则应用成功，外部威胁流量已掐断", "封禁 IP", ip)
	}
}
