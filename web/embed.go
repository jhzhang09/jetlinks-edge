// Package web 提供前端静态资源的嵌入支持。
// Author: jhzhang
// Date: 2026-06-14
package web

import "embed"

// DistFS 导出前端打包后的静态资源文件系统。
// 使用 "all:dist" 确保打包能够包含以点或下划线开头的隐藏资源文件。
//go:embed all:dist
var DistFS embed.FS
