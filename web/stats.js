/* web/stats.js — 系统资源统计面板（原生 ja / zh 双语） */
(function () {
  'use strict';

  const MAX_PTS = 60;
  const MAIN_ENG = ['3D', 'Copy', 'Video Encode', 'Video Decode'];
  const COL = {
    cpu: ['rgba(130,190,255,.28)', '#60a5fa'],
    mem: ['rgba(196,150,255,.26)', '#a78bfa'],
    gpu: ['rgba(130,210,175,.26)', '#34d399'],
    net: ['rgba(251,211,222,.32)', '#d97a9c'],
  };

  // 每项为 [ja, zh]
  const DICT = {
    devices: ['デバイスを選択', '选择设备'],
    errConn: ['サーバーに接続できません', '无法连接到服务器'],
    errFetch: ['取得に失敗しました', '获取失败'],
    errPrefix: ['取得エラー: ', '获取出错：'],
    sec120: ['120 秒間の使用率 (%)', '120 秒占用率 (%)'],
    sec120L: ['120 秒', '120 秒'],
    logical: ['論理プロセッサ', '逻辑处理器'],
    overall: ['全体的な使用率', '总体占用率'],
    usage: ['使用率', '占用率'], speed: ['速度', '速度'],
    procs: ['プロセス数', '进程数'], threads: ['スレッド数', '线程数'],
    handles: ['ハンドル数', '句柄数'], uptime: ['稼働時間', '运行时间'],
    baseSpeed: ['基本速度', '基准速度'], sockets: ['ソケット', '插槽'],
    cores: ['コア', '内核'], logicalCnt: ['論理プロセッサ数', '逻辑处理器数'],
    virt: ['仮想化', '虚拟化'], on: ['有効', '已启用'], off: ['無効', '已禁用'],
    l1: ['L1 キャッシュ', 'L1 缓存'], l2: ['L2 キャッシュ', 'L2 缓存'], l3: ['L3 キャッシュ', 'L3 缓存'],
    memTitle: ['メモリ', '内存'], memUsage: ['メモリ使用量', '内存使用量'],
    memComp: ['メモリ構成', '内存构成'], memUsed: ['使用中', '使用中'],
    memCompS: ['圧縮', '压缩'], memAvail: ['利用可能', '可用'],
    memCommit: ['コミット済み', '已提交'], memCached: ['キャッシュ済み', '已缓存'],
    memPaged: ['ページ プール', '分页池'], memPagefile: ['ページファイル', '页面文件'],
    memNonPaged: ['非ページ プール', '非分页池'],
    memSlots: ['スロットの使用', '插槽占用'], memForm: ['フォーム ファクター', '外形规格'],
    memHwRes: ['ハードウェア予約済み', '硬件保留'],
    gpuMain: ['主要エンジン', '主要引擎'], gpuAll: ['全エンジン', '全部引擎'],
    gpuEng: ['エンジン別使用率 (%)', '各引擎占用率 (%)'],
    gpuDed: ['専用 GPU メモリ', '专用 GPU 内存'], gpuShr: ['共有 GPU メモリ', '共享 GPU 内存'],
    temp: ['温度', '温度'], drvVer: ['ドライバー', '驱动版本'],
    drvDate: ['日付', '驱动日期'], dxVer: ['DirectX', 'DirectX'],
    diskTitle: ['ディスク', '磁盘'], diskFree: ['空き', '可用'],
    netTitle: ['ネットワーク', '网络'], throughput: ['スループット', '吞吐量'],
    recv: ['受信', '接收'], send: ['送信', '发送'],
    adapter: ['アダプター', '适配器'], linkSpeed: ['リンク速度', '链接速度'],
    edition: ['エディション', '版本'], version: ['バージョン', '版本号'],
    build: ['OS ビルド', 'OS 内部版本'], installDate: ['インストール日', '安装日期'],
    sysType: ['システムの種類', '系统类型'], expPack: ['エクスペリエンス パック', '体验包'],
    updTitle: ['更新の履歴', '更新历史'],
    updQuality: ['品質更新プログラム', '质量更新'], updFeature: ['機能更新プログラム', '功能更新'],
    updDriver: ['ドライバー更新プログラム', '驱动程序更新'],
    updDefender: ['定義更新プログラム', '定义更新'], updOther: ['その他の更新プログラム', '其他更新'],
    cpuHint: ['クリックで論理プロセッサ別／全体を切り替え', '点击切换每线程 / 总体'],
    gpuHint: ['クリックで主要／全エンジンを切り替え', '点击切换主要 / 全部引擎'],
  };

  const isZh = () => (document.documentElement.lang || '').startsWith('zh');
  const tr = (k) => (DICT[k] ? DICT[k][isZh() ? 1 : 0] : k);
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  const clamp = (v, lo, hi) => Math.min(Math.max(Number(v) || 0, lo), hi);
  const push = (arr, v) => { arr.push(v); if (arr.length > MAX_PTS) arr.shift(); };

  function fmtBytes(b) {
    const n = Number(b) || 0;
    if (n >= 1099511627776) return (n / 1099511627776).toFixed(2) + ' TB';
    if (n >= 1073741824) return (n / 1073741824).toFixed(1) + ' GB';
    if (n >= 1048576) return (n / 1048576).toFixed(0) + ' MB';
    if (n >= 1024) return (n / 1024).toFixed(0) + ' KB';
    return Math.round(n) + ' B';
  }

  function fmtBits(bps) {
    const n = Math.max(0, Number(bps) || 0);
    if (n >= 1e9) return (n / 1e9).toFixed(2) + ' Gbps';
    if (n >= 1e6) return (n / 1e6).toFixed(1) + ' Mbps';
    if (n >= 1e3) return (n / 1e3).toFixed(0) + ' Kbps';
    return Math.round(n) + ' bps';
  }

  function fmtScale(bps) {
    return bps >= 1e9 ? (bps / 1e9) + ' Gbps' : bps >= 1e6 ? (bps / 1e6) + ' Mbps' : (bps / 1e3) + ' Kbps';
  }

  function fmtDate(ms) {
    if (!ms) return '';
    const d = new Date(Number(ms));
    if (isNaN(d.getTime())) return String(ms);
    return `${d.getFullYear()}/${String(d.getMonth() + 1).padStart(2, '0')}/${String(d.getDate()).padStart(2, '0')}`;
  }

  function areaSvg(pts, maxVal, pair) {
    if (!pts || !pts.length) return '<svg viewBox="0 0 600 180" preserveAspectRatio="none" aria-hidden="true"></svg>';
    if (pts.length === 1) pts = [pts[0], pts[0]];
    const max = maxVal || 100, step = 600 / (MAX_PTS - 1), off = MAX_PTS - pts.length;
    const xOf = (i) => ((off + i) * step).toFixed(2);
    const yOf = (v) => (180 - (clamp(v, 0, max) / max) * 180).toFixed(2);
    const line = pts.map((v, i) => `${xOf(i)},${yOf(v)}`).join(' L');
    return `<svg viewBox="0 0 600 180" preserveAspectRatio="none" aria-hidden="true">` +
      `<path d="M${xOf(0)},180 L${line} L${xOf(pts.length - 1)},180 Z" fill="${pair[0]}"/>` +
      `<path d="M${line}" fill="none" stroke="${pair[1]}" stroke-width="1.5" stroke-linejoin="round" vector-effect="non-scaling-stroke"/></svg>`;
  }

  function dualNetSvg(recv, send, scale) {
    const step = 600 / (MAX_PTS - 1);
    const yOf = (v) => (180 - (clamp(v, 0, scale) / (scale || 1)) * 180).toFixed(2);
    const ser = (pts, dashed) => {
      if (!pts || pts.length < 2) return '';
      const off = MAX_PTS - pts.length;
      const xOf = (i) => ((off + i) * step).toFixed(2);
      const line = pts.map((v, i) => `${xOf(i)},${yOf(v)}`).join(' L');
      return `<path d="M${xOf(0)},180 L${line} L${xOf(pts.length - 1)},180 Z" fill="${COL.net[0]}"/>` +
        `<path d="M${line}" fill="none" stroke="${COL.net[1]}" stroke-width="1.5"${dashed ? ' stroke-dasharray="4 3"' : ''} stroke-linejoin="round" vector-effect="non-scaling-stroke"/>`;
    };
    return `<svg viewBox="0 0 600 180" preserveAspectRatio="none" aria-hidden="true">${ser(recv, false)}${ser(send, true)}</svg>`;
  }

  const pickScale = (peak) => [1e5, 1e6, 1e7, 1e8, 1e9, 1e10].find((s) => s >= peak) || 1e11;

  /* ---- state ---- */
  let devices = Array.isArray(window.HL_STATS_DEVICES) ? window.HL_STATS_DEVICES.slice() : [];
  let host = (devices[0] && devices[0].ip) || 'pc';
  let seq = 0, cpuPerCore = window.innerWidth > 600, gpuAllEng = false;
  const openHw = new Set(), openUpd = new Set();

  const S = {};
  function resetData() {
    Object.assign(S, {
      cpu: null, mem: null, gpu: null, disk: null, net: null, win: null,
      err: {}, sampledAt: {}, anyOk: null,
      cpuPts: [], corePts: [], memPts: [],
      gpuDed: [], gpuShr: [], gpuEng: {}, gpuTs: 0,
      netPts: {}, netPrev: {},
    });
  }
  resetData();

  const isLive = () => {
    const p = document.getElementById('panel-stats');
    return !!(p && p.classList.contains('live') && !document.hidden);
  };

  function updateHeader() {
    const dev = devices.find((d) => d.ip === host) || { name: host, ip: host, os: 'windows' };
    const hn = document.getElementById('hostname');
    const os = document.getElementById('osinfo');
    const cur = document.getElementById('pf-current');
    if (cur) cur.textContent = dev.name || dev.ip;
    if (hn) {
      const osName = (S.cpu && S.cpu.osName) || (S.win && S.win.os && S.win.os.productName) || dev.name || '—';
      hn.textContent = osName.replace(/^Microsoft\s+/, '');
    }
    if (os) os.textContent = (S.cpu && S.cpu.hostname) || dev.name || dev.ip || '—';
  }

  function renderDeviceMenu() {
    const menu = document.getElementById('pf-menu');
    if (!menu) return;
    menu.innerHTML = `<div class="pf-menu-hdr">${esc(tr('devices'))}</div>` +
      devices.map((d) => {
        const ico = d.os === 'linux' ? 'ti-terminal-2' : 'ti-brand-windows';
        const dot = d.online === false ? 'off' : 'on';
        return `<button type="button" class="pf-item" role="menuitemradio" aria-checked="${d.ip === host}" data-host="${esc(d.ip)}">` +
          `<i class="ti ti-check pf-check" aria-hidden="true"></i><i class="ti ${ico} pf-os" aria-hidden="true"></i>` +
          `<span><span class="pf-item-name">${esc(d.name || d.ip)}</span><span class="pf-item-ip">${esc(d.ip)}</span></span>` +
          `<span class="pf-dot ${dot}" aria-hidden="true"></span></button>`;
      }).join('');
  }

  async function fetchDevices() {
    try {
      const r = await fetch('/api/stats/devices', { credentials: 'same-origin', cache: 'no-store' });
      const j = await r.json();
      if (j && j.success && Array.isArray(j.data) && j.data.length) {
        devices = j.data;
        if (!devices.some((d) => d.ip === host)) host = devices[0].ip;
        updateHeader();
        renderDeviceMenu();
      }
    } catch (e) { /* keep current list */ }
  }

  async function fetchSec(sec, hist) {
    const mySeq = seq;
    try {
      const q = `/api/stats/${sec}?host=${encodeURIComponent(host)}${hist ? `&history=${hist}` : ''}`;
      const j = await (await fetch(q, { credentials: 'same-origin', cache: 'no-store' })).json();
      if (mySeq !== seq) return null;
      if (!j || !j.success) { S.err[sec] = (j && j.error) || tr('errFetch'); return null; }
      S.err[sec] = '';
      const at = j.sampledAt || 0, fresh = at !== S.sampledAt[sec];
      S.sampledAt[sec] = at;
      return { data: j.data, samples: Array.isArray(j.samples) ? j.samples : null, sampledAt: at, fresh };
    } catch (e) {
      if (mySeq === seq) S.err[sec] = tr('errConn');
      return null;
    }
  }

  function pushCpu(d) {
    push(S.cpuPts, clamp(d.usagePercent, 0, 100));
    (d.logicalProcessorUsage || []).forEach((c, i) => push(S.corePts[i] || (S.corePts[i] = []), clamp(c.usagePercent, 0, 100)));
  }

  function pushGpu(d) {
    const m = d.memory || {};
    push(S.gpuDed, Number(m.dedicatedUsed) || 0);
    push(S.gpuShr, Number(m.sharedUsed) || 0);
    (d.engines || []).forEach((e) => push(S.gpuEng[e.type] || (S.gpuEng[e.type] = []), clamp(e.usagePercent, 0, 100)));
  }

  function pushNet(d, at) {
    const ts = d.timestamp ? d.timestamp / 1000 : at;
    (d.adapters || []).forEach((a) => {
      const ser = S.netPts[a.name] || (S.netPts[a.name] = { recv: [], send: [] });
      const prev = S.netPrev[a.name];
      let rx = (Number(a.recvBytesPerSec) || 0) * 8, tx = (Number(a.sentBytesPerSec) || 0) * 8;
      if (prev && a.bytesReceived != null && ts > prev.t && ts - prev.t <= 60) {
        rx = Math.max(0, (a.bytesReceived - prev.r) / (ts - prev.t)) * 8;
        tx = Math.max(0, (a.bytesSent - prev.s) / (ts - prev.t)) * 8;
      }
      if (a.bytesReceived != null) S.netPrev[a.name] = { r: a.bytesReceived, s: a.bytesSent, t: ts };
      push(ser.recv, rx); push(ser.send, tx);
    });
  }

  async function pollFast(needHist) {
    const mySeq = seq, h = needHist ? MAX_PTS : 0;
    const [cpu, mem, gpu, net] = await Promise.all([
      fetchSec('cpu', h), fetchSec('memory', h), fetchSec('gpu', h), fetchSec('network', h),
    ]);
    if (mySeq !== seq) return;
    if (cpu && cpu.data) {
      S.cpu = cpu.data; S.anyOk = true;
      if (cpu.samples && cpu.samples.length) { S.cpuPts = []; S.corePts = []; cpu.samples.forEach((x) => x.data && pushCpu(x.data)); } else if (cpu.fresh || !S.cpuPts.length) pushCpu(cpu.data);
      const tsEl = document.getElementById('ts');
      if (tsEl && cpu.sampledAt) tsEl.textContent = new Date(cpu.sampledAt * 1000).toLocaleTimeString('ja-JP', { hour12: false });
    } else if (S.anyOk == null) S.anyOk = false;
    if (mem && mem.data) {
      S.mem = mem.data; S.anyOk = true;
      if (mem.samples && mem.samples.length) { S.memPts = []; mem.samples.forEach((x) => x.data && push(S.memPts, Number(x.data.used) || 0)); } else if (mem.fresh || !S.memPts.length) push(S.memPts, Number(mem.data.used) || 0);
    }
    if (gpu && gpu.data) {
      S.gpu = gpu.data; S.anyOk = true;
      if (gpu.samples && gpu.samples.length) { S.gpuDed = []; S.gpuShr = []; S.gpuEng = {}; gpu.samples.forEach((x) => x.data && pushGpu(x.data)); } else if ((gpu.fresh && gpu.data.timestamp !== S.gpuTs) || !S.gpuDed.length) { S.gpuTs = gpu.data.timestamp; pushGpu(gpu.data); }
    }
    if (net && net.data) {
      S.net = net.data; S.anyOk = true;
      if (net.samples && net.samples.length) { S.netPts = {}; S.netPrev = {}; net.samples.forEach((x) => x.data && pushNet(x.data, x.sampledAt)); } else if (net.fresh || !Object.keys(S.netPts).length) pushNet(net.data, net.sampledAt);
    }
    render();
  }

  async function pollSlow() {
    const mySeq = seq;
    const [disk, win] = await Promise.all([fetchSec('disk', 0), fetchSec('windows', 0)]);
    if (mySeq !== seq) return;
    if (disk && disk.data) { S.disk = disk.data; S.anyOk = true; }
    if (win && win.data) { S.win = win.data; S.anyOk = true; }
    render();
  }

  const dlRows = (rows) => rows.filter((r) => r && r[1] != null && r[1] !== '')
    .map(([k, v]) => `<dt>${esc(k)}:</dt><dd>${esc(v)}</dd>`).join('');
  const cell = (l, v, sub, cls) => `<div class="stat-cell${cls ? ' ' + cls : ''}"><div class="cl">${esc(l)}</div>` +
    `<div class="cv">${esc(v)}${sub ? `<span class="cs">${esc(sub)}</span>` : ''}</div></div>`;

  function renderCpu() {
    const c = S.cpu, tx = c.text || {}, cores = c.logicalProcessorUsage || [];
    const showCores = cpuPerCore && cores.length > 0;
    const graph = showCores
      ? `<div class="cpu-cores">${cores.map((p, i) =>
          `<div class="core">${areaSvg(S.corePts[i], 100, COL.cpu)}<span class="core-idx">${i}</span>` +
          `<span class="core-now">${Math.round(clamp(p.usagePercent, 0, 100))}%</span></div>`
        ).join('')}</div>`
      : `<div class="big-chart">${areaSvg(S.cpuPts, 100, COL.cpu)}</div>` +
        `<div class="chart-foot"><span>${esc(tr('sec120L'))}</span><span>0</span></div>`;
    const hw = dlRows([
      [tr('baseSpeed'), tx.baseSpeed], [tr('sockets'), tx.sockets || c.sockets], [tr('cores'), c.cores],
      [tr('logicalCnt'), c.logicalProcessors], [tr('virt'), c.virtualization == null ? '' : tr(c.virtualization ? 'on' : 'off')],
      [tr('l1'), tx.l1Cache], [tr('l2'), tx.l2Cache], [tr('l3'), tx.l3Cache],
    ]);
    return `<div class="section"><div class="sec-hdr"><div class="sec-title"><i class="ti ti-cpu" aria-hidden="true"></i> CPU</div>` +
      `<div class="sec-badge">${esc(c.name || '')}</div></div>` +
      `<div class="cpu-graph" data-toggle="cpu" title="${esc(tr('cpuHint'))}">` +
      `<div class="graph-hdr"><span>${esc(tr('sec120'))}</span><span>${esc(tr(showCores ? 'logical' : 'overall'))}</span></div>${graph}</div>` +
      `<div class="cpu-detail"><div class="cpu-stats" data-tap="cpu-hw">` +
      cell(tr('usage'), `${Math.round(clamp(c.usagePercent, 0, 100))}%`, '', 'sp3') +
      cell(tr('speed'), tx.speed || '—', '', 'sp3') +
      cell(tr('procs'), tx.processes || '—', '', 'sp2') +
      cell(tr('threads'), tx.threads || '—', '', 'sp2') +
      cell(tr('handles'), tx.handles || '—', '', 'sp2') +
      cell(tr('uptime'), tx.uptime || '—', '', 'sp6') +
      `</div><dl class="cpu-hw${openHw.has('cpu-hw') ? ' open' : ''}">${hw}</dl></div></div>`;
  }

  function renderMem() {
    const m = S.mem, tx = m.text || {}, total = Number(m.total) || Number(m.installed) || 1;
    const used = Number(m.used) || 0, pct = clamp((used / total) * 100, 0, 100);
    const modType = (m.modules && m.modules[0] && m.modules[0].type) || '';
    const baseMem = tx.installed || fmtBytes(m.installed || total);
    const badge = modType && !baseMem.includes(modType) ? `${baseMem} ${modType}` : baseMem;
    const compSub = m.compressed > 0 && tx.compressed ? `(${tx.compressed})` : '';
    const hw = dlRows([
      [tr('speed'), tx.speed], [tr('memSlots'), tx.slots && tx.slots !== '0/0' ? tx.slots : ''],
      [tr('memForm'), tx.formFactor && tx.formFactor !== '—' ? tx.formFactor : ''], [tr('memHwRes'), tx.hardwareReserved],
    ]);
    return `<div class="section"><div class="sec-hdr"><div class="sec-title"><i class="ti ti-server" aria-hidden="true"></i> ${esc(tr('memTitle'))}</div>` +
      `<div class="sec-badge">${esc(badge)}</div></div>` +
      `<div class="graph-hdr"><span>${esc(tr('memUsage'))}</span><span>${esc(tx.total || fmtBytes(total))}</span></div>` +
      `<div class="big-chart">${areaSvg(S.memPts, total, COL.mem)}</div>` +
      `<div class="chart-foot"><span>${esc(tr('sec120L'))}</span><span>0</span></div>` +
      `<div class="graph-hdr" style="margin-top:14px"><span>${esc(tr('memComp'))}</span><span></span></div>` +
      `<div class="mem-comp"><div class="seg used" style="flex-grow:${pct.toFixed(1)}"></div><div class="seg free" style="flex-grow:${(100 - pct).toFixed(1)}"></div></div>` +
      `<div class="mem-detail"><div class="mem-stats" data-tap="mem-hw">` +
      cell(`${tr('memUsed')}${compSub ? ` (${tr('memCompS')})` : ''}`, tx.used || fmtBytes(used), compSub) +
      cell(tr('memAvail'), tx.available || fmtBytes(m.available)) +
      cell(tr('memCommit'), tx.committed || '—') +
      cell(tr('memCached'), tx.cached || fmtBytes(m.cached)) +
      cell(tr(m.pagedPool ? 'memPaged' : 'memPagefile'), m.pagedPool ? (tx.pagedPool || fmtBytes(m.pagedPool)) : (tx.pageFile || '—')) +
      cell(tr('memNonPaged'), tx.nonPagedPool || fmtBytes(m.nonPagedPool)) +
      `</div><dl class="mem-hw${openHw.has('mem-hw') ? ' open' : ''}">${hw}</dl></div></div>`;
  }

  function renderGpu() {
    const g = S.gpu, m = g.memory || {}, tx = g.text || {}, allEng = g.engines || [];
    const shown = gpuAllEng ? allEng : allEng.filter((e) => MAIN_ENG.includes(e.type));
    const engList = shown.length ? shown : allEng.slice(0, 4);
    const graphs = engList.map((e) =>
      `<div><div class="graph-hdr"><span>${esc(e.label || e.type)}</span><span>${Math.round(clamp(e.usagePercent, 0, 100))}%</span></div>` +
      `<div class="big-chart g-eng">${areaSvg(S.gpuEng[e.type], 100, COL.gpu)}</div></div>`
    ).join('') +
      `<div class="g-full"><div class="graph-hdr"><span>${esc(tr('gpuDed'))}</span><span>${esc(tx.dedicated || fmtBytes(m.dedicatedLimit))}</span></div>` +
      `<div class="big-chart g-mem">${areaSvg(S.gpuDed, m.dedicatedLimit || 1, COL.gpu)}</div></div>`;
    const hw = dlRows([
      [tr('drvVer'), g.driver && g.driver.version], [tr('drvDate'), g.driver && g.driver.date],
      [tr('dxVer'), g.directX && g.directX.version ? `${g.directX.version} (FL ${g.directX.featureLevel || ''})` : ''],
    ]);
    return `<div class="section"><div class="sec-hdr"><div class="sec-title"><i class="ti ti-device-desktop" aria-hidden="true"></i> GPU</div>` +
      `<div class="sec-badge">${esc(g.name || '')}</div></div>` +
      `<div class="gpu-graph" data-toggle="gpu" title="${esc(tr('gpuHint'))}">` +
      `<div class="graph-hdr"><span>${esc(tr('gpuEng'))}</span><span>${esc(tr(gpuAllEng ? 'gpuAll' : 'gpuMain'))}</span></div>` +
      `<div class="gpu-graphs">${graphs}</div></div>` +
      `<div class="gpu-detail"><div class="gpu-nums" data-tap="gpu-hw">` +
      cell(tr('usage'), `${Math.round(clamp(g.usagePercent, 0, 100))}%`) +
      cell(tr('temp'), g.temperatureC != null ? `${Math.round(g.temperatureC)} °C` : '—') +
      cell(tr('gpuDed'), `${fmtBytes(m.dedicatedUsed)} / ${fmtBytes(m.dedicatedLimit)}`) +
      cell(tr('gpuShr'), `${fmtBytes(m.sharedUsed)} / ${fmtBytes(m.sharedLimit)}`) +
      `</div><dl class="gpu-hw${openHw.has('gpu-hw') ? ' open' : ''}">${hw}</dl></div></div>`;
  }

  function renderDisk() {
    const sys = String(S.disk.systemDrive || '').replace(/\\+$/, '').toUpperCase();
    const items = (S.disk.disks || []).map((d) => {
      const mp = d.mountpoint || '', isSys = sys && mp.toUpperCase() === sys;
      const pct = clamp(d.usagePercent != null ? d.usagePercent : (d.total ? (d.used / d.total) * 100 : 0), 0, 100);
      const low = pct >= 90, title = d.label && d.label !== mp ? `${d.label} (${mp})` : (mp || d.name || '—');
      return `<div class="disk-item"><img class="disk-ico" src="img/fluent/${isSys ? 'drive-c' : 'drive'}.png" alt="" loading="lazy">` +
        `<div class="disk-name" title="${esc(title)}">${esc(title)}</div>` +
        `<div class="disk-track"><div class="disk-fill ${low ? 'low' : 'ok'}" style="width:${pct.toFixed(1)}%"></div></div>` +
        `<div class="disk-free${low ? ' low' : ''}">${esc(`${fmtBytes(d.free)} ${tr('diskFree')} / ${fmtBytes(d.total)}`)}</div></div>`;
    }).join('');
    return `<div class="section"><div class="sec-hdr"><div class="sec-title"><i class="ti ti-database" aria-hidden="true"></i> ${esc(tr('diskTitle'))}</div></div>` +
      `<div class="disk-grid">${items}</div></div>`;
  }

  function renderNet(wide) {
    const list = S.net.adapters || [], a = list.find((x) => x.isDefault) || list[0];
    if (!a) return '';
    const ser = S.netPts[a.name] || { recv: [], send: [] };
    const scale = pickScale(Math.max(0, ...ser.recv, ...ser.send));
    const rx = ser.recv.length ? ser.recv[ser.recv.length - 1] : (Number(a.recvBytesPerSec) || 0) * 8;
    const tx = ser.send.length ? ser.send[ser.send.length - 1] : (Number(a.sentBytesPerSec) || 0) * 8;
    const hw = dlRows([[tr('adapter'), a.name], ['IPv4', a.ipv4], ['IPv6', a.ipv6], [tr('linkSpeed'), a.linkSpeed ? fmtScale(a.linkSpeed) : '']]);
    return `<div class="section net${wide ? ' wide' : ''}"><div class="sec-hdr"><div class="sec-title"><i class="ti ti-network" aria-hidden="true"></i> ${esc(tr('netTitle'))}</div>` +
      `<div class="sec-badge">${esc(a.description || a.name || '')}</div></div>` +
      `<div class="graph-hdr"><span>${esc(tr('throughput'))}</span><span>${esc(fmtScale(scale))}</span></div>` +
      `<div class="big-chart">${dualNetSvg(ser.recv, ser.send, scale)}</div>` +
      `<div class="chart-foot"><span>${esc(tr('sec120L'))}</span><span>0</span></div>` +
      `<div class="net-detail"><div class="net-stats" data-tap="net-hw">${cell(tr('recv'), fmtBits(rx))}${cell(tr('send'), fmtBits(tx), '', 'send')}</div>` +
      `<dl class="net-hw${openHw.has('net-hw') ? ' open' : ''}">${hw}</dl></div></div>`;
  }

  function renderWin() {
    const o = (S.win && S.win.os) || {}, u = (S.win && S.win.update) || {};
    const isLinux = /linux|debian|ubuntu|alpine/i.test(o.productName || o.name || '');
    const hw = dlRows([
      [tr('edition'), o.edition], [tr('version'), o.displayVersion || o.version],
      [tr('installDate'), fmtDate(o.installDate)], [tr('build'), o.buildFull || o.build],
      [tr('sysType'), o.systemType], [tr('expPack'), o.experiencePack],
    ]);
    let updHtml = '';
    const updates = Array.isArray(u.updates) ? u.updates : (Array.isArray(u.hotfixes) ? u.hotfixes : []);
    if (!isLinux && updates.length) {
      const catKey = { quality: 'updQuality', feature: 'updFeature', driver: 'updDriver', defender: 'updDefender', other: 'updOther' };
      const groups = {};
      updates.forEach((it) => { const k = catKey[it.category] ? it.category : 'other'; (groups[k] || (groups[k] = [])).push(it); });
      updHtml = `<div class="graph-hdr" style="margin-top:16px"><span>${esc(tr('updTitle'))}</span><span>${updates.length}</span></div>` +
        Object.keys(groups).map((k) => {
          const open = openUpd.has(k);
          const list = groups[k].slice(0, 20).map((it) =>
            `<div class="upd-item"><span class="upd-title" title="${esc(it.title || it.kb || '')}">${esc(it.title || it.kb || '—')}</span>` +
            `<span class="upd-date">${esc(it.date || fmtDate(it.dateMs) || '')}</span></div>`
          ).join('');
          return `<div class="upd-grp${open ? ' open' : ''}" data-upd="${esc(k)}"><div class="upd-grp-hdr"><span>${esc(tr(catKey[k]))}</span>` +
            `<span class="cnt">(${groups[k].length})</span><i class="ti ti-chevron-down" aria-hidden="true"></i></div>` +
            `${open ? `<div class="upd-grp-body"><div class="upd-list">${list}</div></div>` : ''}</div>`;
        }).join('');
    }
    return `<div class="section win"><div class="sec-hdr"><div class="sec-title"><i class="ti ${isLinux ? 'ti-terminal-2' : 'ti-brand-windows'}" aria-hidden="true"></i> ${isLinux ? 'LINUX' : 'WINDOWS'}</div>` +
      `<div class="sec-badge">${esc((o.productName || o.name || '').replace(/^Microsoft\s+/, ''))}</div></div>` +
      `<div class="win-detail"><div class="win-stats" data-tap="win-hw">${cell(tr('version'), o.displayVersion || o.version || '—')}${cell(tr('build'), o.buildFull || o.build || '—')}</div>` +
      `<dl class="win-hw${openHw.has('win-hw') ? ' open' : ''}">${hw}</dl></div>${updHtml}</div>`;
  }

  function render() {
    const main = document.getElementById('stats-main');
    if (!main) return;
    updateHeader();
    if (!(S.cpu || S.mem || S.gpu || S.disk || S.net || S.win)) {
      if (S.anyOk === false) {
        main.innerHTML = `<div class="error"><i class="ti ti-alert-circle" aria-hidden="true"></i> ${esc(tr('errPrefix') + (S.err.cpu || tr('errConn')))}</div>`;
      }
      return;
    }
    main.classList.remove('cols-4');
    if (window.innerWidth >= 1400) {
      const cols = [[], [], []];
      if (S.cpu) cols[0].push(renderCpu());
      if (S.gpu) cols[0].push(renderGpu());
      if (S.mem) cols[1].push(renderMem());
      if (S.disk) cols[1].push(renderDisk());
      if (S.net) cols[2].push(renderNet(!S.gpu));
      if (S.win) cols[2].push(renderWin());
      main.innerHTML = cols.map((c) => `<div class="stats-col">${c.join('')}</div>`).join('');
    } else {
      const list = [];
      if (S.cpu) list.push(renderCpu());
      if (S.mem) list.push(renderMem());
      if (S.gpu) list.push(renderGpu());
      if (S.disk) list.push(renderDisk());
      if (S.net) list.push(renderNet(!S.gpu));
      if (S.win) list.push(renderWin());
      main.innerHTML = list.join('');
    }
  }

  function switchHost(nextHost) {
    if (!nextHost || nextHost === host) return;
    host = nextHost; seq += 1;
    resetData(); renderDeviceMenu(); updateHeader();
    pollFast(true); pollSlow();
  }

  let inited = false;
  function initStats() {
    if (inited) return;
    inited = true;
    const btn = document.getElementById('pf-btn'), menu = document.getElementById('pf-menu');
    const refresh = document.getElementById('stats-refresh'), main = document.getElementById('stats-main');

    if (btn && menu) {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        menu.hidden = !menu.hidden;
        btn.setAttribute('aria-expanded', String(!menu.hidden));
      });
      menu.addEventListener('click', (e) => {
        const item = e.target.closest('[data-host]');
        if (!item) return;
        menu.hidden = true; btn.setAttribute('aria-expanded', 'false');
        switchHost(item.dataset.host);
      });
      document.addEventListener('click', () => {
        if (!menu.hidden) { menu.hidden = true; btn.setAttribute('aria-expanded', 'false'); }
      });
    }
    if (refresh) refresh.addEventListener('click', () => { fetchDevices(); pollFast(false); pollSlow(); });
    if (main) {
      main.addEventListener('click', (e) => {
        const tg = e.target.closest('[data-toggle]');
        if (tg) {
          if (tg.dataset.toggle === 'cpu') cpuPerCore = !cpuPerCore;
          if (tg.dataset.toggle === 'gpu') gpuAllEng = !gpuAllEng;
          render(); return;
        }
        const tap = e.target.closest('[data-tap]');
        if (tap && window.innerWidth <= 600) {
          const k = tap.dataset.tap;
          if (openHw.has(k)) openHw.delete(k); else openHw.add(k);
          render(); return;
        }
        const ug = e.target.closest('[data-upd] > .upd-grp-hdr');
        if (ug) {
          const k = ug.parentElement.dataset.upd;
          if (openUpd.has(k)) openUpd.delete(k); else openUpd.add(k);
          render();
        }
      });
    }

    renderDeviceMenu(); updateHeader(); fetchDevices(); pollFast(true); pollSlow();
    setInterval(() => { if (isLive()) pollFast(false); }, 2000);
    setInterval(() => { if (isLive()) { fetchDevices(); pollSlow(); } }, 15000);
    document.addEventListener('visibilitychange', () => { if (isLive()) { pollFast(false); pollSlow(); } });
  }

  window.initStats = initStats;
  window.rerenderStats = function () { renderDeviceMenu(); render(); };
})();
