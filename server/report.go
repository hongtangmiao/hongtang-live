package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

/*
 * 日报流水线（对照 daylog/app/daily.py、llm.py、plugins/telegram.py、server.py 的 scheduler_loop / icu_sync）：
 *   inbox/*.json（ActivityWatch 导出）→ 聚合 → DeepSeek → reports 表 + reports/*.md → Telegram
 * 只有 daylog_writer=true 时定时任务才会跑，免得和原 daylog 容器同时推送。
 */

var runLock sync.Mutex // 手动任务和定时任务共用，防止同一份日报并发生成、重复推送

func dlRoot() string { return or(conf().DaylogRoot, os.Getenv("DAYLOG_ROOT")) }

func dlPath(parts ...string) string { return filepath.Join(append([]string{dlRoot()}, parts...)...) }

/* ---------------- 日志：写到 daylog 的 logs/app.log（和原来同一个文件） ---------------- */

var dlLogMu sync.Mutex

func dlLog(msg string) {
	line := "[" + time.Now().In(loc).Format("2006-01-02 15:04:05") + "] " + msg
	log.Print("[daylog] " + msg)
	if dlRoot() == "" {
		return
	}
	dlLogMu.Lock()
	defer dlLogMu.Unlock()
	p := dlPath("logs", "app.log")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

/* ---------------- reports 表 ---------------- */

type report struct {
	Day       string
	Stats     *OM
	Analysis  *OM
	Markdown  string
	CreatedAt string
	TGSent    bool
}

func saveReport(day string, stats, analysis *OM, md string) error {
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, err = db.Exec("INSERT INTO reports(day, stats, analysis, markdown, created_at) VALUES (?,?,?,?,?) "+
		"ON CONFLICT(day) DO UPDATE SET stats=excluded.stats, analysis=excluded.analysis, markdown=excluded.markdown, created_at=excluded.created_at",
		day, pyDumps(stats, -1), pyDumps(orEmptyOM(analysis), -1), md, naiveNow())
	return err
}

func orEmptyOM(o *OM) *OM {
	if o == nil {
		return NewOM()
	}
	return o
}

func getReport(day string) *report {
	db := daylogDB()
	if db == nil {
		return nil
	}
	var r report
	var st, an string
	var tg int
	if db.QueryRow("SELECT day, stats, analysis, markdown, created_at, telegram_sent FROM reports WHERE day=?", day).
		Scan(&r.Day, &st, &an, &r.Markdown, &r.CreatedAt, &tg) != nil {
		return nil
	}
	r.TGSent = tg != 0
	if v, err := pyLoads([]byte(st)); err == nil {
		r.Stats, _ = v.(*OM)
	}
	if v, err := pyLoads([]byte(an)); err == nil {
		r.Analysis, _ = v.(*OM)
	}
	if r.Stats == nil {
		r.Stats = NewOM()
	}
	if r.Analysis == nil {
		r.Analysis = NewOM()
	}
	return &r
}

type reportRow struct {
	Day       string `json:"day"`
	CreatedAt string `json:"created_at"`
	TGSent    bool   `json:"telegram_sent"`
}

func listReports(limit int) []reportRow {
	out := []reportRow{}
	db := daylogDB()
	if db == nil {
		return out
	}
	rows, err := db.Query("SELECT day, substr(created_at,1,16), telegram_sent FROM reports ORDER BY day DESC LIMIT ?", limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r reportRow
		var tg int
		if rows.Scan(&r.Day, &r.CreatedAt, &tg) == nil {
			r.TGSent = tg != 0
			out = append(out, r)
		}
	}
	return out
}

func markTelegram(day string, ok bool) {
	db, err := dlWriter()
	if err != nil {
		return
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	v := 0
	if ok {
		v = 1
	}
	_, _ = db.Exec("UPDATE reports SET telegram_sent=? WHERE day=?", v, day)
}

/* ---------------- 当天听过的歌 / 身体数据 ---------------- */

func musicOfDay(day string) []any {
	out := []any{}
	db := daylogDB()
	if db == nil {
		return out
	}
	rows, err := db.Query("SELECT title, artist, album, cover, url, first_ts, last_ts, plays FROM music WHERE day=? ORDER BY last_ts ASC", day)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t, a, al, c, u, f, l string
		var plays int64
		if rows.Scan(&t, &a, &al, &c, &u, &f, &l, &plays) != nil {
			continue
		}
		span, ok := secsBetween(f, l)
		if !ok {
			span = 0
		}
		minutes := int64(1)
		if span > 0 {
			minutes = maxI64(1, int64(pyFloorDiv(span, 60)))
		}
		out = append(out, NewOM("title", t, "artist", a, "album", al, "cover", c, "url", u, "first_ts", f, "last_ts", l,
			"plays", plays, "minutes", minutes))
	}
	return out
}

var healthLabels = [][2]string{
	{"sleep_duration", "睡眠时长(分钟)"}, {"sleep_score", "睡眠得分"}, {"deep_sleep_min", "深睡(分钟)"},
	{"sleep_avg_hrv", "睡眠HRV(ms)"}, {"resting_heart_rate", "静息心率(bpm)"}, {"stress", "压力(0-100，越低越松)"},
	{"body_weight", "体重(kg)"},
}

// healthBrief：当天每个指标的最新值翻成人话（给模型和日报用）
func healthBrief(day string) *OM {
	out := NewOM()
	db := daylogDB()
	if db == nil {
		return out
	}
	rows, err := db.Query("SELECT metric, value FROM health_metrics WHERE ts >= ? AND ts <= ? ORDER BY ts ASC", day+"T00:00:00", day+"T23:59:59")
	if err != nil {
		return out
	}
	latest := map[string]float64{}
	for rows.Next() {
		var m string
		var v float64
		if rows.Scan(&m, &v) == nil {
			latest[m] = v
		}
	}
	rows.Close()
	for _, l := range healthLabels {
		if v, ok := latest[l[0]]; ok {
			out.Set(l[1], pyRoundN(v, 1))
		}
	}
	return out
}
