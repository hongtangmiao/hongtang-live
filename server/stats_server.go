package main

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

/* ---- Linux（本机 2 秒采样） ---- */

type cpuSample struct {
	total, idle float64
	per         [][2]float64 // idle, total
	usage       []M
}

type srvState struct {
	mu      sync.Mutex
	at      int64
	cpu     M
	memory  M
	network M
	rings   map[string][]M
	cpuPrev *cpuSample
	netPrev *struct {
		t float64
		m map[string][2]float64
	}
	disksAt  time.Time
	disks    []M
	osName   string
	hostname string
	osLoaded bool
}

var srv = &srvState{rings: map[string][]M{"cpu": {}, "memory": {}, "network": {}}}

func hp(p string) string { return filepath.Join(conf().Stats.HostProc, p) }

func fields(s string) []float64 {
	var out []float64
	for _, f := range strings.Fields(s) {
		v, _ := strconv.ParseFloat(f, 64)
		out = append(out, v)
	}
	return out
}

var reCPULine = regexp.MustCompile(`^cpu(\d+)\s+(.*)$`)

func srvCPUSample() *cpuSample {
	stat := readFile(hp("stat"))
	if stat == "" {
		return nil
	}
	lines := strings.Split(stat, "\n")
	tot := fields(lines[0])
	if len(tot) > 0 {
		tot = tot[1:]
	}
	// 第一列是 "cpu" 本身，fields() 解析成 0 后已经去掉
	sum := 0.0
	for _, v := range tot {
		sum += v
	}
	idle := 0.0
	if len(tot) > 3 {
		idle = tot[3]
	}
	if len(tot) > 4 {
		idle += tot[4]
	}
	cs := &cpuSample{total: sum, idle: idle}
	idx := 0
	for _, l := range lines[1:] {
		m := reCPULine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		p := fields(m[2])
		pt := 0.0
		for _, v := range p {
			pt += v
		}
		pi := 0.0
		if len(p) > 3 {
			pi = p[3]
		}
		if len(p) > 4 {
			pi += p[4]
		}
		cs.per = append(cs.per, [2]float64{pi, pt})
		cs.usage = append(cs.usage, M{"id": "0," + strconv.Itoa(idx), "usagePercent": 0.0})
		idx++
	}
	return cs
}

func clamp100(x float64) float64 { return math.Max(0, math.Min(100, x)) }

var (
	reMHz     = regexp.MustCompile(`(?m)^cpu MHz\s*:\s*([\d.]+)`)
	reModel   = regexp.MustCompile(`(?m)^model name\s*:\s*(.+)$`)
	reNetLine = regexp.MustCompile(`^\s*([^:]+):\s*(\d+)\s+(?:\d+\s+){7}(\d+)\s`)
	reVirtual = regexp.MustCompile(`^(lo|veth|br-|docker|virbr|tun|tap|wg-|docker0)`)
	rePretty  = regexp.MustCompile(`(?m)^PRETTY_NAME="?([^"\n]+)"?`)
)

func srvHostname() string {
	if srv.osLoaded {
		return srv.hostname
	}
	h := strings.TrimSpace(readFile(filepath.Join(conf().Stats.HostRoot, "etc/hostname")))
	if h == "" {
		h = "server"
	}
	return h
}

func srvOsName() string {
	if !srv.osLoaded {
		rel := readFile(filepath.Join(conf().Stats.HostRoot, "etc/os-release"))
		pretty := "Linux"
		if m := rePretty.FindStringSubmatch(rel); m != nil {
			pretty = m[1]
		}
		srv.hostname = srvHostname()
		srv.osName = pretty
		srv.osLoaded = true
	}
	return srv.osName
}

