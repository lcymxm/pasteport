//go:build windows

package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard               = user32.NewProc("OpenClipboard")
	procCloseClipboard              = user32.NewProc("CloseClipboard")
	procEmptyClipboard              = user32.NewProc("EmptyClipboard")
	procGetClipboardData            = user32.NewProc("GetClipboardData")
	procSetClipboardData            = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable  = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequenceNumber  = user32.NewProc("GetClipboardSequenceNumber")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
	procGlobalFree   = kernel32.NewProc("GlobalFree")

	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	procGetConsoleWindow   = kernel32.NewProc("GetConsoleWindow")
	procShowWindow         = user32.NewProc("ShowWindow")
)

// HideConsoleWindow 把控制台窗口藏起来（--tray 用），进程继续在托盘里跑。
func HideConsoleWindow() {
	h, _, _ := procGetConsoleWindow.Call()
	if h != 0 {
		procShowWindow.Call(h, 0) // SW_HIDE
	}
}

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	maxClipBytes  = 1 << 20 // 超过 1 MB 的剪贴板内容不推送
)

// EnableUTF8Console 让 Windows 控制台能正确显示中文和二维码方块字符。
func EnableUTF8Console() {
	procSetConsoleOutputCP.Call(65001)
}

func clipboardSeq() uint32 {
	v, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(v)
}

func openClipboard() error {
	for i := 0; i < 20; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("剪贴板被其他程序占用")
}

// GetClipboardText 读取剪贴板里的纯文本。没有文本或内容过大时返回空串。
func GetClipboardText() (string, error) {
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText); r == 0 {
		return "", nil
	}
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", errors.New("无法锁定剪贴板内存")
	}
	defer procGlobalUnlock.Call(h)

	size, _, _ := procGlobalSize.Call(h)
	if size < 2 || size > maxClipBytes*2 {
		return "", nil
	}
	buf := unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(size)/2)
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(utf16.Decode(buf[:n])), nil
}

// SetClipboardText 把文本写入剪贴板。
func SetClipboardText(s string) error {
	u := utf16.Encode([]rune(s))
	size := (len(u) + 1) * 2
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if h == 0 {
		return errors.New("无法分配剪贴板内存")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return errors.New("无法锁定剪贴板内存")
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)+1)
	copy(dst, u)
	dst[len(u)] = 0
	procGlobalUnlock.Call(h)

	if err := openClipboard(); err != nil {
		procGlobalFree.Call(h)
		return err
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return errors.New("写入剪贴板失败")
	}
	// 成功后内存归系统所有，不能再 GlobalFree
	return nil
}

// ClipboardWatcher 轮询剪贴板序列号，把「本机复制的新内容」回调出去。
type ClipboardWatcher struct {
	onClip func(string)

	mu        sync.Mutex
	lastSeq   uint32
	lastText  string
	lastImage string
	onImage   func([]byte)
	echo      map[string]struct{} // 我们自己写进去的文本，不要回传
}

// OnImage 注册「剪贴板里出现了新图片」的回调，需在 Run 之前调用。
func (w *ClipboardWatcher) OnImage(fn func([]byte)) {
	w.mu.Lock()
	w.onImage = fn
	w.mu.Unlock()
}

func NewClipboardWatcher(onClip func(string)) *ClipboardWatcher {
	return &ClipboardWatcher{
		onClip:  onClip,
		lastSeq: clipboardSeq(),
		echo:    make(map[string]struct{}),
	}
}

// SetText 写剪贴板并抑制回声，避免「收到手机内容 → 又推回手机」的死循环。
func (w *ClipboardWatcher) SetText(s string) error {
	w.mu.Lock()
	w.echo[s] = struct{}{}
	if len(w.echo) > 8 {
		w.echo = map[string]struct{}{s: {}}
	}
	w.lastText = s
	w.mu.Unlock()

	if err := SetClipboardText(s); err != nil {
		w.mu.Lock()
		delete(w.echo, s)
		w.mu.Unlock()
		return err
	}
	return nil
}

func (w *ClipboardWatcher) Run(ctx context.Context) {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		seq := clipboardSeq()
		w.mu.Lock()
		changed := seq != w.lastSeq
		w.lastSeq = seq
		w.mu.Unlock()
		if !changed {
			continue
		}
		text, err := GetClipboardText()
		if err != nil || strings.TrimSpace(text) == "" || len(text) > maxClipBytes {
			// 不是文字，就看看是不是图片（截图、复制的图像）
			w.tryImage()
			continue
		}
		w.mu.Lock()
		if text == w.lastText {
			w.mu.Unlock()
			continue
		}
		if _, ok := w.echo[text]; ok {
			delete(w.echo, text)
			w.lastText = text
			w.mu.Unlock()
			continue
		}
		w.lastText = text
		w.mu.Unlock()
		w.onClip(text)
	}
}

// tryImage 读取剪贴板里的位图并回调，同一张图只推一次。
func (w *ClipboardWatcher) tryImage() {
	w.mu.Lock()
	fn := w.onImage
	w.mu.Unlock()
	if fn == nil {
		return
	}
	pngData, err := GetClipboardPNG()
	if err != nil || len(pngData) == 0 {
		return
	}
	sum := sha1.Sum(pngData)
	key := hex.EncodeToString(sum[:])

	w.mu.Lock()
	dup := key == w.lastImage
	w.lastImage = key
	w.mu.Unlock()
	if dup {
		return
	}
	fn(pngData)
}
