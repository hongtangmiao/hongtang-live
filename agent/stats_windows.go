//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

/*
 * 系统资源（替代原 C# 采集器，输出格式和它一样，见服务器 stats.go 的 pcReportTo）。
 *   动态数据：性能计数器（PDH）+ 几个 Win32 API，4 秒一次
 *   静态数据：CPU / 内存条 / 显卡驱动 / 系统版本 / 补丁，用一次 PowerShell（CIM）查，6 小时刷新
 *   显卡：DXGI 拿显存上限和 LUID（和计数器对上号），NVIDIA 的温度走 nvml.dll
 */

var (
	pdh                          = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery             = pdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounter     = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData      = pdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedValue     = pdh.NewProc("PdhGetFormattedCounterValue")
	procPdhGetFormattedArray     = pdh.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery            = pdh.NewProc("PdhCloseQuery")
	procGetTickCount64           = kernel32.NewProc("GetTickCount64")
	procGetPhysicallyInstalled   = kernel32.NewProc("GetPhysicallyInstalledSystemMemory")
	procGlobalMemoryStatusEx     = kernel32.NewProc("GlobalMemoryStatusEx")
	iphlpapi                     = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIfEntry2              = iphlpapi.NewProc("GetIfEntry2")
	dxgiDLL                      = windows.NewLazySystemDLL("dxgi.dll")
	procCreateDXGIFactory1       = dxgiDLL.NewProc("CreateDXGIFactory1")
	d3d11DLL                     = windows.NewLazySystemDLL("d3d11.dll")
	procD3D11CreateDevice        = d3d11DLL.NewProc("D3D11CreateDevice")
)

const (
	pdhFmtDouble   = 0x00000200
	pdhFmtNoCap100 = 0x00008000
	pdhMoreData    = 0x800007D2
)

/* ---------------- PDH ---------------- */

type pdhQuery struct {
	h   uintptr
	ctr map[string]uintptr
}

func newPDH(paths []string) *pdhQuery {
	q := &pdhQuery{ctr: map[string]uintptr{}}
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q.h))); r != 0 {
		logf("PDH 打不开：0x%x", r)
		return q
	}
	for _, p := range paths {
		var c uintptr
		ptr, _ := windows.UTF16PtrFromString(p)
		if r, _, _ := procPdhAddEnglishCounter.Call(q.h, uintptr(unsafe.Pointer(ptr)), 0, uintptr(unsafe.Pointer(&c))); r == 0 {
			q.ctr[p] = c
		}
	}
	return q
}

func (q *pdhQuery) collect() {
	if q.h != 0 {
		procPdhCollectQueryData.Call(q.h)
	}
}

func (q *pdhQuery) close() {
	if q.h != 0 {
		procPdhCloseQuery.Call(q.h)
		q.h = 0
	}
}

type pdhValue struct {
	Status uint32
	_      uint32
	Double float64
}

func (q *pdhQuery) value(path string, nocap bool) (float64, bool) {
	c, ok := q.ctr[path]
	if !ok {
		return 0, false
	}
	var typ uint32
	var v pdhValue
	f := uintptr(pdhFmtDouble)
	if nocap {
		f |= pdhFmtNoCap100
	}
	if r, _, _ := procPdhGetFormattedValue.Call(c, f, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&v))); r != 0 || v.Status > 1 {
		return 0, false
	}
	return v.Double, true
}

type pdhItem struct {
	Name  *uint16
	Value pdhValue
}

func (q *pdhQuery) array(path string) map[string]float64 {
	out := map[string]float64{}
	c, ok := q.ctr[path]
	if !ok {
		return out
	}
	var size, count uint32
	r, _, _ := procPdhGetFormattedArray.Call(c, pdhFmtDouble|pdhFmtNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return out
	}
	buf := make([]byte, size)
	r, _, _ = procPdhGetFormattedArray.Call(c, pdhFmtDouble|pdhFmtNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return out
	}
	items := unsafe.Slice((*pdhItem)(unsafe.Pointer(&buf[0])), count)
	for _, it := range items {
		if it.Value.Status > 1 {
			continue
		}
		out[windows.UTF16PtrToString(it.Name)] += it.Value.Double // 同名实例（多个同名进程）累加
	}
	return out
}

