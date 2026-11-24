package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Disk struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// Link 是账户页「关于我」里的一张卡片
type Link struct {
	Title  string `json:"title"`
	Sub    string `json:"sub"`
	URL    string `json:"url"`
	Avatar bool   `json:"avatar"` // 用自己的头像当图标
}

// Config 全部配置都在 data/config.json 里；第一次启动时从旧的环境变量导入
type Config struct {
	Site struct {
		Title       string `json:"title"`
		Domain      string `json:"domain"`
		Description string `json:"description"`
	} `json:"site"`
	Profile struct {
		Name   string `json:"name"`
		Handle string `json:"handle"`
		Email  string `json:"email"`
		Links  []Link `json:"links"`
	} `json:"profile"`
	Timezone string `json:"timezone"`
	NetEase  struct {
		API          string  `json:"api"`
		BindKey      string  `json:"bind_key"`
		RefreshHours float64 `json:"refresh_hours"`
	} `json:"netease"`
	Bangumi struct {
		User  string `json:"user"`
		Token string `json:"token"`
	} `json:"bangumi"`
	Jellyfin struct {
		URL  string `json:"url"`
		Key  string `json:"key"`
		User string `json:"user"`
	} `json:"jellyfin"`
	DaylogDB     string `json:"daylog_db"`
	DaylogRoot   string `json:"daylog_root"` // daylog 数据目录（inbox/raw/reports/logs），空则用 env DAYLOG_ROOT
	DaylogWriter bool   `json:"daylog_writer"`
	Daylog       M      `json:"daylog,omitempty"` // 原 daylog config.json 的内容（隐私规则、分类、日报…）
	Stats        struct {
		PCName     string `json:"pc_name"`
		ServerID   string `json:"server_id"`
		ServerName string `json:"server_name"`
		HostProc   string `json:"host_proc"`
		HostRoot   string `json:"host_root"`
		Disks      []Disk `json:"disks"`
	} `json:"stats"`
	Admin struct {
		PasswordHash string `json:"password_hash"` // pbkdf2_sha256$轮数$盐$哈希（与 daylog 同格式）
		Secret       string `json:"secret"`        // 管理员登录 cookie 的签名密钥
		Epoch        int    `json:"epoch"`         // 改密码时 +1，旧 cookie 全部失效
	} `json:"admin"`
}

var (
	cfgMu     sync.RWMutex
	cfgSaveMu sync.Mutex
	cfgPath   string
	cfgv      *Config
)

func conf() *Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfgv
}

func setConfig(c *Config) {
	cfgMu.Lock()
	cfgv = c
	cfgMu.Unlock()
}

// cloneConfig：改配置时先复制一份，改好、存盘成功后再整体换上（读的一方永远拿到完整的一份）
func cloneConfig(c *Config) *Config {
	b, _ := json.Marshal(c)
	n := &Config{}
	_ = json.Unmarshal(b, n)
	return n
}

// commitConfig：存盘成功才生效
func commitConfig(c *Config) error {
	cfgSaveMu.Lock()
	defer cfgSaveMu.Unlock()
	if err := saveConfig(c); err != nil {
		return err
	}
	setConfig(c)
	return nil
}

func loadConfig(path string) (*Config, bool, error) {
	cfgPath = path
	b, err := os.ReadFile(path)
	fromEnv, dirty := false, false
	c := &Config{}
	switch {
	case err == nil:
		if err := json.Unmarshal(b, c); err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
	case os.IsNotExist(err):
		c = configFromEnv()
		fromEnv, dirty = true, true
	default:
		return nil, false, err
	}
	fillDefaults(c)
	if importDaylogConfig(c) {
		dirty = true
	}
	if c.Admin.Secret == "" {
		c.Admin.Secret = randHex(32)
		dirty = true
	}
	if dirty {
		if err := saveConfig(c); err != nil {
			return nil, false, err
		}
	}
	return c, fromEnv, nil
}

func saveConfig(c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(cfgPath, b, 0o600) // 里面有 token / key / 密码哈希，只给自己读
}

func fillDefaults(c *Config) {
	if c.Timezone == "" {
		c.Timezone = "Asia/Shanghai"
	}
	if c.NetEase.RefreshHours <= 0 {
		c.NetEase.RefreshHours = 12
	}
	if c.Stats.HostProc == "" {
		c.Stats.HostProc = "/host/proc"
	}
	if c.Stats.HostRoot == "" {
		c.Stats.HostRoot = "/host/root"
	}
	if c.Stats.PCName == "" {
		c.Stats.PCName = "PC"
	}
	if c.Stats.ServerID == "" {
		c.Stats.ServerID = "server"
	}
	if c.Stats.ServerName == "" {
		c.Stats.ServerName = "Server"
	}
	if len(c.Stats.Disks) == 0 {
		c.Stats.Disks = []Disk{{Label: "/", Path: "/data"}}
	}
	if c.Profile.Links == nil {
		c.Profile.Links = []Link{}
	}
}

// configFromEnv 只在第一次启动时用：把旧 compose / .env 里的值搬进 config.json
func configFromEnv() *Config {
	g := func(k string) string { return strings.TrimSpace(os.Getenv(k)) }
	c := &Config{}
	c.Site.Title = g("SITE_TITLE")
	c.Site.Domain = g("SITE_DOMAIN")
	c.Timezone = g("TZ")
	c.NetEase.API = strings.TrimRight(g("NE_API"), "/")
	c.NetEase.BindKey = g("BIND_KEY")
	if h, err := strconv.ParseFloat(g("NE_REFRESH_HOURS"), 64); err == nil {
		c.NetEase.RefreshHours = h
	}
	c.Bangumi.User = g("BGM_USER")
	c.Bangumi.Token = g("BGM_TOKEN")
	c.Jellyfin.URL = g("JF_URL")
	if c.Jellyfin.URL == "" && g("JF_HOST") != "" {
		port := g("JF_PORT")
		if port == "" {
			port = "8096"
		}
		c.Jellyfin.URL = "http://" + g("JF_HOST") + ":" + port
	}
	c.Jellyfin.URL = strings.TrimRight(c.Jellyfin.URL, "/")
	c.Jellyfin.Key = g("JF_KEY")
	c.Jellyfin.User = g("JF_USER")
	c.DaylogDB = g("DAYLOG_DB")
	c.DaylogRoot = g("DAYLOG_ROOT")
	dw := strings.ToLower(g("DAYLOG_WRITER"))
	c.DaylogWriter = dw != "false" && dw != "0" && dw != "no" && dw != "off"
	c.Stats.HostProc = g("HOST_PROC")
	c.Stats.HostRoot = g("HOST_ROOT")
	// 默认设备显示名：启动后可在设置页修改
	c.Stats.PCName = firstNonEmpty(g("STATS_PC_NAME"), "PC")
	c.Stats.ServerID = firstNonEmpty(g("STATS_SERVER_ID"), "server")
	c.Stats.ServerName = firstNonEmpty(g("STATS_SERVER_NAME"), "Server")
	root := firstNonEmpty(g("ROOT_DISK"), g("HOST_ROOT"))
	if root != "" {
		c.Stats.Disks = append(c.Stats.Disks, Disk{Label: "/", Path: root})
	}
	if vols, _ := filepath.Glob("/host/vol*"); len(vols) > 0 {
		sort.Strings(vols)
		for _, v := range vols {
			c.Stats.Disks = append(c.Stats.Disks, Disk{Label: "/" + filepath.Base(v), Path: v})
		}
	}
	return c
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
