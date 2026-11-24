package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	bgmAPI  = "https://api.bgm.tv/v0"
	bgmSite = "https://bgm.tv"
)

type animeItem struct {
	Key           string   `json:"key"`
	Status        string   `json:"status"`
	Title         string   `json:"title"`
	OrigTitle     string   `json:"origTitle"`
	Cover         string   `json:"cover"`
	FallbackCover string   `json:"fallbackCover"`
	EpWatched     float64  `json:"epWatched"`
	EpTotal       float64  `json:"epTotal"`
	Rate          float64  `json:"rate"`
	Score         float64  `json:"score"`
	Date          string   `json:"date"`
	UpdatedAt     string   `json:"updatedAt"`
	Sources       []string `json:"sources"`
	BgmID         string   `json:"bgmId"`
	BgmURL        string   `json:"bgmUrl"`
	JfNextEp      string   `json:"jfNextEp"`
}

// fetchJSON：最多两次，失败停 400ms 再试
func fetchJSON(rawURL string, hdr map[string]string, timeout time.Duration) (M, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		d, err := fetchJSONOnce(rawURL, hdr, timeout)
		if err == nil {
			return d, nil
		}
		lastErr = err
		if attempt == 0 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	return nil, lastErr
}

var (
	reAnimePunct  = regexp.MustCompile(`[！!？?：:・･\-—_~～「」『』（）()【】\[\]\s\x{3000}\x{00a0} .,，。/／]`)
	reAnimeSeason = regexp.MustCompile(`第([0-9一二三四五六七八九十]+)[期季部]`)
	reSeasonEn    = regexp.MustCompile(`season(\d+)`)
)

func normAnimeKey(s string) string {
	s = strings.ToLower(s)
	s = reAnimePunct.ReplaceAllString(s, "")
	s = reAnimeSeason.ReplaceAllString(s, "s$1")
	return reSeasonEn.ReplaceAllString(s, "s$1")
}

func fmtEpTag(season, ep any) string {
	if ep == nil {
		return ""
	}
	e := "E" + pad2(int64(toNum(ep)))
	if season != nil && toNum(season) > 1 {
		return "S" + pad2(int64(toNum(season))) + e
	}
	return e
}

/* ---------------- Bangumi (bgm.tv) ---------------- */

func fetchBgmType(colType int, status string) ([]*animeItem, error) {
	c := conf()
	hdr := map[string]string{"User-Agent": "hongtang-live/1.0"}
	if c.Bangumi.Token != "" {
		hdr["Authorization"] = "Bearer " + c.Bangumi.Token
	}
	var out []*animeItem
	limit, offset, total := 50, 0, 1<<30
	for offset < total && offset < 250 {
		u := bgmAPI + "/users/" + url.PathEscape(c.Bangumi.User) + "/collections?subject_type=2&type=" + strconv.Itoa(colType) +
			"&limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset)
		d, err := fetchJSON(u, hdr, 10*time.Second)
		if err != nil {
			return nil, err
		}
		list := asA(d["data"])
		total = int(toNum(d["total"]))
		if total == 0 {
			total = len(list)
		}
		if len(list) == 0 {
			break
		}
		for _, x := range list {
			col := asM(x)
			s := asM(col["subject"])
			if s == nil {
				s = M{}
			}
			id := strings.TrimSpace(or(toStr(s["id"]), toStr(col["subject_id"])))
			cn, jp := strings.TrimSpace(toStr(s["name_cn"])), strings.TrimSpace(toStr(s["name"]))
			title := or(or(cn, jp), "#"+id)
			orig := ""
			if cn != "" && jp != "" && cn != jp {
				orig = jp
			}
			cover := ""
			if imgs := asM(s["images"]); imgs != nil {
				cover = or(or(toStr(imgs["common"]), toStr(imgs["medium"])), toStr(imgs["large"]))
			}
			if strings.HasPrefix(cover, "http:") {
				cover = "https:" + cover[5:]
			}
			epTotal := toNum(s["eps"])
			epWatched := toNum(col["ep_status"])
			if status == "watched" && epTotal > 0 {
				epWatched = maxF(epWatched, epTotal)
			}
			key := "bgm:" + id
			if id == "" {
				key = "bgm:" + strconv.Itoa(len(out))
			}
			it := &animeItem{Key: key, Status: status, Title: title, OrigTitle: orig, Cover: cover, EpWatched: epWatched, EpTotal: epTotal,
				Rate: toNum(col["rate"]), Score: toNum(s["score"]), Date: toStr(s["date"]), UpdatedAt: toStr(col["updated_at"]),
				Sources: []string{"bgm"}, BgmID: id}
			if id != "" {
				it.BgmURL = bgmSite + "/subject/" + id
			}
			out = append(out, it)
		}
		offset += len(list)
		if len(list) < limit {
			break
		}
	}
	return out, nil
}

