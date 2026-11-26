package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (l *limiter) reset() {
	l.mu.Lock()
	l.m = nil
	l.mu.Unlock()
}

func resetDevicesForTest() {
	devMu.Lock()
	devLoaded, devList, devDirty = false, nil, false
	pairCodes = map[string]*pairCode{}
	devMu.Unlock()
	pairFailMu.Lock()
	pairFails = 0
	pairFailMu.Unlock()
	devAuthFails.reset()
	pairLimiter.reset()
}

func setupDevices(t *testing.T) string {
	root := setupRoot(t) // daylog 可写、DaylogRoot = 临时目录、dataDir = 临时目录
	resetDevicesForTest()
	t.Cleanup(resetDevicesForTest)
	return root
}

// call：走完整的 route()，和线上一模一样
func call(t *testing.T, method, path, token string, body any, hdr map[string]string) (int, M) {
	t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		bb, _ := json.Marshal(b)
		rd = bytes.NewReader(bb)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "192.168.1.50:5555"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	route(rec, req)
	var out M
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func pairOK(t *testing.T, kind, host string) (string, M) {
	t.Helper()
	pc, err := createPairCode("", kind)
	if err != nil {
		t.Fatal(err)
	}
	code, out := call(t, "POST", "/api/v1/pair", "", M{"code": strings.ToLower(strings.ReplaceAll(pc.Code, "-", " ")), "name": "测试" + kind,
		"kind": kind, "host": host, "app_version": "test/1"}, nil)
	if code != 200 {
		t.Fatalf("配对失败 %d %v", code, out)
	}
	return toStr(out["token"]), asM(out["device"])
}

func TestPairFlow(t *testing.T) {
	setupDevices(t)
	tok, dev := pairOK(t, "windows", "Hongtang PC!")
	if !strings.HasPrefix(tok, "hld_") || len(tok) < 40 {
		t.Fatalf("口令格式: %q", tok)
	}
	eq(t, "host 清洗", dev["host"], "Hongtang-PC")
	eq(t, "默认权限", strings.Join(toStrs(dev["scopes"]), ","), "activity,music,stats,aw")

	b, _ := os.ReadFile(filepath.Join(dataDir, "devices.json"))
	if strings.Contains(string(b), tok) || !strings.Contains(string(b), tokenHash(tok)) {
		t.Error("devices.json 只能存哈希")
	}
	if st, _ := os.Stat(filepath.Join(dataDir, "devices.json")); st.Mode().Perm() != 0o600 {
		t.Errorf("devices.json 权限 %v", st.Mode().Perm())
	}

	code, out := call(t, "GET", "/api/v1/device", tok, nil, nil)
	eq(t, "whoami", code, 200)
	eq(t, "whoami 名字", asM(out["device"])["name"], "测试windows")

	// 同一个 host 再配一台：自动加后缀
	_, dev2 := pairOK(t, "windows", "Hongtang-PC")
	if dev2["host"] == "Hongtang-PC" {
		t.Error("host 撞了要加后缀")
	}
	// 配对码只能用一次
	pc, _ := createPairCode("", "")
	call(t, "POST", "/api/v1/pair", "", M{"code": pc.Code, "kind": "android"}, nil)
	code, _ = call(t, "POST", "/api/v1/pair", "", M{"code": pc.Code, "kind": "android"}, nil)
	eq(t, "用过的码", code, 403)
	// 过期
	pc, _ = createPairCode("", "")
	devMu.Lock()
	pairCodes[pc.Code].Expires = time.Now().Add(-time.Second)
	devMu.Unlock()
	code, _ = call(t, "POST", "/api/v1/pair", "", M{"code": pc.Code}, nil)
	eq(t, "过期的码", code, 403)
	// 管理员预先指定的名字和类型优先
	pc, _ = createPairCode("我的手机", "android")
	_, out = call(t, "POST", "/api/v1/pair", "", M{"code": pc.Code, "name": "xx", "kind": "windows"}, nil)
	eq(t, "预设名字", asM(out["device"])["name"], "我的手机")
	eq(t, "预设类型", asM(out["device"])["kind"], "android")
}

func toStrs(v any) []string {
	var out []string
	for _, x := range asA(v) {
		out = append(out, toStr(x))
	}
	return out
}

func TestPairBruteForce(t *testing.T) {
	setupDevices(t)
	pc, _ := createPairCode("", "")
	for i := 0; i < 10; i++ {
		call(t, "POST", "/api/v1/pair", "", M{"code": "AAAA-AAAA"}, nil)
	}
	code, _ := call(t, "POST", "/api/v1/pair", "", M{"code": pc.Code}, nil)
	eq(t, "同一 IP 错 10 次后连对的也拒绝", code, 429)
	if len(pendingCodes()) != 1 {
		t.Error("被限流时配对码不该被消耗")
	}
	// 全局 50 次失败：作废所有配对码
	pairLimiter.reset()
	for i := 0; i < 50; i++ {
		_, _, _ = pairDevice(pairReq{Code: "AAAA-AAAA"}, "10.0.0."+itoa(int64(i)))
	}
	eq(t, "撞码太多作废全部", len(pendingCodes()), 0)
	if _, err := createPairCode("", "toaster"); err == nil {
		t.Error("不认识的类型要拒绝")
	}
	for i := 0; i < 5; i++ {
		_, _ = createPairCode("", "")
	}
	if _, err := createPairCode("", ""); err == nil {
		t.Error("最多 5 个没用的配对码")
	}
}

func TestDeviceReport(t *testing.T) {
	root := setupDevices(t)
	tok, _ := pairOK(t, "windows", "studio")
	code, out := call(t, "POST", "/api/v1/report", tok, M{
		"activity": M{"app": "Code.exe", "title": "main.go - hongtang-live - Visual Studio Code"},
		"music":    M{"playing": true, "title": "歌A", "artist": "某人"},
		"stats":    M{"host": "", "cpu": M{"usage": 12.5}},
	}, nil)
	eq(t, "report 200", code, 200)
	res := asM(out["results"])
	if l := toStr(asM(res["activity"])["label"]); !strings.Contains(l, "VS Code") {
		t.Errorf("activity 写入: %v", res["activity"])
	}
	eq(t, "music 一起写", asM(res["music"])["ok"], true)
	eq(t, "stats", asM(res["stats"])["ok"], true)
	var host, mus string
	_ = daylogDB().QueryRow("SELECT host, music FROM devices WHERE host='studio'").Scan(&host, &mus)
	eq(t, "daylog 用设备的 host", host, "studio")
	if !strings.Contains(mus, "歌A") {
		t.Errorf("音乐: %s", mus)
	}
	var n int
	_ = daylogDB().QueryRow("SELECT COUNT(*) FROM live WHERE host='studio'").Scan(&n)
	eq(t, "live 一条", n, 1)

	// 没有权限的部分单独报错，其他照常
	code, out = call(t, "POST", "/api/v1/report", tok, M{"health": M{"values": M{"heart_rate_live": 70}}, "battery": M{"level": 50}}, nil)
	eq(t, "部分失败仍 200", code, 200)
	eq(t, "health 没权限", asM(asM(out["results"])["health"])["status"], 403.0)
	eq(t, "battery 没权限", asM(asM(out["results"])["battery"])["status"], 403.0)

	// 手机：健康、电量、只报音乐
	ptok, _ := pairOK(t, "android", "phone")
	code, out = call(t, "POST", "/api/v1/report", ptok, M{
		"health":  M{"values": M{"heart_rate_live": 72, "bad": "x"}},
		"battery": M{"level": 85.4, "charging": true},
		"music":   M{"playing": true, "title": "歌B", "artist": "别人"},
		"ts":      "2099-01-01T00:00:00+08:00",
	}, nil)
	res = asM(out["results"])
	eq(t, "health stored", asM(res["health"])["stored"], 1.0)
	eq(t, "battery", asM(res["battery"])["ok"], true)
	eq(t, "只报音乐不算活动", asM(res["music"])["idle"], true)
	if _, has := res["activity"]; has {
		t.Error("没报 activity 就不该有 activity 结果")
	}
	_ = daylogDB().QueryRow("SELECT COUNT(*) FROM live WHERE host='phone'").Scan(&n)
	eq(t, "手机只报音乐不进 live", n, 0)
	kind, bat := deviceByHost("phone")
	if kind != "android" || bat == nil || bat.Level != 85 || !bat.Charging {
		t.Errorf("电量: %s %+v", kind, bat)
	}
	// 来自未来的时间被纠正
	var ts string
	_ = daylogDB().QueryRow("SELECT last_seen FROM devices WHERE host='phone'").Scan(&ts)
	if strings.HasPrefix(ts, "2099") {
		t.Errorf("未来时间没纠正: %s", ts)
	}

	code, _ = call(t, "POST", "/api/v1/report", tok, M{}, nil)
	eq(t, "空上报 400", code, 400)

	// AW 上传（gzip）
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(`{"date": "2026-10-28", "hostname": "studio", "buckets": {"window": []}}`))
	_ = zw.Close()
	code, out = call(t, "POST", "/api/v1/aw", tok, buf.Bytes(), map[string]string{"Content-Encoding": "gzip"})
	eq(t, "aw 200", code, 200)
	if _, err := os.Stat(filepath.Join(root, "inbox", "2026-10-28.json")); err != nil {
		t.Error("aw 写进 inbox")
	}
	code, _ = call(t, "POST", "/api/v1/aw", ptok, []byte(`{"date": "2026-10-28"}`), nil)
	eq(t, "手机默认不能传 aw", code, 403)

	// 只读模式：daylog 写入关着时活动返回 503，但电量 / 系统资源照常
	c := cloneConfig(conf())
	c.DaylogWriter = false
	setConfig(c)
	_, out = call(t, "POST", "/api/v1/report", ptok, M{"activity": M{"app": "x"}, "battery": M{"level": 10}}, nil)
	res = asM(out["results"])
	eq(t, "只读时 activity 503", asM(res["activity"])["status"], 503.0)
	eq(t, "只读时电量照常", asM(res["battery"])["ok"], true)
}

