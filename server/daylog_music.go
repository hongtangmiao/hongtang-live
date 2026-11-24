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
			cover := httpsURL(toStr(m["cover"]))
			if cover == "" {
				var histCover sql.NullString
				if db.QueryRow("SELECT cover FROM music WHERE title=? AND artist=? AND cover != '' ORDER BY last_ts DESC LIMIT 1", title, artist).Scan(&histCover) == nil {
					cover = httpsURL(histCover.String)
				}
			}
			return M{"title": title, "sub": artist, "album": toStr(m["album"]), "cover": cover,
				"url": toStr(m["url"]), "song_id": sid, "last_seen": ref, "ago": ago, "playing": playing}
		}
	}
	rows, err := db.Query("SELECT title, artist, album, cover, url, song_id, last_ts FROM music ORDER BY last_ts DESC LIMIT 20")
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var title, artist, album, cover, u, last sql.NullString
		var sid sql.NullInt64
		if rows.Scan(&title, &artist, &album, &cover, &u, &sid, &last) != nil || title.String == "" || isNoiseMusic(title.String, artist.String) {
			continue
		}
		var ago any
		if ms, ok := msOf(last.String); ok {
			ago = maxF(0, jsRound((nowMs()-ms)/60000))
		}
		return M{"title": title.String, "sub": artist.String, "album": album.String, "cover": httpsURL(cover.String), "url": u.String,
			"song_id": sid.Int64, "last_seen": last.String, "ago": ago, "playing": false}
	}
	return nil
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
		r = musicRow{Day: day.String, Title: title.String, Artist: artist.String, Album: album.String, Cover: cover.String, URL: u.String,
			FirstTS: first.String, LastTS: last.String, Plays: plays.Float64, Minutes: spanMin(first.String, last.String)}
		if r.Plays == 0 {
			r.Plays = 1
		}
		list = append(list, r)
	}
	rows.Close()

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
