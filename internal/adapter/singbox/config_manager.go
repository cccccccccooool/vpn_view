// ============================================================================
// 文件说明：internal/adapter/singbox/config_manager.go
// 职责概览：Sing-box 适配器内部的 JSON 配置文件模板动态渲染与读写管理器（ConfigManager）。
//           负责管理底座中 Inbound 下的 users 认证表。
//           提供 loadUsersFromConfig 反向解析过渡功能，支持自动冷启动检测初始化。
//           核心功能（同步注入流量监控）：
//           在 writeConfigLocked 覆写配置文件的瞬间，会自动将当前系统的所有用户 ID
//           注入到 Sing-box 配置文件的 `experimental.v2ray_api.stats.users` 中，
//           以此完美拉起并激活底层 gRPC 精准统计，同时利用用户分流 RouteMarker 机制，
//           解决无用户特征协议的身份识别。
// ============================================================================

package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ConfigManager 负责维护内存用户数据与底层物理 JSON 配置文件读写的对齐同步。
type ConfigManager struct {
	cfg   Config                       // 适配器配置项
	mu    sync.Mutex                   // 并发读写锁
	users map[string]map[string]string // 内存中的用户凭据缓存 userID -> credentials
}

// NewConfigManager 实例化并创建一个 ConfigManager。
// 启动逻辑：
//  1. 尝试从已有的运行配置文件反向加载解析出历史用户（平滑过渡，防止面板初始化时踢掉已有连接）。
//  2. 🏆 自动冷启动防护：如果运行配置文件物理不存在，会自动根据底座模板（ConfigTemplatePath）级联创建并生成。
//  3. 流量监控强刷：如果文件已经存在，也会强制 writeConfigLocked 刷盘写入一次，目的是将面板现存的所有用户 UUID
//     注入到 experimental.v2ray_api.stats.users 指标配置中，激活底层流量度量。
func NewConfigManager(cfg Config) (*ConfigManager, error) {
	m := &ConfigManager{
		cfg:   cfg,
		users: make(map[string]map[string]string),
	}

	// 1. 反向平滑过渡加载
	if cfg.ConfigPath != "" {
		if err := m.loadUsersFromConfig(); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	// 2. 冷启动初始化与统计强制刷新
	if cfg.ConfigPath != "" {
		if _, err := os.Stat(cfg.ConfigPath); os.IsNotExist(err) {
			slog.Info("未检测到 Sing-box 运行配置文件，正在根据底座模板自动冷启动创建...", "path", cfg.ConfigPath)
			if err := m.writeConfigLocked(); err != nil {
				return nil, fmt.Errorf("冷启动初始化 Sing-box 配置文件失败: %w", err)
			}
			slog.Info("🎉 冷启动自动初始化 Sing-box 运行配置文件成功", "path", cfg.ConfigPath)
		} else {
			slog.Info("正在增量重构刷新已有 Sing-box 配置以强行激活流量精准统计...", "path", cfg.ConfigPath)
			if err := m.writeConfigLocked(); err != nil {
				slog.Error("重置同步已有 Sing-box 配置失败", "err", err)
			} else {
				slog.Info("🎉 增量重构刷新 Sing-box 配置文件完成！")
			}
		}
	}

	return m, nil
}

// ListUsers 获取当前已经从运行配置文件反解及登记的内存用户 ID 列表（排序输出）。
func (m *ConfigManager) ListUsers(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]string, 0, len(m.users))
	for id := range m.users {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

// AddUser 新增或修改单个用户凭据并即刻保存刷写至物理 JSON 配置文件中。
func (m *ConfigManager) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.users[userID] = cloneStringMap(credentials)
	return m.writeConfigLocked()
}

// RemoveUser 从内存字典彻底抹除目标用户并重新刷写回写物理 JSON 配置文件。
func (m *ConfigManager) RemoveUser(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.users, userID)
	return m.writeConfigLocked()
}

