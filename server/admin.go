package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* ---------------- 管理员：密码 / 登录 cookie / 首次设置码 ---------------- */

const (
	adminCookie   = "hl_admin"
	adminTTL      = 30 * 24 * time.Hour
	pbkdf2Rounds  = 200_000
	minPassword   = 8
	loginWindow   = 10 * time.Minute
	loginMaxFails = 10
)

// hashPassword 与 daylog 的格式一致（pbkdf2_sha256$轮数$盐$十六进制哈希），以后合并时可以直接沿用 daylog 的密码
func hashPassword(pw string) (string, error) {
	salt := randHex(16)
	k, err := pbkdf2.Key(sha256.New, pw, []byte(salt), pbkdf2Rounds, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2_sha256$" + strconv.Itoa(pbkdf2Rounds) + "$" + salt + "$" + hex.EncodeToString(k), nil
}

func checkPassword(stored, pw string) bool {
	p := strings.Split(stored, "$")
	if len(p) != 4 || p[0] != "pbkdf2_sha256" {
		return false
	}
	rounds, err := strconv.Atoi(p[1])
	if err != nil || rounds < 1000 || rounds > 5_000_000 {
		return false
	}
	want, err := hex.DecodeString(p[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, []byte(p[2]), rounds, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

/* 首次设置码：还没有管理员密码时生成一次，只打印到日志（docker logs 能看到） */

var (
	setupMu   sync.Mutex
	setupCode string
)

const setupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉了 0/O/1/I 这种容易看错的

func newSetupCode() string {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == 4 {
			b.WriteByte('-')
		}
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(setupAlphabet))))
		b.WriteByte(setupAlphabet[n.Int64()])
	}
	return b.String()
}

func needsSetup() bool { return conf().Admin.PasswordHash == "" }

// initAdmin：环境变量 HL_ADMIN_PASSWORD 可以直接设好密码（无人值守部署）；否则打印设置码
func initAdmin() {
	if !needsSetup() {
		return
	}
	if pw := os.Getenv("HL_ADMIN_PASSWORD"); pw != "" {
		if len(pw) < minPassword {
			log.Printf("[admin] HL_ADMIN_PASSWORD 少于 %d 位，忽略", minPassword)
		} else if h, err := hashPassword(pw); err == nil {
			c := cloneConfig(conf())
			c.Admin.PasswordHash = h
			if err := commitConfig(c); err == nil {
				log.Printf("[admin] 已按 HL_ADMIN_PASSWORD 设置管理员密码")
				return
			}
		}
	}
	setupMu.Lock()
	setupCode = newSetupCode()
	code := setupCode
	setupMu.Unlock()
	log.Printf("[admin] ==============================================")
	log.Printf("[admin]  还没有管理员密码。打开 /admin，输入设置码：%s", code)
	log.Printf("[admin] ==============================================")
}

/* 登录 cookie：过期时间.epoch.签名（不用存服务端；改密码 epoch+1 就全部作废） */

func adminSig(exp int64, epoch int) string {
	m := hmac.New(sha256.New, []byte(conf().Admin.Secret))
	m.Write([]byte("hl-admin|" + strconv.FormatInt(exp, 10) + "|" + strconv.Itoa(epoch)))
	return hex.EncodeToString(m.Sum(nil))[:40]
}

func isAdmin(r *http.Request) bool {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return false
	}
	p := strings.Split(c.Value, ".")
	if len(p) != 3 {
		return false
	}
	exp, err1 := strconv.ParseInt(p[0], 10, 64)
	epoch, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil || time.Now().Unix() > exp || epoch != conf().Admin.Epoch || conf().Admin.PasswordHash == "" {
		return false
	}
	return hmac.Equal([]byte(p[2]), []byte(adminSig(exp, epoch)))
}

func secureReq(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setAdminCookie(w http.ResponseWriter, r *http.Request) {
	exp := time.Now().Add(adminTTL).Unix()
	epoch := conf().Admin.Epoch
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: strconv.FormatInt(exp, 10) + "." + strconv.Itoa(epoch) + "." + adminSig(exp, epoch),
		Path: "/", MaxAge: int(adminTTL / time.Second), HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureReq(r)})
}

func clearAdminCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureReq(r)})
}

/* 登录限流：同一 IP 10 分钟内失败 10 次就先停一停（只清自己 IP 的记录） */

var (
	failMu sync.Mutex
	fails  = map[string][]time.Time{}
)

func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

