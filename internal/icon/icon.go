// Package icon 内嵌应用图标，托盘和窗口都要用。
package icon

import _ "embed"

//go:embed icon.png
var PNG []byte
