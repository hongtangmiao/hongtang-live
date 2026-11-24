package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPyRepr(t *testing.T) {
	for f, want := range map[float64]string{2: "2.0", 0.1: "0.1", 1e-05: "1e-05", 1e16: "1e+16", 1.5e-7: "1.5e-07", 0.0001: "0.0001",
		21978.123: "21978.123", 123456789012345.6: "123456789012345.6", 1234567890123456.0: "1234567890123456.0", -0.5: "-0.5", 3.14e100: "3.14e+100"} {
		eq(t, fmt.Sprint(f), pyRepr(f), want)
	}
	eq(t, "round 2", pyDumps(pyRoundN(2.675, 2), -1), "2.67") // 2.675 实际是 2.67499999…
	eq(t, "round int", pyDumps(pyRoundN(int64(0), 1), -1), "0")
	eq(t, "dumps", pyDumps(NewOM("a", []any{1.0, int64(2), "x\"\n"}, "b", NewOM(), "c", []any{}), -1), `{"a": [1.0, 2, "x\"\n"], "b": {}, "c": []}`)
	eq(t, "dumps indent", pyDumps(NewOM("a", []any{int64(1)}, "b", "中"), 1), "{\n \"a\": [\n  1\n ],\n \"b\": \"中\"\n}")
	eq(t, "floordiv", pyFloorDiv(599.9999, 300), 1.0)
	eq(t, "netloc", pyDomain("https://WWW.Bilibili.com/x"), "bilibili.com")
	eq(t, "netloc 无 scheme", pyDomain("bilibili.com/x"), "")
	eq(t, "netloc 坏 IPv6", pyDomain("http://[::1/x"), "")
	eq(t, "str(dict)", pyStr(NewOM("time", "9:30", "n", int64(1), "f", 2.0, "x", nil, "l", []any{true, "it's"})),
		`{'time': '9:30', 'n': 1, 'f': 2.0, 'x': None, 'l': [True, "it's"]}`)
	eq(t, "repr 不可打印", pyStrRepr("a\u3000b\u200b\x01中"), `'a\u3000b\u200b\x01中'`)
	eq(t, "sum 补偿求和", pyNeumaier([]float64{0.1, 0.2, 0.3}), 0.6)
}

func TestRepairJSON(t *testing.T) {
	cases := map[string]string{
		`{"headline": "abc", "narrative": "第一段`:       "abc",
		"```json\n{\"headline\": \"x\"}\n```":         "x",
		`好的：{"headline": "y", "patterns": [{"a": 1},`: "y",
		`{"headline": "z", "leaks":`:                  "z",
		`{"headline": "w"} 多余的话 }`:                    "w",
	}
	for in, want := range cases {
		o := parseJSONLoose(in)
		if o == nil {
			t.Errorf("救不回来: %q", in)
			continue
		}
		eq(t, in, pyStr(o.G("headline")), want)
	}
	if parseJSONLoose("完全不是 JSON") != nil {
		t.Error("非 JSON 应返回 nil")
	}
}

func setupRoot(t *testing.T) string {
	setupDL(t, false)
	root := t.TempDir()
	for _, d := range []string{"inbox", "raw", "reports", "logs"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	c := cloneConfig(conf())
	c.DaylogRoot = root
	setConfig(c)
	return root
}

func sampleExport(day string, hours int) M {
	tz := loc
	d, _ := time.Parse("2006-01-02", day)
	base := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, tz)
	ts := func(min int) string {
		return base.Add(time.Duration(min) * time.Minute).UTC().Format("2006-01-02T15:04:05.000000+00:00")
	}
	var win []any
	apps := []string{"Code.exe", "chrome.exe", "StarRail.exe"}
	for i := 0; i < hours*6; i++ {
		win = append(win, M{"timestamp": ts(i * 10), "duration": 600.0, "data": M{"app": apps[i/6%3], "title": fmt.Sprintf("t%d", i/3)}})
	}
	return M{"date": day, "hostname": "pc", "buckets": M{
		"window": win,
		"afk":    []any{M{"timestamp": ts(0), "duration": float64(hours * 3600), "data": M{"status": "not-afk"}}},
		"input":  []any{M{"timestamp": ts(0), "duration": 20.0, "data": M{"presses": 120.0, "clicks": 8.0}}},
		"web":    []any{},
	}}
}

