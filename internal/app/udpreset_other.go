//go:build !windows

package app

import "net"

// 非 Windows 平台没有这个坑，保留空实现即可。
func disableUDPConnReset(*net.UDPConn) {}
