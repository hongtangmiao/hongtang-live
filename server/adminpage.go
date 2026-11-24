package main

import (
	"embed"
	"net/http"
	"regexp"
	"strings"
)

//go:embed adminui/admin.html adminui/js/*.js
var adminUI embed.FS

// adminPageOK：设置页能取的文件只有 admin.html 和 js/ 下的脚本（admin.html 按 00→03 顺序加载）
var adminPageOK = regexp.MustCompile(`^(?:admin\.html|js/[\w-]+\.js)$`)

func serveAdminPage(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != "GET" && r.Method != "HEAD" {
		sendText(w, 405, "method not allowed")
		return
	}
	if !adminPageOK.MatchString(name) {
		sendText(w, 404, "not found")
		return
	}
	b, err := adminUI.ReadFile("adminui/" + name)
	if err != nil {
		sendText(w, 404, "not found")
		return
	}
	ct := "text/html; charset=utf-8"
	if strings.HasSuffix(name, ".js") {
		ct = "text/javascript; charset=utf-8"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	// 设置页只信自己：脚本只能是同源的 /admin/js/*.js，不许被嵌进别的页面
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; "+
		"script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.WriteHeader(200)
	if r.Method == "GET" {
		_, _ = w.Write(b)
	}
}