func TestKeysAndBuildDay(t *testing.T) {
	setupDL(t, false)
	day := buildDay(sampleExport("2026-10-28", 3), dlCfg(), nil)
	tt := day.G("totals").(*OM)
	eq(t, "presses 计入按键", tt.G("keys"), int64(120))
	eq(t, "clicks", tt.G("clicks"), int64(8))
	eq(t, "活跃 3 小时", tt.G("active_sec"), 10800.0)
	eq(t, "本地时间的 first", tt.G("first"), "09:00")
	eq(t, "本地时间的 last", tt.G("last"), "12:00")
	eq(t, "周几", day.G("weekday"), "周三")
	sess := omList(day.G("sessions"))
	if len(sess) != 3 || sess[0].G("start") != "09:00" {
		t.Errorf("长时段: %s", pyDumps(day.G("sessions"), -1))
	}
	ctx := buildContext(day, dlCfg())
	if !strings.Contains(pyDumps(ctx, -1), `"first_activity": "09:00"`) {
		t.Error("上下文里的 first_activity")
	}
	empty := buildDay(M{"date": "2026-10-28", "buckets": M{"window": []any{}}}, dlCfg(), nil)
	eq(t, "空的一天 active_sec 是 int 0", pyDumps(empty.G("totals").(*OM).G("active_sec"), -1), "0")
}

func TestHealthBrief(t *testing.T) {
	setupDL(t, false)
	d := "2026-11-06"
	for _, x := range []struct {
		m, ts string
		v     float64
	}{{"sleep_duration", d + "T08:00:00+08:00", 276}, {"sleep_score", d + "T08:00:00+08:00", 72}, {"sleep_avg_hrv", d + "T08:00:00+08:00", 36},
		{"resting_heart_rate", d + "T08:00:00+08:00", 66}, {"stress", d + "T18:00:00+08:00", 49}, {"sleep_duration", d + "T23:50:00+08:00", 300}} {
		_ = addHealth(x.m, x.ts, x.v, "")
	}
	b := healthBrief(d)
	eq(t, "同一天取最新值", b.G("睡眠时长(分钟)"), 300.0)
	eq(t, "HRV 进上下文", b.G("睡眠HRV(ms)"), 36.0)
	eq(t, "静息心率进上下文", b.G("静息心率(bpm)"), 66.0)
	eq(t, "没数据的日期返回空", healthBrief("2026-11-05").Len(), 0)
}

func TestPendingSkip(t *testing.T) {
	root := setupRoot(t)
	day := "2026-10-01"
	_ = saveReport(day, NewOM("totals", NewOM()), NewOM("headline", "old"), "# old")
	_ = os.WriteFile(filepath.Join(root, "inbox", day+".json"), []byte(`{"date": "2026-10-01", "buckets": {}}`), 0o644)
	rc := runPending(false, false)
	eq(t, "跳过时不报错", rc, 0)
	if _, err := os.Stat(filepath.Join(root, "inbox", day+".json")); err == nil {
		t.Error("inbox 文件应被清掉")
	}
	eq(t, "旧日报没被覆盖", pyStr(getReport(day).Analysis.G("headline")), "old")
	logb, _ := os.ReadFile(filepath.Join(root, "logs", "app.log"))
	if !strings.Contains(string(logb), "跳过") {
		t.Error("日志里应有跳过记录")
	}
	// 当天的不处理
	today := todayKey()
	_ = os.WriteFile(filepath.Join(root, "inbox", today+".json"), []byte(`{"date": "`+today+`", "buckets": {}}`), 0o644)
	runPending(false, false)
	if getReport(today) != nil {
		t.Error("当天还没结束，不该生成日报")
	}
	// 坏文件改名 .failed
	_ = os.WriteFile(filepath.Join(root, "inbox", "2026-09-01.json"), []byte(`不是 json`), 0o644)
	eq(t, "失败返回 1", runPending(false, false), 1)
	if _, err := os.Stat(filepath.Join(root, "inbox", "2026-09-01.json.failed")); err != nil {
		t.Error("坏文件应改名 .failed")
	}
}

