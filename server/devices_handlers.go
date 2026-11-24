package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/* ---------------- 设备端：认证 ---------------- */

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// deviceAuth：找到口令对应的设备，顺手记下最后在线时间
func deviceAuth(r *http.Request) (*Device, *ingestErr) {
	ip := clientIP(r)
	if devAuthFails.blocked(ip, 30, 10*time.Minute) {
		return nil, &ingestErr{429, "口令错误太多次，10 分钟后再试"}
	}
	tok := bearer(r)
	if !strings.HasPrefix(tok, "hld_") || len(tok) > 100 {
		devAuthFails.hit(ip, 1000, 10*time.Minute)
		return nil, &ingestErr{401, "缺少设备口令（Authorization: Bearer hld_…）"}
	}
	h := tokenHash(tok)
	devMu.Lock()
	defer devMu.Unlock()
	devLoadLocked()
	for _, d := range devList {
		if subtle.ConstantTimeCompare([]byte(d.TokenHash), []byte(h)) == 1 {
			if d.Revoked {
				return nil, &ingestErr{401, "这台设备已经被停用，请重新配对"}
			}
			d.LastSeen, d.LastIP = isoSec(time.Now().In(loc)), ip
			devDirty = true
			c := *d
			c.Scopes = append([]string(nil), d.Scopes...)
			return &c, nil
		}
	}
	devAuthFails.hit(ip, 1000, 10*time.Minute)
	return nil, &ingestErr{401, "设备口令不对（可能已被删除），请重新配对"}
}

func devicePublic(d *Device) M {
	return M{"id": d.ID, "name": d.Name, "kind": d.Kind, "host": d.Host, "scopes": d.Scopes}
}

func serverHints() M {
	return M{"name": or(conf().Site.Title, "hongtang-live"), "report_sec": 30, "aw_daily": true, "writer": conf().DaylogWriter,
		"timezone": tzOffset()}
}

/* ---------------- 设备端：统一上报 ---------------- */

type reportReq struct {
	TS       any `json:"ts"`
	Activity M   `json:"activity"`
	Music    M   `json:"music"`
	Health   M   `json:"health"`
	Battery  M   `json:"battery"`
	Stats    M   `json:"stats"`
}

func partErr(e *ingestErr) M { return M{"ok": false, "error": e.msg, "status": e.code} }

func deviceReport(d *Device, req *reportReq) M {
	res := M{}
	deny := func(part string) bool {
		if !hasScope(d, part) {
			res[part] = partErr(&ingestErr{403, "这台设备没有「" + part + "」权限"})
			return true
		}
		return false
	}
	// activity + music 走同一条（和旧的 live 上报一样：音乐挂在活动里）
	actOK := req.Activity != nil && !deny("activity")
	musOK := req.Music != nil && !deny("music")
	if actOK || musOK {
		p := M{}
		if actOK {
			for _, k := range []string{"app", "title", "url", "idle", "package"} {
				if v, has := req.Activity[k]; has {
					p[k] = v
				}
			}
		} else {
			p["idle"] = true // 只报了音乐：不算一段应用使用
		}
		if musOK {
			p["music"] = req.Music
		}
		if req.TS != nil {
			p["ts"] = normTS(req.TS) // 设备时钟不可信：格式不对 / 在未来都换成现在
		}
		r, e := ingestLive(p, d.Host, d.Name)
		var out M
		if e != nil {
			out = partErr(e)
		} else {
			out = r
		}
		if actOK {
			res["activity"] = out
		}
		if musOK {
			if actOK && e == nil {
				res["music"] = M{"ok": true}
			} else {
				res["music"] = out
			}
		}
	}
	if req.Health != nil && !deny("health") {
		p := M{"values": req.Health["values"], "ts": req.Health["ts"]}
		if p["ts"] == nil {
			p["ts"] = req.TS
		}
		r, e := ingestHealth(p)
		if e != nil {
			res["health"] = partErr(e)
		} else {
			res["health"] = r
		}
	}
	if req.Battery != nil && !deny("battery") {
		lv, ok := pyFloat(req.Battery["level"])
		if !ok || lv < 0 || lv > 100 {
			res["battery"] = partErr(&ingestErr{400, "level 要是 0~100"})
		} else {
			devMu.Lock()
			for _, x := range devList {
				if x.ID == d.ID {
					x.Battery = &Battery{Level: int(lv + 0.5), Charging: truthy(req.Battery["charging"]), At: isoSec(time.Now().In(loc))}
					devDirty = true
				}
			}
			devMu.Unlock()
			res["battery"] = M{"ok": true}
		}
	}
	if req.Stats != nil && !deny("stats") {
		raw := M{}
		for k, v := range req.Stats {
			raw[k] = v
		}
		if !truthy(raw["host"]) {
			raw["host"] = d.Name
		}
		slot := d.StatsID
		if slot == "" {
			slot = "pc-" + d.ID
		}
		host, at := pcReportTo(slot, raw)
		res["stats"] = M{"ok": true, "host": host, "at": at, "slot": slot}
	}
	return res
}

