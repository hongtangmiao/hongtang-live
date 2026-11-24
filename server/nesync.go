package main

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
 * nesync — 网易云数据的服务器端快照（与旧版 nesync.js 的 ne-cache.json 格式兼容）
 *
 * 访客打开音乐页只读这里的快照，不触发上游；后台按接口分别定时刷新（请求之间隔几秒、一个一个来）。
 *   record/recent/song  1 小时
 *   user/record         6 小时（周榜 / 总榜）
 *   user/account        6 小时（顺便校验 cookie 是否失效）
 *   其他（歌单、喜欢、歌单详情、歌曲详情）  netease.refresh_hours，默认 12 小时
 */

type nsEntry struct {
	P    string            `json:"p"`
	Q    map[string]string `json:"q"`
	Fp   string            `json:"fp"`
	Text string            `json:"text"`
	Ts   int64             `json:"ts"`
	Hit  int64             `json:"hit"`
}

type nsStat struct {
	LastRun   int64  `json:"lastRun"`
	LastOk    int64  `json:"lastOk"`
	Refreshed int    `json:"refreshed"`
	Failed    int    `json:"failed"`
	Note      string `json:"note"`
}

const (
	nsHour       = int64(3600_000)
	nsKeep       = 30 * 24 * nsHour // 30 天没人看的条目不再刷新
	nsMissBudget = 10               // 没有快照时，10 分钟内最多替访客打 10 次上游
	nsMissWindow = 10 * time.Minute
	nsMaxFails   = 3 // 一轮里连续失败 3 次就当上游挂了
	nsMaxEntries = 300
)

var (
	nsMu        sync.Mutex
	nsEntries   = map[string]*nsEntry{}
	nsStatV     nsStat
	nsMissLog   []time.Time
	nsRunning   bool
	nsInflight  = map[string]chan struct{}{}
	nsSaveTimer *time.Timer
	nsKickTimer *time.Timer
	nsFile      string
	nsGapMin    = 2500 * time.Millisecond
)

func nsTTL(p string) int64 {
	switch p {
	case "record/recent/song":
		return nsHour
	case "user/record", "user/account":
		return 6 * nsHour
	}
	return int64(conf().NetEase.RefreshHours * float64(nsHour))
}

func nsFp(cookie string) string {
	if cookie == "" {
		return ""
	}
	sum := md5.Sum([]byte(cookie))
	return hex.EncodeToString(sum[:])[:8]
}

// nsNorm：去掉 timestamp / cookie / bind_key —— 前端请求和后台预取得到同一个键
func nsNorm(params map[string]string) map[string]string {
	q := map[string]string{}
	for k, v := range params {
		if k == "timestamp" || k == "cookie" || k == "bind_key" || v == "" {
			continue
		}
		q[k] = v
	}
	return q
}

func nsKey(p string, q map[string]string, fp string) string { return p + "?" + qsMap(q) + "#" + fp }

func nsLoad() {
	b, err := os.ReadFile(nsFile)
	if err != nil {
		return
	}
	var raw struct {
		Stat    nsStat     `json:"stat"`
		Entries []*nsEntry `json:"entries"`
	}
	if decodeJSON(b, &raw) != nil {
		return
	}
	for _, e := range raw.Entries {
		if e != nil && e.P != "" && e.Ts != 0 {
			if e.Q == nil {
				e.Q = map[string]string{}
			}
			nsEntries[nsKey(e.P, e.Q, e.Fp)] = e
		}
	}
	nsStatV = raw.Stat
}

func nsSaveNow() {
	nsMu.Lock()
	list := make([]*nsEntry, 0, len(nsEntries))
	for _, e := range nsEntries {
		list = append(list, e)
	}
	data := jsonBytes(M{"v": 1, "stat": nsStatV, "entries": list})
	nsMu.Unlock()
	if err := writeFileAtomic(nsFile, data, 0o600); err != nil {
		log.Printf("[ne-sync] save failed: %v", err)
	}
}

func nsSaveSoon() {
	nsMu.Lock()
	defer nsMu.Unlock()
	if nsSaveTimer != nil {
		return
	}
	nsSaveTimer = time.AfterFunc(3*time.Second, func() {
		nsMu.Lock()
		nsSaveTimer = nil
		nsMu.Unlock()
		nsSaveNow()
	})
}

