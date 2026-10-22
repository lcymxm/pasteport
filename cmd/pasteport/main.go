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
)

// buildArch 由构建脚本通过 -ldflags "-X main.buildArch=..." 写入。
var buildArch = runtime.GOARCH

func main() {
	var (
		port    = flag.Int("port", 0, "监听端口（默认读取配置，8787）")
		save    = flag.String("save", "", "接收文件的保存目录")
		name    = flag.String("name", "", "本机在手机上显示的名字")
		dataDir = flag.String("data", "", "配置目录（默认自动选择可写位置）")
		noOpen  = flag.Bool("no-open", false, "不要自动打开浏览器")
		showVer = flag.Bool("version", false, "只打印版本号")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("PastePort %s windows/%s\n", app.Version, buildArch)
		return
	}

	app.EnableUTF8Console()

	if *dataDir != "" {
		app.SetDataDir(*dataDir)
	}
	dir, err := app.ResolveDataDir()
	if err != nil {
		fatal(err)
	}
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

	ip := app.LocalIPv4()
	a := app.NewApp(cfg, actualPort, ip)

	banner(cfg, actualPort)
	if !*noOpen {
		openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", actualPort))
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

	<-ctx.Done()
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

func banner(cfg *app.Config, port int) {
	url := fmt.Sprintf("http://%s:%d/", app.LocalIPv4(), port)
	fmt.Printf("\n  PastePort 剪贴桥  v%s\n", app.Version)
	fmt.Println("  ─────────────────────────────────────────────")
	fmt.Printf("  电脑     %s\n", cfg.Name)
	fmt.Printf("  地址     %s\n", url)
	fmt.Printf("  配对码   %s\n", app.SpacedCode(cfg.Code))
	fmt.Printf("  保存到   %s\n", cfg.SaveDir)
	fmt.Println()
	fmt.Println("  手机扫码即可使用，无需安装 App：")
	fmt.Println()
	for _, line := range splitLines(app.QRASCII(url + "#c=" + cfg.Code)) {
		fmt.Println("  " + line)
	}
	fmt.Println()
	fmt.Println("  也可以手动输入上面的地址和配对码。按 Ctrl+C 退出。")
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

func openBrowser(url string) {
	if runtime.GOOS != "windows" {
		return
	}
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "\n  ✗ %v\n\n", err)
	os.Exit(1)
}
