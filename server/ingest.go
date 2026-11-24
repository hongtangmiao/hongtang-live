package main

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
 * daylog 的公开接口和采集端上报（对照 daylog/app/server.py 的 route()）：
 *   GET  /api/current          此刻在干嘛（公开页轮询）
 *   GET  /api/day?d=           某一天的时间线（历史只有管理员能看）
 */

const blackListenSec = 120

/* ---------------- 当前状态 ---------------- */

/*
 * 不止一台设备在报的时候（电脑 + 手机），「此刻」显示哪一台：
 *   还在用（没过 stale_sec、也没报「离开」）的设备里，电脑（windows / linux）优先于手机（android）；
 *   同优先级里选最近切过应用的那台；都不在用就和原来一样看最后一条。
 * 只有一台设备时结果和原来完全一样。
 */
var (
	idleMu    sync.Mutex
	idleHosts = map[string]bool{}
)

func hostPriority(host string) int {
	kind, _ := deviceByHost(host)
	switch kind {
	case "windows", "linux":
		return 2
	case "android":
		return 0
	default:
		return 1
	}
}

func setHostIdle(host string, idle bool) {
	idleMu.Lock()
	defer idleMu.Unlock()
	if idle {
		idleHosts[host] = true
	} else {
		delete(idleHosts, host)
	}
}

func hostIdle(host string) bool {
	idleMu.Lock()
	defer idleMu.Unlock()
	return idleHosts[host]
}

// latestRowsByHost：每台设备最后一条（只看今天和昨天）
func latestRowsByHost() []*curRow {
	db := daylogDB()
	if db == nil {
		return nil
	}
	since := time.Now().In(loc).AddDate(0, 0, -1).Format("2006-01-02")
	rows, err := db.Query(`SELECT l.id, l.ts, l.end_ts, l.app, l.label, l.label_plain, l.cat, l.host FROM live l
		JOIN (SELECT MAX(id) AS id FROM live WHERE day >= ? GROUP BY host) m ON l.id = m.id`, since)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*curRow
	for rows.Next() {
		var r curRow
		var plain sql.NullString
		if rows.Scan(&r.ID, &r.TS, &r.EndTS, &r.App, &r.Label, &plain, &r.Cat, &r.Host) == nil {
			r.LabelPlain = plain.String
			out = append(out, &r)
		}
	}
	return out
}

func pickCurrent(staleSec float64) *curRow {
	cur := currentRow()
	all := latestRowsByHost()
	if len(all) < 2 {
		return cur
	}
	var best *curRow
	bestPrio := -1
	for _, r := range all {
		end, ok := pyAware(r.EndTS)
		if !ok || time.Since(end).Seconds() > staleSec || hostIdle(r.Host) {
			continue
		}
		p := hostPriority(r.Host)
		if best == nil || p > bestPrio || (p == bestPrio && (r.TS > best.TS || (r.TS == best.TS && r.ID > best.ID))) {
			best, bestPrio = r, p
		}
	}
	if best != nil {
		return best
	}
	return cur
}

func currentView() M {
	cfg := dlCfg()
	owner := dOwner()
	cur := pickCurrent(float64(dInt(cfg, 180, "live", "stale_sec")))
	if cur == nil {
		return M{"now": "还没有收到 " + owner + " 的活动", "stale": true, "dur_text": "", "cat": "", "label": ""}
	}
	staleSec := float64(dInt(cfg, 180, "live", "stale_sec"))
	end, okE := pyAware(cur.EndTS)
	start, okS := pyAware(cur.TS)
	age := math.Inf(1)
	sec := int64(0)
	if okE {
		age = time.Since(end).Seconds()
		if okS {
			sec = maxI64(0, int64(end.Sub(start).Seconds()))
		}
	}
	if age > staleSec {
		ago := fmtAgo(age)
		d := "最后活动 " + ago + "前"
		if ago == "刚刚" {
			d = "最后活动 刚刚"
		}
		return M{"now": pyMeow(owner + " 不在电脑前"), "stale": true, "dur_text": d, "cat": cur.Cat, "label": pyMeow(or(cur.LabelPlain, cur.Label))}
	}
	return M{"now": pyMeow(cur.Label), "stale": false, "dur_text": "已经 " + pyFmtMin(float64(sec)), "cat": cur.Cat, "label": pyMeow(cur.Label)}
}

