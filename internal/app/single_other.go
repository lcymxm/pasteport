//go:build !windows

package app

// 其他平台暂不实现单实例。

func InstanceTag(string) string          { return "" }
func AcquireSingle(string) (bool, error) { return false, nil }
func ReleaseSingle()                     {}
