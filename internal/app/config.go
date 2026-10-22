package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

// Config 是持久化在数据目录下的设置。配对码和令牌在首次运行时随机生成。
type Config struct {
	Name    string `json:"name"`     // 电脑显示名
	Port    int    `json:"port"`     // 监听端口
	Code    string `json:"code"`     // 6 位配对码
	Token   string `json:"token"`    // 长期令牌（手机保存它，之后免配对）
	SaveDir string `json:"save_dir"` // 收到的文件保存目录

	dir    string // 数据目录（不写进 json）
	saved  bool   // 是否成功落盘
	saveErr error
}

var dataDirOverride string

// SetDataDir 由 --data 参数指定，优先级最高。
func SetDataDir(dir string) { dataDirOverride = dir }

// ResolveDataDir 找一个真正可写的数据目录。
// 依次尝试：--data → 用户配置目录 → LOCALAPPDATA → exe 同目录。
// 这样即使漫游配置目录是只读的（企业环境、受限账户），程序也不会直接崩掉。
func ResolveDataDir() (string, error) {
	if dataDirOverride != "" {
		abs, err := filepath.Abs(dataDirOverride)
		if err != nil {
			abs = dataDirOverride
		}
		if ok := ensureWritable(abs); ok {
			return abs, nil
		}
		return "", fmt.Errorf("指定的数据目录不可写: %s", abs)
	}

	var candidates []string
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		candidates = append(candidates, filepath.Join(d, "PastePort"))
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		candidates = append(candidates, filepath.Join(d, "PastePort"))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "PastePort-data"))
	}
	candidates = append(candidates, filepath.Join(".", "PastePort-data"))

	for _, c := range candidates {
		if ensureWritable(c) {
			return c, nil
		}
	}
	return "", errors.New("找不到可写的数据目录，请用 --data 指定一个")
}

func ensureWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

// DefaultSaveDir 优先用「下载」目录，否则退回家目录。
func DefaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if st, err := os.Stat(filepath.Join(home, "Downloads")); err == nil && st.IsDir() {
		return filepath.Join(home, "Downloads", "PastePort")
	}
	return filepath.Join(home, "PastePort")
}

// LoadConfig 读取配置；缺失或损坏的字段会被补齐并写回。
// 写入失败不算致命错误，只是本次运行不持久化。
func LoadConfig(dir string) (*Config, error) {
	cfg := &Config{dir: dir}
	path := filepath.Join(dir, "config.json")
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("配置文件损坏（%s）: %w", path, err)
		}
	}
	changed := false
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name, _ = os.Hostname()
		if cfg.Name == "" {
			cfg.Name = "My PC"
		}
		changed = true
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		cfg.Port = 8787
		changed = true
	}
	if !ValidCode(cfg.Code) {
		cfg.Code = randomDigits(6)
		changed = true
	}
	if len(cfg.Token) < 32 {
		cfg.Token = randomHex(16)
		changed = true
	}
	if strings.TrimSpace(cfg.SaveDir) == "" {
		cfg.SaveDir = DefaultSaveDir()
		changed = true
	}
	if changed {
		_ = cfg.Save() // 保存失败只影响下次启动，不阻断本次运行
	}
	return cfg, nil
}

// Save 以 0600 权限原子写入配置文件。
func (c *Config) Save() error {
	if c.dir == "" {
		return errors.New("未设置数据目录")
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		c.saveErr = err
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, "config.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		c.saveErr = err
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		c.saveErr = err
		return err
	}
	c.saved = true
	c.saveErr = nil
	return nil
}

// SaveError 返回上一次保存失败的原因（供启动时提示）。
func (c *Config) SaveError() error { return c.saveErr }

// Dir 返回实际使用的数据目录。
func (c *Config) Dir() string { return c.dir }

// ValidCode 判断配对码是否为恰好 6 位数字。
func ValidCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func randomDigits(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			b.WriteByte('0')
			continue
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String()
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}

// SpacedCode 把 123456 显示成 "123 456"，方便照着念和输入。
func SpacedCode(code string) string {
	if len(code) != 6 {
		return code
	}
	return code[:3] + " " + code[3:]
}
