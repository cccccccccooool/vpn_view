// ============================================================================
// 文件说明：internal/adapter/xray/config_manager.go
// 职责概览：Xray / V2Ray 适配器内部的 JSON 配置文件动态渲染与读写管理器（ConfigManager）。
//           负责管理底座 inbounds 下的用户认证表（settings.users[] 或旧别名 settings.clients[]），
//           以 config_patch 模式将面板登记的用户下发到核心配置，并在写盘瞬间：
//             1. 仅替换受管协议 inbound 的用户列表，完整保留 TLS/Reality/证书/路由/DNS/出站等非用户配置。
//             2. 强制启用 stats{} 与 policy.levels 的 statsUserUplink/Downlink，激活底层 gRPC 精准流量统计。
//             3. 为 VLESS inbound 补齐必需的 decryption:"none"，防止缺字段导致核心拒绝加载。
//           每个用户的稳定标识 userID 会写入用户对象的 email 字段——这是 Xray / V2Ray 统计
//           `user>>>[email]>>>traffic>>>uplink` 命名的锚点。
// ============================================================================

package xray

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

// managedProtocols 是本适配器负责下发用户的协议集合；其余协议 inbound（socks/http/dokodemo 等）不予触碰。
var managedProtocols = map[string]bool{
	"vless":       true,
	"vmess":       true,
	"trojan":      true,
	"shadowsocks": true,
}

// ConfigManager 负责维护内存用户数据与底层物理 JSON 配置文件读写的对齐同步。
type ConfigManager struct {
	cfg   Config
	mu    sync.Mutex
	users map[string]map[string]string // 内存用户凭据缓存 userID -> credentials
}

// NewConfigManager 实例化并创建一个 ConfigManager。
// 启动逻辑：
//  1. 从已有运行配置反向加载历史用户（平滑过渡，避免面板初始化踢掉存量连接）。
//  2. 冷启动防护：运行配置文件不存在时，依据底座模板级联创建生成。
//  3. 统计强刷：文件已存在时也强制重写一次，将存量用户与 stats/policy 指标注入激活流量度量。
func NewConfigManager(cfg Config) (*ConfigManager, error) {
	m := &ConfigManager{
		cfg:   cfg,
		users: make(map[string]map[string]string),
	}

	if cfg.ConfigPath != "" {
		if err := m.loadUsersFromConfig(); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	if cfg.ConfigPath != "" {
		if _, err := os.Stat(cfg.ConfigPath); os.IsNotExist(err) {
			slog.Info("未检测到 Xray/V2Ray 运行配置文件，正在根据底座模板自动冷启动创建...", "path", cfg.ConfigPath)
			if err := m.writeConfigLocked(); err != nil {
				return nil, fmt.Errorf("冷启动初始化 Xray/V2Ray 配置文件失败: %w", err)
			}
			slog.Info("🎉 冷启动自动初始化 Xray/V2Ray 运行配置文件成功", "path", cfg.ConfigPath)
		} else {
			slog.Info("正在重构刷新已有 Xray/V2Ray 配置以激活流量精准统计...", "path", cfg.ConfigPath)
			if err := m.writeConfigLocked(); err != nil {
				slog.Error("重置同步已有 Xray/V2Ray 配置失败", "err", err)
			} else {
				slog.Info("🎉 重构刷新 Xray/V2Ray 配置文件完成！")
			}
		}
	}

	return m, nil
}

// ListUsers 获取当前内存中登记的全部用户 ID 列表（排序输出）。
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

// AddUser 新增或修改单个用户凭据并即刻刷写至物理 JSON 配置文件。
func (m *ConfigManager) AddUser(ctx context.Context, userID string, credentials map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.users[userID] = cloneStringMap(credentials)
	return m.writeConfigLocked()
}

// RemoveUser 从内存字典抹除目标用户并重新回写物理 JSON 配置文件。
func (m *ConfigManager) RemoveUser(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.users, userID)
	return m.writeConfigLocked()
}

// Reload 通过执行本地系统命令（如 systemctl reload xray）让核心平滑热重载最新配置。
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
		return fmt.Errorf("热更新命令执行失败: %w, 命令输出: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// loadUsersFromConfig 反解过渡工具。启动时读取现有 JSON，反解出受管协议 inbound 的用户列表并加载到内存，
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
		return fmt.Errorf("解析 JSON 配置文件失败: %w", err)
	}

	inbounds, ok := root["inbounds"].([]any)
	if !ok {
		return fmt.Errorf("配置文件不包含合规的 inbounds 数组")
	}

	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		protocol, _ := inbound["protocol"].(string)
		if !managedProtocols[protocol] {
			continue
		}
		if m.cfg.InboundTag != "" && inbound["tag"] != m.cfg.InboundTag {
			continue
		}
		settings, _ := inbound["settings"].(map[string]any)
		if settings == nil {
			continue
		}
		for _, entry := range userEntries(settings) {
			userMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			// email 是用户稳定标识锚点；缺失则退回 id / password。
			id := firstString(userMap, "email", "id", "password")
			if id == "" {
				continue
			}
			m.users[id] = credentialsFromEntry(protocol, userMap)
		}
	}
	return nil
}

