## VPNView 安装与 sing-box 接管模式问题反馈

测试环境：

* 系统：Debian
* 核心：sing-box
* 安装模式：`--protocol singbox --mode takeover`
* 部署环境：全新 VPS
* VPNView 与 sing-box 均使用 systemd 管理

### 1. 安装脚本没有进入交互安装模式

直接执行：

```bash
bash install.sh --protocol singbox --mode takeover
```

安装脚本全程没有出现交互式配置，包括：

* 面板端口
* 代理入站端口
* 管理密码或 `auth.secret`
* sing-box 路径
* 核心类型确认
* 协议参数
* 初始管理员或用户信息

如果项目设计上本来支持交互安装，希望检查当前交互逻辑是否没有被触发。

如果当前只支持参数化安装，建议：

1. 在 README 中明确说明没有交互安装；
2. 提供 `--interactive` 参数；
3. 缺少必要参数时进入交互引导，而不是直接使用固定默认值。

---

### 2. 安装脚本没有自动安装 sing-box

在全新的 Debian VPS 上运行时，安装直接失败：

```text
[ERROR] sing-box binary was not found. Install it first, or rerun with VPNVIEW_CLIENT_BIN=/path/to/sing-box
```

随后脚本自动回滚。

问题在于 README 或安装体验容易让用户理解为“一键安装”，但实际要求用户预先安装核心。

建议：

* 检测不到 sing-box 时询问是否自动安装；
* 或提供 `--install-core` 参数；
* 至少在 README 的安装命令前明确写出 sing-box 是前置依赖；
* 安装前先完成依赖检查，避免写入文件后才失败并回滚。

---

### 3. sing-box 官方构建不包含项目所需的 V2Ray API

安装 VPNView 后，面板报错：

```text
singbox-main: rpc error: code = Unavailable desc = connection error:
desc = "transport: Error while dialing:
dial tcp 127.0.0.1:10085: connect: connection refused"
```

检查发现：

```bash
ss -lntp | grep 10085
```

没有任何监听。

VPNView 配置中默认写入：

```yaml
v2ray_api: "127.0.0.1:10085"
```

但官方安装的 sing-box 二进制默认不包含 V2Ray API。手动向 sing-box 配置加入：

```json
"experimental": {
  "v2ray_api": {
    "listen": "127.0.0.1:10085",
    "stats": {
      "enabled": true
    }
  }
}
```

之后 sing-box 直接报错：

```text
FATAL[0000] create v2ray-server:
v2ray api is not included in this build,
rebuild with -tags with_v2ray_api
```

也就是说，VPNView 的 sing-box 接管模式依赖：

```text
with_v2ray_api
```

编译标签，但安装脚本：

* 不检查当前 sing-box 是否包含该功能；
* 不提供符合要求的 sing-box 构建；
* 不在安装前提示官方 sing-box 默认构建不可用；
* 仍然生成依赖 `127.0.0.1:10085` 的配置。

这是目前最关键的问题。

建议：

1. 安装前检测 sing-box 是否支持 `experimental.v2ray_api`；
2. 若不支持，直接给出明确错误；
3. 提供项目兼容的预编译 sing-box；
4. 或由安装脚本自动编译带 `with_v2ray_api` 的版本；
5. README 明确说明官方默认 sing-box 二进制可能无法使用；
6. 不要等到安装完成后，由面板持续显示模糊的 RPC connection refused。

错误信息最好改成：

```text
当前 sing-box 构建不包含 with_v2ray_api，
VPNView 的用户统计与账户管理功能无法工作。
请安装兼容构建。
```

而不是只显示：

```text
connection refused
```

---

### 4. `v2ray_api` 名称容易让 sing-box 用户误解

虽然使用的是 sing-box 核心，但配置和报错中反复显示：

```text
v2ray_api
```

普通用户很容易理解为脚本错误地安装或启动了 V2Ray 核心。

实际上这是 sing-box 提供的 V2Ray Stats API 兼容接口。

建议在：

* README
* 面板错误信息
* 配置注释
* 安装日志

中说明：

```text
v2ray_api 是 sing-box 的兼容统计 API，
并不代表当前使用的是 V2Ray/Xray 核心。
```

---

### 5. 安装脚本生成的端口或端口说明不一致

安装后实际 sing-box 日志显示：

```text
inbound/shadowsocks[0]: tcp server started at [::]:8080
```

但用户预期的服务端口、面板中展示的端口，以及安装脚本生成的端口似乎不一致。

同时管理 API 固定使用：

```text
127.0.0.1:10085
```

面板默认端口似乎为：

```text
19463
```

目前至少涉及：

* 19463：VPNView 面板
* 10085：sing-box 管理/统计 API
* 8080：自动生成的 Shadowsocks 入站
* 用户在面板创建的节点端口

这些端口的用途、默认值及是否自动开放不够明确。

建议：

1. 安装结束时列出所有端口及用途；
2. 安装时允许自定义面板端口和默认入站端口；
3. 检测端口是否已被占用；
4. 不要固定生成 8080，或至少明确告知；
5. 输出 UFW、firewalld 和云防火墙所需规则；
6. 10085 仅绑定 `127.0.0.1`，明确提示不要对公网开放；
7. 面板中显示“实际生效端口”，避免配置端口与核心监听端口不一致。

---

