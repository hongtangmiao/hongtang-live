'use strict';
const $ = s => document.querySelector(s);
const app = $('#app');
function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'value') el.value = v;
    else if (k === 'checked') el.checked = !!v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of kids.flat()) { if (c == null || c === false) continue; el.append(c instanceof Node ? c : document.createTextNode(String(c))); }
  return el;
}
let tt;
function toast(m, err) { const t = $('#toast'); t.textContent = m; t.className = 'show' + (err ? ' err' : ''); clearTimeout(tt); tt = setTimeout(() => t.className = '', err ? 5000 : 2000); }
async function api(path, body) {
  const opt = body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-HL-Agent': '1' }, body: JSON.stringify(body) };
  const r = await fetch(path, opt);
  let d = {}; try { d = await r.json(); } catch (e) {}
  if (!r.ok || d.ok === false) throw new Error(d.error || ('HTTP ' + r.status));
  return d;
}
const t2 = s => { if (!s || s.startsWith('0001')) return '—'; const d = new Date(s); const p = n => String(n).padStart(2, '0'); return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`; };
const ago = s => { if (!s || s.startsWith('0001')) return '从没'; const x = Math.round((Date.now() - new Date(s)) / 1000); return x < 60 ? `${x} 秒前` : x < 3600 ? `${Math.floor(x / 60)} 分钟前` : t2(s); };
const KIND = { windows: 'Windows 电脑', linux: 'Linux 电脑' };
const PART = { activity: '在用什么', music: '正在听', stats: '系统资源', aw: '每日 ActivityWatch' };

function pairWin(S) {
  const link = h('input', { type: 'text', placeholder: 'hlpair://pair?s=…&c=…' });
  const server = h('input', { type: 'text', placeholder: 'https://example.com' });
  const code = h('input', { type: 'text', placeholder: 'XXXX-XXXX' });
  const name = h('input', { type: 'text', placeholder: '不填就用电脑名' });
  const go = async () => {
    try { await api('/api/pair', { link: link.value, server: server.value, code: code.value, name: name.value }); toast('配对成功'); load(); }
    catch (e) { toast(e.message, true); }
  };
  return h('section', { class: 'win' },
    h('h2', { text: S.need_pair ? '需要重新配对' : '配对' }),
    S.need_pair ? h('p', { class: 'bad', text: '服务器不认这台电脑了（被停用或删除）。在网站设置页重新生成配对码。' }) : null,
    h('p', { class: 'hint', text: '在网站的设置页 →「设备」→「添加设备」生成配对码，把配对链接粘贴到这里（或者分别填服务器地址和配对码）。' }),
    h('label', { text: '配对链接' }), link,
    h('div', { class: 'grid2' }, h('div', {}, h('label', { text: '或者：服务器地址' }), server), h('div', {}, h('label', { text: '配对码' }), code)),
    h('label', { text: '这台电脑在网站上叫什么' }), name,
    h('div', {}, h('button', { class: 'primary', text: '配对', onclick: go })));
}

function statusWin(S) {
  const r = S.results || {};
  // 503 = 服务器还没接管 daylog（只读），不是这边的错：黄色
  const parts = Object.entries(r).map(([k, v]) => h('span', { class: v.ok ? 'ok' : v.status === 503 ? 'gold' : 'bad', text: `${PART[k] || k}${v.ok ? '✓' : v.status === 503 ? '（服务器只读）' : '✗ ' + (v.error || '')}　` }));
  const paused = S.paused_until && !S.paused_until.startsWith('0001') && new Date(S.paused_until) > new Date();
  return h('section', { class: 'win' },
    h('h2', { text: '状态' }),
    h('div', { class: 'kv' },
      h('span', { text: '服务器' }), h('span', {}, S.server, S.server_name ? `（${S.server_name}）` : ''),
      h('span', { text: '这台电脑' }), h('span', { text: `${S.device.name}（${KIND[S.device.kind] || S.device.kind}，host=${S.device.host}）` }),
      h('span', { text: '连接' }), S.online ? h('span', { class: 'ok', text: '正常' }) : h('span', { class: 'bad', text: '连不上：' + (S.last_err || '') }),
      h('span', { text: '上次上报' }), h('span', { text: ago(S.last_report) }),
      h('span', { text: '这一轮' }), h('span', {}, parts.length ? parts : '—'),
      h('span', { text: '网站上显示' }), h('span', { class: /没写进去.*daylog 写入/.test(S.activity || '') ? 'gold' : '', text: S.activity || '—' }),
      h('span', { text: '正在听' }), h('span', { text: S.music || '—' }),
      h('span', { text: '系统资源' }), S.stats_err ? h('span', { class: 'bad', text: S.stats_err }) : h('span', { text: S.stats_at && !S.stats_at.startsWith('0001') ? ago(S.stats_at) : '—' }),
      h('span', { text: 'ActivityWatch' }), h('span', { class: /503/.test(S.aw_msg || '') ? 'gold' : '', text: S.aw_msg ? `${S.aw_msg}${/503/.test(S.aw_msg) ? '（服务器还没接管，之后会自动补传）' : ''}（${ago(S.aw_at)}）` : '—' }),
      h('span', { text: '允许上报' }), h('span', { text: (S.device.scopes || []).map(x => PART[x] || x).join('、') || '（无）' })),
    !S.writer && S.paired ? h('p', { class: 'gold', text: '服务器还没接管 daylog：「在用什么」暂时写不进去（返回 503），系统资源照常。' }) : null,
    h('div', {},
      paused ? h('span', { class: 'gold', text: `已暂停到 ${t2(S.paused_until)}　` }) : null,
      paused ? h('button', { text: '继续上报', onclick: () => pause(0) })
        : [h('button', { text: '暂停 30 分钟', onclick: () => pause(30) }), h('button', { text: '暂停 2 小时', onclick: () => pause(120) }),
           h('button', { text: '暂停到明早', onclick: () => pause(-1) })],
      h('button', { text: '立即上传今天的 ActivityWatch', onclick: async () => { try { await api('/api/aw-now', {}); toast('已上传'); load(); } catch (e) { toast(e.message, true); } } }),
      h('button', { class: 'danger', text: '解除配对', onclick: async () => { if (!confirm('解除配对后这台电脑就不再上报了，要用得重新配对。确定？')) return; await api('/api/unpair', {}); load(); } })));
}
async function pause(m) { try { await api('/api/pause', { minutes: m }); load(); } catch (e) { toast(e.message, true); } }

function settingsWin(D) {
  const r = D.report;
  const c = {};
  const chk = (k, label) => { c[k] = h('input', { type: 'checkbox', checked: r[k] }); return h('label', { class: 'chk' }, c[k], label); };
  const iv = h('input', { type: 'number', min: 0, max: 600, value: r.interval_sec || 0 });
  const idle = h('input', { type: 'number', min: 0, max: 240, value: r.idle_min });
  const aw = h('input', { type: 'text', value: D.aw_url });
  const auto = h('input', { type: 'checkbox', checked: D.autostart });
  return h('section', { class: 'win' },
    h('h2', { text: '设置' }),
    h('div', {}, chk('activity', '在用什么'), chk('music', '正在听'), chk('stats', '系统资源'), chk('aw', '每日 ActivityWatch')),
    h('p', { class: 'hint', text: '这里关掉的本机就不采集；网站设置页里没勾的，这里开着也不会报。' }),
    h('div', { class: 'grid2' },
      h('div', {}, h('label', { text: '上报间隔（秒，0 = 跟服务器，默认 30）' }), iv),
      h('div', {}, h('label', { text: '多久没碰键鼠算离开（分钟，0 = 不判断）' }), idle)),
    h('p', { class: 'hint', text: '正在放音乐 / 视频的时候不算离开。锁屏马上算离开。' }),
    h('label', { text: 'ActivityWatch 地址' }), aw,
    h('label', { class: 'chk' }, auto, '开机自动启动'),
    h('div', {}, h('button', { class: 'primary', text: '保存', onclick: async () => {
      const report = { activity: c.activity.checked, music: c.music.checked, stats: c.stats.checked, aw: c.aw.checked,
        interval_sec: Number(iv.value) || 0, idle_min: Number(idle.value) || 0 };
      try { await api('/api/settings', { report, aw_url: aw.value.trim(), autostart: auto.checked }); toast('已保存'); load(); }
      catch (e) { toast(e.message, true); }
    } })));
}

async function logWin() {
  const pre = h('pre', { text: '…' });
  const win = h('section', { class: 'win' }, h('h2', { text: '日志' }), pre);
  try { const d = await api('/api/log'); pre.textContent = d.log.slice(-80).join('\n') || '（还没有）'; win.append(h('p', { class: 'hint', text: '完整日志：' + d.dir })); } catch (e) { pre.textContent = e.message; }
  return win;
}

async function load() {
  try {
    const D = await api('/api/status');
    const S = D.status;
    $('#ver').textContent = `hl-agent ${S.version} · ${KIND[S.kind] || S.kind}`;
    const parts = [];
    if (!S.paired || S.need_pair) parts.push(pairWin(S));
    if (S.paired) parts.push(statusWin(S));
    parts.push(settingsWin(D), await logWin());
    app.replaceChildren(...parts.filter(Boolean));
  } catch (e) { app.replaceChildren(h('p', { class: 'bad', text: '读不到状态：' + e.message })); }
}
load();
setInterval(() => { if (!document.hidden && !document.activeElement?.matches('input')) load(); }, 10000);
