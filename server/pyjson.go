package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

/*
 * 和 Python 一致的 JSON / 数字 / 真值：日报的统计、喂给模型的上下文都是 Python dict，
 * 键的顺序、float 的写法（2.0 / 1e-05）都会进到 prompt 和库里。为了能和原版逐字节对比，这里照着 CPython 实现。
 *   OM          有序字典（dict）
 *   float64     Python float
 *   int64/int   Python int
 */

type OM struct {
	keys []string
	vals map[string]any
}

func NewOM(kv ...any) *OM {
	o := &OM{vals: map[string]any{}}
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func (o *OM) Set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *OM) Get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[k]
	return v, ok
}

func (o *OM) G(k string) any {
	v, _ := o.Get(k)
	return v
}

func (o *OM) Keys() []string { return o.keys }
func (o *OM) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

func (o *OM) MarshalJSON() ([]byte, error) { return []byte(pyDumps(o, -1)), nil }

/* ---------------- float repr ---------------- */

// pyRepr：CPython 的 float.__repr__（最短表示；指数 < -4 或 >= 16 用科学计数法）
func pyRepr(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == 0 {
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // -1.2345e+06
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	ei := strings.IndexByte(s, 'e')
	mant, exp := s[:ei], s[ei+1:]
	e, _ := strconv.Atoi(exp)
	digits := strings.Replace(mant, ".", "", 1)
	var out string
	if e < -4 || e >= 16 {
		m := digits[:1]
		if len(digits) > 1 {
			m += "." + digits[1:]
		}
		sign := "+"
		if e < 0 {
			sign = "-"
			e = -e
		}
		es := strconv.Itoa(e)
		if len(es) < 2 {
			es = "0" + es
		}
		out = m + "e" + sign + es
	} else if e < 0 {
		out = "0." + strings.Repeat("0", -e-1) + digits
	} else {
		if len(digits) <= e+1 {
			out = digits + strings.Repeat("0", e+1-len(digits)) + ".0"
		} else {
			out = digits[:e+1] + "." + digits[e+1:]
		}
	}
	if neg {
		return "-" + out
	}
	return out
}

/* ---------------- dumps ---------------- */

func pyEscape(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteString(strconv.FormatInt(int64(r)>>4, 16))
				b.WriteString(strconv.FormatInt(int64(r)&15, 16))
			} else if r == utf8.RuneError {
				b.WriteString("\ufffd")
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// pyDumps：json.dumps(v, ensure_ascii=False[, indent=n])；indent<0 表示不缩进
func pyDumps(v any, indent int) string {
	var b strings.Builder
	pyDump(&b, v, indent, 0)
	return b.String()
}

func pyDump(b *strings.Builder, v any, indent, level int) {
	nl := func(l int) {
		if indent >= 0 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", indent*l))
		}
	}
	sep := ", "
	if indent >= 0 {
		sep = ","
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		pyEscape(b, t)
	case float64:
		b.WriteString(pyRepr(t))
	case float32:
		b.WriteString(pyRepr(float64(t)))
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case json.Number:
		b.WriteString(t.String())
	case *OM:
		if t == nil || len(t.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				b.WriteString(sep)
			}
			nl(level + 1)
			pyEscape(b, k)
			b.WriteString(": ")
			pyDump(b, t.vals[k], indent, level+1)
		}
		nl(level)
		b.WriteByte('}')
	case map[string]any:
		// 普通 map 没有顺序：按键排序输出（只在不在乎顺序的地方用）
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := NewOM()
		for _, k := range keys {
			o.Set(k, t[k])
		}
		pyDump(b, o, indent, level)
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, x := range t {
			if i > 0 {
				b.WriteString(sep)
			}
			nl(level + 1)
			pyDump(b, x, indent, level+1)
		}
		nl(level)
		b.WriteByte(']')
	case []*OM:
		a := make([]any, len(t))
		for i, x := range t {
			a[i] = x
		}
		pyDump(b, a, indent, level)
	case []string:
		a := make([]any, len(t))
		for i, x := range t {
			a[i] = x
		}
		pyDump(b, a, indent, level)
	default:
		bs, _ := json.Marshal(t)
		b.Write(bs)
	}
}

func pyLen(s string) int { return utf8.RuneCountInString(s) }

/* ---------------- 有序解析（json.loads → dict 保留顺序；整数是 int，带小数点/指数的是 float） ---------------- */

func pyLoads(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := pyDecodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errTrailing
	}
	return v, nil
}

type jsonErr string

func (e jsonErr) Error() string { return string(e) }

const errTrailing = jsonErr("extra data")

func pyDecodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewOM()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := pyDecodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			a := []any{}
			for dec.More() {
				v, err := pyDecodeValue(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
		return nil, jsonErr("bad delim")
	case json.Number:
		s := t.String()
		if strings.ContainsAny(s, ".eE") {
			f, err := strconv.ParseFloat(s, 64)
			return f, err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			f, _ := strconv.ParseFloat(s, 64)
			return f, nil
		}
		return n, nil
	default:
		return t, nil // string / bool / nil
	}
}

/* ---------------- Python 语义的小工具 ---------------- */

// pyTruthy：bool(x)
func pyTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case json.Number:
		f, _ := t.Float64()
		return f != 0
	case *OM:
		return t != nil && len(t.keys) > 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	case []*OM:
		return len(t) > 0
	}
	return true
}

