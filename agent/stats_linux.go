//go:build linux

package main

import (
	"bufio"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

/* Linux 系统资源：/proc、/sys、statfs；NVIDIA 用 nvidia-smi，AMD 读 amdgpu 的 sysfs。格式和 Windows 版一样 */

type linStats struct {
	mu      sync.Mutex
	prev    *cpuSnap
	netPrev map[string][3]float64
	host    string
	osName  string
	smi     string
	static  bool
	l1, l2, l3, cores, sockets int
	baseGHz float64
	virt    bool
	cpuName string
}

type cpuSnap struct {
	total, idle float64
	per         [][2]float64
}

func newLinStats() *linStats { return &linStats{netPrev: map[string][3]float64{}} }

func readF(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func cpuRead() *cpuSnap {
	s := &cpuSnap{}
	for _, l := range strings.Split(readF("/proc/stat"), "\n") {
		f := strings.Fields(l)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		var tot float64
		vals := make([]float64, 0, len(f)-1)
		for _, x := range f[1:] {
			v, _ := strconv.ParseFloat(x, 64)
			vals = append(vals, v)
		}
		for i, v := range vals {
			if i < 8 { // guest 已经算在 user 里
				tot += v
			}
		}
		idle := vals[3]
		if len(vals) > 4 {
			idle += vals[4]
		}
		if f[0] == "cpu" {
			s.total, s.idle = tot, idle
		} else {
			s.per = append(s.per, [2]float64{idle, tot})
		}
	}
	return s
}

func cacheKB(s string) int {
	s = strings.TrimSpace(s)
	mul := 1
	if strings.HasSuffix(s, "M") {
		mul, s = 1024, strings.TrimSuffix(s, "M")
	}
	s = strings.TrimSuffix(s, "K")
	n, _ := strconv.Atoi(s)
	return n * mul
}

var rePrettyL = regexp.MustCompile(`(?m)^PRETTY_NAME="?([^"\n]+)"?`)

func (s *linStats) loadStatic() {
	if s.static {
		return
	}
	s.static = true
	s.host, _ = os.Hostname()
	s.osName = "Linux"
	if m := rePrettyL.FindStringSubmatch(readF("/etc/os-release")); m != nil {
		s.osName = m[1]
	}
	ci := readF("/proc/cpuinfo")
	phys, coresSet := map[string]bool{}, map[string]bool{}
	cur := ""
	for _, l := range strings.Split(ci, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "model name":
			if s.cpuName == "" {
				s.cpuName = v
			}
		case "physical id":
			cur = v
			phys[v] = true
		case "core id":
			coresSet[cur+"/"+v] = true
		case "flags":
			if strings.Contains(" "+v+" ", " vmx ") || strings.Contains(" "+v+" ", " svm ") {
				s.virt = true
			}
		}
	}
	s.sockets, s.cores = len(phys), len(coresSet)
	if s.sockets == 0 {
		s.sockets = 1
	}
	if s.cores == 0 {
		s.cores = runtime.NumCPU()
	}
	// 缓存：每个 CPU 自己的 index*，按「共享这块缓存的 CPU 列表」去重后加起来
	seen := map[string]bool{}
	dirs, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cache/index[0-9]*")
	for _, d := range dirs {
		lvl := strings.TrimSpace(readF(d + "/level"))
		typ := strings.TrimSpace(readF(d + "/type"))
		key := lvl + typ + strings.TrimSpace(readF(d+"/shared_cpu_list"))
		if seen[key] {
			continue
		}
		seen[key] = true
		kb := cacheKB(readF(d + "/size"))
		switch lvl {
		case "1":
			s.l1 += kb
		case "2":
			s.l2 += kb
		case "3":
			s.l3 += kb
		}
	}
	for _, f := range []string{"/sys/devices/system/cpu/cpu0/cpufreq/base_frequency", "/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq"} {
		if v, err := strconv.ParseFloat(strings.TrimSpace(readF(f)), 64); err == nil && v > 0 {
			s.baseGHz = math.Round(v/1e4) / 100
			break
		}
	}
	if p, err := exec.LookPath("nvidia-smi"); err == nil {
		s.smi = p
	}
}

func r1l(x float64) float64 { return math.Round(x*10) / 10 }

func (s *linStats) collect(full bool) M {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStatic()
	nowT := time.Now()
	now := nowT.UnixMilli()
	out := M{}

	cur := cpuRead()
	usage := []any{}
	total := 0.0
	if s.prev != nil {
		if dt := cur.total - s.prev.total; dt > 0 {
			total = math.Max(0, math.Min(100, (1-(cur.idle-s.prev.idle)/dt)*100))
		}
	}
	for i := range cur.per {
		v := 0.0
		if s.prev != nil && i < len(s.prev.per) {
			if dt := cur.per[i][1] - s.prev.per[i][1]; dt > 0 {
				v = math.Max(0, math.Min(100, (1-(cur.per[i][0]-s.prev.per[i][0])/dt)*100))
			}
		}
		usage = append(usage, M{"id": "0," + strconv.Itoa(i), "usagePercent": r1l(v)})
	}
	s.prev = cur
	var mhz []float64
	for _, m := range regexp.MustCompile(`(?m)^cpu MHz\s*:\s*([\d.]+)`).FindAllStringSubmatch(readF("/proc/cpuinfo"), -1) {
		v, _ := strconv.ParseFloat(m[1], 64)
		mhz = append(mhz, v)
	}
	speed := s.baseGHz
	if len(mhz) > 0 {
		sum := 0.0
		for _, v := range mhz {
			sum += v
		}
		speed = math.Round(sum/float64(len(mhz))/10) / 100
	}
	up := 0.0
	if f := strings.Fields(readF("/proc/uptime")); len(f) > 0 {
		up, _ = strconv.ParseFloat(f[0], 64)
	}
	threads := 0
	if f := strings.Fields(readF("/proc/loadavg")); len(f) > 3 {
		if p := strings.Split(f[3], "/"); len(p) > 1 {
			threads, _ = strconv.Atoi(p[1])
		}
	}
	procs := 0
	if ents, err := os.ReadDir("/proc"); err == nil {
		for _, e := range ents {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				procs++
			}
		}
	}
	kernel := ""
	if f := strings.Fields(readF("/proc/version")); len(f) > 2 {
		kernel = f[2]
	}
	out["cpu"] = M{"timestamp": now, "hostname": s.host, "osName": s.osName, "osVersion": kernel, "osBuild": "", "name": or(s.cpuName, "CPU"),
		"usagePercent": r1l(total), "speedGHz": speed, "processes": procs, "threads": threads, "handles": nil,
		"uptimeMs": up * 1000, "bootTime": float64(now) - up*1000, "baseSpeedGHz": or0(s.baseGHz, speed), "sockets": s.sockets, "cores": s.cores,
		"logicalProcessors": len(cur.per), "virtualization": s.virt, "cache": M{"l1KB": s.l1, "l2KB": s.l2, "l3KB": s.l3},
		"logicalProcessorUsage": usage}

	mi := readF("/proc/meminfo")
	get := func(k string) float64 {
		m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(k) + `:\s+(\d+)`).FindStringSubmatch(mi)
		if m == nil {
			return 0
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return v * 1024
	}
	tot, avail := get("MemTotal"), get("MemAvailable")
	swapT, swapF := get("SwapTotal"), get("SwapFree")
	used := math.Max(0, tot-avail)
	out["memory"] = M{"timestamp": now, "hostname": s.host, "installed": tot, "total": tot, "used": used, "available": avail,
		"usagePercent": math.Round(used / math.Max(1, tot) * 100), "compressed": 0, "hardwareReserved": 0,
		"committed": get("Committed_AS"), "commitLimit": get("CommitLimit"), "pageFile": M{"used": math.Max(0, swapT-swapF), "total": swapT},
		"cached": get("Cached") + get("Buffers"), "standby": 0, "modified": get("Dirty"), "free": get("MemFree"),
		"pagedPool": 0, "nonPagedPool": get("Slab"), "speed": nil, "type": nil, "formFactor": nil, "slotsUsed": 0, "slotsTotal": 0, "modules": []any{}}

	if g := s.gpu(now); g != nil {
		out["gpu"] = g
	}

	out["network"] = s.network(now)
	if full {
		out["disk"] = s.disks(now)
		arch := runtime.GOARCH
		if arch == "amd64" {
			arch = "64 位"
		}
		out["windows"] = M{"timestamp": now, "hostname": s.host,
			"os": M{"name": s.osName, "productName": s.osName, "edition": "", "displayVersion": "", "releaseId": "", "version": kernel,
				"build": "", "ubr": 0, "buildFull": kernel, "buildLab": "", "installationType": "Desktop", "architecture": arch,
				"systemType": runtime.GOARCH, "experiencePack": "", "installDate": nil},
			"update": M{"lastUpdate": nil, "historyTotal": 0, "historyTotalRaw": 0, "completedTotal": 0,
				"counts": M{"feature": 0, "quality": 0, "driver": 0, "defender": 0, "other": 0, "store": 0},
				"updates": []any{}, "eventHistory": []any{}, "hotfixes": []any{}}}
	}
	return out
}

