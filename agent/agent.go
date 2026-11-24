package main

import (
	"strings"
	"sync"
	"time"
)

/* 平台接口：Windows / Linux 各自实现 */

type WinInfo struct {
	App, Title, Class string
	Unsupported       bool // 这个桌面拿不到前台窗口（Wayland）：改用 ActivityWatch 兜底
	None              bool // 拿不到前台窗口（锁屏、UAC、刚开机）
	Desktop           bool // 焦点在桌面 / 任务栏：不算在用文件管理器
	System            bool // 音量浮层、开始菜单这类系统窗口：跳过这一轮，保持上一个
}

type MediaInfo struct {
	Status       string // Playing / Paused / Stopped / none
	App          string
	Title        string
	Artist       string
	Album        string
	PlaybackType int // Windows SMTC：1 音乐 2 视频
	Err          bool
}

type Platform interface {
	Window() WinInfo
	IdleSeconds() float64 // 没碰键鼠多久；拿不到返回 -1
	Locked() bool
	Media() MediaInfo
	Stats(full bool) M // 系统资源；full=false 时可以省掉不常变的分区（服务器沿用上一次）
	Kind() string      // windows / linux
	Close()
}

/* ---------------- 音乐：和原来 live_agent.music_payload 一样的判断 ---------------- */

var musicApps = []string{"cloudmusic", "netease", "qqmusic", "spotify", "foobar", "music.ui", "aimp", "groove", "cider", "kugou", "kuwo",
	"rhythmbox", "elisa", "amberol", "lollypop", "clementine", "strawberry", "audacious", "deadbeef", "yesplaymusic", "feishin", "mpd"}

// 视频站 / 视频应用的痕迹。出现在标题、歌手、专辑或应用名里就不当音乐。
// 浏览器的媒体会话是网页自己塞进来的：X、微博这类站点会把账号名填进 artist，
// 光靠「artist 非空」判断不出来，必须再加一层。
var videoHints = []string{
	"x.com", "twitter", "pornhub", "xvideos", "xnxx", "missav", "hanime", "nhentai",
	"spankbang", "eporner", "javbus", "bilibili", "b23.tv", "douyin", "tiktok",
	"youtube", "youtu.be", "netflix", "iqiyi", "youku", "v.qq.com", "mgtv",
	"acfun", "nicovideo", "twitch", "dailymotion", "weibo",
}

