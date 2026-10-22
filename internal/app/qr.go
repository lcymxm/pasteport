package app

import (
	"strings"

	qr "pasteport/internal/qr"
)

// QRPNG 返回可直接用于 <img src> 的二维码图片。
func QRPNG(text string) []byte {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil
	}
	return code.PNG()
}

// QRASCII 把二维码渲染成终端里可以直接扫的字符画。
// 白色模块画成实心块，黑色模块留空——在深色终端里正好是标准黑白的反色关系。
func QRASCII(text string) string {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return ""
	}
	const quiet = 2
	size := code.Size
	white := func(x, y int) bool {
		if x < 0 || y < 0 || x >= size || y >= size {
			return true // 静默区当作白色
		}
		return !code.Black(x, y)
	}
	var b strings.Builder
	for y := -quiet; y < size+quiet; y += 2 {
		for x := -quiet; x < size+quiet; x++ {
			top := white(x, y)
			bottom := white(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
