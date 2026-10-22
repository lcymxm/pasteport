//go:build !windows

package app

import "errors"

// 其他平台暂时没有开机自启的实现。

var errNoStartup = errors.New("当前平台不支持开机自启")

func StartupEnabled() bool                     { return false }
func SetStartup(bool) error                    { return errNoStartup }
func ToggleStartup() (bool, error)             { return false, errNoStartup }
func StartupCommand() string                   { return "" }
func HideConsoleWindow()                       {}
