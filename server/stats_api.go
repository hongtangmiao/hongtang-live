package main

import (
	"net/http"
	"strconv"
	"time"
)

/* ---- text 生成（本家のサーバ側整形と同じ書式） ---- */

func cpuText(c M) M {
	cache := asM(c["cache"])
	kb := func(k string) string {
		if cache != nil && truthy(cache[k]) {
			return fmtMBfromKB(toNum(cache[k]))
		}
		return "—"
	}
	handles := "—"
	if c["handles"] != nil {
		handles = fmtInt(toNum(c["handles"]))
	}
	sockets := "1"
	if c["sockets"] != nil {
		sockets = toStr(c["sockets"])
	}
	return M{
		"usagePercent": fmtPct(toNum(c["usagePercent"])), "speed": fmtGHz(toNum(c["speedGHz"])), "baseSpeed": fmtGHz(toNum(c["baseSpeedGHz"])),
		"processes": jsNum(jsRound(toNum(c["processes"]))), "threads": fmtInt(toNum(c["threads"])), "handles": handles,
		"uptime": fmtUptime(toNum(c["uptimeMs"])), "sockets": sockets,
		"l1Cache": kb("l1KB"), "l2Cache": kb("l2KB"), "l3Cache": kb("l3KB"),
	}
}

func memText(m M) M {
	pf := asM(m["pageFile"])
	pfu, pft := 0.0, 0.0
	if pf != nil {
		pfu, pft = toNum(pf["used"]), toNum(pf["total"])
	}
	inst := fixed(toNum(m["installed"])/1073741824, 1) + " GB"
	if truthy(m["type"]) {
		inst += " " + toStr(m["type"])
	}
	speed := "—"
	if m["speed"] != nil {
		speed = toStr(m["speed"])
	}
	ff := "—"
	if truthy(m["formFactor"]) {
		ff = toStr(m["formFactor"])
	}
	return M{
		"installed": inst, "used": fmtGB(toNum(m["used"])), "usagePercent": fmtPct(toNum(m["usagePercent"])),
		"compressed": fmtGBShort(toNum(m["compressed"])), "available": fmtGB(toNum(m["available"])),
		"committed": fmtGBShort(toNum(m["committed"])) + "/" + fmtGBShort(toNum(m["commitLimit"])),
		"pageFile":  fmtGBShort(pfu) + "/" + fmtGBShort(pft),
		"cached":    fmtGB(toNum(m["cached"])), "pagedPool": fmtGBShort(toNum(m["pagedPool"])), "nonPagedPool": fmtGBShort(toNum(m["nonPagedPool"])),
		"speed": speed, "slots": jsNum(toNum(m["slotsUsed"])) + "/" + jsNum(toNum(m["slotsTotal"])),
		"formFactor": ff, "hardwareReserved": fmtGBShort(toNum(m["hardwareReserved"])),
	}
}

