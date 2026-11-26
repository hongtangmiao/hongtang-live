package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

/* ---------------- 听歌历史 ---------------- */

var (
	noiseArtistRE = regexp.MustCompile(`(?i)(?:\bR18\b|🔞|\bNSFW\b|\bFANBOX\b|\bFANTIA\b|\bCI-EN\b|\bSKBMMDer\b)`)
	noiseTitleRE  = regexp.MustCompile(`(?i)(?:\bSD:[A-Za-z0-9_-]+|\bNo sound\b|\bNSFW\b|販売開始|フル動画)`)
)

func isNoiseMusic(title, artist string) bool {
	return noiseArtistRE.MatchString(artist) || noiseTitleRE.MatchString(title)
}

func spanMin(a, b string) float64 {
	am, ok1 := msOf(a)
	bm, ok2 := msOf(b)
	if !ok1 || !ok2 {
		return 1
	}
	t := (bm - am) / 60000
	if t > 0 {
		return maxF(1, jsRound(t))
	}
	return 1
}

func saveMusicCover(title, artist string, e M) {
	cover := httpsURL(toStr(e["cover"]))
	if cover == "" || title == "" {
		return
	}
	db, err := dlWriter()
	if err != nil {
		return
	}
	sid := int64(toNum(e["song_id"]))
	dlWriteMu.Lock()
	defer dlWriteMu.Unlock()
	_, _ = db.Exec("UPDATE music SET cover=?, album=COALESCE(NULLIF(?, ''), album), url=COALESCE(NULLIF(?, ''), url), song_id=CASE WHEN ? > 0 THEN ? ELSE song_id END WHERE title=? AND artist=? AND cover=''",
		cover, toStr(e["album"]), toStr(e["url"]), sid, sid, title, artist)
}

var (
	bfMu      sync.Mutex
	bfRunning bool
	bfLast    time.Time
)

func backfillMusicCovers(items []musicRow) {
	if len(items) == 0 {
		return
	}
	if strings.TrimRight(or(dStr(dlCfg(), "music", "api_base"), conf().NetEase.API), "/") == "" {
		return
	}
	bfMu.Lock()
	if bfRunning || time.Since(bfLast) < 2*time.Minute {
		bfMu.Unlock()
		return
	}
	bfRunning = true
	bfLast = time.Now()
	bfMu.Unlock()
	go func() {
		defer func() {
			bfMu.Lock()
			bfRunning = false
			bfMu.Unlock()
		}()
		updated := false
		for i, r := range items {
			if i >= 25 {
				break
			}
			e := enrichMusic(M{"title": r.Title, "artist": r.Artist, "album": r.Album})
			if truthy(e["cover"]) {
				saveMusicCover(r.Title, r.Artist, e)
				updated = true
			}
		}
		if updated {
			histMu.Lock()
			histData = nil
			histMu.Unlock()
		}
	}()
}

// fillMusicCover：优先用自带封面，缺了查历史 music 表，再走 enrichMusic 补拉
func fillMusicCover(db *sql.DB, title, artist string, m M) string {
	cover := httpsURL(toStr(m["cover"]))
	if cover == "" && title != "" {
		var histCover sql.NullString
		if db.QueryRow("SELECT cover FROM music WHERE title=? AND artist=? AND cover != '' ORDER BY last_ts DESC LIMIT 1", title, artist).Scan(&histCover) == nil {
			cover = httpsURL(histCover.String)
		}
	}
	if cover == "" && title != "" {
		if e := enrichMusic(m); truthy(e["cover"]) {
			cover = httpsURL(toStr(e["cover"]))
			if !truthy(m["url"]) && truthy(e["url"]) {
				m["url"] = e["url"]
			}
		}
	}
	return cover
}

