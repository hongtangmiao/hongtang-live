package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

/* ---------------- Intervals.icu ---------------- */

func icuSync() int {
	cfg := dlCfg()
	if !dTruthy(cfg, "health", "icu", "enabled") {
		return 0
	}
	key, athlete := pyStrip(dStr(cfg, "health", "icu", "api_key")), pyStrip(dStr(cfg, "health", "icu", "athlete_id"))
	if key == "" || athlete == "" {
		return 0
	}
	days := dInt(cfg, 14, "health", "icu", "days")
	if days == 0 {
		days = 14
	}
	tz := loc
	newest := time.Now().In(tz)
	oldest := newest.AddDate(0, 0, -days)
	u := "https://intervals.icu/api/v1/athlete/" + url.PathEscape(athlete) + "/wellness?oldest=" + oldest.Format("2006-01-02") +
		"&newest=" + newest.Format("2006-01-02")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("API_KEY:"+key)))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", browserUA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		dlLog("Intervals.icu 拉取失败：" + err.Error())
		return 0
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		dlLog(fmt.Sprintf("Intervals.icu 拉取失败：HTTP %d", resp.StatusCode))
		return 0
	}
	var recs []any
	if json.Unmarshal(b, &recs) != nil {
		dlLog("Intervals.icu 拉取失败：返回不是列表")
		return 0
	}
	written := 0
	for _, x := range recs {
		rec := asM(x)
		day := toStr(rec["id"])
		if !reDay.MatchString(day) {
			continue
		}
		d, _ := time.Parse("2006-01-02", day)
		ts := isoSec(time.Date(d.Year(), d.Month(), d.Day(), 8, 0, 0, 0, tz))
		for metric, v := range icuToMetrics(rec) {
			if addHealth(metric, ts, v, "") == nil {
				written++
			}
		}
	}
	if written > 0 {
		dlLog(fmt.Sprintf("Intervals.icu 同步：%d 条指标", written))
	}
	return written
}

/* ---------------- 清理 ---------------- */

func pruneLive(keepDays int) {
	db, err := dlWriter()
	if err != nil {
		return
	}
	cutoff := time.Now().In(loc).AddDate(0, 0, -keepDays).Format("2006-01-02")
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, _ = db.Exec("DELETE FROM live WHERE day < ?", cutoff)
}

func pruneHealthLive(keepDays int) {
	db, err := dlWriter()
	if err != nil {
		return
	}
	cutoff := isoSec(time.Now().In(loc).AddDate(0, 0, -keepDays))
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, _ = db.Exec("DELETE FROM health_metrics WHERE metric = 'heart_rate_live' AND ts < ?", cutoff)
}

/* ---------------- 定时任务（scheduler_loop） ---------------- */

var weekdayAbbr = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// pyWeekNum：strftime("%W")（以周一为一周开始，第一个周一之前是第 0 周）
func pyWeekNum(t time.Time) int {
	yd := t.YearDay() - 1
	wd := (int(t.Weekday()) + 6) % 7
	return (yd + 7 - wd) / 7
}

func startScheduler() {
	go func() {
		done := map[string]bool{}
		var icuLast time.Time
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						dlLog(fmt.Sprintf("调度线程出错：%v", r))
					}
				}()
				if !conf().DaylogWriter {
					return // 原 daylog 容器还在负责的时候，这边什么都不做
				}
				reloadStatusTextsIfChanged()
				cfg := dlCfg()
				now := time.Now().In(loc)
				hm := now.Format("15:04")
				if hm == or(dStr(cfg, "analysis", "daily_time"), "00:10") {
					k := "d:" + now.Format("2006-01-02")
					if !done[k] {
						done[k] = true
						dlLog("定时任务：处理 inbox")
						runLock.Lock()
						runPending(false, true)
						runLock.Unlock()
						pruneLive(90)
						pruneHealthLive(30)
					}
				}
				if parts := strings.Fields(or(dStr(cfg, "analysis", "weekly_time"), "Mon 00:40")); len(parts) == 2 {
					if hm == parts[1] && weekdayAbbr[now.Weekday()] == parts[0] {
						k := fmt.Sprintf("w:%d-%02d", now.Year(), pyWeekNum(now))
						if !done[k] {
							done[k] = true
							dlLog("定时任务：周报")
							runLock.Lock()
							runWeekly()
							runLock.Unlock()
						}
					}
				}
				if dTruthy(cfg, "health", "icu", "enabled") {
					every := time.Duration(maxInt(5, dInt(cfg, 30, "health", "icu", "poll_min"))) * time.Minute
					if time.Since(icuLast) > every {
						icuLast = time.Now()
						icuSync()
					}
				}
				if len(done) > 50 {
					done = map[string]bool{}
				}
			}()
			time.Sleep(20 * time.Second)
		}
	}()
}