// writeConfigLocked 读取底座模板、注入内存活跃用户、激活流量统计、重构写盘。
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

	// 逐 inbound 注入受管用户，完整保留其它字段。
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		protocol, _ := inbound["protocol"].(string)
		if !managedProtocols[protocol] {
			continue
		}
		if m.cfg.InboundTag != "" && inbound["tag"] != m.cfg.InboundTag {
			continue
		}

		settings := ensureMap(inbound, "settings")
		key := m.userListKey(settings)
		settings[key] = m.buildUserListForProtocol(protocol)
		// VLESS 必须显式声明 decryption，否则核心拒绝加载。
		if protocol == "vless" {
			if _, ok := settings["decryption"]; !ok {
				settings["decryption"] = "none"
			}
		}
	}

	m.enableUserStats(root)

	if err := os.MkdirAll(filepath.Dir(m.cfg.ConfigPath), 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cfg.ConfigPath, append(out, '\n'), 0644)
}

// enableUserStats 激活 Xray / V2Ray 的用户级流量统计：
//   - 顶层存在 stats{} 即开启统计引擎。
//   - policy.levels."0" 打开 statsUserUplink / statsUserDownlink 才会真正落计数。
//   - 若模板已配置 api 对象，则确保其 services 含 StatsService（仅增补，不改动其它字段）。
func (m *ConfigManager) enableUserStats(root map[string]any) {
	if _, ok := root["stats"]; !ok {
		root["stats"] = map[string]any{}
	}

	policy := ensureMap(root, "policy")
	levels := ensureMap(policy, "levels")
	level0 := ensureMap(levels, "0")
	level0["statsUserUplink"] = true
	level0["statsUserDownlink"] = true

	if api, ok := root["api"].(map[string]any); ok {
		services := stringSlice(api["services"])
		if !slices.Contains(services, "StatsService") {
			services = append(services, "StatsService")
		}
		api["services"] = services
	}
}

// configSourcePath 提取底盘文件的真实加载来源（模板优先，回退运行配置）。
func (m *ConfigManager) configSourcePath() string {
	if m.cfg.ConfigTemplatePath != "" {
		return m.cfg.ConfigTemplatePath
	}
	return m.cfg.ConfigPath
}

// userListKey 决定某 inbound 的用户列表键：优先沿用模板已存在的键（users 或 clients），
// 都不存在时按核心变体给默认值（v2ray 惯用 clients，xray 惯用 users）。
func (m *ConfigManager) userListKey(settings map[string]any) string {
	if _, ok := settings["users"]; ok {
		return "users"
	}
	if _, ok := settings["clients"]; ok {
		return "clients"
	}
	if m.cfg.Variant == "v2ray" {
		return "clients"
	}
	return "users"
}

// buildUserListForProtocol 提取匹配协议的活跃用户凭证，重构为核心 inbound 用户对象数组（含 email 锚点）。
func (m *ConfigManager) buildUserListForProtocol(protocol string) []any {
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
		out = append(out, buildUserEntry(protocol, id, m.users[id]))
	}
	return out
}

// buildUserEntry 依据协议将单个用户凭据翻译为核心的用户对象（email 恒为 userID）。
func buildUserEntry(protocol, userID string, creds map[string]string) map[string]any {
	entry := map[string]any{"email": userID}
	switch protocol {
	case "vmess":
		entry["id"] = fallback(creds["uuid"], userID)
	case "trojan":
		entry["password"] = fallback(creds["password"], userID)
	case "shadowsocks":
		entry["password"] = fallback(creds["ss_password"], userID)
		entry["method"] = fallback(creds["ss_method"], "256-gcm")
	default: // vless
		entry["id"] = fallback(creds["uuid"], userID)
		if flow := creds["flow"]; flow != "" {
			entry["flow"] = flow
		}
	}
	return entry
}

// credentialsFromEntry 反向将核心用户对象翻译为面板内部凭据字典。
func credentialsFromEntry(protocol string, userMap map[string]any) map[string]string {
	creds := map[string]string{"protocol": protocol}
	switch protocol {
	case "vmess":
		if v, ok := userMap["id"].(string); ok {
			creds["uuid"] = v
		}
	case "trojan":
		if v, ok := userMap["password"].(string); ok {
			creds["password"] = v
		}
	case "shadowsocks":
		if v, ok := userMap["password"].(string); ok {
			creds["ss_password"] = v
		}
		if v, ok := userMap["method"].(string); ok {
			creds["ss_method"] = v
		}
	default: // vless
		if v, ok := userMap["id"].(string); ok {
			creds["uuid"] = v
		}
		if v, ok := userMap["flow"].(string); ok && v != "" {
			creds["flow"] = v
		}
	}
	return creds
}

// userEntries 返回某 inbound settings 中的用户对象数组，兼容 users 与 clients 两种键名。
func userEntries(settings map[string]any) []any {
	if list, ok := settings["users"].([]any); ok {
		return list
	}
	if list, ok := settings["clients"].([]any); ok {
		return list
	}
	return nil
}

// stringSlice 将任意 JSON 值安全转换为 []string（用于读取 api.services 等字段）。
func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// firstString 依序返回 map 中首个存在且非空的字符串字段。
func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// fallback 在主值为空时返回兜底值。
func fallback(value, fb string) string {
	if value != "" {
		return value
	}
	return fb
}

// ensureMap 获取（或懒创建）父 map 下指定键的子 map。
func ensureMap(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = make(map[string]any)
		parent[key] = child
	}
	return child
}

// cloneStringMap 深拷贝 credentials 字典，防止并发冲突和脏更改。
func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