// clientIP：只有连接本身来自内网（反代 / cloudflared / docker 网关）才看转发头
func clientIP(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if isPrivateIP(peer) {
		for _, h := range []string{"CF-Connecting-IP", "X-Real-IP"} {
			if v := strings.TrimSpace(r.Header.Get(h)); v != "" && net.ParseIP(v) != nil {
				return v
			}
		}
	}
	return peer
}

func loginBlocked(ip string) int {
	failMu.Lock()
	defer failMu.Unlock()
	now := time.Now()
	keep := fails[ip][:0]
	for _, t := range fails[ip] {
		if now.Sub(t) < loginWindow {
			keep = append(keep, t)
		}
	}
	fails[ip] = keep
	if len(keep) >= loginMaxFails {
		return int(loginWindow.Seconds()-now.Sub(keep[0]).Seconds()) + 1
	}
	return 0
}

func loginFailed(ip string) {
	failMu.Lock()
	fails[ip] = append(fails[ip], time.Now())
	n := len(fails[ip])
	failMu.Unlock()
	time.Sleep(time.Duration(minInt(n, 8)) * 250 * time.Millisecond) // 越错越慢
}

func loginOK(ip string) {
	failMu.Lock()
	delete(fails, ip)
	failMu.Unlock()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

/* ---------------- /api/admin/* ---------------- */

// adminPost：只接受同源、带 X-HL-Admin 头的 JSON POST（跨站表单发不出自定义头，算 CSRF 防护）
func adminPost(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != "POST" {
		writeJSON(w, 405, M{"ok": false, "error": "method not allowed"})
		return false
	}
	if crossOrigin(r) || r.Header.Get("X-HL-Admin") != "1" {
		writeJSON(w, 403, M{"ok": false, "error": "forbidden"})
		return false
	}
	b, err := readBody(r, 256*1024)
	if err != nil {
		writeJSON(w, 413, M{"ok": false, "error": "body too large"})
		return false
	}
	if out != nil {
		if err := decodeJSON(b, out); err != nil {
			writeJSON(w, 400, M{"ok": false, "error": "bad json"})
			return false
		}
	}
	return true
}

func handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	if crossOrigin(r) {
		writeJSON(w, 403, M{"ok": false, "error": "origin not allowed"})
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/admin/")
	switch p {
	case "state":
		writeJSON(w, 200, M{"ok": true, "setup": needsSetup(), "logged": isAdmin(r)})
		return
	case "setup":
		adminSetup(w, r)
		return
	case "login":
		adminLogin(w, r)
		return
	case "logout":
		if adminPost(w, r, nil) {
			clearAdminCookie(w, r)
			writeJSON(w, 200, M{"ok": true})
		}
		return
	}
	if !isAdmin(r) {
		writeJSON(w, 401, M{"ok": false, "error": "login required"})
		return
	}
	switch p {
	case "config":
		if r.Method == "GET" {
			writeJSON(w, 200, M{"ok": true, "config": redactedConfig(), "status": adminStatus()})
			return
		}
		adminSaveConfig(w, r)
	case "password":
		adminPassword(w, r)
	case "ne/unbind":
		if adminPost(w, r, nil) {
			clearOwner()
			log.Printf("[admin] 网易云账号已解绑")
			writeJSON(w, 200, M{"ok": true})
		}
	case "avatar":
		adminAvatar(w, r)
	default:
		if p == "devices" || strings.HasPrefix(p, "devices/") {
			handleAdminDevices(w, r, strings.TrimPrefix(strings.TrimPrefix(p, "devices"), "/"))
			return
		}
		if strings.HasPrefix(p, "daylog/") {
			handleAdminDaylog(w, r, strings.TrimPrefix(p, "daylog/"))
			return
		}
		writeJSON(w, 404, M{"ok": false, "error": "not found"})
	}
}

func adminSetup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code, Password, Title, Name string }
	if !adminPost(w, r, &in) {
		return
	}
	if !needsSetup() {
		writeJSON(w, 409, M{"ok": false, "error": "已经设置过了"})
		return
	}
	ip := clientIP(r)
	if wait := loginBlocked(ip); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeJSON(w, 429, M{"ok": false, "error": "尝试太频繁，" + strconv.Itoa(wait) + " 秒后再试"})
		return
	}
	setupMu.Lock()
	code := setupCode
	setupMu.Unlock()
	got := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Code), " ", ""))
	if code == "" || subtle.ConstantTimeCompare([]byte(got), []byte(code)) != 1 {
		loginFailed(ip)
		writeJSON(w, 403, M{"ok": false, "error": "设置码不对（看 docker logs 里的那一串）"})
		return
	}
	if len([]rune(in.Password)) < minPassword {
		writeJSON(w, 400, M{"ok": false, "error": "密码至少 " + strconv.Itoa(minPassword) + " 位"})
		return
	}
	h, err := hashPassword(in.Password)
	if err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	c := cloneConfig(conf())
	c.Admin.PasswordHash = h
	if t := strings.TrimSpace(in.Title); t != "" {
		c.Site.Title = t
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		c.Profile.Name = n
	}
	if err := commitConfig(c); err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	setupMu.Lock()
	setupCode = ""
	setupMu.Unlock()
	loginOK(ip)
	setAdminCookie(w, r)
	log.Printf("[admin] 管理员密码已设置（来自 %s）", ip)
	writeJSON(w, 200, M{"ok": true})
}

func adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if !adminPost(w, r, &in) {
		return
	}
	if needsSetup() {
		writeJSON(w, 409, M{"ok": false, "error": "还没有设置管理员密码", "setup": true})
		return
	}
	ip := clientIP(r)
	if wait := loginBlocked(ip); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeJSON(w, 429, M{"ok": false, "error": "尝试太频繁，" + strconv.Itoa(wait) + " 秒后再试"})
		return
	}
	if !checkPassword(conf().Admin.PasswordHash, in.Password) {
		loginFailed(ip)
		writeJSON(w, 401, M{"ok": false, "error": "密码不对"})
		return
	}
	loginOK(ip)
	setAdminCookie(w, r)
	writeJSON(w, 200, M{"ok": true})
}

func adminPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Old, New string }
	if !adminPost(w, r, &in) {
		return
	}
	if !checkPassword(conf().Admin.PasswordHash, in.Old) {
		loginFailed(clientIP(r))
		writeJSON(w, 401, M{"ok": false, "error": "原密码不对"})
		return
	}
	if len([]rune(in.New)) < minPassword {
		writeJSON(w, 400, M{"ok": false, "error": "新密码至少 " + strconv.Itoa(minPassword) + " 位"})
		return
	}
	h, err := hashPassword(in.New)
	if err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	c := cloneConfig(conf())
	c.Admin.PasswordHash = h
	c.Admin.Epoch++ // 别的地方登录的全部下线
	if err := commitConfig(c); err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	setAdminCookie(w, r)
	writeJSON(w, 200, M{"ok": true})
}

/* ---------------- 头像上传 ---------------- */

var errNotImage = errors.New("只支持 PNG / JPEG / WebP，最大 2MB")

func imageExt(b []byte) string {
	switch {
	case len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return ".png"
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return ".jpg"
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return ".webp"
	}
	return ""
}

func uploadsDir() string { return filepath.Join(dataDir, "uploads") }

// customAvatar：设置页上传过头像就用它，否则用 web/img/avatar.png
func customAvatar() (string, string) {
	for _, ext := range []string{".png", ".jpg", ".webp"} {
		p := filepath.Join(uploadsDir(), "avatar"+ext)
		if _, err := os.Stat(p); err == nil {
			return p, ext
		}
	}
	return "", ""
}

func adminAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		if crossOrigin(r) || r.Header.Get("X-HL-Admin") != "1" {
			writeJSON(w, 403, M{"ok": false, "error": "forbidden"})
			return
		}
		for _, ext := range []string{".png", ".jpg", ".webp"} {
			_ = os.Remove(filepath.Join(uploadsDir(), "avatar"+ext))
		}
		writeJSON(w, 200, M{"ok": true})
		return
	}
	if r.Method != "POST" || crossOrigin(r) || r.Header.Get("X-HL-Admin") != "1" {
		writeJSON(w, 403, M{"ok": false, "error": "forbidden"})
		return
	}
	b, err := readBody(r, 2<<20)
	ext := imageExt(b)
	if err != nil || ext == "" {
		writeJSON(w, 400, M{"ok": false, "error": errNotImage.Error()})
		return
	}
	if err := writeFileAtomic(filepath.Join(uploadsDir(), "avatar"+ext), b, 0o644); err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	for _, other := range []string{".png", ".jpg", ".webp"} {
		if other != ext {
			_ = os.Remove(filepath.Join(uploadsDir(), "avatar"+other))
		}
	}
	writeJSON(w, 200, M{"ok": true})
}
