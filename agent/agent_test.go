package main

import (
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

func tmpDirs(t *testing.T) {
	d := t.TempDir()
	cfgDirFn = func() string { return d + "/cfg" }
	dataDirFn = func() string { return d + "/data" }
}

/* ---------------- 假平台 ---------------- */

type fakePlat struct {
	mu     sync.Mutex
	win    WinInfo
	idle   float64
	locked bool
	media  MediaInfo
	stats  M
	fulls  int
}

func (f *fakePlat) Window() WinInfo      { f.mu.Lock(); defer f.mu.Unlock(); return f.win }
func (f *fakePlat) IdleSeconds() float64 { return f.idle }
func (f *fakePlat) Locked() bool         { return f.locked }
func (f *fakePlat) Media() MediaInfo     { return f.media }
func (f *fakePlat) Kind() string         { return "linux" }
func (f *fakePlat) Close()               {}
func (f *fakePlat) Stats(full bool) M {
	if full {
		f.fulls++
	}
	out := M{"cpu": M{"usagePercent": 1.0}}
	if full {
		out["windows"] = M{"os": M{}}
	}
	return out
}

/* ---------------- 假 ActivityWatch ---------------- */

type fakeAW struct {
	srv     *httptest.Server
	queries []url.Values
	urlTS   time.Time
	winTS   time.Time
	empty   bool
}

func newFakeAW(t *testing.T) *fakeAW {
	f := &fakeAW{urlTS: time.Now(), winTS: time.Now()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/0/info":
			_, _ = io.WriteString(w, `{"hostname": "studio", "version": "v0.13.2"}`)
		case r.URL.Path == "/api/0/buckets/":
			_, _ = io.WriteString(w, `{"aw-watcher-window_studio": {}, "aw-watcher-window_studio-old": {}, "aw-watcher-afk_studio": {},
				"aw-watcher-web-chrome": {}, "aw-watcher-input_studio": {}}`)
		case strings.HasPrefix(r.URL.Path, "/api/0/buckets/"):
			q := r.URL.Query()
			f.queries = append(f.queries, q)
			bid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/0/buckets/"), "/events")
			if q.Get("limit") == "1" {
				ts := f.urlTS
				data := `{"url": "https://www.bilibili.com/video/1"}`
				if strings.Contains(bid, "window") {
					ts, data = f.winTS, `{"app": "firefox", "title": "Wayland 窗口"}`
				}
				_, _ = io.WriteString(w, `[{"timestamp": "`+ts.UTC().Format(time.RFC3339Nano)+`", "duration": 5.5, "data": `+data+`}]`)
				return
			}
			if f.empty {
				_, _ = io.WriteString(w, `[]`)
				return
			}
			_, _ = io.WriteString(w, `[{"id": 1, "timestamp": "2026-11-21T01:00:00.000000+00:00", "duration": 12.25, "data": {"app": "Code.exe", "title": "x"}}]`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

/* ---------------- 假服务器 ---------------- */

type fakeServer struct {
	mu      sync.Mutex
	srv     *httptest.Server
	reports []M
	aws     []M
	auth    []string
	revoked bool
	scopes  []string
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{scopes: []string{"activity", "music", "stats", "aw"}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		dev := M{"id": "abcd1234", "name": "书房", "kind": "linux", "host": "studio", "scopes": f.scopes}
		hints := M{"name": "红糖似了吗？", "report_sec": 30, "writer": true}
		if r.URL.Path == "/api/v1/pair" {
			var b M
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["code"] != "ABCD-EFGH" {
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"ok": false, "error": "配对码不对或已过期"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(M{"ok": true, "token": "hld_secret", "device": dev, "server": hints})
			return
		}
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer hld_secret" || f.revoked {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"ok": false, "error": "这台设备已经被停用，请重新配对"}`)
			return
		}
		switch r.URL.Path {
		case "/api/v1/device":
			_ = json.NewEncoder(w).Encode(M{"ok": true, "device": dev, "server": hints})
		case "/api/v1/report":
			var b M
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.reports = append(f.reports, b)
			res := M{}
			if a, ok := b["activity"].(M); ok {
				if a["idle"] == true {
					res["activity"] = M{"ok": true, "idle": true}
				} else {
					res["activity"] = M{"ok": true, "label": "在用 " + a["app"].(string)}
				}
			}
			if _, ok := b["stats"]; ok {
				res["stats"] = M{"ok": true}
			}
			_ = json.NewEncoder(w).Encode(M{"ok": true, "results": res, "server": hints})
		case "/api/v1/aw":
			zr, err := gzip.NewReader(r.Body)
			if err != nil || r.Header.Get("Content-Encoding") != "gzip" {
				w.WriteHeader(400)
				return
			}
			var b M
			_ = json.NewDecoder(zr).Decode(&b)
			f.aws = append(f.aws, b)
			_ = json.NewEncoder(w).Encode(M{"ok": true, "day": b["date"], "report_exists": false})
		case "/api/v1/unpair":
			_, _ = io.WriteString(w, `{"ok": true}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) last() M {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reports) == 0 {
		return nil
	}
	return f.reports[len(f.reports)-1]
}

func pairedAgent(t *testing.T) (*Agent, *fakePlat, *fakeServer, *fakeAW) {
	tmpDirs(t)
	srv, aw := newFakeServer(t), newFakeAW(t)
	p := &fakePlat{win: WinInfo{App: "code", Title: "main.go"}, media: MediaInfo{Status: "none"}}
	a := newAgent(p)
	a.cfg.AWURL = aw.srv.URL
	if err := a.Pair(srv.srv.URL, "ABCD-EFGH", "书房"); err != nil {
		t.Fatal(err)
	}
	return a, p, srv, aw
}

/* ---------------- 测试 ---------------- */

func TestParsePair(t *testing.T) {
	s, c, err := parsePairInput("hlpair://pair?s=https%3A%2F%2Fexample.com&c=ABCD-EFGH", "", "")
	eq(t, "链接", s+"|"+c+"|"+errS(err), "https://example.com|ABCD-EFGH|")
	s, c, _ = parsePairInput("", " http://127.0.0.1:8091/ ", "abcd efgh")
	eq(t, "分开填", s+"|"+c, "http://127.0.0.1:8091|abcd efgh")
	s, c, _ = parsePairInput("https://x.example ABCD-EFGH", "", "")
	eq(t, "地址 + 空格 + 码", s+"|"+c, "https://x.example|ABCD-EFGH")
	for _, bad := range [][3]string{{"hlpair://pair", "", ""}, {"", "ftp://x", "ABCDEFGH"}, {"", "https://x", "ABC"}, {"随便写的", "", ""}} {
		if _, _, err := parsePairInput(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("应该报错: %v", bad)
		}
	}
}

func errS(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestMusicPayload(t *testing.T) {
	eq(t, "读不到", musicPayload(MediaInfo{Err: true})["reason"], "unknown")
	eq(t, "没会话", musicPayload(MediaInfo{Status: "none"})["reason"], "none")
	eq(t, "暂停", musicPayload(MediaInfo{Status: "Paused", Title: "x"})["reason"], "paused")
	eq(t, "网易云", musicPayload(MediaInfo{Status: "Playing", App: "cloudmusic.exe", Title: "歌"})["playing"], true)
	eq(t, "浏览器只有歌手没专辑", musicPayload(MediaInfo{Status: "Playing", App: "chrome", Title: "歌", Artist: "人"})["reason"], "video")
	eq(t, "浏览器有歌手有专辑", musicPayload(MediaInfo{Status: "Playing", App: "chrome", Title: "歌", Artist: "人", Album: "专辑"})["playing"], true)
	eq(t, "X 视频（歌手是账号名、没有专辑）", musicPayload(MediaInfo{Status: "Playing", App: "msedge", Title: "某个视频", Artist: "@someone"})["reason"], "video")
	eq(t, "标题里带站点名", musicPayload(MediaInfo{Status: "Playing", App: "chrome", Title: "标题 - x.com", Artist: "人", Album: "专辑"})["reason"], "video")
	eq(t, "音乐软件标成视频也照收", musicPayload(MediaInfo{Status: "Playing", App: "spotify", Title: "播客", Artist: "人", PlaybackType: 2})["playing"], true)
	eq(t, "网页视频", musicPayload(MediaInfo{Status: "Playing", App: "Chrome", Title: "视频"})["reason"], "video")
	eq(t, "明确是视频", musicPayload(MediaInfo{Status: "Playing", App: "PotPlayerMini64.exe", Title: "片子", PlaybackType: 2})["reason"], "video")
	eq(t, "没标题", musicPayload(MediaInfo{Status: "Playing", App: "spotify"})["reason"], "unknown")
	eq(t, "MPRIS 名字", mprisApp("org.mpris.MediaPlayer2.chromium.instance1234"), "chromium")
	eq(t, "浏览器", isBrowser("msedge.exe") && isBrowser("firefox") && !isBrowser("code.exe") && !isBrowser("ArcSoft.exe"), true)
}

func TestFindBucket(t *testing.T) {
	ids := []string{"aw-watcher-window_pc-old", "aw-watcher-window_pc", "aw-watcher-afk_pc", "aw-watcher-web-chrome", "aw-watcher-web-firefox_x"}
	eq(t, "最短的", findBucket(ids, "aw-watcher-window_*", "aw-watcher-window"), "aw-watcher-window_pc")
	eq(t, "web", findBucket(ids, "aw-watcher-web*"), "aw-watcher-web-chrome")
	eq(t, "没有", findBucket(ids, "aw-watcher-input*"), "")
}

func TestAWExport(t *testing.T) {
	tmpDirs(t)
	aw := newFakeAW(t)
	day := time.Date(2026, 11, 21, 15, 0, 0, 0, time.Local)
	raw, counts, err := newAW(aw.srv.URL).ExportDay(day)
	if err != nil {
		t.Fatal(err)
	}
	var ex map[string]json.RawMessage
	_ = json.Unmarshal(raw, &ex)
	for _, k := range []string{"schema", "hostname", "date", "exported_at", "aw_version", "counts", "bucket_ids", "buckets", "screens"} {
		if _, ok := ex[k]; !ok {
			t.Errorf("导出缺 %s", k)
		}
	}
	eq(t, "日期", string(ex["date"]), `"2026-11-21"`)
	eq(t, "窗口条数", counts["window"], 1)
	var ids map[string]string
	_ = json.Unmarshal(ex["bucket_ids"], &ids)
	eq(t, "bucket 选最短的", ids["window"], "aw-watcher-window_studio")
	eq(t, "web", ids["web"], "aw-watcher-web-chrome")
	q := aw.queries[0]
	start, _ := time.Parse(time.RFC3339, q.Get("start"))
	end, _ := time.Parse(time.RFC3339, q.Get("end"))
	eq(t, "从本地零点开始", start.Equal(time.Date(2026, 11, 21, 0, 0, 0, 0, time.Local)), true)
	eq(t, "到次日零点", end.Sub(start), 24*time.Hour)
	eq(t, "全部事件", q.Get("limit"), "-1")
	if !strings.Contains(string(ex["buckets"]), `"duration": 12.25`) && !strings.Contains(string(ex["buckets"]), `"duration":12.25`) {
		t.Errorf("事件原样带过去: %s", ex["buckets"])
	}
	a := newAW(aw.srv.URL)
	eq(t, "最近的网址", a.LatestURL(), "https://www.bilibili.com/video/1")
	aw.urlTS = time.Now().Add(-10 * time.Minute)
	eq(t, "太旧的网址不要", a.LatestURL(), "")
	app, title, ok := a.LatestWindow()
	eq(t, "Wayland 兜底", app+"|"+title+"|"+map[bool]string{true: "ok"}[ok], "firefox|Wayland 窗口|ok")
	if _, _, err := newAW("http://127.0.0.1:1").ExportDay(day); err == nil {
		t.Error("连不上要报错")
	}
}

func TestPairAndReport(t *testing.T) {
	a, p, srv, aw := pairedAgent(t)
	cfg, st := a.snapshot()
	eq(t, "已配对", cfg.paired() && st.Paired, true)
	if strings.Contains(cfg.TokenEnc, "hld_secret") && !strings.HasPrefix(cfg.TokenEnc, "plain:") {
		t.Error("口令要加密（Linux 上是 plain: 前缀 + 600 文件）")
	}
	eq(t, "重新读配置口令还在", loadConfig().token(), "hld_secret")

	a.reportOnce()
	eq(t, "正常上报", srv.last()["activity"].(M)["app"], "code")
	eq(t, "状态里有网站显示的", a.status.Activity, "在用 code")

	p.win = WinInfo{Desktop: true}
	a.reportOnce()
	eq(t, "桌面算空闲", srv.last()["activity"].(M)["idle"], true)

	n := len(srv.reports)
	p.win = WinInfo{App: "ShellExperienceHost.exe"}
	p.media = MediaInfo{Status: "Playing", App: "cloudmusic.exe", Title: "歌", Artist: "人"}
	a.reportOnce()
	eq(t, "系统窗口不报 activity 但照样报音乐", len(srv.reports) == n+1 && srv.last()["activity"] == nil && srv.last()["music"].(M)["playing"] == true, true)

	p.win = WinInfo{App: "code"}
	p.idle = 20 * 60
	a.reportOnce()
	eq(t, "在放歌就不算离开", srv.last()["activity"].(M)["app"], "code")
	p.media = MediaInfo{Status: "none"}
	a.reportOnce()
	eq(t, "20 分钟没动又没放东西：离开", srv.last()["activity"].(M)["idle"], true)
	p.idle = 0
	p.locked = true
	a.reportOnce()
	eq(t, "锁屏：离开", srv.last()["activity"].(M)["idle"], true)
	p.locked = false

	p.win = WinInfo{App: "chrome.exe", Title: "视频"}
	a.reportOnce()
	eq(t, "浏览器带上 AW 的网址", srv.last()["activity"].(M)["url"], "https://www.bilibili.com/video/1")

	p.win = WinInfo{Unsupported: true}
	a.reportOnce()
	eq(t, "Wayland 用 AW 兜底", srv.last()["activity"].(M)["app"], "firefox")
	aw.winTS = time.Now().Add(-time.Hour)
	n = len(srv.reports)
	a.reportOnce()
	if r := srv.last(); len(srv.reports) == n+1 && r["activity"] != nil {
		t.Error("兜底也拿不到：不报 activity")
	}

	a.setPause(time.Now().Add(time.Hour))
	n = len(srv.reports)
	a.reportOnce()
	eq(t, "暂停时不报", len(srv.reports), n)
	a.setPause(time.Time{})

	_ = a.updateConfig(func(c *Config) { c.Report.Activity = false })
	p.win = WinInfo{App: "code"}
	a.reportOnce()
	eq(t, "本机关掉 activity", srv.last()["activity"] == nil && srv.last()["music"] != nil, true)
	_ = a.updateConfig(func(c *Config) { c.Report.Activity = true })

	srv.mu.Lock()
	srv.scopes = []string{"stats"}
	srv.mu.Unlock()
	a.refreshDevice()
	n = len(srv.reports)
	a.reportOnce()
	eq(t, "服务器没给权限就不报", len(srv.reports), n)

	srv.mu.Lock()
	srv.revoked = true
	srv.mu.Unlock()
	a.statsOnce()
	_, st = a.snapshot()
	eq(t, "401 → 需要重新配对", st.NeedPair, true)
}

func TestStatsCadence(t *testing.T) {
	a, p, srv, _ := pairedAgent(t)
	now := time.Now()
	a.now = func() time.Time { return now }
	a.statsOnce()
	a.statsOnce()
	eq(t, "第一次完整，之后不带硬件信息", p.fulls, 1)
	_, has := srv.last()["stats"].(M)["windows"]
	eq(t, "第二次没带 windows 分区", has, false)
	now = now.Add(11 * time.Minute)
	a.statsOnce()
	eq(t, "10 分钟后再带一次", p.fulls, 2)
}

func TestAWUploads(t *testing.T) {
	a, _, srv, aw := pairedAgent(t)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.Local)
	a.now = func() time.Time { return now }
	a.awOnce()
	eq(t, "补传前三天", len(srv.aws), 3)
	eq(t, "从最早的开始", srv.aws[0]["date"], "2026-11-19")
	a.awOnce()
	eq(t, "传过的不再传", len(srv.aws), 3)
	eq(t, "记下来了", loadState().AWDone["2026-11-21"] != "", true)
	now = time.Date(2026, 11, 22, 23, 52, 0, 0, time.Local)
	a.awOnce()
	eq(t, "23:50 以后先传一份今天的", len(srv.aws), 4)
	eq(t, "今天的", srv.aws[3]["date"], "2026-11-22")
	a.awOnce()
	eq(t, "今天的只传一次", len(srv.aws), 4)
	now = time.Date(2026, 11, 23, 0, 3, 0, 0, time.Local)
	a.awOnce()
	eq(t, "过了零点传完整的昨天", len(srv.aws), 5)
	aw.empty = true
	now = time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)
	a.awOnce()
	eq(t, "没数据的一天不传、也不再试", len(srv.aws), 5)
	eq(t, "空的也记下", loadState().AWDone["2026-11-23"], "empty")
}

func TestUIAuth(t *testing.T) {
	tmpDirs(t)
	a := newAgent(&fakePlat{})
	u, err := startUI(a)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(u.port)
	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string, hdr map[string]string) (int, *http.Response) {
		req, _ := http.NewRequest("GET", base+path, nil)
		for k, v := range hdr {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		r, err := noRedir.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode, r
	}
	code, _ := get("/api/status", nil)
	eq(t, "没钥匙 403", code, 403)
	code, _ = get("/?k=wrong", nil)
	eq(t, "错钥匙 403", code, 403)
	code, r := get("/?k="+u.key, nil)
	eq(t, "钥匙换 cookie", code, 302)
	ck := r.Cookies()
	if len(ck) == 0 || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", ck)
	}
	cookie := "hlagent=" + ck[0].Value
	code, _ = get("/api/status", map[string]string{"Cookie": cookie})
	eq(t, "有 cookie 能看", code, 200)
	code, _ = get("/api/status", map[string]string{"Cookie": cookie, "Host": "evil.example:" + strconv.Itoa(u.port)})
	eq(t, "Host 不对（DNS rebinding）", code, 403)
	post := func(hdr map[string]string) int {
		req, _ := http.NewRequest("POST", base+"/api/pause", strings.NewReader(`{"minutes": 30}`))
		req.Header.Set("Cookie", cookie)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	eq(t, "POST 没自定义头", post(nil), 403)
	eq(t, "POST 别的网站", post(map[string]string{"X-HL-Agent": "1", "Origin": "https://evil.example"}), 403)
	eq(t, "POST 正常", post(map[string]string{"X-HL-Agent": "1", "Origin": base}), 200)
	eq(t, "真的暂停了", a.paused(), true)
}

func TestIcon(t *testing.T) {
	ico := pngToICO(drawIcon("ok", 32), 32)
	eq(t, "ICO 头", binary.LittleEndian.Uint16(ico[2:]), uint16(1))
	eq(t, "PNG 在 22 字节后", string(ico[23:26]), "PNG")
	for _, s := range []string{"ok", "pause", "err", "off"} {
		if len(drawIcon(s, 64)) < 100 {
			t.Errorf("图标 %s 太小", s)
		}
	}
}
