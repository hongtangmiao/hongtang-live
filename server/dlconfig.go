package main

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
)

/*
 * daylog 部分的配置：结构和原来 daylog 的 config.json 完全一样（放在 config.json 的 "daylog" 里），
 * 这样隐私规则、分类、日报这些逻辑可以逐行对照 Python 移植。
 * 第一次启动时如果有 DAYLOG_CONFIG（原 config.json 的路径）就导入一次，否则用默认值。
 */

// dlDefaultsJSON 就是 daylog/app/settings.py 里的 DEFAULTS
const dlDefaultsJSON = `{
 "site": {"title": "今天在干嘛", "subtitle": "实时状态面板", "owner": "我"},
 "analysis": {"enabled": true, "api_base": "https://api.deepseek.com", "api_key": "", "model": "deepseek-chat",
   "max_output_tokens": 8000, "temperature": 0.5, "timeout_sec": 240, "daily_time": "00:10", "weekly_time": "Mon 00:40"},
 "report": {"title_max_chars": 80, "max_timeline_rows": 300, "max_payload_chars": 26000, "top_apps": 15,
   "top_domains": 15, "max_sessions": 25, "ocr_chars_per_screen": 240, "max_screens": 400},
 "privacy": {
   "secret_apps": ["1password", "bitwarden", "keepass", "lastpass", "keepassxc", "网银", "银行", "支付宝", "微信支付", "证券", "券商",
     "authenticator", "验证器", "身份证", "密码"],
   "hide_title_apps": ["wechat", "微信", "qq", "tim", "feishu", "飞书", "dingtalk", "钉钉", "outlook", "thunderbird", "foxmail", "mail",
     "telegram", "discord"],
   "browser_apps": ["chrome", "msedge", "edge", "firefox", "brave", "arc", "vivaldi", "opera", "chromium", "librewolf", "zen", "floorp"]},
 "live": {"enabled": true, "public_show_title": false, "stale_sec": 180, "poll_sec": 10, "segment_gap_sec": 120, "token": "", "max_rows": 60},
 "telegram": {"enabled": false, "bot_token": "", "chat_id": "", "send_daily": true, "send_weekly": false},
 "plugins": {"telegram": true, "ocr": true},
 "health": {"icu": {"enabled": false, "api_key": "", "athlete_id": "", "poll_min": 30, "days": 14}},
 "music": {"enabled": true, "api_base": "", "link_base": "https://music.163.com/song?id="},
 "app_names": {"code": "VS Code", "chrome": "Chrome", "msedge": "Edge", "firefox": "Firefox", "explorer": "文件管理器", "wechat": "微信",
   "qq": "QQ", "potplayermini64": "PotPlayer", "photoshop": "Photoshop", "bilibili": "哔哩哔哩"},
 "categories": [
   {"name": "写代码", "patterns": [
     "(?im)^code(\\.exe)?$|visual studio code|vscode|cursor|pycharm|intellij|webstorm|goland|sublime|notepad\\+\\+",
     "(?i)powershell|cmd\\.exe|windowsterminal|windows terminal|mintty|wsl|putty|tabby",
     "(?i)github|gitlab|stack overflow|\\.py\\b|\\.ts\\b|\\.rs\\b|\\.go\\b|\\.java\\b|\\.ps1\\b|\\.json\\b|\\.toml\\b"]},
   {"name": "创作", "patterns": ["(?i)photoshop|illustrator|indesign|blender|premiere|after effects|davinci|figma|clip studio|krita|sai2|aseprite|obs\\b|audition"]},
   {"name": "沟通", "patterns": ["(?i)wechat|微信|qq\\.exe|telegram|discord|feishu|飞书|dingtalk|钉钉|slack|outlook|thunderbird|foxmail|邮件|收件箱|inbox|(?<![a-z])mail(?![a-z])"]},
   {"name": "娱乐", "patterns": ["(?i)bilibili|youtube|netflix|twitch|steam|epicgames|原神|崩坏|星穹|抖音|douyin|tiktok|weibo|微博|贴吧|reddit|小红书|知乎"]},
   {"name": "读资料", "patterns": ["(?i)acrobat|sumatrapdf|pdf|obsidian|notion|zotero|calibre|epub|kindle|readwise"]},
   {"name": "浏览", "patterns": ["(?i)chrome|msedge|edge|firefox|brave|arc\\.exe|opera|vivaldi"]}
 ]
}`

