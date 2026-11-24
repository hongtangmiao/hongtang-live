package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// 设置页的脚本拆在 adminui/js/ 下：admin.html 引用的每个脚本都必须能取到，而且带着设置页的安全头；
// 别的路径（旧的 admin.js、目录穿越、别的后缀）一律 404
func TestAdminPageServesSplitJS(t *testing.T) {
	get := func(p string) (int, string, string) {
		rec := httptest.NewRecorder()
		route(rec, httptest.NewRequest("GET", p, nil))
		return rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Security-Policy")
	}
	html, _ := adminUI.ReadFile("adminui/admin.html")
	var scripts []string
	for _, l := range strings.Split(string(html), "\n") {
		if i := strings.Index(l, `<script src="`); i >= 0 {
			s := l[i+len(`<script src="`):]
			scripts = append(scripts, s[:strings.Index(s, `"`)])
		}
	}
	if len(scripts) != 4 {
		t.Fatalf("admin.html 应该按顺序引用 4 个脚本，实际 %d 个：%v", len(scripts), scripts)
	}
	for _, p := range append([]string{"/admin"}, scripts...) {
		code, ct, csp := get(p)
		if code != 200 {
			t.Errorf("%s 返回 %d，应该是 200", p, code)
		}
		if p != "/admin" && !strings.HasPrefix(ct, "text/javascript") {
			t.Errorf("%s 的 Content-Type 是 %q，应该是 text/javascript", p, ct)
		}
		if !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s 少了设置页的 CSP 头", p)
		}
	}
	for _, p := range []string{"/admin/admin.js", "/admin/js/../admin.html", "/admin/js/a.txt", "/admin/js/sub/a.js", "/admin/js/.js", "/admin/js/"} {
		if code, _, _ := get(p); code != 404 {
			t.Errorf("%s 返回 %d，应该是 404", p, code)
		}
	}
}
