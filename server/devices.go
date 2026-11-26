package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"
)

/*
 * 第 3 阶段：设备配对 + 统一上报接口。
 *
 *   设置页「添加设备」→ 生成 10 分钟有效、只能用一次的配对码（同时给二维码 / 配对链接）
 *   设备  POST /api/v1/pair   {code, name, kind, host, app_version} → 设备口令（只返回这一次）
 *   设备  GET  /api/v1/device                  我是谁、服务器希望多久报一次
 *   设备  POST /api/v1/report {activity, music, health, battery, stats}   一次报多样，各自独立成功/失败
 *   设备  POST /api/v1/aw     ActivityWatch 一天的导出（可 gzip）
 *   设备  POST /api/v1/unpair                  自己解除配对
 *   请求头 Authorization: Bearer hld_…
 *
 * 口令只存 SHA-256（口令本身是 32 字节随机数，不需要慢哈希）。设备清单在 data/devices.json（600）。
 */

type Battery struct {
	Level    int    `json:"level"`
	Charging bool   `json:"charging"`
	At       string `json:"at"`
}

type Device struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Host       string   `json:"host"`
	Scopes     []string `json:"scopes"`
	TokenHash  string   `json:"token_hash"`
	Created    string   `json:"created"`
	LastSeen   string   `json:"last_seen,omitempty"`
	LastIP     string   `json:"last_ip,omitempty"`
	AppVersion string   `json:"app_version,omitempty"`
	Battery    *Battery `json:"battery,omitempty"`
	StatsID    string   `json:"stats_id,omitempty"` // 统计页上的槽位（pc / pc-<id>），电脑才有
	Revoked    bool     `json:"revoked,omitempty"`
}

var deviceKinds = map[string]string{"windows": "Windows 电脑", "linux": "Linux 电脑", "android": "安卓手机", "other": "其他设备"}

var allScopes = []string{"activity", "music", "health", "battery", "stats", "aw"}

func defaultScopes(kind string) []string {
	switch kind {
	case "windows", "linux":
		return []string{"activity", "music", "stats", "aw"}
	case "android":
		return []string{"activity", "music", "health", "battery"}
	}
	return []string{"activity"}
}

type pairCode struct {
	Code    string    `json:"code"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Expires time.Time `json:"expires"`
}

var (
	devMu      sync.Mutex
	devList    []*Device
	devLoaded  bool
	devDirty   bool
	pairCodes  = map[string]*pairCode{}
	pairFailMu sync.Mutex
	pairFails  int // 全局失败计数：太多就作废所有配对码
)

const pairTTL = 10 * time.Minute

func devicesFile() string { return filepath.Join(dataDir, "devices.json") }

// devLoadLocked：第一次用时从磁盘读（测试里 dataDir 会变，所以不在 init 里读）
func devLoadLocked() {
	if devLoaded {
		return
	}
	devLoaded = true
	devList = nil
	b, err := os.ReadFile(devicesFile())
	if err != nil {
		return
	}
	var raw struct {
		Devices []*Device `json:"devices"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		log.Printf("[devices] %s 读不了：%v（当作没有设备）", devicesFile(), err)
		return
	}
	devList = raw.Devices
	// 第 4 阶段之前配对的电脑没有统计槽位：补上（第一台用 pc）
	taken := false
	for _, x := range devList {
		if x.StatsID == "pc" {
			taken = true
		}
	}
	for _, x := range devList {
		if x.StatsID == "" && (x.Kind == "windows" || x.Kind == "linux") {
			if !taken {
				x.StatsID, taken = "pc", true
			} else {
				x.StatsID = "pc-" + x.ID
			}
			devDirty = true
		}
	}
}

func devSaveLocked() error {
	b, _ := json.MarshalIndent(M{"devices": devList}, "", "  ")
	if err := writeFileAtomic(devicesFile(), b, 0o600); err != nil {
		return err
	}
	devDirty = false
	return nil
}