/* ---------------- 设备端：HTTP ---------------- */

func handleV1(w http.ResponseWriter, r *http.Request, sub string) {
	w.Header().Set("Cache-Control", "no-store")
	if sub == "pair" {
		if r.Method != "POST" {
			writeJSON(w, 405, M{"ok": false, "error": "method not allowed"})
			return
		}
		b, err := readBody(r, 8*1024)
		if err != nil {
			writeJSON(w, 413, M{"ok": false, "error": "body too large"})
			return
		}
		var req pairReq
		if json.Unmarshal(b, &req) != nil {
			writeJSON(w, 400, M{"ok": false, "error": "bad json"})
			return
		}
		d, tok, e := pairDevice(req, clientIP(r))
		if e != nil {
			writeIngest(w, nil, e)
			return
		}
		writeJSON(w, 200, M{"ok": true, "token": tok, "device": devicePublic(d), "server": serverHints()})
		return
	}
	d, e := deviceAuth(r)
	if e != nil {
		writeIngest(w, nil, e)
		return
	}
	switch {
	case sub == "device" && r.Method == "GET":
		writeJSON(w, 200, M{"ok": true, "device": devicePublic(d), "server": serverHints()})
	case sub == "report" && r.Method == "POST":
		b, err := readBody(r, 256*1024)
		if err != nil {
			writeJSON(w, 413, M{"ok": false, "error": "body too large"})
			return
		}
		var req reportReq
		if decodeJSON(b, &req) != nil {
			writeJSON(w, 400, M{"ok": false, "error": "bad json"})
			return
		}
		res := deviceReport(d, &req)
		if len(res) == 0 {
			writeJSON(w, 400, M{"ok": false, "error": "什么都没报（activity / music / health / battery / stats 至少要一样）"})
			return
		}
		writeJSON(w, 200, M{"ok": true, "results": res, "server": serverHints()})
	case sub == "aw" && r.Method == "POST":
		if !hasScope(d, "aw") {
			writeJSON(w, 403, M{"ok": false, "error": "这台设备没有「aw」权限"})
			return
		}
		b, e := readAWBody(w, r)
		if e != nil {
			writeIngest(w, nil, e)
			return
		}
		res, e := ingestAW(b, d.Name)
		writeIngest(w, res, e)
	case sub == "unpair" && r.Method == "POST":
		devMu.Lock()
		for i, x := range devList {
			if x.ID == d.ID {
				devList = append(devList[:i], devList[i+1:]...)
				break
			}
		}
		err := devSaveLocked()
		devMu.Unlock()
		if err != nil {
			writeJSON(w, 500, M{"ok": false, "error": err.Error()})
			return
		}
		log.Printf("[devices] %s 自己解除了配对", d.Name)
		writeJSON(w, 200, M{"ok": true})
	default:
		writeJSON(w, 404, M{"ok": false, "error": "not found"})
	}
}

/* ---------------- 管理员：/api/admin/devices/* ---------------- */

func deviceAdminView(d *Device) M {
	online := false
	if t, ok := pyAware(d.LastSeen); ok {
		online = time.Since(t) < 3*time.Minute
	}
	return M{"id": d.ID, "name": d.Name, "kind": d.Kind, "kind_label": deviceKinds[d.Kind], "host": d.Host, "scopes": d.Scopes, "stats_id": d.StatsID,
		"created": d.Created, "last_seen": d.LastSeen, "last_ip": d.LastIP, "app_version": d.AppVersion, "battery": d.Battery,
		"revoked": d.Revoked, "online": online && !d.Revoked}
}

func devicesAdminList() M {
	devMu.Lock()
	devLoadLocked()
	list := []M{}
	for _, d := range devList {
		list = append(list, deviceAdminView(d))
	}
	devMu.Unlock()
	pend := []M{}
	for _, p := range pendingCodes() {
		pend = append(pend, M{"code": p.Code, "name": p.Name, "kind": p.Kind, "expires": isoSec(p.Expires.In(loc))})
	}
	_, apkErr := os.Stat(filepath.Join(dataDir, "downloads", "hongtang-live.apk"))
	return M{"ok": true, "devices": list, "pending": pend, "kinds": deviceKinds, "scopes": allScopes, "apk_ready": apkErr == nil}
}

