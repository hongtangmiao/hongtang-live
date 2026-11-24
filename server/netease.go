package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* ---------------- 网易云：会话 / 站点账号 / 上游 ---------------- */

const (
	sessionTTL       = 30 * 24 * time.Hour
	qrTTL            = 100 * time.Second
	profileVerifyTTL = 6 * time.Hour
)

type neSession struct {
	mu       sync.Mutex
	cookie   string
	profile  any
	ts       time.Time
	verifyAt time.Time
	owner    bool // 访客借用站点账号的只读视角
}

func (s *neSession) get() (string, any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cookie, s.profile
}

func (s *neSession) set(cookie string, profile any) {
	s.mu.Lock()
	s.cookie, s.profile = cookie, profile
	s.mu.Unlock()
}

type qrEntry struct {
	key, image, url string
	ts              time.Time
}

var (
	sessMu   sync.Mutex
	sessions = map[string]*neSession{}
	qrMu     sync.Mutex
	qrCache  = map[string]qrEntry{}
)

/* 站点账号（落盘，重启 / 换浏览器都不用再扫码） */

type ownerT struct {
	Cookie  string `json:"cookie"`
	Profile any    `json:"profile"`
	Ts      string `json:"ts"`
}

var (
	ownerMu sync.Mutex
	owner   *ownerT
)

func ownerFile() string { return filepath.Join(dataDir, "owner.json") }

func initOwner() {
	b, err := os.ReadFile(ownerFile())
	if err != nil {
		return
	}
	var o ownerT
	if decodeJSON(b, &o) == nil && o.Cookie != "" {
		owner = &o
	}
}

func persistOwnerLocked() {
	var v any = M{}
	if owner != nil {
		v = owner
	}
	if err := writeFileAtomic(ownerFile(), jsonBytes(v), 0o600); err != nil {
		log.Printf("[owner] save failed: %v", err)
	}
}

func getOwner() (string, any) {
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if owner == nil {
		return "", nil
	}
	return owner.Cookie, owner.Profile
}

func ownerExists() bool {
	ownerMu.Lock()
	defer ownerMu.Unlock()
	return owner != nil
}

func ownerBound() bool { c, _ := getOwner(); return c != "" }

func ownerUID() string {
	_, p := getOwner()
	if m := asM(p); m != nil {
		return toStr(m["userId"])
	}
	return ""
}

func setOwner(cookie string, profile any) {
	ownerMu.Lock()
	owner = &ownerT{Cookie: cookie, Profile: profile, Ts: isoUTC(time.Now())}
	persistOwnerLocked()
	ownerMu.Unlock()
}

func setOwnerProfile(p any) {
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if owner == nil || string(jsonBytes(owner.Profile)) == string(jsonBytes(p)) {
		return
	}
	owner.Profile = p
	persistOwnerLocked()
}

func clearOwner() {
	ownerMu.Lock()
	owner = nil
	_ = os.Remove(ownerFile())
	ownerMu.Unlock()
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sidOf(r *http.Request) string {
	if c, err := r.Cookie("ne_sid"); err == nil && c.Value != "" {
		return c.Value
	}
	return "anon"
}

// getSession：只读访问不建会话（create=false），免得每个爬虫请求都在内存里留 30 天
func getSession(w http.ResponseWriter, r *http.Request, create bool) *neSession {
	sid := ""
	if c, err := r.Cookie("ne_sid"); err == nil {
		sid = c.Value
	}
	sessMu.Lock()
	defer sessMu.Unlock()
	s := sessions[sid]
	if sid == "" || s == nil {
		if !create {
			return &neSession{ts: time.Now()}
		}
		sid = randHex(16)
		s = &neSession{ts: time.Now()}
		sessions[sid] = s
		w.Header().Add("Set-Cookie", "ne_sid="+sid+"; Path=/; HttpOnly; SameSite=Lax; Max-Age="+strconv.Itoa(int(sessionTTL/time.Second)))
	}
	s.ts = time.Now()
	return s
}

func sessionFor(w http.ResponseWriter, r *http.Request) *neSession {
	s := getSession(w, r, false)
	if c, _ := s.get(); c == "" {
		if oc, op := getOwner(); oc != "" {
			return &neSession{cookie: oc, profile: op, owner: true, ts: time.Now()}
		}
	}
	return s
}

func sessionJanitor() {
	for range time.Tick(time.Hour) {
		now := time.Now()
		sessMu.Lock()
		for k, s := range sessions {
			c, _ := s.get()
			if now.Sub(s.ts) > sessionTTL || (c == "" && now.Sub(s.ts) > time.Hour) {
				delete(sessions, k)
			}
		}
		sessMu.Unlock()
		qrMu.Lock()
		for k, q := range qrCache {
			if now.Sub(q.ts) > 3*qrTTL {
				delete(qrCache, k)
			}
		}
		qrMu.Unlock()
	}
}

/* 表单编码（与浏览器 URLSearchParams 一致） */
func formEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '*', c == '-', c == '.', c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

type kv struct{ k, v string }

func qs(pairs ...kv) string {
	var b strings.Builder
	for _, p := range pairs {
		if p.v == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(formEscape(p.k) + "=" + formEscape(p.v))
	}
	return b.String()
}

func qsMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]kv, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, kv{k, m[k]})
	}
	return qs(pairs...)
}

