package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/* 对照 daylog/tests/test_rules.py 移植（不依赖网络） */

func setupDL(t *testing.T, showTitle bool) {
	t.Helper()
	l, _ := time.LoadLocation("Asia/Shanghai")
	loc, time.Local = l, l
	dataDir = t.TempDir()
	c := &Config{}
	fillDefaults(c)
	c.Daylog = normalizeDaylog(nil)
	asM(c.Daylog["live"])["public_show_title"] = showTitle
	asM(c.Daylog["live"])["token"] = "test-token"
	asM(c.Daylog["music"])["api_base"] = "" // 测试不上网
	c.DaylogWriter = true
	c.DaylogDB = filepath.Join(dataDir, "app.db")
	setConfig(c)
	dbMu.Lock()
	if dbHandle != nil {
		dbHandle.Close()
		dbHandle = nil
	}
	dbFailedAt = time.Time{}
	dbMu.Unlock()
	loadStatusTexts()
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

func TestGameNames(t *testing.T) {
	setupDL(t, true)
	for app, want := range map[string]string{"DeltaForceClient-Win64-Shipping.exe": "三角洲行动", "StarRail.exe": "崩坏：星穹铁道",
		"ZenlessZoneZero.exe": "绝区零", "ntegame.exe": "异环"} {
		eq(t, app, appLabel(dlCfg(), app), want)
	}
}

func TestIdentityNotOverwritten(t *testing.T) {
	setupDL(t, true)
	stMu.Lock()
	saved := stTexts.Heuristics
	stTexts.Heuristics = []any{[]any{"游戏|game|steam", "i", "在打游戏"}}
	stMu.Unlock()
	defer func() { stMu.Lock(); stTexts.Heuristics = saved; stMu.Unlock() }()
	eq(t, "异环 保留身份", describe(dlCfg(), "NTEGame.exe", "异环", "").Label, "在用 异环")
	eq(t, "未知游戏仍可用启发式", describe(dlCfg(), "WeirdGame.exe", "Whatever", "").Label, "在打游戏")
}

func TestCategories(t *testing.T) {
	setupDL(t, true)
	c := dlCfg()
	eq(t, "Cloudflare 不当邮件", describe(c, "chrome.exe", "Workers # Pages | a@gmail.com's Account | Cloudflare", "https://dash.cloudflare.com/x").Cat, "写代码")
	eq(t, "Outlook 仍识别为沟通", describe(c, "chrome.exe", "收件箱 - Outlook", "https://outlook.office.com/mail").Cat, "沟通")
	if got := describe(c, "chrome.exe", "某篇文章 contact a@b.com", "https://example.com/x").Cat; got == "沟通" {
		t.Errorf("普通含邮箱网页不算邮件: %s", got)
	}
	eq(t, "游戏归娱乐", describe(c, "StarRail.exe", "崩坏：星穹铁道", "").Cat, "娱乐")
	// (?<![a-z])mail(?![a-z]) 这种 Python 写法要能用
	eq(t, "lookbehind 正则", categoryOf(c, "Mail", "", ""), "沟通")
	if got := categoryOf(c, "gmailer", "", ""); got == "沟通" {
		t.Errorf("gmailer 不该算邮件: %s", got)
	}
}

func TestBrowserUnion(t *testing.T) {
	setupDL(t, false)
	d := normalizeDaylog(M{"privacy": M{"browser_apps": []any{"firefox"}}})
	got := dList(d, "privacy", "browser_apps")
	if got[0] != "firefox" || !strings.Contains(strings.Join(got, ","), "chrome") {
		t.Errorf("chrome 被补回: %v", got)
	}
	if lbl := describe(d, "chrome.exe", "some page title", "").Label; strings.Contains(lbl, "：") {
		t.Errorf("chrome 不再带标题: %s", lbl)
	}
}

func addS(t *testing.T, day, ts, app, label, plain, cat, title string) {
	t.Helper()
	if err := addSample(sample{Day: day, TS: ts, Host: "t", App: app, Label: label, LabelPlain: plain, Cat: cat, Title: title}, 120); err != nil {
		t.Fatal(err)
	}
}

func TestNeighbourDays(t *testing.T) {
	setupDL(t, false)
	for _, d := range []string{"2025-12-31", "2026-01-01", "2026-03-01"} {
		addS(t, d, d+"T10:00:00+08:00", "code", "在用 VS Code", "在用 VS Code", "写代码", "")
	}
	p, n := neighbourDays("2026-01-01", "2026-03-01")
	eq(t, "跨年前一天", p, "2025-12-31")
	eq(t, "后一天", n, "2026-01-02")
	_, n = neighbourDays("2026-12-31", "2026-12-31")
	eq(t, "超过最晚日期时后一天为空", n, "")
	p, _ = neighbourDays("2025-12-31", "2026-03-01")
	eq(t, "最早日期前一天为空", p, "")
}

func TestIdleAndPlain(t *testing.T) {
	setupDL(t, false)
	day := time.Now().In(loc).Format("2006-01-02")
	addS(t, day, day+"T10:00:00+08:00", "chrome", "Chrome：某个页面", "在用 Chrome", "浏览", "某个页面")
	if err := markIdle("t", "", day+"T10:05:00+08:00"); err != nil {
		t.Fatal(err)
	}
	apps := appTotals(day)
	eq(t, "idle 不新增使用记录", len(apps), 1)
	eq(t, "plain label 已保存", apps[0].LabelPlain, "在用 Chrome")
	eq(t, "历史只看应用名", appsPayload(apps, false)[0]["label"], "在用 Chrome喵~")
	eq(t, "当前在线仍可显示标题", appsPayload(apps, true)[0]["label"], "Chrome：某个页面喵~")
}

func TestMusic(t *testing.T) {
	setupDL(t, false)
	now := isoSec(time.Now().In(loc))
	m := `{"title":"A","artist":"B","cover":"","url":""}`
	if err := touchDevice("pc", "", now, "chrome", "在用 Chrome", &m); err != nil {
		t.Fatal(err)
	}
	if got := musicOf(240); got == nil || got["title"] != "A" {
		t.Errorf("music_of 能读到: %v", got)
	}
	nullStr := "null"
	_ = touchDevice("pc", "", now, "chrome", "在用 Chrome", &nullStr)
	if got := musicOf(240); got != nil {
		t.Errorf("JSON null 不应 panic 且返回 nil: %v", got)
	}
	empty := ""
	_ = touchDevice("pc", "", now, "chrome", "在用 Chrome", &empty)
	if got := musicOf(240); got != nil {
		t.Errorf("停止播放后清空: %v", got)
	}
}

func TestSameSongName(t *testing.T) {
	eq(t, "完全一样", sameSongName("ビビデバ", "ビビデバ"), true)
	eq(t, "带后缀", sameSongName("Two to Tango 交缠舞步", "Two to Tango 交缠舞步 (Live)"), true)
	eq(t, "大小写和空格", sameSongName("Pattern Recognition (feat. ODDEEO)", "pattern recognition"), true)
	eq(t, "视频标题搜到的无关结果", sameSongName("晴天", "要不怎么说你能赚钱呢，客户咳嗽一声就能瞬间明白对方需求。"), false)
	// 实测的原始数据：X 视频标题里含「瞬间」，盲搜配到了歌名就叫「瞬间」的歌
	eq(t, "长标题里凑巧含歌名", sameSongName("瞬间", "要不怎么说你能赚钱呢，客户咳嗽一声就能瞬间明白对方需求。"), false)
	eq(t, "空名字", sameSongName("", "x"), false)
	for _, src := range []string{"chrome", "MSEdge.exe", "com.android.chrome", "org.mozilla.firefox", "com.brave.browser",
		"Microsoft.MicrosoftEdge_8wekyb3d8bbwe!MicrosoftEdge", "google-chrome", "Safari"} {
		eq(t, "浏览器来源 "+src, looksLikeBrowser(src), true)
	}
	for _, src := range []string{"cloudmusic.exe", "spotify", "searchmusic", "citizen"} {
		eq(t, "非浏览器来源 "+src, looksLikeBrowser(src), false)
	}
}

func TestMedia(t *testing.T) {
	setupDL(t, true)
	now := time.Now().In(loc)
	_ = upsertLive("live:live.bilibili.com", "某直播间", "https://live.bilibili.com/1", isoSec(now))
	if items := buildMedia(); len(items) == 0 || items[0]["state"] != "focus" {
		t.Errorf("刚看过直播 -> focus: %v", items)
	}
	db := daylogDB()
	_, _ = db.Exec("UPDATE media SET last_focus=?, last_seen=? WHERE key=?", isoSec(now.Add(-200*time.Second)), isoSec(now), "live:live.bilibili.com")
	if items := buildMedia(); len(items) != 0 {
		t.Errorf("超过 120 秒不再出直播卡: %v", items)
	}
	_ = upsertLive("live:live.bilibili.com", "某直播间", "https://live.bilibili.com/1", isoSec(time.Now().In(loc)))
	m := `{"title":"A","artist":"B","cover":"","url":""}`
	_ = touchDevice("pc", "", isoSec(now), "chrome", "在用 Chrome", &m)
	eq(t, "直播 + 音乐 = 双卡", len(buildMedia()), 2)
	_, _ = db.Exec("DELETE FROM media WHERE key=?", "live:live.bilibili.com")
	items := buildMedia()
	if len(items) != 1 || items[0]["kind"] != "music" {
		t.Errorf("直播结束只剩音乐: %v", items)
	}
	k, name := liveKey("https://www.huya.com/123")
	eq(t, "虎牙直播间", k+"|"+name, "live:huya.com|虎牙")
}

func TestHealthCloud(t *testing.T) {
	setupDL(t, false)
	now := isoSec(time.Now().In(loc))
	for m, v := range map[string]float64{"sleep_duration": 402, "sleep_score": 81, "sleep_avg_hrv": 43, "resting_heart_rate": 57} {
		_ = addHealth(m, now, v, "")
	}
	v := healthView()
	eq(t, "睡眠分钟", v["sleep_min"], 402.0)
	eq(t, "评分", v["sleep_score"], 81.0)
	for _, k := range []string{"hrv", "rhr"} {
		if _, ok := v[k]; ok {
			t.Errorf("%s 不能对外暴露", k)
		}
	}
	allowed := map[string]bool{"available": true, "source": true, "updated": true, "dates": true, "hr_live": true, "hr_live_age": true,
		"hr_last_text": true, "sleep_min": true, "sleep_score": true, "spo2": true, "stress": true}
	for k := range v {
		if !allowed[k] {
			t.Errorf("公开视图越界字段: %s", k)
		}
	}
	_ = addHealth("sleep_duration", now, 500, "")
	eq(t, "同 metric 同 ts 幂等", healthView()["sleep_min"], 500.0)
	var n int
	_ = daylogDB().QueryRow("SELECT COUNT(*) FROM health_metrics WHERE metric='sleep_avg_hrv'").Scan(&n)
	eq(t, "HRV 仍留在库里", n, 1)
	setupDL(t, false)
	eq(t, "无数据返回空", len(healthView()), 0)
}

func TestICUMapping(t *testing.T) {
	m := icuToMetrics(M{"id": "2026-10-26", "restingHR": 58.0, "steps": 411.0, "hrv": 43.0, "sleepSecs": 24120.0, "sleepScore": 81.0, "spO2": 96.0, "stress": 34.0})
	eq(t, "静息心率", m["resting_heart_rate"], 58.0)
	eq(t, "步数", m["steps"], 411.0)
	eq(t, "HRV", m["sleep_avg_hrv"], 43.0)
	eq(t, "睡眠秒→分", m["sleep_duration"], 402.0)
	eq(t, "血氧", m["spo2"], 96.0)
	eq(t, "空记录不产出", len(icuToMetrics(M{"id": "2026-10-26"})), 0)
	_, has := icuToMetrics(M{"steps": 0.0})["steps"]
	eq(t, "0 值被忽略", has, false)
}

func TestBrowserTitle(t *testing.T) {
	setupDL(t, true)
	d := describe(dlCfg(), "chrome.exe", "周刊虚拟歌手中文曲排行榜 - 哔哩哔哩", "https://www.bilibili.com/x")
	if !strings.HasPrefix(d.Label, "Chrome：") {
		t.Errorf("开标题时显示 Chrome：页面标题: %s", d.Label)
	}
	eq(t, "plain 仍然只有应用名", d.Plain, "在用 Chrome")
	setupDL(t, false)
	eq(t, "关标题时只看域名", describe(dlCfg(), "chrome.exe", "周刊虚拟歌手中文曲排行榜 - 哔哩哔哩", "https://www.bilibili.com/x").Label, "在看 bilibili.com")
	setupDL(t, true)
	eq(t, "没标题时退回域名", describe(dlCfg(), "chrome.exe", "", "https://b23.tv/abc").Label, "在看 b23.tv")
}

func TestFmtAgoMeow(t *testing.T) {
	eq(t, "129 分钟", fmtAgo(129*60), "2小时9分钟")
	eq(t, "1540 分钟", fmtAgo(1540*60), "1天1小时40分钟")
	eq(t, "整小时", fmtAgo(3600), "1小时")
	eq(t, "几十秒", fmtAgo(30), "刚刚")
	eq(t, "喵~ 自动补", meow("在用 Chrome"), "在用 Chrome喵~")
	eq(t, "已有喵~ 不重复", meow("正在听歌喵~"), "正在听歌喵~")
	eq(t, "fmt_min 截断", fmtMin(119.9), "1m")
}

func TestUWPTitle(t *testing.T) {
	setupDL(t, true)
	c := dlCfg()
	asM(c["privacy"])["hide_title_apps"] = []any{"Unigram", "telegram"}
	d := describe(c, "ApplicationFrameHost.exe", "红糖server通知 @ 红 糖 \u200e- Unigram", "")
	eq(t, "只显示应用名", d.Label, "在用 Unigram")
	eq(t, "标题不下发", d.Title, "")
	if l := describe(c, "ApplicationFrameHost.exe", "某个群聊 - Unigram", "").Label; strings.Contains(l, "群聊") {
		t.Errorf("会话名不出现: %s", l)
	}
	eq(t, "无分隔符时用标题当名字", describe(c, "ApplicationFrameHost.exe", "计算器", "").Label, "在用 计算器")
	eq(t, "hide_title_apps 命中标题也隐藏", describe(c, "Telegram.exe", "Alice: 你好呀", "").Label, "在用 Telegram")
}

func TestSegments(t *testing.T) {
	setupDL(t, false)
	d := "2026-10-01"
	addS(t, d, d+"T10:00:00+08:00", "code", "在用 VS Code", "在用 VS Code", "写代码", "")
	addS(t, d, d+"T10:00:30+08:00", "code", "在用 VS Code", "在用 VS Code", "写代码", "")
	addS(t, d, d+"T10:01:00+08:00", "chrome", "在看 x.com", "在用 Chrome", "浏览", "")
	addS(t, d, d+"T10:10:00+08:00", "chrome", "在看 x.com", "在用 Chrome", "浏览", "") // 超过 gap：新起一段
	rows, _ := daylogDB().Query("SELECT app, dur FROM live ORDER BY id")
	var got []string
	for rows.Next() {
		var a string
		var d int64
		rows.Scan(&a, &d)
		got = append(got, a+":"+itoa(d))
	}
	rows.Close()
	eq(t, "分段与补时长", strings.Join(got, ","), "code:60,chrome:0,chrome:0")
}

func TestFreshDaylogAutoCreate(t *testing.T) {
	t.Setenv("DAYLOG_WRITER", "")
	if !configFromEnv().DaylogWriter {
		t.Fatal("全新部署 configFromEnv 默认应开启 DaylogWriter")
	}
	t.Setenv("DAYLOG_WRITER", "false")
	if configFromEnv().DaylogWriter {
		t.Fatal("显式设置 DAYLOG_WRITER=false 时应关闭")
	}
	dir := t.TempDir()
	c := &Config{}
	fillDefaults(c)
	c.Daylog = normalizeDaylog(nil)
	asM(c.Daylog["site"])["owner"] = "小明"
	c.DaylogWriter = true
	c.DaylogDB = filepath.Join(dir, "nested", "data", "app.db")
	setConfig(c)
	resetDaylogDB()
	t.Cleanup(resetDaylogDB)

	db := daylogDB()
	if db == nil {
		t.Fatal("全新嵌套路径下 daylogDB 应自动建目录并初始化数据库")
	}
	feed := getDaylogFeed(db)
	if feed == nil || !truthy(feed["ok"]) {
		t.Fatalf("getDaylogFeed 失败: %v", feed)
	}
	eq(t, "空库文案用配置的 owner", toStr(asM(feed["view"])["now"]), "还没有收到 小明 的活动")
}