// currentMusic：优先 devices.music；清空或过期就用最近一条有效播放记录（标成没在放）
func currentMusic() any {
	db := daylogDB()
	if db == nil {
		return nil
	}
	var music, mts, seen sql.NullString
	if db.QueryRow("SELECT music, music_ts, last_seen FROM devices WHERE music != '' ORDER BY music_ts DESC, last_seen DESC LIMIT 1").Scan(&music, &mts, &seen) == nil && music.String != "" {
		var m M
		if decodeJSON([]byte(music.String), &m) == nil && truthy(m["title"]) && !isNoiseMusic(toStr(m["title"]), toStr(m["artist"])) {
			ref := or(mts.String, seen.String)
			var ago any
			playing := false
			if ms, ok := msOf(ref); ok {
				a := maxF(0, jsRound((nowMs()-ms)/60000))
				ago, playing = a, a < 12
			}
			sid := any(0)
			if truthy(m["song_id"]) {
				sid = m["song_id"]
			}
			title, artist := toStr(m["title"]), toStr(m["artist"])
			cover := fillMusicCover(db, title, artist, m)
			return M{"title": title, "sub": artist, "album": toStr(m["album"]), "cover": cover,
				"url": toStr(m["url"]), "song_id": sid, "last_seen": ref, "ago": ago, "playing": playing}
		}
	}
	rows, err := db.Query("SELECT title, artist, album, cover, url, song_id, last_ts FROM music ORDER BY last_ts DESC LIMIT 20")
	if err != nil {
		return nil
	}
	var picked *struct {
		title, artist, album, cover, url, last string
		sid                                    int64
	}
	for rows.Next() {
		var title, artist, album, cover, u, last sql.NullString
		var sid sql.NullInt64
		if rows.Scan(&title, &artist, &album, &cover, &u, &sid, &last) != nil || title.String == "" || isNoiseMusic(title.String, artist.String) {
			continue
		}
		picked = &struct {
			title, artist, album, cover, url, last string
			sid                                    int64
		}{title.String, artist.String, album.String, cover.String, u.String, last.String, sid.Int64}
		break
	}
	rows.Close()
	if picked == nil {
		return nil
	}
	var ago any
	if ms, ok := msOf(picked.last); ok {
		ago = maxF(0, jsRound((nowMs()-ms)/60000))
	}
	cv := httpsURL(picked.cover)
	if cv == "" {
		if e := enrichMusic(M{"title": picked.title, "artist": picked.artist, "album": picked.album}); truthy(e["cover"]) {
			cv = httpsURL(toStr(e["cover"]))
			go saveMusicCover(picked.title, picked.artist, e)
		}
	}
	return M{"title": picked.title, "sub": picked.artist, "album": picked.album, "cover": cv, "url": picked.url,
		"song_id": picked.sid, "last_seen": picked.last, "ago": ago, "playing": false}
}

type musicRow struct {
	Day     string  `json:"day"`
	Title   string  `json:"title"`
	Artist  string  `json:"artist"`
	Album   string  `json:"album"`
	Cover   string  `json:"cover"`
	URL     string  `json:"url"`
	FirstTS string  `json:"first_ts"`
	LastTS  string  `json:"last_ts"`
	Plays   float64 `json:"plays"`
	Minutes float64 `json:"minutes"`
}

var (
	histMu   sync.Mutex
	histAt   time.Time
	histData M
)