func TestDeviceAuth(t *testing.T) {
	setupDevices(t)
	tok, dev := pairOK(t, "linux", "nas")
	code, _ := call(t, "GET", "/api/v1/device", "", nil, nil)
	eq(t, "没口令 401", code, 401)
	code, _ = call(t, "GET", "/api/v1/device", "hld_nope", nil, nil)
	eq(t, "错口令 401", code, 401)
	code, _ = call(t, "GET", "/api/v1/device", tok, nil, map[string]string{"Authorization": "bearer " + tok})
	eq(t, "bearer 小写也行", code, 200)

	// 停用 / 恢复 / 删除
	devMu.Lock()
	findDeviceLocked(toStr(dev["id"])).Revoked = true
	devMu.Unlock()
	code, out := call(t, "GET", "/api/v1/device", tok, nil, nil)
	eq(t, "停用后 401", code, 401)
	if !strings.Contains(toStr(out["error"]), "停用") {
		t.Error("要说明是被停用")
	}
	devMu.Lock()
	findDeviceLocked(toStr(dev["id"])).Revoked = false
	devMu.Unlock()
	code, _ = call(t, "POST", "/api/v1/unpair", tok, M{}, nil)
	eq(t, "自己解除配对", code, 200)
	code, _ = call(t, "GET", "/api/v1/device", tok, nil, nil)
	eq(t, "解除后 401", code, 401)

	// 错 30 次后同一 IP 被限流（连对的也不行）
	tok2, _ := pairOK(t, "linux", "nas2")
	for i := 0; i < 30; i++ {
		call(t, "GET", "/api/v1/device", "hld_wrong"+itoa(int64(i)), nil, nil)
	}
	code, _ = call(t, "GET", "/api/v1/device", tok2, nil, nil)
	eq(t, "限流", code, 429)

	// 重启后（重新读盘）口令仍然有效
	devAuthFails.reset()
	devMu.Lock()
	devLoaded = false
	devMu.Unlock()
	code, _ = call(t, "GET", "/api/v1/device", tok2, nil, nil)
	eq(t, "读盘后有效", code, 200)
}