func looksLikeVideo(s string) bool {
	s = strings.ToLower(s)
	for _, k := range videoHints {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func musicPayload(m MediaInfo) M {
	if m.Err {
		return M{"playing": false, "reason": "unknown"}
	}
	if m.Status == "none" || m.Status == "" {
		return M{"playing": false, "reason": "none"}
	}
	if m.Status != "Playing" {
		return M{"playing": false, "reason": "paused"}
	}
	title, artist, album := strings.TrimSpace(m.Title), strings.TrimSpace(m.Artist), strings.TrimSpace(m.Album)
	if title == "" {
		return M{"playing": false, "reason": "unknown"}
	}
	app := strings.ToLower(m.App)
	knownMusic := false
	for _, k := range musicApps {
		if strings.Contains(app, k) {
			knownMusic = true
		}
	}
	// 明确标成视频的会话（SMTC PlaybackType=2）：已知音乐软件除外，一律当视频
	if m.PlaybackType == 2 && !knownMusic {
		return M{"playing": false, "reason": "video"}
	}
	// 标题 / 歌手 / 专辑 / 应用名里带视频站痕迹的一律当视频
	if !knownMusic && looksLikeVideo(title+"\n"+artist+"\n"+album+"\n"+app) {
		return M{"playing": false, "reason": "video"}
	}
	var isMusic bool
	switch {
	case knownMusic:
		isMusic = true
	case isBrowser(m.App):
		// 浏览器里只有标题、没有专辑的会话基本是网页视频（X、B 站、视频号…）；
		// 网易云网页版这类音乐站点会同时给出歌手和专辑。
		isMusic = artist != "" && album != ""
	default:
		isMusic = artist != ""
	}
	if !isMusic {
		return M{"playing": false, "reason": "video"}
	}
	return M{"playing": true, "title": title, "artist": artist, "album": album, "source": app, "reason": "music"}
}

// mprisApp：org.mpris.MediaPlayer2.chromium.instance1234 → chromium
func mprisApp(n string) string {
	s := strings.TrimPrefix(n, "org.mpris.MediaPlayer2.")
	if i := strings.Index(s, ".instance"); i >= 0 {
		s = s[:i]
	}
	return s
}

// 和服务端 server/dlconfig.go 的 privacy.browser_apps 保持同一份名单，改一边要改另一边
var browserApps = []string{"chrome", "msedge", "edge", "firefox", "brave", "arc", "vivaldi", "opera", "chromium", "librewolf", "zen", "floorp"}

func isBrowser(app string) bool {
	a := strings.ToLower(strings.TrimSuffix(strings.ToLower(app), ".exe"))
	for _, b := range browserApps {
		if a == b || strings.HasPrefix(a, b+"-") || strings.HasPrefix(a, b+"_") {
			return true
		}
	}
	return false
}

/* ---------------- 核心：几条循环 ---------------- */

type Status struct {
	Paired      bool                  `json:"paired"`
	NeedPair    bool                  `json:"need_pair"`
	Server      string                `json:"server"`
	Device      DeviceInfo            `json:"device"`
	ServerName  string                `json:"server_name"`
	Writer      bool                  `json:"writer"`
	Online      bool                  `json:"online"`
	LastReport  time.Time             `json:"last_report"`
	LastErr     string                `json:"last_err"`
	Results     map[string]PartResult `json:"results"`
	Activity    string                `json:"activity"`
	Music       string                `json:"music"`
	StatsAt     time.Time             `json:"stats_at"`
	StatsErr    string                `json:"stats_err"`
	AWAt        time.Time             `json:"aw_at"`
	AWMsg       string                `json:"aw_msg"`
	PausedUntil time.Time             `json:"paused_until"`
	Version     string                `json:"version"`
	Kind        string                `json:"kind"`
}

type Agent struct {
	mu      sync.Mutex
	cfg     *Config
	st      *State
	plat    Platform
	hints   Hints
	status  Status
	changed chan struct{} // 状态变了，托盘刷新
	kick    chan struct{} // 配对 / 改设置后马上报一次
	lastAct M
	statsN  int
	statsT  time.Time // 上次发完整的系统资源
	now     func() time.Time
}

func newAgent(p Platform) *Agent {
	a := &Agent{cfg: loadConfig(), st: loadState(), plat: p, changed: make(chan struct{}, 1), kick: make(chan struct{}, 1), now: time.Now}
	a.status.Version, a.status.Kind = version, p.Kind()
	a.refreshStatusLocked()
	return a
}

func (a *Agent) notify() {
	select {
	case a.changed <- struct{}{}:
	default:
	}
}

func (a *Agent) kickNow() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func (a *Agent) refreshStatusLocked() {
	a.status.Paired = a.cfg.paired()
	a.status.Server = a.cfg.Server
	a.status.Device = a.cfg.Device
	a.status.PausedUntil = a.cfg.PausedUntil
}

func (a *Agent) snapshot() (*Config, Status) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := *a.cfg
	c.Device.Scopes = append([]string(nil), a.cfg.Device.Scopes...)
	s := a.status
	return &c, s
}

func (a *Agent) client() *Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.paired() {
		return nil
	}
	return newClient(a.cfg.Server, a.cfg.token())
}

func (a *Agent) paused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.now().Before(a.cfg.PausedUntil)
}

