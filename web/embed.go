// Package web 提供嵌入式前端静态资源。
// Embeds all frontend static files for serving via HTTP.
package web

import "embed"

// FS 包含当前目录下所有前端静态文件（HTML/CSS/JS 等），
// 通过 Go embed 机制在编译时打包进二进制文件，运行时无需外部文件依赖。
//
//go:embed *
var FS embed.FS