/* ---------------- 静态信息（PowerShell / CIM） ---------------- */

//go:embed static.ps1
var staticScript string

type staticInfo struct {
	CPU struct {
		Name           string `json:"name"`
		Cores          int    `json:"cores"`
		Logical        int    `json:"logical"`
		Sockets        int    `json:"sockets"`
		L1KB           int    `json:"l1KB"`
		L2KB           int    `json:"l2KB"`
		L3KB           int    `json:"l3KB"`
		BaseMHz        int    `json:"baseMHz"`
		Virtualization bool   `json:"virtualization"`
	} `json:"cpu"`
	Memory struct {
		Modules    []M    `json:"modules"`
		SlotsTotal int    `json:"slotsTotal"`
		Type       string `json:"type"`
		FormFactor string `json:"formFactor"`
		Speed      int    `json:"speed"`
	} `json:"memory"`
	GPU []struct {
		Name          string `json:"name"`
		DriverVersion string `json:"driverVersion"`
		DriverDate    string `json:"driverDate"`
		Provider      string `json:"provider"`
	} `json:"gpu"`
	OS struct {
		Caption        string `json:"caption"`
		Version        string `json:"version"`
		Build          string `json:"build"`
		InstallDate    int64  `json:"installDate"`
		Architecture   string `json:"architecture"`
		SystemType     string `json:"systemType"`
		ExperiencePack string `json:"experiencePack"`
	} `json:"os"`
	Updates []struct {
		KB     string `json:"kb"`
		Title  string `json:"title"`
		DateMs int64  `json:"dateMs"`
	} `json:"updates"`
	PageFile struct {
		AllocatedMB float64 `json:"allocatedMB"`
	} `json:"pagefile"`
}

func loadStatic() (*staticInfo, error) {
	dir := dataDirFn()
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "static.ps1")
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte(staticScript)...), 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = cmd.Output(); close(done) }()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		_ = cmd.Process.Kill()
		return nil, agentErr("硬件信息查询超时")
	}
	if err != nil {
		return nil, err
	}
	s := &staticInfo{}
	b := []byte(strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff")))
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("硬件信息看不懂：%w", err)
	}
	return s, nil
}

/* ---------------- 显卡：DXGI / D3D11 / NVML ---------------- */

type dxgiDesc1 struct {
	Description           [128]uint16
	VendorID, DeviceID    uint32
	SubSysID, Revision    uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	LuidLow               uint32
	LuidHigh              int32
	Flags                 uint32
}

type gpuAdapter struct {
	name                    string
	vendor                  uint32
	dedicated, shared       float64
	luid                    string // 计数器实例名里的写法：luid_0x00000000_0x0000D1A5
}

var iidDXGIFactory1 = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}

func comCall(obj unsafe.Pointer, idx uintptr, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, idx*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func dxgiAdapters() []gpuAdapter {
	if procCreateDXGIFactory1.Find() != nil {
		return nil
	}
	var f unsafe.Pointer
	if r, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidDXGIFactory1)), uintptr(unsafe.Pointer(&f))); r != 0 || f == nil {
		return nil
	}
	defer comCall(f, 2) // Release
	var out []gpuAdapter
	for i := uintptr(0); i < 16; i++ {
		var ad unsafe.Pointer
		if r := comCall(f, 12, i, uintptr(unsafe.Pointer(&ad))); r != 0 || ad == nil { // EnumAdapters1
			break
		}
		var d dxgiDesc1
		r := comCall(ad, 10, uintptr(unsafe.Pointer(&d))) // GetDesc1
		comCall(ad, 2)
		if r != 0 || d.Flags&2 != 0 || d.VendorID == 0x1414 { // 软件渲染器 / 微软基本显示
			continue
		}
		out = append(out, gpuAdapter{name: windows.UTF16ToString(d.Description[:]), vendor: d.VendorID,
			dedicated: float64(d.DedicatedVideoMemory), shared: float64(d.SharedSystemMemory),
			luid: fmt.Sprintf("luid_0x%08X_0x%08X", uint32(d.LuidHigh), d.LuidLow)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].dedicated > out[j].dedicated })
	return out
}