func nowStr() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }

/* 上游请求：8 秒超时 + 3 次尝试 */
type upResp struct {
	status int
	text   string
}

func upstream(p, search, method, body string) (upResp, error) {
	base := conf().NetEase.API
	if base == "" {
		return upResp{}, errors.New("netease api not configured")
	}
	u := base + "/" + p
	if search != "" {
		u += "?" + search
	}
	if method == "" {
		method = "GET"
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			cancel()
			return upResp{}, err
		}
		req.Header.Set("User-Agent", "hongtang-live/1.0")
		req.Header.Set("Accept", "application/json")
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := httpClient.Do(req)
		if err == nil {
			b, rerr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			resp.Body.Close()
			cancel()
			if rerr == nil && strings.TrimSpace(string(b)) != "" {
				return upResp{resp.StatusCode, string(b)}, nil
			}
			if rerr != nil {
				err = rerr
			} else {
				err = errors.New("empty response")
			}
		} else {
			cancel()
		}
		lastErr = err
		if attempt < 2 {
			time.Sleep(time.Duration(350*(attempt+1)) * time.Millisecond)
		}
	}
	return upResp{}, lastErr
}

func fetchAccount(cookie string) any {
	r, err := upstream("user/account", qs(kv{"cookie", cookie}, kv{"timestamp", nowStr()}), "", "")
	if err != nil {
		return nil
	}
	var d M
	if decodeJSON([]byte(r.text), &d) != nil {
		return nil
	}
	return d["profile"]
}

/* 白名单（只读 + 登录） */
var neAllow = []*regexp.Regexp{
	regexp.MustCompile(`^search$`), regexp.MustCompile(`^song/detail$`), regexp.MustCompile(`^personalized$`),
	regexp.MustCompile(`^toplist$`), regexp.MustCompile(`^playlist/detail$`), regexp.MustCompile(`^user/playlist$`),
	regexp.MustCompile(`^user/record$`), regexp.MustCompile(`^record/recent/song$`), regexp.MustCompile(`^likelist$`),
	regexp.MustCompile(`^user/account$`), regexp.MustCompile(`^login/qr/(key|create|check)$`),
	regexp.MustCompile(`^login/cellphone$`), regexp.MustCompile(`^logout$`),
}

// 只读接口只读服务器快照（nesync），访客打开页面不会打到网易云
var neCacheable = map[string]bool{
	"user/account": true, "record/recent/song": true, "user/record": true, "user/playlist": true,
	"likelist": true, "song/detail": true, "playlist/detail": true, "personalized": true,
	"toplist": true, "search": true, "top/song": true,
}

// 访客（借用站点账号视角）只能在这些展示类接口上带站点 cookie
var ownerShared = map[string]bool{
	"record/recent/song": true, "user/record": true, "user/playlist": true,
	"likelist": true, "song/detail": true, "playlist/detail": true,
}

var reNEPrefix = regexp.MustCompile(`^/api/ne/?`)