func TestPairLinkAndQR(t *testing.T) {
	link := pairLink("https://example.com", "ABCD-EFGH")
	eq(t, "链接", link, "hlpair://pair?s=https%3A%2F%2Fexample.com&c=ABCD-EFGH")
	rows, err := qrRows(link)
	if err != nil || len(rows) < 21 || len(rows[0]) != len(rows) {
		t.Fatalf("二维码: %v %d", err, len(rows))
	}
	eq(t, "定位角", rows[0][:7], "1111111")
	eq(t, "配对码规整", normCode(" abcd efgh "), "ABCD-EFGH")
	eq(t, "长度不对", normCode("ABC"), "")
	eq(t, "host 清洗", cleanHost("  书房 的 PC!  "), "书房-的-PC")
	eq(t, "host 中文", cleanHost("测试电脑"), "测试电脑")
}

func TestNowDevicesBattery(t *testing.T) {
	setupDevices(t)
	ptok, _ := pairOK(t, "android", "phone")
	call(t, "POST", "/api/v1/report", ptok, M{"activity": M{"app": "com.bilibili.app"}, "battery": M{"level": 42}}, nil)
	code, out := call(t, "GET", "/api/now", "", nil, nil)
	eq(t, "now 200", code, 200)
	devs := asA(out["devices"])
	if len(devs) == 0 || asM(devs[0])["kind"] != "android" || asM(devs[0])["battery"] != 42.0 {
		t.Errorf("此刻页设备: %v", devs)
	}
}