func d3dFeatureLevel() string {
	if procD3D11CreateDevice.Find() != nil {
		return ""
	}
	try := func(levels []uint32) uint32 {
		var fl uint32
		r, _, _ := procD3D11CreateDevice.Call(0, 1, 0, 0, uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)), 7, 0,
			uintptr(unsafe.Pointer(&fl)), 0)
		if r != 0 && r != 1 { // 不要设备、只问功能级别时成功返回的是 S_FALSE（1）
			return 0
		}
		return fl
	}
	fl := try([]uint32{0xc100, 0xc000, 0xb100, 0xb000, 0xa100, 0xa000})
	if fl == 0 {
		fl = try([]uint32{0xb100, 0xb000, 0xa100, 0xa000})
	}
	if fl == 0 {
		return ""
	}
	return fmt.Sprintf("%d_%d", fl>>12, (fl>>8)&0xf)
}

type nvml struct {
	ok   bool
	dev  uintptr
	temp *windows.LazyProc
}

func loadNVML() *nvml {
	n := &nvml{}
	paths := []string{filepath.Join(os.Getenv("SystemRoot"), "System32", "nvml.dll"),
		filepath.Join(os.Getenv("ProgramFiles"), "NVIDIA Corporation", "NVSMI", "nvml.dll")}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		dll := windows.NewLazyDLL(p)
		initp, geth, temp := dll.NewProc("nvmlInit_v2"), dll.NewProc("nvmlDeviceGetHandleByIndex_v2"), dll.NewProc("nvmlDeviceGetTemperature")
		if initp.Find() != nil || geth.Find() != nil || temp.Find() != nil {
			continue
		}
		if r, _, _ := initp.Call(); r != 0 {
			continue
		}
		if r, _, _ := geth.Call(0, uintptr(unsafe.Pointer(&n.dev))); r != 0 {
			continue
		}
		n.ok, n.temp = true, temp
		return n
	}
	return n
}

func (n *nvml) temperature() any {
	if !n.ok {
		return nil
	}
	var t uint32
	if r, _, _ := n.temp.Call(n.dev, 0, uintptr(unsafe.Pointer(&t))); r != 0 {
		return nil
	}
	return float64(t)
}

/* ---------------- 网络 ---------------- */

// mibIfRow2：MIB_IF_ROW2（netioapi.h），x64 布局，只用到计数器
type mibIfRow2 struct {
	InterfaceLuid            uint64
	InterfaceIndex           uint32
	InterfaceGuid            windows.GUID
	Alias                    [257]uint16
	Description              [257]uint16
	PhysicalAddressLength    uint32
	PhysicalAddress          [32]byte
	PermanentPhysicalAddress [32]byte
	Mtu                      uint32
	Type                     uint32
	TunnelType               uint32
	MediaType                uint32
	PhysicalMediumType       uint32
	AccessType               uint32
	DirectionType            uint32
	Flags                    uint8
	OperStatus               uint32
	AdminStatus              uint32
	MediaConnectState        uint32
	NetworkGuid              windows.GUID
	ConnectionType           uint32
	TransmitLinkSpeed        uint64
	ReceiveLinkSpeed         uint64
	InOctets                 uint64
	InUcastPkts              uint64
	InNUcastPkts             uint64
	InDiscards               uint64
	InErrors                 uint64
	InUnknownProtos          uint64
	InUcastOctets            uint64
	InMulticastOctets        uint64
	InBroadcastOctets        uint64
	OutOctets                uint64
	OutUcastPkts             uint64
	OutNUcastPkts            uint64
	OutDiscards              uint64
	OutErrors                uint64
	OutUcastOctets           uint64
	OutMulticastOctets       uint64
	OutBroadcastOctets       uint64
	OutQLen                  uint64
}

func ifOctets(luid uint64) (uint64, uint64, bool) {
	var row mibIfRow2
	row.InterfaceLuid = luid
	if r, _, _ := procGetIfEntry2.Call(uintptr(unsafe.Pointer(&row))); r != 0 {
		return 0, 0, false
	}
	return row.InOctets, row.OutOctets, true
}

