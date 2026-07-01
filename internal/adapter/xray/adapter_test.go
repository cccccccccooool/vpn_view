// ============================================================================
// 文件说明：internal/adapter/xray/adapter_test.go
// 职责概览：Xray / V2Ray 适配器的单元测试。覆盖维护指南《测试清单》要求的 adapter 项：
//           能创建 adapter、返回正确 capabilities 与 credential fields、AddUser/RemoveUser
//           行为正确、不支持的能力返回 domain.ErrNotSupported；并额外验证 config_patch
//           写盘时对 TLS/证书/路由等非用户配置的保留、stats/policy 激活、以及 v2ray 变体
//           的 gRPC 统计路径归一化。
// ============================================================================

package xray

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpnview/internal/domain"
)

// 一份带 TLS/证书/路由/直连出站的 VLESS + Trojan 双 inbound 底座模板，用于验证非用户配置保留。
const templateJSON = `{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "tag": "vless-in",
      "protocol": "vless",
      "port": 443,
      "settings": {
        "decryption": "none",
        "users": []
      },
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "certificates": [
            {"certificateFile": "/etc/ssl/fullchain.pem", "keyFile": "/etc/ssl/key.pem"}
          ]
        }
      }
    },
    {
      "tag": "trojan-in",
      "protocol": "trojan",
      "port": 8443,
      "settings": {"clients": []}
    },
    {
      "tag": "socks-in",
      "protocol": "socks",
      "port": 1080,
      "settings": {"auth": "noauth"}
    }
  ],
  "outbounds": [{"protocol": "freedom", "tag": "direct"}],
  "routing": {"rules": [{"type": "field", "ip": ["geoip:private"], "outboundTag": "direct"}]}
}`

// newTestAdapter 在临时目录写入模板并构造 adapter（不含 gRPC API、不含订阅域名）。
func newTestAdapter(t *testing.T, variant string, extra map[string]any) (*Adapter, string) {
	t.Helper()
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "template.json")
	if err := os.WriteFile(tmpl, []byte(templateJSON), 0644); err != nil {
		t.Fatalf("写入模板失败: %v", err)
	}
	runPath := filepath.Join(dir, "config.json")

	raw := map[string]any{
		"xray_config_path":     runPath,
		"config_template_path": tmpl,
	}
	for k, v := range extra {
		raw[k] = v
	}

	a, err := New(raw, variant)
	if err != nil {
		t.Fatalf("创建 adapter 失败: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, runPath
}

// readConfig 读取运行配置文件并反序列化。
func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取运行配置失败: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("解析运行配置失败: %v", err)
	}
	return root
}

// inboundByTag 从配置根中按 tag 定位 inbound。
func inboundByTag(root map[string]any, tag string) map[string]any {
	inbounds, _ := root["inbounds"].([]any)
	for _, item := range inbounds {
		in, _ := item.(map[string]any)
		if in != nil && in["tag"] == tag {
			return in
		}
	}
	return nil
}

func TestCapabilitiesWithoutOptionalAPIs(t *testing.T) {
	a, _ := newTestAdapter(t, "xray", nil)
	caps := a.Capabilities()

	for _, must := range []domain.Capability{
		domain.CapListUsers, domain.CapAddUser, domain.CapRemoveUser,
		domain.CapDisableUser, domain.CapEnableUser, domain.CapCredentialDefs,
	} {
		if !caps.Has(must) {
			t.Errorf("缺少应有能力 %s", must.Name())
		}
	}
	// 未配置 gRPC / 订阅域名时不得虚报这些能力。
	for _, absent := range []domain.Capability{
		domain.CapQueryTraffic, domain.CapUserSpeed, domain.CapSubscription,
		domain.CapActiveConns, domain.CapKillConn, domain.CapRealtimeSpeed,
	} {
		if caps.Has(absent) {
			t.Errorf("不应声明能力 %s", absent.Name())
		}
	}
}

func TestCredentialFieldsShape(t *testing.T) {
	a, _ := newTestAdapter(t, "xray", nil)
	fields := a.CredentialFields()
	if len(fields) == 0 {
		t.Fatal("凭据字段不应为空")
	}
	if fields[0].Key != "protocol" || fields[0].Type != "select" {
		t.Errorf("首字段应为 protocol select, 实得 %+v", fields[0])
	}
	keys := map[string]bool{}
	for _, f := range fields {
		keys[f.Key] = true
	}
	for _, want := range []string{"protocol", "uuid", "flow", "password", "ss_password", "ss_method"} {
		if !keys[want] {
			t.Errorf("缺少凭据字段 %q", want)
		}
	}
}

func TestProfile(t *testing.T) {
	a, _ := newTestAdapter(t, "xray", nil)
	p := a.Profile()
	if p.Name != "xray" {
		t.Errorf("Name 应为 xray, 实得 %q", p.Name)
	}
	if p.UserProvisionMode != domain.UserProvisionConfigPatch {
		t.Errorf("provision 模式应为 config_patch, 实得 %q", p.UserProvisionMode)
	}
	if p.ConfigFormat != domain.ConfigFormatJSON {
		t.Errorf("配置格式应为 json, 实得 %q", p.ConfigFormat)
	}
	// 无 gRPC API 时流量应声明为 unsupported。
	if p.TrafficMode != domain.TrafficModeUnsupported {
		t.Errorf("无 API 时流量模式应为 unsupported, 实得 %q", p.TrafficMode)
	}
}