func srvSample() {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	now := time.Now().UnixMilli()
	sampled := now / 1000

	cur := srvCPUSample()
	if cur != nil && srv.cpuPrev != nil {
		prev := srv.cpuPrev
		dt, di := cur.total-prev.total, cur.idle-prev.idle
		u := 0.0
		if dt > 0 {
			u = clamp100((1 - di/dt) * 100)
		}
		for i := range cur.usage {
			if i >= len(cur.per) || i >= len(prev.per) {
				continue
			}
			t2, i2 := cur.per[i][1]-prev.per[i][1], cur.per[i][0]-prev.per[i][0]
			v := 0.0
			if t2 > 0 {
				v = jsRound(clamp100((1-i2/t2)*100)*10) / 10
			}
			cur.usage[i]["usagePercent"] = v
		}
		cpuinfo := readFile(hp("cpuinfo"))
		var mhz []float64
		for _, m := range reMHz.FindAllStringSubmatch(cpuinfo, -1) {
			v, _ := strconv.ParseFloat(m[1], 64)
			mhz = append(mhz, v)
		}
		avg := 0.0
		if len(mhz) > 0 {
			for _, v := range mhz {
				avg += v
			}
			avg /= float64(len(mhz))
		}
		up := 0.0
		if f := strings.Fields(readFile(hp("uptime"))); len(f) > 0 {
			up, _ = strconv.ParseFloat(f[0], 64)
		}
		threads := 0.0
		if f := strings.Fields(readFile(hp("loadavg"))); len(f) > 3 {
			if p := strings.Split(f[3], "/"); len(p) > 1 {
				threads, _ = strconv.ParseFloat(p[1], 64)
			}
		}
		name := "CPU"
		if m := reModel.FindStringSubmatch(cpuinfo); m != nil {
			name = m[1]
		}
		usage := make([]any, len(cur.usage))
		for i, x := range cur.usage {
			usage[i] = x
		}
		ghz := jsRound(avg) / 1000
		srv.cpu = M{
			"timestamp": now, "hostname": srvHostname(), "osName": srvOsName(), "osVersion": "Linux", "osBuild": "",
			"name": name, "usagePercent": jsRound(u*10) / 10, "speedGHz": ghz, "performancePercent": nil,
			"processes": srvCountProcesses(), "threads": threads, "handles": nil,
			"uptimeMs": up * 1000, "bootTime": float64(now) - up*1000, "baseSpeedGHz": ghz,
			"sockets": 1, "cores": len(cur.usage), "logicalProcessors": len(cur.usage), "virtualization": false,
			"logicalProcessorUsage": usage, "cache": M{},
		}
		pushRing(srv.rings, "cpu", M{"data": M{"usagePercent": srv.cpu["usagePercent"], "logicalProcessorUsage": usage}, "sampledAt": sampled})
	}
	srv.cpuPrev = cur

	/* メモリ */
	if mi := readFile(hp("meminfo")); mi != "" {
		get := func(k string) float64 {
			m := regexp.MustCompile(`(?m)^` + k + `:\s+(\d+)`).FindStringSubmatch(mi)
			if m == nil {
				return 0
			}
			v, _ := strconv.ParseFloat(m[1], 64)
			return v * 1024
		}
		total := get("MemTotal")
		avail := get("MemAvailable")
		if avail == 0 {
			avail = get("MemFree")
		}
		swapT, swapF := get("SwapTotal"), get("SwapFree")
		pct := 0.0
		if total > 0 {
			pct = (total - avail) / total * 100
		}
		srv.memory = M{
			"timestamp": now, "hostname": srvHostname(), "installed": total, "total": total,
			"used": maxF(0, total-avail), "available": avail, "usagePercent": pct,
			"compressed": 0, "hardwareReserved": 0, "committed": get("Committed_AS"), "commitLimit": get("CommitLimit"),
			"pageFile": M{"used": maxF(0, swapT-swapF), "total": swapT},
			"cached":   get("Cached"), "standby": 0, "modified": get("Dirty"), "free": get("MemFree"),
			"pagedPool": 0, "nonPagedPool": get("Slab"),
			"speed": nil, "type": nil, "formFactor": nil, "slotsUsed": 0, "slotsTotal": 0, "modules": []any{},
		}
		pushRing(srv.rings, "memory", M{"data": M{"used": srv.memory["used"]}, "sampledAt": sampled})
	}

	/* ネットワーク（/proc/net/dev はホストのもの） */
	if nd := readFile(hp("net/dev")); nd != "" {
		type ad struct {
			name       string
			rx, tx     float64
			rxps, txps float64
		}
		var ads []*ad
		lines := strings.Split(nd, "\n")
		if len(lines) > 2 {
			lines = lines[2:]
		}
		for _, l := range lines {
			m := reNetLine.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			name := strings.TrimSpace(m[1])
			if reVirtual.MatchString(name) {
				continue
			}
			rx, _ := strconv.ParseFloat(m[2], 64)
			tx, _ := strconv.ParseFloat(m[3], 64)
			ads = append(ads, &ad{name: name, rx: rx, tx: tx})
		}
		ts := float64(now) / 1000
		if srv.netPrev != nil {
			dt := ts - srv.netPrev.t
			for _, a := range ads {
				if p, ok := srv.netPrev.m[a.name]; ok && dt > 0 {
					a.rxps = maxF(0, (a.rx-p[0])/dt)
					a.txps = maxF(0, (a.tx-p[1])/dt)
				}
			}
		}
		full, short := []any{}, []any{}
		first := ""
		if len(ads) > 0 {
			first = ads[0].name
		}
		for _, a := range ads {
			full = append(full, M{"name": a.name, "description": a.name, "kind": "ethernet", "mediaType": "802.3",
				"mac": nil, "linkSpeed": nil, "isDefault": a.name == first, "ipv4": nil, "ipv6": nil,
				"bytesReceived": a.rx, "bytesSent": a.tx, "recvBytesPerSec": a.rxps, "sentBytesPerSec": a.txps, "wifi": nil})
			short = append(short, M{"name": a.name, "bytesReceived": a.rx, "bytesSent": a.tx})
		}
		srv.network = M{"timestamp": now, "hostname": srvHostname(), "gateway": nil, "gatewayDev": nil, "dns": []any{}, "adapters": full}
		pushRing(srv.rings, "network", M{"data": M{"timestamp": now, "adapters": short}, "sampledAt": sampled})
		np := &struct {
			t float64
			m map[string][2]float64
		}{t: ts, m: map[string][2]float64{}}
		for _, a := range ads {
			np.m[a.name] = [2]float64{a.rx, a.tx}
		}
		srv.netPrev = np
	}
	srv.at = now
}