func pyFaceFor(view M) string {
	if !truthy(view["label"]) {
		return "ฅ(\u3000・ω・)"
	}
	if stale, _ := view["stale"].(bool); stale {
		return "ฅ(´-ω- )"
	}
	return "ฅ^•ﻌ•^ฅ"
}

// displayApps：只有在线且是当前应用才带窗口标题，其余只用不含标题的名字（P0-6）
func appsPayload(apps []*appTotal, detailed bool) []M {
	out := []M{}
	for _, a := range apps {
		label := pyMeow(or(a.LabelPlain, a.Label))
		if detailed && a.Cur {
			label = pyMeow(a.Label)
		}
		out = append(out, M{"label": label, "cat": a.Cat, "color": pyCatColor(a.Cat), "total": pyFmtMin(float64(a.Total)),
			"clock": a.Clock, "cur": a.Cur})
	}
	return out
}

// pyCatColor：pages.cat_color（按 Unicode 码点求和）
func pyCatColor(name string) string {
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

/* ---------------- 媒体卡片 ---------------- */

var liveHosts = []struct{ host, name string }{
	{"live.bilibili.com", "哔哩哔哩直播"}, {"live.douyu.com", "斗鱼"}, {"www.huya.com", "虎牙"}, {"huya.com", "虎牙"},
	{"live.douyin.com", "抖音直播"}, {"www.twitch.tv", "Twitch"}, {"twitch.tv", "Twitch"}, {"www.youtube.com", "YouTube"},
}

func liveKey(raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	host := strings.ToLower(u.Host)
	if u.User != nil {
		host = strings.ToLower(u.User.String() + "@" + u.Host)
	}
	if host == "" {
		return "", ""
	}
	host = strings.TrimPrefix(host, "www.")
	for _, h := range liveHosts {
		if host == h.host || strings.HasSuffix(host, "."+h.host) || host == strings.TrimPrefix(h.host, "www.") {
			return "live:" + host, h.name
		}
	}
	return "", ""
}

func buildMediaPy() []M {
	cfg := dlCfg()
	items := []M{}
	if dBoolDef(cfg, true, "live", "enabled") {
		for _, s := range liveSessions(900, blackListenSec) {
			if s.State != "focus" {
				continue
			}
			items = append(items, M{"kind": "live", "key": s.Key, "title": or(or(s.Title, s.URL), "直播间"), "sub": "正在看直播",
				"url": s.URL, "cover": "", "state": "focus", "confidence": "page-open-only"})
			if len(items) >= 2 {
				break
			}
		}
	}
	if len(items) < 2 && dTruthy(cfg, "music", "enabled") {
		if m := musicOf(240); m != nil && truthy(m["title"]) {
			items = append(items, M{"kind": "music", "key": "music", "title": toStr(m["title"]), "sub": toStr(m["artist"]),
				"url": toStr(m["url"]), "cover": toStr(m["cover"]), "state": "playing", "confidence": "smtc"})
		}
	}
	if len(items) > 2 {
		items = items[:2]
	}
	return items
}

/* ---------------- 健康卡（公开视图） ---------------- */

var healthFields = map[string]string{
	"sleep_duration": "sleep_min", "sleep_score": "sleep_score", "sleep_avg_hrv": "hrv", "resting_heart_rate": "rhr",
	"stress": "stress", "spo2": "spo2", "blood_oxygen": "spo2", "steps": "steps", "battery": "battery",
	"heart_rate_live": "hr_live", "heart_rate": "hr_live",
}

// healthViewPy：server.health_view —— rhr / hrv 是私密字段，任何接口都不下发
func healthViewPy() M {
	raw := healthLatest(30)
	if len(raw) == 0 {
		return M{}
	}
	out := M{"available": true, "source": "huawei"}
	dates := M{}
	now := time.Now()
	latest := ""
	for metric, it := range raw {
		if it.ts > latest {
			latest = it.ts
		}
		key := healthFields[metric]
		if key == "" {
			key = metric
		}
		value := math.RoundToEven(it.value) // Python 的 round() 是银行家舍入
		ts, ok := pyAware(it.ts)
		if key == "hr_live" {
			if !ok || now.Sub(ts).Seconds() > 120 {
				if ok {
					ago := fmtAgo(now.Sub(ts).Seconds())
					if ago == "刚刚" {
						out["hr_last_text"] = "最后 刚刚"
					} else {
						out["hr_last_text"] = "最后 " + ago + "前"
					}
				}
				continue
			}
			out["hr_live"] = value
			dates["hr_live"] = ts.In(loc).Format("15:04:05")
			out["hr_live_age"] = maxI64(0, int64(now.Sub(ts).Seconds()))
			continue
		}
		out[key] = value
		if ok {
			dates[key] = ts.In(loc).Format("01-02")
		} else {
			dates[key] = ""
		}
	}
	has := false
	for _, k := range []string{"sleep_min", "rhr", "hrv", "hr_live", "spo2", "stress"} {
		if _, ok := out[k]; ok {
			has = true
		}
	}
	if !has {
		return M{}
	}
	keep := map[string]bool{"hr_live": true, "hr_live_age": true, "hr_last_text": true, "sleep_min": true, "sleep_score": true,
		"spo2": true, "stress": true, "available": true, "source": true, "updated": true}
	for k := range out {
		if !keep[k] {
			delete(out, k)
		}
	}
	dk := map[string]bool{"sleep": true, "sleep_min": true, "sleep_score": true, "spo2": true, "stress": true, "hr_live": true}
	fd := M{}
	for k, v := range dates {
		if dk[k] {
			fd[k] = v
		}
	}
	out["dates"] = fd
	out["updated"] = latest
	return out
}

// icuToMetrics：Intervals.icu 一条 wellness → {metric: value}（只取 > 0 的）
var icuFields = []struct {
	field, metric string
	scale         float64
}{
	{"restingHR", "resting_heart_rate", 1}, {"steps", "steps", 1}, {"hrv", "sleep_avg_hrv", 1}, {"hrvSDNN", "hrv_sdnn", 1},
	{"sleepSecs", "sleep_duration", 1 / 60.0}, {"sleepScore", "sleep_score", 1}, {"sleepQuality", "sleep_quality", 1},
	{"weight", "body_weight", 1}, {"avgSleepingHR", "sleep_avg_heart_rate", 1}, {"spO2", "spo2", 1}, {"stress", "stress", 1},
	{"respiration", "respiration", 1},
}

func icuToMetrics(rec M) map[string]float64 {
	out := map[string]float64{}
	for _, f := range icuFields {
		v, ok := rec[f.field]
		if !ok || v == nil || v == "" {
			continue
		}
		n, ok := pyFloat(v)
		if !ok {
			continue
		}
		n *= f.scale
		if n <= 0 {
			continue
		}
		out[f.metric] = math.RoundToEven(n*10) / 10
	}
	return out
}

// pyFloat：Python 的 float(x)（字符串会去掉两边空白）
func pyFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(pyStrip(t), 64)
		return f, err == nil
	case nil:
		return 0, false
	}
	switch v.(type) {
	case float64, json.Number, int, int64:
		return toNum(v), true
	}
	return 0, false
}

