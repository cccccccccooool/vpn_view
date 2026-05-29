// ============================================================================
// 文件说明：internal/service/ddns_service.go
// 职责概览：实现动态 DNS（DDNS）动态公网 IP 定时自动更新同步服务（DDNSService）。
//           主要依靠后台循环协程，定时（如每 5 分钟）向外部服务探测本机的公网 IP 地址。
//           若探测到的当前公网 IP 与本地内存缓存的 cachedIP 不同，代表宽带运营商重置了公网 IP。
//           系统自动调用绑定的 DDNS 厂商 API（如 Cloudflare），一键更新目标域名解析记录的 A/AAAA 指向，
//           确保客户端及后台面板的域名指向始终保持最新与高可用。
// ============================================================================

package service

import (
	"context"
	"log/slog"
	"time"

	"vpnview/internal/config"
	"vpnview/internal/port"
)

// DDNSService 提供全自动的域名公网 IP 解析定时状态比对与强制重置纠偏服务。
type DDNSService struct {
	provider port.DDNSProvider  // 外部 DDNS 支持商适配器（如 Cloudflare 适配器）
	cfg      *config.DDNSConfig // DDNS 更新所需的域名、Token 密钥等配置
	interval time.Duration      // 周期性定时检测间隔时间
	cachedIP string             // 上一次成功检测并绑定的公网 IP，用于在本地比对是否发生变更
}

// NewDDNSService 创建并实例化一个新的 DDNSService 服务。
func NewDDNSService(provider port.DDNSProvider, cfg *config.DDNSConfig, interval time.Duration) *DDNSService {
	return &DDNSService{
		provider: provider,
		cfg:      cfg,
		interval: interval,
	}
}

// Start 后台定时巡检协程的运行大入口。由主程序启动时并发启动。
// 边界对齐：如果未配置任何有效的 provider 或 config 选项，后台协程打印关闭后自动退出，防止空转。
// 本函数会永久阻塞，直至外部广播取消 ctx 上下文。
func (s *DDNSService) Start(ctx context.Context) {
	if s.provider == nil || s.cfg == nil {
		slog.Info("DDNS 解析自动更新功能已关闭")
		return
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 启动时立即主动触发首次公网 IP 检测
	s.check(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

// check 执行一次公网 IP 变化度量与域名解析强刷。
// 算法：
//  1. 通过 DDNSProvider 探测并获取本机公网广域网出口 IP。
//  2. 匹配 s.cachedIP 是否发生变化；如果完全一致，不做动作退出。
//  3. 如果发生改变，向外部运营商同步触发 A/AAAA 记录的覆盖更新操作。
//  4. 更新成功后，将 cachedIP 更新为最新公网 IP，等待下一次比对。
func (s *DDNSService) check(ctx context.Context) {
	ip, err := s.provider.GetCurrentIP(ctx)
	if err != nil {
		slog.Error("DDNS 检测本机公网 IP 失败", "err", err)
		return
	}

	// 如果 IP 没有发生任何变化，无需发起 API 浪费请求限制
	if ip == s.cachedIP {
		return
	}

	slog.Info("检测到本机公网 IP 发生变更，正在自动更新域名 DNS 解析记录...", "原 IP", s.cachedIP, "新 IP", ip)
	if err := s.provider.UpdateDNSRecord(ctx, ip); err != nil {
		slog.Error("DDNS 同步域名解析记录失败", "err", err)
		return
	}

	// 成功后缓存
	s.cachedIP = ip
	slog.Info("DDNS 域名解析记录自动同步修改更新成功！")
}
