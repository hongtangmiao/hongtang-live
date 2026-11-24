package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

/*
 * ActivityWatch：本机 http://127.0.0.1:5600。
 *   - 每天导出一份（格式和原来的 windows/export_day.ps1 一样），交给服务器写日报
 *   - 浏览器在前台时，从 aw-watcher-web 拿最近的网址
 *   - Wayland 下拿不到前台窗口时，用 aw-watcher-window 最近一条兜底
 */

type AW struct {
	Base string
	HC   *http.Client
}

func newAW(base string) *AW {
	return &AW{Base: strings.TrimRight(or(base, "http://127.0.0.1:5600"), "/"), HC: &http.Client{Timeout: 60 * time.Second}}
}

func (a *AW) get(p string, out any) error {
	resp, err := a.HC.Get(a.Base + p)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("ActivityWatch HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 512<<20)).Decode(out)
}

type awInfo struct {
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
}

func (a *AW) Info() (awInfo, error) {
	var i awInfo
	err := a.get("/api/0/info", &i)
	return i, err
}

func (a *AW) bucketIDs() ([]string, error) {
	var b map[string]json.RawMessage
	if err := a.get("/api/0/buckets/", &b); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(b))
	for k := range b {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return ids, nil
}

// findBucket：和 export_day.ps1 的 Find-Bucket 一样：按模式顺序找，同一模式取名字最短的
func findBucket(ids []string, patterns ...string) string {
	for _, p := range patterns {
		var hits []string
		for _, id := range ids {
			if ok, _ := path.Match(p, id); ok {
				hits = append(hits, id)
			}
		}
		if len(hits) > 0 {
			sort.SliceStable(hits, func(i, j int) bool { return len(hits[i]) < len(hits[j]) })
			return hits[0]
		}
	}
	return ""
}

func (a *AW) events(bid string, start, end time.Time, limit int) ([]json.RawMessage, error) {
	if bid == "" {
		return []json.RawMessage{}, nil
	}
	q := url.Values{}
	if !start.IsZero() {
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
	}
	q.Set("limit", fmt.Sprint(limit))
	var evs []json.RawMessage
	err := a.get("/api/0/buckets/"+url.PathEscape(bid)+"/events?"+q.Encode(), &evs)
	if evs == nil {
		evs = []json.RawMessage{}
	}
	return evs, err
}

type awExport struct {
	Schema     int                          `json:"schema"`
	Hostname   string                       `json:"hostname"`
	Date       string                       `json:"date"`
	ExportedAt string                       `json:"exported_at"`
	AWVersion  string                       `json:"aw_version"`
	Counts     map[string]int               `json:"counts"`
	BucketIDs  map[string]string            `json:"bucket_ids"`
	Buckets    map[string][]json.RawMessage `json:"buckets"`
	Screens    []any                        `json:"screens"`
}

// ExportDay：导出本地时间某一天（00:00 ~ 次日 00:00）
func (a *AW) ExportDay(day time.Time) ([]byte, map[string]int, error) {
	info, err := a.Info()
	if err != nil {
		return nil, nil, fmt.Errorf("连不上 ActivityWatch（%s）：%w", a.Base, err)
	}
	ids, err := a.bucketIDs()
	if err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("ActivityWatch 里还没有任何记录")
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)
	roles := []struct {
		role string
		pats []string
	}{
		{"window", []string{"aw-watcher-window_*", "aw-watcher-window"}},
		{"afk", []string{"aw-watcher-afk_*", "aw-watcher-afk"}},
		{"web", []string{"aw-watcher-web*"}},
		{"input", []string{"aw-watcher-input*"}},
	}
	ex := awExport{Schema: 1, Hostname: or(info.Hostname, hostnameClean()), Date: start.Format("2006-01-02"),
		ExportedAt: time.Now().Format("2006-01-02T15:04:05-07:00"), AWVersion: info.Version,
		Counts: map[string]int{}, BucketIDs: map[string]string{}, Buckets: map[string][]json.RawMessage{}, Screens: []any{}}
	for _, r := range roles {
		bid := findBucket(ids, r.pats...)
		evs, err := a.events(bid, start, end, -1)
		if err != nil {
			logf("ActivityWatch %s 读失败：%v", bid, err)
			evs = []json.RawMessage{}
		}
		ex.BucketIDs[r.role], ex.Buckets[r.role], ex.Counts[r.role] = bid, evs, len(evs)
	}
	ex.Counts["screens"] = 0
	b, err := json.Marshal(ex)
	return b, ex.Counts, err
}

type awEvent struct {
	Timestamp string         `json:"timestamp"`
	Duration  float64        `json:"duration"`
	Data      map[string]any `json:"data"`
}

// latest：某类 bucket 的最近一条，太旧（结束时间超过 maxAge）就不要
func (a *AW) latest(pattern string, maxAge time.Duration) (map[string]any, bool) {
	hc := *a.HC
	hc.Timeout = 3 * time.Second
	ac := &AW{Base: a.Base, HC: &hc}
	ids, err := ac.bucketIDs()
	if err != nil {
		return nil, false
	}
	bid := findBucket(ids, pattern)
	if bid == "" {
		return nil, false
	}
	raw, err := ac.events(bid, time.Time{}, time.Time{}, 1)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var ev awEvent
	if json.Unmarshal(raw[0], &ev) != nil {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
	if err != nil {
		return nil, false
	}
	endT := t.Add(time.Duration(ev.Duration * float64(time.Second)))
	if time.Since(endT) > maxAge {
		return nil, false
	}
	return ev.Data, true
}

func (a *AW) LatestURL() string {
	d, ok := a.latest("aw-watcher-web*", 150*time.Second)
	if !ok {
		return ""
	}
	u, _ := d["url"].(string)
	return u
}

func (a *AW) LatestWindow() (string, string, bool) {
	d, ok := a.latest("aw-watcher-window*", 90*time.Second)
	if !ok {
		return "", "", false
	}
	app, _ := d["app"].(string)
	title, _ := d["title"].(string)
	return app, title, app != ""
}