func findDeviceLocked(id string) *Device {
	devLoadLocked()
	for _, d := range devList {
		if d.ID == id {
			return d
		}
	}
	return nil
}

func handleAdminDevices(w http.ResponseWriter, r *http.Request, sub string) {
	if sub == "" && r.Method == "GET" {
		writeJSON(w, 200, devicesAdminList())
		return
	}
	var body struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Kind   string   `json:"kind"`
		Host   string   `json:"host"`
		Scopes []string `json:"scopes"`
		Code   string   `json:"code"`
		Base   string   `json:"base"`
	}
	if !adminPost(w, r, &body) {
		return
	}
	switch sub {
	case "pair":
		p, err := createPairCode(body.Name, body.Kind)
		if err != nil {
			writeJSON(w, 400, M{"ok": false, "error": err.Error()})
			return
		}
		base := strings.TrimRight(strings.TrimSpace(body.Base), "/")
		if base == "" {
			base = baseURLOf(r)
		} else if b, err := httpURL(base, "服务器地址"); err != nil {
			cancelPairCode(p.Code)
			writeJSON(w, 400, M{"ok": false, "error": err.Error()})
			return
		} else {
			base = b
		}
		link := pairLink(base, p.Code)
		rows, err := qrRows(link)
		if err != nil {
			rows = nil
		}
		writeJSON(w, 200, M{"ok": true, "code": p.Code, "expires": isoSec(p.Expires.In(loc)), "base": base, "link": link, "qr": rows})
		return
	case "cancel":
		writeJSON(w, 200, M{"ok": cancelPairCode(body.Code)})
		return
	}
	devMu.Lock()
	d := findDeviceLocked(body.ID)
	if d == nil {
		devMu.Unlock()
		writeJSON(w, 404, M{"ok": false, "error": "没有这台设备"})
		return
	}
	var err error
	switch sub {
	case "update":
		name := cleanName(body.Name, d.Name)
		host := cleanHost(body.Host)
		if host == "" {
			err = errors.New("daylog 里的名字（host）只能有字母、数字、. _ -")
			break
		}
		for _, x := range devList {
			if x.ID != d.ID && !x.Revoked && strings.EqualFold(x.Host, host) {
				err = fmt.Errorf("host「%s」已经被「%s」用了", host, x.Name)
			}
		}
		if err != nil {
			break
		}
		scopes := []string{}
		for _, s := range allScopes {
			for _, want := range body.Scopes {
				if want == s {
					scopes = append(scopes, s)
				}
			}
		}
		d.Name, d.Host, d.Scopes = name, host, scopes
	case "revoke":
		d.Revoked = true
	case "restore":
		d.Revoked = false
	case "delete":
		for i, x := range devList {
			if x.ID == d.ID {
				devList = append(devList[:i], devList[i+1:]...)
				break
			}
		}
	default:
		devMu.Unlock()
		writeJSON(w, 404, M{"ok": false, "error": "not found"})
		return
	}
	if err == nil {
		err = devSaveLocked()
	}
	devMu.Unlock()
	if err != nil {
		writeJSON(w, 400, M{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("[devices] %s：%s", sub, body.ID)
	writeJSON(w, 200, devicesAdminList())
}

/* 给「此刻」页：按 daylog host 找设备的类型和电量 */
func deviceByHost(host string) (kind string, bat *Battery) {
	devMu.Lock()
	defer devMu.Unlock()
	devLoadLocked()
	for _, d := range devList {
		if !d.Revoked && strings.EqualFold(d.Host, host) {
			if d.Battery != nil {
				if t, ok := pyAware(d.Battery.At); ok && time.Since(t) < 30*time.Minute {
					b := *d.Battery
					bat = &b
				}
			}
			return d.Kind, bat
		}
	}
	return "", nil
}

type slotOwner struct{ name, kind string }

// statsSlotOwners：槽位 → 配对的设备（停用的不算）
func statsSlotOwners() map[string]slotOwner {
	devMu.Lock()
	defer devMu.Unlock()
	devLoadLocked()
	out := map[string]slotOwner{}
	for _, d := range devList {
		if d.StatsID != "" && !d.Revoked && hasScope(d, "stats") {
			out[d.StatsID] = slotOwner{d.Name, d.Kind}
		}
	}
	return out
}

// deviceSeenByHost：配对设备最后一次带口令请求的时间（任何上报都算）
func deviceSeenByHost(host string) (time.Time, bool) {
	devMu.Lock()
	defer devMu.Unlock()
	devLoadLocked()
	for _, d := range devList {
		if !d.Revoked && strings.EqualFold(d.Host, host) {
			return pyAware(d.LastSeen)
		}
	}
	return time.Time{}, false
}