### 6. 创建账户后没有订阅链接

安装完成后虽然可以创建账户，但账户状态看起来不正常，且没有生成订阅链接。

目前无法确定这是单独的前端/后端问题，还是因为 sing-box 的 `v2ray_api` 没有成功启动，导致账户写入、配置同步或订阅生成流程没有完成。

建议开发者检查以下链路：

1. 创建账户后是否成功写入 SQLite；
2. 是否成功更新 sing-box 配置；
3. 是否调用了配置重载；
4. 重载失败时是否回滚账户；
5. 订阅 token 是否实际生成；
6. 订阅 URL 是否依赖面板的公网地址配置；
7. 前端是否吞掉了后端错误；
8. 核心 API 不可用时是否仍返回“账户创建成功”；
9. 是否只有创建节点后才会生成订阅链接；
10. 订阅基础 URL、协议、域名或 IP 是否为空。

建议创建账户接口返回更完整的信息，例如：

```json
{
  "user_created": true,
  "database_written": true,
  "core_config_updated": false,
  "core_reloaded": false,
  "subscription_created": false,
  "error": "sing-box management API unavailable"
}
```

不要在配置没有生效时只显示创建成功。

---

### 7. 安装程序缺少核心兼容性预检查

当前安装脚本只检查：

```text
sing-box binary 是否存在
```

但没有检查：

* sing-box 版本是否兼容；
* 是否包含 `with_v2ray_api`；
* 配置结构是否适配当前版本；
* API 端口能否启动；
* systemd 服务实际加载哪个配置文件；
* 核心能否成功重载；
* 当前核心究竟是 sing-box、Xray 还是 V2Ray。

建议安装前执行完整预检：

```text
1. 找到核心二进制
2. 获取核心版本
3. 检查所需编译功能
4. 检查配置路径
5. 检查 systemd 服务
6. 检查端口占用
7. 验证测试配置
8. 验证本地 API
9. 再正式安装 VPNView
```

---

### 8. systemd 服务文件存在字段位置错误

生成的 sing-box systemd 服务里：

```ini
[Service]
StartLimitIntervalSec=60
StartLimitBurst=10
```

systemd 日志持续提示：

```text
Unknown key 'StartLimitIntervalSec' in section [Service], ignoring.
```

这两个字段应放在 `[Unit]` 段，而不是 `[Service]` 段。

虽然这不是本次主要故障，但会产生大量无意义警告，也意味着自动重启限制没有按预期生效。

建议修正为：

```ini
[Unit]
StartLimitIntervalSec=60
StartLimitBurst=10
```

---

### 9. 错误日志不够直观

安装完成后，面板只显示：

```text
rpc error: code = Unavailable
connection refused
```

普通用户无法判断是：

* sing-box 没启动；
* API 没配置；
* API 端口错了；
* 二进制缺少编译标签；
* systemd 加载了错误配置；
* 防火墙问题。

建议后端区分并展示：

```text
核心未运行
核心配置校验失败
管理 API 未启用
管理 API 端口未监听
当前核心缺少 with_v2ray_api
核心重载失败
```

同时在面板提供“核心诊断”页面，显示：

* 核心类型；
* 核心版本；
* 二进制路径；
* 配置路径；
* systemd 状态；
* 管理 API 地址；
* API 连通状态；
* 最近一次重载错误。

---

### 10. 安装成功标准不完整

当前脚本似乎在复制文件、写入配置和创建 systemd 服务后就认为安装成功，但没有验证：

* VPNView 服务是否正常；
* sing-box 是否正常；
* 10085 是否监听；
* VPNView 能否连接核心；
* 默认入站是否监听；
* 创建账户是否可用；
* 订阅链接能否访问。

建议安装结束前做自动验收：

```bash
systemctl is-active vpnview
systemctl is-active sing-box
sing-box check -c /etc/sing-box/config.json
检查 127.0.0.1:10085
调用一次核心 API
调用一次 VPNView 健康检查
创建临时测试用户
验证订阅 URL
删除临时测试用户
```

只有全部成功才输出：

```text
Installation completed successfully
```

否则应显示：

```text
Installation completed, but core integration is unavailable
```

---

### 11. 回滚机制虽然存在，但仍需覆盖核心替换与服务状态

脚本失败时可以根据 manifest 删除创建的文件，这一点很好。

但建议继续确认回滚是否能完整恢复：

* 原 sing-box 配置；
* 原核心二进制；
* 原 systemd unit；
* 原服务启停状态；
* 原防火墙规则；
* 原数据库；
* 原端口监听状态。

尤其如果以后安装脚本负责替换带 `with_v2ray_api` 的 sing-box，必须保证失败时恢复原二进制。

---

## 当前最可能影响订阅功能的根因

综合测试结果，目前最可能的主因是：

```text
VPNView 的 sing-box 模式依赖 v2ray_api，
但官方 sing-box 默认构建不包含 with_v2ray_api。
```

因此虽然：

* VPNView 能安装；
* sing-box 能运行；
* 默认 Shadowsocks 入站能监听；

但 VPNView 无法连接核心管理 API，进而可能导致：

* 用户配置无法同步；
* 流量统计不可用；
* 用户状态异常；
* 配置重载失败；
* 订阅链接未生成或没有可用节点。

建议优先修复核心兼容性与安装前检测，再复测订阅链接问题。