func fakeDeepSeek(t *testing.T, replies []string) (*httptest.Server, *int32, *[]M) {
	var n int32
	var bodies []M
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" || r.URL.Path != "/chat/completions" {
			w.WriteHeader(401)
			return
		}
		var b M
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		if replies[i] == "HTTP500" {
			w.WriteHeader(500)
			return
		}
		finish := "stop"
		if strings.HasPrefix(replies[i], "LEN:") {
			finish = "length"
		}
		content := strings.TrimPrefix(replies[i], "LEN:")
		_ = json.NewEncoder(w).Encode(M{"choices": []any{M{"message": M{"content": content}, "finish_reason": finish}}, "usage": M{"total_tokens": 1234}})
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &bodies
}

func useLLM(t *testing.T, base string) {
	c := cloneConfig(conf())
	c.Daylog = deepMerge(c.Daylog, M{"analysis": M{"api_base": base, "api_key": "sk-test"}})
	setConfig(c)
	llmSleep = func(time.Duration) {}
	t.Cleanup(func() { llmSleep = time.Sleep })
}

func TestChatJSONRetry(t *testing.T) {
	setupDL(t, false)
	srv, n, bodies := fakeDeepSeek(t, []string{"HTTP500", "不是 JSON", `LEN:{"headline": "截断了", "narrative": "一半`})
	useLLM(t, srv.URL)
	o, err := chatJSON(dlCfg(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "三次请求", atomic.LoadInt32(n), int32(3))
	eq(t, "救回截断的字段", pyStr(o.G("headline")), "截断了")
	eq(t, "第二次之后 max_tokens 翻倍", toNum((*bodies)[2]["max_tokens"]), 16000.0)
	eq(t, "要 JSON 输出", toStr(asM((*bodies)[0]["response_format"])["type"]), "json_object")

	srv2, _, _ := fakeDeepSeek(t, []string{"HTTP500"})
	useLLM(t, srv2.URL)
	if _, err := chatJSON(dlCfg(), "s", "u"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("一直 500 要报错: %v", err)
	}
}

func TestPipelineEndToEnd(t *testing.T) {
	root := setupRoot(t)
	reply := `{"headline": "笨蛋主人今天写了 1 小时代码喵~", "narrative": "第一段\n\n第二段", "timeline_notes": [{"time": "09:00–10:00", "what": "VS Code", "note": "哼"}],
	  "patterns": [{"pattern": "p", "evidence": "e"}], "leaks": [{"target": "x.com", "minutes": 95, "why": "w"}], "music": {}, "question": "q?", "data_quality": "ok"}`
	srv, _, bodies := fakeDeepSeek(t, []string{reply})
	useLLM(t, srv.URL)
	day := "2026-10-28"
	// 当天听过的歌
	_ = addMusic(day, day+"T09:30:00+08:00", M{"title": "歌A", "artist": "某人"})
	b, _ := json.Marshal(sampleExport(day, 3))
	_ = os.WriteFile(filepath.Join(root, "inbox", day+".json"), b, 0o644)
	eq(t, "rc", runPending(false, true), 0)
	r := getReport(day)
	if r == nil {
		t.Fatal("没生成日报")
	}
	eq(t, "分析入库", pyStr(r.Analysis.G("headline")), "笨蛋主人今天写了 1 小时代码喵~")
	for _, s := range []string{"# 2026-10-28 周三", "- 活动区间：09:00 → 12:00", "## 今天听过的歌", "- 歌A — 某人（1 次）", "### 时间黑洞", "- x.com（95 分钟）：w", "> q?"} {
		if !strings.Contains(r.Markdown, s) {
			t.Errorf("markdown 缺 %q:\n%s", s, r.Markdown)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "reports", day+".md")); err != nil {
		t.Error("reports/*.md")
	}
	if _, err := os.Stat(filepath.Join(root, "raw", day+".json.gz")); err != nil {
		t.Error("raw 归档")
	}
	if _, err := os.Stat(filepath.Join(root, "inbox", day+".json")); err == nil {
		t.Error("分析成功后 inbox 应删掉")
	}
	prompt := toStr(asM(asA((*bodies)[0]["messages"])[1])["content"])
	for _, s := range []string{"笨蛋主人 2026-10-28 这一天", `"listened": [`, `"first_activity": "09:00"`, `"_payload_chars": `} {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt 缺 %q", s)
		}
	}
	// 分析失败：日报照存但 inbox 留着，下次再试（原版会在第二天把它当成「已有日报」删掉）
	srvBad, _, _ := fakeDeepSeek(t, []string{"HTTP500"})
	useLLM(t, srvBad.URL)
	d2 := "2026-10-29"
	b2, _ := json.Marshal(sampleExport(d2, 2))
	_ = os.WriteFile(filepath.Join(root, "inbox", d2+".json"), b2, 0o644)
	runPending(false, true)
	if getReport(d2) == nil {
		t.Error("分析失败也要有统计")
	}
	if _, err := os.Stat(filepath.Join(root, "inbox", d2+".json")); err != nil {
		t.Error("分析失败的 inbox 文件要留着")
	}
	useLLM(t, srv.URL)
	runPending(false, true)
	eq(t, "重试成功", pyStr(getReport(d2).Analysis.G("headline")), "笨蛋主人今天写了 1 小时代码喵~")
}

