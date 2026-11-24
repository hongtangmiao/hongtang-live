package main

import (
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* ---------------- サーバー統計（本家 /api/stats/* 互換） ---------------- */

const (
	hist      = 60     // 每个指标保留的采样点
	pcStaleMs = 90_000 // PC 上报超过这个时间算失联
)

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

/* ---- 本家と同じ整形 ---- */

func fmtInt(n float64) string {
	v := int64(jsRound(n))
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
func fmtGB(b float64) string { return fixed(b/1073741824, 1) + " GB" }
func fmtGBShort(b float64) string {
	if b < 1073741824 {
		return jsNum(maxF(0, jsRound(b/1048576))) + " MB"
	}
	return fixed(b/1073741824, 1) + " GB"
}
func fmtPct(n float64) string       { return jsNum(jsRound(n)) + "%" }
func fmtGHz(n float64) string       { return fixed(n, 2) + " GHz" }
func fmtMBfromKB(kb float64) string { return fixed(kb/1024, 1) + " MB" }
func fmtUptime(ms float64) string {
	s := int64(math.Floor(ms / 1000))
	d, h, m, sec := s/86400, s%86400/3600, s%3600/60, s%60
	return strconv.FormatInt(d, 10) + ":" + pad2(h) + ":" + pad2(m) + ":" + pad2(sec)
}

/* ---- PC（采集器上报的完整快照） ---- */

type pcState struct {
	mu      sync.Mutex
	at      int64
	snap    M
	rings   map[string][]M
	savedAt int64
}

/*
 * 每台电脑一个槽位：「pc」是旧的 C# 采集器 / 第一台配对的电脑，之后配对的是「pc-<设备 id>」。
 * 槽位 id 就是统计页网址里的 host=，所以旧链接 host=pc 不变。
 */
var (
	pcsMu sync.Mutex
	pcs   = map[string]*pcState{}
)

var reSlotID = regexp.MustCompile(`^pc(-[0-9a-f]{8})?$`)

func newPCState() *pcState {
	return &pcState{rings: map[string][]M{"cpu": {}, "memory": {}, "gpu": {}, "network": {}}}
}

// pcSlot：取（或新建）一个槽位
func pcSlot(id string) *pcState {
	pcsMu.Lock()
	defer pcsMu.Unlock()
	st := pcs[id]
	if st == nil {
		st = newPCState()
		pcs[id] = st
	}
	return st
}

// pcSlotIf：只取已有的
func pcSlotIf(id string) *pcState {
	pcsMu.Lock()
	defer pcsMu.Unlock()
	return pcs[id]
}

func pcSlotIDs() []string {
	pcsMu.Lock()
	defer pcsMu.Unlock()
	ids := make([]string, 0, len(pcs))
	for id := range pcs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func pcSnapFile(id string) string {
	if id == "pc" {
		return filepath.Join(dataDir, "pc-snap.json")
	}
	return filepath.Join(dataDir, "pc-snap-"+strings.TrimPrefix(id, "pc-")+".json")
}

func initPC() {
	pcsMu.Lock()
	pcs = map[string]*pcState{"pc": newPCState()}
	pcsMu.Unlock()
	files, _ := filepath.Glob(filepath.Join(dataDir, "pc-snap*.json"))
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".json")
		id := "pc"
		if base != "pc-snap" {
			id = "pc-" + strings.TrimPrefix(base, "pc-snap-")
		}
		if !reSlotID.MatchString(id) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var raw M
		if decodeJSON(b, &raw) != nil {
			continue
		}
		if at := toNum(raw["at"]); at > 0 && asM(raw["snapshot"]) != nil {
			st := pcSlot(id)
			st.at, st.snap = int64(at), asM(raw["snapshot"])
		}
	}
}

func pcPersistSlot(id string) {
	st := pcSlotIf(id)
	if st == nil {
		return
	}
	st.mu.Lock()
	if st.snap == nil {
		st.mu.Unlock()
		return
	}
	data := jsonBytes(M{"at": st.at, "snapshot": st.snap})
	st.mu.Unlock()
	if err := writeFileAtomic(pcSnapFile(id), data, 0o644); err != nil {
		log.Printf("[stats] %s save failed: %v", id, err)
	}
}

func pcPersist() {
	for _, id := range pcSlotIDs() {
		pcPersistSlot(id)
	}
}

func pick(src M, keys ...string) M {
	out := M{}
	for _, k := range keys {
		if v, ok := src[k]; ok {
			out[k] = v
		}
	}
	return out
}

func orNil(v any) any {
	if !truthy(v) {
		return nil
	}
	return v
}

// pushRing：环形缓冲，只留最近 hist 个（电脑和服务器两边共用）
func pushRing(rings map[string][]M, name string, item M) {
	r := append(rings[name], item)
	if len(r) > hist {
		r = r[len(r)-hist:]
	}
	rings[name] = r
}

var pcSections = []string{"cpu", "memory", "gpu", "disk", "network", "windows"}

// pcReport：旧接口，写进「pc」槽位
func pcReport(raw M) (string, int64) { return pcReportTo("pc", raw) }

// pcReportTo：没带的分区（键都没有）沿用上一次的，托盘程序可以只在变化时才发硬件信息这类大块头；
// 公网 IP 不保存（统计页是公开的）
func pcReportTo(id string, raw M) (string, int64) {
	pc := pcSlot(id)
	host := toStr(raw["host"])
	if !truthy(raw["host"]) {
		host = "PC"
	}
	if rs := []rune(host); len(rs) > 48 {
		host = string(rs[:48])
	}
	snap := M{"host": host}
	pc.mu.Lock()
	for _, k := range pcSections {
		v, has := raw[k]
		if !has && pc.snap != nil {
			v = pc.snap[k]
		}
		snap[k] = orNil(v)
	}
	if n := asM(snap["network"]); n != nil {
		if _, a := n["publicIpv4"]; a {
			n = clone(n)
			delete(n, "publicIpv4")
			delete(n, "publicIpv6")
			snap["network"] = n
		}
		if _, b := n["publicIpv6"]; b {
			n = clone(n)
			delete(n, "publicIpv6")
			snap["network"] = n
		}
	}
	pc.snap = snap
	pc.at = time.Now().UnixMilli()
	sampled := pc.at / 1000
	if c := asM(snap["cpu"]); c != nil {
		pushRing(pc.rings, "cpu", M{"data": pick(c, "usagePercent", "logicalProcessorUsage"), "sampledAt": sampled})
	}
	if m := asM(snap["memory"]); m != nil {
		pushRing(pc.rings, "memory", M{"data": pick(m, "used"), "sampledAt": sampled})
	}
	if g := asM(snap["gpu"]); g != nil {
		mem := M{}
		if gm := asM(g["memory"]); gm != nil {
			mem = pick(gm, "dedicatedUsed", "sharedUsed")
		}
		eng := g["engines"]
		if !truthy(eng) {
			eng = []any{}
		}
		pushRing(pc.rings, "gpu", M{"data": M{"memory": mem, "engines": eng}, "sampledAt": sampled})
	}
	if n := asM(snap["network"]); n != nil {
		ts := n["timestamp"]
		if !truthy(ts) {
			ts = pc.at
		}
		ads := []M{}
		for _, a := range asA(n["adapters"]) {
			am := asM(a)
			if am == nil {
				continue
			}
			x := pick(am, "name", "bytesReceived", "bytesSent")
			x["recvBytesPerSec"], x["sentBytesPerSec"] = numOr0(am["recvBytesPerSec"]), numOr0(am["sentBytesPerSec"])
			ads = append(ads, x)
		}
		pushRing(pc.rings, "network", M{"data": M{"timestamp": ts, "adapters": ads}, "sampledAt": sampled})
	}
	at := pc.at
	save := at-pc.savedAt > 30_000 // 4 秒一次的上报不必每次落盘
	if save {
		pc.savedAt = at
	}
	pc.mu.Unlock()
	if save {
		pcPersistSlot(id)
	}
	return host, at
}

func numOr0(v any) any {
	if truthy(v) {
		return v
	}
	return 0
}
