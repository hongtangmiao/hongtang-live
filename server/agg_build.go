package main

import (
	"math"
	"sort"
	"strings"
	"time"
)

/* ---------------- 主流程 ---------------- */

type aggSeg struct {
	s, e                    time.Time
	app, title, domain, cat string
	dur                     float64
}

type slotT struct {
	bin                     int
	t                       string
	sec                     float64
	app, title, cat, domain string
	keys                    int64
}

func fmtClockTZ(t time.Time, tz *time.Location) string { return t.In(tz).Format("15:04") }

func pyIntOf(v any) int64 {
	f, ok := pyNum(v)
	if !ok {
		if s, isS := v.(string); isS {
			f, _ = pyFloat(s)
		}
	}
	return int64(f) // int(float) 向零截断
}

func cfgInt(cfg M, def int, keys ...string) int { return dInt(cfg, def, keys...) }

// buildDay：agg.build_day；cfg 是 daylog 配置（时区用全局 loc）
func buildDay(export M, cfg M, blocklist []string) *OM {
	tz := loc
	titleMax := cfgInt(cfg, 80, "report", "title_max_chars")
	rules := newAggRules(cfg, blocklist)

	d, _ := time.Parse("2006-01-02", toStr(export["date"]))
	ds := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, tz)
	de := ds.AddDate(0, 0, 1)
	if de.Sub(ds) != 24*time.Hour { // 固定时区，不会发生；保险
		de = ds.Add(24 * time.Hour)
	}

	buckets := asM(export["buckets"])
	var roleMap M
	if buckets != nil {
		if _, ok := buckets["window"].([]any); ok {
			roleMap, buckets = buckets, M{}
		}
	}
	ids := asM(export["bucket_ids"])
	find := func(prefixes []string, role string) (string, []any) {
		if roleMap != nil {
			id := role
			if v, ok := ids[role]; ok {
				id = pyStr(v)
			}
			return id, asA(roleMap[role])
		}
		// Python 按 dict 顺序找第一个；Go 的 map 没顺序，按键排序（实际只会有一个匹配）
		keys := make([]string, 0, len(buckets))
		for k := range buckets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, bid := range keys {
			for _, p := range prefixes {
				if bid == p || strings.HasPrefix(bid, p) {
					return bid, asA(buckets[bid])
				}
			}
		}
		return "", nil
	}
	winID, winRaw := find([]string{"aw-watcher-window_", "aw-watcher-window"}, "window")
	afkID, afkRaw := find([]string{"aw-watcher-afk_", "aw-watcher-afk"}, "afk")
	webID, webRaw := find([]string{"aw-watcher-web"}, "web")
	inpID, inpRaw := find([]string{"aw-watcher-input"}, "input")

	win := normEvents(winRaw, ds, de, tz)
	afk := normEvents(afkRaw, ds, de, tz)
	web := normEvents(webRaw, ds, de, tz)
	inp := normEvents(inpRaw, ds, de, tz)

	var afkIv, activeIv []ival
	for _, x := range afk {
		status := "None"
		if v, ok := x.data["status"]; ok {
			status = pyStr(v)
		} else {
			status = ""
		}
		if strings.ToLower(status) == "afk" {
			afkIv = append(afkIv, ival{x.s, x.e})
		} else {
			activeIv = append(activeIv, ival{x.s, x.e})
		}
	}
	afkIv = mergeIntervals(afkIv)
	var covers []ival
	if len(afk) > 0 {
		covers = mergeIntervals(activeIv)
	} else {
		covers = []ival{{ds, de}}
	}

	var webSegs []webSeg
	for _, x := range web {
		u := ""
		if pyTruthy(x.data["url"]) {
			u = pyStr(x.data["url"])
		}
		if dom := pyDomain(u); dom != "" {
			webSegs = append(webSegs, webSeg{x.s, x.e, dom})
		}
	}
	sort.SliceStable(webSegs, func(i, j int) bool { return webSegs[i].s.Before(webSegs[j].s) })

	var raw []rawSeg
	for _, x := range win {
		str := func(k string) string {
			if pyTruthy(x.data[k]) {
				return pyStrip(pyStr(x.data[k]))
			}
			return ""
		}
		u := ""
		if pyTruthy(x.data["url"]) {
			u = pyStr(x.data["url"])
		}
		raw = append(raw, rawSeg{x.s, x.e, str("app"), str("title"), pyDomain(u)})
	}
	clipped := clipSegs(raw, covers)

	var segs []aggSeg
	for _, c := range clipped {
		dom := c.domain
		if dom == "" {
			dom = pickDomain(c.s, c.e, webSegs)
		}
		segs = append(segs, aggSeg{s: c.s, e: c.e, app: c.app, title: c.title, domain: dom})
	}

	var merged []*aggSeg
	for i := range segs {
		sg := segs[i]
		if n := len(merged); n > 0 {
			last := merged[n-1]
			if pySeconds(sg.s.Sub(last.e)) <= 90 && last.app == sg.app && last.title == sg.title {
				last.e = maxT(last.e, sg.e)
				continue
			}
		}
		c := sg
		merged = append(merged, &c)
	}
	for _, sg := range merged {
		if pyLen(sg.title) > titleMax {
			sg.title = runeCut(sg.title, titleMax) + "…"
		}
	}

	redacted := 0.0
	for _, sg := range merged {
		dur := pySeconds(sg.e.Sub(sg.s))
		if rules.blocked(sg.title, sg.domain) {
			redacted += dur
			sg.title = "[已屏蔽]"
			sg.domain = ""
		}
		sg.cat = rules.category(sg.app, sg.title, sg.domain)
		sg.dur = dur
	}

	// 输入强度
	inputKeys := map[int]int64{}
	var totalKeys, totalClicks int64
	for _, x := range inp {
		var keys int64
		if pyTruthy(x.data["presses"]) {
			keys = pyIntOf(x.data["presses"])
		} else if pyTruthy(x.data["keys"]) {
			keys = pyIntOf(x.data["keys"])
		}
		var clicks int64
		if pyTruthy(x.data["clicks"]) {
			clicks = pyIntOf(x.data["clicks"])
		}
		totalKeys += keys
		totalClicks += clicks
		b := int(pyFloorDiv(pySeconds(x.s.Sub(ds)), 300))
		inputKeys[b] += keys
	}

	// 5 分钟格子
	const nBins = 24 * 12
	type binKey struct{ app, title, cat, domain string }
	type binAcc struct {
		order []binKey
		m     map[binKey]float64
	}
	bins := make([]*binAcc, nBins)
	for _, sg := range merged {
		sb := int(pyFloorDiv(pySeconds(sg.s.Sub(ds)), 300))
		eb := int(pyFloorDiv(pySeconds(sg.e.Sub(ds)), 300))
		if sb < 0 {
			sb = 0
		}
		if eb > nBins-1 {
			eb = nBins - 1
		}
		for b := sb; b <= eb; b++ {
			bs := ds.Add(time.Duration(b*300) * time.Second)
			be := bs.Add(300 * time.Second)
			ov := pySeconds(minT(sg.e, be).Sub(maxT(sg.s, bs)))
			if ov <= 0 {
				continue
			}
			if bins[b] == nil {
				bins[b] = &binAcc{m: map[binKey]float64{}}
			}
			k := binKey{sg.app, sg.title, sg.cat, sg.domain}
			if _, ok := bins[b].m[k]; !ok {
				bins[b].order = append(bins[b].order, k)
				bins[b].m[k] = 0.0
			}
			bins[b].m[k] += ov
		}
	}
	var slots []slotT
	for b, acc := range bins {
		if acc == nil {
			continue
		}
		var bk binKey
		bv := 0.0
		vals := make([]float64, 0, len(acc.order))
		for i, k := range acc.order {
			if i == 0 || acc.m[k] > bv {
				bk, bv = k, acc.m[k]
			}
			vals = append(vals, acc.m[k])
		}
		total := pyNeumaier(vals) // sum(acc.values())
		slots = append(slots, slotT{bin: b, t: fmtClockTZ(ds.Add(time.Duration(b*300)*time.Second), tz), sec: total,
			app: bk.app, title: bk.title, cat: bk.cat, domain: bk.domain, keys: inputKeys[b]})
	}

	// 长时段
	type run struct {
		app, start, end string
		lastBin         int
		sec             float64
		cats            *accF
	}
	var runs []*run
	var cur *run
	for _, sl := range slots {
		end := fmtClockTZ(ds.Add(time.Duration((sl.bin+1)*300)*time.Second), tz)
		if cur != nil && sl.app == cur.app && sl.bin-cur.lastBin <= 1 {
			cur.lastBin, cur.end = sl.bin, end
			cur.sec += sl.sec
			cur.cats.add(sl.cat, sl.sec)
		} else {
			if cur != nil {
				runs = append(runs, cur)
			}
			cur = &run{app: sl.app, start: sl.t, end: end, lastBin: sl.bin, sec: sl.sec, cats: newAccF()}
			cur.cats.add(sl.cat, sl.sec)
		}
	}
	if cur != nil {
		runs = append(runs, cur)
	}
	var sess []*run
	for _, r := range runs {
		if r.sec >= 15*60 {
			sess = append(sess, r)
		}
	}
	sort.SliceStable(sess, func(i, j int) bool { return -sess[i].sec < -sess[j].sec })

	// 汇总
	durs := make([]float64, 0, len(merged))
	for _, sg := range merged {
		durs = append(durs, sg.dur)
	}
	totalActive := pySumF(durs)
	afkDurs := []float64{}
	for _, x := range afkIv {
		afkDurs = append(afkDurs, pySeconds(x.e.Sub(x.s)))
	}
	totalAFK := pySumF(afkDurs)
	byApp, byCat, byDom := newAccF(), newAccF(), newAccF()
	titles := map[string]*accF{}
	var titleApps []string
	for _, sg := range merged {
		app := sg.app
		if app == "" {
			app = "未知"
		}
		byApp.add(app, sg.dur)
		byCat.add(sg.cat, sg.dur)
		if sg.domain != "" {
			byDom.add(sg.domain, sg.dur)
		}
		if sg.title != "" {
			if titles[app] == nil {
				titles[app] = newAccF()
				titleApps = append(titleApps, app)
			}
			titles[app].add(sg.title, sg.dur)
		}
	}

	switches := int64(0)
	var prevKey [2]string
	hasPrev := false
	var prevEnd time.Time
	for _, sg := range merged {
		k := [2]string{sg.app, sg.title}
		if hasPrev && k != prevKey && pySeconds(sg.s.Sub(prevEnd)) <= 300 {
			switches++
		}
		prevKey, prevEnd, hasPrev = k, sg.e, true
	}

	focus := map[string]bool{"写代码": true, "创作": true, "读资料": true}
	longest, curF := 0.0, 0.0
	prevBin := -1
	for _, sl := range slots {
		isF := focus[sl.cat]
		if isF && (prevBin < 0 || sl.bin-prevBin <= 1) {
			curF += sl.sec
		} else if isF {
			curF = sl.sec
		} else {
			longest = math.Max(longest, curF)
			if curF > longest {
				longest = curF
			}
			curF = 0.0
		}
		prevBin = sl.bin
	}
	if curF > longest {
		longest = curF
	}

	var spanIv []ival
	for _, sg := range merged {
		spanIv = append(spanIv, ival{sg.s, sg.e})
	}
	spans := mergeIntervals(spanIv)
	covDurs := []float64{}
	for _, x := range spans {
		covDurs = append(covDurs, pySeconds(x.e.Sub(x.s)))
	}
	covered := pySumF(covDurs)
	type gapT struct {
		start, end string
		sec        float64
	}
	var gaps []gapT
	for i := 1; i < len(spans); i++ {
		g := pySeconds(spans[i].s.Sub(spans[i-1].e))
		if g >= 300 {
			gaps = append(gaps, gapT{fmtClockTZ(spans[i-1].e, tz), fmtClockTZ(spans[i].s, tz), g})
		}
	}
	sort.SliceStable(gaps, func(i, j int) bool { return -gaps[i].sec < -gaps[j].sec })

	var unknownD, noTitleD []float64
	for _, sg := range merged {
		switch strings.ToLower(sg.app) {
		case "", "unknown", "未知":
			unknownD = append(unknownD, sg.dur)
		}
		switch strings.ToLower(sg.title) {
		case "", "untitled", "<no title>", "无标题":
			noTitleD = append(noTitleD, sg.dur)
		}
	}

	activeF := pyF(totalActive)
	var sph any = int64(0)
	if activeF > 60 {
		sph = float64(switches) / (activeF / 3600)
	}
	first, last := "", ""
	coverage := 0.0
	if len(spans) > 0 {
		first, last = fmtClockTZ(spans[0].s, tz), fmtClockTZ(spans[len(spans)-1].e, tz)
		coverage = activeF / math.Max(1.0, pySeconds(spans[len(spans)-1].e.Sub(spans[0].s)))
	}

	cats := []any{}
	for _, x := range byCat.sortedDesc() {
		if x.v > 0 {
			cats = append(cats, NewOM("name", x.k, "sec", x.v))
		}
	}
	apps := []any{}
	appList := byApp.sortedDesc()
	if n := cfgInt(cfg, 15, "report", "top_apps"); len(appList) > n {
		appList = appList[:maxInt(n, 0)]
	}
	for _, x := range appList {
		if x.v <= 0 {
			continue
		}
		ts := []any{}
		if a := titles[x.k]; a != nil {
			for i, t := range a.sortedDesc() {
				if i >= 3 {
					break
				}
				ts = append(ts, NewOM("title", t.k, "sec", t.v))
			}
		}
		apps = append(apps, NewOM("name", x.k, "sec", x.v, "titles", ts))
	}
	doms := []any{}
	domList := byDom.sortedDesc()
	if n := cfgInt(cfg, 15, "report", "top_domains"); len(domList) > n {
		domList = domList[:maxInt(n, 0)]
	}
	for _, x := range domList {
		doms = append(doms, NewOM("name", x.k, "sec", x.v))
	}
	sessOut := []any{}
	for i, r := range sess {
		if i >= cfgInt(cfg, 25, "report", "max_sessions") {
			break
		}
		ck, _ := r.cats.firstMax()
		sessOut = append(sessOut, NewOM("app", r.app, "start", r.start, "end", r.end, "sec", r.sec, "cat", ck))
	}
	slotsOut := []any{}
	for _, s := range slots {
		slotsOut = append(slotsOut, NewOM("bin", int64(s.bin), "t", s.t, "sec", s.sec, "app", s.app, "title", s.title,
			"cat", s.cat, "domain", s.domain, "keys", s.keys))
	}
	gapsOut := []any{}
	for i, g := range gaps {
		if i >= 8 {
			break
		}
		gapsOut = append(gapsOut, NewOM("start", g.start, "end", g.end, "sec", g.sec))
	}

	str := func(k string) any {
		if v, ok := export[k]; ok {
			return pyJSONVal(v)
		}
		return ""
	}
	wd := []string{"一", "二", "三", "四", "五", "六", "日"}[(int(d.Weekday())+6)%7]
	return NewOM(
		"date", toStr(export["date"]),
		"weekday", "周"+wd,
		"hostname", str("hostname"),
		"exported_at", str("exported_at"),
		"totals", NewOM("active_sec", totalActive, "afk_sec", totalAFK, "covered_sec", covered, "switches", switches,
			"switches_per_hour", sph, "longest_focus_sec", longest, "keys", totalKeys, "clicks", totalClicks,
			"first", first, "last", last, "screens", int64(0), "ocr_chars", int64(0)),
		"categories", cats,
		"apps", apps,
		"domains", doms,
		"sessions", sessOut,
		"slots", slotsOut,
		"gaps", gapsOut,
		"screens", []any{},
		"quality", NewOM("unknown_app_sec", pySumF(unknownD), "no_title_sec", pySumF(noTitleD), "redacted_sec", redacted,
			"coverage", coverage, "buckets_found", NewOM("window", winID, "afk", afkID, "web", webID, "input", inpID)),
	)
}

// pyJSONVal：json.Unmarshal 出来的值（float64 数字）转成 Python 语义（整数值的 JSON 数字是 int）
func pyJSONVal(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return int64(t)
		}
		return t
	case map[string]any:
		return t
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
