package main

import (
	"bytes"
	"encoding/json"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 只发前端需要的文件：配置、数据、源码一律 404
var staticOK = regexp.MustCompile(`^/(?:index\.html|css/[\w-]+\.css|pixel\.css|now\.css|stats\.js|js/[\w-]+\.js|(?:icons|img|fonts)/[\w./-]+)$`)

var mimeTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "/" || p == "" {
		p = "/index.html"
	}
	if !staticOK.MatchString(p) || strings.Contains(p, "..") {
		sendText(w, 404, "not found")
		return
	}
	file := filepath.Join(webDir, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	ext := strings.ToLower(filepath.Ext(file))
	cache := "public, max-age=3600"
	switch ext {
	case ".html", ".js", ".css":
		cache = "no-cache" // 每次都问一下，改了就能马上生效
	case ".woff2":
		cache = "public, max-age=2592000, immutable" // 字体 700KB，一个月不变
	}
	if p == "/img/avatar.png" {
		if up, uext := customAvatar(); up != "" { // 设置页上传的头像优先
			file, ext, cache = up, uext, "no-cache"
		}
	}
	st, err := os.Stat(file)
	if err != nil || st.IsDir() {
		sendText(w, 404, "not found")
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		sendText(w, 404, "not found")
		return
	}
	mod := st.ModTime()
	if p == "/index.html" {
		b = renderIndexFor(b, r.Host)
		mod = time.Time{} // 内容跟着配置变，不给 Last-Modified
	}
	ct := mimeTypes[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", cache)
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", mod, bytes.NewReader(b)) // 处理 HEAD / 304 / Range
}

// renderIndexFor：原来 daylog 的域名（live.xxx）打开首页时直接进「此刻」页
func renderIndexFor(b []byte, host string) []byte {
	out := renderIndex(b)
	h := strings.ToLower(host)
	if strings.HasPrefix(h, "live.") {
		out = bytes.Replace(out, []byte("<script>window.HL_SITE="), []byte(`<script>window.HL_DEFAULT_ROUTE="now";window.HL_SITE=`), 1)
	}
	return out
}

// renderIndex：把站点名字、资料塞进 index.html，前端不用再写死
func renderIndex(b []byte) []byte {
	si := siteInfo()
	title, _ := si["title"].(string)
	name, _ := si["name"].(string)
	domain, _ := si["domain"].(string)
	desc, _ := si["description"].(string)
	if title == "" {
		title = name
	}
	full := title
	if domain != "" && title != "" {
		full = title + " | " + domain
	} else if domain != "" {
		full = domain
	}
	js, _ := json.Marshal(si) // 默认会把 < > & 转义成 \u003c，放进 <script> 是安全的
	rep := strings.NewReplacer(
		"{{HL_FULL_TITLE}}", html.EscapeString(full),
		"{{HL_TITLE}}", html.EscapeString(title),
		"{{HL_NAME}}", html.EscapeString(name),
		"{{HL_DOMAIN}}", html.EscapeString(domain),
		"{{HL_DESC}}", html.EscapeString(desc),
		"<!--HL_SITE-->", "<script>window.HL_SITE="+string(js)+";window.HL_STATS_DEVICES="+string(statsDevicesJS())+";</script>",
	)
	return []byte(rep.Replace(string(b)))
}

// statsDevicesJS：统计页的设备菜单（stats.js 原来写死了两台）
func statsDevicesJS() []byte {
	list := []M{}
	for _, d := range statsDevices() {
		list = append(list, M{"ip": d["ip"], "name": d["name"], "os": d["os"]})
	}
	b, _ := json.Marshal(list)
	return b
}

// serveAPK：/app/hongtang-live.apk —— 安卓 App 的安装包（放在 data/downloads/，网址固定，手机上直接点就能装）
func serveAPK(w http.ResponseWriter, r *http.Request) {
	file := filepath.Join(dataDir, "downloads", "hongtang-live.apk")
	f, err := os.Open(file)
	if err != nil {
		sendText(w, 404, "还没有上传安装包")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		sendText(w, 404, "not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/vnd.android.package-archive")
	h.Set("Content-Disposition", `attachment; filename="hongtang-live.apk"`)
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "hongtang-live.apk", st.ModTime(), f)
}