func (a *Agent) setPause(until time.Time) {
	a.mu.Lock()
	a.cfg.PausedUntil = until
	_ = saveConfig(a.cfg)
	a.refreshStatusLocked()
	a.mu.Unlock()
	if until.IsZero() {
		logf("继续上报")
		a.kickNow()
	} else {
		logf("暂停上报到 %s", until.Format("01-02 15:04"))
	}
	a.notify()
}

func (a *Agent) updateConfig(f func(c *Config)) error {
	a.mu.Lock()
	f(a.cfg)
	err := saveConfig(a.cfg)
	a.refreshStatusLocked()
	a.mu.Unlock()
	a.notify()
	a.kickNow()
	return err
}

// handleErr：401 = 口令失效（被停用 / 删除），停下来等重新配对
func (a *Agent) handleErr(where string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if isAuthErr(err) {
		if !a.status.NeedPair {
			logf("%s：服务器不认这台设备了（%v），需要重新配对", where, err)
		}
		a.status.NeedPair, a.status.Online = true, true
	} else {
		if a.status.Online || a.status.LastErr == "" {
			logf("%s 失败：%v", where, err)
		}
		a.status.Online = false
	}
	a.status.LastErr = err.Error()
	go a.notify()
}

func (a *Agent) Pair(server, code, name string) error {
	host := rawHostname() // 和原来 live_agent 报的一样（Windows 上就是电脑名），daylog 里接得上
	if name == "" {
		name = host
	}
	c := newClient(server, "")
	tok, dev, hints, err := c.Pair(code, name, a.plat.Kind(), host)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg.Server = strings.TrimRight(server, "/")
	if err := a.cfg.setToken(tok); err != nil {
		a.mu.Unlock()
		return err
	}
	a.cfg.Device, a.hints = dev, hints
	err = saveConfig(a.cfg)
	a.status = Status{Version: version, Kind: a.plat.Kind(), Online: true, ServerName: hints.Name, Writer: hints.Writer}
	a.st.AWDone = map[string]string{} // 换了服务器，之前几天的数据也传过去
	a.statsT = time.Time{}
	a.refreshStatusLocked()
	a.mu.Unlock()
	saveState(a.st)
	logf("已配对：%s（%s，host=%s）→ %s", dev.Name, dev.Kind, dev.Host, server)
	a.notify()
	a.kickNow()
	return err
}

func (a *Agent) Unpair(tellServer bool) {
	if c := a.client(); c != nil && tellServer {
		if err := c.Unpair(); err != nil && !isAuthErr(err) {
			logf("通知服务器解除配对失败：%v（本机照样清掉）", err)
		}
	}
	a.mu.Lock()
	a.cfg.Server, a.cfg.Device = "", DeviceInfo{}
	_ = a.cfg.setToken("")
	_ = saveConfig(a.cfg)
	a.status = Status{Version: version, Kind: a.plat.Kind()}
	a.refreshStatusLocked()
	a.mu.Unlock()
	logf("已解除配对")
	a.notify()
}

func (a *Agent) refreshDevice() {
	c := a.client()
	if c == nil {
		return
	}
	dev, hints, err := c.Device()
	if err != nil {
		a.handleErr("刷新设备信息", err)
		return
	}
	a.mu.Lock()
	a.cfg.Device, a.hints = dev, hints
	_ = saveConfig(a.cfg)
	a.status.NeedPair, a.status.Online, a.status.ServerName, a.status.Writer = false, true, hints.Name, hints.Writer
	a.refreshStatusLocked()
	a.mu.Unlock()
	a.notify()
}

func (a *Agent) interval() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.cfg.Report.IntervalSec
	if n <= 0 {
		n = a.hints.ReportSec
	}
	if n <= 0 {
		n = 30
	}
	if n < 10 {
		n = 10
	}
	return time.Duration(n) * time.Second
}

var systemHosts = map[string]bool{"shellexperiencehost": true, "startmenuexperiencehost": true, "searchapp": true, "searchhost": true,
	"textinputhost": true, "lockapp": true, "sihost": true}

