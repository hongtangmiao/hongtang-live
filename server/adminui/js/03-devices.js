'use strict';
/* 设备：配对 / 停用 / 权限；加载设置、启动（由 admin.js 按原顺序拆出，admin.html 按 00→03 顺序加载） */
/* ---------------- 设备：配对 / 停用 / 权限 ---------------- */
const SCOPE_LABEL = { activity: '在用什么', music: '正在听', health: '健康数据', battery: '电量', stats: '系统资源', aw: '每日 ActivityWatch' };
const SVGNS = 'http://www.w3.org/2000/svg';

function qrSvg(rows) {
  const n = rows.length, q = 4;
  const svg = document.createElementNS(SVGNS, 'svg');
  svg.setAttribute('viewBox', `0 0 ${n + q * 2} ${n + q * 2}`);
  svg.setAttribute('width', '220'); svg.setAttribute('height', '220');
  svg.setAttribute('shape-rendering', 'crispEdges');
  const bg = document.createElementNS(SVGNS, 'rect');
  bg.setAttribute('width', '100%'); bg.setAttribute('height', '100%'); bg.setAttribute('fill', '#fff');
  let d = '';
  rows.forEach((r, y) => { for (let x = 0; x < r.length; x++) if (r[x] === '1') d += `M${x + q} ${y + q}h1v1h-1z`; });
  const path = document.createElementNS(SVGNS, 'path');
  path.setAttribute('d', d); path.setAttribute('fill', '#000');
  svg.append(bg, path);
  return svg;
}