/* 只存需要的部分：歌单详情动辄几百 KB，前端只用前 60 首 */
func nsSlim(p string, d M) {
	if p == "playlist/detail" || p == "song/detail" {
		delete(d, "privileges")
	}
	if pl := asM(d["playlist"]); p == "playlist/detail" && pl != nil {
		for _, k := range []string{"tracks", "trackIds"} {
			if a := asA(pl[k]); len(a) > 100 {
				pl[k] = a[:100]
			}
		}
	}
}

type neCodeErr struct{ ne M }

func (e *neCodeErr) Error() string { return "upstream code " + toStr(e.ne["code"]) }

// nsFetchOne：拉一次上游并存快照；code 不是 200 的一律不存
func nsFetchOne(p string, q map[string]string, cookie string) (M, error) {
	params := map[string]string{}
	for k, v := range q {
		params[k] = v
	}
	if cookie != "" {
		params["cookie"] = cookie
	}
	params["timestamp"] = nowStr()
	r, err := upstream(p, qsMap(params), "", "")
	if err != nil {
		return nil, err
	}
	var d M
	if decodeJSON([]byte(r.text), &d) != nil || d == nil {
		return nil, errors.New("bad json")
	}
	if c, ok := d["code"]; ok && toStr(c) != "200" {
		return nil, &neCodeErr{d}
	}
	nsSlim(p, d)
	f := nsFp(cookie)
	k := nsKey(p, q, f)
	now := time.Now().UnixMilli()
	nsMu.Lock()
	hit := now
	if old := nsEntries[k]; old != nil && old.Hit != 0 {
		hit = old.Hit
	}
	nsEntries[k] = &nsEntry{P: p, Q: q, Fp: f, Text: string(jsonBytes(d)), Ts: now, Hit: hit}
	nsMu.Unlock()
	nsSaveSoon()
	return d, nil
}

/* ---------------- 给请求用 ---------------- */

func nsGet(p string, params map[string]string, cookie string) *nsEntry {
	nsMu.Lock()
	defer nsMu.Unlock()
	e := nsEntries[nsKey(p, nsNorm(params), nsFp(cookie))]
	if e == nil {
		return nil
	}
	now := time.Now().UnixMilli()
	if e.Fp == "" && now-e.Ts > nsTTL(p) { // 匿名请求（搜索等）不做定时刷新，过期就当没有
		return nil
	}
	e.Hit = now
	return e
}

func nsEvictLocked() {
	if len(nsEntries) <= nsMaxEntries {
		return
	}
	type ke struct {
		k string
		e *nsEntry
	}
	list := make([]ke, 0, len(nsEntries))
	for k, e := range nsEntries {
		list = append(list, ke{k, e})
	}
	at := func(e *nsEntry) int64 {
		if e.Hit != 0 {
			return e.Hit
		}
		return e.Ts
	}
	sort.Slice(list, func(i, j int) bool { // 先淘汰匿名的，再按最近访问时间淘汰
		ai, aj := list[i].e.Fp != "", list[j].e.Fp != ""
		if ai != aj {
			return !ai
		}
		return at(list[i].e) < at(list[j].e)
	})
	for i := 0; len(nsEntries) > nsMaxEntries && i < len(list); i++ {
		delete(nsEntries, list[i].k)
	}
}

// nsMiss：没有快照（比如第一次点开某个歌单）时限额内替访客拉一次
func nsMiss(p string, params map[string]string, cookie string) (*nsEntry, error) {
	q := nsNorm(params)
	k := nsKey(p, q, nsFp(cookie))
	nsMu.Lock()
	if ch, ok := nsInflight[k]; ok {
		nsMu.Unlock()
		<-ch
		nsMu.Lock()
		defer nsMu.Unlock()
		return nsEntries[k], nil
	}
	now := time.Now()
	keep := nsMissLog[:0]
	for _, t := range nsMissLog {
		if now.Sub(t) < nsMissWindow {
			keep = append(keep, t)
		}
	}
	nsMissLog = keep
	if len(nsMissLog) >= nsMissBudget {
		nsMu.Unlock()
		return nil, nil
	}
	nsMissLog = append(nsMissLog, now)
	ch := make(chan struct{})
	nsInflight[k] = ch
	nsMu.Unlock()

	_, err := nsFetchOne(p, q, cookie)

	nsMu.Lock()
	delete(nsInflight, k)
	close(ch)
	nsEvictLocked()
	e := nsEntries[k]
	if e != nil {
		e.Hit = time.Now().UnixMilli()
	}
	nsMu.Unlock()
	if err != nil {
		return nil, err
	}
	return e, nil
}