func TestTelegramMessage(t *testing.T) {
	setupDL(t, false)
	day := NewOM("date", "2026-10-28", "totals", NewOM("active_sec", 10800.0, "longest_focus_sec", 3599.9, "switches", int64(7)),
		"categories", []any{NewOM("name", "写代码", "sec", 4000.0)}, "listened", []any{NewOM("title", "A"), NewOM("title", "B"), NewOM("title", "C"), NewOM("title", "D")})
	msg := tgDailyMessage(dlCfg(), day, NewOM("headline", " <好> ", "question", "Q&A"))
	want := "<b>今天在干嘛 · 2026-10-28</b>\n\n<b>&lt;好&gt;</b>\n在电脑前 3h00m · 最长专注 59m · 切换 7 次\n写代码 1h06m\n🎵 A、B、C 等 4 首\n\n<i>Q&amp;A</i>"
	eq(t, "日报消息", msg, want)
}

func TestIngestAW(t *testing.T) {
	root := setupRoot(t)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(`{"date": "2026-10-28", "hostname": "pc", "buckets": {"window": []}}`))
	_ = zw.Close()
	req := httptest.NewRequest("POST", "/api/v1/aw", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	b, e := readAWBody(rec, req)
	if e != nil {
		t.Fatalf("readAWBody: %v", e)
	}
	res, e := ingestAW(b, "pc")
	if e != nil || res["day"] != "2026-10-28" {
		t.Fatalf("ingestAW: %v %v", res, e)
	}
	if _, err := os.Stat(filepath.Join(root, "inbox", "2026-10-28.json")); err != nil {
		t.Error("写进 inbox")
	}
	if _, e := ingestAW([]byte(`{"date":"../x"}`), "pc"); e == nil || e.code != 400 {
		t.Errorf("坏日期应 400: %v", e)
	}
}

func TestWeekNum(t *testing.T) {
	for d, want := range map[string]int{"2026-01-01": 0, "2026-01-05": 1, "2026-11-23": 47, "2027-01-04": 1} {
		tm, _ := time.Parse("2006-01-02", d)
		eq(t, d, pyWeekNum(tm), want)
	}
}

