package main

import (
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

/* ---------------- 上报 ---------------- */

// normTS：客户端给的时间统一成本地时区秒级 ISO；格式不对或比现在晚一天以上就用现在
func normTS(raw any) string {
	now := time.Now().In(loc)
	if !truthy(raw) {
		return isoSec(now)
	}
	t, ok := pyAware(pyStrip(toStr(raw)))
	if !ok {
		return isoSec(now)
	}
	t = t.In(loc)
	if t.Sub(now).Seconds() > 86400 {
		return isoSec(now)
	}
	return isoSec(t)
}

/* 音乐：歌名 + 歌手 → 封面和网易云链接（按歌缓存一小时） */

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

var (
	musicCacheMu sync.Mutex
	musicCache   = map[string]struct {
		at time.Time
		v  M
	}{}
	reBracket = regexp.MustCompile(`[（(\[【].*?[)）\]】]`)
)

// pyQuote：urllib.parse.quote（safe="/"）
func pyQuote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.IndexByte("_.-~/", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)>>4, 16)+strconv.FormatInt(int64(c)&15, 16)))
		}
	}
	return b.String()
}

// normSongName：只留字母和数字，用来比歌名（大小写、空格、标点、全半角的差异都抹掉）
func normSongName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// bigramDice：二元组 Dice 系数（按 rune 切，中文也能用）
func bigramDice(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 2 || len(rb) < 2 {
		if a == b {
			return 1
		}
		return 0
	}
	cnt := map[string]int{}
	for i := 0; i+1 < len(ra); i++ {
		cnt[string(ra[i:i+2])]++
	}
	hit := 0
	for i := 0; i+1 < len(rb); i++ {
		g := string(rb[i : i+2])
		if cnt[g] > 0 {
			cnt[g]--
			hit++
		}
	}
	return 2 * float64(hit) / float64(len(ra)-1+len(rb)-1)
}

// sameSongName：网易云搜出来的这首歌，和上报的标题到底是不是同一首。
// 搜索接口 limit=1 只返回「最像的」，标题是视频标题时也能搜到一首无关的歌
// （实测：X 视频标题「…咳嗽一声就能瞬间明白对方需求。」搜到了歌名「瞬间」的歌），
// 不校验的话就会给它套上别人的封面、专辑和 song_id。
func sameSongName(name, title string) bool {
	n, t := normSongName(name), normSongName(title)
	if n == "" || t == "" {
		return false
	}
	if n == t {
		return true
	}
	short, long := n, t
	if len([]rune(short)) > len([]rune(long)) {
		short, long = long, short
	}
	ls, ll := len([]rune(short)), len([]rune(long))
	// 名副其实的包含：长出来的部分只能是 (Live)、(feat. xxx) 这类尾巴。
	// 长标题里凑巧出现一个短歌名不算，否则「…瞬间…」就会匹配到歌名「瞬间」。
	if strings.Contains(long, short) {
		extra := ll - ls
		if extra <= 4 || float64(extra) <= 0.5*float64(ll) {
			return true
		}
		return false
	}
	return bigramDice(n, t) >= 0.5
}

// 浏览器上报的来源（Chrome / MSEdge / Firefox …）
var browserSourceRE = regexp.MustCompile(`(?i)chrome|msedge|edge|firefox|brave|opera|vivaldi|chromium|safari`)

func looksLikeBrowser(source string) bool { return browserSourceRE.MatchString(source) }

func httpsURL(u string) string {
	if strings.HasPrefix(u, "http://") {
		return "https://" + u[7:]
	}
	return u
}

