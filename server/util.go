package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// M 是解码后的通用 JSON 对象
type M = map[string]any

var loc = time.Local

// jsRound 与 JS Math.round 一致（.5 向上取整）
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// toNum 近似 JS 的 Number(v) || 0
func toNum(v any) float64 {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0
		}
		return t
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0
		}
		return f
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0
		}
		return f
	case bool:
		if t {
			return 1
		}
	}
	return 0
}

// truthy 近似 JS 真值判断
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0 && !math.IsNaN(t)
	case json.Number:
		f, err := t.Float64()
		return err == nil && f != 0
	}
	return true
}

func toStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return jsNum(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// jsNum 近似 JS 的 String(number)
func jsNum(f float64) string {
	if f == 0 {
		return "0"
	}
	a := math.Abs(f)
	if a >= 1e21 || a < 1e-6 {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func fixed(f float64, n int) string { return strconv.FormatFloat(f, 'f', n, 64) }

func asM(v any) M {
	m, _ := v.(map[string]any)
	return m
}

func asA(v any) []any {
	a, _ := v.([]any)
	return a
}

// jsonBytes 不转义 < > &，与 JSON.stringify 的输出一致
func jsonBytes(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("null")
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// decodeJSON 保留数字原样（网易云的 id 很大）
func decodeJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

var reTZSuffix = regexp.MustCompile(`(?i)(z|[+-]\d{2}:?\d{2})$`)

// parseTS 近似 JS Date.parse：带时区按时区，不带时区按本地时间，纯日期按 UTC
func parseTS(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if reTZSuffix.MatchString(s) && len(s) > 10 {
		for _, l := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05.999999999-0700", "2006-01-02 15:04:05.999999999Z07:00"} {
			if t, err := time.Parse(l, s); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	for _, l := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, true
		}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func msOf(s string) (float64, bool) {
	t, ok := parseTS(s)
	if !ok {
		return 0, false
	}
	return float64(t.UnixMilli()), true
}

func nowMs() float64 { return float64(time.Now().UnixMilli()) }

func isoUTC(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func dayOf(t time.Time) string { return t.In(loc).Format("2006-01-02") }

// tzOffset：当前时区的 UTC 偏移（"+08:00"），配对时告诉设备
func tzOffset() string { return time.Now().In(loc).Format("-07:00") }

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	_ = os.Chmod(tmp, perm)
	return os.Rename(tmp, path)
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func strconvI(n int64) string { return strconv.FormatInt(n, 10) }
