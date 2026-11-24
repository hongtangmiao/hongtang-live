//go:build windows

package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo         = user32.NewProc("GetLastInputInfo")
	procOpenInputDesktop         = user32.NewProc("OpenInputDesktop")
	procCloseDesktop             = user32.NewProc("CloseDesktop")
	procGetUserObjectInformation = user32.NewProc("GetUserObjectInformationW")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetTickCount             = kernel32.NewProc("GetTickCount")
)

type winPlat struct {
	media *smtcHelper
	stats *winStats
}

func newPlatform() Platform {
	return &winPlat{media: &smtcHelper{}, stats: newWinStats()}
}

func (p *winPlat) Kind() string { return "windows" }

func (p *winPlat) Close() {
	p.media.stop()
	p.stats.close()
}

var desktopClasses = map[string]bool{"progman": true, "workerw": true, "shell_traywnd": true, "dv2controlhost": true,
	"shell_secondarytraywnd": true}

func (p *winPlat) Window() WinInfo {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return WinInfo{None: true}
	}
	cls := make([]uint16, 256)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 256)
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	tb := make([]uint16, n+2)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&tb[0])), n+2)
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	app := ""
	if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
		buf := make([]uint16, 1024)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil {
			app = filepath.Base(windows.UTF16ToString(buf[:size]))
		}
		windows.CloseHandle(h)
	}
	w := WinInfo{App: app, Title: strings.TrimSpace(windows.UTF16ToString(tb)), Class: windows.UTF16ToString(cls)}
	lc := strings.ToLower(w.Class)
	w.Desktop = desktopClasses[lc] || (strings.EqualFold(app, "explorer.exe") && w.Title == "")
	return w
}

func (p *winPlat) IdleSeconds() float64 {
	var lii struct {
		Size uint32
		Time uint32
	}
	lii.Size = 8
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii))); r == 0 {
		return -1
	}
	now, _, _ := procGetTickCount.Call()
	return float64(uint32(now)-lii.Time) / 1000 // 32 位毫秒数，回绕也算得对
}

// Locked：锁屏 / UAC 的时候输入桌面不是 Default
func (p *winPlat) Locked() bool {
	const desktopSwitch = 0x0100
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopSwitch)
	if h == 0 {
		return true
	}
	defer procCloseDesktop.Call(h)
	name := make([]uint16, 64)
	var need uint32
	const uoiName = 2
	if r, _, _ := procGetUserObjectInformation.Call(h, uoiName, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&need))); r == 0 {
		return false
	}
	return !strings.EqualFold(windows.UTF16ToString(name), "Default")
}

func (p *winPlat) Media() MediaInfo { return p.media.query() }

func (p *winPlat) Stats(full bool) M { return p.stats.collect(full) }

/* ---------------- 正在播放：常驻一个隐藏的 PowerShell 读 SMTC（WinRT），问一句答一行 ---------------- */

//go:embed smtc_loop.ps1
var smtcScript string

type smtcHelper struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   *bufio.Reader
	fails int
	until time.Time // 连续失败后歇一会儿
}

func (s *smtcHelper) start() error {
	dir := dataDirFn()
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "smtc_loop.ps1")
	// 带 BOM：Windows PowerShell 5.1 才会按 UTF-8 读中文注释
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte(smtcScript)...), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd, s.in, s.out = cmd, in, bufio.NewReaderSize(out, 64<<10)
	return nil
}

func (s *smtcHelper) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *smtcHelper) stopLocked() {
	if s.cmd != nil {
		_ = s.in.Close()
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
		s.cmd = nil
	}
}

func (s *smtcHelper) query() MediaInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().Before(s.until) {
		return MediaInfo{Err: true}
	}
	if s.cmd == nil {
		if err := s.start(); err != nil {
			logf("读不了正在播放（PowerShell 起不来）：%v", err)
			s.until = time.Now().Add(5 * time.Minute)
			return MediaInfo{Err: true}
		}
	}
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		if _, err := io.WriteString(s.in, "q\n"); err != nil {
			ch <- res{"", err}
			return
		}
		l, err := s.out.ReadString('\n')
		ch <- res{l, err}
	}()
	var r res
	select {
	case r = <-ch:
	case <-time.After(10 * time.Second):
		r.err = agentErr("超时")
	}
	if r.err != nil {
		s.fails++
		if s.fails == 1 || s.fails%20 == 0 {
			logf("读正在播放失败（第 %d 次）：%v，重启 PowerShell", s.fails, r.err)
		}
		s.stopLocked()
		if s.fails >= 3 {
			s.until = time.Now().Add(time.Minute)
		}
		return MediaInfo{Err: true}
	}
	s.fails = 0
	var m struct {
		Status       string `json:"status"`
		App          string `json:"app"`
		Title        string `json:"title"`
		Artist       string `json:"artist"`
		Album        string `json:"album"`
		PlaybackType int    `json:"playbackType"`
		Error        string `json:"error"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(r.line, "\ufeff"))), &m) != nil || m.Error != "" {
		return MediaInfo{Err: true}
	}
	return MediaInfo{Status: m.Status, App: m.App, Title: m.Title, Artist: m.Artist, Album: m.Album, PlaybackType: m.PlaybackType}
}
