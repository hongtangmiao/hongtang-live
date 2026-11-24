package main

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

/*
 * 把 ActivityWatch 导出的一天原始事件整理成日报结构（逐段对照 daylog/app/agg.py）。
 * 与原版唯一的有意不同：first/last/gaps 的钟点换算成本地时区（原版直接用 AW 的 UTC 时间，日报上的「活动区间」是错的）。
 */

type awEv struct {
	s, e time.Time
	data M
}

// toMicro：Python datetime 只有微秒精度
func toMicro(t time.Time) time.Time { return t.Round(time.Microsecond) }

func pySeconds(d time.Duration) float64 { return float64(d.Microseconds()) / 1e6 }

func pyTimedelta(sec float64) time.Duration {
	return time.Duration(math.RoundToEven(sec*1e6)) * time.Microsecond
}

func parseAWTime(s string, tz *time.Location) (time.Time, bool) {
	s = pyStrip(s)
	if strings.HasSuffix(s, "Z") {
		s = s[:len(s)-1] + "+00:00"
	}
	t, hasTZ, ok := pyFromISO(s)
	if !ok {
		return t, false
	}
	if !hasTZ {
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), tz)
	}
	return toMicro(t), true
}

func normEvents(events []any, ds, de time.Time, tz *time.Location) []awEv {
	var out []awEv
	for _, x := range events {
		ev := asM(x)
		if ev == nil || !pyTruthy(ev["timestamp"]) {
			continue
		}
		s, ok := parseAWTime(toStr(ev["timestamp"]), tz)
		if !ok {
			continue
		}
		dur := 0.0
		if pyTruthy(ev["duration"]) {
			dur, _ = pyFloat(ev["duration"])
		}
		if dur <= 0 {
			continue
		}
		e := s.Add(pyTimedelta(dur))
		if !e.After(ds) || !s.Before(de) {
			continue
		}
		if ds.After(s) {
			s = ds
		}
		if de.Before(e) {
			e = de
		}
		if !e.After(s) {
			continue
		}
		data := asM(ev["data"])
		if data == nil {
			data = M{}
		}
		out = append(out, awEv{s, e, data})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].s.Before(out[j].s) })
	return out
}

type ival struct{ s, e time.Time }

func mergeIntervals(iv []ival) []ival {
	if len(iv) == 0 {
		return nil
	}
	iv = append([]ival(nil), iv...)
	sort.SliceStable(iv, func(i, j int) bool {
		if !iv[i].s.Equal(iv[j].s) {
			return iv[i].s.Before(iv[j].s)
		}
		return iv[i].e.Before(iv[j].e)
	})
	merged := []ival{iv[0]}
	for _, x := range iv[1:] {
		last := &merged[len(merged)-1]
		if !x.s.After(last.e) {
			if x.e.After(last.e) {
				last.e = x.e
			}
		} else {
			merged = append(merged, x)
		}
	}
	return merged
}

type rawSeg struct {
	s, e               time.Time
	app, title, domain string
}

func maxT(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func minT(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// clipSegs：只保留落在 covers（已合并、有序）里的部分
func clipSegs(segs []rawSeg, covers []ival) []rawSeg {
	if len(covers) == 0 {
		return nil
	}
	var out []rawSeg
	j := 0
	for _, sg := range segs {
		k := j
		for k < len(covers) {
			cs, ce := covers[k].s, covers[k].e
			if !ce.After(sg.s) {
				k++
				continue
			}
			if !cs.Before(sg.e) {
				break
			}
			a, b := maxT(sg.s, cs), minT(sg.e, ce)
			if b.After(a) {
				x := sg
				x.s, x.e = a, b
				out = append(out, x)
			}
			if !ce.Before(sg.e) {
				break
			}
			k++
		}
		j = k - 1
		if j < 0 {
			j = 0
		}
	}
	return out
}

type webSeg struct {
	s, e time.Time
	dom  string
}

func pickDomain(s, e time.Time, web []webSeg) string {
	var order []string
	acc := map[string]float64{}
	for _, w := range web {
		if !w.e.After(s) || !w.s.Before(e) {
			continue
		}
		ov := pySeconds(minT(w.e, e).Sub(maxT(w.s, s)))
		if ov <= 0 {
			continue
		}
		if _, ok := acc[w.dom]; !ok {
			order = append(order, w.dom)
		}
		acc[w.dom] += ov
	}
	best, bestSec := "", 0.0
	for _, d := range order {
		if acc[d] > bestSec {
			best, bestSec = d, acc[d]
		}
	}
	return best
}

/* ---------------- 规则（分类 + 黑名单，re.M） ---------------- */

type aggRules struct {
	cats  []aggCat
	block []*regexp2.Regexp
}

type aggCat struct {
	name string
	pats []*regexp2.Regexp
}

func compileM(p string) *regexp2.Regexp {
	r, err := regexp2.Compile(p, regexp2.Multiline)
	if err != nil {
		return nil
	}
	r.MatchTimeout = 200 * time.Millisecond
	return r
}

func newAggRules(cfg M, blocklist []string) *aggRules {
	r := &aggRules{}
	for _, c := range asA(cfg["categories"]) {
		cm := asM(c)
		if cm == nil {
			continue
		}
		name := "其他"
		if v, ok := cm["name"]; ok {
			name = pyStr(v)
		}
		ac := aggCat{name: name}
		for _, p := range asA(cm["patterns"]) {
			if re := compileM(toStr(p)); re != nil {
				ac.pats = append(ac.pats, re)
			}
		}
		r.cats = append(r.cats, ac)
	}
	for _, p := range blocklist {
		if re := compileM(p); re != nil {
			r.block = append(r.block, re)
		}
	}
	return r
}

func (r *aggRules) blocked(texts ...string) bool {
	if len(r.block) == 0 {
		return false
	}
	var parts []string
	for _, t := range texts {
		if t != "" {
			parts = append(parts, t)
		}
	}
	blob := strings.Join(parts, "\n")
	for _, re := range r.block {
		if reSearch(re, blob) {
			return true
		}
	}
	return false
}

func (r *aggRules) category(app, title, domain string) string {
	blob := app + "\n" + domain + "\n" + title
	for _, c := range r.cats {
		for _, re := range c.pats {
			if reSearch(re, blob) {
				return c.name
			}
		}
	}
	return "其他"
}

/* ---------------- 有序累加 ---------------- */

type accF struct {
	order []string
	m     map[string]float64
}

func newAccF() *accF { return &accF{m: map[string]float64{}} }

func (a *accF) add(k string, v float64) {
	if _, ok := a.m[k]; !ok {
		a.order = append(a.order, k)
		a.m[k] = 0.0
	}
	a.m[k] += v
}

type kvF struct {
	k string
	v float64
}

// sortedDesc：sorted(d.items(), key=lambda kvF: -kvF[1])（稳定）
func (a *accF) sortedDesc() []kvF {
	out := make([]kvF, 0, len(a.order))
	for _, k := range a.order {
		out = append(out, kvF{k, a.m[k]})
	}
	sort.SliceStable(out, func(i, j int) bool { return -out[i].v < -out[j].v })
	return out
}

// firstMax：max(d.items(), key=lambda kvF: kvF[1])，并列时取先插入的
func (a *accF) firstMax() (string, float64) {
	bk, bv := "", math.Inf(-1)
	for i, k := range a.order {
		if i == 0 || a.m[k] > bv {
			bk, bv = k, a.m[k]
		}
	}
	return bk, bv
}