func TestDaylogSettingsPatch(t *testing.T) {
	setupDL(t, false)
	d := deepCopyM(dlCfg()).(map[string]any)
	var p dlPatch
	if err := json.Unmarshal([]byte(`{"daylog": {"has_categories": true, "categories": [{"name": "坏", "patterns": ["(?i)abc("]}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := applyDlPatch(d, &p); err == nil || !strings.Contains(err.Error(), "规则写错了") {
		t.Errorf("坏正则要拦下: %v", err)
	}
	p = dlPatch{}
	_ = json.Unmarshal([]byte(`{"daylog": {"analysis": {"enabled": true, "api_base": "https://api.deepseek.com", "model": "m", "max_output_tokens": 8000,
	  "temperature": 0.5, "daily_time": "25:00", "weekly_time": "Mon 00:40"}}}`), &p)
	if err := applyDlPatch(d, &p); err == nil {
		t.Error("25:00 要拦下")
	}
	p = dlPatch{}
	_ = json.Unmarshal([]byte(`{"daylog": {"privacy": {"secret_apps": [" a ", "a", ""], "hide_title_apps": [], "browser_apps": ["chrome"]},
	  "has_categories": true, "categories": [{"name": "沟通", "patterns": ["(?<![a-z])mail(?![a-z])"]}]},
	  "secrets": {"analysis.api_key": "sk-1"}}`), &p)
	if err := applyDlPatch(d, &p); err != nil {
		t.Fatal(err)
	}
	eq(t, "去重去空", len(asA(asM(d["privacy"])["secret_apps"])), 1)
	eq(t, "密钥写入", dStr(d, "analysis", "api_key"), "sk-1")
	n := normalizeDaylog(d)
	if !strings.Contains(strings.Join(dList(n, "privacy", "browser_apps"), ","), "msedge") {
		t.Error("浏览器名单要补回默认值")
	}
	view := func() M {
		c := cloneConfig(conf())
		c.Daylog = n
		setConfig(c)
		return dlSettingsView()
	}()
	if dStr(asM(view["daylog"]), "analysis", "api_key") != "" || asM(view["secrets_set"])["analysis.api_key"] != true {
		t.Error("读设置时密钥不能回显")
	}
}

func TestRecomputeReports(t *testing.T) {
	root := setupRoot(t)
	day := "2026-10-28"
	exp := sampleExport(day, 3)
	b, _ := json.Marshal(exp)
	if err := writeGzip(filepath.Join(root, "raw", day+".json.gz"), b); err != nil {
		t.Fatal(err)
	}
	// 造一篇「原版」日报：活动区间是 UTC 的
	st := buildDay(exp, dlCfg(), nil)
	st.Set("listened", []any{NewOM("title", "歌A", "artist", "某人", "plays", int64(2))})
	tt := st.G("totals").(*OM)
	tt.Set("first", "01:00")
	tt.Set("last", "04:00")
	an := NewOM("headline", "AI 写的字不能动", "narrative", "01:00 就开始了")
	if err := saveReport(day, st, an, toMarkdown(st, an)); err != nil {
		t.Fatal(err)
	}
	// 再造一篇被手动改过的：要跳过
	d2 := "2026-10-29"
	exp2 := sampleExport(d2, 2)
	b2, _ := json.Marshal(exp2)
	_ = writeGzip(filepath.Join(root, "raw", d2+".json.gz"), b2)
	st2 := buildDay(exp2, dlCfg(), nil)
	st2.G("totals").(*OM).Set("keys", int64(999))
	_ = saveReport(d2, st2, NewOM(), toMarkdown(st2, NewOM()))

	eq(t, "预览", recomputeOne(day, false).Status, "会改")
	eq(t, "预览不写库", pyStr(getReport(day).Stats.G("totals").(*OM).G("first")), "01:00")
	r := recomputeOne(day, true)
	eq(t, "写入", r.Status, "已改")
	rep := getReport(day)
	eq(t, "first 改成本地时间", pyStr(rep.Stats.G("totals").(*OM).G("first")), "09:00")
	eq(t, "last 改成本地时间", pyStr(rep.Stats.G("totals").(*OM).G("last")), "12:00")
	eq(t, "AI 分析不动", pyDumps(rep.Analysis, -1), pyDumps(an, -1))
	eq(t, "听过的歌保留", len(asA(rep.Stats.G("listened"))), 1)
	if !strings.Contains(rep.Markdown, "- 活动区间：09:00 → 12:00") || !strings.Contains(rep.Markdown, "01:00 就开始了") {
		t.Errorf("markdown:\n%s", rep.Markdown)
	}
	md, _ := os.ReadFile(filepath.Join(root, "reports", day+".md"))
	eq(t, "reports/*.md 同步", string(md), rep.Markdown)
	eq(t, "再跑一次不用改", recomputeOne(day, true).Status, "不用改")
	eq(t, "别的字段不同就跳过", recomputeOne(d2, true).Status, "跳过")
	eq(t, "没有 raw 就跳过", recomputeOne("2026-01-01", true).Status, "跳过")
}
