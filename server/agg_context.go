package main

/* ---------------- 给模型看的上下文（agg.build_context） ---------------- */

func omList(v any) []*OM {
	var out []*OM
	for _, x := range asA(v) {
		if o, ok := x.(*OM); ok {
			out = append(out, o)
		}
	}
	return out
}

func buildContext(day *OM, cfg M) *OM {
	t := day.G("totals").(*OM)
	active := t.G("active_sec")
	activeF := pyF(active)
	if !pyTruthy(active) {
		activeF = 1.0
	}
	pct := func(sec any) int64 { return pyRound0(pyF(sec) / activeF * 100) }
	h := func(sec any) any { return pyRoundN(pyF(sec)/3600, 2) }

	cats := []any{}
	for _, c := range omList(day.G("categories")) {
		cats = append(cats, NewOM("name", c.G("name"), "h", h(c.G("sec")), "pct", pct(c.G("sec"))))
	}
	apps := []any{}
	for _, a := range omList(day.G("apps")) {
		tt := []any{}
		for _, x := range omList(a.G("titles")) {
			tt = append(tt, runeCut(toStr(x.G("title")), 60))
		}
		apps = append(apps, NewOM("name", a.G("name"), "h", h(a.G("sec")), "pct", pct(a.G("sec")), "top_titles", tt))
	}
	doms := []any{}
	for _, x := range omList(day.G("domains")) {
		doms = append(doms, NewOM("name", x.G("name"), "h", h(x.G("sec")), "pct", pct(x.G("sec"))))
	}
	var rows []any
	for _, s := range omList(day.G("slots")) {
		app := toStr(s.G("app"))
		if app == "" {
			app = "未知"
		}
		label := toStr(s.G("t")) + " " + toStr(s.G("cat")) + " | " + app
		if dm := toStr(s.G("domain")); dm != "" {
			label += " | " + dm
		}
		if ti := toStr(s.G("title")); ti != "" {
			label += " | " + runeCut(ti, 70)
		}
		label += " (" + itoa(pyRound0(pyF(s.G("sec"))/60)) + "m"
		if k := pyF(s.G("keys")); k != 0 {
			label += ", " + pyStr(s.G("keys")) + "键"
		}
		label += ")"
		rows = append(rows, label)
	}
	maxRows := cfgInt(cfg, 300, "report", "max_timeline_rows")
	if len(rows) > maxRows && maxRows > 0 {
		step := float64(len(rows)) / float64(maxRows)
		pick := make([]any, 0, maxRows)
		for i := 0; i < maxRows; i++ {
			pick = append(pick, rows[int(float64(i)*step)])
		}
		rows = pick
	}
	if rows == nil {
		rows = []any{}
	}
	sess := []any{}
	for _, s := range omList(day.G("sessions")) {
		sess = append(sess, NewOM("start", s.G("start"), "end", s.G("end"), "app", s.G("app"), "cat", s.G("cat"),
			"min", pyRound0(pyF(s.G("sec"))/60)))
	}
	gaps := []any{}
	for _, g := range omList(day.G("gaps")) {
		gaps = append(gaps, NewOM("from", g.G("start"), "to", g.G("end"), "min", pyRound0(pyF(g.G("sec"))/60)))
	}
	q := day.G("quality").(*OM)
	ctx := NewOM(
		"date", day.G("date"), "weekday", day.G("weekday"), "hostname", day.G("hostname"),
		"totals", NewOM("active_h", h(active), "afk_h", h(t.G("afk_sec")), "covered_h", h(t.G("covered_sec")),
			"first_activity", t.G("first"), "last_activity", t.G("last"),
			"switches_per_hour", pyRoundN(t.G("switches_per_hour"), 1),
			"longest_focus_min", pyRound0(pyF(t.G("longest_focus_sec"))/60), "keys", t.G("keys"), "clicks", t.G("clicks")),
		"categories", cats, "apps", apps, "domains", doms, "sessions", sess, "timeline", rows, "gaps", gaps,
		"screens_count", t.G("screens"), "screens", []any{},
		"quality", NewOM("unknown_app_pct", pct(q.G("unknown_app_sec")), "no_title_pct", pct(q.G("no_title_sec")),
			"redacted_pct", pct(q.G("redacted_sec")), "screenshot_enabled", false),
	)
	blob := pyDumps(ctx, -1)
	limit := cfgInt(cfg, 26000, "report", "max_payload_chars")
	if n := pyLen(blob); n > limit {
		tl := asA(ctx.G("timeline"))
		keep := maxInt(40, int(float64(len(tl))*float64(limit)/float64(n)))
		if keep < len(tl) {
			tl = tl[:keep]
		}
		ctx.Set("timeline", tl)
		blob = pyDumps(ctx, -1)
	}
	ctx.Set("_payload_chars", int64(pyLen(blob)))
	return ctx
}
