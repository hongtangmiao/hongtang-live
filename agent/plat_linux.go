//go:build linux

package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

/*
 * Linux：
 *   前台窗口  X11（_NET_ACTIVE_WINDOW）；Wayland 下拿不到，改用 ActivityWatch 的 aw-watcher-window / awatcher 兜底
 *   空闲      X11 屏保扩展；没有 X 就问 GNOME Mutter / freedesktop ScreenSaver（D-Bus）
 *   锁屏      freedesktop / GNOME ScreenSaver 的 GetActive
 *   正在播放  MPRIS（D-Bus），网易云 / Spotify / 浏览器都支持
 */

type linPlat struct {
	mu     sync.Mutex
	x      *xgb.Conn
	xTried bool
	root   xproto.Window
	atoms  map[string]xproto.Atom
	ss     bool
	bus    *dbus.Conn
	stats  *linStats
}

func newPlatform() Platform { return &linPlat{atoms: map[string]xproto.Atom{}, stats: newLinStats()} }

func (p *linPlat) Kind() string { return "linux" }

func (p *linPlat) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.x != nil {
		p.x.Close()
	}
	if p.bus != nil {
		p.bus.Close()
	}
}

func (p *linPlat) xconn() *xgb.Conn {
	if p.x != nil || p.xTried {
		return p.x
	}
	p.xTried = true
	if os.Getenv("DISPLAY") == "" {
		return nil
	}
	c, err := xgb.NewConn()
	if err != nil {
		logf("连不上 X11（%v），前台窗口改用 ActivityWatch", err)
		return nil
	}
	p.x = c
	p.root = xproto.Setup(c).DefaultScreen(c).Root
	p.ss = screensaver.Init(c) == nil
	return c
}

func (p *linPlat) atom(name string) xproto.Atom {
	if a, ok := p.atoms[name]; ok {
		return a
	}
	r, err := xproto.InternAtom(p.x, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0
	}
	p.atoms[name] = r.Atom
	return r.Atom
}

func (p *linPlat) prop(w xproto.Window, name string, typ xproto.Atom, n uint32) []byte {
	r, err := xproto.GetProperty(p.x, false, w, p.atom(name), typ, 0, n).Reply()
	if err != nil || r == nil {
		return nil
	}
	return r.Value
}

var linDesktopClasses = map[string]bool{"desktop_window": true, "xfdesktop": true, "nautilus-desktop": true, "plasmashell": true, "nemo-desktop": true,
	"caja": false, "xfce4-panel": true, "gnome-shell": true}

var genericExe = map[string]bool{"electron": true, "python": true, "python3": true, "java": true, "node": true, "bwrap": true, "wine": true,
	"wine64-preloader": true, "flatpak": true, "steam": false}

func (p *linPlat) Window() WinInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" && os.Getenv("HL_FORCE_X11") == "" {
		return WinInfo{Unsupported: true} // XWayland 只看得到 X 程序，会报错，宁可用 ActivityWatch
	}
	if p.xconn() == nil {
		return WinInfo{Unsupported: true}
	}
	v := p.prop(p.root, "_NET_ACTIVE_WINDOW", xproto.AtomWindow, 1)
	if len(v) < 4 {
		return WinInfo{Desktop: true}
	}
	w := xproto.Window(xgb.Get32(v))
	if w == 0 {
		return WinInfo{Desktop: true}
	}
	title := string(p.prop(w, "_NET_WM_NAME", p.atom("UTF8_STRING"), 1024))
	if title == "" {
		title = string(p.prop(w, "WM_NAME", xproto.AtomString, 1024))
	}
	cls := strings.Split(string(p.prop(w, "WM_CLASS", xproto.AtomString, 256)), "\x00")
	class := ""
	if len(cls) > 1 {
		class = cls[1]
	} else if len(cls) == 1 {
		class = cls[0]
	}
	app := ""
	if pv := p.prop(w, "_NET_WM_PID", xproto.AtomCardinal, 1); len(pv) >= 4 {
		pid := strconv.Itoa(int(xgb.Get32(pv)))
		if exe, err := os.Readlink("/proc/" + pid + "/exe"); err == nil {
			app = strings.TrimSuffix(filepath.Base(strings.TrimSuffix(exe, " (deleted)")), "-bin")
		} else if b, err := os.ReadFile("/proc/" + pid + "/comm"); err == nil {
			app = strings.TrimSpace(string(b))
		}
	}
	if app == "" || genericExe[strings.ToLower(app)] {
		app = strings.ToLower(or(class, app))
	}
	info := WinInfo{App: app, Title: strings.TrimSpace(title), Class: class}
	wt := p.prop(w, "_NET_WM_WINDOW_TYPE", xproto.AtomAtom, 4)
	for i := 0; i+4 <= len(wt); i += 4 {
		if xproto.Atom(xgb.Get32(wt[i:])) == p.atom("_NET_WM_WINDOW_TYPE_DESKTOP") {
			info.Desktop = true
		}
	}
	if linDesktopClasses[strings.ToLower(class)] {
		info.Desktop = true
	}
	return info
}