func neAllowed(p string) bool {
	for _, re := range neAllow {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

func scheduleProfileVerify(s *neSession) {
	if s.owner {
		return // 站点账号由 nesync 的后台刷新顺带校验
	}
	s.mu.Lock()
	if !s.verifyAt.IsZero() && time.Since(s.verifyAt) < profileVerifyTTL {
		s.mu.Unlock()
		return
	}
	s.verifyAt = time.Now()
	cookie := s.cookie
	s.mu.Unlock()
	go func() {
		r, err := upstream("user/account", qs(kv{"cookie", cookie}, kv{"timestamp", nowStr()}), "", "")
		if err != nil {
			return // 上游抖动，保持现状
		}
		var d M
		if decodeJSON([]byte(r.text), &d) != nil {
			return
		}
		if p := d["profile"]; p == nil {
			s.set("", nil)
		} else {
			c, _ := s.get()
			s.set(c, p)
		}
	}()
}

func handleNE(w http.ResponseWriter, r *http.Request) {
	p := reNEPrefix.ReplaceAllString(r.URL.Path, "")
	isLogin := strings.HasPrefix(p, "login/")
	// 登录 / 二维码 / 登出只认这台浏览器自己的会话，绝不借用站点账号
	var s *neSession
	if p == "qr" || p == "logout" || isLogin {
		s = getSession(w, r, p != "logout")
	} else {
		s = sessionFor(w, r)
	}

	// 已经绑过账号时，只有管理员或带对 bind_key 才能重新绑定，避免被访客顶掉
	if isLogin && ownerExists() && !isAdmin(r) {
		bk := conf().NetEase.BindKey
		key := r.URL.Query().Get("bind_key")
		if bk == "" || subtle.ConstantTimeCompare([]byte(key), []byte(bk)) != 1 {
			writeJSON(w, 403, M{"ok": false, "error": "already bound", "bound": true})
			return
		}
	}
	switch p {
	case "session":
		handleNESession(w, s)
		return
	case "qr":
		handleNEQR(w, r)
		return
	}
	if !neAllowed(p) {
		writeJSON(w, 403, M{"ok": false, "error": "endpoint not allowed"})
		return
	}
	cookie, _ := s.get()
	if p == "logout" && cookie == "" {
		writeJSON(w, 403, M{"ok": false, "error": "not logged in"})
		return
	}
	if s.owner && p == "user/account" {
		writeJSON(w, 403, M{"ok": false, "error": "login required"})
		return
	}
	useCookie := cookie
	if s.owner && !ownerShared[p] {
		useCookie = "" // search 等公共接口匿名调用
	}

	params := map[string]string{}
	if vals, err := url.ParseQuery(r.URL.RawQuery); err == nil {
		for k, v := range vals {
			if len(v) > 0 {
				params[k] = v[len(v)-1]
			}
		}
	}
	delete(params, "cookie")
	if useCookie != "" {
		params["cookie"] = useCookie
	}
	if params["timestamp"] == "" {
		params["timestamp"] = nowStr()
	}

	var resp upResp
	var err error
	switch {
	case r.Method == "POST":
		body, rerr := readBody(r, 64*1024)
		if rerr != nil {
			writeJSON(w, 504, M{"ok": false, "error": rerr.Error()})
			return
		}
		form, _ := url.ParseQuery(string(body))
		form.Del("cookie")
		if useCookie != "" {
			form.Set("cookie", useCookie)
		}
		resp, err = upstream(p, "", "POST", form.Encode())
	case neCacheable[p]:
		snap := nsGet(p, params, useCookie)
		if snap == nil && s.owner && !nsKnown(p, params) {
			writeJSON(w, 404, M{"ok": false, "error": "not available"})
			return
		}
		if snap == nil {
			var merr error
			snap, merr = nsMiss(p, params, useCookie)
			if merr != nil {
				writeJSON(w, 502, M{"ok": false, "error": "upstream: " + merr.Error()})
				return
			}
		}
		if snap == nil {
			writeJSON(w, 503, M{"ok": false, "error": "warming up, try later"})
			return
		}
		w.Header().Set("X-Snapshot-Age", strconv.FormatInt((time.Now().UnixMilli()-snap.Ts)/1000, 10))
		send(w, 200, []byte(snap.Text), map[string]string{"Content-Type": "application/json; charset=utf-8"})
		return
	default:
		resp, err = upstream(p, qsMap(params), "", "")
	}
	if err != nil {
		writeJSON(w, 504, M{"ok": false, "error": err.Error()})
		return
	}
	var data any
	if decodeJSON([]byte(resp.text), &data) != nil {
		writeJSON(w, 502, M{"ok": false, "error": "bad upstream json"})
		return
	}
	// 登录接口会回传 cookie：存在服务端，不发给浏览器
	if dm := asM(data); dm != nil && isLogin {
		if ck, ok := dm["cookie"].(string); ok && ck != "" {
			var prof any
			newCookie := ck
			if code := toStr(dm["code"]); code == "803" || code == "200" {
				prof = fetchAccount(newCookie)
				if prof == nil {
					newCookie = ""
				}
			}
			if prof == nil {
				prof = fetchAccount(ck) // 上游偶尔慢，再补一次
			}
			s.set(newCookie, prof)
			setOwner(newCookie, prof) // 登录一次 → 全站可见
			nsKick()
			dm["bound"] = true
			dm["profile"] = prof
			delete(dm, "cookie")
		}
	}
	if p == "logout" {
		if !s.owner {
			s.set("", nil)
			clearOwner()
		}
		qrMu.Lock()
		delete(qrCache, sidOf(r))
		qrMu.Unlock()
	}
	writeJSON(w, 200, data)
}

func handleNESession(w http.ResponseWriter, s *neSession) {
	cookie, profile := s.get()
	// profile 已在本地 → 直接返回；只有确实没有时才同步拉一次
	if cookie != "" && profile == nil {
		if r, err := upstream("user/account", qs(kv{"cookie", cookie}, kv{"timestamp", nowStr()}), "", ""); err == nil {
			var d M
			if decodeJSON([]byte(r.text), &d) == nil {
				profile = d["profile"]
				if s.owner {
					if profile != nil {
						setOwnerProfile(profile)
					} else {
						clearOwner()
						cookie = ""
					}
				} else {
					if profile == nil {
						cookie = ""
					}
					s.set(cookie, profile)
				}
			}
		}
	} else if cookie != "" && profile != nil {
		scheduleProfileVerify(s)
	}
	writeJSON(w, 200, M{
		"ok": true, "logged": cookie != "" && profile != nil, "profile": profile,
		"owner": s.owner && ownerBound(), "bound": ownerBound(),
	})
}

func handleNEQR(w http.ResponseWriter, r *http.Request) {
	sid := sidOf(r)
	qrMu.Lock()
	c, ok := qrCache[sid]
	qrMu.Unlock()
	if ok && time.Since(c.ts) < qrTTL {
		writeJSON(w, 200, M{"ok": true, "cached": true, "key": c.key, "image": c.image, "url": c.url})
		return
	}
	kr, err := upstream("login/qr/key", qs(kv{"timestamp", nowStr()}), "", "")
	if err != nil {
		writeJSON(w, 504, M{"ok": false, "error": "upstream timeout"})
		return
	}
	var k M
	_ = decodeJSON([]byte(kr.text), &k)
	key := toStr(asM(k["data"])["unikey"])
	if key == "" {
		writeJSON(w, 502, M{"ok": false, "error": "no key"})
		return
	}
	cr, err := upstream("login/qr/create", qs(kv{"key", key}, kv{"qrimg", "1"}, kv{"timestamp", nowStr()}), "", "")
	if err != nil {
		writeJSON(w, 504, M{"ok": false, "error": "upstream timeout"})
		return
	}
	var q M
	_ = decodeJSON([]byte(cr.text), &q)
	data := asM(q["data"])
	img := toStr(data["qrimg"])
	if img == "" {
		writeJSON(w, 502, M{"ok": false, "error": "no image"})
		return
	}
	qrMu.Lock()
	qrCache[sid] = qrEntry{key: key, image: img, url: toStr(data["qrurl"]), ts: time.Now()}
	qrMu.Unlock()
	writeJSON(w, 200, M{"ok": true, "key": key, "image": img, "url": toStr(data["qrurl"])})
}