// Reload 通过在本地物理系统执行命令（如 systemctl reload sing-box）让代理应用平滑热重载最新配置。
func (m *ConfigManager) Reload(ctx context.Context) error {
	if m.cfg.ReloadCommand == "" {
		return nil
	}
	parts := strings.Fields(m.cfg.ReloadCommand)
	if len(parts) == 0 {
		return nil
	}
	// 利用 ExecCommand 执行热更
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("热更新命令执行失败: %w, 命令输出: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// loadUsersFromConfig 反解过渡工具。
// 启动时读取现有 JSON 文件，反解出 VMESS/VLESS/Trojan/Shadowsocks 等 inbound.users 列表并加载到内存，
// 防止面板初次挂载导致历史客户端断连。
func (m *ConfigManager) loadUsersFromConfig() error {
	path := m.cfg.ConfigPath
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = m.configSourcePath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("解析模板 JSON 配置文件失败: %w", err)
	}

	inbounds, ok := root["inbounds"].([]any)
	if !ok {
		return fmt.Errorf("配置模板不包含合规的 inbounds 数组")
	}

	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}

		protoType, _ := inbound["type"].(string)
		if protoType == "" {
			continue
		}

		// inbound tag 过滤器对齐
		if m.cfg.InboundTag != "" && inbound["tag"] != m.cfg.InboundTag {
			continue
		}

		users, _ := inbound["users"].([]any)
		for _, uItem := range users {
			userMap, ok := uItem.(map[string]any)
			if !ok {
				continue
			}

			// vm/vless/trojan 首选提取 name 或 uuid 作为用户标志 ID
			id := firstString(userMap, "name", "username", "uuid", "password")
			if id == "" {
				continue
			}

			creds := make(map[string]string)
			creds["protocol"] = protoType

			for k, v := range userMap {
				if s, ok := v.(string); ok && k != "name" {
					// Shadowsocks 加密专有字段的反向翻译
					if protoType == "shadowsocks" {
						if k == "password" {
							creds["ss_password"] = s
							continue
						}
						if k == "method" {
							creds["ss_method"] = s
							continue
						}
					}
					creds[k] = s
				}
			}
			m.users[id] = creds
		}
	}
	return nil
}

// writeConfigLocked 读取底板模板、动态注入内存中的活跃 users 表、生成精准流量监控标识、并重构写盘。
// 流程：
//  1. 取得底座模板（SOURCE）进行反序列化。
//  2. 扫描 inbounds，针对匹配的 tag，清空原 users 并全新注入本管理面板登记的对应协议的所有用户（VMESS、VLESS 等）。
//  3. 🏆 强制注入 V2Ray Stats 流量监控列表：
//     - 抽取当前全员 UserIDs。
//     - 重构写入 experimental.v2ray_api.stats.users。这能使底层内置 V2Ray gRPC 流量统计引擎知道监控哪些用户。
//  4. 🏆 分流防白嫖：调用 injectUserRouteMarkers 建立以 `vpnview-user-[hex]` 命名的专用分流 Direct 路由 rules 绑定，
//     以便 Clash API 能精准获取来源握手关系。
//  5. 写回 ConfigPath，自动创建所属目录。
func (m *ConfigManager) writeConfigLocked() error {
	if err := m.cfg.ValidateForConfigWrites(); err != nil {
		return err
	}

	raw, err := os.ReadFile(m.configSourcePath())
	if err != nil {
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("解析底座 JSON 配置文件模板失败: %w", err)
	}

	inbounds, ok := root["inbounds"].([]any)
	if !ok {
		return fmt.Errorf("底座模板缺少 inbounds 数组定义")
	}

	managedInboundTags := make([]string, 0)

	// 动态注入
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}

		protoType, _ := inbound["type"].(string)
		if protoType == "" {
			continue
		}

		if m.cfg.InboundTag != "" && inbound["tag"] != m.cfg.InboundTag {
			continue
		}

		users := m.buildUsersArrayForProtocol(protoType)
		inbound["users"] = users
		if len(users) > 0 {
			if tag, _ := inbound["tag"].(string); tag != "" {
				managedInboundTags = append(managedInboundTags, tag)
			}
		}
	}

	// 🏆 流量指标注入
	userIDs := make([]string, 0, len(m.users))
	for id := range m.users {
		userIDs = append(userIDs, id)
	}
	slices.Sort(userIDs)

	stats := ensureMap(ensureMap(ensureMap(root, "experimental"), "v2ray_api"), "stats")
	stats["enabled"] = true
	stats["users"] = userIDs

	// 🏆 身份反查分流注入
	m.injectUserRouteMarkers(root, managedInboundTags, userIDs)

	if err := os.MkdirAll(filepath.Dir(m.cfg.ConfigPath), 0755); err != nil {
		return err
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cfg.ConfigPath, append(out, '\n'), 0644)
}

// configSourcePath 提取底盘文件的真实加载来源。
func (m *ConfigManager) configSourcePath() string {
	if m.cfg.ConfigTemplatePath != "" {
		return m.cfg.ConfigTemplatePath
	}
	return m.cfg.ConfigPath
}

