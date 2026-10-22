package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileInfo 描述一个已经落在磁盘上的文件。
type FileInfo struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Size int64     `json:"size"`
	At   time.Time `json:"at"`
}

// Event 是推送给所有已连接设备的统一消息体。
type Event struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"` // clip | file | sys
	Text     string    `json:"text,omitempty"`
	From     string    `json:"from,omitempty"`      // 发送方客户端 id
	FromName string    `json:"from_name,omitempty"` // 发送方显示名
	File     *FileInfo `json:"file,omitempty"`
	At       time.Time `json:"at"`
}

// Hub 负责把事件扇出给所有 SSE 订阅者，并保留最近的历史。
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
	hist []Event
	seq  int
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

func (h *Hub) Subscribe() chan Event {
	ch := make(chan Event, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Publish 给事件补上 id/时间，写入历史，并异步扇出（慢客户端不阻塞发送方）。
func (h *Hub) Publish(e Event) Event {
	h.mu.Lock()
	h.seq++
	if e.ID == "" {
		e.ID = fmt.Sprintf("e%d", h.seq)
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	h.hist = append(h.hist, e)
	if len(h.hist) > 60 {
		h.hist = h.hist[len(h.hist)-60:]
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default: // 客户端太慢，丢弃这一条，避免拖住整个服务
		}
	}
	h.mu.Unlock()
	return e
}

func (h *Hub) History() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Event, len(h.hist))
	copy(out, h.hist)
	return out
}

func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// FileStore 维护「收到的文件」索引，文件名落盘时加上随机前缀避免覆盖。
type FileStore struct {
	mu    sync.Mutex
	dir   string
	items map[string]FileInfo
	path  map[string]string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir, items: make(map[string]FileInfo), path: make(map[string]string)}
}

var errBadName = errors.New("文件名不合法")

func safeName(name string) (string, error) {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "_" {
		return "", errBadName
	}
	if len(name) > 120 {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		base := strings.TrimSuffix(name, ext)
		if len(base) > 100 {
			base = base[:100]
		}
		name = base + ext
	}
	return name, nil
}

// Add 把 r 写到保存目录，返回登记后的文件信息。
func (fs *FileStore) Add(r io.Reader, name string) (FileInfo, error) {
	clean, err := safeName(name)
	if err != nil {
		return FileInfo{}, err
	}
	if err := os.MkdirAll(fs.dir, 0o755); err != nil {
		return FileInfo{}, fmt.Errorf("无法创建保存目录: %w", err)
	}
	id := randomHex(6)
	dst := filepath.Join(fs.dir, id[:4]+"-"+clean)
	f, err := os.Create(dst)
	if err != nil {
		return FileInfo{}, err
	}
	n, err := io.Copy(f, r)
	cerr := f.Close()
	if err != nil {
		os.Remove(dst)
		return FileInfo{}, err
	}
	if cerr != nil {
		os.Remove(dst)
		return FileInfo{}, cerr
	}
	info := FileInfo{ID: id, Name: clean, Size: n, At: time.Now()}

	fs.mu.Lock()
	fs.items[id] = info
	fs.path[id] = dst
	fs.mu.Unlock()
	return info, nil
}

// AddFile 把本地已有的文件登记进索引（不复制内容）。
func (fs *FileStore) AddFile(absPath, displayName string) (FileInfo, error) {
	st, err := os.Stat(absPath)
	if err != nil {
		return FileInfo{}, err
	}
	id := randomHex(6)
	info := FileInfo{ID: id, Name: displayName, Size: st.Size(), At: time.Now()}
	fs.mu.Lock()
	fs.items[id] = info
	fs.path[id] = absPath
	fs.mu.Unlock()
	return info, nil
}

func (fs *FileStore) Get(id string) (FileInfo, string, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	info, ok := fs.items[id]
	if !ok {
		return FileInfo{}, "", false
	}
	return info, fs.path[id], true
}

func (fs *FileStore) List() []FileInfo {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]FileInfo, 0, len(fs.items))
	for _, v := range fs.items {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// HumanSize 把字节数变成 "1.2 MB" 这种好读的形式。
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