// pyStr：str(x)（f-string 里插值就是这个）
func pyStr(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case string:
		return t
	case float64:
		r := pyRepr(t)
		switch r {
		case "NaN":
			return "nan"
		case "Infinity":
			return "inf"
		case "-Infinity":
			return "-inf"
		}
		return r
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return t.String()
	}
	return pyReprObj(v)
}

// pyReprObj：repr(x)（str(dict) / str(list) 用的就是元素的 repr）
func pyReprObj(v any) string {
	switch t := v.(type) {
	case string:
		return pyStrRepr(t)
	case *OM:
		if t == nil {
			return "{}"
		}
		parts := make([]string, 0, len(t.keys))
		for _, k := range t.keys {
			parts = append(parts, pyStrRepr(k)+": "+pyReprObj(t.vals[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			parts = append(parts, pyReprObj(x))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := NewOM()
		for _, k := range keys {
			o.Set(k, t[k])
		}
		return pyReprObj(o)
	}
	return pyStr(v)
}

// pyPrintable：str.isprintable() 的单字符版
func pyPrintable(r rune) bool {
	if r == ' ' {
		return true
	}
	if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Co, unicode.Zl, unicode.Zp, unicode.Zs) {
		return false
	}
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S) // 其余（未分配 Cn）不可打印
}

// pyStrRepr：repr(str)：默认单引号，含 ' 不含 " 时用双引号
func pyStrRepr(s string) string {
	q := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == rune(q) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < ' ' || r == 0x7f:
			b.WriteString(fmt.Sprintf(`\x%02x`, r))
		case r < 0x7f || pyPrintable(r):
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(fmt.Sprintf(`\x%02x`, r))
		case r < 0x10000:
			b.WriteString(fmt.Sprintf(`\u%04x`, r))
		default:
			b.WriteString(fmt.Sprintf(`\U%08x`, r))
		}
	}
	b.WriteByte(q)
	return b.String()
}

// pyNum：数字转 float64；第二个返回值表示是不是数字（bool 在 Python 里也是 int）
func pyNum(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

func isPyInt(v any) bool {
	switch v.(type) {
	case int, int64, bool:
		return true
	}
	return false
}

// pyRoundN：round(x, n)（float 用正确舍入的十进制；int 原样）
func pyRoundN(v any, n int) any {
	if isPyInt(v) {
		f, _ := pyNum(v)
		return int64(f)
	}
	f, _ := pyNum(v)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', n, 64), 64)
	if r == 0 {
		r = math.Copysign(0, f)
	}
	return r
}

// pyRound0：round(x) → int（银行家舍入）
func pyRound0(f float64) int64 { return int64(math.RoundToEven(f)) }

// pyFloorDiv：float // float（照 CPython float_floor_div）
func pyFloorDiv(vx, wx float64) float64 {
	mod := math.Mod(vx, wx)
	div := (vx - mod) / wx
	if mod != 0 {
		if (wx < 0) != (mod < 0) {
			div -= 1.0
		}
	}
	var fd float64
	if div != 0 {
		fd = math.Floor(div)
		if div-fd > 0.5 {
			fd += 1.0
		}
	} else {
		fd = math.Copysign(0, vx/wx)
	}
	return fd
}

// pySumF：sum(生成器)，空的时候 Python 给的是 int 0
func pySumF(vals []float64) any {
	if len(vals) == 0 {
		return int64(0)
	}
	return pyNeumaier(vals)
}

// pyNeumaier：Python 3.12+ 的 sum() 对 float 用 Neumaier 补偿求和（照 bltinmodule.c）；+= 累加不是这个
func pyNeumaier(vals []float64) float64 {
	f, c := 0.0, 0.0
	for _, x := range vals {
		t := f + x
		if math.Abs(f) >= math.Abs(x) {
			c += (f - t) + x
		} else {
			c += (x - t) + f
		}
		f = t
	}
	if c != 0 && !math.IsInf(c, 0) && !math.IsNaN(c) {
		f += c
	}
	return f
}

func pyF(v any) float64 {
	f, _ := pyNum(v)
	return f
}

// pyHTMLEscape：html.escape(s, quote=True)
func pyHTMLEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;")
	return r.Replace(s)
}

// pyNetloc：urllib.parse.urlsplit(url).netloc（够用的近似；坏的 IPv6 方括号返回 ok=false）
func pyNetloc(raw string) (string, bool) {
	u := strings.TrimLeftFunc(raw, func(r rune) bool { return r <= ' ' })
	u = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(u)
	if i := strings.IndexByte(u, ':'); i > 0 {
		c0 := u[0]
		if c0 < 0x80 && ((c0 >= 'a' && c0 <= 'z') || (c0 >= 'A' && c0 <= 'Z')) {
			ok := true
			for _, c := range u[:i] {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.') {
					ok = false
					break
				}
			}
			if ok {
				u = u[i+1:]
			}
		}
	}
	if !strings.HasPrefix(u, "//") {
		return "", true
	}
	rest := u[2:]
	end := len(rest)
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		end = j
	}
	nl := rest[:end]
	if strings.Contains(nl, "[") != strings.Contains(nl, "]") {
		return "", false
	}
	return nl, true
}

func pyDomain(raw string) string {
	if raw == "" {
		return ""
	}
	h, ok := pyNetloc(raw)
	if !ok {
		return ""
	}
	h = strings.ToLower(h)
	return strings.TrimPrefix(h, "www.")
}
