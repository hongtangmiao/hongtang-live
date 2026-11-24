package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

/*
 * 设置页里的 daylog 部分：GET/POST /api/admin/daylog/settings
 * 密钥（DeepSeek key / Telegram token / ICU key / 上报口令）只回「有没有」，留空不改。
 */

var dlSecretPaths = map[string][]string{
	"analysis.api_key":   {"analysis", "api_key"},
	"telegram.bot_token": {"telegram", "bot_token"},
	"health.icu.api_key": {"health", "icu", "api_key"},
}

func deepCopyM(v any) any {
	switch t := v.(type) {
	case map[string]any:
		o := make(map[string]any, len(t))
		for k, x := range t {
			o[k] = deepCopyM(x)
		}
		return o
	case []any:
		o := make([]any, len(t))
		for i, x := range t {
			o[i] = deepCopyM(x)
		}
		return o
	}
	return v
}

func setPath(m M, path []string, v any) {
	cur := m
	for _, k := range path[:len(path)-1] {
		nx := asM(cur[k])
		if nx == nil {
			nx = M{}
			cur[k] = nx
		}
		cur = nx
	}
	cur[path[len(path)-1]] = v
}

func dlSettingsView() M {
	d := deepCopyM(dlCfg()).(map[string]any)
	set := M{}
	for name, p := range dlSecretPaths {
		set[name] = dStr(d, p...) != ""
		setPath(d, p, "")
	}
	setPath(d, []string{"live", "token"}, "")
	inbox, _ := filepath.Glob(dlPath("inbox", "*.json"))
	failed, _ := filepath.Glob(dlPath("inbox", "*.json.failed"))
	last := ""
	if r := listReports(1); len(r) > 0 {
		last = r[0].Day
	}
	root := dlRoot()
	rootOK := false
	if root != "" {
		if st, err := os.Stat(root); err == nil && st.IsDir() {
			rootOK = true
		}
	}
	busy := !runLock.TryLock()
	if !busy {
		runLock.Unlock()
	}
	return M{"daylog": d, "secrets_set": set, "status": M{
		"writer": conf().DaylogWriter, "root": root, "root_ok": rootOK, "db_ready": daylogDB() != nil,
		"inbox": len(inbox), "failed": len(failed), "last_report": last, "busy": busy, "status_texts_error": stErr,
	}}
}

type dlPatch struct {
	Daylog struct {
		Site *struct {
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
			Owner    string `json:"owner"`
		} `json:"site"`
		Analysis *struct {
			Enabled         bool    `json:"enabled"`
			APIBase         string  `json:"api_base"`
			Model           string  `json:"model"`
			MaxOutputTokens int     `json:"max_output_tokens"`
			Temperature     float64 `json:"temperature"`
			DailyTime       string  `json:"daily_time"`
			WeeklyTime      string  `json:"weekly_time"`
		} `json:"analysis"`
		Telegram *struct {
			Enabled    bool   `json:"enabled"`
			ChatID     string `json:"chat_id"`
			SendDaily  bool   `json:"send_daily"`
			SendWeekly bool   `json:"send_weekly"`
		} `json:"telegram"`
		Live *struct {
			PublicShowTitle bool `json:"public_show_title"`
			StaleSec        int  `json:"stale_sec"`
			PollSec         int  `json:"poll_sec"`
			SegmentGapSec   int  `json:"segment_gap_sec"`
		} `json:"live"`
		Privacy *struct {
			SecretApps    []string `json:"secret_apps"`
			HideTitleApps []string `json:"hide_title_apps"`
			BrowserApps   []string `json:"browser_apps"`
		} `json:"privacy"`
		Health *struct {
			Public bool `json:"public"`
			ICU    struct {
				Enabled   bool   `json:"enabled"`
				AthleteID string `json:"athlete_id"`
				PollMin   int    `json:"poll_min"`
				Days      int    `json:"days"`
			} `json:"icu"`
		} `json:"health"`
		Music *struct {
			Enabled bool   `json:"enabled"`
			APIBase string `json:"api_base"`
		} `json:"music"`
		AppNames   map[string]string `json:"app_names"`
		Categories []struct {
			Name     string   `json:"name"`
			Patterns []string `json:"patterns"`
		} `json:"categories"`
		HasCategories bool `json:"has_categories"`
	} `json:"daylog"`
	Secrets map[string]string `json:"secrets"`
	Clear   []string          `json:"clear"`
}