func or0(a, b float64) float64 {
	if a > 0 {
		return a
	}
	return b
}

var reVirtL = regexp.MustCompile(`^(lo|veth|br-|docker|virbr|tun|tap|wg|tailscale|zt|vnet|lxc|cni|flannel|kube)`)

func (s *linStats) network(now int64) M {
	ts := float64(now) / 1000
	gwDev, gw := "", ""
	for _, l := range strings.Split(readF("/proc/net/route"), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) > 2 && f[1] == "00000000" {
			gwDev = f[0]
			if v, err := strconv.ParseUint(f[2], 16, 32); err == nil {
				gw = strconv.Itoa(int(v&0xff)) + "." + strconv.Itoa(int(v>>8&0xff)) + "." + strconv.Itoa(int(v>>16&0xff)) + "." + strconv.Itoa(int(v>>24))
			}
			break
		}
	}
	ads := []any{}
	lines := strings.Split(readF("/proc/net/dev"), "\n")
	if len(lines) > 2 {
		lines = lines[2:]
	}
	for _, l := range lines {
		name, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if reVirtL.MatchString(name) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseFloat(f[0], 64)
		tx, _ := strconv.ParseFloat(f[8], 64)
		if strings.TrimSpace(readF("/sys/class/net/"+name+"/operstate")) == "down" {
			continue
		}
		rps, tps := 0.0, 0.0
		if p, had := s.netPrev[name]; had && ts > p[2] {
			rps, tps = math.Max(0, (rx-p[0])/(ts-p[2])), math.Max(0, (tx-p[1])/(ts-p[2]))
		}
		s.netPrev[name] = [3]float64{rx, tx, ts}
		kind, media := "ethernet", "802.3"
		if _, err := os.Stat("/sys/class/net/" + name + "/wireless"); err == nil {
			kind, media = "wifi", "802.11"
		}
		var speed any
		if v, err := strconv.ParseFloat(strings.TrimSpace(readF("/sys/class/net/"+name+"/speed")), 64); err == nil && v > 0 {
			speed = v * 1e6
		}
		var mac any
		if m := strings.TrimSpace(readF("/sys/class/net/" + name + "/address")); m != "" {
			mac = strings.ToUpper(strings.ReplaceAll(m, ":", "-"))
		}
		ads = append(ads, M{"name": name, "description": name, "kind": kind, "mediaType": media, "mac": mac, "linkSpeed": speed,
			"isDefault": name == gwDev, "ipv4": ifaceIP(name, true), "ipv6": ifaceIP(name, false), "bytesReceived": rx, "bytesSent": tx,
			"recvBytesPerSec": r1l(rps), "sentBytesPerSec": r1l(tps), "wifi": nil})
	}
	var gwA, gwD any
	if gw != "" {
		gwA, gwD = gw, gwDev
	}
	dns := []any{}
	for _, l := range strings.Split(readF("/etc/resolv.conf"), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "nameserver" {
			dns = append(dns, f[1])
		}
	}
	return M{"timestamp": now, "hostname": s.host, "gateway": gwA, "gatewayDev": gwD, "dns": dns, "adapters": ads}
}

