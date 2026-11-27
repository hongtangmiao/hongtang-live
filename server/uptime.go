package main

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	uptimeCacheTTL   = 30 * time.Second
	uptimeRefreshMin = 5 * time.Second
	uptimeMaxBeats   = 30
)

type uptimeBeat struct {
	S int     `json:"s"` // 0=down, 1=up, 2=pending, 3=maintenance
	P float64 `json:"p"` // ping ms
	T string  `json:"t"` // timestamp
}

type uptimeMonitor struct {
	ID       int          `json:"id"`
	Name     string       `json:"name"`
	Status   int          `json:"status"`
	Ping     *float64     `json:"ping"`
	Uptime24 *float64     `json:"uptime24"`
	Msg      string       `json:"msg,omitempty"`
	Time     string       `json:"time,omitempty"`
	Beats    []uptimeBeat `json:"beats"`
}

type uptimeGroup struct {
	ID       int             `json:"id"`
	Name     string          `json:"name"`
	Monitors []uptimeMonitor `json:"monitors"`
}

var (
	uptimeMu      sync.Mutex
	uptimeData    M
	uptimeAt      time.Time
	uptimeLastErr string
)

func resetUptimeCache() {
	uptimeMu.Lock()
	uptimeData = nil
	uptimeAt = time.Time{}
	uptimeLastErr = ""
	uptimeMu.Unlock()
}

// parseUptimeTarget 允许用户把根地址（http://uptime-kuma:3001）或完整状态页链接（.../status/web）填进 url
func parseUptimeTarget(rawURL, slug string) (baseURL, resolvedSlug string) {
	u := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	s := strings.Trim(strings.TrimSpace(slug), "/")
	if idx := strings.Index(u, "/status/"); idx > 0 {
		tail := strings.Trim(u[idx+len("/status/"):], "/")
		u = u[:idx]
		if s == "" && tail != "" {
			s = strings.SplitN(tail, "/", 2)[0]
		}
	}
	if s == "" {
		s = "web"
	}
	return u, s
}

func uptimeReady() bool {
	c := conf()
	return strings.TrimSpace(c.Uptime.URL) != ""
}

func uptimeStatus() M {
	c := conf()
	ready := strings.TrimSpace(c.Uptime.URL) != ""
	out := M{"ready": ready}
	uptimeMu.Lock()
	d, at, lastErr := uptimeData, uptimeAt, uptimeLastErr
	uptimeMu.Unlock()
	if d != nil {
		if sum := asM(d["summary"]); sum != nil {
			out["summary"] = sum
		}
		if !at.IsZero() {
			out["updated"] = isoUTC(at)
		}
		out["ok"] = d["ok"]
	}
	if lastErr != "" {
		out["error"] = lastErr
	}
	return out
}

func handleUptime(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, 200, getUptime(force))
}

func getUptime(force bool) M {
	c := conf()
	baseURL, slug := parseUptimeTarget(c.Uptime.URL, c.Uptime.Slug)
	pubURL := strings.TrimSpace(c.Uptime.PublicURL)
	if baseURL == "" {
		return M{
			"ok":         false,
			"configured": false,
			"publicUrl":  pubURL,
			"error":      "uptime kuma not configured",
		}
	}

	uptimeMu.Lock()
	if uptimeData != nil {
		age := time.Since(uptimeAt)
		if (!force && age < uptimeCacheTTL) || (force && age < uptimeRefreshMin) {
			d := uptimeData
			uptimeMu.Unlock()
			return d
		}
	}
	prev := uptimeData
	uptimeMu.Unlock()

	fresh, err := fetchUptimeData(baseURL, slug, pubURL)
	uptimeMu.Lock()
	defer uptimeMu.Unlock()
	if err != nil {
		uptimeLastErr = err.Error()
		if prev != nil {
			return prev
		}
		return M{
			"ok":         false,
			"configured": true,
			"publicUrl":  pubURL,
			"error":      err.Error(),
		}
	}
	uptimeData = fresh
	uptimeAt = time.Now()
	uptimeLastErr = ""
	return fresh
}

