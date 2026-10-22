package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"pasteport/internal/app"
	"pasteport/internal/icon"
	"pasteport/internal/tray"
)

// buildArch 由构建脚本通过 -ldflags "-X main.buildArch=..." 写入。
var buildArch = runtime.GOARCH

func main() {
	var (
		port             = flag.Int("port", 0, "监听端口（默认读取配置，8787）")
		save             = flag.String("save", "", "接收文件的保存目录")
		name             = flag.String("name", "", "本机在手机上显示的名字")
		dataDir          = flag.String("data", "", "配置目录（默认自动选择可写位置）")
		noOpen           = flag.Bool("no-open", false, "不要自动打开浏览器")
		noTray           = flag.Bool("no-tray", false, "不显示托盘图标")
		trayMode         = flag.Bool("tray", false, "静默启动：隐藏控制台，只留在托盘里")
		showQR           = flag.Bool("show-qr", false, "启动后用看图器打开二维码图片")
		installStartup   = flag.Bool("install-startup", false, "加入开机自启后退出")
		uninstallStartup = flag.Bool("uninstall-startup", false, "取消开机自启后退出")
		showVer          = flag.Bool("version", false, "只打印版本号")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("PastePort %s windows/%s\n", app.Version, buildArch)
		return
	}

	app.EnableUTF8Console()

	if *installStartup {
		if err := app.SetStartup(true); err != nil {
			fatal(err)
		}
		fmt.Println("  已加入开机自启：", app.StartupCommand())
		return
	}
	if *uninstallStartup {
		if err := app.SetStartup(false); err != nil {
			fatal(err)
		}
		fmt.Println("  已取消开机自启")
		return
	}

	if *dataDir != "" {
		app.SetDataDir(*dataDir)
	}
	dir, err := app.ResolveDataDir()
	if err != nil {
		fatal(err)
	}

	// 单实例：第二次双击不应该再占一个端口，而是把已有窗口叫出来
	if already, err := app.AcquireSingle(app.InstanceTag(dir)); err == nil && already {
		if !*noOpen {
			if rt, ok := app.ReadRuntime(dir); ok && reachable(rt.Port) {
				shellOpen(fmt.Sprintf("http://127.0.0.1:%d/", rt.Port))
			}
		}
		fmt.Println("  PastePort 已经在运行了，不用再开一个。")
		return
	}
	defer app.ReleaseSingle()

	cfg, err := app.LoadConfig(dir)
	if err != nil {
		fatal(err)
	}
	if *port > 0 {
		cfg.Port = *port
	}
	if *save != "" {
		cfg.SaveDir = *save
	}
	if *name != "" {
		cfg.Name = *name
	}
	if err := cfg.SaveError(); err != nil {
		fmt.Printf("  ⚠ 配置无法写入 %s：%v（本次运行照常，重启后可能重置）\n", cfg.Dir(), err)
	}

	ln, actualPort, err := listen(cfg.Port)
	if err != nil {
		fatal(err)
	}

	ips := app.LanIPs()
	primary := "127.0.0.1"
	if len(ips) > 0 {
		primary = ips[0]
	}
	a := app.NewApp(cfg, actualPort, ips)
	a.SetStartupHooks(app.StartupEnabled, app.SetStartup)

	consoleURL := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)
	lanURL := fmt.Sprintf("http://%s:%d/", primary, actualPort)
	_ = app.WriteRuntime(dir, app.RuntimeInfo{
		PID:     os.Getpid(),
		Port:    actualPort,
		URL:     lanURL,
		Version: app.Version,
	})
	defer app.ClearRuntime(dir)

	// 二维码另存一份 PNG：终端字体/配色不可靠时，这个才是能扫的那个
	qrPath, qrErr := app.WriteQRFile(dir, lanURL+"#c="+cfg.Code)

	quit := make(chan struct{})
	var once bool

	if !*trayMode {
		banner(cfg, actualPort, ips, qrErr)
	}
	if *showQR && qrErr == nil {
		shellOpen(qrPath)
	}
	if !*noOpen {
		shellOpen(consoleURL)
	}

	// 托盘：把窗口关掉也能继续跑
	var tr *tray.Tray
	if !*noTray {
		tr, err = tray.Register(icon.PNG, "PastePort · "+cfg.Name, dir, tray.Actions{
			OpenConsole: func() {
				shellOpen(consoleURL)
			},
			ShowQR: func() {
				if qrErr == nil {
					shellOpen(qrPath)
				}
			},
			CopyPairCode: func() {
				if err := a.Clipboard().SetText(app.SpacedCode(cfg.Code)); err == nil {
					tr.Notify("配对码已复制", app.SpacedCode(cfg.Code))
				}
			},
			StartupEnabled: a.StartupEnabled,
			ToggleStartup:  a.ToggleStartup,
			Quit: func() {
				if !once {
					once = true
					close(quit)
				}
			},
		})
		if err != nil {
			fmt.Printf("  ⚠ 托盘图标没能起来（不影响使用）：%v\n", err)
		} else {
			a.SetTray(true)
			tr.Notify("PastePort 已就绪", "电脑复制的内容会自动推到手机")
		}
	}
	defer tr.Dispose()

	if *trayMode {
		app.HideConsoleWindow()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go a.Clipboard().Run(ctx)
	if _, err := app.StartDiscovery(actualPort, cfg.Name, nil); err != nil {
		fmt.Printf("  ⚠ 局域网自动发现未开启（UDP %d 被占用）：%v\n", app.DiscoverPort, err)
	}

	srv := &http.Server{
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fatal(err)
		}
	}()

	select {
	case <-ctx.Done():
	case <-quit:
	}

	fmt.Println("\n  正在退出…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}

func listen(want int) (net.Listener, int, error) {
	for i := 0; i < 20; i++ {
		p := want + i
		if p > 65535 {
			break
		}
		ln, err := net.Listen("tcp4", fmt.Sprintf(":%d", p))
		if err == nil {
			return ln, p, nil
		}
	}
	return nil, 0, fmt.Errorf("端口 %d 附近都被占用了，试试 --port 9000", want)
}

func banner(cfg *app.Config, port int, ips []string, qrErr error) {
	primary := "127.0.0.1"
	if len(ips) > 0 {
		primary = ips[0]
	}
	url := fmt.Sprintf("http://%s:%d/", primary, port)

	fmt.Printf("\n  PastePort 剪贴桥  v%s\n", app.Version)
	fmt.Println("  ─────────────────────────────────────────────")
	fmt.Printf("  电脑     %s\n", cfg.Name)
	fmt.Printf("  地址     %s\n", url)
	if len(ips) > 1 {
		fmt.Println("  其它地址（上面那个打不开就换一个）：")
		for _, ip := range ips[1:] {
			fmt.Printf("           http://%s:%d/\n", ip, port)
		}
	}
	fmt.Printf("  配对码   %s\n", app.SpacedCode(cfg.Code))
	fmt.Printf("  保存到   %s\n", cfg.SaveDir)
	fmt.Println()
	fmt.Println("  手机扫码即可使用，无需安装 App：")
	fmt.Println()
	for _, line := range splitLines(app.QRASCII(url + "#c=" + cfg.Code)) {
		fmt.Println("  " + line)
	}
	fmt.Println()
	if qrErr == nil {
		fmt.Println("  扫不出来？在托盘图标上右键 →「显示二维码」，会打开一张大图。")
	} else {
		fmt.Println("  ⚠ 二维码图片没能生成，请在浏览器里扫码：" + url)
	}
	fmt.Println("  关掉这个窗口不影响使用，程序会留在托盘里。")
	fmt.Println()
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func shellOpen(target string) {
	if runtime.GOOS != "windows" {
		return
	}
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

// reachable 用来验证 runtime.json 里的端口是不是真的有人听，
// 避免上次被强杀留下的陈旧记录把浏览器带到一个死地址。
func reachable(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "\n  ✗ %v\n\n", err)
	os.Exit(1)
}