func TestAddListRemoveUserVLESS(t *testing.T) {
	a, runPath := newTestAdapter(t, "xray", nil)
	ctx := context.Background()

	creds := map[string]string{"protocol": "vless", "uuid": "11111111-1111-4111-8111-111111111111", "flow": "xtls-rprx-vision"}
	if err := a.AddUser(ctx, "alice", creds); err != nil {
		t.Fatalf("AddUser 失败: %v", err)
	}

	ids, err := a.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers 失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != "alice" {
		t.Fatalf("ListUsers 期望 [alice], 实得 %v", ids)
	}

	root := readConfig(t, runPath)
	vless := inboundByTag(root, "vless-in")
	settings, _ := vless["settings"].(map[string]any)
	users, _ := settings["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("vless inbound 应有 1 个用户, 实得 %d", len(users))
	}
	u0, _ := users[0].(map[string]any)
	if u0["email"] != "alice" {
		t.Errorf("用户 email 应为 alice, 实得 %v", u0["email"])
	}
	if u0["id"] != creds["uuid"] {
		t.Errorf("用户 id 应为 %q, 实得 %v", creds["uuid"], u0["id"])
	}
	if u0["flow"] != "xtls-rprx-vision" {
		t.Errorf("用户 flow 应保留, 实得 %v", u0["flow"])
	}
	if settings["decryption"] != "none" {
		t.Errorf("vless decryption 应保留 none, 实得 %v", settings["decryption"])
	}

	// 关键：非用户配置（TLS 证书、路由、直连出站）必须原样保留。
	streamSettings, _ := vless["streamSettings"].(map[string]any)
	if streamSettings == nil || streamSettings["security"] != "tls" {
		t.Errorf("streamSettings.security 应保留 tls, 实得 %v", streamSettings)
	}
	if root["routing"] == nil {
		t.Error("routing 配置不应丢失")
	}
	if root["outbounds"] == nil {
		t.Error("outbounds 配置不应丢失")
	}

	// stats / policy 应被激活。
	if root["stats"] == nil {
		t.Error("stats{} 应被启用")
	}
	policy, _ := root["policy"].(map[string]any)
	levels, _ := policy["levels"].(map[string]any)
	lvl0, _ := levels["0"].(map[string]any)
	if lvl0 == nil || lvl0["statsUserUplink"] != true || lvl0["statsUserDownlink"] != true {
		t.Errorf("policy.levels.0 应打开用户上下行统计, 实得 %v", lvl0)
	}

	// 移除用户。
	if err := a.RemoveUser(ctx, "alice"); err != nil {
		t.Fatalf("RemoveUser 失败: %v", err)
	}
	ids, _ = a.ListUsers(ctx)
	if len(ids) != 0 {
		t.Fatalf("RemoveUser 后应为空, 实得 %v", ids)
	}
	root = readConfig(t, runPath)
	settings, _ = inboundByTag(root, "vless-in")["settings"].(map[string]any)
	if users, _ := settings["users"].([]any); len(users) != 0 {
		t.Errorf("移除后 vless 用户表应为空, 实得 %v", users)
	}
}

func TestTrojanUsesClientsKeyPreserved(t *testing.T) {
	a, runPath := newTestAdapter(t, "xray", nil)
	ctx := context.Background()

	if err := a.AddUser(ctx, "bob", map[string]string{"protocol": "trojan", "password": "s3cret"}); err != nil {
		t.Fatalf("AddUser trojan 失败: %v", err)
	}
	root := readConfig(t, runPath)
	settings, _ := inboundByTag(root, "trojan-in")["settings"].(map[string]any)
	// 模板 trojan inbound 用 clients 键，写回应沿用 clients（而非新增 users）。
	if _, ok := settings["users"]; ok {
		t.Error("trojan inbound 不应被塞入 users 键, 应沿用模板的 clients 键")
	}
	clients, _ := settings["clients"].([]any)
	if len(clients) != 1 {
		t.Fatalf("trojan clients 应有 1 项, 实得 %d", len(clients))
	}
	c0, _ := clients[0].(map[string]any)
	if c0["password"] != "s3cret" || c0["email"] != "bob" {
		t.Errorf("trojan 用户字段错误: %v", c0)
	}
}

func TestUnmanagedInboundUntouched(t *testing.T) {
	a, runPath := newTestAdapter(t, "xray", nil)
	if err := a.AddUser(context.Background(), "alice", map[string]string{"protocol": "vless", "uuid": "u"}); err != nil {
		t.Fatalf("AddUser 失败: %v", err)
	}
	root := readConfig(t, runPath)
	socks, _ := inboundByTag(root, "socks-in")["settings"].(map[string]any)
	if socks["auth"] != "noauth" {
		t.Errorf("非受管 socks inbound 不应被改动, 实得 %v", socks)
	}
	if _, ok := socks["users"]; ok {
		t.Error("非受管 inbound 不应被注入用户表")
	}
}

