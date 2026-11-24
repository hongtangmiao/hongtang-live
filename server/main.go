package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // 容器里没有时区数据也能用 Asia/Shanghai
)

var (
	dataDir string
	webDir  string
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	dataDir = env("DATA_DIR", "data")
	webDir = env("WEB_DIR", "web")

	c, created, err := loadConfig(filepath.Join(dataDir, "config.json"))
	if err != nil {
		log.Fatalf("[config] %v", err)
	}
	cfgv = c
	if l, err := time.LoadLocation(c.Timezone); err == nil {
		loc, time.Local = l, l
	}
	if created {
		log.Printf("[config] 第一次启动：已从环境变量生成 %s", cfgPath)
	}
	if len(os.Args) > 1 && os.Args[1] == "recompute-reports" {
		os.Exit(runRecompute(os.Args[2:]))
	}

	initAdmin()
	loadStatusTexts()
	startScheduler()
	startDevicesSaver()
	initOwner()
	initPC()
	startServerSampler()
	initNesync()
	go sessionJanitor()

	addr := net.JoinHostPort(env("HOST", "0.0.0.0"), env("PORT", "8787"))
	hs := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(route),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	go func() {
		log.Printf("hongtang-live      http://%s", addr)
		log.Printf("netEase upstream   %s", c.NetEase.API)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		if err := hs.Serve(badURLListener{ln}); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	go func() {
		defer func() { recover() }()
		musicHistory() // 预热，daylog 还没起来就算了
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
	pcPersist()
	devicesPersist()
	nsSaveNow()
	log.Printf("bye")
}

var (
	feedMu   sync.Mutex
	feedAt   time.Time
	feedData M
)

// route：HTTP 路由分发；任何一个请求出错都只影响它自己
func route(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if e := recover(); e != nil {
			log.Printf("[req] %s %.200s panic: %v\n%s", r.Method, r.URL.Path, e, debug.Stack())
			writeJSON(w, 500, M{"ok": false, "error": "internal error"})
		}
	}()
	p := r.URL.Path
	denied := func() bool {
		if crossOrigin(r) {
			writeJSON(w, 403, M{"ok": false, "error": "origin not allowed"})
			return true
		}
		return false
	}
	switch {
	case p == "/app/hongtang-live.apk":
		serveAPK(w, r)
	case p == "/healthz":
		writeJSON(w, 200, M{"ok": true})
	case p == "/api/health":
		c := conf()
		var dbPath any
		if c.DaylogDB != "" {
			dbPath = c.DaylogDB
		}
		out := M{"ok": true, "server": "hongtang-live", "upstream": c.NetEase.API,
			"daylogDb": dbPath, "daylogReady": daylogDB() != nil, "bound": ownerBound(), "ne": nsStatus()}
		for k, v := range videosStatus() {
			out[k] = v
		}
		writeJSON(w, 200, out)

	case p == "/admin" || p == "/admin/":
		serveAdminPage(w, r, "admin.html")
	case strings.HasPrefix(p, "/admin/js/"):
		serveAdminPage(w, r, strings.TrimPrefix(p, "/admin/"))

	case strings.HasPrefix(p, "/api/admin/"):
		handleAdminAPI(w, r)

	case p == "/api/site":
		if denied() {
			return
		}
		writeJSON(w, 200, M{"ok": true, "site": siteInfo()})

	case strings.HasPrefix(p, "/api/v1/"):
		handleV1(w, r, strings.TrimPrefix(p, "/api/v1/"))
	case p == "/api/now":
		if denied() {
			return
		}
		handleNow(w, r)
	case p == "/api/current":
		if denied() {
			return
		}
		handleCurrent(w, r)
	case p == "/api/day":
		if denied() {
			return
		}
		handleDay(w, r)
	case p == "/api/daylog/feed":
		if denied() {
			return
		}
		feedMu.Lock()
		if feedData != nil && time.Since(feedAt) < 3*time.Second {
			d := feedData
			feedMu.Unlock()
			writeJSON(w, 200, d)
			return
		}
		feedMu.Unlock()
		d := getDaylogFeed(daylogDB())
		if d == nil {
			writeJSON(w, 200, M{"ok": false, "error": "daylog db unavailable"})
			return
		}
		feedMu.Lock()
		feedData, feedAt = d, time.Now()
		feedMu.Unlock()
		writeJSON(w, 200, d)

	case p == "/api/music/history":
		if denied() {
			return
		}
		d := musicHistory()
		if d == nil {
			writeJSON(w, 200, M{"ok": false, "error": "daylog db unavailable"})
			return
		}
		writeJSON(w, 200, d)

	case p == "/api/stats/devices":
		if denied() {
			return
		}
		handleStatsDevices(w)

	case strings.HasPrefix(p, "/api/stats/"):
		if denied() {
			return
		}
		handleStatsSection(w, r, strings.TrimPrefix(p, "/api/stats/"))

	case p == "/api/music/now":
		if denied() {
			return
		}
		now := currentMusic()
		writeJSON(w, 200, M{"ok": now != nil, "now": now})

	case p == "/api/videos/wall":
		if denied() {
			return
		}
		writeJSON(w, 200, getWall(r.URL.Query().Get("refresh") == "1"))

	case strings.HasPrefix(p, "/api/videos/jf-img/"):
		if denied() {
			return
		}
		proxyJfImage(w, strings.TrimPrefix(p, "/api/videos/jf-img/"))

	case strings.HasPrefix(p, "/api/ne"):
		if denied() {
			return
		}
		if r.Method == "OPTIONS" {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}
			send(w, 204, nil, map[string]string{
				"Access-Control-Allow-Origin": origin, "Access-Control-Allow-Methods": "GET,POST,OPTIONS",
				"Access-Control-Allow-Headers": "Content-Type", "Access-Control-Allow-Credentials": "true",
			})
			return
		}
		handleNE(w, r)

	default:
		if r.Method != "GET" && r.Method != "HEAD" {
			sendText(w, 405, "method not allowed")
			return
		}
		serveStatic(w, r)
	}
}