func fetchUptimeData(baseURL, slug, pubURL string) (M, error) {
	escSlug := url.PathEscape(slug)
	pageURL := baseURL + "/api/status-page/" + escSlug
	beatURL := baseURL + "/api/status-page/heartbeat/" + escSlug
	hdr := map[string]string{"User-Agent": "hongtang-live/1.0"}

	var (
		pageData M
		beatData M
		pageErr  error
		beatErr  error
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		pageData, pageErr = fetchJSONOnce(pageURL, hdr, 8*time.Second)
	}()
	go func() {
		defer wg.Done()
		beatData, beatErr = fetchJSONOnce(beatURL, hdr, 8*time.Second)
	}()
	wg.Wait()

	if pageErr != nil {
		return nil, errors.New("status-page: " + pageErr.Error())
	}
	if beatErr != nil {
		return nil, errors.New("heartbeat: " + beatErr.Error())
	}

	cfgM := asM(pageData["config"])
	title := ""
	desc := ""
	if cfgM != nil {
		title = strings.TrimSpace(toStr(cfgM["title"]))
		desc = strings.TrimSpace(toStr(cfgM["description"]))
	}

	hbMap := asM(beatData["heartbeatList"])
	upMap := asM(beatData["uptimeList"])

	rawGroups := asA(pageData["publicGroupList"])
	type weightedGroup struct {
		weight int
		g      uptimeGroup
	}
	var wgroups []weightedGroup
	total, upCnt, downCnt, pendCnt, maintCnt := 0, 0, 0, 0, 0

	for _, rawG := range rawGroups {
		gm := asM(rawG)
		if gm == nil {
			continue
		}
		g := uptimeGroup{
			ID:       int(toNum(gm["id"])),
			Name:     strings.TrimSpace(toStr(gm["name"])),
			Monitors: []uptimeMonitor{},
		}
		for _, rawM := range asA(gm["monitorList"]) {
			mm := asM(rawM)
			if mm == nil {
				continue
			}
			mid := int(toNum(mm["id"]))
			key := strconv.Itoa(mid)
			m := uptimeMonitor{
				ID:     mid,
				Name:   strings.TrimSpace(toStr(mm["name"])),
				Status: 2, // 默认 pending（尚无心跳时）
				Beats:  []uptimeBeat{},
			}

			if hbMap != nil {
				rawBeats := asA(hbMap[key])
				if len(rawBeats) > uptimeMaxBeats {
					rawBeats = rawBeats[len(rawBeats)-uptimeMaxBeats:]
				}
				for _, rb := range rawBeats {
					bm := asM(rb)
					if bm == nil {
						continue
					}
					st := int(toNum(bm["status"]))
					ping := toNum(bm["ping"])
					tm := strings.TrimSpace(toStr(bm["time"]))
					m.Beats = append(m.Beats, uptimeBeat{S: st, P: ping, T: tm})
				}
				if len(m.Beats) > 0 {
					last := m.Beats[len(m.Beats)-1]
					m.Status = last.S
					m.Time = last.T
					if rawLast := asM(rawBeats[len(rawBeats)-1]); rawLast != nil {
						if rawLast["ping"] != nil {
							pv := toNum(rawLast["ping"])
							m.Ping = &pv
						}
						m.Msg = strings.TrimSpace(toStr(rawLast["msg"]))
					}
				}
			}

			if upMap != nil {
				if v, ok := upMap[key+"_24"]; ok && v != nil {
					uv := toNum(v)
					m.Uptime24 = &uv
				}
			}

			total++
			switch m.Status {
			case 1:
				upCnt++
			case 0:
				downCnt++
			case 3:
				maintCnt++
			default:
				pendCnt++
			}
			g.Monitors = append(g.Monitors, m)
		}
		wgroups = append(wgroups, weightedGroup{weight: int(toNum(gm["weight"])), g: g})
	}

	sort.SliceStable(wgroups, func(i, j int) bool {
		return wgroups[i].weight < wgroups[j].weight
	})
	groups := make([]uptimeGroup, 0, len(wgroups))
	for _, wg := range wgroups {
		groups = append(groups, wg.g)
	}

	overall := "empty"
	if total > 0 {
		switch {
		case downCnt == total:
			overall = "down"
		case downCnt > 0 || pendCnt > 0:
			overall = "degraded"
		case maintCnt == total:
			overall = "maintenance"
		default:
			overall = "up"
		}
	}

	var incident any
	if inc := asM(pageData["incident"]); inc != nil {
		t := strings.TrimSpace(toStr(inc["title"]))
		body := strings.TrimSpace(toStr(inc["content"]))
		if t != "" || body != "" {
			incident = M{
				"title":   t,
				"content": body,
				"style":   strings.TrimSpace(toStr(inc["style"])),
				"updated": strings.TrimSpace(or(toStr(inc["lastUpdatedDate"]), toStr(inc["createdDate"]))),
			}
		}
	}

	return M{
		"ok":          true,
		"configured":  true,
		"title":       title,
		"description": desc,
		"publicUrl":   pubURL,
		"updated":     isoUTC(time.Now()),
		"summary": M{
			"total":       total,
			"up":          upCnt,
			"down":        downCnt,
			"pending":     pendCnt,
			"maintenance": maintCnt,
			"overall":     overall,
		},
		"incident": incident,
		"groups":   groups,
	}, nil
}
