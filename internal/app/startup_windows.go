//go:build windows

package app

import (
	"os"
	"syscall"
	"unsafe"
)

// 新版 Go 的 syscall 包已经砍掉了注册表写入相关的函数，
// 所以这里直接调 advapi32，反正也就四个调用。
var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValue  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName    = "PastePort"

	keyQueryValue = 0x0001
	keySetValue   = 0x0002
	regSZ         = 1
	errorSuccess  = 0
	errorNotFound = 2 // ERROR_FILE_NOT_FOUND
)

type regKey syscall.Handle

func openRunKey(access uint32) (regKey, error) {
	path, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := procRegOpenKeyExW.Call(
		hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, uintptr(access),
		uintptr(unsafe.Pointer(&h)))
	if r != errorSuccess {
		return 0, syscall.Errno(r)
	}
	return regKey(h), nil
}

func (k regKey) close() {
	procRegCloseKey.Call(uintptr(k))
}

// StartupEnabled 判断注册表里有没有 PastePort 的开机启动项。
func StartupEnabled() bool {
	k, err := openRunKey(keyQueryValue)
	if err != nil {
		return false
	}
	defer k.close()

	name, err := syscall.UTF16PtrFromString(runValueName)
	if err != nil {
		return false
	}
	var typ, size uint32
	buf := make([]uint16, 512)
	size = uint32(len(buf) * 2)
	r, _, _ := procRegQueryValueEx.Call(
		uintptr(k), uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)))
	return r == errorSuccess && typ == regSZ && size > 2
}

// SetStartup 写入 / 移除 HKCU 下的开机启动项（不需要管理员权限）。
func SetStartup(enable bool) error {
	k, err := openRunKey(keySetValue | keyQueryValue)
	if err != nil {
		return err
	}
	defer k.close()

	name, err := syscall.UTF16PtrFromString(runValueName)
	if err != nil {
		return err
	}

	if !enable {
		r, _, _ := procRegDeleteValue.Call(uintptr(k), uintptr(unsafe.Pointer(name)))
		if r != errorSuccess && r != errorNotFound {
			return syscall.Errno(r)
		}
		return nil
	}

	value, err := syscall.UTF16FromString(StartupCommand())
	if err != nil {
		return err
	}
	r, _, _ := procRegSetValueExW.Call(
		uintptr(k), uintptr(unsafe.Pointer(name)), 0, regSZ,
		uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	if r != errorSuccess {
		return syscall.Errno(r)
	}
	return nil
}

// ToggleStartup 翻转开机自启，返回新的状态。
func ToggleStartup() (bool, error) {
	next := !StartupEnabled()
	if err := SetStartup(next); err != nil {
		return StartupEnabled(), err
	}
	return next, nil
}

// StartupCommand 返回写进注册表的那条命令。
func StartupCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return `"` + exe + `" --tray --no-open`
}