/* ---------------- 天 ---------------- */

var reDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func todayKey() string { return time.Now().In(loc).Format("2006-01-02") }

// pickDay：历史只能管理员看，公开页只给「今天」
func pickDay(r *http.Request) string {
	today := todayKey()
	d := r.URL.Query().Get("d")
	if d == "" || !reDay.MatchString(d) {
		return today
	}
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return today
	}
	if d != today && !isAdmin(r) {
		return today
	}
	return d
}

// neighbourDays：前一天 / 后一天按自然日走，只在有数据的日期范围内开放
func neighbourDays(day, today string) (string, string) {
	days := knownDays()
	first, last := day, day
	if len(days) > 0 {
		first = days[0]
		last = days[len(days)-1]
		if today > last {
			last = today
		}
	}
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return "", ""
	}
	prev, next := d.AddDate(0, 0, -1).Format("2006-01-02"), d.AddDate(0, 0, 1).Format("2006-01-02")
	if prev < first {
		prev = ""
	}
	if next > last {
		next = ""
	}
	return prev, next
}

/* ---------------- 访客计数 cookie ---------------- */

func viewerID(r *http.Request) string {
	c, err := r.Cookie("dl_v")
	if err != nil || len(c.Value) < 8 || len(c.Value) > 64 {
		return ""
	}
	return c.Value
}

