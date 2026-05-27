// config_manager.go 负责 sing-box 配置文件的读写和用户管理。
// 通过模板注入机制，将内存中的用户凭证动态写入 sing-box JSON 配置文件。
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

// ConfigManager 管理 sing-box 配置文件中的用户数据。
// 维护内存中的用户凭证副本，并负责将变更同步回写到 JSON 配置文件。
type ConfigManager struct {
	cfg   Config                       // 适配器配置
	mu    sync.Mutex                   // 保护 users 的并发读写
	users map[string]map[string]string // userID -> credentials 的内存映射
}

// NewConfigManager 创建并初始化 ConfigManager。
// 初始化过程中会尝试从已有配置文件反向加载用户，并在配置文件不存在时自动根据模板创建。
func NewConfigManager(cfg Config) (*ConfigManager, error) {
	m := &ConfigManager{
		cfg:   cfg,
		users: make(map[string]map[string]string),
	}

	// 尝试反向加载已有配置中的用户（进行平滑过渡）
	if cfg.ConfigPath != "" {
		if err := m.loadUsersFromConfig(); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	// 🏆 自动冷启动与流量统计同步激活：
	// 如果配置文件不存在，则立即根据模板生成。
	// 如果配置文件已经存在，我们也会强制 writeConfigLocked 一次，以确保 `experimental.v2ray_api.stats.users` 中注入了所有后台用户的唯一 UUID，从而激活精准的流量统计功能！
	if cfg.ConfigPath != "" {
		if _, err := os.Stat(cfg.ConfigPath); os.IsNotExist(err) {
			slog.Info("未检测到 sing-box 运行配置文件，正在根据底板自动初始化创建...", "path", cfg.ConfigPath)
			if err := m.writeConfigLocked(); err != nil {
				return nil, fmt.Errorf("自动初始化 sing-box 配置失败: %w", err)
			}
			slog.Info("🎉 自动初始化 sing-box 运行配置文件成功！", "path", cfg.ConfigPath)
		} else {
			slog.Info("正在同步更新已有 sing-box 配置文件以确保流量统计生效...", "path", cfg.ConfigPath)
			if err := m.writeConfigLocked(); err != nil {
				slog.Error("同步更新 sing-box 配置文件失败", "err", err)
			} else {
				slog.Info("🎉 同步更新 sing-box 配置文件成功！")
			}
		}
	}

	return m, nil
}

// ListUsers 返回内存中所有已注册用户的 ID 列表（按字典序排序）。
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

// AddUser 添加用户凭证到内存并立即回写配置文件。
func (m *ConfigManager) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.users[userID] = cloneStringMap(credentials)
	return m.writeConfigLocked()
}

// RemoveUser 从内存中删除指定用户并立即回写配置文件。
func (m *ConfigManager) RemoveUser(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从内存中删除用户凭证，并立即回写配置文件
	delete(m.users, userID)
	return m.writeConfigLocked()
}