func ifaceIP(name string, v4 bool) any {
	ifc, err := netInterfaceByName(name)
	if err != nil {
		return nil
	}
	for _, a := range ifc {
		if (a.To4() != nil) == v4 && a.IsGlobalUnicast() {
			return a.String()
		}
	}
	return nil
}

var realFS = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "btrfs": true, "xfs": true, "vfat": true, "exfat": true, "ntfs": true,
	"ntfs3": true, "fuseblk": true, "zfs": true, "f2fs": true, "bcachefs": true, "jfs": true, "reiserfs": true}

func (s *linStats) disks(now int64) M {
	disks := []any{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(readF("/proc/mounts")))
	type mnt struct{ dev, mp, fs string }
	var ms []mnt
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		mp := strings.ReplaceAll(f[1], `\040`, " ")
		if strings.HasPrefix(mp, "/snap/") || strings.HasPrefix(mp, "/boot") || strings.HasPrefix(mp, "/var/lib/docker") {
			continue
		}
		seen[f[0]] = true
		ms = append(ms, mnt{f[0], mp, f[2]})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].mp < ms[j].mp })
	for _, m := range ms {
		var st syscall.Statfs_t
		if syscall.Statfs(m.mp, &st) != nil || st.Blocks == 0 {
			continue
		}
		total := float64(st.Blocks) * float64(st.Bsize)
		free := float64(st.Bavail) * float64(st.Bsize)
		used := total - float64(st.Bfree)*float64(st.Bsize)
		removable := strings.HasPrefix(m.mp, "/media/") || strings.HasPrefix(m.mp, "/run/media/")
		disks = append(disks, M{"mountpoint": m.mp, "label": filepath.Base(m.dev), "name": filepath.Base(m.dev), "model": nil, "fileSystem": m.fs,
			"removable": removable, "total": total, "used": used, "free": free, "usagePercent": r1l(used / total * 100)})
	}
	return M{"timestamp": now, "hostname": s.host, "systemDrive": "/", "disks": disks}
}

