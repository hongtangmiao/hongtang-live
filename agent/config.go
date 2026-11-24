package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/*
 * 配置：%APPDATA%\hongtang-live\agent.json（Linux：~/.config/hongtang-live/agent.json），权限 600。
 * 设备口令在 Windows 上用 DPAPI 加密（只有当前用户能解开），Linux 上靠文件权限。
 * 运行状态（日志、ActivityWatch 上传记录、界面端口）放在 %LOCALAPPDATA%\hongtang-live / ~/.cache/hongtang-live。
 */

type DeviceInfo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Host   string   `json:"host"`
	Scopes []string `json:"scopes"`
}

type ReportOpts struct {
	Activity    bool `json:"activity"`
	Music       bool `json:"music"`
	Stats       bool `json:"stats"`
	AW          bool `json:"aw"`
	IntervalSec int  `json:"interval_sec"` // 0 = 听服务器的（默认 30）
	IdleMin     int  `json:"idle_min"`     // 这么久没碰键鼠、也没在放东西，就算不在电脑前；0 = 不判断
}

type Config struct {
	Server      string     `json:"server"`
	TokenEnc    string     `json:"token_enc,omitempty"`
	Device      DeviceInfo `json:"device"`
	Report      ReportOpts `json:"report"`
	AWURL       string     `json:"aw_url"`
	PausedUntil time.Time  `json:"paused_until,omitempty"`
}

func defaultConfig() *Config {
	return &Config{Report: ReportOpts{Activity: true, Music: true, Stats: true, AW: true, IdleMin: 15}, AWURL: "http://127.0.0.1:5600"}
}

var (
	cfgMu     sync.Mutex
	cfgDirFn  = func() string { d, _ := os.UserConfigDir(); return filepath.Join(d, "hongtang-live") }
	dataDirFn = func() string { d, _ := os.UserCacheDir(); return filepath.Join(d, "hongtang-live") }
)

func configPath() string { return filepath.Join(cfgDirFn(), "agent.json") }

func loadConfig() *Config {
	c := defaultConfig()
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, c)
	}
	if c.AWURL == "" {
		c.AWURL = "http://127.0.0.1:5600"
	}
	return c
}

func saveConfig(c *Config) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if err := os.MkdirAll(cfgDirFn(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeAtomic(configPath(), b, 0o600)
}

func writeAtomic(path string, b []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	_ = os.Chmod(tmp, perm)
	return os.Rename(tmp, path)
}

func (c *Config) token() string {
	if c.TokenEnc == "" {
		return ""
	}
	t, err := unprotect(c.TokenEnc)
	if err != nil {
		logf("口令解不开（换了用户 / 系统重装？）：%v，需要重新配对", err)
		return ""
	}
	return t
}

func (c *Config) setToken(t string) error {
	if t == "" {
		c.TokenEnc = ""
		return nil
	}
	enc, err := protect(t)
	if err != nil {
		return err
	}
	c.TokenEnc = enc
	return nil
}

func (c *Config) paired() bool { return c.Server != "" && c.TokenEnc != "" }

func (c *Config) hasScope(s string) bool {
	for _, x := range c.Device.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

/* ---------------- 运行状态（不重要，丢了就重新来） ---------------- */

type State struct {
	AWDone  map[string]string `json:"aw_done"` // 日期 → 上传时间（那天结束之后才算完整）
	UIPort  int               `json:"ui_port"`
	UIKey   string            `json:"ui_key"`
	LastOK  time.Time         `json:"last_ok"`
	Version string            `json:"version"`
}

var stMu sync.Mutex

func statePath() string { return filepath.Join(dataDirFn(), "state.json") }

func loadState() *State {
	s := &State{AWDone: map[string]string{}}
	b, err := os.ReadFile(statePath())
	if err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.AWDone == nil {
		s.AWDone = map[string]string{}
	}
	return s
}

func saveState(s *State) {
	stMu.Lock()
	defer stMu.Unlock()
	_ = os.MkdirAll(dataDirFn(), 0o700)
	for d := range s.AWDone { // 只留最近 30 天
		if t, err := time.Parse("2006-01-02", d); err == nil && time.Since(t) > 30*24*time.Hour {
			delete(s.AWDone, d)
		}
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = writeAtomic(statePath(), b, 0o600)
}

/* ---------------- 配对链接 ---------------- */

// parsePairInput：认 hlpair://pair?s=…&c=…，也认「服务器地址 + 空格 + 配对码」
func parsePairInput(link, server, code string) (string, string, error) {
	link = strings.TrimSpace(link)
	if link != "" {
		if strings.HasPrefix(strings.ToLower(link), "hlpair:") {
			q := link[strings.Index(link, "?")+1:]
			if !strings.Contains(link, "?") {
				return "", "", errors.New("配对链接不完整")
			}
			vals, err := parseQuery(q)
			if err != nil {
				return "", "", errors.New("配对链接看不懂")
			}
			server, code = vals["s"], vals["c"]
		} else if f := strings.Fields(link); len(f) == 2 {
			server, code = f[0], f[1]
		} else {
			return "", "", errors.New("请粘贴设置页给的配对链接（hlpair:// 开头）")
		}
	}
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		return "", "", errors.New("服务器地址要以 http:// 或 https:// 开头")
	}
	if len(strings.NewReplacer("-", "", " ", "").Replace(code)) != 8 {
		return "", "", errors.New("配对码是 8 位（XXXX-XXXX）")
	}
	return server, code, nil
}
