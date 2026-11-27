package main

import "testing"

// 网页的 JS 拆在 web/js/ 下、样式拆在 web/css/ 下：这些文件必须放行，否则页面白屏或没有样式；
// 目录穿越和别的后缀不放行
func TestStaticOKServesSplitJS(t *testing.T) {
	for _, p := range []string{"/index.html", "/stats.js", "/js/00-core.js", "/js/02b-uptime.js", "/js/10-router-stats.js", "/js/11-boot.js",
		"/css/00-base.css", "/css/06-home-cards.css", "/css/07-uptime.css", "/pixel.css", "/now.css"} {
		if !staticOK.MatchString(p) {
			t.Errorf("%s 应该放行", p)
		}
	}
	for _, p := range []string{"/js/../data/config.json", "/js/a.txt", "/js/sub/a.js", "/js/.js", "/app.js", "/server/main.go",
		"/style.css", "/css/../data/config.json", "/css/a.txt", "/css/sub/a.css", "/css/.css"} {
		if staticOK.MatchString(p) {
			t.Errorf("%s 不应该放行", p)
		}
	}
}
