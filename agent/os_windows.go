//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"


	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var procAttachConsole = kernel32.NewProc("AttachConsole")

// attachConsole：程序是 GUI 程序（-H windowsgui，双击不弹黑框），从命令行跑的时候把输出接回父控制台
func attachConsole() {
	if r, _, _ := procAttachConsole.Call(uintptr(^uint32(0))); r == 0 {
		consoleOK = false
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
		log.SetOutput(f)
	}
	fmt.Println()
}

func consoleAttached() bool { return false }

// rawHostname：Windows 上 os.Hostname 拿到的是 NetBIOS 名（大写 ASCII），中文电脑名要用 DNS 名
func rawHostname() string {
	// 和原来的 live_agent.py 一样先用 COMPUTERNAME：daylog 里这台电脑的记录都是按它记的
	if h := strings.TrimSpace(os.Getenv("COMPUTERNAME")); h != "" {
		return h
	}
	n := uint32(256)
	buf := make([]uint16, n)
	const computerNameDnsHostname = 1
	if windows.GetComputerNameEx(computerNameDnsHostname, &buf[0], &n) == nil && n > 0 {
		return windows.UTF16ToString(buf[:n])
	}
	h, _ := os.Hostname()
	return strings.TrimSpace(h)
}

func singleInstance() (func(), bool) {
	name, _ := windows.UTF16PtrFromString(`Local\HongtangLiveAgent`)
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return func() {}, false
	}
	return func() { windows.CloseHandle(h) }, true
}

func openURL(u string) {
	verb, _ := windows.UTF16PtrFromString("open")
	p, _ := windows.UTF16PtrFromString(u)
	_ = windows.ShellExecute(0, verb, p, nil, nil, windows.SW_SHOWNORMAL)
}

func runtimeDesc() string {
	ver := ""
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		b, _, _ := k.GetStringValue("CurrentBuild")
		d, _, _ := k.GetStringValue("DisplayVersion")
		ver = " build " + b + " " + d
		k.Close()
	}
	return "windows/" + runtime.GOARCH + ver
}

/* ---------------- 口令：DPAPI ---------------- */

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

var entropy = []byte("hongtang-live/agent")

func protect(s string) (string, error) {
	var out windows.DataBlob
	desc, _ := windows.UTF16PtrFromString("hongtang-live device token")
	if err := windows.CryptProtectData(blob([]byte(s)), desc, blob(entropy), 0, nil, 1, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return "dpapi:" + base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

func unprotect(enc string) (string, error) {
	if !strings.HasPrefix(enc, "dpapi:") {
		return "", errors.New("不是 DPAPI 加密的口令")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, "dpapi:"))
	if err != nil {
		return "", err
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(raw), nil, blob(entropy), 0, nil, 1, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}

/* ---------------- 开机自启 / 配对链接 ---------------- */

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "HongtangLive"

func exePath() string {
	p, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runName)
	return err == nil && strings.EqualFold(strings.Trim(v, `"`), exePath())
}

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		logf("已关闭开机自启")
		return nil
	}
	logf("已打开开机自启：%s", exePath())
	return k.SetStringValue(runName, `"`+exePath()+`"`)
}

// registerProtocol：点网页上的 hlpair:// 链接时打开本程序（只写当前用户的注册表，不需要管理员）
func registerProtocol() {
	cmd := `"` + exePath() + `" "%1"`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\hlpair\shell\open\command`, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return
	}
	old, _, _ := k.GetStringValue("")
	if old != cmd {
		_ = k.SetStringValue("", cmd)
		if r, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\hlpair`, registry.SET_VALUE); err == nil {
			_ = r.SetStringValue("", "URL:红糖似了吗？配对")
			_ = r.SetStringValue("URL Protocol", "")
			r.Close()
		}
	}
	k.Close()
}