var reVirtualNIC = regexp.MustCompile(`(?i)virtual|hyper-v|vmware|virtualbox|vethernet|wsl|tap-|wireguard|tailscale|zerotier|loopback|bluetooth|wan miniport|teredo|isatap`)

type netAdapter struct {
	name, desc, kind, media, mac string
	linkSpeed                    float64
	gateway                      string
	dns                          []string
	ipv4, ipv6                   string
	luid                         uint64
	metric                       uint32
}

func listAdapters() []netAdapter {
	var size uint32 = 15000
	var buf []byte
	for i := 0; i < 3; i++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS|windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST,
			0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil
		}
	}
	var out []netAdapter
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp || (a.IfType != 6 && a.IfType != 71) {
			continue
		}
		desc := windows.UTF16PtrToString(a.Description)
		name := windows.UTF16PtrToString(a.FriendlyName)
		if reVirtualNIC.MatchString(desc) || reVirtualNIC.MatchString(name) {
			continue
		}
		ad := netAdapter{name: name, desc: desc, kind: "ethernet", media: "802.3", linkSpeed: float64(a.TransmitLinkSpeed), luid: a.Luid, metric: a.Ipv4Metric}
		if a.IfType == 71 {
			ad.kind, ad.media = "wifi", "Native 802.11"
		}
		if a.PhysicalAddressLength > 0 {
			parts := make([]string, a.PhysicalAddressLength)
			for i := range parts {
				parts[i] = fmt.Sprintf("%02X", a.PhysicalAddress[i])
			}
			ad.mac = strings.Join(parts, "-")
		}
		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			ip := u.Address.IP()
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				if ad.ipv4 == "" {
					ad.ipv4 = v4.String()
				}
			} else if ip.IsGlobalUnicast() && ad.ipv6 == "" {
				ad.ipv6 = ip.String()
			}
		}
		for g := a.FirstGatewayAddress; g != nil; g = g.Next {
			if ip := g.Address.IP(); ip != nil && ip.To4() != nil && ad.gateway == "" {
				ad.gateway = ip.String()
			}
		}
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			if ip := d.Address.IP(); ip != nil && !ip.IsLinkLocalUnicast() {
				ad.dns = append(ad.dns, ip.String())
			}
		}
		out = append(out, ad)
	}
	return out
}

/* ---------------- 汇总 ---------------- */

type winStats struct {
	mu        sync.Mutex
	q         *pdhQuery
	static    *staticInfo
	staticAt  time.Time
	loading   bool
	gpus      []gpuAdapter
	fl        string
	nv        *nvml
	netPrev   map[uint64][3]float64 // luid → rx, tx, 时间
	adapters  []netAdapter
	adaptAt   time.Time
	host      string
	started   bool
}

func newWinStats() *winStats { return &winStats{netPrev: map[uint64][3]float64{}} }

var counterPaths = []string{
	`\Processor Information(_Total)\% Processor Utility`,
	`\Processor Information(*)\% Processor Utility`,
	`\Processor Information(_Total)\% Processor Performance`,
	`\System\Processes`, `\System\Threads`, `\Process(_Total)\Handle Count`,
	`\Memory\Committed Bytes`, `\Memory\Commit Limit`, `\Memory\Pool Paged Bytes`, `\Memory\Pool Nonpaged Bytes`,
	`\Memory\Modified Page List Bytes`, `\Memory\Standby Cache Core Bytes`, `\Memory\Standby Cache Normal Priority Bytes`,
	`\Memory\Standby Cache Reserve Bytes`, `\Memory\Free & Zero Page List Bytes`, `\Process(Memory Compression)\Working Set`,
	`\Paging File(_Total)\% Usage`,
	`\GPU Engine(*)\Utilization Percentage`, `\GPU Adapter Memory(*)\Dedicated Usage`, `\GPU Adapter Memory(*)\Shared Usage`,
}