// gpu：NVIDIA（nvidia-smi，一次几十毫秒）或 AMD（sysfs），都没有就不报
func (s *linStats) gpu(now int64) M {
	if s.smi != "" {
		out, err := exec.Command(s.smi, "--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,driver_version,utilization.encoder,utilization.decoder",
			"--format=csv,noheader,nounits").Output()
		if err == nil {
			f := strings.Split(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0], ",")
			if len(f) >= 6 {
				num := func(i int) float64 {
					if i >= len(f) {
						return 0
					}
					v, _ := strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
					return v
				}
				ded, lim := num(2)*1024*1024, num(3)*1024*1024
				pct := 0.0
				if lim > 0 {
					pct = r1l(ded / lim * 100)
				}
				engs := []any{M{"type": "3D", "label": "3D", "usagePercent": num(1)}}
				if len(f) >= 8 {
					engs = append(engs, M{"type": "VideoEncode", "label": "Video Encode", "usagePercent": num(6)},
						M{"type": "VideoDecode", "label": "Video Decode", "usagePercent": num(7)})
				}
				return M{"timestamp": now, "hostname": s.host, "gpuCount": 1, "name": strings.TrimSpace(f[0]), "usagePercent": num(1), "temperatureC": num(4),
					"memory": M{"dedicatedUsed": ded, "dedicatedLimit": lim, "sharedUsed": 0, "sharedLimit": 0, "totalUsed": ded, "totalLimit": lim,
						"usagePercent": pct, "dedicatedPercent": pct, "adapterRAM": 0},
					"engines": engs, "driver": M{"version": strings.TrimSpace(f[5]), "date": nil, "dateMs": nil, "provider": "NVIDIA", "vendorVersion": strings.TrimSpace(f[5])},
					"directX": M{"version": nil, "featureLevel": nil}}
			}
		}
	}
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]/device/gpu_busy_percent")
	if len(cards) == 0 {
		return nil
	}
	dev := filepath.Dir(cards[0])
	num := func(p string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(readF(p)), 64); return v }
	busy := num(dev + "/gpu_busy_percent")
	ded, lim := num(dev+"/mem_info_vram_used"), num(dev+"/mem_info_vram_total")
	gtt, gttT := num(dev+"/mem_info_gtt_used"), num(dev+"/mem_info_gtt_total")
	var temp any
	if hw, _ := filepath.Glob(dev + "/hwmon/hwmon*/temp1_input"); len(hw) > 0 {
		temp = num(hw[0]) / 1000
	}
	pct := 0.0
	if lim > 0 {
		pct = r1l(ded / lim * 100)
	}
	name := "AMD GPU"
	if id := strings.TrimSpace(readF(dev + "/product_name")); id != "" {
		name = id
	}
	return M{"timestamp": now, "hostname": s.host, "gpuCount": len(cards), "name": name, "usagePercent": busy, "temperatureC": temp,
		"memory": M{"dedicatedUsed": ded, "dedicatedLimit": lim, "sharedUsed": gtt, "sharedLimit": gttT, "totalUsed": ded + gtt, "totalLimit": lim + gttT,
			"usagePercent": pct, "dedicatedPercent": pct, "adapterRAM": 0},
		"engines": []any{M{"type": "3D", "label": "3D", "usagePercent": busy}},
		"driver": M{"version": nil, "date": nil, "dateMs": nil, "provider": "AMD", "vendorVersion": nil}, "directX": M{"version": nil, "featureLevel": nil}}
}