func fetchBgmAll() (bool, []*animeItem) {
	if conf().Bangumi.User == "" {
		return false, nil
	}
	var wg sync.WaitGroup
	var a, b []*animeItem
	var ea, eb error
	wg.Add(2)
	go func() { defer wg.Done(); a, ea = fetchBgmType(3, "watching") }()
	go func() { defer wg.Done(); b, eb = fetchBgmType(2, "watched") }()
	wg.Wait()
	if ea != nil || eb != nil {
		log.Printf("[videos] bgm fetch failed: %v %v", ea, eb)
		return false, nil
	}
	return true, append(a, b...)
}

/* ---------------- Jellyfin ---------------- */

var (
	jfMu     sync.Mutex
	jfUserID string
)

func jfHdr() map[string]string {
	return map[string]string{"Authorization": `MediaBrowser Token="` + conf().Jellyfin.Key + `"`}
}

func resetJfUser() {
	jfMu.Lock()
	jfUserID = ""
	jfMu.Unlock()
}

func resolveJfUser() (string, error) {
	jfMu.Lock()
	defer jfMu.Unlock()
	if jfUserID != "" {
		return jfUserID, nil
	}
	c := conf()
	d, err := fetchJSON(c.Jellyfin.URL+"/Users", jfHdr(), 8*time.Second)
	if err != nil {
		return "", err
	}
	users := asA(d["_"])
	if len(users) == 0 {
		return "", errors.New("no jellyfin users")
	}
	chosen := asM(users[0])
	for _, u := range users {
		if um := asM(u); um != nil && c.Jellyfin.User != "" && strings.EqualFold(toStr(um["Name"]), c.Jellyfin.User) {
			chosen = um
			break
		}
	}
	jfUserID = toStr(chosen["Id"])
	return jfUserID, nil
}

type epInfo struct{ label, last string }