func (s *winStats) init() {
	if s.started {
		return
	}
	s.started = true
	s.q = newPDH(counterPaths)
	s.q.collect()
	s.gpus = dxgiAdapters()
	s.fl = d3dFeatureLevel()
	if len(s.gpus) > 0 && s.gpus[0].vendor == 0x10DE {
		s.nv = loadNVML()
	} else {
		s.nv = &nvml{}
	}
	s.host, _ = windows.ComputerName()
	if s.host == "" {
		s.host, _ = os.Hostname()
	}
	logf("系统资源：计数器 %d/%d 个可用，显卡 %d 块，DirectX 功能级别 %s", len(s.q.ctr), len(counterPaths), len(s.gpus), or(s.fl, "?"))
}

func (s *winStats) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.q != nil {
		s.q.close()
	}
}

func (s *winStats) refreshStatic(wait bool) {
	if s.loading {
		return
	}
	s.loading = true
	run := func() {
		st, err := loadStatic()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.loading = false
		if err != nil {
			logf("硬件信息读取失败：%v", err)
			s.staticAt = time.Now().Add(-5*time.Hour - 50*time.Minute) // 10 分钟后再试
			return
		}
		s.static, s.staticAt = st, time.Now()
	}
	if wait {
		s.mu.Unlock()
		run()
		s.mu.Lock()
	} else {
		go run()
	}
}

func regStr(k registry.Key, name string) string {
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}

func r1(x float64) float64 { return math.Round(x*10) / 10 }

