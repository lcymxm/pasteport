package app

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"pasteport/web"
)

// Version 会显示在网页页脚和 /api/info 里。
const Version = "0.1.0"

// App 把所有部件串起来：配置、广播中心、文件库、剪贴板。
type App struct {
	cfg   *Config
	hub   *Hub
	files *FileStore
	clip  *ClipboardWatcher
	url   string

	pairMu    sync.Mutex
	pairFails int
	pairLock  time.Time
}

func NewApp(cfg *Config, port int, ip string) *App {
	a := &App{
		cfg:   cfg,
		hub:   NewHub(),
		files: NewFileStore(cfg.SaveDir),
		url:   fmtURL(ip, port),
	}
	a.clip = NewClipboardWatcher(func(text string) {
		// 本机复制了新内容 → 推给所有已连接设备
		a.hub.Publish(Event{Type: "clip", Text: text, From: "pc", FromName: cfg.Name})
	})
	return a
}

// SetClipboard 供外部（如文件拖放）复用同一个剪贴板实例。
func (a *App) Clipboard() *ClipboardWatcher { return a.clip }

func (a *App) Hub() *Hub { return a.hub }

// Handler 组装所有路由。
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	static := http.FileServer(http.FS(web.FS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		static.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/info", a.handleInfo)
	mux.HandleFunc("/api/local", a.handleLocalToken)
	mux.HandleFunc("/api/pair", a.handlePair)
	mux.HandleFunc("/api/state", a.mustAuth(a.handleState))
	mux.HandleFunc("/api/events", a.mustAuth(a.handleEvents))
	mux.HandleFunc("/api/clip", a.mustAuth(a.handleClip))
	mux.HandleFunc("/api/upload", a.mustAuth(a.handleUpload))
	mux.HandleFunc("/api/download", a.mustAuth(a.handleDownload))
	mux.HandleFunc("/api/qr", a.mustAuth(a.handleQR))

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ---------- 认证 ----------

func (a *App) tokenFrom(r *http.Request) string {
	if t := r.Header.Get("X-PP-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("t")
}

func (a *App) authed(r *http.Request) bool {
	t := a.tokenFrom(r)
	if len(t) != len(a.cfg.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(t), []byte(a.cfg.Token)) == 1
}

func (a *App) mustAuth(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authed(r) {
			writeErr(w, http.StatusUnauthorized, "未配对")
			return
		}
		h(w, r)
	}
}

func isLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ---------- 免认证接口 ----------

func (a *App) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     a.cfg.Name,
		"version":  Version,
		"pair":     true,
		"platform": "windows",
	})
}

// handleLocalToken 只允许本机回环地址调用，让桌面浏览器无需配对就能打开控制台。
func (a *App) handleLocalToken(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		writeErr(w, http.StatusForbidden, "仅限本机访问")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": a.cfg.Token,
		"code":  a.cfg.Code,
		"name":  a.cfg.Name,
		"url":   a.url,
	})
}

type pairReq struct {
	Code string `json:"code"`
}

func (a *App) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "请用 POST")
		return
	}
	a.pairMu.Lock()
	locked := time.Now().Before(a.pairLock)
	a.pairMu.Unlock()
	if locked {
		writeErr(w, http.StatusTooManyRequests, "错误次数过多，请稍后再试")
		return
	}

	var req pairReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	code := strings.TrimSpace(req.Code)
	if subtle.ConstantTimeCompare([]byte(code), []byte(a.cfg.Code)) != 1 {
		a.pairMu.Lock()
		a.pairFails++
		if a.pairFails >= 5 {
			a.pairLock = time.Now().Add(60 * time.Second)
			a.pairFails = 0
		}
		a.pairMu.Unlock()
		writeErr(w, http.StatusForbidden, "配对码不正确")
		return
	}
	a.pairMu.Lock()
	a.pairFails = 0
	a.pairMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"token": a.cfg.Token,
		"name":  a.cfg.Name,
		"url":   a.url,
	})
}

// ---------- 状态 ----------

type stateResp struct {
	Name     string     `json:"name"`
	Version  string     `json:"version"`
	URL      string     `json:"url"`
	Code     string     `json:"code"`
	Devices  int        `json:"devices"`
	History  []Event    `json:"history"`
	Files    []FileInfo `json:"files"`
	SaveDir  string     `json:"save_dir"`
	Platform string     `json:"platform"`
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, stateResp{
		Name:     a.cfg.Name,
		Version:  Version,
		URL:      a.url,
		Code:     a.cfg.Code,
		Devices:  a.hub.Clients(),
		History:  a.hub.History(),
		Files:    a.files.List(),
		SaveDir:  a.cfg.SaveDir,
		Platform: "windows",
	})
}

// ---------- SSE ----------

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "不支持流式响应")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	ch := a.hub.Subscribe()
	defer a.hub.Unsubscribe(ch)

	hello, _ := json.Marshal(Event{Type: "hello", Text: a.cfg.Name, At: time.Now()})
	fmt.Fprintf(w, "data: %s\n\n", hello)
	fl.Flush()

	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			fl.Flush()
		case <-tick.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// ---------- 剪贴板 ----------

type clipReq struct {
	Text string `json:"text"`
	From string `json:"from"`
	Name string `json:"name"`
}

func (a *App) handleClip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "请用 POST")
		return
	}
	var req clipReq
	if err := json.NewDecoder(io.LimitReader(r.Body, maxClipBytes+4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	text := req.Text
	if strings.TrimSpace(text) == "" {
		writeErr(w, http.StatusBadRequest, "内容为空")
		return
	}
	if len(text) > maxClipBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "内容过大")
		return
	}
	var wrote bool
	if err := a.clip.SetText(text); err == nil {
		wrote = true
	}
	ev := a.hub.Publish(Event{Type: "clip", Text: text, From: req.From, FromName: req.Name})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": ev.ID, "copied": wrote})
}

// ---------- 文件 ----------

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "请用 POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "不是合法的上传请求")
		return
	}
	var from, fromName string
	var saved []FileInfo
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "读取上传内容失败: "+err.Error())
			return
		}
		switch part.FormName() {
		case "from":
			b, _ := io.ReadAll(io.LimitReader(part, 256))
			from = string(b)
		case "name":
			b, _ := io.ReadAll(io.LimitReader(part, 256))
			fromName = string(b)
		case "file":
			info, err := a.files.Add(part, part.FileName())
			if err != nil {
				part.Close()
				writeErr(w, http.StatusBadRequest, "保存文件失败: "+err.Error())
				return
			}
			saved = append(saved, info)
		}
		part.Close()
	}
	if len(saved) == 0 {
		writeErr(w, http.StatusBadRequest, "没有收到文件")
		return
	}
	for _, info := range saved {
		f := info
		a.hub.Publish(Event{Type: "file", File: &f, From: from, FromName: fromName})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": saved})
}

func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	if ascii == "" {
		ascii = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", ascii, url.PathEscape(name))
}

func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	info, path, ok := a.files.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "文件不存在或已过期")
		return
	}
	w.Header().Set("Content-Disposition", contentDisposition(info.Name))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

// ---------- 二维码 ----------

func (a *App) handleQR(w http.ResponseWriter, r *http.Request) {
	target := a.url + "#c=" + a.cfg.Code
	png := QRPNG(target)
	if png == nil {
		writeErr(w, http.StatusInternalServerError, "二维码生成失败")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// LogRequests 是一个很轻量的访问日志，只在调试时开启。
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/events") {
			log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