var (
	reHHMM   = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	reWeekly = regexp.MustCompile(`^(Mon|Tue|Wed|Thu|Fri|Sat|Sun) ([01]\d|2[0-3]):[0-5]\d$`)
	reChatID = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z0-9_]{4,40})$`)
	reAthID  = regexp.MustCompile(`^[A-Za-z0-9_-]{0,40}$`)
)

func cleanList(in []string, name string, max int) ([]any, error) {
	out := []any{}
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		if utf8.RuneCountInString(s) > 100 {
			return nil, errors.New(name + " 里有一项太长（最多 100 字）：" + runeCut(s, 20) + "…")
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) > max {
		return nil, fmt.Errorf("%s 最多 %d 项", name, max)
	}
	return out, nil
}

func between(n, lo, hi int, name string) error {
	if n < lo || n > hi {
		return fmt.Errorf("%s 要在 %d 到 %d 之间", name, lo, hi)
	}
	return nil
}

func applyDlPatch(d M, p *dlPatch) error {
	x := p.Daylog
	var err error
	if s := x.Site; s != nil {
		t, err1 := limit(s.Title, 60, "daylog 标题")
		sub, err2 := limit(s.Subtitle, 80, "副标题")
		owner, err3 := limit(s.Owner, 20, "主人名字")
		if e := errors.Join(err1, err2, err3); e != nil {
			return e
		}
		setPath(d, []string{"site", "title"}, or(t, "今天在干嘛"))
		setPath(d, []string{"site", "subtitle"}, sub)
		setPath(d, []string{"site", "owner"}, or(owner, "我"))
	}
	if a := x.Analysis; a != nil {
		base, e := httpURL(a.APIBase, "DeepSeek API 地址")
		if e != nil {
			return e
		}
		model, e := limit(a.Model, 60, "模型名")
		if e != nil {
			return e
		}
		if !reHHMM.MatchString(a.DailyTime) {
			return errors.New("日报时间要写成 00:10 这种")
		}
		if !reWeekly.MatchString(a.WeeklyTime) {
			return errors.New("周报时间要写成 Mon 00:40 这种（星期用英文缩写）")
		}
		if e := between(a.MaxOutputTokens, 500, 16000, "最大输出 token"); e != nil {
			return e
		}
		if a.Temperature < 0 || a.Temperature > 2 {
			return errors.New("temperature 要在 0 到 2 之间")
		}
		for k, v := range map[string]any{"enabled": a.Enabled, "api_base": or(base, "https://api.deepseek.com"), "model": or(model, "deepseek-flash"),
			"max_output_tokens": float64(a.MaxOutputTokens), "temperature": a.Temperature, "daily_time": a.DailyTime, "weekly_time": a.WeeklyTime} {
			setPath(d, []string{"analysis", k}, v)
		}
	}
	if t := x.Telegram; t != nil {
		chat := strings.TrimSpace(t.ChatID)
		if chat != "" && !reChatID.MatchString(chat) {
			return errors.New("Telegram chat id 要是数字（群是负数）或 @频道名")
		}
		for k, v := range map[string]any{"enabled": t.Enabled, "chat_id": chat, "send_daily": t.SendDaily, "send_weekly": t.SendWeekly} {
			setPath(d, []string{"telegram", k}, v)
		}
	}
	if l := x.Live; l != nil {
		if err = errors.Join(between(l.StaleSec, 30, 3600, "离开判定秒数"), between(l.PollSec, 5, 120, "页面刷新秒数"),
			between(l.SegmentGapSec, 30, 900, "合并间隔秒数")); err != nil {
			return err
		}
		for k, v := range map[string]any{"public_show_title": l.PublicShowTitle, "stale_sec": float64(l.StaleSec),
			"poll_sec": float64(l.PollSec), "segment_gap_sec": float64(l.SegmentGapSec)} {
			setPath(d, []string{"live", k}, v)
		}
	}
	if pv := x.Privacy; pv != nil {
		for _, it := range []struct {
			key  string
			v    []string
			name string
		}{{"secret_apps", pv.SecretApps, "私密应用"}, {"hide_title_apps", pv.HideTitleApps, "隐藏标题的应用"}, {"browser_apps", pv.BrowserApps, "浏览器"}} {
			l, e := cleanList(it.v, it.name, 300)
			if e != nil {
				return e
			}
			setPath(d, []string{"privacy", it.key}, l)
		}
	}
	if h := x.Health; h != nil {
		if !reAthID.MatchString(strings.TrimSpace(h.ICU.AthleteID)) {
			return errors.New("Intervals.icu athlete id 只能有字母、数字、- 和 _")
		}
		if err = errors.Join(between(h.ICU.PollMin, 5, 1440, "ICU 拉取间隔（分钟）"), between(h.ICU.Days, 1, 90, "ICU 拉取天数")); err != nil {
			return err
		}
		setPath(d, []string{"health", "public"}, h.Public)
		for k, v := range map[string]any{"enabled": h.ICU.Enabled, "athlete_id": strings.TrimSpace(h.ICU.AthleteID),
			"poll_min": float64(h.ICU.PollMin), "days": float64(h.ICU.Days)} {
			setPath(d, []string{"health", "icu", k}, v)
		}
	}
	if m := x.Music; m != nil {
		base, e := httpURL(m.APIBase, "音乐 API 地址")
		if e != nil {
			return e
		}
		setPath(d, []string{"music", "enabled"}, m.Enabled)
		setPath(d, []string{"music", "api_base"}, base)
	}
	if x.AppNames != nil {
		if len(x.AppNames) > 500 {
			return errors.New("应用改名最多 500 条")
		}
		names := M{}
		for k, v := range x.AppNames {
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if k == "" || v == "" {
				continue
			}
			if utf8.RuneCountInString(k) > 80 || utf8.RuneCountInString(v) > 40 {
				return errors.New("应用改名「" + runeCut(k, 20) + "」太长")
			}
			names[k] = v
		}
		d["app_names"] = names
	}
	if x.HasCategories {
		if len(x.Categories) > 30 {
			return errors.New("分类最多 30 个")
		}
		cats := []any{}
		for _, c := range x.Categories {
			name, e := limit(c.Name, 20, "分类名")
			if e != nil {
				return e
			}
			if name == "" {
				return errors.New("分类名不能为空")
			}
			if len(c.Patterns) > 20 {
				return errors.New("分类「" + name + "」的规则最多 20 条")
			}
			pats := []any{}
			for _, p := range c.Patterns {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if len(p) > 1000 {
					return errors.New("分类「" + name + "」有一条规则太长")
				}
				if _, e := regexp2.Compile(p, regexp2.Multiline); e != nil {
					return errors.New("分类「" + name + "」的规则写错了：" + runeCut(p, 40) + "（" + e.Error() + "）")
				}
				pats = append(pats, p)
			}
			cats = append(cats, M{"name": name, "patterns": pats})
		}
		d["categories"] = cats
	}
	for name, v := range p.Secrets {
		path, ok := dlSecretPaths[name]
		if !ok {
			return errors.New("不认识的密钥：" + name)
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if len(v) > 300 || strings.ContainsAny(v, "\r\n\t ") {
			return errors.New(name + " 看起来不对（有空格或太长）")
		}
		setPath(d, path, v)
	}
	for _, name := range p.Clear {
		if path, ok := dlSecretPaths[name]; ok {
			setPath(d, path, "")
		}
	}
	return nil
}

func handleDaylogSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		writeJSON(w, 200, M{"ok": true, "settings": dlSettingsView()})
		return
	}
	var p dlPatch
	if !adminPost(w, r, &p) {
		return
	}
	c := cloneConfig(conf())
	d := deepCopyM(c.Daylog).(map[string]any)
	if err := applyDlPatch(d, &p); err != nil {
		writeJSON(w, 400, M{"ok": false, "error": err.Error()})
		return
	}
	c.Daylog = normalizeDaylog(d)
	if err := commitConfig(c); err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	dlLog("设置页修改了 daylog 配置")
	writeJSON(w, 200, M{"ok": true, "settings": dlSettingsView()})
}

var _ = time.Now
