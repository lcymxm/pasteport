//go:build !windows

// Package tray 在非 Windows 平台上是个空实现，只为让代码能跨平台编译。
package tray

import "errors"

type Actions struct {
	OpenConsole    func()
	CopyPairCode   func()
	StartupEnabled func() bool
	ToggleStartup  func() (bool, error)
	Quit           func()
}

type Tray struct{}

func Register(iconPNG []byte, tip, tmpDir string, a Actions) (*Tray, error) {
	return nil, errors.New("当前平台不支持托盘图标")
}

func (t *Tray) Notify(title, body string) {}
func (t *Tray) Dispose()                  {}