/* ---------------- 后台刷新 ---------------- */

type nsReq struct {
	p string
	q map[string]string
}

// nsSeeds 与前端 loadMusic() 的请求参数保持一致，否则键对不上就白存了
func nsSeeds(uid string) []nsReq {
	return []nsReq{
		{"user/account", map[string]string{}},
		{"record/recent/song", map[string]string{"limit": "30"}},
		{"user/record", map[string]string{"uid": uid, "type": "1"}},
		{"user/record", map[string]string{"uid": uid, "type": "0"}},
		{"user/playlist", map[string]string{"uid": uid, "limit": "20"}},
		{"likelist", map[string]string{"uid": uid}},
	}
}

func nsRead(p string, q map[string]string, fp string) M {
	nsMu.Lock()
	e := nsEntries[nsKey(p, q, fp)]
	nsMu.Unlock()
	if e == nil {
		return nil
	}
	var d M
	if decodeJSON([]byte(e.Text), &d) != nil {
		return nil
	}
	return d
}

// nsDerived：喜欢列表前 30 首的详情 + 每个非空歌单的详情
func nsDerived(ofp, uid string) []nsReq {
	var out []nsReq
	if likes := nsRead("likelist", map[string]string{"uid": uid}, ofp); likes != nil {
		var ids []string
		for i, x := range asA(likes["ids"]) {
			if i >= 30 {
				break
			}
			ids = append(ids, toStr(x))
		}
		if len(ids) > 0 {
			out = append(out, nsReq{"song/detail", map[string]string{"ids": strings.Join(ids, ",")}})
		}
	}
	if pls := nsRead("user/playlist", map[string]string{"uid": uid, "limit": "20"}, ofp); pls != nil {
		for _, x := range asA(pls["playlist"]) {
			pl := asM(x)
			if pl != nil && truthy(pl["id"]) && toNum(pl["trackCount"]) > 0 {
				out = append(out, nsReq{"playlist/detail", map[string]string{"id": toStr(pl["id"])}})
			}
		}
	}
	return out
}

// nsKnown：站点账号视角只允许读固定的参数组合，防止借号查别人的 uid / 歌单，或把随机参数塞进定时刷新
func nsKnown(p string, params map[string]string) bool {
	cookie, _ := getOwner()
	uid := ownerUID()
	if cookie == "" || uid == "" {
		return false
	}
	want := nsKey(p, nsNorm(params), "")
	for _, r := range append(nsSeeds(uid), nsDerived(nsFp(cookie), uid)...) {
		if nsKey(r.p, r.q, "") == want {
			return true
		}
	}
	return false
}

