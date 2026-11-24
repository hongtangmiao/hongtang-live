package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

/* ---------------- 管理接口：/api/admin/daylog/* ---------------- */

func handleAdminDaylog(w http.ResponseWriter, r *http.Request, sub string) {
	switch sub {
	case "reports":
		writeJSON(w, 200, M{"ok": true, "reports": listReports(120)})
	case "report":
		d := r.URL.Query().Get("d")
		if !reDay.MatchString(d) {
			writeJSON(w, 400, M{"ok": false, "error": "bad day"})
			return
		}
		rep := getReport(d)
		if rep == nil {
			writeJSON(w, 404, M{"ok": false, "error": "没有这一天的日报"})
			return
		}
		send(w, 200, []byte(pyDumps(NewOM("ok", true, "day", rep.Day, "created_at", rep.CreatedAt, "telegram_sent", rep.TGSent,
			"stats", rep.Stats, "analysis", rep.Analysis, "markdown", rep.Markdown), -1)), map[string]string{"Content-Type": "application/json; charset=utf-8"})
	case "log":
		writeJSON(w, 200, M{"ok": true, "log": tailFile(dlPath("logs", "app.log"), 16000)})
	case "settings":
		handleDaylogSettings(w, r)
	case "run":
		var body struct {
			What  string `json:"what"`
			Force bool   `json:"force"`
		}
		if !adminPost(w, r, &body) {
			return
		}
		if !conf().DaylogWriter && body.What != "test_tg" {
			writeJSON(w, 409, M{"ok": false, "error": "这台服务器还没接管 daylog（daylog_writer 没开），日报由原 daylog 生成"})
			return
		}
		var job func()
		switch body.What {
		case "pending":
			force := body.Force
			job = func() { runPending(force, true) }
		case "weekly":
			job = runWeekly
		case "icu":
			job = func() { icuSync() }
		case "test_tg":
			job = func() { tgSend(dlCfg(), "<b>hongtang-live</b>\n这是一条测试消息。") }
		default:
			writeJSON(w, 400, M{"ok": false, "error": "what 只能是 pending / weekly / icu / test_tg"})
			return
		}
		if !runLock.TryLock() {
			writeJSON(w, 409, M{"ok": false, "busy": true, "error": "上一个任务还在跑，等它结束再试"})
			return
		}
		go func() {
			defer runLock.Unlock()
			defer func() {
				if x := recover(); x != nil {
					dlLog(fmt.Sprintf("手动任务出错：%v", x))
				}
			}()
			dlLog("手动任务：" + body.What)
			job()
		}()
		writeJSON(w, 200, M{"ok": true, "started": true})
	default:
		writeJSON(w, 404, M{"ok": false, "error": "not found"})
	}
}

func tailFile(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, _ := f.Stat()
	off := int64(0)
	if st != nil && st.Size() > n {
		off = st.Size() - n
	}
	_, _ = f.Seek(off, io.SeekStart)
	b, _ := io.ReadAll(f)
	s := string(b)
	if off > 0 {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return strings.ToValidUTF8(s, "")
}

/* ---------------- 上传 ActivityWatch 导出：POST /api/ingest/aw（给第 4 阶段的托盘程序用） ---------------- */

var reSafeHost = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// readAWBody：读 ActivityWatch 导出（可以 gzip），最大 64MB 压缩 / 256MB 解压
func readAWBody(w http.ResponseWriter, r *http.Request) ([]byte, *ingestErr) {
	var rd io.Reader = http.MaxBytesReader(w, r.Body, 64<<20)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(rd)
		if err != nil {
			return nil, &ingestErr{400, "gzip 解不开"}
		}
		rd = io.LimitReader(zr, 256<<20)
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		return nil, &ingestErr{413, "太大了"}
	}
	return b, nil
}

func ingestAW(b []byte, from string) (M, *ingestErr) {
	if dlRoot() == "" {
		return nil, &ingestErr{503, "没有配置 daylog 目录"}
	}
	var m M
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &m) != nil || !reDay.MatchString(toStr(m["date"])) {
		return nil, &ingestErr{400, "不是 ActivityWatch 导出（缺 date）"}
	}
	day := toStr(m["date"])
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, &ingestErr{400, "bad date"}
	}
	if err := os.MkdirAll(dlPath("inbox"), 0o755); err != nil {
		return nil, &ingestErr{500, "inbox 建不了"}
	}
	if err := writeFileAtomic(dlPath("inbox", day+".json"), b, 0o644); err != nil {
		return nil, &ingestErr{500, "写入失败"}
	}
	exists := getReport(day) != nil
	if from == "" {
		from = reSafeHost.ReplaceAllString(toStr(m["hostname"]), "")
	}
	dlLog(fmt.Sprintf("收到 %s 的 ActivityWatch 导出（%d KB，来自 %s）", day, len(b)/1024, from))
	return M{"ok": true, "day": day, "report_exists": exists}, nil
}
