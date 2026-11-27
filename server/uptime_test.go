package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseUptimeTarget(t *testing.T) {
	cases := []struct {
		inURL, inSlug      string
		wantBase, wantSlug string
	}{
		{"http://uptime-kuma:3001/", "web", "http://uptime-kuma:3001", "web"},
		{"http://uptime-kuma:3001", "", "http://uptime-kuma:3001", "web"},
		{"https://state.7120721.xyz/status/web", "", "https://state.7120721.xyz", "web"},
		{"https://state.7120721.xyz/status/web/", "custom", "https://state.7120721.xyz", "custom"},
		{"https://state.7120721.xyz/status/web?foo=1#top", "", "https://state.7120721.xyz", "web"},
	}
	for _, tc := range cases {
		b, s := parseUptimeTarget(tc.inURL, tc.inSlug)
		if b != tc.wantBase || s != tc.wantSlug {
			t.Errorf("parseUptimeTarget(%q, %q) = (%q, %q), want (%q, %q)",
				tc.inURL, tc.inSlug, b, s, tc.wantBase, tc.wantSlug)
		}
	}
}

func TestUptimeFetchAndAggregate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status-page/web":
			_, _ = w.Write([]byte(`{
				"config":{"slug":"web","title":"Web State","description":"My services"},
				"incident":{"title":"Notice","content":"Checking DNS","style":"warning","createdDate":"2026-11-27 10:00:00"},
				"publicGroupList":[
					{"id":2,"name":"Media","weight":2,"monitorList":[{"id":10,"name":"Jellyfin"}]},
					{"id":1,"name":"Core","weight":1,"monitorList":[{"id":1,"name":"Blog"},{"id":2,"name":"API"}]}
				]
			}`))
		case "/api/status-page/heartbeat/web":
			_, _ = w.Write([]byte(`{
				"heartbeatList":{
					"1":[{"status":1,"time":"2026-11-27 11:00:00","msg":"200 - OK","ping":35}],
					"2":[{"status":1,"time":"2026-11-27 10:59:00","msg":"200","ping":20},{"status":0,"time":"2026-11-27 11:00:00","msg":"timeout","ping":0}],
					"10":[{"status":3,"time":"2026-11-27 11:00:00","msg":"maint","ping":12}]
				},
				"uptimeList":{"1_24":0.999,"2_24":0.85,"10_24":1}
			}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	oldCfg := conf()
	defer func() {
		setConfig(oldCfg)
		resetUptimeCache()
	}()
	c := &Config{}
	c.Uptime.URL = srv.URL
	c.Uptime.Slug = "web"
	c.Uptime.PublicURL = "https://state.7120721.xyz/status/web"
	setConfig(c)
	resetUptimeCache()

	d := getUptime(true)
	if ok, _ := d["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %v", d)
	}
	if d["title"] != "Web State" {
		t.Errorf("title = %v, want Web State", d["title"])
	}
	sum := asM(d["summary"])
	if int(toNum(sum["total"])) != 3 || int(toNum(sum["up"])) != 1 || int(toNum(sum["down"])) != 1 || int(toNum(sum["maintenance"])) != 1 {
		t.Errorf("unexpected summary: %v", sum)
	}
	if sum["overall"] != "degraded" {
		t.Errorf("overall = %v, want degraded", sum["overall"])
	}
	groups, ok := d["groups"].([]uptimeGroup)
	if !ok || len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %#v", d["groups"])
	}
	// weight=1 (Core) 应排在 weight=2 (Media) 前面
	if groups[0].Name != "Core" || groups[1].Name != "Media" {
		t.Errorf("groups not sorted by weight: %v, %v", groups[0].Name, groups[1].Name)
	}
	if len(groups[0].Monitors) != 2 || groups[0].Monitors[1].Status != 0 {
		t.Errorf("unexpected Core monitors: %+v", groups[0].Monitors)
	}
	if groups[0].Monitors[0].Uptime24 == nil || *groups[0].Monitors[0].Uptime24 != 0.999 {
		t.Errorf("unexpected uptime24: %+v", groups[0].Monitors[0].Uptime24)
	}
}

func TestUptimeUnconfigured(t *testing.T) {
	oldCfg := conf()
	defer func() {
		setConfig(oldCfg)
		resetUptimeCache()
	}()
	setConfig(&Config{})
	resetUptimeCache()

	d := getUptime(false)
	if ok, _ := d["ok"].(bool); ok {
		t.Fatalf("expected ok=false when unconfigured")
	}
	if configured, _ := d["configured"].(bool); configured {
		t.Fatalf("expected configured=false")
	}
}

func TestUptimeErrorSanitizedAndBackoff(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(502)
	}))
	defer srv.Close()

	oldCfg := conf()
	defer func() {
		setConfig(oldCfg)
		resetUptimeCache()
	}()
	c := &Config{}
	c.Uptime.URL = srv.URL
	c.Uptime.Slug = "web"
	setConfig(c)
	resetUptimeCache()

	d := getUptime(false)
	if ok, _ := d["ok"].(bool); ok {
		t.Fatalf("expected ok=false on upstream 502")
	}
	if got := toStr(d["error"]); got != "upstream unavailable" {
		t.Errorf("public error = %q, want %q (must not leak internal URL)", got, "upstream unavailable")
	}
	firstHits := hits
	// 退避期内再次请求不应穿透上游
	_ = getUptime(false)
	if hits != firstHits {
		t.Errorf("expected failure backoff to prevent upstream hit, got %d -> %d", firstHits, hits)
	}
	// 管理员状态仍保留详细错误便于排查
	if stErr := toStr(uptimeStatus()["error"]); stErr == "" {
		t.Errorf("expected detailed error in admin uptimeStatus()")
	}
}