func fetchJfAll() (bool, []*animeItem) {
	c := conf()
	if c.Jellyfin.URL == "" || c.Jellyfin.Key == "" {
		return false, nil
	}
	uid, err := resolveJfUser()
	if err != nil {
		resetJfUser()
		log.Printf("[videos] jellyfin fetch failed: %v", err)
		return false, nil
	}
	base := c.Jellyfin.URL
	fq := "PrimaryImageAspectRatio,ProductionYear,ProviderIds,UserData,RecursiveItemCount,OriginalTitle"
	urls := []string{
		base + "/Users/" + uid + "/Items?IncludeItemTypes=Series&Recursive=true&Fields=" + fq + "&SortBy=DatePlayed,SortName&SortOrder=Descending&Limit=250",
		base + "/Users/" + uid + "/Items/Resume?Limit=40&Recursive=true&Fields=PrimaryImageAspectRatio,ProductionYear,ProviderIds&MediaTypes=Video",
		base + "/Shows/NextUp?UserId=" + uid + "&Limit=40&Fields=PrimaryImageAspectRatio,ProductionYear,ProviderIds",
		base + "/Users/" + uid + "/Items?IncludeItemTypes=Movie&Filters=IsPlayed&Recursive=true&Fields=" + fq + "&SortBy=DatePlayed,SortName&SortOrder=Descending&Limit=100",
	}
	timeouts := []time.Duration{10 * time.Second, 8 * time.Second, 8 * time.Second, 8 * time.Second}
	res := make([]M, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range urls {
		wg.Add(1)
		go func(i int) { defer wg.Done(); res[i], errs[i] = fetchJSON(urls[i], jfHdr(), timeouts[i]) }(i)
	}
	wg.Wait()
	if errs[0] != nil && errs[3] != nil {
		resetJfUser()
		log.Printf("[videos] jellyfin fetch failed: items unreachable")
		return false, nil
	}

	resume, nextUp := map[string]epInfo{}, map[string]epInfo{}
	if errs[1] == nil {
		for _, x := range asA(res[1]["Items"]) {
			it := asM(x)
			sid := or(toStr(it["SeriesId"]), toStr(it["Id"]))
			if _, seen := resume[sid]; sid == "" || seen {
				continue
			}
			last := ""
			if ud := asM(it["UserData"]); ud != nil {
				last = toStr(ud["LastPlayedDate"])
			}
			resume[sid] = epInfo{fmtEpTag(it["ParentIndexNumber"], it["IndexNumber"]), last}
		}
	}
	if errs[2] == nil {
		for _, x := range asA(res[2]["Items"]) {
			it := asM(x)
			sid := toStr(it["SeriesId"])
			if _, seen := nextUp[sid]; sid == "" || seen {
				continue
			}
			nextUp[sid] = epInfo{label: fmtEpTag(it["ParentIndexNumber"], it["IndexNumber"])}
		}
	}
	coverOf := func(it M) string {
		if tags := asM(it["ImageTags"]); tags != nil && truthy(tags["Primary"]) {
			return "api/videos/jf-img/" + url.PathEscape(toStr(it["Id"]))
		}
		return ""
	}
	bgmOf := func(it M) (string, string) {
		id := ""
		if p := asM(it["ProviderIds"]); p != nil {
			id = strings.TrimSpace(or(toStr(p["Bangumi"]), toStr(p["bgm"])))
		}
		if id == "" {
			return "", ""
		}
		return id, bgmSite + "/subject/" + id
	}
	titles := func(it M) (string, string) {
		t, o := strings.TrimSpace(toStr(it["Name"])), strings.TrimSpace(toStr(it["OriginalTitle"]))
		if o == t {
			o = ""
		}
		return t, o
	}
	year := func(it M) string {
		if truthy(it["ProductionYear"]) {
			return toStr(it["ProductionYear"])
		}
		return ""
	}

	var out []*animeItem
	if errs[0] == nil {
		for _, x := range asA(res[0]["Items"]) {
			it := asM(x)
			ud := asM(it["UserData"])
			if ud == nil {
				ud = M{}
			}
			total := toNum(it["RecursiveItemCount"])
			unplayed := total
			if ud["UnplayedItemCount"] != nil {
				unplayed = toNum(ud["UnplayedItemCount"])
			}
			watched := maxF(0, total-unplayed)
			id := toStr(it["Id"])
			ri, hasRes := resume[id]
			ni, hasNx := nextUp[id]
			played, _ := ud["Played"].(bool)
			status := ""
			switch {
			case played && total > 0:
				status = "watched"
			case watched > 0 || toNum(ud["PlaybackPositionTicks"]) > 0 || hasRes || hasNx:
				status = "watching"
			default:
				continue
			}
			bid, burl := bgmOf(it)
			t, o := titles(it)
			ew := watched
			if status == "watched" && total > 0 {
				ew = total
			}
			next := ""
			if status == "watching" {
				next = or(ri.label, ni.label)
			}
			out = append(out, &animeItem{Key: "jf:" + id, Status: status, Title: t, OrigTitle: o, Cover: coverOf(it),
				EpWatched: ew, EpTotal: total, Score: toNum(it["CommunityRating"]), Date: year(it),
				UpdatedAt: or(toStr(ud["LastPlayedDate"]), ri.last), Sources: []string{"jellyfin"},
				BgmID: bid, BgmURL: burl, JfNextEp: next})
		}
	}
	if errs[3] == nil {
		for _, x := range asA(res[3]["Items"]) {
			it := asM(x)
			ud := asM(it["UserData"])
			if ud == nil {
				ud = M{}
			}
			bid, burl := bgmOf(it)
			t, o := titles(it)
			out = append(out, &animeItem{Key: "jf:" + toStr(it["Id"]), Status: "watched", Title: t, OrigTitle: o, Cover: coverOf(it),
				EpWatched: 1, EpTotal: 1, Score: toNum(it["CommunityRating"]), Date: year(it), UpdatedAt: toStr(ud["LastPlayedDate"]),
				Sources: []string{"jellyfin"}, BgmID: bid, BgmURL: burl})
		}
	}
	return true, out
}

/* ---------------- 智能合并 ---------------- */

func buildWall() M {
	var bgmOK, jfOK bool
	var bgm, jf []*animeItem
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); bgmOK, bgm = fetchBgmAll() }()
	go func() { defer wg.Done(); jfOK, jf = fetchJfAll() }()
	wg.Wait()

	merged := make([]*animeItem, 0, len(bgm)+len(jf))
	byID, byTitle := map[string]*animeItem{}, map[string]*animeItem{}
	for _, x := range bgm {
		c := *x
		c.Sources = append([]string(nil), x.Sources...)
		it := &c
		merged = append(merged, it)
		if it.BgmID != "" {
			byID[it.BgmID] = it
		}
		for _, k := range []string{normAnimeKey(it.Title), normAnimeKey(it.OrigTitle)} {
			if _, seen := byTitle[k]; k != "" && !seen {
				byTitle[k] = it
			}
		}
	}
	for _, j := range jf {
		var hit *animeItem
		if j.BgmID != "" {
			hit = byID[j.BgmID]
		}
		if hit == nil {
			hit = byTitle[normAnimeKey(j.Title)]
		}
		if hit == nil && j.OrigTitle != "" {
			hit = byTitle[normAnimeKey(j.OrigTitle)]
		}
		if hit == nil {
			merged = append(merged, j)
			continue
		}
		has := false
		for _, s := range hit.Sources {
			has = has || s == "jellyfin"
		}
		if !has {
			hit.Sources = append(hit.Sources, "jellyfin")
		}
		if j.Cover != "" {
			if hit.Cover == "" {
				hit.Cover = j.Cover
			} else if hit.Cover != j.Cover {
				hit.FallbackCover = j.Cover
			}
		}
		if j.EpWatched > hit.EpWatched && (hit.EpTotal == 0 || j.EpWatched <= hit.EpTotal) {
			hit.EpWatched = j.EpWatched
		}
		if hit.EpTotal == 0 && j.EpTotal > 0 {
			hit.EpTotal = j.EpTotal
		}
		if j.JfNextEp != "" && hit.Status == "watching" {
			hit.JfNextEp = j.JfNextEp
		}
		if j.UpdatedAt != "" && (hit.UpdatedAt == "" || j.UpdatedAt > hit.UpdatedAt) {
			hit.UpdatedAt = j.UpdatedAt
		}
	}
	// updatedAt 新的在前；没有时间的放最后并保持原顺序。
	// 时间相同的反转原顺序 —— 旧版 JS 比较器相等时返回 -1，V8 排出来就是这样（300 组随机数据验证过）
	idx := make(map[*animeItem]int, len(merged))
	for i, x := range merged {
		idx[x] = i
	}
	sort.SliceStable(merged, func(i, k int) bool {
		a, b := merged[i], merged[k]
		switch {
		case a.UpdatedAt != "" && b.UpdatedAt != "":
			if a.UpdatedAt != b.UpdatedAt {
				return a.UpdatedAt > b.UpdatedAt
			}
			return idx[a] > idx[b]
		case a.UpdatedAt != "":
			return true
		case b.UpdatedAt != "":
			return false
		}
		return idx[a] < idx[b]
	})
	watching, watched := 0, 0
	for _, x := range merged {
		switch x.Status {
		case "watching":
			watching++
		case "watched":
			watched++
		}
	}
	c := conf()
	profile := ""
	if c.Bangumi.User != "" {
		profile = bgmSite + "/user/" + url.PathEscape(c.Bangumi.User)
	}
	return M{
		"ok": bgmOK || jfOK || len(merged) > 0, "bgmUser": c.Bangumi.User, "bgmProfileUrl": profile,
		"sources": M{
			"bgm":      M{"enabled": c.Bangumi.User != "", "ok": bgmOK, "count": len(bgm)},
			"jellyfin": M{"enabled": c.Jellyfin.URL != "" && c.Jellyfin.Key != "", "ok": jfOK, "count": len(jf)},
		},
		"stats":   M{"watching": watching, "watched": watched, "total": len(merged)},
		"items":   merged,
		"updated": isoUTC(time.Now()),
	}
}

