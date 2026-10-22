//go:build windows

package app

import (
	"net"
	"syscall"
	"unsafe"
)

var (
	ws2_32       = syscall.NewLazyDLL("ws2_32.dll")
	procWSAIoctl = ws2_32.NewProc("WSAIoctl")
)

// SIO_UDP_CONNRESET = _WSAIOW(IOC_VENDOR, 12)
const sioUDPConnReset = 0x98000000 | 12

// disableUDPConnReset 关掉 Windows 的一个历史行为：
// 收到 ICMP 端口不可达时，下一次 recvfrom 会返回 WSAECONNRESET(10054)。
//
// 广播应答这种「发完就走」的场景里，对方关得快一点就会触发；
// 读循环如果把它当致命错误就会退出，表现就是「发现功能用一会儿就没了」。
func disableUDPConnReset(conn *net.UDPConn) {
	rc, err := conn.SyscallConn()
	if err != nil {
		return
	}
	var off int32 // FALSE
	var returned uint32
	_ = rc.Control(func(fd uintptr) {
		procWSAIoctl.Call(
			fd,
			uintptr(sioUDPConnReset),
			uintptr(unsafe.Pointer(&off)),
			unsafe.Sizeof(off),
			0, 0,
			uintptr(unsafe.Pointer(&returned)),
			0, 0,
		)
	})
}