func TestStatsSlots(t *testing.T) {
	setupDevices(t)
	initPC()
	c := cloneConfig(conf())
	c.Stats.ServerID, c.Stats.PCName = "10.0.0.1", "老 PC"
	setConfig(c)
	// 旧采集器写 pc
	pcReportTo("pc", M{"host": "old", "cpu": M{"usagePercent": 1.0}, "network": M{"publicIpv4": "1.2.3.4", "adapters": []any{}}})
	if n := asM(sectionData("pc", "network")); n == nil || n["publicIpv4"] != nil {
		t.Errorf("公网 IP 要去掉: %v", n)
	}
	w1, d1 := pairOK(t, "windows", "studio")
	l1, d2 := pairOK(t, "linux", "laptop")
	eq(t, "第一台电脑用 pc", d1["id"] != nil, true)
	devMu.Lock()
	s1, s2 := findDeviceLocked(toStr(d1["id"])).StatsID, findDeviceLocked(toStr(d2["id"])).StatsID
	devMu.Unlock()
	eq(t, "第一台 pc", s1, "pc")
	eq(t, "第二台 pc-<id>", s2, "pc-"+toStr(d2["id"]))
	_, out := call(t, "POST", "/api/v1/report", w1, M{"stats": M{"cpu": M{"usagePercent": 50.0}, "windows": M{"os": M{"name": "Win"}}}}, nil)
	eq(t, "电脑 stats 进 pc", asM(asM(out["results"])["stats"])["slot"], "pc")
	// 只发 cpu：windows 分区沿用上一次
	call(t, "POST", "/api/v1/report", w1, M{"stats": M{"cpu": M{"usagePercent": 60.0}}}, nil)
	if w := asM(sectionData("pc", "windows")); w == nil || asM(w["os"])["name"] != "Win" {
		t.Errorf("没带的分区要沿用: %v", w)
	}
	eq(t, "cpu 更新", toNum(asM(sectionData("pc", "cpu"))["usagePercent"]), 60.0)
	_, out = call(t, "POST", "/api/v1/report", l1, M{"stats": M{"cpu": M{"usagePercent": 7.0}}}, nil)
	eq(t, "Linux 电脑进自己的槽位", asM(asM(out["results"])["stats"])["slot"], s2)
	eq(t, "两台互不覆盖", toNum(asM(sectionData("pc", "cpu"))["usagePercent"]), 60.0)
	eq(t, "statsHost 认识新槽位", statsHost(s2), s2)
	eq(t, "statsHost 不认识的当 pc", statsHost("pc-deadbeef"), "pc")
	eq(t, "statsHost 服务器", statsHost("10.0.0.1"), "10.0.0.1")
	devs := statsDevices()
	names := []string{}
	for _, d := range devs {
		names = append(names, toStr(d["name"])+"/"+toStr(d["os"]))
	}
	eq(t, "设备菜单", strings.Join(names, ","), "测试windows/windows,测试linux/linux,"+conf().Stats.ServerName+"/linux")
	code, body := call(t, "GET", "/api/stats/cpu?host="+s2, "", nil, nil)
	eq(t, "统计接口按槽位取", code == 200 && toNum(asM(body["data"])["usagePercent"]) == 7.0, true)
	// 重启后两个槽位都读得回来
	pcPersist()
	initPC()
	eq(t, "读回 Linux 槽位", toNum(asM(sectionData(s2, "cpu"))["usagePercent"]), 7.0)
	idx := string(renderIndex([]byte("<!--HL_SITE-->")))
	if !strings.Contains(idx, "window.HL_STATS_DEVICES=") || !strings.Contains(idx, `"ip":"`+s2+`"`) {
		t.Errorf("首页要注入设备菜单: %s", idx)
	}
}

