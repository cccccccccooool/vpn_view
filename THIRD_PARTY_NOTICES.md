# 第三方依赖与开源声明 (Third Party Notices)

本文件列出了 VPNView 中使用的第三方开源组件、其对应的版本、上游项目地址、许可证名称（SPDX 标识）、版权声明以及使用方式。

---

## 目录
1. [Go 后端依赖 (Go Backend Dependencies)](#1-go-后端依赖-go-backend-dependencies)
2. [前端依赖 (Frontend Dependencies)](#2-前端依赖-frontend-dependencies)
3. [安装脚本下载组件 (Installer Components)](#3-安装脚本下载组件-installer-components)
4. [GitHub Actions 依赖 (GitHub Actions Dependencies)](#4-github-actions-依赖-github-actions-dependencies)
5. [许可证全文说明 (License Texts)](#5-许可证全文说明-license-texts)

---

## 1. Go 后端依赖 (Go Backend Dependencies)

以下 Go 依赖在编译时静态链接合并至 VPNView 二进制发行版中：

| 组件名称 | 使用版本 | 上游项目地址 | 许可证名称与 SPDX | 版权声明 (Copyright) | 许可证全文位置 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **go-humanize** | `v1.0.1` | [github.com/dustin/go-humanize](https://github.com/dustin/go-humanize) | MIT | Copyright (c) 2005-2008 Dustin Sallings | `LICENSES/MIT.txt` |
| **jwt/v5** | `v5.2.1` | [github.com/golang-jwt/jwt](https://github.com/golang-jwt/jwt) | MIT | Copyright (c) 2022 DGrijalva | `LICENSES/MIT.txt` |
| **protobuf** | `v1.5.4` | [github.com/golang/protobuf](https://github.com/golang/protobuf) | BSD-3-Clause | Copyright (c) 2010 The Go Authors | `LICENSES/BSD-3-Clause.txt` |
| **go-isatty** | `v0.0.20` | [github.com/mattn/go-isatty](https://github.com/mattn/go-isatty) | MIT | Copyright (c) Yasuhiro Matsumoto | `LICENSES/MIT.txt` |
| **go-strftime** | `v0.1.9` | [github.com/ncruces/go-strftime](https://github.com/ncruces/go-strftime) | MIT | Copyright (c) 2023 Nuno Cruces | `LICENSES/MIT.txt` |
| **bigfft** | `v0.0.0` | [github.com/remyoudompheng/bigfft](https://github.com/remyoudompheng/bigfft) | BSD-3-Clause | Copyright (c) 2012 Rémy Oudompheng | `LICENSES/BSD-3-Clause.txt` |
| **x/net** | `v0.28.0` | [golang.org/x/net](https://cs.opensource.google/go/x/net/) | BSD-3-Clause | Copyright (c) 2009 The Go Authors | `LICENSES/BSD-3-Clause.txt` |
| **x/sys** | `v0.24.0` | [golang.org/x/sys](https://cs.opensource.google/go/x/sys/) | BSD-3-Clause | Copyright (c) 2009 The Go Authors | `LICENSES/BSD-3-Clause.txt` |
| **x/text** | `v0.17.0` | [golang.org/x/text](https://cs.opensource.google/go/x/text/) | BSD-3-Clause | Copyright (c) 2009 The Go Authors | `LICENSES/BSD-3-Clause.txt` |
| **genproto** | `v0.0.0` | [google.golang.org/genproto](https://github.com/googleapis/go-genproto) | Apache-2.0 | Copyright 2020 Google LLC | `LICENSES/Apache-2.0.txt` |
| **grpc** | `v1.67.1` | [google.golang.org/grpc](https://github.com/grpc/grpc-go) | Apache-2.0 | Copyright 2015 gRPC authors | `LICENSES/Apache-2.0.txt` |
| **protobuf-go** | `v1.34.2` | [google.golang.org/protobuf](https://github.com/protocolbuffers/protobuf-go) | BSD-3-Clause | Copyright (c) 2018 The Go Authors | `LICENSES/BSD-3-Clause.txt` |
| **yaml.v3** | `v3.0.1` | [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) | MIT | Copyright (c) 2019 Kirill Simonov | `LICENSES/MIT.txt` |
| **modernc.org/libc** | `v1.55.3` | [gitlab.com/cznic/libc](https://gitlab.com/cznic/libc) | BSD-3-Clause | Copyright (c) 2016 The modernc.org Authors | `LICENSES/BSD-3-Clause.txt` |
| **modernc.org/mathutil** | `v1.6.0` | [gitlab.com/cznic/mathutil](https://gitlab.com/cznic/mathutil) | BSD-3-Clause | Copyright (c) 2014 The modernc.org Authors | `LICENSES/BSD-3-Clause.txt` |
| **modernc.org/memory** | `v1.8.0` | [gitlab.com/cznic/memory](https://gitlab.com/cznic/memory) | BSD-3-Clause | Copyright (c) 2016 The modernc.org Authors | `LICENSES/BSD-3-Clause.txt` |
| **modernc.org/sqlite** | `v1.34.5` | [gitlab.com/cznic/sqlite](https://gitlab.com/cznic/sqlite) | BSD-3-Clause | Copyright (c) 2017 The modernc.org Authors | `LICENSES/BSD-3-Clause.txt` |

---

## 2. 前端依赖 (Frontend Dependencies)

- **VPNView 前端静态资源**:
  - `web/index.html` 及其下的所有 CSS (`web/css/style.css`)、JavaScript 代码 (`web/js/*.js`) 以及矢量图标 (`web/assets/favicon.svg`) **全部为 VPNView 项目自有原生原创代码**。
  - 未使用任何外部的第三方 JavaScript/CSS/图标/字体等 CDN 库或本地引用包。
  - 遵循 VPNView 自有项目的开源许可证声明（GPL-3.0-or-later）。

---

## 3. 安装脚本下载组件 (Installer Components)

安装脚本 `install.sh` 可选择下载并部署以下外部二进制组件：

### 3.1. sing-box 非官方兼容核心 (vpnview-core-linux-*)
- **版本与来源**: 对应 Release 中分发的兼容构建编译自 [SagerNet/sing-box](https://github.com/SagerNet/sing-box)。
- **许可证类型**: **GPL-3.0-or-later**。
- **发布特别说明**: 本构建为 VPNView 团队独立为实现 API 统计功能而带 `with_v2ray_api` 标签编译的 **非官方兼容构建**，非 SagerNet 官方发布，亦不代表其任何立场。
- **源码对应性**: 对应源码压缩包与构建元数据 (`BUILDINFO-sing-box.txt`) 在相同 Release 中打包随二进制分发。

---

## 4. GitHub Actions 依赖 (GitHub Actions Dependencies)

在自动化 CI/CD 构建中，我们通过 GitHub Actions 使用了以下第三方组件。构建配置中通过特定的 commit hash 进行了固定，以防漂移风险：

- **actions/checkout**: 
  - 上游项目: [github.com/actions/checkout](https://github.com/actions/checkout)
  - 许可证: **MIT** (Copyright (c) 2018 GitHub, Inc.)
- **actions/setup-go**:
  - 上游项目: [github.com/actions/setup-go](https://github.com/actions/setup-go)
  - 许可证: **MIT** (Copyright (c) 2021 GitHub, Inc.)
- **goreleaser/goreleaser-action**:
  - 上游项目: [github.com/goreleaser/goreleaser-action](https://github.com/goreleaser/goreleaser-action)
  - 许可证: **MIT** (Copyright (c) 2019 GoReleaser)

---

## 5. 许可证全文说明 (License Texts)

所有用到的第三方开源许可证全文均保存在本项目的 `LICENSES/` 目录中：
- GPL 3.0 或更新版本许可证：[LICENSES/GPL-3.0-or-later.txt](file:///d:/code/vpn_view/LICENSES/GPL-3.0-or-later.txt)
- Apache 2.0 许可证：[LICENSES/Apache-2.0.txt](file:///d:/code/vpn_view/LICENSES/Apache-2.0.txt)
- MIT 许可证：[LICENSES/MIT.txt](file:///d:/code/vpn_view/LICENSES/MIT.txt)
- BSD 3-Clause 许可证：[LICENSES/BSD-3-Clause.txt](file:///d:/code/vpn_view/LICENSES/BSD-3-Clause.txt)
