# VPNView

VPNView 是一个轻量 VPN 用户管理面板，用于管理用户、订阅、流量统计和核心配置同步。

当前主要支持：

- `sing-box`
- `xray` / `v2ray`
- `stub` 测试模式

## 重要说明

`takeover` 模式会接管底层核心配置。它会备份原配置，生成 VPNView 可管理的模板，清空旧用户列表，并让 VPNView 重新写入受管理用户。

如果只想安装面板，不想改动现有 VPN 核心，请使用：

```bash
sudo bash install.sh --protocol singbox --mode panel-only
```

## 安装模式

### 参数化安装

```bash
sudo bash install.sh --protocol singbox --mode takeover
```

这条命令不会逐项询问参数。适合已经知道要接管哪个核心的环境。

### 交互安装

```bash
sudo bash install.sh --interactive
```

交互模式会询问协议核心和安装模式。

### 非交互环境

非交互环境必须显式指定协议：

```bash
sudo env VPNVIEW_PROTOCOL=singbox VPNVIEW_MODE=takeover bash install.sh
```

如果没有指定 `--protocol` 或 `VPNVIEW_PROTOCOL`，脚本会直接退出，不会静默默认 sing-box。

### 只查看计划

```bash
sudo bash install.sh --protocol singbox --mode dry-run
```

### 使用本地 VPNView 二进制

```bash
sudo bash install.sh --local ./vpnview-linux-amd64 --protocol singbox --mode takeover
```

## sing-box takeover 前置条件

sing-box takeover 需要系统中已经存在兼容的 `sing-box` 二进制。

可以通过环境变量指定路径：

```bash
sudo env VPNVIEW_CLIENT_BIN=/usr/local/bin/sing-box bash install.sh --protocol singbox --mode takeover
```

VPNView 的 sing-box 管理链路依赖 sing-box 的 `experimental.v2ray_api`。常见官方构建可能不包含 `with_v2ray_api`，这种情况下 takeover 会被预检阻断。

典型错误：

```text
v2ray api is not included in this build, rebuild with -tags with_v2ray_api
```

含义：当前 sing-box 构建不包含 VPNView 所需的 V2Ray Stats API 兼容接口。请更换兼容构建，或使用 `--mode panel-only`。

`v2ray_api` 是 sing-box 的兼容统计 API 名称，不代表当前核心是 V2Ray/Xray。

## 端口

默认涉及以下端口：

| 端口 | 用途 | 是否应公网开放 |
| --- | --- | --- |
| `19463` | VPNView 面板 | 按你的部署决定 |
| `10085` | sing-box V2Ray Stats API | 否，只应监听 `127.0.0.1` |
| 核心入站端口 | 实际代理协议端口 | 是，按节点配置开放 |

如果使用现有 sing-box 配置接管，VPNView 会尽量保留原来的监听端口、TLS、Reality、路由、DNS、出站等非用户配置。


## 主要配置

正式使用前至少检查：

- `auth.secret`：管理密钥，必须修改。
- `server.listen`：面板监听地址，默认 `0.0.0.0:19463`。
- `store.sqlite.path`：SQLite 数据库路径。
- `cores.default`：默认核心 ID。
- `cores.items.<id>.type`：核心类型，例如 `singbox`。
- `cores.items.<id>.config.*`：核心配置路径、模板路径、重载命令、订阅参数。
- `subscription.domain`：全局订阅域名兜底。
- `server.tls`：只有面板直接启用 HTTPS 时才需要配置。

sing-box 示例：

```yaml
cores:
  default: "singbox-main"
  enabled: ["singbox-main"]
  items:
    singbox-main:
      type: "singbox"
      enabled: true
      role: "primary"
      config:
        singbox_config_path: "/etc/sing-box/config.json"
        config_template_path: "/etc/vpnview/singbox_template.json"
        reload_command: "systemctl reload sing-box"
        clash_api: "http://127.0.0.1:9090"
        clash_secret: ""
        v2ray_api: "127.0.0.1:10085"
        subscription_domain: "vpn.example.com"
        subscription_port: 443
        subscription_type: "tcp"
        subscription_tls: true
```

## HTTPS 策略

面板安全模式由 `security.deployment_mode` 控制：

```yaml
security:
  deployment_mode: "insecure"
  cookie_secure: "auto"
  hsts_enabled: false
```

可选模式：

- `insecure`：HTTP 私有面板，不强制 Secure Cookie，不启用 HSTS。
- `self_signed`：自签证书或 HTTPS 反向代理，HTTPS 请求启用 Secure Cookie。
- `strict`：正式 HTTPS 部署，Secure Cookie 始终启用，可开启 HSTS。

订阅节点链接不跟面板 HTTPS 自动绑定。节点链接由各核心的 `subscription_domain`、`subscription_port`、`subscription_tls` 决定。