var (
	wallMu         sync.Mutex
	wallAt         time.Time
	wallData       M
	wallRefreshing bool
	wallBuildMu    sync.Mutex
)

const (
	wallTTL   = 10 * time.Minute // 新鲜期
	wallStale = 2 * time.Hour    // 过期兜底
)

func buildLocked() M {
	wallBuildMu.Lock()
	defer wallBuildMu.Unlock()
	return buildWall()
}

func keepWall(d M) {
	if ok, _ := d["ok"].(bool); ok {
		wallMu.Lock()
		wallData, wallAt = d, time.Now()
		wallMu.Unlock()
	}
}

func getWall(force bool) M {
	wallMu.Lock()
	age, data := time.Since(wallAt), wallData
	if !force && data != nil && age < wallTTL {
		wallMu.Unlock()
		return data
	}
	if !force && data != nil && age < wallStale {
		if !wallRefreshing {
			wallRefreshing = true
			go func() {
				keepWall(buildLocked())
				wallMu.Lock()
				wallRefreshing = false
				wallMu.Unlock()
			}()
		}
		wallMu.Unlock()
		return data
	}
	wallMu.Unlock()
	d := buildLocked()
	keepWall(d)
	return d
}

var reJfID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

func proxyJfImage(w http.ResponseWriter, itemID string) {
	c := conf()
	if c.Jellyfin.URL == "" || c.Jellyfin.Key == "" || !reJfID.MatchString(itemID) {
		sendRaw(w, 404, "text/plain; charset=utf-8", []byte("not found"), "")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", c.Jellyfin.URL+"/Items/"+url.PathEscape(itemID)+"/Images/Primary?maxWidth=300&quality=82", nil)
	for k, v := range jfHdr() {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		sendRaw(w, 502, "text/plain; charset=utf-8", []byte("upstream error"), "")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		code := 502
		if resp.StatusCode == 404 {
			code = 404
		}
		sendRaw(w, code, "text/plain; charset=utf-8", []byte("image unavailable"), "")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		sendRaw(w, 502, "text/plain; charset=utf-8", []byte("upstream error"), "")
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	sendRaw(w, 200, ct, body, "public, max-age=604800, immutable")
}

// sendRaw：不加 no-store（图片要能缓存）
func sendRaw(w http.ResponseWriter, code int, ct string, body []byte, cache string) {
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	if cache != "" {
		h.Set("Cache-Control", cache)
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func videosStatus() M {
	c := conf()
	return M{"bgmReady": c.Bangumi.User != "", "jfReady": c.Jellyfin.URL != "" && c.Jellyfin.Key != ""}
}
