package main

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

/* ---------------- 设置页：读 / 存配置 ---------------- */

// 密钥类字段：读的时候只说「有没有」，存的时候留空表示不改
var secretFields = []string{"bangumi.token", "jellyfin.key", "netease.bind_key"}

func secretPtr(c *Config, name string) *string {
	switch name {
	case "bangumi.token":
		return &c.Bangumi.Token
	case "jellyfin.key":
		return &c.Jellyfin.Key
	case "netease.bind_key":
		return &c.NetEase.BindKey
	}
	return nil
}

func redactedConfig() M {
	c := conf()
	set := M{}
	for _, f := range secretFields {
		set[f] = *secretPtr(c, f) != ""
	}
	return M{
		"site":      c.Site,
		"profile":   c.Profile,
		"timezone":  c.Timezone,
		"netease":   M{"api": c.NetEase.API, "refresh_hours": c.NetEase.RefreshHours},
		"bangumi":   M{"user": c.Bangumi.User},
		"jellyfin":  M{"url": c.Jellyfin.URL, "user": c.Jellyfin.User},
		"daylog_db": c.DaylogDB,
		"stats": M{"pc_name": c.Stats.PCName, "server_id": c.Stats.ServerID, "server_name": c.Stats.ServerName,
			"host_proc": c.Stats.HostProc, "host_root": c.Stats.HostRoot, "disks": c.Stats.Disks},
		"secrets_set": set,
	}
}

func adminStatus() M {
	_, prof := getOwner()
	ne := M{"bound": ownerBound()}
	if pm := asM(prof); pm != nil {
		ne["nickname"], ne["uid"] = toStr(pm["nickname"]), toStr(pm["userId"])
	}
	for k, v := range nsStatus() {
		ne[k] = v
	}
	wallMu.Lock()
	wd := wallData
	wallMu.Unlock()
	anime := videosStatus()
	if wd != nil {
		anime["sources"] = wd["sources"]
		anime["updated"] = wd["updated"]
	}
	var pcAt int64
	if pc := pcSlotIf("pc"); pc != nil {
		pc.mu.Lock()
		pcAt = pc.at
		pc.mu.Unlock()
	}
	var pcLast any
	if pcAt > 0 {
		pcLast = isoUTC(time.UnixMilli(pcAt))
	}
	_, ext := customAvatar()
	return M{"netease": ne, "anime": anime, "daylog_ready": daylogDB() != nil,
		"pc": M{"online": hostAlive("pc"), "last": pcLast}, "custom_avatar": ext != ""}
}

type configPatch struct {
	Config struct {
		Site    *struct{ Title, Domain, Description string } `json:"site"`
		Profile *struct {
			Name, Handle, Email string
			Links               []Link `json:"links"`
		} `json:"profile"`
		Timezone *string `json:"timezone"`
		NetEase  *struct {
			API          string  `json:"api"`
			RefreshHours float64 `json:"refresh_hours"`
		} `json:"netease"`
		Bangumi  *struct{ User string }      `json:"bangumi"`
		Jellyfin *struct{ URL, User string } `json:"jellyfin"`
		DaylogDB *string                     `json:"daylog_db"`
		Stats    *struct {
			PCName     string `json:"pc_name"`
			ServerID   string `json:"server_id"`
			ServerName string `json:"server_name"`
			Disks      []Disk `json:"disks"`
		} `json:"stats"`
	} `json:"config"`
	Secrets map[string]string `json:"secrets"` // 填了才改
	Clear   []string          `json:"clear"`   // 明确要清空的
}

func limit(s string, n int, name string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		return "", errors.New(name + " 太长（最多 " + itoa(int64(n)) + " 字）")
	}
	return s, nil
}

// httpURL：空串可以；否则必须是 http(s)，去掉末尾的 /
func httpURL(s, name string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New(name + " 不是有效的 http(s) 地址")
	}
	return s, nil
}