let devPoll = null;
function renderDevices(box, D) {
  clearInterval(devPoll); devPoll = null;
  const kinds = D.kinds || {};
  const kindSel = (val) => h('select', {}, Object.entries(kinds).map(([k, label]) => {
    const o = h('option', { value: k, text: label }); if (k === val) o.selected = true; return o;
  }));
  const reload = async () => { try { renderDevices(box, await api('devices')); } catch (e) { toast(e.message, true); } };
  const act = (sub, body, okMsg) => async () => {
    try { renderDevices(box, await api('devices/' + sub, body)); if (okMsg) toast(okMsg); } catch (e) { toast(e.message, true); }
  };

  /* 已配对 */
  const list = h('div', { class: 'list' });
  if (!(D.devices || []).length) list.append(h('p', { class: 'muted', text: '还没有配对过设备。' }));
  for (const d of D.devices || []) {
    const [fName, iName] = field('名字', { value: d.name });
    const [fHost, iHost] = field('daylog 里的 host', { value: d.host, hint: '时间线按它区分设备；改了以后旧记录还用旧名字' });
    const scopes = {};
    const scopeRow = h('div', { class: 'row' }, (D.scopes || []).map(s => {
      const c = h('input', { type: 'checkbox', checked: (d.scopes || []).includes(s) }); scopes[s] = c;
      return h('label', { class: 'chk' }, c, SCOPE_LABEL[s] || s);
    }));
    const bat = d.battery ? ` · 电量 ${d.battery.level}%${d.battery.charging ? '（充电中）' : ''}` : '';
    const status = d.revoked ? h('span', { class: 'bad', text: '已停用' })
      : d.online ? h('span', { class: 'ok', text: '在线' }) : h('span', { class: 'muted', text: '离线' });
    list.append(h('div', { class: 'item', style: 'grid-template-columns:1fr' },
      h('div', { class: 'row' }, h('b', { text: d.name }), h('span', { class: 'muted', text: d.kind_label || d.kind }), status),
      h('div', { class: 'hint', text: `配对于 ${fmtTime(d.created)} · 最后上报 ${fmtTime(d.last_seen)}${d.last_ip ? '（' + d.last_ip + '）' : ''}${d.app_version ? ' · ' + d.app_version : ''}${bat}` }),
      h('div', { class: 'grid' }, fName, fHost),
      h('div', {}, h('label', { text: '允许上报' }), scopeRow),
      h('div', { class: 'row' },
        h('button', { class: 'small', text: '保存', onclick: () => act('update', { id: d.id, name: iName.value, host: iHost.value,
          scopes: Object.keys(scopes).filter(k => scopes[k].checked) }, '已保存')() }),
        d.revoked
          ? h('button', { class: 'small', text: '恢复', onclick: act('restore', { id: d.id }, '已恢复') })
          : h('button', { class: 'small danger', text: '停用', onclick: () => confirm(`停用「${d.name}」？它的口令马上失效，可以再恢复。`) && act('revoke', { id: d.id }, '已停用')() }),
        h('button', { class: 'small danger', text: '删除', onclick: () => confirm(`删除「${d.name}」？要再用就得重新配对。`) && act('delete', { id: d.id }, '已删除')() }))));
    // 每个输入框都要即时读：上面的闭包在点击时才取 value
  }

  /* 添加设备 */
  const [fNew, iNew] = field('设备名字（可以不填，用设备自己报的）', { placeholder: '比如：书房电脑 / 我的手机' });
  const kSel = kindSel('');
  kSel.prepend((() => { const o = h('option', { value: '', text: '让设备自己说' }); o.selected = true; return o; })());
  const [fBase, iBase] = field('设备用来连服务器的地址', { value: location.origin,
    hint: '手机在外面也要能用的话，填公网域名（https://…）；只在家里用填局域网地址就行' });
  const out = h('div');
  const add = h('button', { class: 'primary', text: '生成配对码', onclick: async () => {
    try {
      const r = await api('devices/pair', { name: iNew.value, kind: kSel.value, base: iBase.value });
      const left = h('span', { class: 'muted' });
      const tick = () => {
        const s = Math.min(600, Math.max(0, Math.round((new Date(r.expires) - Date.now()) / 1000)));   // 浏览器和服务器的钟可能差几秒
        left.textContent = s ? `还剩 ${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}` : '已过期';
        return s;
      };
      tick();
      const linkIn = h('input', { type: 'text', readonly: true, value: r.link });
      out.replaceChildren(h('div', { class: 'once' },
        h('div', { class: 'row' }, '配对码：', h('b', { text: r.code, style: 'font-size:20px;letter-spacing:.1em' }), left,
          h('button', { class: 'small', text: '取消', onclick: act('cancel', { code: r.code }, '已取消') })),
        h('div', { class: 'row', style: 'align-items:flex-start;margin-top:8px' },
          r.qr ? qrSvg(r.qr) : null,
          h('div', { style: 'flex:1;min-width:220px' },
            h('p', { class: 'hint', text: '手机：用 App 扫这个二维码。电脑：在托盘程序里粘贴下面的配对链接，或者分别填服务器地址和配对码。' }),
            h('label', { text: '配对链接' }), linkIn,
            h('div', { style: 'margin-top:6px' }, h('a', { href: r.link, text: '▶ 在这台设备上打开（用手机打开这个页面时点这里）' })),
            h('div', { class: 'row', style: 'margin-top:6px' }, h('button', { class: 'small', text: '复制链接', onclick: () => {
              linkIn.select(); navigator.clipboard?.writeText(r.link).then(() => toast('已复制'), () => toast('复制失败，手动选中复制', true));
            } })),
            h('p', { class: 'hint', text: `服务器地址：${r.base}　·　配对码 10 分钟内有效，只能用一次。` })))));
      const before = (D.devices || []).length;
      devPoll = setInterval(async () => {
        if (!tick()) { clearInterval(devPoll); return; }
        try {
          const n = await api('devices');
          if ((n.devices || []).length > before) { toast('配对成功：' + n.devices[n.devices.length - 1].name); renderDevices(box, n); }
        } catch (e) { /* 下一轮再试 */ }
      }, 3000);
    } catch (e) { toast(e.message, true); }
  } });

  box.replaceChildren(h('section', { class: 'win' },
    h('h2', { text: '设备' }),
    h('p', { class: 'hint', text: '电脑托盘程序和安卓 App 都在这里配对。每台设备一个口令，丢了 / 不用了就停用或删除，不影响其他设备。' }),
    h('p', { class: 'hint' }, '安卓 App 安装包：', h('a', { href: '/app/hongtang-live.apk', text: location.origin + '/app/hongtang-live.apk' }), '（手机浏览器打开就能下载）'),
    list,
    h('div', { style: 'margin-top:16px' }, h('label', { text: '添加设备' }),
      h('div', { class: 'grid' }, fNew, h('div', {}, h('label', { text: '类型' }), kSel), h('div', { class: 'full' }, fBase)),
      h('div', { class: 'row', style: 'margin-top:8px' }, add, h('button', { class: 'small', text: '刷新列表', onclick: reload })), out),
    (D.pending || []).length ? h('p', { class: 'hint', text: `还有 ${D.pending.length} 个配对码没用掉：` + D.pending.map(p => p.code).join('、') }) : null));
}

async function loadDevices(box) {
  try { renderDevices(box, await api('devices')); }
  catch (e) { box.replaceChildren(h('section', { class: 'win' }, h('h2', { text: '设备' }), h('p', { class: 'bad', text: '读取设备失败：' + e.message }))); }
}

async function loadSettings() {
  try {
    const d = await api('config');
    renderSettings(d.config, d.status);
  } catch (e) {
    if (e.status === 401) return renderLogin();
    app.replaceChildren(h('p', { class: 'bad', text: '读取设置失败：' + e.message }));
  }
}

async function boot() {
  $('#logout').hidden = true;
  try {
    const s = await api('state');
    if (s.setup) return renderSetup();
    if (!s.logged) return renderLogin();
    loadSettings();
  } catch (e) {
    app.replaceChildren(h('p', { class: 'bad', text: '连不上服务器：' + e.message }));
  }
}

$('#logout').addEventListener('click', async () => {
  try { await api('logout', {}); } catch (e) { /* 无所谓 */ }
  boot();
});
boot();