func (s *winStats) collect(full bool) M {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	if s.static == nil && !s.loading {
		s.refreshStatic(true)
	} else if time.Since(s.staticAt) > 6*time.Hour {
		s.refreshStatic(false)
	}
	st := s.static
	if st == nil {
		st = &staticInfo{}
	}
	s.q.collect()
	nowT := time.Now()
	now := nowT.UnixMilli()
	out := M{}

	/* CPU */
	up, _, _ := procGetTickCount64.Call()
	total, _ := s.q.value(`\Processor Information(_Total)\% Processor Utility`, false)
	perf, _ := s.q.value(`\Processor Information(_Total)\% Processor Performance`, true)
	per := s.q.array(`\Processor Information(*)\% Processor Utility`)
	type lp struct {
		g, n int
		v    float64
	}
	var lps []lp
	for name, v := range per {
		p := strings.SplitN(name, ",", 2)
		if len(p) != 2 || strings.Contains(name, "_Total") {
			continue
		}
		g, e1 := strconv.Atoi(p[0])
		n, e2 := strconv.Atoi(p[1])
		if e1 != nil || e2 != nil {
			continue
		}
		lps = append(lps, lp{g, n, math.Min(100, v)})
	}
	sort.Slice(lps, func(i, j int) bool { return lps[i].g < lps[j].g || lps[i].g == lps[j].g && lps[i].n < lps[j].n })
	usage := []any{}
	for _, x := range lps {
		usage = append(usage, M{"id": fmt.Sprintf("%d,%d", x.g, x.n), "usagePercent": r1(x.v)})
	}
	base := float64(st.CPU.BaseMHz) / 1000
	speed := base
	if perf > 0 && base > 0 {
		speed = math.Round(base*perf/100*100) / 100
	}
	procs, _ := s.q.value(`\System\Processes`, true)
	threads, _ := s.q.value(`\System\Threads`, true)
	handles, _ := s.q.value(`\Process(_Total)\Handle Count`, true)
	cpuName := st.CPU.Name
	if cpuName == "" {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE); err == nil {
			cpuName = strings.TrimSpace(regStr(k, "ProcessorNameString"))
			k.Close()
		}
	}
	logical := st.CPU.Logical
	if logical == 0 {
		logical = len(lps)
	}
	out["cpu"] = M{"timestamp": now, "hostname": s.host, "osName": st.OS.Caption, "osVersion": st.OS.Version, "osBuild": st.OS.Build,
		"name": or(cpuName, "CPU"), "usagePercent": r1(math.Min(100, total)), "speedGHz": speed,
		"processes": int(procs), "threads": int(threads), "handles": int(handles), "uptimeMs": float64(up), "bootTime": float64(now) - float64(up),
		"baseSpeedGHz": base, "sockets": max(1, st.CPU.Sockets), "cores": st.CPU.Cores, "logicalProcessors": logical,
		"virtualization": st.CPU.Virtualization, "cache": M{"l1KB": st.CPU.L1KB, "l2KB": st.CPU.L2KB, "l3KB": st.CPU.L3KB},
		"logicalProcessorUsage": usage}

	/* 内存 */
	var ms struct {
		Length, MemoryLoad                       uint32
		TotalPhys, AvailPhys                     uint64
		TotalPageFile, AvailPageFile             uint64
		TotalVirtual, AvailVirtual, AvailExtVirt uint64
	}
	ms.Length = uint32(unsafe.Sizeof(ms))
	procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	var instKB uint64
	procGetPhysicallyInstalled.Call(uintptr(unsafe.Pointer(&instKB)))
	tot, avail := float64(ms.TotalPhys), float64(ms.AvailPhys)
	inst := float64(instKB) * 1024
	if inst < tot {
		inst = tot
	}
	v := func(p string) float64 { x, _ := s.q.value(p, true); return x }
	standby := v(`\Memory\Standby Cache Core Bytes`) + v(`\Memory\Standby Cache Normal Priority Bytes`) + v(`\Memory\Standby Cache Reserve Bytes`)
	modified := v(`\Memory\Modified Page List Bytes`)
	pfTotal := st.PageFile.AllocatedMB * 1024 * 1024
	pfUsed := pfTotal * v(`\Paging File(_Total)\% Usage`) / 100
	used := math.Max(0, tot-avail)
	modules := st.Memory.Modules
	if modules == nil {
		modules = []M{}
	}
	var speedAny, typeAny, ffAny any
	if st.Memory.Speed > 0 {
		speedAny = st.Memory.Speed
	}
	if st.Memory.Type != "" {
		typeAny = st.Memory.Type
	}
	if st.Memory.FormFactor != "" {
		ffAny = st.Memory.FormFactor
	}
	out["memory"] = M{"timestamp": now, "hostname": s.host, "installed": inst, "total": tot, "used": used, "available": avail,
		"usagePercent": math.Round(used / math.Max(1, tot) * 100), "compressed": v(`\Process(Memory Compression)\Working Set`),
		"hardwareReserved": math.Max(0, inst-tot), "committed": v(`\Memory\Committed Bytes`), "commitLimit": v(`\Memory\Commit Limit`),
		"pageFile": M{"used": math.Round(pfUsed), "total": pfTotal}, "cached": standby + modified, "standby": standby, "modified": modified,
		"free": v(`\Memory\Free & Zero Page List Bytes`), "pagedPool": v(`\Memory\Pool Paged Bytes`), "nonPagedPool": v(`\Memory\Pool Nonpaged Bytes`),
		"speed": speedAny, "type": typeAny, "formFactor": ffAny, "slotsUsed": len(modules), "slotsTotal": st.Memory.SlotsTotal, "modules": modules}

	/* 显卡 */
	if len(s.gpus) > 0 {
		g := s.gpus[0]
		engine := s.q.array(`\GPU Engine(*)\Utilization Percentage`)
		perEng := map[string]float64{} // phys_eng → 各进程加起来
		engType := map[string]string{}
		for inst, val := range engine {
			if !strings.Contains(inst, g.luid) {
				continue
			}
			i := strings.Index(inst, "_phys_")
			j := strings.Index(inst, "_engtype_")
			if i < 0 || j < i {
				continue
			}
			key := inst[i:j]
			perEng[key] += val
			engType[key] = inst[j+len("_engtype_"):]
		}
		byType := map[string]float64{}
		maxU := 0.0
		for k, val := range perEng {
			val = math.Min(100, val)
			t := engType[k]
			if val > byType[t] || byType[t] == 0 {
				byType[t] = math.Max(byType[t], val)
			}
			maxU = math.Max(maxU, val)
		}
		label := map[string]string{"3D": "3D", "Copy": "Copy", "VideoEncode": "Video Encode", "VideoDecode": "Video Decode",
			"Video Codec": "Video Codec", "VideoProcessing": "Video Processing", "Compute_0": "Compute_0", "Compute_1": "Compute_1", "Cuda": "Cuda"}
		engs := []any{}
		for _, t := range []string{"3D", "Copy", "VideoEncode", "VideoDecode", "Video Codec", "VideoProcessing", "Compute_0", "Compute_1", "Cuda"} {
			if _, ok := byType[t]; ok && len(engs) < 4 {
				engs = append(engs, M{"type": t, "label": label[t], "usagePercent": math.Round(byType[t])})
			}
		}
		ded, shr := 0.0, 0.0
		for inst, val := range s.q.array(`\GPU Adapter Memory(*)\Dedicated Usage`) {
			if strings.Contains(inst, g.luid) {
				ded += val
			}
		}
		for inst, val := range s.q.array(`\GPU Adapter Memory(*)\Shared Usage`) {
			if strings.Contains(inst, g.luid) {
				shr += val
			}
		}
		pct := 0.0
		if g.dedicated > 0 {
			pct = r1(ded / g.dedicated * 100)
		}
		drv := M{"version": nil, "date": nil, "dateMs": nil, "provider": nil, "vendorVersion": nil}
		for _, sg := range st.GPU {
			if strings.EqualFold(strings.TrimSpace(sg.Name), strings.TrimSpace(g.name)) || len(st.GPU) == 1 {
				var dm any
				if t, err := time.ParseInLocation("2006-01-02", sg.DriverDate, time.Local); err == nil {
					dm = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
				}
				drv = M{"version": sg.DriverVersion, "date": sg.DriverDate, "dateMs": dm, "provider": sg.Provider, "vendorVersion": sg.DriverVersion}
			}
		}
		var fl any
		if s.fl != "" {
			fl = s.fl
		}
		out["gpu"] = M{"timestamp": now, "hostname": s.host, "gpuCount": len(s.gpus), "name": g.name, "usagePercent": math.Round(maxU),
			"temperatureC": s.nv.temperature(),
			"memory": M{"dedicatedUsed": ded, "dedicatedLimit": g.dedicated, "sharedUsed": shr, "sharedLimit": g.shared,
				"totalUsed": ded + shr, "totalLimit": g.dedicated + g.shared, "usagePercent": pct, "dedicatedPercent": pct, "adapterRAM": 0},
			"engines": engs, "driver": drv, "directX": M{"version": "12", "featureLevel": fl}}
	}

	/* 网络（网卡列表 60 秒刷新一次，计数器每次读） */
	if s.adapters == nil || time.Since(s.adaptAt) > time.Minute {
		s.adapters, s.adaptAt = listAdapters(), time.Now()
	}
	ts := float64(now) / 1000
	var def *netAdapter
	for i := range s.adapters {
		a := &s.adapters[i]
		if a.gateway != "" && (def == nil || a.metric < def.metric) {
			def = a
		}
	}
	ads := []any{}
	for i := range s.adapters {
		a := &s.adapters[i]
		rx, tx, ok := ifOctets(a.luid)
		if !ok {
			continue
		}
		rps, tps := 0.0, 0.0
		if p, had := s.netPrev[a.luid]; had && ts > p[2] {
			rps = math.Max(0, (float64(rx)-p[0])/(ts-p[2]))
			tps = math.Max(0, (float64(tx)-p[1])/(ts-p[2]))
		}
		s.netPrev[a.luid] = [3]float64{float64(rx), float64(tx), ts}
		var ip4, ip6, mac any
		if a.ipv4 != "" {
			ip4 = a.ipv4
		}
		if a.ipv6 != "" {
			ip6 = a.ipv6
		}
		if a.mac != "" {
			mac = a.mac
		}
		ads = append(ads, M{"name": a.name, "description": a.desc, "kind": a.kind, "mediaType": a.media, "mac": mac, "linkSpeed": a.linkSpeed,
			"isDefault": def == a, "ipv4": ip4, "ipv6": ip6, "bytesReceived": float64(rx), "bytesSent": float64(tx),
			"recvBytesPerSec": r1(rps), "sentBytesPerSec": r1(tps), "wifi": nil})
	}
	var gw, gwDev any
	dns := []any{}
	if def != nil {
		gw, gwDev = def.gateway, def.name
		for _, d := range def.dns {
			dns = append(dns, d)
		}
	}
	out["network"] = M{"timestamp": now, "hostname": s.host, "gateway": gw, "gatewayDev": gwDev, "dns": dns, "adapters": ads}

	if full {
		out["disk"] = s.disks(now)
		out["windows"] = s.windowsSection(st, now)
	}
	return out
}

