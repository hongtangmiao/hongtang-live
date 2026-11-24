package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

/*
 * hl-agent：「红糖似了吗？」的电脑端采集程序（Windows / Linux）。
 * 替代原来的 live_agent.py（此刻在干嘛 + 正在听）、export_day.ps1（每天的 ActivityWatch）和 C# 系统资源采集器。
 *
 *   hl-agent                      托盘程序（双击运行）
 *   hl-agent --no-tray            不要托盘图标（Linux 没有托盘的桌面 / 服务器）
 *   hl-agent pair <配对链接>       命令行配对（或者 pair <服务器地址> <配对码>）
 *   hl-agent dump [文件]           采一次、不上传，结果写成 JSON（排查用）
 *   hl-agent unpair / version
 *   hl-agent hlpair://…           点配对链接时由系统调起
 */

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		attachConsole()
	} else {
		consoleOK = consoleAttached()
	}
	noTray := false
	var rest []string
	for _, x := range args {
		if x == "--no-tray" {
			noTray = true
		} else {
			rest = append(rest, x)
		}
	}
	cmd := ""
	if len(rest) > 0 {
		cmd = rest[0]
	}
	switch {
	case cmd == "__icon" && len(rest) > 1: // 构建时生成 exe 图标用
		_ = os.WriteFile(rest[1], drawIcon("ok", 256), 0o644)
		return
	case cmd == "version" || cmd == "--version":
		fmt.Println("hl-agent", version)
		return
	case cmd == "dump":
		out := ""
		if len(rest) > 1 {
			out = rest[1]
		}
		os.Exit(runDump(out))
	case cmd == "pair":
		os.Exit(runPairCLI(rest[1:]))
	case cmd == "unpair":
		p := newPlatform()
		newAgent(p).Unpair(true)
		p.Close()
		fmt.Println("已解除配对")
		return
	case cmd == "-h" || cmd == "--help" || cmd == "help":
		fmt.Println("用法：hl-agent [--no-tray] | pair <配对链接> | pair <服务器地址> <配对码> | dump [文件] | unpair | version")
		return
	}
	link := ""
	if strings.HasPrefix(strings.ToLower(cmd), "hlpair:") {
		link = cmd
	} else if cmd != "" {
		fmt.Fprintln(os.Stderr, "不认识的参数：", cmd)
		os.Exit(2)
	}

	release, first := singleInstance()
	if !first {
		// 已经在跑了：把配对链接转给它，然后打开它的设置页
		forwardToRunning(link)
		return
	}
	defer release()

	logf("hl-agent %s 启动（%s）", version, runtimeDesc())
	p := newPlatform()
	a := newAgent(p)
	ui, err := startUI(a)
	if err != nil {
		logf("本机设置页起不来：%v", err)
	}
	registerProtocol()
	if link != "" {
		if s, c, err := parsePairInput(link, "", ""); err == nil {
			if err := a.Pair(s, c, ""); err != nil {
				logf("配对失败：%v", err)
			}
		}
	}
	a.Start()
	cfg, _ := a.snapshot()
	if ui != nil && (!cfg.paired() || link != "") {
		openURL(ui.URL())
	}
	if noTray {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		if ui != nil {
			logf("设置页：%s", ui.URL())
		}
		<-sig
		p.Close()
		return
	}
	runTray(a, ui)
	p.Close()
	logf("退出")
}

func forwardToRunning(link string) {
	st := loadState()
	if st.UIPort == 0 {
		return
	}
	base := "http://127.0.0.1:" + strconv.Itoa(st.UIPort)
	if link != "" {
		b, _ := json.Marshal(M{"link": link})
		req, _ := http.NewRequest("POST", base+"/api/pair", strings.NewReader(string(b)))
		req.Header.Set("X-HL-Key", st.UIKey)
		req.Header.Set("X-HL-Agent", "1")
		req.Header.Set("Content-Type", "application/json")
		if resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req); err == nil {
			resp.Body.Close()
		}
	}
	openURL(base + "/?k=" + st.UIKey)
}

func runPairCLI(args []string) int {
	var link, server, code, name string
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.StringVar(&name, "name", "", "这台电脑在网站上叫什么")
	_ = fs.Parse(args)
	rest := fs.Args()
	switch len(rest) {
	case 1:
		link = rest[0]
	case 2:
		server, code = rest[0], rest[1]
	default:
		fmt.Println("用法：hl-agent pair [--name 名字] <配对链接>  或  hl-agent pair <服务器地址> <配对码>")
		return 2
	}
	s, c, err := parsePairInput(link, server, code)
	if err != nil {
		fmt.Println(err)
		return 2
	}
	p := newPlatform()
	defer p.Close()
	a := newAgent(p)
	if err := a.Pair(s, c, name); err != nil {
		fmt.Println("配对失败：", friendlyPairErr(err))
		return 1
	}
	cfg, _ := a.snapshot()
	fmt.Printf("配对成功：%s（host=%s）→ %s\n", cfg.Device.Name, cfg.Device.Host, cfg.Server)
	return 0
}

// runDump：采一次，不上传
func runDump(out string) int {
	p := newPlatform()
	defer p.Close()
	cfg := loadConfig()
	t0 := time.Now()
	w := p.Window()
	media := p.Media()
	res := M{"version": version, "runtime": runtimeDesc(), "time": t0.Format(time.RFC3339),
		"window": w, "idle_sec": p.IdleSeconds(), "locked": p.Locked(), "media": media, "music_payload": musicPayload(media),
		"autostart": autostartEnabled(), "paired": cfg.paired(), "server": cfg.Server, "device": cfg.Device}
	t1 := time.Now()
	_ = p.Stats(true) // 第一次只是打底（CPU 这类要两次采样才有数）
	time.Sleep(2 * time.Second)
	res["stats"] = p.Stats(true)
	res["stats_ms"] = time.Since(t1).Milliseconds() - 2000
	aw := newAW(cfg.AWURL)
	if info, err := aw.Info(); err == nil {
		res["aw_info"] = info
		res["aw_url_now"] = aw.LatestURL()
		app, title, ok := aw.LatestWindow()
		res["aw_window_now"] = M{"app": app, "title": title, "ok": ok}
		if raw, counts, err := aw.ExportDay(time.Now()); err == nil {
			res["aw_today_counts"], res["aw_today_kb"] = counts, len(raw)/1024
		} else {
			res["aw_err"] = err.Error()
		}
	} else {
		res["aw_err"] = err.Error()
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if out == "" {
		exe, _ := os.Executable()
		out = filepath.Join(filepath.Dir(exe), "hl-agent-dump.json")
	}
	if err := os.WriteFile(out, b, 0o600); err != nil {
		fmt.Println(string(b))
		fmt.Println("写不进", out, err)
		return 1
	}
	fmt.Println("已写入", out)
	return 0
}