func devicesPersist() {
	devMu.Lock()
	defer devMu.Unlock()
	if devDirty {
		if err := devSaveLocked(); err != nil {
			log.Printf("[devices] 保存失败：%v", err)
		}
	}
}

// startDevicesSaver：最后在线时间 / 电量只在内存里改，每分钟落一次盘
func startDevicesSaver() {
	go func() {
		for range time.Tick(time.Minute) {
			devicesPersist()
		}
	}()
}

/* ---------------- 小工具 ---------------- */

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉 I O 0 1

func randCode() string {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == 4 {
			b.WriteByte('-')
		}
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b.WriteByte(codeAlphabet[n.Int64()])
	}
	return b.String()
}

// normCode：大小写、空格、横杠都不讲究（字母表里没有 I O 0 1，打错了就是对不上）
func normCode(s string) string {
	s = strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\u3000' {
			return -1
		}
		return r
	}, s))
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "hld_" + base64.RawURLEncoding.EncodeToString(b)
}

func tokenHash(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// host 可以有中文，只去掉空白、符号这些
var reHostName = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

func cleanHost(s string) string {
	s = reHostName.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-.")
	return runeCut(s, 40)
}

func cleanName(s string, def string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if s == "" {
		return def
	}
	return runeCut(s, 40)
}

func hasScope(d *Device, s string) bool {
	for _, x := range d.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

/* 简单的滑动窗口限流 */

type limiter struct {
	mu sync.Mutex
	m  map[string][]time.Time
}

func (l *limiter) hit(key string, n int, win time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string][]time.Time{}
	}
	now := time.Now()
	keep := l.m[key][:0]
	for _, t := range l.m[key] {
		if now.Sub(t) < win {
			keep = append(keep, t)
		}
	}
	if len(keep) >= n {
		l.m[key] = keep
		return false
	}
	l.m[key] = append(keep, now)
	if len(l.m) > 5000 {
		l.m = map[string][]time.Time{}
	}
	return true
}

func (l *limiter) blocked(key string, n int, win time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cnt := 0
	for _, t := range l.m[key] {
		if time.Since(t) < win {
			cnt++
		}
	}
	return cnt >= n
}

var (
	pairLimiter  limiter // 配对失败：每个 IP 10 分钟 10 次
	devAuthFails limiter // 口令错误：每个 IP 10 分钟 30 次
)

/* ---------------- 配对码（管理员生成） ---------------- */

func prunePairLocked() {
	now := time.Now()
	for k, p := range pairCodes {
		if now.After(p.Expires) {
			delete(pairCodes, k)
		}
	}
}

func createPairCode(name, kind string) (*pairCode, error) {
	if _, ok := deviceKinds[kind]; !ok && kind != "" {
		return nil, errors.New("设备类型只能是 windows / linux / android / other")
	}
	devMu.Lock()
	defer devMu.Unlock()
	prunePairLocked()
	if len(pairCodes) >= 5 {
		return nil, errors.New("还有 5 个没用掉的配对码，先用掉或者取消几个")
	}
	p := &pairCode{Code: randCode(), Name: cleanName(name, ""), Kind: kind, Expires: time.Now().Add(pairTTL)}
	pairCodes[p.Code] = p
	return p, nil
}

func pendingCodes() []*pairCode {
	devMu.Lock()
	defer devMu.Unlock()
	prunePairLocked()
	out := make([]*pairCode, 0, len(pairCodes))
	for _, p := range pairCodes {
		c := *p
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Expires.Before(out[j].Expires) })
	return out
}

func cancelPairCode(code string) bool {
	devMu.Lock()
	defer devMu.Unlock()
	code = normCode(code)
	if _, ok := pairCodes[code]; !ok {
		return false
	}
	delete(pairCodes, code)
	return true
}

// pairLink：配对链接 / 二维码内容。安卓 App 扫码、托盘程序粘贴都认这个
func pairLink(base, code string) string {
	return "hlpair://pair?s=" + url.QueryEscape(base) + "&c=" + url.QueryEscape(code)
}