// buildLive：这一轮要报的 activity / music（不发请求，方便测试）
func (a *Agent) buildLive() M {
	cfg, st := a.snapshot()
	body := M{"ts": a.now().Format(time.RFC3339)}
	wantAct := cfg.Report.Activity && cfg.hasScope("activity")
	wantMus := cfg.Report.Music && cfg.hasScope("music")
	var media MediaInfo
	if wantMus || (wantAct && cfg.Report.IdleMin > 0) {
		media = a.plat.Media()
	}
	if wantAct {
		w := a.plat.Window()
		if w.Unsupported {
			if app, title, ok := newAW(cfg.AWURL).LatestWindow(); ok {
				w = WinInfo{App: app, Title: title}
			}
		}
		idle := a.plat.IdleSeconds()
		switch {
		case w.Unsupported:
			// 拿不到也猜不到：这一轮不报「在用什么」
		case a.plat.Locked() || w.None:
			body["activity"] = M{"idle": true}
		case w.Desktop:
			body["activity"] = M{"idle": true}
		case w.System || systemHosts[strings.ToLower(strings.TrimSuffix(strings.ToLower(w.App), ".exe"))]:
			// 跳过：保持上一个真实应用
		case cfg.Report.IdleMin > 0 && idle >= float64(cfg.Report.IdleMin*60) && media.Status != "Playing":
			body["activity"] = M{"idle": true}
		default:
			act := M{"app": w.App, "title": w.Title}
			if isBrowser(w.App) && cfg.Report.AW {
				if u := newAW(cfg.AWURL).LatestURL(); u != "" {
					act["url"] = u
				}
			}
			body["activity"] = act
		}
	}
	if wantMus {
		body["music"] = musicPayload(media)
	}
	_ = st
	return body
}

