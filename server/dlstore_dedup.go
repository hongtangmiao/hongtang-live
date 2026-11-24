package main

import (
	"math"
	"time"
)

// subtractIntervals：从 base 中扣掉已被 sub 覆盖的时间段（两者均需已排序合并）
func subtractIntervals(base, sub []ival) []ival {
	if len(base) == 0 || len(sub) == 0 {
		return base
	}
	var out []ival
	j := 0
	for _, b := range base {
		cur := b.s
		for j < len(sub) && !sub[j].e.After(cur) {
			j++
		}
		for k := j; k < len(sub) && sub[k].s.Before(b.e); k++ {
			if sub[k].s.After(cur) {
				out = append(out, ival{cur, sub[k].s})
			}
			if sub[k].e.After(cur) {
				cur = sub[k].e
			}
			if !cur.Before(b.e) {
				break
			}
		}
		if cur.Before(b.e) {
			out = append(out, ival{cur, b.e})
		}
	}
	return out
}

// dayBounds：某一天的起止时间（今天截到当前时刻，避免累计时长超过当天已过去的时间）
func dayBounds(day string) (time.Time, time.Time, bool) {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	ds := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	de := ds.AddDate(0, 0, 1)
	if day == todayKey() {
		if now := time.Now().In(loc); now.Before(de) {
			de = now
		}
	}
	return ds, de, true
}

type liveSpan struct {
	ts, endTS, host, cat string
	dur                  int64
}

// dedupLiveTotals：多设备同时上报时按时间段去重（电脑优先，避免同一时段被电脑+手机重复累加）
func dedupLiveTotals(day string, spans []liveSpan) (int64, map[string]int64) {
	ds, de, hasBounds := dayBounds(day)
	type item struct {
		iv   ival
		cat  string
		prio int
	}
	var items []item
	for _, r := range spans {
		s, okS := pyAware(r.ts)
		if !okS {
			continue
		}
		e, okE := pyAware(or(r.endTS, r.ts))
		if !okE || e.Before(s) {
			e = s
		}
		if r.dur > 0 {
			if e2 := s.Add(time.Duration(r.dur) * time.Second); e2.After(e) {
				e = e2
			}
		}
		if hasBounds {
			if s.Before(ds) {
				s = ds
			}
			if e.After(de) {
				e = de
			}
		}
		if !e.After(s) {
			continue
		}
		items = append(items, item{ival{s, e}, or(r.cat, "其他"), hostPriority(r.host)})
	}
	catSec := map[string]int64{}
	var total int64
	var covered []ival
	for prio := 2; prio >= 0; prio-- {
		for _, it := range items {
			if it.prio != prio {
				continue
			}
			eff := subtractIntervals([]ival{it.iv}, covered)
			if len(eff) == 0 {
				continue
			}
			var sec float64
			for _, x := range eff {
				sec += x.e.Sub(x.s).Seconds()
			}
			add := int64(math.Round(sec))
			if add > 0 {
				total += add
				catSec[it.cat] += add
			}
			covered = mergeIntervals(append(covered, eff...))
		}
	}
	return total, catSec
}