func (s *winStats) disks(now int64) M {
	disks := []any{}
	mask, _ := windows.GetLogicalDrives()
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		rp, _ := windows.UTF16PtrFromString(root)
		t := windows.GetDriveType(rp)
		if t != windows.DRIVE_FIXED && t != windows.DRIVE_REMOVABLE {
			continue
		}
		var free, total, totalFree uint64
		if windows.GetDiskFreeSpaceEx(rp, &free, &total, &totalFree) != nil || total == 0 {
			continue
		}
		label := make([]uint16, 261)
		fs := make([]uint16, 261)
		_ = windows.GetVolumeInformation(rp, &label[0], uint32(len(label)), nil, nil, nil, &fs[0], uint32(len(fs)))
		lb := windows.UTF16ToString(label)
		used := float64(total - totalFree)
		disks = append(disks, M{"mountpoint": root[:2], "label": lb, "name": lb, "model": nil, "fileSystem": windows.UTF16ToString(fs),
			"removable": t == windows.DRIVE_REMOVABLE, "total": float64(total), "used": used, "free": float64(totalFree),
			"usagePercent": r1(used / float64(total) * 100)})
	}
	sd := os.Getenv("SystemDrive")
	if sd == "" {
		sd = "C:"
	}
	return M{"timestamp": now, "hostname": s.host, "systemDrive": sd + `\`, "disks": disks}
}

func (s *winStats) windowsSection(st *staticInfo, now int64) M {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	cur := map[string]string{}
	var ubr uint64
	if err == nil {
		for _, n := range []string{"EditionID", "DisplayVersion", "ReleaseId", "CurrentBuild", "BuildLabEx", "InstallationType", "ProductName"} {
			cur[n] = regStr(k, n)
		}
		ubr, _, _ = k.GetIntegerValue("UBR")
		k.Close()
	}
	caption := or(st.OS.Caption, cur["ProductName"])
	build := or(cur["CurrentBuild"], st.OS.Build)
	var install any
	if st.OS.InstallDate > 0 {
		install = st.OS.InstallDate
	}
	updates := []any{}
	var last int64
	for _, u := range st.Updates {
		var dm any
		if u.DateMs > 0 {
			dm = u.DateMs
			if u.DateMs > last {
				last = u.DateMs
			}
		}
		updates = append(updates, M{"kb": u.KB, "title": u.Title, "dateMs": dm, "result": 2, "operation": "Install", "kind": "quality",
			"source": "wmi.qfe", "updateId": nil, "moreInfoUrl": nil})
	}
	var lastAny any
	if last > 0 {
		lastAny = last
	}
	return M{"timestamp": now, "hostname": s.host,
		"os": M{"name": caption, "productName": strings.TrimPrefix(caption, "Microsoft "), "edition": cur["EditionID"],
			"displayVersion": cur["DisplayVersion"], "releaseId": cur["ReleaseId"], "version": st.OS.Version, "build": build, "ubr": ubr,
			"buildFull": fmt.Sprintf("%s.%d", build, ubr), "buildLab": cur["BuildLabEx"], "installationType": cur["InstallationType"],
			"architecture": st.OS.Architecture, "systemType": or(st.OS.SystemType, "x64-based PC"), "experiencePack": st.OS.ExperiencePack,
			"installDate": install},
		"update": M{"lastUpdate": lastAny, "historyTotal": len(updates), "historyTotalRaw": len(updates), "historySource": "wmi.qfe",
			"completedTotal": len(updates), "counts": M{"feature": 0, "quality": len(updates), "driver": 0, "defender": 0, "other": 0, "store": 0},
			"updates": updates, "eventHistory": []any{}, "hotfixes": []any{}}}
}

var _ = net.IPv4len
