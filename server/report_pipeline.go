package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

/* ---------------- 流水线（daily.process_file / run_pending / run_weekly） ---------------- */

func readBlocklist() []string {
	b, err := os.ReadFile(dlPath("blocklist.txt"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimPrefix(string(b), "\ufeff"), "\n") {
		line = pyStrip(strings.TrimRight(line, "\r"))
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func loadExport(path string) (M, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	b := raw
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, nil, err
		}
		b, err = io.ReadAll(io.LimitReader(zr, 512<<20))
		if err != nil {
			return nil, nil, err
		}
	}
	var m M
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &m); err != nil {
		return nil, nil, err
	}
	if m == nil || !reDay.MatchString(toStr(m["date"])) {
		return nil, nil, errors.New("不是 ActivityWatch 导出（缺 date）")
	}
	return m, b, nil
}

func processFile(path string, cfg M, useLLM bool) (*OM, error) {
	export, rawJSON, err := loadExport(path)
	if err != nil {
		return nil, err
	}
	day := buildDay(export, cfg, readBlocklist())
	d := pyStr(day.G("date"))
	t := day.G("totals").(*OM)
	dlLog(fmt.Sprintf("处理 %s：活跃 %s，%d 格，%s 张截图", d, pyFmtHMS(pyF(t.G("active_sec"))), len(asA(day.G("slots"))), pyStr(t.G("screens"))))

	listened := musicOfDay(d)
	if len(listened) > 0 {
		day.Set("listened", listened)
	}
	health := healthBrief(d)
	if health.Len() > 0 {
		day.Set("health", health)
	}

	var analysis *OM
	if useLLM && dBoolDef(cfg, true, "analysis", "enabled") {
		ctx := buildContext(day, cfg)
		if len(listened) > 0 {
			ls := []any{}
			for i, x := range listened {
				if i >= 40 {
					break
				}
				o := x.(*OM)
				ls = append(ls, NewOM("title", o.G("title"), "artist", o.G("artist"), "plays", o.G("plays"), "minutes", o.G("minutes")))
			}
			ctx.Set("listened", ls)
		}
		if health.Len() > 0 {
			ctx.Set("health", health)
		}
		a, err := analyzeDay(cfg, d, ctx)
		if err != nil {
			if err == errFakeLLM {
				a = fakeAnalysis(day)
			} else {
				dlLog(d + " 分析失败：" + err.Error())
			}
		}
		analysis = a
	}
	md := toMarkdown(day, orEmptyOM(analysis))
	if err := saveReport(d, day, analysis, md); err != nil {
		return nil, err
	}
	if dlRoot() != "" {
		_ = os.MkdirAll(dlPath("reports"), 0o755)
		_ = os.WriteFile(dlPath("reports", d+".md"), []byte(md), 0o644)
	}
	tgOnDaily(cfg, day, orEmptyOM(analysis))

	// 原始数据归档到 raw/；分析成功才从 inbox 删掉（失败的留着，下次再试）
	if dlRoot() != "" {
		if err := writeGzip(dlPath("raw", d+".json.gz"), rawJSON); err != nil {
			dlLog("归档 raw 失败：" + err.Error())
		} else if pyTruthy(analysis) {
			_ = os.Remove(path)
		}
	}
	return day, nil
}

func writeGzip(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, 6)
	_, _ = zw.Write(data)
	if err := zw.Close(); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.Bytes(), 0o644)
}

