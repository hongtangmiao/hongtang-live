package main

import (
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var version = "dev"

type M = map[string]any

/* ---------------- 日志：按天一个文件，留 7 天 ---------------- */

var (
	logMu   sync.Mutex
	logFile *os.File
	logDay  string
	logTail []string // 给状态页看最近的
)

func logDir() string { return filepath.Join(dataDirFn(), "logs") }

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	now := time.Now()
	line := now.Format("15:04:05") + " " + msg
	logMu.Lock()
	defer logMu.Unlock()
	logTail = append(logTail, now.Format("01-02 ")+line)
	if len(logTail) > 200 {
		logTail = logTail[len(logTail)-200:]
	}
	day := now.Format("20060102")
	if logFile == nil || day != logDay {
		if logFile != nil {
			logFile.Close()
		}
		_ = os.MkdirAll(logDir(), 0o700)
		f, err := os.OpenFile(filepath.Join(logDir(), "agent-"+day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			logFile, logDay = f, day
			pruneLogs()
		}
	}
	if logFile != nil {
		_, _ = io.WriteString(logFile, line+"\n")
	}
	if consoleOK {
		log.Print(msg)
	}
}

var consoleOK = true

func pruneLogs() {
	files, _ := filepath.Glob(filepath.Join(logDir(), "agent-*.log"))
	sort.Strings(files)
	for len(files) > 7 {
		_ = os.Remove(files[0])
		files = files[1:]
	}
}

func recentLog() []string {
	logMu.Lock()
	defer logMu.Unlock()
	return append([]string(nil), logTail...)
}

func parseQuery(q string) (map[string]string, error) {
	v, err := url.ParseQuery(q)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k := range v {
		out[k] = v.Get(k)
	}
	return out, nil
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func hostnameClean() string {
	h, _ := os.Hostname()
	h = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, h)
	return strings.Trim(h, "-.")
}

func safeGo(name string, f func()) {
	go func() {
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logf("%s 出错了，10 秒后重启：%v", name, r)
					}
				}()
				f()
			}()
			time.Sleep(10 * time.Second)
		}
	}()
}
