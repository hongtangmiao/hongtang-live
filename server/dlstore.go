package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
 * daylog 数据库的写入和 Python 版同样口径的读取（对照 daylog/app/store.py）。
 * 只有 daylog_writer=true 时才会写；否则 daylogDB() 是只读打开的，这里的写入都会失败返回。
 */

const dlSchema = `
CREATE TABLE IF NOT EXISTS live (
    id INTEGER PRIMARY KEY AUTOINCREMENT, day TEXT NOT NULL, ts TEXT NOT NULL, end_ts TEXT NOT NULL,
    host TEXT NOT NULL, app TEXT NOT NULL, label TEXT NOT NULL, cat TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '', dur INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS idx_live_day ON live(day, host);
CREATE TABLE IF NOT EXISTS devices (host TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', last_seen TEXT NOT NULL,
    last_app TEXT NOT NULL DEFAULT '', last_label TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS viewers (vid TEXT PRIMARY KEY, ts TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS music (day TEXT NOT NULL, title TEXT NOT NULL, artist TEXT NOT NULL DEFAULT '',
    album TEXT NOT NULL DEFAULT '', song_id INTEGER NOT NULL DEFAULT 0, cover TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '', first_ts TEXT NOT NULL, last_ts TEXT NOT NULL, plays INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (day, title, artist));
CREATE TABLE IF NOT EXISTS health_metrics (metric TEXT NOT NULL, ts TEXT NOT NULL, value REAL NOT NULL,
    unit TEXT NOT NULL DEFAULT '', PRIMARY KEY (metric, ts));
CREATE TABLE IF NOT EXISTS media (key TEXT PRIMARY KEY, kind TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
    sub TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '', cover TEXT NOT NULL DEFAULT '',
    first_ts TEXT NOT NULL, last_seen TEXT NOT NULL, last_focus TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS sessions (token TEXT PRIMARY KEY, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS reports (day TEXT PRIMARY KEY, stats TEXT NOT NULL, analysis TEXT NOT NULL,
    markdown TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, telegram_sent INTEGER NOT NULL DEFAULT 0);
`

var dlWriteMu sync.Mutex // SQLite 同一时间只让一个写，免得 database is locked

var errReadOnly = errors.New("daylog 数据库是只读打开的（daylog_writer 没开）")