// runPending：处理 inbox。已有（带分析的）日报的日期直接跳过；处理失败的改名 .json.failed
func runPending(force, useLLM bool) int {
	cfg := dlCfg()
	if dlRoot() == "" {
		dlLog("没有配置 daylog 目录（daylog_root），跳过")
		return 1
	}
	files, _ := filepath.Glob(dlPath("inbox", "*.json"))
	sort.Strings(files)
	if len(files) == 0 {
		dlLog("inbox 是空的")
		return 0
	}
	today := todayKey()
	failed, skipped := 0, 0
	analysisOn := dBoolDef(cfg, true, "analysis", "enabled") && useLLM
	for _, f := range files {
		day := strings.TrimSuffix(filepath.Base(f), ".json")
		if !force && day >= today {
			dlLog(day + " 还没结束，先跳过")
			skipped++
			continue
		}
		if !force {
			if r := getReport(day); r != nil && (pyTruthy(r.Analysis) || !analysisOn) {
				skipped++
				dlLog(day + " 已有日报，跳过")
				_ = os.Remove(f)
				continue
			}
		}
		if _, err := processFile(f, cfg, useLLM); err != nil {
			failed++
			dlLog("处理 " + filepath.Base(f) + " 失败：" + err.Error())
			_ = os.Rename(f, strings.TrimSuffix(f, ".json")+".json.failed")
		}
	}
	if skipped > 0 {
		dlLog(fmt.Sprintf("本次跳过 %d 天（已有日报）", skipped))
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func runWeekly() {
	cfg := dlCfg()
	rows := listReports(8)
	days := []any{}
	for i, r := range rows {
		if i >= 7 {
			break
		}
		rep := getReport(r.Day)
		if rep == nil {
			continue
		}
		t, _ := rep.Stats.G("totals").(*OM)
		if t == nil {
			continue
		}
		cats := NewOM()
		for _, c := range omList(rep.Stats.G("categories")) {
			cats.Set(pyStr(c.G("name")), pyRoundN(pyF(c.G("sec"))/3600, 2))
		}
		days = append(days, NewOM("date", rep.Day, "active_h", pyRoundN(pyF(t.G("active_sec"))/3600, 2), "categories", cats,
			"switches_per_hour", pyRoundN(t.G("switches_per_hour"), 1), "headline", getOr(rep.Analysis, "headline", "")))
	}
	if len(days) == 0 {
		dlLog("周报：没有数据")
		return
	}
	payload := NewOM("days", days)
	analysis, err := chatJSON(cfg, systemWeekly, strings.Replace(promptWeekly, "{payload}", pyDumps(payload, 1), 1))
	if err != nil {
		dlLog("周报分析失败：" + err.Error())
		return
	}
	tgOnWeekly(cfg, analysis)
	dlLog("周报：" + pyStr(getOr(analysis, "headline", "")))
}

func fakeAnalysis(day *OM) *OM {
	t := day.G("totals").(*OM)
	var cats []string
	for i, c := range omList(day.G("categories")) {
		if i >= 3 {
			break
		}
		cats = append(cats, pyStr(c.G("name")))
	}
	c := strings.Join(cats, "、")
	if c == "" {
		c = "其他"
	}
	notes := []any{}
	for i, s := range omList(day.G("sessions")) {
		if i >= 4 {
			break
		}
		notes = append(notes, NewOM("time", pyStr(s.G("start"))+"–"+pyStr(s.G("end")), "what", pyStr(s.G("app"))+"（"+pyStr(s.G("cat"))+"）",
			"note", "连续 "+pyStr(int64(pyFloorDiv(pyF(s.G("sec")), 60)))+" 分钟"))
	}
	return NewOM(
		"headline", fmt.Sprintf("在电脑前 %.1f 小时，主要在 %s。", pyF(t.G("active_sec"))/3600, c),
		"narrative", "这是本地假分析（HL_FAKE_LLM=1），用来在没有模型 key 时把页面跑通。\n\n真实分析会在填了 key 之后生成。",
		"timeline_notes", notes,
		"patterns", []any{NewOM("pattern", "示例模式", "evidence", "假数据")},
		"leaks", []any{}, "tomorrow", []any{"把最想做的事放到上午第一个 90 分钟。"},
		"question", "今天哪一段时间最不想被打断？", "data_quality", "假分析，不看质量。",
	)
}

/* ---------------- DeepSeek（llm.py） ---------------- */

var errFakeLLM = errors.New("fake")

func stripFence(text string) string {
	t := pyStrip(text)
	if strings.HasPrefix(t, "```") {
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[i+1:]
		}
		if r := strings.TrimRightFunc(t, isPySpace); strings.HasSuffix(r, "```") {
			t = r[:len(r)-3]
		}
	}
	i, j := strings.Index(t, "{"), strings.LastIndex(t, "}")
	if i >= 0 && j > i {
		return t[i : j+1]
	}
	return t
}

// repairJSON：把被 token 上限截断的 JSON 尽量补完整（补引号、去掉尾巴上的逗号/冒号、补括号）
func repairJSON(t string) string {
	var out strings.Builder
	var stack []byte
	inStr, esc := false, false
	for _, ch := range t {
		out.WriteRune(ch)
		if inStr {
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	s := out.String()
	if inStr {
		s += `"`
	}
	s = strings.TrimRight(strings.TrimRightFunc(s, isPySpace), ",")
	if strings.HasSuffix(s, ":") {
		s += " null"
	}
	for strings.HasSuffix(s, "，") {
		s = strings.TrimSuffix(s, "，")
	}
	for i := len(stack) - 1; i >= 0; i-- {
		s += string(stack[i])
	}
	return s
}

func parseJSONLoose(text string) *OM {
	raw := stripFence(text)
	for _, cand := range []string{raw, repairJSON(raw)} {
		if v, err := pyLoads([]byte(cand)); err == nil {
			if o, ok := v.(*OM); ok {
				return o
			}
		}
	}
	idx := len(raw)
	for i := 0; i < 40; i++ {
		idx = strings.LastIndex(raw[:idx], "}")
		if idx < 0 {
			break
		}
		if v, err := pyLoads([]byte(raw[:idx+1])); err == nil {
			if o, ok := v.(*OM); ok {
				return o
			}
		}
	}
	return nil
}

var llmSleep = time.Sleep // 测试里换成不睡

func chatJSON(cfg M, system, user string) (*OM, error) {
	if os.Getenv("HL_FAKE_LLM") == "1" || os.Getenv("DAYLOG_FAKE") == "1" {
		return nil, errFakeLLM
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		key = dStr(cfg, "analysis", "api_key")
	}
	if key == "" {
		return nil, errors.New("没有 DeepSeek API key：请在设置页里填")
	}
	base := strings.TrimRight(or(dStr(cfg, "analysis", "api_base"), "https://api.deepseek.com"), "/")
	tokens := dInt(cfg, 8000, "analysis", "max_output_tokens")
	if tokens <= 0 {
		tokens = 8000
	}
	temp := 0.5
	if v, ok := pyFloat(dget(cfg, "analysis", "temperature")); ok {
		temp = v
	}
	timeout := time.Duration(dInt(cfg, 240, "analysis", "timeout_sec")) * time.Second
	model := or(dStr(cfg, "analysis", "model"), "deepseek-chat")

	last := ""
	for attempt := 0; attempt < 3; attempt++ {
		body := NewOM("model", model, "messages", []any{NewOM("role", "system", "content", system), NewOM("role", "user", "content", user)},
			"temperature", temp, "max_tokens", int64(tokens), "response_format", NewOM("type", "json_object"), "stream", false)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		req, _ := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", strings.NewReader(pyDumps(body, -1)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			last = "网络错误：" + strings.ReplaceAll(err.Error(), key, "***")
			if attempt < 2 {
				llmSleep(time.Duration(5*(attempt+1)) * time.Second)
				continue
			}
			break
		}
		rb, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		cancel()
		if rerr != nil {
			last = "网络错误：" + rerr.Error()
			if attempt < 2 {
				llmSleep(time.Duration(5*(attempt+1)) * time.Second)
				continue
			}
			break
		}
		if resp.StatusCode != 200 {
			last = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, runeCut(string(rb), 300))
			if (resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode <= 504) && attempt < 2 {
				llmSleep(time.Duration(5*(attempt+1)) * time.Second)
				continue
			}
			break
		}
		var payload struct {
			Choices []struct {
				Message      struct{ Content string } `json:"message"`
				FinishReason string                   `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				TotalTokens any `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(rb, &payload); err != nil {
			last = "返回不是合法 JSON：" + err.Error()
			break
		}
		content, finish := "", ""
		if len(payload.Choices) > 0 {
			content, finish = payload.Choices[0].Message.Content, payload.Choices[0].FinishReason
		}
		used := "?"
		if payload.Usage.TotalTokens != nil {
			used = pyStr(pyJSONVal(payload.Usage.TotalTokens))
		}
		dlLog(fmt.Sprintf("模型返回 %d 字，tokens=%s，finish=%s，max_tokens=%d", pyLen(content), used, or(finish, "?"), tokens))
		var data *OM
		if content != "" {
			data = parseJSONLoose(content)
		}
		if data != nil {
			if finish == "length" {
				dlLog("注意：这次输出被 token 上限截断，已从残缺 JSON 里救回部分字段")
			}
			return data, nil
		}
		last = fmt.Sprintf("第 %d 次输出不是合法 JSON（finish=%s，%d 字）", attempt+1, or(finish, "?"), pyLen(content))
		if attempt < 2 {
			tokens = minInt(16000, tokens*2)
			temp = maxF(0.2, pyF(pyRoundN(temp-0.15, 2)))
			dlLog(fmt.Sprintf("%s，把 max_tokens 提到 %d 重试", last, tokens))
			llmSleep(3 * time.Second)
			continue
		}
		break
	}
	if last == "" {
		last = "未知错误"
	}
	return nil, errors.New(last)
}

func analyzeDay(cfg M, date string, ctx *OM) (*OM, error) {
	p := strings.Replace(promptDaily, "{date}", date, 1)
	p = strings.Replace(p, "{payload}", pyDumps(ctx, 1), 1)
	return chatJSON(cfg, systemDaily, p)
}
