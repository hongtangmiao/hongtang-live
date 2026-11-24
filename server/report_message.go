package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

/* ---------------- Markdown（daily.to_markdown） ---------------- */

// pyFmtHMS：agg.fmt_hms
func pyFmtHMS(sec float64) string {
	s := pyRound0(sec)
	h, rem := s/3600, s%3600
	if s < 0 { // Python divmod 对负数向下取整；正常不会出现
		h, rem = int64(pyFloorDiv(float64(s), 3600)), s-int64(pyFloorDiv(float64(s), 3600))*3600
	}
	m, ss := rem/60, rem%60
	if h != 0 {
		return itoa(h) + " 小时 " + pad2(m) + " 分"
	}
	if m != 0 {
		return itoa(m) + " 分 " + pad2(ss) + " 秒"
	}
	return itoa(ss) + " 秒"
}

func getOr(o *OM, k string, def any) any {
	if v, ok := o.Get(k); ok {
		return v
	}
	return def
}

func toMarkdown(day, analysis *OM) string {
	t := day.G("totals").(*OM)
	total := pyF(t.G("active_sec"))
	if !pyTruthy(t.G("active_sec")) {
		total = 1
	}
	L := []string{
		"# " + pyStr(day.G("date")) + " " + pyStr(day.G("weekday")),
		"",
		"**在电脑前 " + pyFmtHMS(pyF(t.G("active_sec"))) + " · 最长专注 " + pyFmtHMS(pyF(t.G("longest_focus_sec"))) +
			" · 切换 " + pyStr(t.G("switches")) + " 次**",
		"",
		"- 活动区间：" + pyStr(t.G("first")) + " → " + pyStr(t.G("last")),
		"- 键盘：" + pyStr(t.G("keys")) + " 次按键 / " + pyStr(t.G("clicks")) + " 次点击",
	}
	if pyTruthy(t.G("screens")) {
		L = append(L, "- 截图："+pyStr(t.G("screens"))+" 张，OCR "+pyStr(t.G("ocr_chars"))+" 字")
	}
	L = append(L, "", "## 分类", "")
	for _, c := range omList(day.G("categories")) {
		L = append(L, "- "+pyStr(c.G("name"))+"："+pyFmtHMS(pyF(c.G("sec")))+"（"+itoa(pyRound0(pyF(c.G("sec"))/total*100))+"%）")
	}
	L = append(L, "", "## 应用", "")
	for _, a := range omList(day.G("apps")) {
		L = append(L, "- "+pyStr(a.G("name"))+"："+pyFmtHMS(pyF(a.G("sec"))))
	}
	if pyTruthy(day.G("domains")) {
		L = append(L, "", "## 网站", "")
		for _, d := range omList(day.G("domains")) {
			L = append(L, "- "+pyStr(d.G("name"))+"："+pyFmtHMS(pyF(d.G("sec"))))
		}
	}
	if pyTruthy(day.G("listened")) {
		L = append(L, "", "## 今天听过的歌", "")
		for _, x := range omList(day.G("listened")) {
			L = append(L, "- "+pyStr(x.G("title"))+" — "+pyStr(x.G("artist"))+"（"+pyStr(x.G("plays"))+" 次）")
		}
	}
	if pyTruthy(analysis) {
		L = append(L, "", "## AI 分析", "", "**"+pyStr(getOr(analysis, "headline", ""))+"**", "")
		narr := analysis.G("narrative")
		if !pyTruthy(narr) {
			narr = ""
		}
		for _, para := range strings.Split(pyStr(narr), "\n\n") {
			if p := pyStrip(para); p != "" {
				L = append(L, p, "")
			}
		}
		if h, ok := day.G("health").(*OM); ok && pyTruthy(h) {
			L = append(L, "### 身体数据", "")
			for _, k := range h.Keys() {
				L = append(L, "- "+k+"："+pyStr(h.G(k)))
			}
			L = append(L, "")
		}
		if pyTruthy(analysis.G("timeline_notes")) {
			L = append(L, "### 时间线解读", "")
			for _, n := range omList(analysis.G("timeline_notes")) {
				L = append(L, "- `"+pyStr(getOr(n, "time", ""))+"` "+pyStr(getOr(n, "what", ""))+" —— "+pyStr(getOr(n, "note", "")))
			}
			L = append(L, "")
		}
		if pyTruthy(analysis.G("patterns")) {
			L = append(L, "### 模式", "")
			for _, p := range omList(analysis.G("patterns")) {
				L = append(L, "- **"+pyStr(getOr(p, "pattern", ""))+"** "+pyStr(getOr(p, "evidence", "")))
			}
			L = append(L, "")
		}
		if pyTruthy(analysis.G("leaks")) {
			L = append(L, "### 时间黑洞", "")
			for _, x := range omList(analysis.G("leaks")) {
				L = append(L, "- "+pyStr(getOr(x, "target", ""))+"（"+itoa(int64(pyF(getOr(x, "minutes", int64(0)))))+" 分钟）："+pyStr(getOr(x, "why", "")))
			}
			L = append(L, "")
		}
		music, _ := getOr(analysis, "music", nil).(*OM)
		if music == nil {
			music = NewOM()
		}
		if pyTruthy(music.G("summary")) || pyTruthy(music.G("styles")) {
			L = append(L, "### 今天在听什么", "")
			if pyTruthy(music.G("summary")) {
				L = append(L, pyStr(music.G("summary")), "")
			}
			if pyTruthy(music.G("styles")) {
				var ss []string
				for i, x := range asA(music.G("styles")) {
					if i >= 6 {
						break
					}
					ss = append(ss, pyStr(x))
				}
				L = append(L, "曲风："+strings.Join(ss, "、"))
			}
			if pyTruthy(music.G("obsession")) {
				L = append(L, "最上头："+pyStr(music.G("obsession")))
			}
			L = append(L, "")
		}
		if pyTruthy(analysis.G("tomorrow")) {
			L = append(L, "### 明天可以试", "")
			for _, x := range asA(analysis.G("tomorrow")) {
				L = append(L, "- "+pyStr(x))
			}
			L = append(L, "")
		}
		if pyTruthy(analysis.G("question")) {
			L = append(L, "> "+pyStr(analysis.G("question")), "")
		}
	}
	return strings.Join(L, "\n") + "\n"
}