func musicHistory() M {
	histMu.Lock()
	defer histMu.Unlock()
	if histData != nil && time.Since(histAt) < 60*time.Second {
		return withNow(histData)
	}
	db := daylogDB()
	if db == nil {
		return nil
	}
	now := time.Now()
	from := dayOf(now.Add(-92 * 24 * time.Hour))
	rows, err := db.Query("SELECT day, title, artist, album, cover, url, first_ts, last_ts, plays FROM music WHERE day >= ? ORDER BY last_ts DESC", from)
	if err != nil {
		log.Printf("[music] query failed: %v", err)
		return nil
	}
	var list []musicRow
	for rows.Next() {
		var r musicRow
		var day, title, artist, album, cover, u, first, last sql.NullString
		var plays sql.NullFloat64
		if rows.Scan(&day, &title, &artist, &album, &cover, &u, &first, &last, &plays) != nil {
			continue
		}
		if isNoiseMusic(title.String, artist.String) {
			continue
		}
		r = musicRow{Day: day.String, Title: title.String, Artist: artist.String, Album: album.String, Cover: httpsURL(cover.String), URL: u.String,
			FirstTS: first.String, LastTS: last.String, Plays: plays.Float64, Minutes: spanMin(first.String, last.String)}
		if r.Cover == "" {
			musicCacheMu.Lock()
			if hit, ok := musicCache[r.Title+"|"+r.Artist]; ok && truthy(hit.v["cover"]) {
				r.Cover = httpsURL(toStr(hit.v["cover"]))
			}
			musicCacheMu.Unlock()
		}
		if r.Plays == 0 {
			r.Plays = 1
		}
		list = append(list, r)
	}
	rows.Close()

	// 历史记录里缺封面的歌（最近 25 首）在后台补拉并写回 SQLite
	var missing []musicRow
	seenMiss := map[string]bool{}
	for _, r := range list {
		k := r.Title + "|" + r.Artist
		if r.Cover == "" && !seenMiss[k] {
			seenMiss[k] = true
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		backfillMusicCovers(missing)
	}

	sumPlays := func(a []musicRow) (p, m float64) {
		for _, x := range a {
			p += x.Plays
			m += x.Minutes
		}
		return
	}
	type topItem struct {
		Title   string  `json:"title"`
		Artist  string  `json:"artist"`
		Album   string  `json:"album"`
		Cover   string  `json:"cover"`
		URL     string  `json:"url"`
		Plays   float64 `json:"plays"`
		Minutes float64 `json:"minutes"`
		Last    string  `json:"last"`
	}
	type win struct {
		top            []*topItem
		songs          int
		plays, minutes float64
	}
	windowOf := func(days int) win {
		cutoff := dayOf(now.Add(-time.Duration(days) * 24 * time.Hour))
		var part []musicRow
		var order []*topItem
		idx := map[string]*topItem{}
		for _, r := range list {
			if r.Day < cutoff {
				continue
			}
			part = append(part, r)
			k := r.Title + "|" + r.Artist
			c := idx[k]
			if c == nil {
				c = &topItem{Title: r.Title, Artist: r.Artist, Album: r.Album, Cover: r.Cover, URL: r.URL, Last: r.LastTS}
				idx[k] = c
				order = append(order, c)
			}
			c.Plays += r.Plays
			c.Minutes += r.Minutes
			if r.LastTS > c.Last {
				c.Last = r.LastTS
			}
			if c.Cover == "" && r.Cover != "" {
				c.Cover = r.Cover
			}
		}
		sort.SliceStable(order, func(i, j int) bool {
			if order[i].Plays != order[j].Plays {
				return order[i].Plays > order[j].Plays
			}
			return order[i].Minutes > order[j].Minutes
		})
		p, m := sumPlays(part)
		return win{top: order, songs: len(idx), plays: p, minutes: m}
	}
	head := func(a []*topItem, n int) []*topItem {
		if len(a) > n {
			return a[:n]
		}
		if a == nil {
			return []*topItem{}
		}
		return a
	}
	w7, w30, w90 := windowOf(7), windowOf(30), windowOf(90)
	today := dayOf(now)
	todayRows := []musicRow{}
	for _, r := range list {
		if r.Day == today {
			todayRows = append(todayRows, r)
		}
	}
	sort.SliceStable(todayRows, func(i, j int) bool { return todayRows[i].LastTS > todayRows[j].LastTS })

	type artist struct {
		Name    string  `json:"name"`
		Plays   float64 `json:"plays"`
		Minutes float64 `json:"minutes"`
		Songs   int     `json:"songs"`
		Cover   string  `json:"cover"`
	}
	var arts []*artist
	aidx := map[string]*artist{}
	c30 := dayOf(now.Add(-30 * 24 * time.Hour))
	for _, r := range list {
		if r.Day < c30 {
			continue
		}
		name := strings.TrimSpace(strings.Split(or(r.Artist, "—"), ",")[0])
		a := aidx[name]
		if a == nil {
			a = &artist{Name: name, Cover: r.Cover}
			aidx[name] = a
			arts = append(arts, a)
		}
		a.Plays += r.Plays
		a.Minutes += r.Minutes
		a.Songs++
		if a.Cover == "" && r.Cover != "" {
			a.Cover = r.Cover
		}
	}
	sort.SliceStable(arts, func(i, j int) bool { return arts[i].Plays > arts[j].Plays })
	if len(arts) > 10 {
		arts = arts[:10]
	}
	if arts == nil {
		arts = []*artist{}
	}

	days := []M{}
	for i := 0; i < 14; i++ {
		d := dayOf(now.Add(-time.Duration(i) * 24 * time.Hour))
		var part []musicRow
		for _, r := range list {
			if r.Day == d {
				part = append(part, r)
			}
		}
		if len(part) == 0 {
			continue
		}
		_, mins := sumPlays(part)
		titles := []string{}
		for k := 0; k < len(part) && k < 3; k++ {
			titles = append(titles, part[k].Title)
		}
		days = append(days, M{"day": d, "songs": len(part), "minutes": mins, "titles": titles})
	}
	tp, tm := sumPlays(todayRows)
	data := M{
		"ok": true, "today": today,
		"stats": M{
			"todaySongs": len(todayRows), "todayPlays": tp, "todayMinutes": tm,
			"weekSongs": w7.songs, "weekPlays": w7.plays, "weekMinutes": w7.minutes,
			"monthSongs": w30.songs, "monthPlays": w30.plays, "monthMinutes": w30.minutes,
		},
		"today_rows": todayRows,
		"top":        M{"d7": head(w7.top, 12), "d30": head(w30.top, 12), "d90": head(w90.top, 12)},
		"artists":    arts,
		"days":       days,
		"updated":    isoUTC(now),
	}
	// 存一份纯 JSON 结构，之后每次只换 now
	var frozen M
	_ = decodeJSON(jsonBytes(data), &frozen)
	histData, histAt = frozen, now
	return withNow(frozen)
}

func withNow(d M) M {
	out := make(M, len(d)+1)
	for k, v := range d {
		out[k] = v
	}
	out["now"] = currentMusic()
	return out
}

func itoa(n int64) string { return jsonNumber(n) }

func pad2(n int64) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func jsonNumber(n int64) string { return json.Number(strconvI(n)).String() }
