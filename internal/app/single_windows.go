//go:build windows

package app

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32dll     = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32dll.NewProc("CreateMutexW")
	procSetLastErr  = kernel32dll.NewProc("SetLastError")
	procCloseHandle = kernel32dll.NewProc("CloseHandle")
)

var singleMutex uintptr

// InstanceTag 用数据目录派生一个稳定短标记，
// 这样不同数据目录的两个 PastePort 可以各跑各的。
func InstanceTag(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	sum := sha1.Sum([]byte(abs))
	return hex.EncodeToString(sum[:4])
}

// AcquireSingle 取一个命名互斥体。返回 already=true 表示本机已有实例在跑。
func AcquireSingle(tag string) (already bool, err error) {
	name, err := syscall.UTF16PtrFromString("Local\\PastePort-" + tag)
	if err != nil {
		return false, err
	}
	// GetLastError 是复用的，先清零才能区分「刚创建」和「早就存在」
	procSetLastErr.Call(0)
	h, _, lastErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return false, lastErr
	}
	if lastErr == syscall.ERROR_ALREADY_EXISTS {
		procCloseHandle.Call(h)
		return true, nil
	}
	singleMutex = h
	return false, nil
}

// ReleaseSingle 释放互斥体（进程退出时系统也会自动释放）。
func ReleaseSingle() {
	if singleMutex != 0 {
		procCloseHandle.Call(singleMutex)
		singleMutex = 0
	}
}