// dlMigrate：建表 + 老库补字段（Store.__init__ 里的那几条）
func dlMigrate(db *sql.DB) error {
	if _, err := db.Exec(dlSchema); err != nil {
		return err
	}
	cols := func(table string) map[string]bool {
		out := map[string]bool{}
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt sql.NullString
			if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
				out[name] = true
			}
		}
		return out
	}
	dc := cols("devices")
	for _, c := range []string{"music", "music_ts"} {
		if !dc[c] {
			if _, err := db.Exec("ALTER TABLE devices ADD COLUMN " + c + " TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	if !cols("live")["label_plain"] {
		if _, err := db.Exec("ALTER TABLE live ADD COLUMN label_plain TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

func dlWriter() (*sql.DB, error) {
	if !conf().DaylogWriter {
		return nil, errReadOnly
	}
	db := daylogDB()
	if db == nil {
		return nil, errors.New("daylog 数据库打不开")
	}
	return db, nil
}

// pyAware：fromisoformat，没有时区的当本地时间（Python 的 astimezone() 也是这么处理的）
func pyAware(s string) (time.Time, bool) {
	t, hasTZ, ok := pyFromISO(s)
	if !ok {
		return t, false
	}
	if !hasTZ {
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
	}
	return t, true
}

func secsBetween(a, b string) (float64, bool) {
	ta, ok1 := pyAware(a)
	tb, ok2 := pyAware(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	return tb.Sub(ta).Seconds(), true
}

/* ---------------- 实时 ---------------- */

// addSample：同一个 (host, app, label, cat) 在 gap 内就延长上一段，否则新起一段
func addSample(s sample, gapSec int) error {
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	var endTS string
	var dur int64
	err = tx.QueryRow("SELECT id, end_ts, dur FROM live WHERE host=? AND day=? ORDER BY id DESC LIMIT 1", s.Host, s.Day).Scan(&id, &endTS, &dur)
	hasRow := err == nil
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if hasRow {
		gap, ok := secsBetween(endTS, s.TS)
		var app, label, cat string
		if ok && tx.QueryRow("SELECT app,label,cat FROM live WHERE id=?", id).Scan(&app, &label, &cat) == nil &&
			gap <= float64(gapSec) && app == s.App && label == s.Label && cat == s.Cat {
			if _, err := tx.Exec("UPDATE live SET end_ts=?, dur=?, label_plain=COALESCE(NULLIF(label_plain,''), ?) WHERE id=?",
				s.TS, dur+int64(gap), s.LabelPlain, id); err != nil {
				return err
			}
			return tx.Commit()
		}
		// 这段时间其实花在「上一条采样」的应用上，补给它，否则每段都会是 0s
		if ok && gap > 0 && gap <= 300 {
			if _, err := tx.Exec("UPDATE live SET dur = dur + ? WHERE id = ?", int64(gap), id); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec("INSERT INTO live(day, ts, end_ts, host, app, label, label_plain, cat, title, dur) VALUES (?,?,?,?,?,?,?,?,?,0)",
		s.Day, s.TS, s.TS, s.Host, s.App, s.Label, or(s.LabelPlain, s.Label), s.Cat, s.Title); err != nil {
		return err
	}
	return tx.Commit()
}

// markIdle：桌面 / 锁屏期间只推进设备的 last_seen，不写 live
func markIdle(host, name, ts string) error {
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, err = db.Exec("INSERT INTO devices(host, name, last_seen, last_app, last_label) VALUES (?,?,?,?,?) "+
		"ON CONFLICT(host) DO UPDATE SET name=excluded.name, last_seen=excluded.last_seen", host, name, ts, "", "")
	return err
}

// touchDevice：music 为 nil 表示不动音乐字段，"" 表示清空
func touchDevice(host, name, ts, app, label string, music *string) error {
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO devices(host, name, last_seen, last_app, last_label) VALUES (?,?,?,?,?) "+
		"ON CONFLICT(host) DO UPDATE SET name=excluded.name, last_seen=excluded.last_seen, last_app=excluded.last_app, last_label=excluded.last_label",
		host, name, ts, app, label); err != nil {
		return err
	}
	if music != nil {
		if _, err := tx.Exec("UPDATE devices SET music=?, music_ts=? WHERE host=?", *music, ts, host); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// musicOf：最近一首（超过 maxAge 秒就当没在放）
func musicOf(maxAge float64) M {
	db := daylogDB()
	if db == nil {
		return nil
	}
	var music, ts string
	if db.QueryRow("SELECT music, music_ts FROM devices WHERE music != '' ORDER BY music_ts DESC LIMIT 1").Scan(&music, &ts) != nil {
		return nil
	}
	t, ok := pyAware(ts)
	if !ok || time.Since(t).Seconds() > maxAge {
		return nil
	}
	var m M
	if json.Unmarshal([]byte(music), &m) != nil {
		return nil
	}
	cover := httpsURL(toStr(m["cover"]))
	title, artist := toStr(m["title"]), toStr(m["artist"])
	if cover == "" && title != "" {
		var histCover sql.NullString
		if db.QueryRow("SELECT cover FROM music WHERE title=? AND artist=? AND cover != '' ORDER BY last_ts DESC LIMIT 1", title, artist).Scan(&histCover) == nil {
			cover = httpsURL(histCover.String)
		}
	}
	if cover == "" && title != "" {
		if e := enrichMusic(m); truthy(e["cover"]) {
			cover = httpsURL(toStr(e["cover"]))
			if !truthy(m["url"]) && truthy(e["url"]) {
				m["url"] = e["url"]
			}
		}
	}
	m["cover"] = cover
	return m
}

/* ---------------- 健康 ---------------- */

func addHealth(metric, ts string, value float64, unit string) error {
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, err = db.Exec("INSERT INTO health_metrics(metric, ts, value, unit) VALUES (?,?,?,?) "+
		"ON CONFLICT(metric, ts) DO UPDATE SET value=excluded.value, unit=excluded.unit", metric, ts, value, unit)
	return err
}

type healthItem struct {
	value float64
	ts    string
}

func healthLatest(days int) map[string]healthItem {
	db := daylogDB()
	out := map[string]healthItem{}
	if db == nil {
		return out
	}
	cutoff := isoSec(time.Now().Add(-time.Duration(days) * 24 * time.Hour))
	rows, err := db.Query("SELECT metric, MAX(ts) AS ts, value FROM health_metrics WHERE ts >= ? GROUP BY metric", cutoff)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m, ts string
		var v float64
		if rows.Scan(&m, &ts, &v) == nil {
			out[m] = healthItem{v, ts}
		}
	}
	return out
}

/* ---------------- 直播会话 ---------------- */

func upsertLive(key, title, u, ts string) error {
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	res, err := db.Exec("UPDATE media SET title=?, url=?, last_seen=?, last_focus=? WHERE key=?", title, u, ts, ts, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = db.Exec("INSERT INTO media(key, kind, title, url, first_ts, last_seen, last_focus) VALUES (?,?,?,?,?,?,?)",
		key, "live", title, u, ts, ts, ts)
	return err
}

type liveSess struct {
	Key, Title, URL, LastFocus, State string
}

func liveSessions(maxAge, blackAfter float64) []liveSess {
	db := daylogDB()
	if db == nil {
		return nil
	}
	rows, err := db.Query("SELECT key, title, url, last_seen, last_focus FROM media WHERE kind='live'")
	if err != nil {
		return nil
	}
	defer rows.Close()
	now := time.Now()
	var out []liveSess
	for rows.Next() {
		var k, title, u, seen, focus string
		if rows.Scan(&k, &title, &u, &seen, &focus) != nil {
			continue
		}
		ts, ok1 := pyAware(seen)
		tf, ok2 := pyAware(or(focus, seen))
		if !ok1 || !ok2 || now.Sub(ts).Seconds() > maxAge {
			continue
		}
		st := "background"
		if now.Sub(tf).Seconds() < blackAfter {
			st = "focus"
		}
		out = append(out, liveSess{k, title, u, focus, st})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastFocus > out[j].LastFocus })
	return out
}

/* ---------------- 听歌 ---------------- */

func addMusic(day, ts string, m M) error {
	title := pyStrip(toStr(m["title"]))
	if title == "" {
		return nil
	}
	artist := pyStrip(toStr(m["artist"]))
	db, err := dlWriter()
	if err != nil {
		return err
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	sid := int64(toNum(m["song_id"]))
	var last string
	var plays int64
	err = db.QueryRow("SELECT last_ts, plays FROM music WHERE day=? AND title=? AND artist=?", day, title, artist).Scan(&last, &plays)
	if err == nil {
		gap, ok := secsBetween(last, ts)
		if !(ok && gap <= 600) {
			plays++
		}
		_, err = db.Exec("UPDATE music SET last_ts=?, plays=?, album=COALESCE(NULLIF(?, ''), album), cover=COALESCE(NULLIF(?, ''), cover), url=COALESCE(NULLIF(?, ''), url), song_id=CASE WHEN ? > 0 THEN ? ELSE song_id END WHERE day=? AND title=? AND artist=?",
			ts, plays, toStr(m["album"]), httpsURL(toStr(m["cover"])), toStr(m["url"]), sid, sid, day, title, artist)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = db.Exec("INSERT INTO music(day, title, artist, album, song_id, cover, url, first_ts, last_ts, plays) VALUES (?,?,?,?,?,?,?,?,?,1)",
		day, title, artist, toStr(m["album"]), sid, httpsURL(toStr(m["cover"])), toStr(m["url"]), ts, ts)
	return err
}

/* ---------------- 访客 ---------------- */

func naiveNow() string { return time.Now().In(loc).Format("2006-01-02T15:04:05") }

func touchView(vid string) {
	db, err := dlWriter()
	if err != nil {
		return
	}
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, _ = db.Exec("INSERT INTO viewers(vid, ts) VALUES (?,?) ON CONFLICT(vid) DO UPDATE SET ts=excluded.ts", vid, naiveNow())
}

func viewers(windowSec int) int {
	db := daylogDB()
	if db == nil {
		return 0
	}
	cutoff := time.Now().In(loc).Add(-time.Duration(windowSec) * time.Second).Format("2006-01-02T15:04:05")
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM viewers WHERE ts >= ?", cutoff).Scan(&n)
	return n
}

/* ---------------- 读：当前 / 应用汇总 / 当天总计 ---------------- */

type curRow struct {
	ID                                           int64
	TS, EndTS, App, Label, LabelPlain, Cat, Host string
}

func currentRow() *curRow {
	db := daylogDB()
	if db == nil {
		return nil
	}
	var r curRow
	if db.QueryRow("SELECT id, ts, end_ts, app, label, label_plain, cat, host FROM live ORDER BY id DESC LIMIT 1").
		Scan(&r.ID, &r.TS, &r.EndTS, &r.App, &r.Label, &r.LabelPlain, &r.Cat, &r.Host) != nil {
		return nil
	}
	return &r
}

type appTotal struct {
	App, LastTS, Label, LabelPlain, Cat, Clock string
	Total                                      int64
	ID                                         int64
	Cur                                        bool
}

func hhmm(iso string) string {
	t, _, ok := pyFromISO(iso)
	if !ok {
		return ""
	}
	return t.Format("15:04") // fromisoformat 保留原时区，不换算
}

// appTotals：按应用合并（总时长 + 最后一次用的时间），按最后使用时间倒序
func appTotals(day string) []*appTotal {
	db := daylogDB()
	if db == nil {
		return nil
	}
	rows, err := db.Query("SELECT id, ts, end_ts, app, label, label_plain, cat, dur FROM live WHERE day=? ORDER BY id ASC", day)
	if err != nil {
		return nil
	}
	type r struct {
		id                              int64
		ts, end, app, label, plain, cat string
		dur                             int64
	}
	var all []r
	for rows.Next() {
		var x r
		var plain sql.NullString
		if rows.Scan(&x.id, &x.ts, &x.end, &x.app, &x.label, &plain, &x.cat, &x.dur) == nil {
			x.plain = plain.String
			all = append(all, x)
		}
	}
	rows.Close()
	var lastID int64 = -1
	if len(all) > 0 {
		lastID = all[len(all)-1].id
		if day == todayKey() {
			if cur := pickCurrent(float64(dInt(dlCfg(), 180, "live", "stale_sec"))); cur != nil {
				for _, x := range all {
					if x.id == cur.ID {
						lastID = cur.ID
						break
					}
				}
			}
		}
	}
	now := time.Now()
	var order []*appTotal
	idx := map[string]*appTotal{}
	for _, x := range all {
		span, _ := secsBetween(x.ts, x.end)
		sec := maxI64(x.dur, maxI64(0, int64(span))) // max(int(dur), max(0, span))
		if x.id == lastID {
			if t, ok := pyAware(x.ts); ok {
				live := int64(now.Sub(t).Seconds())
				if l := minI64(live, 300); l > sec {
					sec = l
				}
			}
		}
		a := idx[x.app]
		if a == nil {
			a = &appTotal{App: x.app, LastTS: x.ts, Label: x.label, LabelPlain: or(x.plain, x.label), Cat: x.cat, ID: x.id}
			idx[x.app] = a
			order = append(order, a)
		}
		a.Total += sec
		if x.ts >= a.LastTS {
			a.LastTS, a.Label, a.LabelPlain, a.Cat, a.ID = x.ts, x.label, or(x.plain, x.label), x.cat, x.id
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].LastTS > order[j].LastTS })
	for _, a := range order {
		a.Cur = a.ID == lastID
		a.Clock = hhmm(a.LastTS)
	}
	return order
}

func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minI64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

type dayTotal struct {
	Total int64
	Cats  []M
	Apps  int64
	Last  string
}

func dayTotals(day string) dayTotal {
	out := dayTotal{Cats: []M{}}
	db := daylogDB()
	if db == nil {
		return out
	}
	if rows, err := db.Query("SELECT cat, SUM(dur) AS sec FROM live WHERE day=? GROUP BY cat ORDER BY sec DESC", day); err == nil {
		for rows.Next() {
			var c string
			var s sql.NullInt64
			if rows.Scan(&c, &s) == nil {
				out.Cats = append(out.Cats, M{"name": c, "sec": s.Int64})
			}
		}
		rows.Close()
	}
	var tot sql.NullInt64
	_ = db.QueryRow("SELECT SUM(dur) FROM live WHERE day=?", day).Scan(&tot)
	out.Total = tot.Int64
	_ = db.QueryRow("SELECT COUNT(DISTINCT app) FROM live WHERE day=?", day).Scan(&out.Apps)
	var last sql.NullString
	_ = db.QueryRow("SELECT MAX(end_ts) FROM live WHERE day=?", day).Scan(&last)
	out.Last = last.String
	return out
}

func knownDays() []string {
	db := daylogDB()
	if db == nil {
		return nil
	}
	set := map[string]bool{}
	for _, q := range []string{"SELECT DISTINCT day FROM live", "SELECT DISTINCT day FROM reports"} {
		rows, err := db.Query(q)
		if err != nil {
			continue
		}
		for rows.Next() {
			var d sql.NullString
			if rows.Scan(&d) == nil && d.String != "" {
				set[d.String] = true
			}
		}
		rows.Close()
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// pyFmtMin：截断到分钟，不四舍五入
func pyFmtMin(sec float64) string {
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

func logErr(what string, err error) {
	if err != nil && !strings.Contains(err.Error(), "只读") {
		log.Printf("[daylog] %s: %v", what, err)
	}
}