func (a *Agent) reportOnce() {
	if a.paused() {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	body := a.buildLive()
	if len(body) <= 1 {
		return
	}
	res, hints, err := c.Report(body)
	if err != nil {
		a.handleErr("上报", err)
		return
	}
	a.mu.Lock()
	if !a.status.Online || a.status.NeedPair {
		logf("上报恢复正常")
	}
	a.hints = hints
	a.status.Online, a.status.NeedPair, a.status.LastErr = true, false, ""
	a.status.LastReport, a.status.Results = a.now(), res
	a.status.ServerName, a.status.Writer = hints.Name, hints.Writer
	if r, ok := res["activity"]; ok {
		if r.OK {
			a.status.Activity = or(r.Label, map[bool]string{true: "不在电脑前（空闲 / 锁屏）"}[r.Idle])
		} else {
			a.status.Activity = "没写进去：" + r.Error
		}
	}
	if m, ok := body["music"].(M); ok {
		if m["playing"] == true {
			a.status.Music = m["title"].(string) + " — " + m["artist"].(string)
		} else {
			a.status.Music = ""
		}
	}
	a.st.LastOK = a.now()
	a.mu.Unlock()
	a.notify()
}

func (a *Agent) reportLoop() {
	a.reportOnce()
	for {
		select {
		case <-time.After(a.interval()):
		case <-a.kick:
		}
		a.reportOnce()
	}
}

/* 系统资源：4 秒一次；硬件信息 / 系统版本 / 磁盘这些不常变的 10 分钟带一次 */
func (a *Agent) statsOnce() {
	cfg, _ := a.snapshot()
	if !cfg.paired() || !cfg.Report.Stats || !cfg.hasScope("stats") || a.paused() {
		return
	}
	full := a.statsT.IsZero() || a.now().Sub(a.statsT) > 10*time.Minute
	sec := a.plat.Stats(full)
	if len(sec) == 0 {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	res, _, err := c.Report(M{"stats": sec})
	if err != nil {
		a.mu.Lock()
		a.status.StatsErr = err.Error()
		a.mu.Unlock()
		if isAuthErr(err) {
			a.handleErr("系统资源", err)
		}
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if r := res["stats"]; r.OK {
		if full {
			a.statsT = a.now()
		}
		a.status.StatsAt, a.status.StatsErr = a.now(), ""
	} else {
		a.status.StatsErr = r.Error
	}
}

func (a *Agent) statsLoop() {
	for {
		a.statsOnce()
		time.Sleep(4 * time.Second)
	}
}

/* ActivityWatch：前三天里没传完整的补上；23:50 以后先传一份今天的，过了零点再传完整的 */
func (a *Agent) awOnce() {
	cfg, _ := a.snapshot()
	if !cfg.paired() || !cfg.Report.AW || !cfg.hasScope("aw") {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	aw := newAW(cfg.AWURL)
	now := a.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	for i := 3; i >= 1; i-- {
		d := today.AddDate(0, 0, -i)
		key := d.Format("2006-01-02")
		a.mu.Lock()
		done := a.st.AWDone[key] != ""
		a.mu.Unlock()
		if done {
			continue
		}
		if err := a.uploadDay(c, aw, d, true); err != nil {
			return // ActivityWatch 没开 / 网络不通：下一轮再说
		}
	}
	if now.Hour() == 23 && now.Minute() >= 50 {
		a.mu.Lock()
		last := a.status.AWAt
		a.mu.Unlock()
		if last.Before(today.Add(23*time.Hour + 50*time.Minute)) {
			_ = a.uploadDay(c, aw, today, false)
		}
	}
}

func (a *Agent) uploadDay(c *Client, aw *AW, day time.Time, complete bool) error {
	key := day.Format("2006-01-02")
	raw, counts, err := aw.ExportDay(day)
	if err != nil {
		a.mu.Lock()
		a.status.AWMsg = err.Error()
		a.mu.Unlock()
		return err
	}
	if counts["window"] == 0 && counts["afk"] == 0 {
		if complete {
			a.mu.Lock()
			a.st.AWDone[key] = "empty"
			a.mu.Unlock()
			saveState(a.st)
		}
		return nil
	}
	exists, err := c.UploadAW(raw)
	if err != nil {
		a.mu.Lock()
		a.status.AWMsg = "上传 " + key + " 失败：" + err.Error()
		a.mu.Unlock()
		if isAuthErr(err) {
			a.handleErr("上传 ActivityWatch", err)
		} else {
			logf("上传 %s 的 ActivityWatch 失败：%v", key, err)
		}
		return err
	}
	a.mu.Lock()
	a.status.AWAt = a.now()
	note := ""
	if exists {
		note = "（服务器上已有这天的日报，不会重写）"
	}
	a.status.AWMsg = "已上传 " + key + note
	if complete {
		a.st.AWDone[key] = a.now().Format(time.RFC3339)
	}
	a.mu.Unlock()
	saveState(a.st)
	logf("已上传 %s 的 ActivityWatch（窗口 %d 条，%d KB）%s", key, counts["window"], len(raw)/1024, note)
	a.notify()
	return nil
}

// UploadToday：托盘菜单「立即上传今天的」
func (a *Agent) UploadToday() error {
	c := a.client()
	if c == nil {
		return errNotPaired
	}
	cfg, _ := a.snapshot()
	return a.uploadDay(c, newAW(cfg.AWURL), a.now(), false)
}

func (a *Agent) awLoop() {
	time.Sleep(90 * time.Second) // 开机时 ActivityWatch 可能还没起来
	for {
		a.awOnce()
		time.Sleep(10 * time.Minute)
	}
}

func (a *Agent) deviceLoop() {
	for {
		a.refreshDevice()
		time.Sleep(30 * time.Minute)
	}
}

func (a *Agent) Start() {
	safeGo("设备信息", a.deviceLoop)
	safeGo("上报", a.reportLoop)
	safeGo("系统资源", a.statsLoop)
	safeGo("ActivityWatch", a.awLoop)
}

type agentErr string

func (e agentErr) Error() string { return string(e) }

const errNotPaired = agentErr("还没配对")
