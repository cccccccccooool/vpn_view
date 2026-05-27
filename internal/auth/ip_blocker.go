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

// IPBlocker 基于内存的 IP 黑名单管理器，用于防御登录暴力破解攻击。
// 追踪每个 IP 的连续失败次数，达到阈值后将其加入黑名单并联动系统防火墙进行物理封锁。
// 本机及服务器网卡 IP 会被自动加入豁免白名单，防止自锁。
type IPBlocker struct {
	mu          sync.RWMutex
	attempts    map[string]int
	blacklist   map[string]bool
	exemptedIPs map[string]bool
}

// NewIPBlocker 创建并初始化一个 IPBlocker 实例。
// 构造时自动将环回地址和本机所有网卡 IP 加入豁免白名单。
func NewIPBlocker() *IPBlocker {
	b := &IPBlocker{
		attempts:    make(map[string]int),
		blacklist:   make(map[string]bool),
		exemptedIPs: make(map[string]bool),
	}

	// 1. 默认强制白名单豁免本地环回地址，确保本机内部进程通讯绝对安全
	b.exemptedIPs["127.0.0.1"] = true
	b.exemptedIPs["::1"] = true

	// 2. 自动检测并绝对豁免当前服务器本地网卡的所有物理 IP（防止自锁）
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				ipStr := ipNet.IP.String()
				// 剔除子网掩码后缀，提取纯 IP 字符串
				if idx := strings.Index(ipStr, "/"); idx != -1 {
					ipStr = ipStr[:idx]
				}
				b.exemptedIPs[ipStr] = true
				slog.Info("安全白名单自动豁免本机网卡 IP", "ip", ipStr)
			}
		}
	}
	return b
}

// GetClientIP 从 HTTP 请求中提取客户端的真实源 IP 字符串
func (b *IPBlocker) GetClientIP(r *http.Request) string {
	// 优先取代理转发头
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	// 降级使用网络连接原生地址
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// IsBlocked 检查该 IP 是否已被拦截封锁
func (b *IPBlocker) IsBlocked(ip string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.blacklist[ip]
}

// RecordFailure 记录该 IP 登录失败一次。如果连续达到 3 次，则将其永久封禁并联动系统物理防火墙。
func (b *IPBlocker) RecordFailure(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 对属于服务器白名单范围内的 IP 直接予以绝对豁免，不做记录和拦截
	if b.exemptedIPs[ip] {
		slog.Info("白名单特权 IP 密码尝试失败，豁免安全限制", "ip", ip)
		return
	}

	b.attempts[ip]++
	count := b.attempts[ip]
	slog.Warn("密码校验失败", "ip", ip, "failed_count", count)

	if count >= 3 {
		b.blacklist[ip] = true
		slog.Error("🚨 警告：检测到 IP 密码连续错误达到 3 次！已将该 IP 列入控制面板安全黑名单，并强制启动物理防火墙防御封锁！", "ip", ip)

		// 自动跨平台联动系统物理防火墙
		b.blockAtSystemFirewall(ip)
	}
}

// ResetAttempts 密码成功登录后重置错误尝试计数
func (b *IPBlocker) ResetAttempts(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.attempts, ip)
}

// blockAtSystemFirewall 跨平台调用系统命令行执行硬性防火墙阻断
func (b *IPBlocker) blockAtSystemFirewall(ip string) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		slog.Error("IP 格式非法，中止防火墙联动", "ip", ip)
		return
	}

	var cmd *exec.Cmd
	osType := runtime.GOOS

	switch osType {
	case "linux":
		// Linux 使用 iptables 将该 IP 流入的所有流量直接丢弃 (DROP)
		cmd = exec.Command("iptables", "-I", "INPUT", "-s", ip, "-j", "DROP")
	case "windows":
		// Windows Defender 使用 netsh 防火墙添加强制阻断规则
		ruleName := fmt.Sprintf("VPNViewBlock_%s", ip)
		cmd = exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+ruleName, "dir=in", "action=block", "remoteip="+ip)
	default:
		slog.Warn("当前操作系统不支持自动联动物理防火墙，已安全降级为内置控制台内存封锁", "os", osType)
		return
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("报错",
			"err", err, "output", strings.TrimSpace(string(out)))
	} else {
		slog.Info("已封禁", "ip", ip)
	}
}