func countViewer(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.UserAgent(), "Mozilla") {
		return
	}
	vid := viewerID(r)
	if vid == "" {
		vid = randHex(9)
		http.SetCookie(w, &http.Cookie{Name: "dl_v", Value: vid, Path: "/", MaxAge: 31536000, HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: secureReq(r)})
	}
	touchView(vid)
}

/* ---------------- GET /api/current ---------------- */

func healthPublic() bool { return dBoolDef(dlCfg(), true, "health", "public") }

func handleCurrent(w http.ResponseWriter, r *http.Request) {
	countViewer(w, r)
	view := currentView()
	day := pickDay(r)
	stale, _ := view["stale"].(bool)
	out := M{"ok": true, "face": pyFaceFor(view), "apps": appsPayload(appTotals(day), !stale), "viewers": viewers(300),
		"media": buildMediaPy(), "health": nil, "today": pyFmtMin(float64(dayTotals(day).Total))}
	for k, v := range view {
		out[k] = v
	}
	if healthPublic() {
		out["health"] = healthViewPy()
	}
	writeJSON(w, 200, out)
}

/* ---------------- GET /api/day ---------------- */

func handleDay(w http.ResponseWriter, r *http.Request) {
	target := pickDay(r)
	view := currentView()
	totals := dayTotals(target)
	today := todayKey()
	isToday := target == today
	stale, _ := view["stale"].(bool)
	prev, next := neighbourDays(target, today)
	cats := []M{}
	for i, c := range totals.Cats {
		if i >= 6 {
			break
		}
		cats = append(cats, M{"name": c["name"], "dur": pyFmtMin(float64(c["sec"].(int64)))})
	}
	media := []M{}
	if isToday {
		media = buildMediaPy()
	}
	out := M{"ok": true, "day": target, "is_today": isToday, "apps": appsPayload(appTotals(target), isToday && !stale),
		"total": pyFmtMin(float64(totals.Total)), "cats": cats, "prev": prev, "next": next, "viewers": viewers(300), "media": media}
	if isToday {
		meta := toStr(view["dur_text"])
		if truthy(view["cat"]) {
			meta += " · " + toStr(view["cat"])
		}
		out["title"], out["meta"], out["face"], out["stale"], out["day_label"] = view["now"], pyStrip(meta), pyFaceFor(view), stale, "今天"
	} else {
		d, _ := time.Parse("2006-01-02", target)
		wd := []string{"一", "二", "三", "四", "五", "六", "日"}[(int(d.Weekday())+6)%7]
		out["title"] = strconv.Itoa(int(d.Month())) + " 月 " + strconv.Itoa(d.Day()) + " 日"
		out["meta"] = "周" + wd + " · 一共在电脑前 " + pyFmtMin(float64(totals.Total))
		out["face"], out["stale"], out["day_label"] = "ฅ(・ω・ )", true, "当天"
	}
	writeJSON(w, 200, out)
}
