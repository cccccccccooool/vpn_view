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

### 参数化安装与核心接管

```bash
# 仅接管已有核心配置（若已有内核不兼容，则会触发阻断报错）
sudo bash install.sh --protocol singbox --mode takeover

# 接管配置并自动下载/安装非官方兼容的 sing-box 核心二进制
sudo bash install.sh --protocol singbox --mode takeover --install-core
```

* `--install-core`：在进行接管前，自动检测当前架构（`amd64` / `arm64`），拉取带 `with_v2ray_api` 标签的非官方兼容核心进行替换，并在替换前自动备份原核心。下载完成后会进行 SHA-256 完整性校验。
* `--no-install-core`：明确指定不下载兼容核心。

### 交互式安装

```bash
sudo bash install.sh --interactive
```

交互模式会在安装时引导选择协议和部署模式。如果检测到底层核心不兼容，且在 TTY/终端交互环境下，会提示并询问用户是否自动下载安装非官方兼容核心。

### 非交互自动化环境

非交互环境在未显式传递协议参数时不会静默安装。若需脚本完全静默运行，必须显式传递协议和安装参数：

```bash
# 例子：非交互模式下静默下载面板并接管/安装兼容核心
sudo env VPNVIEW_PROTOCOL=singbox VPNVIEW_MODE=takeover VPNVIEW_INSTALL_CORE=1 bash install.sh
```

### 仅预览安装计划 (Dry Run)

```bash
sudo bash install.sh --protocol singbox --mode dry-run
```

### 使用本地 VPNView 面板二进制进行部署

```bash
sudo bash install.sh --local ./vpnview-linux-amd64 --protocol singbox --mode takeover --install-core
```

## sing-box takeover 核心接管与前置条件

VPNView 的核心流量统计与动态限速依赖于底层 `sing-box` 内核中的 V2Ray Stats API 功能。官方 APT 仓库及默认发布的 `sing-box` 二进制通常**不包含**该统计组件（编译缺少 `-tags with_v2ray_api`），因此在执行 `takeover` 模式前：

1. **自动前置安装**: 运行安装脚本时，推荐传入 `--install-core` 参数。脚本会自动安装经过合规审计与 SHA 校验的兼容核心二进制；
2. **已有兼容检测**: 如果系统中已有名为 `sing-box` 且包含 `with_v2ray_api` 标签的兼容内核，安装脚本会自动跳过重复下载，保留并接管现有核心；
3. **指定自定义路径**: 如果兼容内核存放在特殊路径，可使用环境变量指定：

```bash
sudo env VPNVIEW_CLIENT_BIN=/usr/local/custom/sing-box bash install.sh --protocol singbox --mode takeover
```

*核心预检检测在任何实质性安装与系统配置写入之前执行，确保在环境不符合时不会留下脏配置或对系统产生修改。*


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

## 开源许可证

VPNView 自有代码采用 **GNU GPL version 3 or later**（[GPL-3.0-or-later](LICENSE)）开源许可证发布。本项目的第三方组件仍遵循各自原作者的开源许可证协议，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 非官方 sing-box 核心声明

本项目提供的 `vpnview-core-linux-*` 核心二进制，是基于 [SagerNet/sing-box](https://github.com/SagerNet/sing-box) 源码、启用 `with_v2ray_api` 编译标签生成的 **非官方兼容构建**。本构建并非 SagerNet 官方发行版，也不代表获得 SagerNet 的关联、认可或技术支持。

