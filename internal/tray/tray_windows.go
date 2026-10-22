//go:build windows

// Package tray 提供 Windows 系统托盘图标和右键菜单。
// 全部用 syscall 直接调 Win32，不引第三方依赖。
//
// 注意：Win32 的消息是投递给「创建窗口的那个线程」的，
// 所以创建窗口和跑消息循环必须在同一个被 LockOSThread 锁定的线程上，
// 否则 windowProc 永远不会被调用（图标能显示，但点不动）。
package tray

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procPostMessage      = user32.NewProc("PostMessageW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenu       = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procCheckMenuItem    = user32.NewProc("CheckMenuItem")
	procLoadImage        = user32.NewProc("LoadImageW")
	procLoadIcon         = user32.NewProc("LoadIconW")
	procGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmNull          = 0x0000
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B
	wmApp           = 0x8000
	wmTrayCallback  = wmApp + 1
	wmTrayDispose   = wmApp + 2

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfChecked   = 0x00000008
	mfUnchecked = 0x00000000
	mfByCommand = 0x00000000

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	imageIcon      = 1
	lrLoadFromFile = 0x0010
	idiApplication = 32512

	menuOpen    = 1001
	menuCopy    = 1002
	menuStartup = 1003
	menuQuit    = 1004
)

// Actions 是托盘菜单触发时回调进主程序的动作。
type Actions struct {
	OpenConsole  func()
	CopyPairCode func()
	// StartupEnabled 返回当前是否已设置开机自启（用来打勾）
	StartupEnabled func() bool
	// ToggleStartup 切换开机自启，返回切换后的状态
	ToggleStartup func() (bool, error)
	Quit          func()
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          [16]byte
	HBalloonIcon      uintptr
}

type msgStruct struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type point struct{ X, Y int32 }

// Tray 一个进程只应该有一个。
type Tray struct {
	actions Actions
	tip     string
	hIcon   uintptr
	hwnd    uintptr
	hmenu   uintptr
	data    notifyIconData
}

var (
	className   = syscall.StringToUTF16Ptr("PastePortTrayWnd")
	windowTitle = syscall.StringToUTF16Ptr("PastePort")
	instance    *Tray
	wndProc     = syscall.NewCallback(windowProc)
)

// Register 创建隐藏窗口和托盘图标。iconPNG 用来生成托盘图标，
// tmpDir 必须是可写目录（图标会以 .ico 形式临时落盘）。
func Register(iconPNG []byte, tip, tmpDir string, a Actions) (*Tray, error) {
	t := &Tray{actions: a, tip: tip}
	instance = t

	t.hIcon = loadIcon(iconPNG, tmpDir)
	if t.hIcon == 0 {
		if v, _, _ := procLoadIcon.Call(0, idiApplication); v != 0 {
			t.hIcon = v
		}
	}

	ready := make(chan error, 1)
	go func() {
		// 整个生命周期都钉在这个线程上：窗口和消息循环必须同线程
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := t.init(); err != nil {
			ready <- err
			return
		}
		ready <- nil
		t.messageLoop()
	}()

	if err := <-ready; err != nil {
		instance = nil
		return nil, err
	}
	return t, nil
}

// init 必须在锁定后的线程里调用。
func (t *Tray) init() error {
	hInst, _, _ := procGetModuleHandle.Call(0)
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   wndProc,
		HInstance:     hInst,
		LpszClassName: className,
	}
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx 失败: %v", err)
	}

	hwnd, _, err := procCreateWindowEx.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowTitle)),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx 失败: %v", err)
	}
	t.hwnd = hwnd

	hmenu, _, _ := procCreatePopupMenu.Call()
	t.hmenu = hmenu
	appendMenu(hmenu, mfString, menuOpen, "打开控制台")
	appendMenu(hmenu, mfString, menuCopy, "复制配对码")
	appendMenu(hmenu, mfString, menuStartup, "开机自启")
	appendMenu(hmenu, mfSeparator, 0, "")
	appendMenu(hmenu, mfString, menuQuit, "退出")

	t.data = notifyIconData{
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayCallback,
		HIcon:            t.hIcon,
	}
	t.data.CbSize = uint32(unsafe.Sizeof(t.data))
	copyUTF16(t.data.SzTip[:], t.tip)

	if ok := t.shellNotify(nimAdd); !ok {
		procDestroyWindow.Call(hwnd)
		t.hwnd = 0
		return errors.New("Shell_NotifyIcon 添加托盘图标失败")
	}
	return nil
}

