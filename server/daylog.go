package main

import (
	"database/sql"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	_ "modernc.org/sqlite"
)

/* ---------------- daylog 的 SQLite（只读） ---------------- */

var (
	dbMu       sync.Mutex
	dbHandle   *sql.DB
	dbFailedAt time.Time
)

// daylogDB 打开失败 60 秒后再试（daylog 比这里晚起来时不至于永久失效）
func daylogDB() *sql.DB {
	dbMu.Lock()
	defer dbMu.Unlock()
	if dbHandle != nil {
		return dbHandle
	}
	path := conf().DaylogDB
	if path == "" {
		return nil
	}
	if !dbFailedAt.IsZero() && time.Since(dbFailedAt) < 60*time.Second {
		return nil
	}
	writer := conf().DaylogWriter
	dsn := "file:" + path + "?mode=ro&_pragma=busy_timeout(3000)&_pragma=query_only(1)"
	if writer {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				log.Printf("[daylog] mkdir %s failed: %v", dir, err)
				dbFailedAt = time.Now()
				return nil
			}
		}
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_txlock=immediate"
	}
	db, err := sql.Open("sqlite", dsn)
	if err == nil {
		err = db.Ping()
	}
	if err == nil && writer {
		err = dlMigrate(db)
	}
	if err != nil {
		log.Printf("[daylog] open db failed: %v", err)
		if db != nil {
			db.Close()
		}
		dbFailedAt = time.Now()
		return nil
	}
	db.SetMaxOpenConns(2)
	dbFailedAt = time.Time{}
	dbHandle = db
	return db
}

func resetDaylogDB() {
	dbMu.Lock()
	if dbHandle != nil {
		dbHandle.Close()
		dbHandle = nil
	}
	dbFailedAt = time.Time{}
	dbMu.Unlock()
	histMu.Lock()
	histData = nil
	histMu.Unlock()
	feedMu.Lock()
	feedData = nil
	feedMu.Unlock()
}

/* ---------------- 文案格式（与 daylog/app 一致） ---------------- */

// 像素主题：与 daylog 同一套 8-bit 调色板
var catColors = map[string]string{
	"写代码": "#ffd23f", "创作": "#c0b6ff", "沟通": "#ffa94d", "娱乐": "#ff9ec4",
	"读资料": "#8ce99a", "浏览": "#8fd3ff", "其他": "#7c86b0", "私密": "#4a5890",
}
var fallbackColors = []string{"#ff9ec4", "#c0b6ff", "#ffa94d", "#8fd3ff", "#8ce99a"}
var meowTails = []string{"喵~", "喵", "喵～", "喵~。"}

func meow(text string) string {
	t := strings.TrimRight(text, " \t\r\n\u3000\u00a0")
	if t == "" {
		return ""
	}
	for _, tail := range meowTails {
		if strings.HasSuffix(t, tail) {
			return t
		}
	}
	return t + "喵~"
}

func catColor(name string) string {
	if c, ok := catColors[name]; ok {
		return c
	}
	s := name
	if s == "" {
		s = "?"
	}
	sum := 0
	for _, u := range utf16.Encode([]rune(s)) { // 和 JS charCodeAt 一样按 UTF-16 算
		sum += int(u)
	}
	return fallbackColors[sum%len(fallbackColors)]
}

func fmtMin(sec float64) string {
	s := int64(maxF(0, jsRound(sec)))
	if s < 60 {
		return itoa(s) + "s"
	}
	m := s / 60
	if m < 60 {
		return itoa(m) + "m"
	}
	h, rm := m/60, m%60
	return itoa(h) + "h" + pad2(rm) + "m"
}

func fmtAgo(sec float64) string {
	s := int64(math.Floor(maxF(0, sec)))
	days := s / 86400
	rem := s % 86400
	hours := rem / 3600
	minutes := (rem % 3600) / 60
	out := ""
	if days > 0 {
		out += itoa(days) + "天"
	}
	if hours > 0 {
		out += itoa(hours) + "小时"
	}
	if minutes > 0 {
		out += itoa(minutes) + "分钟"
	}
	if out == "" {
		return "刚刚"
	}
	return out
}

var (
	reClock    = regexp.MustCompile(`T(\d{2}:\d{2})`)
	reMonthDay = regexp.MustCompile(`^\d{4}-(\d{2}-\d{2})`)
	reTimeSec  = regexp.MustCompile(`T(\d{2}:\d{2}:\d{2})`)
)

func fmtClock(iso string) string {
	if iso == "" {
		return ""
	}
	if m := reClock.FindStringSubmatch(iso); m != nil {
		return m[1]
	}
	if t, ok := parseTS(iso); ok {
		return t.In(loc).Format("15:04")
	}
	return ""
}