func enrichMusic(m M) M {
	title, artist, album := pyStrip(toStr(m["title"])), pyStrip(toStr(m["artist"])), pyStrip(toStr(m["album"]))
	if title == "" {
		return M{}
	}
	key := title + "|" + artist
	musicCacheMu.Lock()
	if hit, ok := musicCache[key]; ok {
		ttl := time.Hour
		if !truthy(hit.v["cover"]) {
			ttl = 2 * time.Minute // 没拿到封面时短缓存，避免偶发失败一直无封面
		}
		if time.Since(hit.at) < ttl {
			musicCacheMu.Unlock()
			return hit.v
		}
	}
	musicCacheMu.Unlock()
	out := M{"title": title, "artist": artist, "album": album, "cover": "", "url": "", "song_id": 0}
	base := strings.TrimRight(or(dStr(dlCfg(), "music", "api_base"), conf().NetEase.API), "/")
	if base != "" {
		cleaned := pyStrip(reBracket.ReplaceAllString(title, ""))
		seen := map[string]bool{}
		hdr := map[string]string{"User-Agent": browserUA}
		for _, q := range []string{pyStrip(title + " " + artist), pyStrip(title), pyStrip(cleaned + " " + artist)} {
			if q == "" || seen[q] {
				continue
			}
			seen[q] = true
			found, err := fetchJSONOnce(base+"/search?keywords="+pyQuote(q)+"&type=1&limit=1", hdr, 8*time.Second)
			if err != nil {
				continue
			}
			songs := asA(asM(found["result"])["songs"])
			if len(songs) == 0 || songs[0] == nil {
				continue
			}
			song0 := asM(songs[0])
			sid := song0["id"]
			detail, err := fetchJSONOnce(base+"/song/detail?ids="+toStr(sid), hdr, 8*time.Second)
			if err != nil {
				continue
			}
			s0 := M{}
			if ss := asA(detail["songs"]); len(ss) > 0 && asM(ss[0]) != nil {
				s0 = asM(ss[0])
			}
			// 搜到的第一条未必是同一首，名字差太多就当没匹配上，继续换关键词试
			if !sameSongName(toStr(s0["name"]), title) {
				continue
			}
			al := asM(s0["al"])
			pic := or(toStr(al["picUrl"]), toStr(asM(song0["album"])["picUrl"]))
			out["cover"] = httpsURL(pic)
			out["album"] = or(toStr(al["name"]), album)
			out["song_id"] = sid
			out["url"] = "https://music.163.com/song?id=" + toStr(sid)
			break
		}
	}
	if !truthy(out["url"]) {
		out["url"] = "https://music.163.com/#/search/m/?s=" + pyQuote(pyStrip(title+" "+artist))
	}
	musicCacheMu.Lock()
	if len(musicCache) > 500 {
		musicCache = map[string]struct {
			at time.Time
			v  M
		}{}
	}
	musicCache[key] = struct {
		at time.Time
		v  M
	}{time.Now(), out}
	musicCacheMu.Unlock()
	return out
}

// applyMusic：返回要写进 devices.music 的值；nil 表示不动（SMTC 读不到时保留上一次）
func applyMusic(p M, day, ts string) *string {
	if !dTruthy(dlCfg(), "music", "enabled") {
		return nil
	}
	m, ok := p["music"].(map[string]any)
	if !ok {
		return nil
	}
	if truthy(m["playing"]) && truthy(m["title"]) {
		e := enrichMusic(m)
		// 浏览器上报、又匹配不到任何一首歌：多半是网页视频（X、B 站这类），不算在听音乐。
		// 正常放歌时网易云都能搜到，这条只挡「搜不到却硬说是音乐」的情况。
		if !truthy(e["song_id"]) && looksLikeBrowser(toStr(m["source"])) {
			empty := ""
			return &empty
		}
		logErr("add_music", addMusic(day, ts, e))
		s := string(jsonBytes(e))
		return &s
	}
	reason := ""
	if truthy(m["reason"]) {
		reason = toStr(m["reason"])
	}
	if reason == "unknown" || reason == "none" {
		return nil
	}
	if _, has := m["playing"]; has || reason == "paused" || reason == "video" {
		empty := ""
		return &empty
	}
	return nil
}

/* 上报的核心逻辑（供 /api/v1/report 调用）。返回 (结果, HTTP 状态, 错误) */

type ingestErr struct {
	code int
	msg  string
}

func (e *ingestErr) Error() string { return e.msg }

func needWriter() *ingestErr {
	if !conf().DaylogWriter {
		return &ingestErr{503, "这台服务器没有开 daylog 写入"}
	}
	return nil
}

func ingestLive(p M, host, name string) (M, *ingestErr) {
	if e := needWriter(); e != nil {
		return nil, e
	}
	if truthy(p["idle"]) {
		ts := isoSec(time.Now().In(loc))
		if truthy(p["ts"]) {
			ts = toStr(p["ts"])
		}
		stored := applyMusic(p, todayKey(), ts)
		setHostIdle(host, true)
		logErr("mark_idle", markIdle(host, name, ts))
		if stored != nil {
			logErr("touch_device", touchDevice(host, name, ts, "", "", stored))
		}
		return M{"ok": true, "idle": true}, nil
	}
	p["host"] = host
	setHostIdle(host, false)
	s := buildSample(dlCfg(), p)
	if err := addSample(s, dInt(dlCfg(), 120, "live", "segment_gap_sec")); err != nil {
		logErr("add_sample", err)
		return nil, &ingestErr{500, "写入失败"}
	}
	if s.URL != "" {
		if lk, _ := liveKey(s.URL); lk != "" {
			logErr("upsert_live", upsertLive(lk, s.Label, s.URL, s.TS))
		}
	}
	stored := applyMusic(p, s.Day, s.TS)
	logErr("touch_device", touchDevice(host, name, s.TS, s.App, s.Label, stored))
	return M{"ok": true, "label": s.Label, "cat": s.Cat}, nil
}

