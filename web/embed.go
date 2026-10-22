// Package web 把前端静态资源嵌进 exe，所以只有单个可执行文件，没有外部依赖。
package web

import "embed"

//go:embed index.html app.js style.css
var FS embed.FS
