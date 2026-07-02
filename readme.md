# VPNView

轻量 VPN 用户管理面板。当前主要支持：

- `sing-box`
- `xray` / `v2ray`
- `stub` 测试模式

## 快速安装

Linux 一键安装：

```bash
sudo bash install.sh --protocol singbox --mode takeover
```

非交互环境必须显式指定协议：

```bash
sudo env VPNVIEW_PROTOCOL=singbox bash install.sh
```

常用模式：

```bash
# 只安装面板，不接管核心
sudo bash install.sh --protocol singbox --mode panel-only

# 只查看安装计划，不写文件
sudo bash install.sh --protocol singbox --mode dry-run

# 使用本地二进制
sudo bash install.sh --local ./vpnview-linux-amd64 --protocol singbox
```

## 手动运行

复制配置：

```bash
cp configs/config.example.yaml config.yaml
```

启动：

```bash
go run ./cmd/vpnview -config config.yaml
```

或构建后运行：

```bash
go build -o vpnview ./cmd/vpnview
./vpnview -config config.yaml
```

只初始化核心配置后退出：

```bash
./vpnview -config config.yaml -init-once
```

## 配置

默认配置可以启动，但正式使用前至少检查：

- `auth.secret`：管理密钥，必须改。
- `server.listen`：面板监听地址，默认 `0.0.0.0:19463`。
- `store.sqlite.path`：数据库路径。
- `cores`：核心类型、核心配置路径、模板路径、重载命令。
- `subscription.domain`：全局订阅域名兜底。
- `subscription_domain` / `subscription_port` / `subscription_tls`：单核心订阅参数，优先于全局订阅域名。
- `server.tls`：只有面板直接启用 HTTPS 时才需要配置。

示例：

```yaml
cores:
  default: "singbox-main"
  enabled: ["singbox-main"]
  items:
    singbox-main:
      type: "singbox"
      enabled: true
      config:
        singbox_config_path: "/etc/sing-box/config.json"
        config_template_path: "/etc/sing-box/config.template.json"
        reload_command: "systemctl reload sing-box"
        subscription_domain: "vpn.example.com"
        subscription_port: 443
        subscription_tls: true
```

## 面板 HTTPS 策略

按部署环境选择：

```yaml
security:
  deployment_mode: "insecure"   # 无证书/HTTP 私有面板
  cookie_secure: "auto"         # auto / always / never
```

三种模式：

- `insecure`：无域名或无证书，HTTP 可登录，不启用 Secure Cookie 和 HSTS。
- `self_signed`：自签证书或 HTTPS 反向代理，HTTPS 请求启用 Secure Cookie。
- `strict`：正式 HTTPS 部署，Secure Cookie 始终启用，可启用 HSTS。

订阅链接不跟面板 HTTPS 自动绑定，仍由各核心的 `subscription_*` 配置决定。