func ingestHealth(p M) (M, *ingestErr) {
	if e := needWriter(); e != nil {
		return nil, e
	}
	values, _ := p["values"].(map[string]any)
	if len(values) == 0 {
		return nil, &ingestErr{400, "values 为空"}
	}
	ts := normTS(p["ts"])
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 40 {
		keys = keys[:40]
	}
	stored := 0
	for _, k := range keys {
		n, ok := pyFloat(values[k])
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			continue
		}
		metric := k
		if utf8.RuneCountInString(metric) > 64 {
			metric = string([]rune(metric)[:64])
		}
		if addHealth(metric, ts, n, "") == nil {
			stored++
		}
	}
	return M{"ok": true, "stored": stored}, nil
}

func writeIngest(w http.ResponseWriter, res M, e *ingestErr) {
	if e != nil {
		writeJSON(w, e.code, M{"ok": false, "error": e.msg})
		return
	}
	writeJSON(w, 200, res)
}

/* ---------------- GET /api/now：新页面「いま / 此刻」一次拿全（原 daylog 首页服务端渲染的那些） ---------------- */

type devView struct {
	Name     string `json:"name"`
	Online   bool   `json:"online"`
	Kind     string `json:"kind,omitempty"`
	Battery  *int   `json:"battery,omitempty"`
	Charging bool   `json:"charging,omitempty"`
}

func devicesView(limit int) []devView {
	out := []devView{}
	db := daylogDB()
	if db == nil {
		return out
	}
	rows, err := db.Query("SELECT host, name, last_seen FROM devices ORDER BY last_seen DESC LIMIT ?", limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	stale := float64(dInt(dlCfg(), 180, "live", "stale_sec"))
	for rows.Next() {
		var host, name, seen string
		if rows.Scan(&host, &name, &seen) != nil {
			continue
		}
		t, ok := pyAware(seen)
		online := ok && time.Since(t).Seconds() < stale
		// 只报心率 / 电量的设备（固定心率设备）不会更新 daylog 的 last_seen：看配对设备最后一次上报
		if !online {
			if rt, rok := deviceSeenByHost(host); rok && time.Since(rt).Seconds() < stale*2 {
				online = true
			}
		}
		v := devView{Name: or(name, host), Online: online}
		if kind, bat := deviceByHost(host); kind != "" {
			v.Kind = kind
			if bat != nil {
				lv := bat.Level
				v.Battery, v.Charging = &lv, bat.Charging
			}
		}
		out = append(out, v)
	}
	return out
}

func pollSeconds() int {
	n := dInt(dlCfg(), 10, "live", "poll_sec")
	if n < 5 {
		n = 5
	}
	return n
}

func handleNow(w http.ResponseWriter, r *http.Request) {
	countViewer(w, r)
	admin := isAdmin(r)
	target := pickDay(r)
	today := todayKey()
	isToday := target == today
	view := currentView()
	stale, _ := view["stale"].(bool)
	totals := dayTotals(target)
	cats := []M{}
	for i, c := range totals.Cats {
		if i >= 6 {
			break
		}
		sec := c["sec"].(int64)
		cats = append(cats, M{"name": c["name"], "sec": sec, "dur": pyFmtMin(float64(sec)), "color": pyCatColor(toStr(c["name"]))})
	}
	prev, next := "", ""
	if admin {
		prev, next = neighbourDays(target, today)
	}
	out := M{
		"ok": true, "day": target, "today": today, "is_today": isToday, "admin": admin,
		"prev": prev, "next": next, "owner": dOwner(),
		"apps":  appsPayload(appTotals(target), isToday && !stale),
		"total": pyFmtMin(float64(totals.Total)), "total_sec": totals.Total, "app_count": totals.Apps,
		"last_clock": hhmm(totals.Last), "cats": cats, "devices": devicesView(4), "viewers": viewers(300),
		"poll": pollSeconds(), "media": []M{}, "health": nil,
	}
	if isToday {
		out["view"] = view
		out["face"] = pyFaceFor(view)
		out["stale"] = stale
		out["media"] = buildMediaPy()
		if healthPublic() {
			if h := healthViewPy(); len(h) > 0 {
				out["health"] = h
			}
		}
	} else {
		out["view"] = nil
		out["face"] = "ฅ(・ω・ )"
		out["stale"] = true
		if admin {
			if rep := getReport(target); rep != nil {
				out["has_report"] = true
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}