func (p *linPlat) session() *dbus.Conn {
	if p.bus != nil && p.bus.Connected() {
		return p.bus
	}
	c, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil
	}
	p.bus = c
	return c
}

func (p *linPlat) IdleSeconds() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if os.Getenv("XDG_SESSION_TYPE") != "wayland" && p.xconn() != nil && p.ss {
		if r, err := screensaver.QueryInfo(p.x, xproto.Drawable(p.root)).Reply(); err == nil {
			return float64(r.MsSinceUserInput) / 1000
		}
	}
	bus := p.session()
	if bus == nil {
		return -1
	}
	var ms uint64
	if bus.Object("org.gnome.Mutter.IdleMonitor", "/org/gnome/Mutter/IdleMonitor/Core").Call("org.gnome.Mutter.IdleMonitor.GetIdletime", 0).Store(&ms) == nil {
		return float64(ms) / 1000
	}
	var sec uint32
	if bus.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver").Call("org.freedesktop.ScreenSaver.GetSessionIdleTime", 0).Store(&sec) == nil {
		return float64(sec)
	}
	return -1
}

func (p *linPlat) Locked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	bus := p.session()
	if bus == nil {
		return false
	}
	for _, s := range [][3]string{
		{"org.gnome.ScreenSaver", "/org/gnome/ScreenSaver", "org.gnome.ScreenSaver.GetActive"},
		{"org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver.GetActive"},
	} {
		var on bool
		if bus.Object(s[0], dbus.ObjectPath(s[1])).Call(s[2], 0).Store(&on) == nil {
			return on
		}
	}
	return false
}

func (p *linPlat) Media() MediaInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	bus := p.session()
	if bus == nil {
		return MediaInfo{Err: true}
	}
	var names []string
	if bus.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names) != nil {
		return MediaInfo{Err: true}
	}
	sort.Strings(names)
	var paused *MediaInfo
	for _, n := range names {
		if !strings.HasPrefix(n, "org.mpris.MediaPlayer2.") {
			continue
		}
		obj := bus.Object(n, "/org/mpris/MediaPlayer2")
		st, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.PlaybackStatus")
		if err != nil {
			continue
		}
		status, _ := st.Value().(string)
		m := MediaInfo{Status: status, App: mprisApp(n)}
		if md, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.Metadata"); err == nil {
			if mm, ok := md.Value().(map[string]dbus.Variant); ok {
				m.Title, _ = mm["xesam:title"].Value().(string)
				m.Album, _ = mm["xesam:album"].Value().(string)
				switch a := mm["xesam:artist"].Value().(type) {
				case []string:
					m.Artist = strings.Join(a, " / ")
				case string:
					m.Artist = a
				}
			}
		}
		if status == "Playing" {
			return m
		}
		if paused == nil {
			c := m
			paused = &c
		}
	}
	if paused != nil {
		return *paused
	}
	return MediaInfo{Status: "none"}
}

func (p *linPlat) Stats(full bool) M { return p.stats.collect(full) }
