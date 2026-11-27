package main

import (
	"database/sql"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

/* ---------------- daylog 的 SQLite ---------------- */

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

/* ---------------- 文案格式（全站共用） ---------------- */

// 像素主题：与 daylog 同一套 8-bit 调色板
var catColors = map[string]string{
	"写代码": "#ffd23f", "游戏": "#ff6b9d", "听歌": "#63e6be", "创作": "#c0b6ff",
	"沟通": "#ffa94d", "娱乐": "#ff9ec4", "工具": "#74c0fc", "生活": "#b2f2bb",
	"读资料": "#8ce99a", "浏览": "#8fd3ff", "其他": "#7c86b0", "私密": "#4a5890",
}
var fallbackColors = []string{"#ff9ec4", "#c0b6ff", "#ffa94d", "#8fd3ff", "#8ce99a"}
var meowTails = []string{"喵~", "喵", "喵～", "喵~。"}

// meow：给「记录」补一句喵~；已经有就不重复加（去掉结尾 Unicode 空白）
func meow(text string) string {
	t := strings.TrimRightFunc(text, isPySpace)
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
	s := or(name, "?")
	sum := 0
	for _, r := range s {
		sum += int(r)
	}
	return fallbackColors[sum%len(fallbackColors)]
}

// fmtMin：截断到分钟，不四舍五入
func fmtMin(sec float64) string {
	s := int64(math.Max(0, sec))
	if s < 60 {
		return itoa(s) + "s"
	}
	m := s / 60
	if m < 60 {
		return itoa(m) + "m"
	}
	return itoa(m/60) + "h" + pad2(m%60) + "m"
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

func faceFor(stale, hasLabel bool) string {
	if !hasLabel {
		return "ฅ( ・ω・)"
	}
	if stale {
		return "ฅ(´-ω- )"
	}
	return "ฅ^•ﻌ•^ฅ"
}

func faceForView(view M) string {
	stale, _ := view["stale"].(bool)
	return faceFor(stale, truthy(view["label"]))
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

/* ---------------- /api/daylog/feed ---------------- */

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
	today := dayOf(now)
	yesterday := dayOf(now.Add(-24 * time.Hour))

	view := currentView()
	view["face"] = faceForView(view)
	stale, _ := view["stale"].(bool)
	detailed := !stale

	apps := appTotals(today)
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Total != apps[j].Total {
			return apps[i].Total > apps[j].Total
		}
		return apps[i].LastTS > apps[j].LastTS
	})
	outApps := []M{}
	for i, a := range apps {
		if i >= 12 {
			break
		}
		raw := or(a.LabelPlain, a.Label)
		if detailed && a.Cur {
			raw = a.Label
		}
		outApps = append(outApps, M{"label": meow(raw), "cat": a.Cat, "color": catColor(a.Cat),
			"total_sec": float64(a.Total), "total": fmtMin(float64(a.Total)), "clock": a.Clock, "cur": a.Cur && detailed})
	}

	totals := dayTotals(today)
	total := float64(totals.Total)
	cats := []M{}
	for i, c := range totals.Cats {
		if i >= 6 {
			break
		}
		name := toStr(c["name"])
		sec := float64(c["sec"].(int64))
		pct := 0.0
		if total > 0 {
			pct = maxF(2, jsRound(sec/total*100))
		}
		cats = append(cats, M{"name": name, "sec": sec, "dur": fmtMin(sec), "pct": pct, "color": catColor(name)})
	}

	var health any
	if healthPublic() {
		if h := healthView(); len(h) > 0 {
			health = h
		}
	}

	return M{
		"ok": true, "today": today, "updated": isoUTC(now), "view": view,
		"summary": M{"total_sec": total, "total": fmtMin(total), "app_count": len(apps), "cats": cats},
		"apps":    outApps,
		"media":   buildMedia(),
		"health":  health,
		"report":  buildReport(db, yesterday),
	}
}