func baseURLOf(r *http.Request) string {
	scheme := "http"
	if secureReq(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// qrRows：二维码矩阵，每行一个 "0101…" 字符串（前端自己画 SVG，不用 innerHTML / 外部脚本）
func qrRows(text string) ([]string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	rows := make([]string, c.Size)
	for y := 0; y < c.Size; y++ {
		b := make([]byte, c.Size)
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				b[x] = '1'
			} else {
				b[x] = '0'
			}
		}
		rows[y] = string(b)
	}
	return rows, nil
}

/* ---------------- 设备端：配对 ---------------- */

type pairReq struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Host       string `json:"host"`
	AppVersion string `json:"app_version"`
}

func pairDevice(req pairReq, ip string) (*Device, string, *ingestErr) {
	if pairLimiter.blocked(ip, 10, 10*time.Minute) {
		return nil, "", &ingestErr{429, "试错太多次了，10 分钟后再来"}
	}
	code := normCode(req.Code)
	devMu.Lock()
	prunePairLocked()
	var pc *pairCode
	for k, p := range pairCodes { // 逐个常量时间比较，不用 map 直接查
		if code != "" && subtle.ConstantTimeCompare([]byte(k), []byte(code)) == 1 {
			pc = p
		}
	}
	if pc == nil {
		devMu.Unlock()
		pairLimiter.hit(ip, 1000, 10*time.Minute)
		pairFailMu.Lock()
		pairFails++
		tooMany := pairFails >= 50
		if tooMany {
			pairFails = 0
		}
		pairFailMu.Unlock()
		if tooMany { // 有人在全网段撞码：把所有配对码作废
			devMu.Lock()
			pairCodes = map[string]*pairCode{}
			devMu.Unlock()
			log.Printf("[devices] 配对失败太多，已作废所有配对码")
		}
		return nil, "", &ingestErr{403, "配对码不对或已过期"}
	}
	delete(pairCodes, pc.Code) // 只能用一次
	devLoadLocked()
	kind := req.Kind
	if pc.Kind != "" {
		kind = pc.Kind
	}
	if _, ok := deviceKinds[kind]; !ok {
		kind = "other"
	}
	if len(devList) >= 50 {
		devMu.Unlock()
		return nil, "", &ingestErr{409, "设备太多了（最多 50 台），先在设置页删掉几台"}
	}
	id := randHex(4)
	host := cleanHost(req.Host)
	if host == "" {
		host = kind + "-" + id
	}
	for _, d := range devList { // daylog 里的 host 要唯一，撞了就加后缀
		if !d.Revoked && strings.EqualFold(d.Host, host) {
			host = host + "-" + id
			break
		}
	}
	tok := newToken()
	d := &Device{ID: id, Name: cleanName(or(pc.Name, req.Name), deviceKinds[kind]), Kind: kind, Host: host,
		Scopes: defaultScopes(kind), TokenHash: tokenHash(tok), Created: isoSec(time.Now().In(loc)),
		LastSeen: isoSec(time.Now().In(loc)), LastIP: ip, AppVersion: runeCut(strings.TrimSpace(req.AppVersion), 40)}
	if kind == "windows" || kind == "linux" {
		d.StatsID = "pc-" + id
		taken := false
		for _, x := range devList {
			if x.StatsID == "pc" {
				taken = true
			}
		}
		if !taken {
			d.StatsID = "pc" // 第一台电脑沿用旧的「pc」槽位：统计页的旧链接、旧数据都接得上
		}
	}
	devList = append(devList, d)
	err := devSaveLocked()
	devMu.Unlock()
	if err != nil {
		return nil, "", &ingestErr{500, "保存设备失败：" + err.Error()}
	}
	log.Printf("[devices] 已配对：%s（%s，host=%s，来自 %s）", d.Name, d.Kind, d.Host, ip)
	return d, tok, nil
}