const oldMailPattern = `(?i)wechat|微信|qq\.exe|telegram|discord|feishu|飞书|dingtalk|钉钉|slack|outlook|thunderbird|foxmail|邮件|mail`
const newMailPattern = `(?i)wechat|微信|qq\.exe|telegram|discord|feishu|飞书|dingtalk|钉钉|slack|outlook|thunderbird|foxmail|邮件|收件箱|inbox|(?<![a-z])mail(?![a-z])`

func dlDefaults() M {
	var m M
	if err := json.Unmarshal([]byte(dlDefaultsJSON), &m); err != nil {
		panic(err)
	}
	return m
}

// deepMerge 与 settings.deep_merge 一致：字典递归合并，其余直接覆盖
func deepMerge(base, over M) M {
	out := M{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if vm, ok := v.(map[string]any); ok {
			if bm, ok := out[k].(map[string]any); ok {
				out[k] = deepMerge(bm, vm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// normalizeDaylog：补默认值 + settings.load() 里的两个迁移（浏览器名单取并集、旧版宽松邮件正则换成严格版）
func normalizeDaylog(d M) M {
	d = deepMerge(dlDefaults(), d)
	priv := asM(d["privacy"])
	if priv == nil {
		priv = M{}
		d["privacy"] = priv
	}
	seen := map[string]bool{}
	merged := []any{}
	for _, x := range append(asA(priv["browser_apps"]), asA(asM(dlDefaults()["privacy"])["browser_apps"])...) {
		s := toStr(x)
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}
	priv["browser_apps"] = merged
	for _, c := range asA(d["categories"]) {
		cm := asM(c)
		if cm == nil || toStr(cm["name"]) != "沟通" {
			continue
		}
		pats := asA(cm["patterns"])
		for i, p := range pats {
			if toStr(p) == oldMailPattern {
				pats[i] = newMailPattern
			}
		}
	}
	if an := asM(d["analysis"]); an != nil {
		base := strings.ToLower(toStr(an["api_base"]))
		if toStr(an["model"]) == "deepseek-flash" && (base == "" || strings.Contains(base, "deepseek.com")) {
			an["model"] = "deepseek-chat"
		}
	}
	return d
}

// importDaylogConfig：第一次启动时把原 daylog 的 config.json 搬进来（之后以 hongtang-live 的为准）
func importDaylogConfig(c *Config) bool {
	if c.Daylog != nil {
		c.Daylog = normalizeDaylog(c.Daylog)
		return false
	}
	var raw M
	if p := os.Getenv("DAYLOG_CONFIG"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[daylog] 读不到 DAYLOG_CONFIG=%s：%v，用默认配置", p, err)
		} else if err := json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\ufeff")), &raw); err != nil {
			log.Printf("[daylog] %s 不是有效的 JSON：%v，用默认配置", p, err)
			raw = nil
		} else {
			log.Printf("[daylog] 已导入 %s", p)
		}
	}
	// 登录 / 会话这些现在由 hongtang-live 自己管，不搬
	delete(raw, "security")
	c.Daylog = normalizeDaylog(raw)
	return true
}

/* ---------- 读取 daylog 配置的小工具（对照 Python 的 (cfg.get("x") or {}).get("y")） ---------- */

func dlCfg() M { return conf().Daylog }

func dget(m M, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = cm[k]
	}
	return cur
}

func dStr(m M, keys ...string) string {
	v := dget(m, keys...)
	if v == nil {
		return ""
	}
	return toStr(v)
}

// dTruthy：Python 的 bool(x)
func dTruthy(m M, keys ...string) bool { return truthy(dget(m, keys...)) }

// dBoolDef：键不存在时用默认值（Python 的 .get(k, default)）
func dBoolDef(m M, def bool, keys ...string) bool {
	parent := asM(dget(m, keys[:len(keys)-1]...))
	if parent == nil {
		if len(keys) == 1 {
			parent = m
		} else {
			return def
		}
	}
	v, ok := parent[keys[len(keys)-1]]
	if !ok {
		return def
	}
	return truthy(v)
}

func dInt(m M, def int, keys ...string) int {
	v := dget(m, keys...)
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return def
		}
		return n
	}
	return int(toNum(v))
}

func dList(m M, keys ...string) []string {
	out := []string{}
	for _, x := range asA(dget(m, keys...)) {
		out = append(out, toStr(x))
	}
	return out
}

func dOwner() string { return or(dStr(dlCfg(), "site", "owner"), "我") }