func nsTick(reason string) {
	cookie, _ := getOwner()
	uid := ownerUID()
	if cookie == "" || uid == "" {
		return
	}
	nsMu.Lock()
	if nsRunning {
		nsMu.Unlock()
		return
	}
	nsRunning = true
	ofp := nsFp(cookie)
	start := time.Now()
	now := start.UnixMilli()
	nsStatV.LastRun = now
	// 换过账号的旧快照、30 天没人看的、过期的匿名条目：删掉
	for k, e := range nsEntries {
		at := e.Hit
		if at == 0 {
			at = e.Ts
		}
		if (e.Fp != "" && e.Fp != ofp) || now-at > nsKeep || (e.Fp == "" && now-e.Ts > nsTTL(e.P)) {
			delete(nsEntries, k)
		}
	}
	nsMu.Unlock()

	done, failed, streak := 0, 0, 0
	due := func(r nsReq, f string) bool {
		nsMu.Lock()
		e := nsEntries[nsKey(r.p, r.q, f)]
		nsMu.Unlock()
		return e == nil || now-e.Ts > nsTTL(r.p)
	}
	run := func(r nsReq, f string) bool {
		if !due(r, f) {
			return true
		}
		if done+failed > 0 {
			time.Sleep(nsGapMin + time.Duration(rand.Int64N(int64(nsGapMin))))
		}
		ck := ""
		if f == ofp {
			ck = cookie
		}
		d, err := nsFetchOne(r.p, r.q, ck)
		if err == nil {
			if r.p == "user/account" {
				if prof := d["profile"]; prof != nil {
					setOwnerProfile(prof)
				} else { // 上游明确说没登录：解绑
					nsMu.Lock()
					nsStatV.Note = "cookie 失效，已解绑"
					nsMu.Unlock()
					log.Printf("[ne-sync] 站点账号 cookie 失效，已解绑")
					clearOwner()
					return false
				}
			}
			done++
			streak = 0
			return true
		}
		failed++
		streak++
		var ce *neCodeErr
		if r.p == "user/account" && errors.As(err, &ce) && toStr(ce.ne["code"]) == "301" {
			nsMu.Lock()
			nsStatV.Note = "cookie 失效，已解绑"
			nsMu.Unlock()
			log.Printf("[ne-sync] 站点账号 cookie 失效，已解绑")
			clearOwner()
			return false
		}
		return streak < nsMaxFails
	}

	stopped := false
	for _, r := range nsSeeds(uid) {
		if !run(r, ofp) {
			stopped = true
			break
		}
	}
	if !stopped {
		for _, r := range nsDerived(ofp, uid) {
			if !run(r, ofp) {
				stopped = true
				break
			}
		}
	}
	if !stopped { // 访客点开过、但不在上面列表里的也按期刷新
		nsMu.Lock()
		var rest []nsReq
		for _, e := range nsEntries {
			if e.Fp == ofp {
				rest = append(rest, nsReq{e.P, e.Q})
			}
		}
		nsMu.Unlock()
		for _, r := range rest {
			if !run(r, ofp) {
				stopped = true
				break
			}
		}
	}

	nsMu.Lock()
	if !stopped && failed == 0 {
		nsStatV.Note = ""
	}
	nsRunning = false
	nsStatV.Refreshed, nsStatV.Failed = done, failed
	if done > 0 && failed == 0 {
		nsStatV.LastOk = time.Now().UnixMilli()
	}
	total := len(nsEntries)
	nsMu.Unlock()
	if done > 0 || failed > 0 {
		log.Printf("[ne-sync] %s: 刷新 %d 项，失败 %d 项，用时 %ds，快照共 %d 项", reason, done, failed, int(time.Since(start).Seconds()), total)
	}
	nsSaveSoon()
}

// nsKick：刚登录 / 换账号后 15 秒补拉一轮
func nsKick() {
	nsMu.Lock()
	defer nsMu.Unlock()
	if nsKickTimer != nil {
		nsKickTimer.Stop()
	}
	nsKickTimer = time.AfterFunc(15*time.Second, func() { nsTick("login") })
}

func nsStatus() M {
	nsMu.Lock()
	defer nsMu.Unlock()
	now := time.Now().UnixMilli()
	var oldest int64
	for _, e := range nsEntries {
		if now-e.Ts > oldest {
			oldest = now - e.Ts
		}
	}
	iso := func(ms int64) any {
		if ms == 0 {
			return nil
		}
		return isoUTC(time.UnixMilli(ms))
	}
	out := M{"entries": len(nsEntries), "lastRun": iso(nsStatV.LastRun), "lastOk": iso(nsStatV.LastOk),
		"refreshed": nsStatV.Refreshed, "failed": nsStatV.Failed, "oldestMin": jsRound(float64(oldest) / 60000)}
	if nsStatV.Note != "" {
		out["note"] = nsStatV.Note
	}
	return out
}

func initNesync() {
	nsFile = filepath.Join(dataDir, "ne-cache.json")
	nsLoad()
	if v, err := strconv.Atoi(os.Getenv("NE_SYNC_GAP_MS")); err == nil && v > 0 {
		nsGapMin = time.Duration(v) * time.Millisecond
	}
	if os.Getenv("HL_NE_SYNC") == "0" {
		log.Printf("[ne-sync] 后台刷新已关闭（HL_NE_SYNC=0），只读现有快照")
		return
	}
	delay := 20 * time.Second // 启动后 20 秒：只补过期的，没过期的不动
	if v, err := strconv.Atoi(os.Getenv("NE_SYNC_DELAY_MS")); err == nil && v > 0 {
		delay = time.Duration(v) * time.Millisecond
	}
	time.AfterFunc(delay, func() { nsTick("start") })
	go func() {
		for range time.Tick(10 * time.Minute) {
			nsTick("timer")
		}
	}()
}