// buildUsersArrayForProtocol 提取匹配协议类型的活跃用户凭证，并重构为 Sing-box 的 inbound 内部 users 的 JSON 定义格式。
func (m *ConfigManager) buildUsersArrayForProtocol(protocol string) []any {
	ids := make([]string, 0)
	for id, creds := range m.users {
		proto := creds["protocol"]
		if proto == "" {
			proto = "vless"
		}
		if proto == protocol {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	out := make([]any, 0, len(ids))
	for _, id := range ids {
		item := map[string]any{"name": id}
		for k, v := range m.users[id] {
			if v == "" || k == "protocol" {
				continue
			}

			// Shadowsocks 写配置协议字段适配
			if protocol == "shadowsocks" {
				if k == "ss_password" {
					item["password"] = v
					continue
				}
				if k == "ss_method" {
					item["method"] = v
					continue
				}
			}

			item[k] = v
		}
		out = append(out, item)
	}
	return out
}

// injectUserRouteMarkers 为每个受管理的用户构建一条专用的 Direct 出站，并在 route.rules 里强制进行路由映射绑定。
// 巧妙之处：这允许主程序利用分流标签还原身份，完美打通了无用户特征协议在 Clash API 下的身份反查解析。
func (m *ConfigManager) injectUserRouteMarkers(root map[string]any, inboundTags, userIDs []string) {
	outbounds, ok := root["outbounds"].([]any)
	if !ok || len(userIDs) == 0 {
		return
	}

	baseOutbound := findBaseDirectOutbound(root, outbounds)
	if baseOutbound == nil {
		slog.Warn("跳过 Sing-box 路由标记注入：未找到直连出站出入口 (direct outbound)")
		return
	}

	outbounds = removeManagedUserOutbounds(outbounds)
	for _, userID := range userIDs {
		outbound := cloneAny(baseOutbound).(map[string]any)
		outbound["tag"] = userRouteTag(userID)
		outbounds = append(outbounds, outbound)
	}
	root["outbounds"] = outbounds

	route := ensureMap(root, "route")
	rules, _ := route["rules"].([]any)
	rules = removeManagedUserRules(rules)
	rules = append(rules, buildManagedUserRules(inboundTags, userIDs)...)
	route["rules"] = rules
}

func findBaseDirectOutbound(root map[string]any, outbounds []any) map[string]any {
	if route, ok := root["route"].(map[string]any); ok {
		if final, ok := route["final"].(string); ok && final != "" {
			if outbound := findDirectOutboundByTag(outbounds, final); outbound != nil {
				return outbound
			}
		}
	}
	if outbound := findDirectOutboundByTag(outbounds, "direct"); outbound != nil {
		return outbound
	}
	for _, item := range outbounds {
		outbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := outbound["type"].(string); typ == "direct" {
			return outbound
		}
	}
	return nil
}

func findDirectOutboundByTag(outbounds []any, tag string) map[string]any {
	for _, item := range outbounds {
		outbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if outboundTag, _ := outbound["tag"].(string); outboundTag != tag {
			continue
		}
		if typ, _ := outbound["type"].(string); typ != "direct" {
			continue
		}
		return outbound
	}
	return nil
}

func removeManagedUserOutbounds(outbounds []any) []any {
	out := make([]any, 0, len(outbounds))
	for _, item := range outbounds {
		outbound, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		tag, _ := outbound["tag"].(string)
		if isUserRouteTag(tag) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeManagedUserRules(rules []any) []any {
	out := make([]any, 0, len(rules))
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		outbound, _ := rule["outbound"].(string)
		if isUserRouteTag(outbound) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func buildManagedUserRules(inboundTags, userIDs []string) []any {
	rules := make([]any, 0, len(userIDs))
	slices.Sort(inboundTags)
	inboundTags = slices.Compact(inboundTags)
	for _, userID := range userIDs {
		rule := map[string]any{
			"auth_user": []string{userID},
			"outbound":  userRouteTag(userID),
		}
		if len(inboundTags) == 1 {
			rule["inbound"] = inboundTags[0]
		} else if len(inboundTags) > 1 {
			rule["inbound"] = inboundTags
		}
		rules = append(rules, rule)
	}
	return rules
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func ensureMap(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = make(map[string]any)
		parent[key] = child
	}
	return child
}

func cloneAny(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, value := range typed {
			out[k] = cloneAny(value)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = cloneAny(value)
		}
		return out
	default:
		return typed
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