func srvCountProcesses() int {
	ents, err := os.ReadDir(conf().Stats.HostProc)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err == nil {
			n++
		}
	}
	return n
}

func startServerSampler() {
	srvSample()
	go func() {
		t := time.NewTicker(2 * time.Second)
		for range t.C {
			srvSample()
		}
	}()
}

/* ディスク（60 秒缓存） */
func srvDisk() []M {
	if srv.disks != nil && time.Since(srv.disksAt) < 60*time.Second {
		return srv.disks
	}
	out := []M{}
	for _, d := range conf().Stats.Disks {
		var st syscall.Statfs_t
		if syscall.Statfs(d.Path, &st) != nil {
			continue
		}
		total := float64(st.Blocks) * float64(st.Bsize)
		free := float64(st.Bavail) * float64(st.Bsize)
		if total == 0 {
			continue
		}
		out = append(out, M{"mountpoint": d.Label, "label": d.Label, "name": d.Label, "model": nil,
			"fileSystem": "ext4", "removable": false, "total": total, "used": maxF(0, total-free), "free": free,
			"usagePercent": (total - free) / total * 100})
	}
	srv.disks, srv.disksAt = out, time.Now()
	return out
}

func srvWindows() M {
	rel := readFile(filepath.Join(conf().Stats.HostRoot, "etc/os-release"))
	pretty := "Linux"
	if m := rePretty.FindStringSubmatch(rel); m != nil {
		pretty = m[1]
	}
	version := ""
	if f := strings.Split(readFile("/proc/version"), " "); len(f) > 2 {
		version = f[2]
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "64 ビット"
	}
	return M{
		"timestamp": time.Now().UnixMilli(), "hostname": srvHostname(),
		"os": M{"name": pretty, "productName": pretty, "edition": "", "displayVersion": "", "releaseId": "", "version": version,
			"build": "", "ubr": 0, "buildFull": version, "buildLab": "", "installationType": "Server",
			"architecture": arch, "systemType": "x64-based PC", "experiencePack": "", "installDate": nil},
		"update": M{"lastUpdate": nil, "historyTotal": 0, "historyTotalRaw": 0, "completedTotal": 0,
			"counts":  M{"feature": 0, "quality": 0, "driver": 0, "defender": 0, "other": 0, "store": 0},
			"updates": []any{}, "eventHistory": []any{}, "hotfixes": []any{}},
	}
}