// Reload 尝试通过执行配置好的外部命令（如 systemctl reload sing-box）来重新加载 sing-box 进程
func (m *ConfigManager) Reload(ctx context.Context) error {
	if m.cfg.ReloadCommand == "" {
		return nil
	}
	parts := strings.Fields(m.cfg.ReloadCommand)
	if len(parts) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// loadUsersFromConfig 从配置文件中解析并加载已有的用户凭证到内存中
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
		return fmt.Errorf("parse sing-box config: %w", err)
	}

	inbounds, ok := root["inbounds"].([]any)
	if !ok {
		return fmt.Errorf("sing-box config has no inbounds array")
	}

	// 遍历底板或成品配置文件里的所有 inbound，解析并加载已有用户
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}

		protoType, _ := inbound["type"].(string)
		if protoType == "" {
			continue
		}

		// 若指定了 InboundTag，只解析匹配该 Tag 的 inbound（保持向后兼容）
		if m.cfg.InboundTag != "" && inbound["tag"] != m.cfg.InboundTag {
			continue
		}

		users, _ := inbound["users"].([]any)
		for _, uItem := range users {
			userMap, ok := uItem.(map[string]any)
			if !ok {
				continue
			}

			// 优先取 name, username, uuid, password 作为内部 ID
			id := firstString(userMap, "name", "username", "uuid", "password")
			if id == "" {
				continue
			}

			creds := make(map[string]string)
			creds["protocol"] = protoType

			for k, v := range userMap {
				if s, ok := v.(string); ok && k != "name" {
					// 针对 shadowsocks，反向还原 password 和 method 为 ss_password 和 ss_method
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

// writeConfigLocked 读取模板（或当前文件），将内存中的用户凭证注入，并回写生成最终的 sing-box 配置文件
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
		return fmt.Errorf("parse sing-box config: %w", err)
	}

	inbounds, ok := root["inbounds"].([]any)
	if !ok {
		return fmt.Errorf("sing-box config has no inbounds array")
	}

	managedInboundTags := make([]string, 0)

	// 针对每一个 inbound 动态注入匹配协议的用户
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}

		protoType, _ := inbound["type"].(string)
		if protoType == "" {
			continue
		}

		// 若指定了 InboundTag，只注入匹配该 Tag 的 inbound
		if m.cfg.InboundTag != "" && inbound["tag"] != m.cfg.InboundTag {
			continue
		}

		// 根据协议类型构建用户数组并注入
		users := m.buildUsersArrayForProtocol(protoType)
		inbound["users"] = users
		if len(users) > 0 {
			if tag, _ := inbound["tag"].(string); tag != "" {
				managedInboundTags = append(managedInboundTags, tag)
			}
		}
	}

	// 🏆 动态注入所有用户到 experimental.v2ray_api.stats.users 列表中，生成 V2Ray 流量计数器
	userIDs := make([]string, 0, len(m.users))
	for id := range m.users {
		userIDs = append(userIDs, id)
	}
	slices.Sort(userIDs)

	stats := ensureMap(ensureMap(ensureMap(root, "experimental"), "v2ray_api"), "stats")
	stats["enabled"] = true
	stats["users"] = userIDs
	m.injectUserRouteMarkers(root, managedInboundTags, userIDs)

	if err := os.MkdirAll(filepath.Dir(m.cfg.ConfigPath), 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	// 保存最终配置文件，供 sing-box 运行或重载读取
	return os.WriteFile(m.cfg.ConfigPath, append(out, '\n'), 0644)
}

// configSourcePath 返回读取配置模板的文件路径。
// 优先使用 ConfigTemplatePath，为空时回退到 ConfigPath。
func (m *ConfigManager) configSourcePath() string {
	if m.cfg.ConfigTemplatePath != "" {
		return m.cfg.ConfigTemplatePath
	}
	return m.cfg.ConfigPath
}

// buildUsersArrayForProtocol 根据指定协议类型，从内存中筛选匹配的用户并构建 sing-box users JSON 数组。
// 对 shadowsocks 协议进行字段名映射（ss_password -> password, ss_method -> method）。
func (m *ConfigManager) buildUsersArrayForProtocol(protocol string) []any {
	ids := make([]string, 0)
	for id, creds := range m.users {
		proto := creds["protocol"]
		if proto == "" {
			proto = "vless" // 默认 fallback 为 vless
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

			// 如果是 shadowsocks，在写入配置文件时进行字段名映射适配
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

// injectUserRouteMarkers routes each authenticated user through an equivalent
// direct outbound with a reversible tag, so Clash API connection chains can be
// mapped back to the panel user even when metadata omits auth_user.
func (m *ConfigManager) injectUserRouteMarkers(root map[string]any, inboundTags, userIDs []string) {
	outbounds, ok := root["outbounds"].([]any)
	if !ok || len(userIDs) == 0 {
		return
	}

	baseOutbound := findBaseDirectOutbound(root, outbounds)
	if baseOutbound == nil {
		slog.Warn("skip sing-box user route markers: no direct outbound found")
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

// firstString 从 map 中按优先级依次尝试获取第一个非空字符串值。
func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// ensureMap returns a nested object map, creating or replacing it when needed.
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

// cloneStringMap 对 string map 进行浅拷贝，避免外部修改影响内部状态。
func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