/* ---------------- Telegram（plugins/telegram.py） ---------------- */

func tgFmt(sec any) string {
	m := int64(pyF(sec)) / 60
	h, mm := m/60, m%60
	if h != 0 {
		return itoa(h) + "h" + pad2(mm) + "m"
	}
	return itoa(mm) + "m"
}

func tgDailyMessage(cfg M, day, analysis *OM) string {
	t, _ := day.G("totals").(*OM)
	if t == nil {
		t = NewOM()
	}
	title := "日报"
	if v := dget(cfg, "site", "title"); v != nil {
		title = pyStr(v)
	}
	head := pyStrip(pyStr(orV(analysis.G("headline"), "今天没有标题")))
	L := []string{
		"<b>" + pyHTMLEscape(title) + " · " + pyHTMLEscape(pyStr(day.G("date"))) + "</b>",
		"",
		"<b>" + pyHTMLEscape(head) + "</b>",
		"在电脑前 " + tgFmt(getOr(t, "active_sec", int64(0))) + " · 最长专注 " + tgFmt(getOr(t, "longest_focus_sec", int64(0))) +
			" · 切换 " + pyStr(getOr(t, "switches", int64(0))) + " 次",
	}
	cats := omList(day.G("categories"))
	if len(cats) > 3 {
		cats = cats[:3]
	}
	if len(cats) > 0 {
		var parts []string
		for _, c := range cats {
			parts = append(parts, pyStr(c.G("name"))+" "+tgFmt(c.G("sec")))
		}
		L = append(L, strings.Join(parts, " · "))
	}
	songs := omList(day.G("listened"))
	if len(songs) > 0 {
		var names []string
		for i, x := range songs {
			if i >= 3 {
				break
			}
			names = append(names, pyStr(getOr(x, "title", "")))
		}
		more := ""
		if len(songs) > 3 {
			more = fmt.Sprintf(" 等 %d 首", len(songs))
		}
		L = append(L, "🎵 "+strings.Join(names, "、")+more)
	}
	if tm := asA(analysis.G("tomorrow")); len(tm) > 0 {
		L = append(L, "", "<b>明天可以试</b>")
		for i, x := range tm {
			if i >= 3 {
				break
			}
			L = append(L, "· "+pyHTMLEscape(pyStr(x)))
		}
	}
	if q := pyStrip(pyStr(orV(analysis.G("question"), ""))); q != "" {
		L = append(L, "", "<i>"+pyHTMLEscape(q)+"</i>")
	}
	return strings.Join(L, "\n")
}

// orV：Python 的 (x or default)
func orV(v, def any) any {
	if pyTruthy(v) {
		return v
	}
	return def
}

func tgSend(cfg M, text string) bool {
	token := pyStrip(dStr(cfg, "telegram", "bot_token"))
	chat := pyStrip(dStr(cfg, "telegram", "chat_id"))
	if token == "" || chat == "" {
		dlLog("telegram：还没填 bot_token / chat_id，跳过")
		return false
	}
	form := url.Values{"chat_id": {chat}, "text": {runeCut(text, 3900)}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// 错误信息里会带 URL（含 bot token），不能原样写日志
		dlLog("telegram 发送失败：" + strings.ReplaceAll(err.Error(), token, "***"))
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	ok := resp.StatusCode == 200
	if ok {
		dlLog("telegram：已发送")
	} else {
		dlLog(fmt.Sprintf("telegram：发送失败（HTTP %d）", resp.StatusCode))
	}
	return ok
}

func tgOnDaily(cfg M, day, analysis *OM) {
	if !dTruthy(cfg, "telegram", "enabled") || !dTruthy(cfg, "telegram", "send_daily") || !pyTruthy(analysis) {
		return
	}
	if tgSend(cfg, tgDailyMessage(cfg, day, analysis)) {
		markTelegram(pyStr(day.G("date")), true)
	}
}

func tgWeeklyMessage(analysis *OM) string {
	head := pyStr(orV(analysis.G("headline"), ""))
	body := pyStr(orV(analysis.G("narrative"), ""))
	var tr []string
	for i, x := range asA(orV(analysis.G("trends"), []any{})) {
		if i >= 4 {
			break
		}
		title := ""
		if o, ok := x.(*OM); ok {
			title = pyStr(getOr(o, "title", ""))
		}
		tr = append(tr, "· "+title)
	}
	return "<b>周报</b>\n\n<b>" + pyHTMLEscape(head) + "</b>\n\n" + pyHTMLEscape(body) + "\n\n" + pyHTMLEscape(strings.Join(tr, "\n"))
}

func tgOnWeekly(cfg M, analysis *OM) {
	if !dTruthy(cfg, "telegram", "enabled") || !dTruthy(cfg, "telegram", "send_weekly") || !pyTruthy(analysis) {
		return
	}
	tgSend(cfg, tgWeeklyMessage(analysis))
}
