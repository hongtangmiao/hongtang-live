package main

import (
	"encoding/json"
	"hash/fnv"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "embed"

	"github.com/dlclark/regexp2"
)

/*
 * 隐私规则：把采集端报上来的前台窗口变成「人话」，并按隐私档位裁剪。
 * 逐行对照 daylog/app/live.py。用户配置里的正则是 Python 写法（有 (?<![a-z]) 这种），
 * Go 自带的 regexp 不支持，所以用 regexp2（.NET 语法，和 Python re 基本一致）。
 */

/* ---------------- 正则（带缓存、带超时） ---------------- */

type reKey struct {
	pat string
	opt regexp2.RegexOptions
}

var (
	reMu    sync.Mutex
	reCache = map[reKey]*regexp2.Regexp{}
	reBad   = map[reKey]bool{}
)

// pyRe 编译失败返回 nil（对应 Python 里的 re.error → continue）
func pyRe(pat string, ignoreCase bool) *regexp2.Regexp {
	opt := regexp2.RegexOptions(0)
	if ignoreCase {
		opt |= regexp2.IgnoreCase
	}
	k := reKey{pat, opt}
	reMu.Lock()
	defer reMu.Unlock()
	if r, ok := reCache[k]; ok {
		return r
	}
	if reBad[k] {
		return nil
	}
	r, err := regexp2.Compile(pat, opt)
	if err != nil {
		reBad[k] = true
		return nil
	}
	r.MatchTimeout = 200 * time.Millisecond
	if len(reCache) > 2000 {
		reCache = map[reKey]*regexp2.Regexp{}
	}
	reCache[k] = r
	return r
}

func reSearch(r *regexp2.Regexp, s string) bool {
	if r == nil {
		return false
	}
	ok, err := r.MatchString(s)
	return err == nil && ok
}

// reSplitFirst：re.split(pattern, s)，返回所有片段
func reSplit(r *regexp2.Regexp, s string) []string {
	var out []string
	last := 0
	runes := []rune(s)
	m, _ := r.FindRunesMatch(runes)
	for m != nil {
		out = append(out, string(runes[last:m.Index]))
		last = m.Index + m.Length
		m, _ = r.FindNextMatch(m)
	}
	return append(out, string(runes[last:]))
}

var reTitleSep = regexp2.MustCompile(`\s+[-—|]\s+`, 0)

/* ---------------- status-texts.json ---------------- */

//go:embed status-texts.json
var builtinStatusTexts []byte

type statusTexts struct {
	Exact      map[string]any
	Heuristics []any
	Names      map[string]any
	Default    string
}

var (
	stMu    sync.RWMutex
	stTexts statusTexts
	stMtime time.Time
	stErr   string
)

func statusPaths() []string { return []string{filepath.Join(dataDir, "status-texts.json")} }

// loadStatusTexts：内置一份 + data/status-texts.json 覆盖（后读的覆盖先读的）
func loadStatusTexts() {
	merged := statusTexts{Exact: map[string]any{}, Names: map[string]any{}}
	apply := func(b []byte, name string) {
		var d M
		if err := json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\ufeff")), &d); err != nil {
			stErr = name + ": " + err.Error()
			log.Printf("[status-texts] %s", stErr)
			return
		}
		for k, v := range asM(d["exact"]) {
			merged.Exact[k] = v
		}
		for k, v := range asM(d["names"]) {
			merged.Names[k] = v
		}
		merged.Heuristics = append(merged.Heuristics, asA(d["heuristics"])...)
		if truthy(d["default"]) {
			merged.Default = toStr(d["default"])
		}
	}
	apply(builtinStatusTexts, "builtin")
	var newest time.Time
	for _, p := range statusPaths() {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.ModTime().After(newest) {
			newest = st.ModTime()
		}
		b, err := os.ReadFile(p)
		if err == nil {
			apply(b, p)
		}
	}
	stMu.Lock()
	stTexts, stMtime = merged, newest
	stMu.Unlock()
}