func TestSubscriptionCapabilityAndOutput(t *testing.T) {
	// 无域名 -> 不支持。
	a, _ := newTestAdapter(t, "xray", nil)
	if _, _, err := a.GenerateSubscription(context.Background(), "alice", map[string]string{"protocol": "vless", "uuid": "u"}); !errors.Is(err, domain.ErrNotSupported) {
		t.Errorf("无订阅域名时应返回 ErrNotSupported, 实得 %v", err)
	}

	// 有域名 -> 输出可解码的 vless 链接。
	b, _ := newTestAdapter(t, "xray", map[string]any{"subscription_domain": "vpn.example.com", "subscription_port": 443})
	if !b.Capabilities().Has(domain.CapSubscription) {
		t.Fatal("配置订阅域名后应声明 CapSubscription")
	}
	data, mime, err := b.GenerateSubscription(context.Background(), "alice", map[string]string{"protocol": "vless", "uuid": "abc", "flow": "xtls-rprx-vision"})
	if err != nil {
		t.Fatalf("GenerateSubscription 失败: %v", err)
	}
	if !strings.HasPrefix(mime, "text/plain") {
		t.Errorf("MIME 应为 text/plain, 实得 %q", mime)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		t.Fatalf("订阅内容应为合法 base64: %v", err)
	}
	uri := string(decoded)
	if !strings.HasPrefix(uri, "vless://abc@vpn.example.com:443") {
		t.Errorf("vless 链接格式错误: %s", uri)
	}
	if !strings.Contains(uri, "flow=xtls-rprx-vision") {
		t.Errorf("vless 链接应含 flow 参数: %s", uri)
	}
}

func TestSpeedLimitUnsupported(t *testing.T) {
	a, _ := newTestAdapter(t, "xray", nil)
	ctx := context.Background()
	if err := a.SetUserSpeedLimit(ctx, "alice", 100, 100); !errors.Is(err, domain.ErrNotSupported) {
		t.Errorf("SetUserSpeedLimit 应返回 ErrNotSupported, 实得 %v", err)
	}
	if err := a.SetGlobalSpeedLimit(ctx, 100, 100); !errors.Is(err, domain.ErrNotSupported) {
		t.Errorf("SetGlobalSpeedLimit 应返回 ErrNotSupported, 实得 %v", err)
	}
	// 无 gRPC API 时流量查询也应不支持。
	if _, err := a.QueryTraffic(ctx); !errors.Is(err, domain.ErrNotSupported) {
		t.Errorf("无 API 时 QueryTraffic 应返回 ErrNotSupported, 实得 %v", err)
	}
}

func TestVariantStatsMethodNormalization(t *testing.T) {
	xrayCfg := ParseConfig(map[string]any{}, "xray-core")
	if xrayCfg.Variant != "xray" || xrayCfg.StatsQueryMethod != xrayStatsQueryMethod {
		t.Errorf("xray-core 应归一化为 xray 且用 xray 统计路径, 实得 variant=%q method=%q", xrayCfg.Variant, xrayCfg.StatsQueryMethod)
	}
	v2Cfg := ParseConfig(map[string]any{}, "v2ray-core")
	if v2Cfg.Variant != "v2ray" || v2Cfg.StatsQueryMethod != v2rayStatsQueryMethod {
		t.Errorf("v2ray-core 应归一化为 v2ray 且用 v2ray 统计路径, 实得 variant=%q method=%q", v2Cfg.Variant, v2Cfg.StatsQueryMethod)
	}
	// 显式覆盖优先。
	override := ParseConfig(map[string]any{"stats_query_method": "/custom/Path"}, "xray")
	if override.StatsQueryMethod != "/custom/Path" {
		t.Errorf("stats_query_method 覆盖失效, 实得 %q", override.StatsQueryMethod)
	}
}

func TestReverseLoadExistingUsers(t *testing.T) {
	// 预置一份已有用户的运行配置，验证冷启动反解加载不丢用户。
	dir := t.TempDir()
	runPath := filepath.Join(dir, "config.json")
	existing := `{
      "inbounds": [
        {"tag": "vless-in", "protocol": "vless", "settings": {"decryption": "none", "users": [
          {"id": "uuid-1", "email": "carol", "flow": "xtls-rprx-vision"}
        ]}}
      ],
      "outbounds": [{"protocol": "freedom"}]
    }`
	if err := os.WriteFile(runPath, []byte(existing), 0644); err != nil {
		t.Fatalf("写入既有配置失败: %v", err)
	}
	a, err := New(map[string]any{"xray_config_path": runPath, "config_template_path": runPath}, "xray")
	if err != nil {
		t.Fatalf("创建 adapter 失败: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ids, _ := a.ListUsers(context.Background())
	if len(ids) != 1 || ids[0] != "carol" {
		t.Fatalf("应反解加载既有用户 carol, 实得 %v", ids)
	}
}