func TestLiveHostAndOldPaths(t *testing.T) {
	setupDevices(t)
	idx := string(renderIndexFor([]byte("<!--HL_SITE-->"), "live.example.com"))
	if !strings.Contains(idx, `window.HL_DEFAULT_ROUTE="now"`) {
		t.Errorf("live 域名默认进此刻: %s", idx)
	}
	if strings.Contains(string(renderIndexFor([]byte("<!--HL_SITE-->"), "example.com")), "HL_DEFAULT_ROUTE") {
		t.Error("主域名不改默认页")
	}
	code, _ := call(t, "GET", "/healthz", "", nil, nil)
	eq(t, "/healthz", code, 200)
}

func TestPhoneAndMultiDevice(t *testing.T) {
	setupDevices(t)
	pc, _ := pairOK(t, "windows", "studio")
	ph, _ := pairOK(t, "android", "phone")
	// 手机：标签 + 包名，包名只用来分类
	_, out := call(t, "POST", "/api/v1/report", ph, M{"activity": M{"app": "哔哩哔哩", "package": "tv.danmaku.bili"}}, nil)
	a := asM(asM(out["results"])["activity"])
	if a["cat"] != "娱乐" {
		t.Errorf("包名帮着分类: %v", a)
	}
	var title string
	_ = daylogDB().QueryRow("SELECT title FROM live WHERE host='phone'").Scan(&title)
	eq(t, "包名不保存", title, "")

	// 电脑在线时优先显示电脑（即使手机后来切过应用）
	call(t, "POST", "/api/v1/report", pc, M{"activity": M{"app": "Code.exe", "title": "x"}}, nil)
	time.Sleep(1100 * time.Millisecond)
	call(t, "POST", "/api/v1/report", ph, M{"activity": M{"app": "微信", "package": "com.tencent.mm"}}, nil)
	v := currentView()
	if !strings.Contains(toStr(v["now"]), "VS Code") && !strings.Contains(toStr(v["now"]), "Code") {
		t.Errorf("电脑在线时应优先显示电脑: %v", v["now"])
	}
	feed := getDaylogFeed(daylogDB())
	if fv := asM(feed["view"]); fv["stale"] != false || (!strings.Contains(toStr(fv["now"]), "VS Code") && !strings.Contains(toStr(fv["now"]), "Code")) {
		t.Errorf("首页 feed 电脑在线时也应优先显示电脑: %v", fv)
	}
	// 手机熄屏不会把在线的电脑标成离开
	call(t, "POST", "/api/v1/report", ph, M{"activity": M{"idle": true}}, nil)
	feed = getDaylogFeed(daylogDB())
	if fv := asM(feed["view"]); fv["stale"] != false {
		t.Errorf("手机离开不应影响电脑在线状态: %v", fv)
	}
	// 电脑离开、手机在线 → 显示手机
	call(t, "POST", "/api/v1/report", ph, M{"activity": M{"app": "微信", "package": "com.tencent.mm"}}, nil)
	call(t, "POST", "/api/v1/report", pc, M{"activity": M{"idle": true}}, nil)
	v = currentView()
	if !strings.Contains(toStr(v["now"]), "微信") {
		t.Errorf("电脑离开后显示手机: %v", v["now"])
	}
	// 两台都离开：和原来一样看最后一条（还没过 stale_sec，所以照样显示）
	call(t, "POST", "/api/v1/report", ph, M{"activity": M{"idle": true}}, nil)
	if v := currentView(); v["stale"] != false {
		t.Errorf("都离开但没过期：照原来的规矩: %v", v)
	}
}