// reloadStatusTextsIfChanged：改了 data/status-texts.json 不用重启（定时任务里调）
func reloadStatusTextsIfChanged() {
	var newest time.Time
	for _, p := range statusPaths() {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	stMu.RLock()
	changed := newest.After(stMtime)
	stMu.RUnlock()
	if changed {
		loadStatusTexts()
	}
}

func curStatus() statusTexts {
	stMu.RLock()
	defer stMu.RUnlock()
	return stTexts
}

func strHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// statusText：按 exact / heuristics 取一句人话；allowHeuristics=false 用于已定名的应用（保身份）
func statusText(app, name, title string, allowHeuristics bool) string {
	st := curStatus()
	keys := map[string]any{}
	for k, v := range st.Exact {
		keys[strings.ToLower(k)] = v
	}
	for _, cand := range []string{name, appKey(app), strings.ToLower(appKey(app))} {
		v, ok := keys[strings.ToLower(cand)]
		if !ok || !truthy(v) {
			continue
		}
		if list, isList := v.([]any); isList {
			if len(list) == 0 {
				continue
			}
			// Python 用 hash()（每次启动都不一样），这里换成稳定的哈希：同一个标题总是同一句
			return toStr(list[int(strHash(or(title, name))%uint32(len(list)))])
		}
		return toStr(v)
	}
	if !allowHeuristics {
		return ""
	}
	blob := app + " " + title
	for _, rule := range st.Heuristics {
		r := asA(rule)
		if len(r) != 3 {
			continue
		}
		if reSearch(pyRe(toStr(r[0]), strings.Contains(toStr(r[1]), "i")), blob) {
			return toStr(r[2])
		}
	}
	return ""
}

/* ---------------- 名字 / 分类 ---------------- */

var meowTailsPy = []string{"喵~", "喵", "喵～", "喵~。"}

// pyMeow：给「记录」补一句喵~；已经有就不重复加（Python 的 rstrip 去掉的是 Unicode 空白）
func pyMeow(text string) string {
	t := strings.TrimRightFunc(text, isPySpace)
	if t == "" {
		return t
	}
	for _, tail := range meowTailsPy {
		if strings.HasSuffix(t, tail) {
			return t
		}
	}
	return t + "喵~"
}

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x1f, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func pyStrip(s string) string { return strings.TrimFunc(s, isPySpace) }

func hitAny(patterns []string, text string) bool {
	low := strings.ToLower(text)
	for _, p := range patterns {
		if r := pyRe(p, true); r != nil {
			if reSearch(r, text) || strings.Contains(low, strings.ToLower(p)) {
				return true
			}
		} else if strings.Contains(low, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func appKey(app string) string {
	name := pyStrip(app)
	for _, suf := range []string{".exe", ".app", ".AppImage"} {
		if len(name) >= len(suf) && strings.EqualFold(name[len(name)-len(suf):], suf) {
			name = name[:len(name)-len(suf)]
		}
	}
	return name
}

func lowerMap(m M) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[strings.ToLower(k)] = toStr(v)
	}
	return out
}

func curatedName(cfg M, app string) string {
	return lowerMap(asM(cfg["app_names"]))[strings.ToLower(appKey(app))]
}

func customName(app string) string {
	key := strings.ToLower(appKey(app))
	for name, v := range curStatus().Names {
		if strings.ToLower(pyStrip(name)) == key {
			return toStr(v)
		}
	}
	return ""
}

func appLabel(cfg M, app string) string {
	key := strings.ToLower(appKey(app))
	if v, ok := lowerMap(asM(cfg["app_names"]))[key]; ok {
		return v
	}
	if c := customName(app); c != "" {
		return c
	}
	return or(appKey(app), "未知应用")
}

// 几条高置信度的内置规则：先于用户的 categories，免得改分类时把老问题带回来
var builtinRules = []struct {
	name string
	pats []string
}{
	{"写代码", []string{`(?i)cloudflare|dash\.cloudflare\.com|workers\.dev|pages\.dev`}},
	{"娱乐", []string{`(?i)三角洲|deltaforce|starrail|崩坏|星穹|绝区零|zenless|异环|ntegame|neverness|原神|genshin`}},
	{"沟通", []string{`(?i)mail\.google\.com|outlook\.(?:com|office\.com|live\.com)|mail\.(?:qq|163|126|sina)\.com` +
		`|inbox|收件箱|撰写邮件|foxmail|thunderbird|网易邮箱`}},
}

func categoryOf(cfg M, app, title, domain string) string {
	blob := app + "\n" + title + "\n" + domain
	for _, b := range builtinRules {
		for _, p := range b.pats {
			if reSearch(pyRe(p, false), blob) {
				return b.name
			}
		}
	}
	for _, c := range asA(cfg["categories"]) {
		cm := asM(c)
		for _, p := range asA(cm["patterns"]) {
			if reSearch(pyRe(toStr(p), false), blob) {
				return or(toStr(cm["name"]), "其他")
			}
		}
	}
	return "其他"
}

var uwpHosts = map[string]bool{"applicationframehost": true, "wwahost": true, "runtimebroker": true, "shellexperiencehost": true,
	"textinputhost": true, "lockapp": true, "searchapp": true}

// 这些进程名对人没意义，换成看得懂的名字（空串表示用窗口标题当名字）
var hostNames = map[string]string{
	"shellexperiencehost": "系统界面", "startmenuexperiencehost": "开始菜单", "searchapp": "搜索", "searchhost": "搜索",
	"textinputhost": "输入法", "lockapp": "锁屏", "sihost": "系统界面",
	"applicationframehost": "", "wwahost": "", "runtimebroker": "",
}

func isBrowser(cfg M, app string) bool {
	key := strings.ToLower(appKey(app))
	for _, p := range dList(cfg, "privacy", "browser_apps") {
		if r := pyRe(`^(?:`+p+`)(?:\b|$)`, true); r != nil {
			if reSearch(r, key) {
				return true
			}
		} else if strings.ToLower(p) == key {
			return true
		}
	}
	return false
}

// Windows 标题里会混进 U+200E 之类的双向控制符，先清掉再切分
func cleanTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || r == 0xfeff {
			return -1
		}
		return r
	}, s)
	return pyStrip(s)
}

