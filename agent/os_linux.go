//go:build linux

package main

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

func attachConsole()          {}
func consoleAttached() bool   { return true }

func rawHostname() string {
	h, _ := os.Hostname()
	return strings.TrimSpace(h)
}
func protect(s string) (string, error) { return "plain:" + s, nil } // 配置文件本身是 600
func unprotect(s string) (string, error) {
	if !strings.HasPrefix(s, "plain:") {
		return "", errors.New("口令格式不对")
	}
	return strings.TrimPrefix(s, "plain:"), nil
}

func singleInstance() (func(), bool) {
	_ = os.MkdirAll(dataDirFn(), 0o700)
	f, err := os.OpenFile(filepath.Join(dataDirFn(), "agent.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, true
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return func() {}, false
	}
	return func() { f.Close() }, true
}

func openURL(u string) {
	for _, c := range []string{"xdg-open", "gio", "sensible-browser"} {
		if p, err := exec.LookPath(c); err == nil {
			args := []string{u}
			if c == "gio" {
				args = []string{"open", u}
			}
			cmd := exec.Command(p, args...)
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
				return
			}
		}
	}
	logf("打不开浏览器，请手动打开：%s", u)
}

func runtimeDesc() string {
	s := "linux/" + runtime.GOARCH
	if t := os.Getenv("XDG_SESSION_TYPE"); t != "" {
		s += " " + t
	}
	if d := os.Getenv("XDG_CURRENT_DESKTOP"); d != "" {
		s += " " + d
	}
	return s
}

func exePath() string {
	p, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

func autostartFile() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "autostart", "hongtang-live.desktop")
}

func desktopEntry(extra string) string {
	return "[Desktop Entry]\nType=Application\nName=红糖似了吗？\nComment=hongtang-live 采集程序\nExec=\"" + exePath() + "\"" + extra +
		"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

func autostartEnabled() bool {
	b, err := os.ReadFile(autostartFile())
	return err == nil && strings.Contains(string(b), exePath())
}

func setAutostart(on bool) error {
	if !on {
		if err := os.Remove(autostartFile()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(autostartFile()), 0o700)
	return os.WriteFile(autostartFile(), []byte(desktopEntry("")), 0o600)
}

// registerProtocol：hlpair:// 交给本程序（桌面环境支持 x-scheme-handler 的话）
func registerProtocol() {
	d, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(d, ".local", "share", "applications")
	f := filepath.Join(dir, "hongtang-live-pair.desktop")
	body := desktopEntry(" %u") + "NoDisplay=true\nMimeType=x-scheme-handler/hlpair;\n"
	if old, _ := os.ReadFile(f); string(old) == body {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	if os.WriteFile(f, []byte(body), 0o600) == nil {
		if p, err := exec.LookPath("xdg-mime"); err == nil {
			_ = exec.Command(p, "default", "hongtang-live-pair.desktop", "x-scheme-handler/hlpair").Run()
		}
	}
}

func netInterfaceByName(name string) ([]net.IP, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil, err
	}
	var out []net.IP
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP)
		}
	}
	return out, nil
}