func TestAPKDownload(t *testing.T) {
	setupDevices(t)
	code, _ := call(t, "GET", "/app/hongtang-live.apk", "", nil, nil)
	eq(t, "还没有安装包 404", code, 404)
	_ = os.MkdirAll(filepath.Join(dataDir, "downloads"), 0o755)
	_ = os.WriteFile(filepath.Join(dataDir, "downloads", "hongtang-live.apk"), []byte("PK\x03\x04test"), 0o644)
	req := httptest.NewRequest("GET", "/app/hongtang-live.apk", nil)
	rec := httptest.NewRecorder()
	route(rec, req)
	eq(t, "有安装包 200", rec.Code, 200)
	eq(t, "类型", rec.Header().Get("Content-Type"), "application/vnd.android.package-archive")
	eq(t, "内容", rec.Body.String(), "PK\x03\x04test")
}

func TestHRDeviceOnline(t *testing.T) {
	setupDevices(t)
	hr, _ := pairOK(t, "android", "hr-box")
	call(t, "POST", "/api/v1/report", hr, M{"activity": M{"idle": true}}, nil) // 刚装好时报过一次（daylog 里有这台）
	db := daylogDB()
	_, _ = db.Exec("UPDATE devices SET last_seen=? WHERE host='hr-box'", isoSec(time.Now().Add(-time.Hour)))
	call(t, "POST", "/api/v1/report", hr, M{"health": M{"values": M{"heart_rate_live": 80}}, "battery": M{"level": 33}}, nil)
	for _, d := range devicesView(4) {
		if d.Name == "测试android" || d.Kind == "android" {
			eq(t, "只报心率也算在线", d.Online, true)
			eq(t, "电量", *d.Battery, 33)
			return
		}
	}
	t.Error("设备栏里没有心率设备")
}

func TestMultiDeviceNoDoubleCount(t *testing.T) {
	setupDevices(t)
	_, _ = pairOK(t, "windows", "pc1")
	_, _ = pairOK(t, "android", "ph1")
	day := "2026-10-15"
	db := daylogDB()
	// 电脑 01:00–02:00（1h），手机 01:15–02:15（1h，与电脑重叠 45m）→ 合计应为 1h15m = 4500s
	_, _ = db.Exec("INSERT INTO live(day, ts, end_ts, host, app, label, label_plain, cat, title, dur) VALUES (?,?,?,?,?,?,?,?,?,?)",
		day, day+"T01:00:00+08:00", day+"T02:00:00+08:00", "pc1", "code", "VS Code", "VS Code", "写代码", "", 3600)
	_, _ = db.Exec("INSERT INTO live(day, ts, end_ts, host, app, label, label_plain, cat, title, dur) VALUES (?,?,?,?,?,?,?,?,?,?)",
		day, day+"T01:15:00+08:00", day+"T02:15:00+08:00", "ph1", "chrome", "Chrome", "Chrome", "浏览", "", 3600)
	tot := dayTotals(day)
	eq(t, "多设备重叠时段不重复计算", tot.Total, int64(4500))
	if len(tot.Cats) != 2 || tot.Cats[0]["name"] != "写代码" || tot.Cats[0]["sec"] != int64(3600) || tot.Cats[1]["sec"] != int64(900) {
		t.Errorf("分类应电脑优先、手机只计非重叠部分: %v", tot.Cats)
	}
}