func runeCut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func shortTitle(title string) string {
	short := pyStrip(reSplit(reTitleSep, title)[0])
	if short == "" {
		short = title
	}
	if utf8.RuneCountInString(short) > 42 {
		short = runeCut(short, 42) + "…"
	}
	return short
}

// netlocOf 近似 Python urlparse(url).netloc（小写，去掉 www.）
func domainOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := u.Host
	if u.User != nil {
		h = u.User.String() + "@" + h
	}
	h = strings.ToLower(h)
	return strings.TrimPrefix(h, "www.")
}

type described struct {
	Label, Cat, Title, Plain string
	Secret                   bool
}

func describe(cfg M, app, title, rawURL string) described {
	title = cleanTitle(title)
	if max := dInt(cfg, 80, "report", "title_max_chars"); utf8.RuneCountInString(title) > max {
		title = runeCut(title, max)
	}
	showTitle := dTruthy(cfg, "live", "public_show_title")
	hide := dList(cfg, "privacy", "hide_title_apps")
	if hitAny(dList(cfg, "privacy", "secret_apps"), app+" "+title) {
		return described{Label: "私密应用", Cat: "私密", Secret: true, Plain: "私密应用"}
	}
	cat := categoryOf(cfg, app, title, rawURL)
	name := appLabel(cfg, app)
	key := strings.ToLower(appKey(app))

	if friendly := hostNames[key]; friendly != "" {
		return described{Label: "在用 " + friendly, Cat: cat, Plain: "在用 " + friendly}
	}
	if uwpHosts[key] && title == "" {
		return described{Label: "在用 应用", Cat: cat, Plain: "在用 应用"}
	}
	curated := curatedName(cfg, app) != "" || customName(app) != ""
	custom := statusText(app, name, title, !curated)

	if isBrowser(cfg, app) {
		dom := domainOf(rawURL)
		plain := "在用 " + name
		if showTitle && !hitAny(hide, app) && title != "" {
			return described{Label: name + "：" + shortTitle(title), Cat: cat, Title: title, Plain: plain}
		}
		if dom != "" {
			return described{Label: "在看 " + dom, Cat: cat, Plain: plain}
		}
		if custom != "" {
			return described{Label: custom, Cat: cat, Plain: custom}
		}
		return described{Label: "在 " + name + " 里逛", Cat: cat, Plain: plain}
	}

	// UWP 应用跑在 ApplicationFrameHost 里：标题「会话名 - 应用名」只取最后一段，标题一律不显示
	if uwpHosts[key] && title != "" {
		var parts []string
		for _, x := range reSplit(reTitleSep, title) {
			if x = pyStrip(x); x != "" {
				parts = append(parts, x)
			}
		}
		guess := ""
		if len(parts) > 1 {
			guess = parts[len(parts)-1]
		} else if len(parts) == 1 {
			guess = parts[0]
		}
		guess = runeCut(guess, 30)
		if guess != "" {
			cat = categoryOf(cfg, guess, "", "")
			shown := or(customName(guess), guess)
			return described{Label: "在用 " + shown, Cat: cat, Plain: "在用 " + shown}
		}
	}

	plain := "在用 " + name
	if custom != "" {
		t := ""
		if showTitle {
			t = title
		}
		return described{Label: custom, Cat: cat, Title: t, Plain: custom}
	}
	if hitAny(hide, app) || title == "" || hitAny(hide, title) {
		return described{Label: plain, Cat: cat, Plain: plain}
	}
	if !showTitle {
		return described{Label: plain, Cat: cat, Plain: plain}
	}
	short := shortTitle(title)
	ls := strings.ToLower(short)
	ln := strings.ToLower(name)
	if ls == ln || strings.HasPrefix(ls, ln+".") || ls == key || ls == key+".exe" || strings.HasPrefix(ls, key+"-") {
		return described{Label: plain, Cat: cat, Title: title, Plain: plain}
	}
	return described{Label: name + "：" + short, Cat: cat, Title: title, Plain: plain}
}