func applyPatch(c *Config, p *configPatch) error {
	var err error
	if s := p.Config.Site; s != nil {
		if c.Site.Title, err = limit(s.Title, 60, "站点标题"); err != nil {
			return err
		}
		if c.Site.Domain, err = limit(s.Domain, 100, "域名"); err != nil {
			return err
		}
		if c.Site.Description, err = limit(s.Description, 200, "站点描述"); err != nil {
			return err
		}
	}
	if pr := p.Config.Profile; pr != nil {
		if c.Profile.Name, err = limit(pr.Name, 40, "名字"); err != nil {
			return err
		}
		if c.Profile.Handle, err = limit(strings.TrimPrefix(strings.TrimSpace(pr.Handle), "@"), 40, "ID"); err != nil {
			return err
		}
		if c.Profile.Email, err = limit(pr.Email, 120, "邮箱"); err != nil {
			return err
		}
		if c.Profile.Email != "" && !strings.Contains(c.Profile.Email, "@") {
			return errors.New("邮箱格式不对")
		}
		if len(pr.Links) > 12 {
			return errors.New("链接最多 12 个")
		}
		links := []Link{}
		for _, l := range pr.Links {
			l.Title, _ = limit(l.Title, 40, "")
			l.Sub, _ = limit(l.Sub, 80, "")
			l.URL = strings.TrimSpace(l.URL)
			if l.Title == "" && l.URL == "" {
				continue
			}
			u, perr := url.Parse(l.URL)
			if perr != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
				return errors.New("链接「" + l.Title + "」的地址要以 https:// 或 mailto: 开头")
			}
			links = append(links, l)
		}
		c.Profile.Links = links
	}
	if tz := p.Config.Timezone; tz != nil {
		if _, terr := time.LoadLocation(strings.TrimSpace(*tz)); terr != nil || strings.TrimSpace(*tz) == "" {
			return errors.New("时区不认识（例：Asia/Shanghai）")
		}
		c.Timezone = strings.TrimSpace(*tz)
	}
	if n := p.Config.NetEase; n != nil {
		if c.NetEase.API, err = httpURL(n.API, "网易云 API 地址"); err != nil {
			return err
		}
		if n.RefreshHours < 1 || n.RefreshHours > 168 {
			return errors.New("网易云刷新间隔要在 1 到 168 小时之间")
		}
		c.NetEase.RefreshHours = n.RefreshHours
	}
	if b := p.Config.Bangumi; b != nil {
		c.Bangumi.User, _ = limit(b.User, 60, "")
	}
	if j := p.Config.Jellyfin; j != nil {
		if c.Jellyfin.URL, err = httpURL(j.URL, "Jellyfin 地址"); err != nil {
			return err
		}
		c.Jellyfin.User, _ = limit(j.User, 60, "")
	}
	if d := p.Config.DaylogDB; d != nil {
		v := strings.TrimSpace(*d)
		if v != "" && !filepath.IsAbs(v) {
			return errors.New("daylog 数据库要写绝对路径")
		}
		c.DaylogDB = v
	}
	if s := p.Config.Stats; s != nil {
		c.Stats.PCName, _ = limit(s.PCName, 60, "")
		c.Stats.ServerName, _ = limit(s.ServerName, 60, "")
		id, _ := limit(s.ServerID, 60, "")
		if id == "" || id == "pc" {
			return errors.New("服务器 ID 不能为空，也不能叫 pc")
		}
		c.Stats.ServerID = id
		disks := []Disk{}
		for _, d := range s.Disks {
			d.Label, d.Path = strings.TrimSpace(d.Label), strings.TrimSpace(d.Path)
			if d.Label == "" && d.Path == "" {
				continue
			}
			if d.Label == "" || !filepath.IsAbs(d.Path) {
				return errors.New("磁盘要有名字，路径要写绝对路径")
			}
			disks = append(disks, d)
		}
		if len(disks) == 0 {
			return errors.New("至少保留一个磁盘")
		}
		c.Stats.Disks = disks
	}
	for _, name := range p.Clear {
		if ptr := secretPtr(c, name); ptr != nil {
			*ptr = ""
		}
	}
	for name, v := range p.Secrets {
		v = strings.TrimSpace(v)
		if ptr := secretPtr(c, name); ptr != nil && v != "" {
			*ptr = v
		}
	}
	return nil
}

func adminSaveConfig(w http.ResponseWriter, r *http.Request) {
	var p configPatch
	if !adminPost(w, r, &p) {
		return
	}
	old := conf()
	c := cloneConfig(old)
	if err := applyPatch(c, &p); err != nil {
		writeJSON(w, 400, M{"ok": false, "error": err.Error()})
		return
	}
	if err := commitConfig(c); err != nil {
		writeJSON(w, 500, M{"ok": false, "error": err.Error()})
		return
	}
	afterConfigChange(old, c)
	writeJSON(w, 200, M{"ok": true, "config": redactedConfig(), "status": adminStatus()})
}

// afterConfigChange：改了哪块就把哪块的缓存丢掉，不用重启
func afterConfigChange(old, c *Config) {
	if old.Timezone != c.Timezone {
		if l, err := time.LoadLocation(c.Timezone); err == nil {
			loc = l
		}
	}
	if old.Bangumi != c.Bangumi || old.Jellyfin != c.Jellyfin {
		resetJfUser()
		wallMu.Lock()
		wallData, wallAt = nil, time.Time{}
		wallMu.Unlock()
	}
	if old.DaylogDB != c.DaylogDB {
		dbMu.Lock()
		if dbHandle != nil {
			dbHandle.Close()
			dbHandle = nil
		}
		dbFailedAt = time.Time{}
		dbMu.Unlock()
		histMu.Lock()
		histData = nil
		histMu.Unlock()
		feedMu.Lock()
		feedData = nil
		feedMu.Unlock()
	}
	srv.mu.Lock()
	srv.disks = nil
	srv.mu.Unlock()
}

/* ---------------- 给前端用的公开站点信息 ---------------- */

func siteInfo() M {
	c := conf()
	ne := ""
	if c.NetEase.API != "" {
		ne = c.NetEase.API + "/"
	}
	return M{
		"title": c.Site.Title, "domain": c.Site.Domain, "description": c.Site.Description,
		"name": c.Profile.Name, "handle": c.Profile.Handle, "email": c.Profile.Email,
		"links": c.Profile.Links, "ne_api": ne,
	}
}
