package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

/*
 * 本机设置页：只听 127.0.0.1 的随机端口。
 *   - 托盘菜单打开时网址里带一次性的钥匙（?k=…），换成 HttpOnly + SameSite=Strict 的 cookie
 *   - Host 头必须是 127.0.0.1:端口（防 DNS rebinding），POST 还要带 X-HL-Agent: 1（别的网页发不出这种请求）
 */

//go:embed ui/*
var uiFS embed.FS

type UI struct {
	a    *Agent
	key  string
	port int
	ln   net.Listener
}

func randKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func startUI(a *Agent) (*UI, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	u := &UI{a: a, key: randKey(), port: ln.Addr().(*net.TCPAddr).Port, ln: ln}
	a.mu.Lock()
	a.st.UIPort, a.st.UIKey, a.st.Version = u.port, u.key, version
	a.mu.Unlock()
	saveState(a.st)
	srv := &http.Server{Handler: u, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return u, nil
}

func (u *UI) URL() string { return "http://127.0.0.1:" + strconv.Itoa(u.port) + "/?k=" + u.key }

func (u *UI) authed(r *http.Request) bool {
	if c, err := r.Cookie("hlagent"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(u.key)) == 1 {
		return true
	}
	if h := r.Header.Get("X-HL-Key"); h != "" && subtle.ConstantTimeCompare([]byte(h), []byte(u.key)) == 1 {
		return true // 第二个实例转交配对链接用
	}
	return false
}

func uiJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (u *UI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host != "127.0.0.1:"+strconv.Itoa(u.port) && host != "localhost:"+strconv.Itoa(u.port) {
		http.Error(w, "bad host", 403)
		return
	}
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/" && r.URL.Query().Get("k") != "" {
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("k")), []byte(u.key)) != 1 {
			http.Error(w, "钥匙不对：请从托盘图标的菜单里打开", 403)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "hlagent", Value: u.key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if !u.authed(r) {
		http.Error(w, "请从托盘图标的菜单里打开这个页面", 403)
		return
	}
	switch r.URL.Path {
	case "/", "/index.html":
		u.static(w, "ui/index.html", "text/html; charset=utf-8")
		return
	case "/app.js":
		u.static(w, "ui/app.js", "text/javascript; charset=utf-8")
		return
	case "/api/status":
		uiJSON(w, 200, u.statusJSON())
		return
	case "/api/log":
		uiJSON(w, 200, M{"ok": true, "log": recentLog(), "dir": logDir()})
		return
	}
	if r.Method != "POST" || r.Header.Get("X-HL-Agent") != "1" {
		http.Error(w, "forbidden", 403)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+host {
		http.Error(w, "bad origin", 403)
		return
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	str := func(k string) string { s, _ := body[k].(string); return strings.TrimSpace(s) }
	fail := func(err error) { uiJSON(w, 400, M{"ok": false, "error": err.Error()}) }
	switch r.URL.Path {
	case "/api/pair":
		server, code, err := parsePairInput(str("link"), str("server"), str("code"))
		if err != nil {
			fail(err)
			return
		}
		if err := u.a.Pair(server, code, str("name")); err != nil {
			fail(friendlyPairErr(err))
			return
		}
	case "/api/unpair":
		u.a.Unpair(true)
	case "/api/pause":
		mins, _ := body["minutes"].(float64)
		switch {
		case mins == 0:
			u.a.setPause(time.Time{})
		case mins < 0:
			n := time.Now()
			u.a.setPause(time.Date(n.Year(), n.Month(), n.Day()+1, 6, 0, 0, 0, time.Local))
		default:
			u.a.setPause(time.Now().Add(time.Duration(mins) * time.Minute))
		}
	case "/api/settings":
		var s struct {
			Report    ReportOpts `json:"report"`
			AWURL     string     `json:"aw_url"`
			Autostart bool       `json:"autostart"`
		}
		_ = json.Unmarshal(b, &s)
		if s.AWURL != "" && !strings.HasPrefix(s.AWURL, "http://") && !strings.HasPrefix(s.AWURL, "https://") {
			fail(agentErr("ActivityWatch 地址要以 http:// 开头"))
			return
		}
		if s.Report.IntervalSec != 0 && (s.Report.IntervalSec < 10 || s.Report.IntervalSec > 600) {
			fail(agentErr("上报间隔要在 10 到 600 秒之间（0 = 跟服务器）"))
			return
		}
		if s.Report.IdleMin < 0 || s.Report.IdleMin > 240 {
			fail(agentErr("空闲判定要在 0 到 240 分钟之间"))
			return
		}
		_ = u.a.updateConfig(func(c *Config) { c.Report, c.AWURL = s.Report, or(s.AWURL, "http://127.0.0.1:5600") })
		if s.Autostart != autostartEnabled() {
			if err := setAutostart(s.Autostart); err != nil {
				fail(err)
				return
			}
		}
	case "/api/aw-now":
		if err := u.a.UploadToday(); err != nil {
			fail(err)
			return
		}
	default:
		http.Error(w, "not found", 404)
		return
	}
	uiJSON(w, 200, M{"ok": true, "status": u.statusJSON()})
}

func friendlyPairErr(err error) error {
	var ae *APIError
	if e, ok := err.(*APIError); ok {
		ae = e
	}
	if ae == nil {
		return agentErr("连不上服务器：" + err.Error())
	}
	return agentErr(ae.Msg)
}

func (u *UI) static(w http.ResponseWriter, name, ct string) {
	b, err := uiFS.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func (u *UI) statusJSON() M {
	cfg, st := u.a.snapshot()
	return M{"ok": true, "status": st, "report": cfg.Report, "aw_url": cfg.AWURL, "autostart": autostartEnabled(),
		"config_path": configPath(), "log_dir": logDir()}
}