/* ---------------- 一条上报 → 一条采样 ---------------- */

type sample struct {
	Day, TS, Host, App, Label, LabelPlain, Cat, Title, URL string
}

// pyFromISO 近似 Python 3.11+ datetime.fromisoformat；没有时区时 hasTZ=false
func pyFromISO(s string) (t time.Time, hasTZ, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, false
	}
	if len(s) >= 11 && (s[10] == ' ') {
		s = s[:10] + "T" + s[11:]
	}
	for _, l := range []string{"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04Z07:00", "2006-01-02T15Z07:00",
		"2006-01-02T15:04:05.999999999Z0700", "2006-01-02T15:04:05.999999999Z07"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true, true
		}
	}
	for _, l := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02T15", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, false, true
		}
	}
	return time.Time{}, false, false
}

// isoSec：Python 的 isoformat(timespec="seconds")
func isoSec(t time.Time) string { return t.Format("2006-01-02T15:04:05-07:00") }

func buildSample(cfg M, p M) sample {
	tz := loc
	ts := time.Now().In(tz)
	if raw := p["ts"]; truthy(raw) {
		if t, hasTZ, ok := pyFromISO(toStr(raw)); ok {
			if !hasTZ {
				t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), tz)
			}
			ts = t.In(tz)
		}
	}
	app := pyStrip(toStr(p["app"]))
	if app == "" {
		app = "未知"
	}
	title, rawURL := toStr(p["title"]), toStr(p["url"])
	host := "pc"
	if truthy(p["host"]) {
		host = toStr(p["host"])
	}
	hint := rawURL
	if pkg := pyStrip(toStr(p["package"])); hint == "" && pkg != "" {
		hint = phoneHint(pkg) // 手机：没有网址，拿包名帮着分类；不保存、不显示
	}
	d := describe(cfg, app, title, hint)
	keep := ""
	if dTruthy(cfg, "live", "public_show_title") {
		keep = d.Title
	}
	return sample{Day: ts.Format("2006-01-02"), TS: isoSec(ts), Host: host, App: appKey(app), Label: d.Label,
		LabelPlain: or(d.Plain, d.Label), Cat: d.Cat, Title: keep, URL: rawURL}
}

// phoneHint：安卓包名里多半没有分类规则认识的词（B 站是 tv.danmaku.bili），常见的翻成规则里的关键字
var phoneHints = []struct{ prefix, word string }{
	{"tv.danmaku.bili", "bilibili"}, {"com.bilibili", "bilibili"}, {"com.ss.android.ugc.aweme", "douyin"},
	{"com.sina.weibo", "weibo"}, {"com.xingin.xhs", "小红书"}, {"com.zhihu", "知乎"}, {"com.baidu.tieba", "贴吧"},
	{"com.google.android.youtube", "youtube"}, {"com.netflix", "netflix"}, {"tv.twitch", "twitch"}, {"com.reddit", "reddit"},
	{"com.tencent.mm", "wechat"}, {"com.tencent.mobileqq", "qq.exe"}, {"com.tencent.tim", "qq.exe"}, {"org.telegram", "telegram"},
	{"org.thunderdog.challegram", "telegram"}, {"com.discord", "discord"}, {"com.ss.android.lark", "feishu"},
	{"com.alibaba.android.rimet", "dingtalk"}, {"com.Slack", "slack"}, {"com.microsoft.office.outlook", "outlook"},
	{"com.github.android", "github"}, {"md.obsidian", "obsidian"}, {"notion.id", "notion"}, {"com.amazon.kindle", "kindle"},
	{"com.adobe.reader", "acrobat"}, {"com.valvesoftware", "steam"},
}

func phoneHint(pkg string) string {
	for _, h := range phoneHints {
		if strings.HasPrefix(pkg, h.prefix) {
			return pkg + "\n" + h.word
		}
	}
	return pkg
}