func fmtMonthDay(iso string) string {
	if iso == "" {
		return ""
	}
	if m := reMonthDay.FindStringSubmatch(iso); m != nil {
		return m[1]
	}
	if t, ok := parseTS(iso); ok {
		return t.In(loc).Format("01-02")
	}
	return ""
}

func fmtTimeSec(iso string) string {
	if iso == "" {
		return ""
	}
	if m := reTimeSec.FindStringSubmatch(iso); m != nil {
		return m[1]
	}
	if t, ok := parseTS(iso); ok {
		return t.In(loc).Format("15:04:05")
	}
	return ""
}

func faceFor(stale, hasLabel bool) string {
	if !hasLabel {
		return "ฅ( ・ω・)"
	}
	if stale {
		return "ฅ(´-ω- )"
	}
	return "ฅ^•ﻌ•^ฅ"
}

/* ---------------- /api/daylog/feed ---------------- */

type liveRow struct {
	ID                                                int64
	Day, TS, EndTS, App, Label, LabelPlain, Cat, Host string
	Dur                                               float64
}

const liveCols = "id, day, ts, end_ts, app, label, label_plain, cat, host, dur"

func scanLive(sc interface{ Scan(...any) error }) (liveRow, error) {
	var r liveRow
	var dur sql.NullFloat64
	var day, ts, end, app, label, plain, cat, host sql.NullString
	err := sc.Scan(&r.ID, &day, &ts, &end, &app, &label, &plain, &cat, &host, &dur)
	r.Day, r.TS, r.EndTS, r.App = day.String, ts.String, end.String, app.String
	r.Label, r.LabelPlain, r.Cat, r.Host, r.Dur = label.String, plain.String, cat.String, host.String, dur.Float64
	return r, err
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func buildHealth(db *sql.DB, now float64) any {
	cutoff := isoUTC(time.UnixMilli(int64(now - 30*86400*1000)))
	rows, err := db.Query("SELECT metric, MAX(ts) AS ts, value, unit FROM health_metrics WHERE ts >= ? GROUP BY metric", cutoff)
	if err != nil {
		return nil
	}
	defer rows.Close()
	fieldMap := map[string]string{
		"sleep_duration": "sleep_min", "sleep_score": "sleep_score", "sleep_avg_hrv": "hrv",
		"resting_heart_rate": "rhr", "heart_rate": "hr_live", "heart_rate_live": "hr_live",
		"stress": "stress", "spo2": "spo2", "blood_oxygen": "spo2", "steps": "steps",
	}
	out := M{"available": true, "source": "huawei"}
	dates := M{}
	newest := ""
	n := 0
	for rows.Next() {
		var metric, ts, unit sql.NullString
		var value sql.NullFloat64
		if rows.Scan(&metric, &ts, &value, &unit) != nil {
			continue
		}
		n++
		if ts.String > newest {
			newest = ts.String
		}
		key := fieldMap[metric.String]
		if key == "" {
			key = metric.String
		}
		if key == "rhr" || key == "hrv" { // 私密字段一律不下发
			continue
		}
		val := jsRound(value.Float64)
		tsMs, valid := msOf(ts.String)
		if key == "hr_live" {
			age := math.Inf(1)
			if valid {
				age = math.Floor((now - tsMs) / 1000)
			}
			if age > 120 {
				if valid {
					ago := fmtAgo(age)
					if ago == "刚刚" {
						out["hr_last_text"] = "最后 刚刚"
					} else {
						out["hr_last_text"] = "最后 " + ago + "前"
					}
				}
				continue
			}
			out["hr_live"] = val
			out["hr_live_age"] = maxF(0, age)
			dates["hr_live"] = fmtTimeSec(ts.String)
			continue
		}
		switch key {
		case "sleep_min", "sleep_score", "steps", "spo2", "stress":
			out[key] = val
			dates[key] = fmtMonthDay(ts.String)
		}
	}
	if n == 0 {
		return nil
	}
	has := false
	for _, k := range []string{"sleep_min", "sleep_score", "steps", "hr_live", "spo2", "stress"} {
		if _, ok := out[k]; ok {
			has = true
		}
	}
	if !has {
		return nil
	}
	out["dates"] = dates
	out["updated"] = newest
	return out
}

func buildMedia(db *sql.DB, now float64) []M {
	items := []M{}
	if rows, err := db.Query("SELECT key, title, url, last_seen, last_focus FROM media WHERE kind = 'live'"); err == nil {
		for rows.Next() {
			var key, title, u, seen, focus sql.NullString
			if rows.Scan(&key, &title, &u, &seen, &focus) != nil {
				continue
			}
			seenMs, ok := msOf(seen.String)
			if !ok || (now-seenMs)/1000 > 900 {
				continue
			}
			if focusMs, ok := msOf(or(focus.String, seen.String)); ok && (now-focusMs)/1000 > 120 {
				continue
			}
			items = append(items, M{"kind": "live", "key": key.String, "title": or(or(title.String, u.String), "直播间"),
				"sub": "正在看直播", "url": u.String, "cover": ""})
			if len(items) >= 2 {
				break
			}
		}
		rows.Close()
	}
	if len(items) < 2 {
		var music, ts sql.NullString
		if db.QueryRow("SELECT music, music_ts FROM devices WHERE music != '' ORDER BY music_ts DESC LIMIT 1").Scan(&music, &ts) == nil && music.String != "" {
			if tsMs, ok := msOf(ts.String); ok && (now-tsMs)/1000 <= 240 {
				var m M
				if decodeJSON([]byte(music.String), &m) == nil && truthy(m["title"]) {
					items = append(items, M{"kind": "music", "key": "music", "title": toStr(m["title"]),
						"sub": toStr(m["artist"]), "url": toStr(m["url"]), "cover": httpsURL(toStr(m["cover"]))})
				}
			}
		}
	}
	if len(items) > 2 {
		items = items[:2]
	}
	return items
}

func buildReport(db *sql.DB, yesterday string) any {
	var day, stats, analysis, created sql.NullString
	err := db.QueryRow("SELECT day, stats, analysis, created_at FROM reports WHERE day = ? LIMIT 1", yesterday).Scan(&day, &stats, &analysis, &created)
	if err != nil {
		if db.QueryRow("SELECT day, stats, analysis, created_at FROM reports ORDER BY day DESC LIMIT 1").Scan(&day, &stats, &analysis, &created) != nil {
			return nil
		}
	}
	var a, s M
	if decodeJSON([]byte(or(analysis.String, "{}")), &a) != nil || a == nil {
		return nil
	}
	_ = decodeJSON([]byte(or(stats.String, "{}")), &s)
	if !truthy(a["headline"]) && !truthy(a["narrative"]) {
		return nil
	}
	top := ""
	if apps := asA(s["apps"]); len(apps) > 0 {
		top = toStr(asM(apps[0])["name"])
	}
	str := func(v any) string {
		if !truthy(v) {
			return ""
		}
		return toStr(v)
	}
	return M{"day": day.String, "headline": str(a["headline"]), "narrative": str(a["narrative"]),
		"total_hms": str(s["total_hms"]), "top_app": top, "created_at": created.String}
}

func getDaylogFeed(db *sql.DB) M {
	if db == nil {
		return nil
	}
	now := time.Now()
	nowMsv := float64(now.UnixMilli())
	today := dayOf(now)
	yesterday := dayOf(now.Add(-24 * time.Hour))
	staleSec := float64(dInt(dlCfg(), 180, "live", "stale_sec"))

	cur := pickCurrent(staleSec)
	var devSeen, devApp sql.NullString
	hasDev := false
	if cur != nil && cur.Host != "" {
		if err := db.QueryRow("SELECT last_seen, last_app FROM devices WHERE host = ?", cur.Host).Scan(&devSeen, &devApp); err == nil {
			hasDev = true
		} else if err != sql.ErrNoRows {
			return nil
		}
	} else {
		if err := db.QueryRow("SELECT last_seen, last_app FROM devices ORDER BY last_seen DESC LIMIT 1").Scan(&devSeen, &devApp); err == nil {
			hasDev = true
		} else if err != sql.ErrNoRows {
			return nil
		}
	}

	var view M
	owner := dOwner()
	if cur == nil {
		view = M{"now": "还没有收到 " + owner + " 的活动", "stale": true, "dur_text": "", "cat": "", "label": "", "face": faceFor(true, false)}
	} else {
		endMs, endOK := msOf(or(cur.EndTS, cur.TS))
		startMs, startOK := msOf(cur.TS)
		devSeenMs, devOK := msOf(devSeen.String)
		age := math.Inf(1)
		if endOK {
			age = maxF(0, (nowMsv-endMs)/1000)
		}
		run := 0.0
		if endOK && startOK {
			run = maxF(0, math.Floor((endMs-startMs)/1000))
		}
		// 设备报了空闲（last_app 为空且比最后一段活动新）也算离开电脑
		markedIdle := hasDev && devApp.String == "" && devOK && endOK && devSeenMs > endMs
		stale := age > staleSec || markedIdle
		lastPlain := meow(or(cur.LabelPlain, cur.Label))
		liveFull := meow(cur.Label)
		if stale {
			ago := fmtAgo(age)
			d := "最后活动 " + ago + "前"
			if ago == "刚刚" {
				d = "最后活动 刚刚"
			}
			view = M{"now": meow(owner + " 不在电脑前"), "stale": true, "dur_text": d, "cat": cur.Cat, "label": lastPlain, "face": faceFor(true, lastPlain != "")}
		} else {
			view = M{"now": liveFull, "stale": false, "dur_text": "已经 " + fmtMin(run), "cat": cur.Cat, "label": liveFull, "face": faceFor(false, liveFull != "")}
		}
	}
	viewStale, _ := view["stale"].(bool)

	var rows []liveRow
	if rs, err := db.Query("SELECT "+liveCols+" FROM live WHERE day = ? ORDER BY id ASC", today); err == nil {
		for rs.Next() {
			if r, err := scanLive(rs); err == nil {
				rows = append(rows, r)
			}
		}
		rs.Close()
	}

	var lastID int64 = -1
	if len(rows) > 0 {
		lastID = rows[len(rows)-1].ID
		if cur != nil {
			for _, r := range rows {
				if r.ID == cur.ID {
					lastID = cur.ID
					break
				}
			}
		}
	}
	type appAgg struct {
		app, lastTS, label, plain, cat string
		total                          float64
		id                             int64
	}
	var apps []*appAgg
	appIdx := map[string]*appAgg{}
	spans := make([]liveSpan, 0, len(rows))
	for _, r := range rows {
		sMs, sOK := msOf(r.TS)
		eMs, eOK := msOf(or(r.EndTS, r.TS))
		span := 0.0
		if sOK && eOK {
			span = maxF(0, math.Floor((eMs-sMs)/1000))
		}
		sec := maxF(r.Dur, span)
		if r.ID == lastID && !viewStale && sOK {
			liveSpanSec := maxF(0, math.Floor((nowMsv-sMs)/1000))
			sec = maxF(sec, minF(liveSpanSec, 300))
		}
		cat := or(r.Cat, "其他")
		spans = append(spans, liveSpan{ts: r.TS, endTS: r.EndTS, host: r.Host, cat: cat, dur: int64(sec)})
		a := appIdx[r.App]
		if a == nil {
			a = &appAgg{app: r.App, lastTS: r.TS, label: r.Label, plain: or(r.LabelPlain, r.Label), cat: cat, id: r.ID}
			appIdx[r.App] = a
			apps = append(apps, a)
		}
		a.total += sec
		if r.TS >= a.lastTS {
			a.lastTS, a.label, a.plain, a.cat, a.id = r.TS, r.Label, or(r.LabelPlain, r.Label), cat, r.ID
		}
	}
	totDedup, catDedup := dedupLiveTotals(today, spans)
	total := float64(totDedup)
	detailed := !viewStale
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].total != apps[j].total {
			return apps[i].total > apps[j].total
		}
		return apps[i].lastTS > apps[j].lastTS
	})
	outApps := []M{}
	for i, a := range apps {
		if i >= 12 {
			break
		}
		isCur := a.id == lastID
		raw := a.plain // 只有在线且是当前前台应用才带窗口标题
		if detailed && isCur {
			raw = a.label
		}
		outApps = append(outApps, M{"label": meow(raw), "cat": a.cat, "color": catColor(a.cat),
			"total_sec": a.total, "total": fmtMin(a.total), "clock": fmtClock(a.lastTS), "cur": isCur && detailed})
	}
	cats := []M{}
	for name, s := range catDedup {
		sec := float64(s)
		pct := 0.0
		if total > 0 {
			pct = maxF(2, jsRound(sec/total*100))
		}
		cats = append(cats, M{"name": name, "sec": sec, "dur": fmtMin(sec), "pct": pct, "color": catColor(name)})
	}
	sort.SliceStable(cats, func(i, j int) bool { return cats[i]["sec"].(float64) > cats[j]["sec"].(float64) })
	if len(cats) > 6 {
		cats = cats[:6]
	}
	return M{
		"ok": true, "today": today, "updated": isoUTC(now), "view": view,
		"summary": M{"total_sec": total, "total": fmtMin(total), "app_count": len(apps), "cats": cats},
		"apps":    outApps,
		"media":   buildMedia(db, nowMsv),
		"health":  buildHealth(db, nowMsv),
		"report":  buildReport(db, yesterday),
	}
}