func (t *Tray) messageLoop() {
	var m msgStruct
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// Notify 弹一个气泡提示。
func (t *Tray) Notify(title, body string) {
	if t == nil || t.hwnd == 0 {
		return
	}
	d := t.data
	d.UFlags = nifInfo
	d.DwInfoFlags = 1 // NIIF_INFO
	copyUTF16(d.SzInfoTitle[:], title)
	copyUTF16(d.SzInfo[:], body)
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

// Dispose 移除托盘图标并销毁窗口。
func (t *Tray) Dispose() {
	if t == nil || t.hwnd == 0 {
		return
	}
	// 图标从这里移除没问题，但 DestroyWindow 必须由创建窗口的那个线程来做
	t.shellNotify(nimDelete)
	procPostMessage.Call(t.hwnd, wmTrayDispose, 0, 0)
	t.hwnd = 0
}

func (t *Tray) shellNotify(action uintptr) bool {
	d := t.data
	d.CbSize = uint32(unsafe.Sizeof(d))
	r, _, _ := procShellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&d)))
	return r != 0
}

func windowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmTrayCallback:
		if instance == nil {
			return 0
		}
		switch uint32(lparam) & 0xffff {
		case wmRButtonUp, wmContextMenu:
			instance.showMenu()
		case wmLButtonDblClk:
			if instance.actions.OpenConsole != nil {
				go instance.actions.OpenConsole()
			}
		}
		return 0
	case wmCommand:
		return 0
	case wmClose:
		// 关窗口等于退出整个程序，不然会留下一个没有图标、还在跑服务的僵尸进程
		if instance != nil && instance.actions.Quit != nil {
			instance.actions.Quit()
		} else {
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmTrayDispose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func (t *Tray) showMenu() {
	if t.hmenu == 0 {
		return
	}
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWin.Call(t.hwnd)

	check := uintptr(mfUnchecked)
	if t.actions.StartupEnabled != nil && t.actions.StartupEnabled() {
		check = mfChecked
	}
	procCheckMenuItem.Call(t.hmenu, menuStartup, mfByCommand|check)

	cmd, _, _ := procTrackPopupMenu.Call(
		t.hmenu, tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	// 让菜单能正常消失的老规矩
	procPostMessage.Call(t.hwnd, wmNull, 0, 0)

	switch cmd {
	case menuOpen:
		if t.actions.OpenConsole != nil {
			go t.actions.OpenConsole()
		}
	case menuCopy:
		if t.actions.CopyPairCode != nil {
			go t.actions.CopyPairCode()
		}
	case menuStartup:
		if t.actions.ToggleStartup != nil {
			on, err := t.actions.ToggleStartup()
			switch {
			case err != nil:
				t.Notify("开机自启设置失败", err.Error())
			case on:
				t.Notify("PastePort", "已加入开机自启")
			default:
				t.Notify("PastePort", "已取消开机自启")
			}
		}
	case menuQuit:
		if t.actions.Quit != nil {
			t.actions.Quit()
		}
	}
}

// ---------- 图标 ----------

// loadIcon 把 PNG 转成 .ico 落盘，再用 LoadImage 读出来。
// Vista 之后 ICO 里可以直接放 PNG，比手搓 DIB 稳当得多。
func loadIcon(iconPNG []byte, dir string) uintptr {
	if len(iconPNG) == 0 || dir == "" {
		return 0
	}
	src, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return 0
	}
	small := resizeBox(src, 32)
	var buf bytes.Buffer
	if err := png.Encode(&buf, small); err != nil {
		return 0
	}
	ico := buildICO(buf.Bytes(), 32)

	path := filepath.Join(dir, "tray.ico")
	if err := os.WriteFile(path, ico, 0o600); err != nil {
		return 0
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	h, _, _ := procLoadImage.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0, lrLoadFromFile)
	return h
}

func buildICO(pngData []byte, n int) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&b, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&b, binary.LittleEndian, uint16(1)) // count
	b.WriteByte(byte(n))                             // width
	b.WriteByte(byte(n))                             // height
	b.WriteByte(0)                                   // palette
	b.WriteByte(0)                                   // reserved
	binary.Write(&b, binary.LittleEndian, uint16(1)) // planes
	binary.Write(&b, binary.LittleEndian, uint16(32))
	binary.Write(&b, binary.LittleEndian, uint32(len(pngData)))
	binary.Write(&b, binary.LittleEndian, uint32(22))
	b.Write(pngData)
	return b.Bytes()
}

func resizeBox(src image.Image, n int) *image.NRGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		y0, y1 := y*sh/n, (y+1)*sh/n
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < n; x++ {
			x0, x1 := x*sw/n, (x+1)*sw/n
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bb, a, cnt uint32
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
					r += cr
					g += cg
					bb += cb
					a += ca
					cnt++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r / cnt >> 8), G: uint8(g / cnt >> 8),
				B: uint8(bb / cnt >> 8), A: uint8(a / cnt >> 8),
			})
		}
	}
	return dst
}

// ---------- 小工具 ----------

func appendMenu(hmenu uintptr, flags uintptr, id uintptr, text string) {
	var p *uint16
	if text != "" {
		p = syscall.StringToUTF16Ptr(text)
	}
	procAppendMenu.Call(hmenu, flags, id, uintptr(unsafe.Pointer(p)))
}

func copyUTF16(dst []uint16, s string) {
	src, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	copy(dst, src) // UTF16FromString 已经自带结尾的 0
}
