package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RuntimeInfo 是「当前正在运行的那个实例」的坐标。
// 单实例逻辑靠它把第二次双击导向已经开着的那个窗口。
type RuntimeInfo struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Version string `json:"version"`
	Started string `json:"started"`
}

func runtimePath(dir string) string {
	return filepath.Join(dir, "runtime.json")
}

// WriteRuntime 把当前实例信息写进数据目录（0600）。
func WriteRuntime(dir string, info RuntimeInfo) error {
	if info.Started == "" {
		info.Started = time.Now().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(runtimePath(dir), b, 0o600)
}

// ReadRuntime 读取当前实例信息；文件不存在或已损坏返回 ok=false。
func ReadRuntime(dir string) (RuntimeInfo, bool) {
	var info RuntimeInfo
	b, err := os.ReadFile(runtimePath(dir))
	if err != nil {
		return info, false
	}
	if err := json.Unmarshal(b, &info); err != nil || info.Port <= 0 {
		return info, false
	}
	return info, true
}

// ClearRuntime 退出时清掉，免得下次误以为还有实例在跑。
func ClearRuntime(dir string) {
	os.Remove(runtimePath(dir))
}
