package app

import (
	"errors"
	"os"
	"path/filepath"
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

// WriteQRFile 把二维码按原始分辨率存成 PNG，返回文件路径。
//
// 终端里的字符画依赖字体和配色，不是所有环境都能扫；
// 这张图是「一定能扫」的兜底，托盘菜单里可以直接打开。
func WriteQRFile(dir, text string) (string, error) {
	data := QRPNG(text)
	if len(data) == 0 {
		return "", errors.New("二维码生成失败")
	}
	path := filepath.Join(dir, "qr.png")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// QRASCII 把二维码渲染成终端里可以直接扫的字符画。
// 白色模块画成实心块，黑色模块留空——在深色终端里正好是标准黑白的反色关系。
//
// 注意：静默区（quiet zone）必须是 4 个模块，少一格都可能扫不出来。
func QRASCII(text string) string {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return ""
	}
	const quiet = 4
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