func clone(m M) M {
	out := make(M, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func zeroList(v any, key string) []any {
	out := []any{}
	for _, x := range asA(v) {
		if xm := asM(x); xm != nil {
			c := clone(xm)
			c[key] = 0
			out = append(out, c)
		} else {
			out = append(out, x)
		}
	}
	return out
}

func hostAlive(host string) bool {
	if host == conf().Stats.ServerID {
		return true
	}
	pc := pcSlotIf(host)
	if pc == nil {
		return false
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.snap != nil && time.Now().UnixMilli()-pc.at < pcStaleMs
}

// statsHost：网址里的 host= → 槽位 id / 服务器 id，不认识的当成 pc
func statsHost(q string) string {
	if q == conf().Stats.ServerID {
		return q
	}
	if reSlotID.MatchString(q) && pcSlotIf(q) != nil {
		return q
	}
	return "pc"
}

/* ---- 各分区の data 組み立て ---- */
func sectionData(host, sec string) any {
	isPC := host != conf().Stats.ServerID
	var snap M
	if isPC {
		if pc := pcSlotIf(host); pc != nil {
			pc.mu.Lock()
			snap = pc.snap
			pc.mu.Unlock()
		}
	}
	off := isPC && !hostAlive(host)
	switch sec {
	case "cpu":
		var raw M
		if isPC {
			raw = asM(snap["cpu"])
		} else {
			srv.mu.Lock()
			raw = srv.cpu
			srv.mu.Unlock()
		}
		if raw == nil {
			return nil
		}
		c := clone(raw)
		if off {
			for _, k := range []string{"usagePercent", "speedGHz", "performancePercent", "processes", "threads", "handles", "uptimeMs"} {
				c[k] = 0
			}
			c["logicalProcessorUsage"] = zeroList(raw["logicalProcessorUsage"], "usagePercent")
		}
		c["text"] = cpuText(c)
		return c
	case "memory":
		var raw M
		if isPC {
			raw = asM(snap["memory"])
		} else {
			srv.mu.Lock()
			raw = srv.memory
			srv.mu.Unlock()
		}
		if raw == nil {
			return nil
		}
		m := clone(raw)
		if off {
			total := raw["total"]
			if !truthy(total) {
				total = 0
			}
			pft := any(0)
			if pf := asM(raw["pageFile"]); pf != nil && truthy(pf["total"]) {
				pft = pf["total"]
			}
			for _, k := range []string{"used", "usagePercent", "compressed", "committed", "cached", "standby", "modified", "pagedPool", "nonPagedPool"} {
				m[k] = 0
			}
			m["available"], m["free"] = total, total
			m["pageFile"] = M{"used": 0, "total": pft}
		}
		m["text"] = memText(m)
		return m
	case "gpu":
		if !isPC {
			return nil // サーバー側は GPU 無し
		}
		raw := asM(snap["gpu"])
		if raw == nil {
			return nil
		}
		if !off {
			return raw
		}
		g := clone(raw)
		g["usagePercent"], g["temperatureC"] = 0, nil
		if gm := asM(raw["memory"]); gm != nil {
			mm := clone(gm)
			for _, k := range []string{"dedicatedUsed", "sharedUsed", "totalUsed", "usagePercent", "dedicatedPercent"} {
				mm[k] = 0
			}
			g["memory"] = mm
		}
		g["engines"] = zeroList(raw["engines"], "usagePercent")
		return g
	case "disk":
		if isPC {
			return orNil(snap["disk"])
		}
		srv.mu.Lock()
		d := srvDisk()
		srv.mu.Unlock()
		return M{"timestamp": time.Now().UnixMilli(), "hostname": srvHostname(), "systemDrive": "/", "disks": d}
	case "network":
		if !isPC {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			if srv.network == nil {
				return nil
			}
			return srv.network
		}
		raw := asM(snap["network"])
		if raw == nil {
			return nil
		}
		if !off {
			return raw
		}
		n := clone(raw)
		ads := []any{}
		for _, a := range asA(raw["adapters"]) {
			am := asM(a)
			if am == nil {
				continue
			}
			c := clone(am)
			c["recvBytesPerSec"], c["sentBytesPerSec"] = 0, 0
			if w := asM(am["wifi"]); w != nil {
				wc := clone(w)
				wc["signalPercent"] = 0
				c["wifi"] = wc
			} else {
				c["wifi"] = nil
			}
			ads = append(ads, c)
		}
		n["adapters"] = ads
		return n
	case "windows":
		if isPC {
			return orNil(snap["windows"])
		}
		return srvWindows()
	}
	return nil
}

func sectionRing(host, sec string) []M {
	if host != conf().Stats.ServerID {
		pc := pcSlotIf(host)
		if pc == nil {
			return nil
		}
		pc.mu.Lock()
		defer pc.mu.Unlock()
		switch sec {
		case "memory", "gpu", "network":
			return append([]M(nil), pc.rings[sec]...)
		}
		return append([]M(nil), pc.rings["cpu"]...)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	switch sec {
	case "memory", "network":
		return append([]M(nil), srv.rings[sec]...)
	}
	return append([]M(nil), srv.rings["cpu"]...)
}

// statsDevices：统计页的设备菜单。电脑槽位按配对的设备起名，没有配对设备的「pc」用设置页里的 PC 显示名
func statsDevices() []M {
	c := conf()
	owners := statsSlotOwners()
	out := []M{}
	for _, id := range pcSlotIDs() {
		o, owned := owners[id]
		if id != "pc" && !owned {
			continue // 设备被删了，槽位不再显示
		}
		name, osName := c.Stats.PCName, "windows"
		if owned {
			name = o.name
			if o.kind == "linux" {
				osName = "linux"
			}
		} else if pc := pcSlotIf(id); pc != nil {
			pc.mu.Lock()
			hasData := pc.snap != nil
			pc.mu.Unlock()
			if !hasData && len(owners) > 0 {
				continue // 旧的 pc 槽位从来没收到过数据、又已经有配对的电脑：不显示空的
			}
		}
		out = append(out, M{"ip": id, "name": name, "os": osName, "online": hostAlive(id)})
	}
	out = append(out, M{"ip": c.Stats.ServerID, "name": c.Stats.ServerName, "os": "linux", "online": true})
	return out
}

func handleStatsDevices(w http.ResponseWriter) {
	writeJSON(w, 200, M{"success": true, "timestamp": time.Now().UnixMilli(), "data": statsDevices()})
}

var statSections = map[string]bool{"cpu": true, "memory": true, "gpu": true, "disk": true, "network": true, "windows": true}
var histSections = map[string]bool{"cpu": true, "memory": true, "gpu": true, "network": true}

func handleStatsSection(w http.ResponseWriter, r *http.Request, sec string) {
	q := r.URL.Query()
	host := statsHost(q.Get("host"))
	if !statSections[sec] {
		writeJSON(w, 404, M{"success": false, "error": "unknown section"})
		return
	}
	data := sectionData(host, sec)
	if data == nil {
		writeJSON(w, 200, M{"success": false, "error": "no data"})
		return
	}
	if dm, ok := data.(M); ok && dm == nil {
		writeJSON(w, 200, M{"success": false, "error": "no data"})
		return
	}
	out := M{"success": true, "data": data}
	want, _ := strconv.Atoi(q.Get("history"))
	if want > 0 && histSections[sec] {
		ring := sectionRing(host, sec)
		if want < len(ring) {
			ring = ring[len(ring)-want:]
		}
		if host != conf().Stats.ServerID && !hostAlive(host) {
			z := make([]M, 0, len(ring))
			for _, s := range ring {
				d := asM(s["data"])
				var nd M
				switch sec {
				case "cpu":
					nd = M{"usagePercent": 0, "logicalProcessorUsage": zeroList(d["logicalProcessorUsage"], "usagePercent")}
				case "memory":
					nd = M{"used": 0}
				case "gpu":
					nd = M{"memory": M{"dedicatedUsed": 0, "sharedUsed": 0}, "engines": zeroList(d["engines"], "usagePercent")}
				case "network":
					nd = clone(d)
					nd["adapters"] = zeroList(zeroList(d["adapters"], "recvBytesPerSec"), "sentBytesPerSec")
				default:
					nd = d
				}
				c := clone(s)
				c["data"] = nd
				z = append(z, c)
			}
			out["samples"] = z
		} else {
			out["samples"] = ring
		}
	}
	if pc := pcSlotIf(host); host != conf().Stats.ServerID && pc != nil {
		pc.mu.Lock()
		out["sampledAt"] = pc.at / 1000
		out["stale"] = time.Now().UnixMilli()-pc.at > pcStaleMs
		pc.mu.Unlock()
	} else if host != conf().Stats.ServerID {
		out["sampledAt"], out["stale"] = 0, true
	} else {
		srv.mu.Lock()
		out["sampledAt"] = srv.at / 1000
		srv.mu.Unlock()
		out["stale"] = false
	}
	writeJSON(w, 200, out)
}
