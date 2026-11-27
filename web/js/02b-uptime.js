'use strict';
// 服务状态（Uptime Kuma）：数据拉取、首页卡片、独立状态页、X2 桌面摘要
const upState = { loading: false, data: null, error: null, at: 0 };

function upStatusMeta(code) {
  if (code === 1) return { cls: 'is-up', key: 'up_up' };
  if (code === 0) return { cls: 'is-down', key: 'up_off' };
  if (code === 3) return { cls: 'is-maint', key: 'up_maint' };
  return { cls: 'is-pend', key: 'up_pending' };
}

function upOverallMeta(overall) {
  if (overall === 'up') return { dot: 'is-up', pill: 'is-up', key: 'up_all_ok' };
  if (overall === 'down') return { dot: 'is-down', pill: 'is-down', key: 'up_down' };
  if (overall === 'maintenance') return { dot: 'is-maint', pill: 'is-maint', key: 'up_maint' };
  return { dot: 'is-warn', pill: 'is-warn', key: 'up_degraded' };
}

function upFmtPct(v) {
  if (v == null || !isFinite(Number(v))) return '';
  return (Number(v) * 100).toFixed(2).replace(/\.00$/, '') + '%';
}

function upFmtPing(v) {
  if (v == null || !isFinite(Number(v)) || Number(v) <= 0) return '';
  return Math.round(Number(v)) + ' ms';
}

function upPctLevel(v) {
  if (v == null || !isFinite(Number(v))) return '';
  const p = Number(v) * 100;
  if (p < 95) return 'is-bad';
  if (p < 99) return 'is-warn';
  return '';
}

function upPingLevel(v) {
  if (v == null || !isFinite(Number(v)) || Number(v) <= 0) return '';
  const ms = Number(v);
  if (ms >= 1000) return 'is-bad';
  if (ms >= 300) return 'is-warn';
  return '';
}

function upBarsHtml(beats, count, large) {
  const list = Array.isArray(beats) ? beats.slice(-count) : [];
  const pad = Math.max(0, count - list.length);
  const cells = [];
  for (let i = 0; i < pad; i++) cells.push('<span class="up-bar"></span>');
  for (const b of list) {
    const m = upStatusMeta(b.s);
    const tip = [b.t || '', t(m.key), upFmtPing(b.p)].filter(Boolean).join(' · ');
    cells.push(`<span class="up-bar ${m.cls}" title="${esc(tip)}"></span>`);
  }
  return `<div class="up-bars${large ? ' is-lg' : ''}">${cells.join('')}</div>`;
}

function upFlattenMonitors(d) {
  const out = [];
  if (!d || !Array.isArray(d.groups)) return out;
  for (const g of d.groups) {
    if (!g || !Array.isArray(g.monitors)) continue;
    for (const m of g.monitors) out.push(m);
  }
  return out;
}

function upOnlineText(sum) {
  const up = (sum && sum.up) || 0;
  const total = (sum && sum.total) || 0;
  return t('up_online_cnt').replace('{up}', String(up)).replace('{total}', String(total));
}

function homeUptimeCardHtml() {
  const d = upState.data;
  if (upState.loading && !d) {
    return `<section class="card hm-card hm-link" data-go="status">
      <div class="hm-card-head">
        <span class="hm-card-title">${ICON.globe}<span>${t('nav_status')}</span></span>
        <a class="hm-more" href="#status">${t('hm_more')}</a>
      </div>
      <div class="mu-skel" style="height:68px"></div>
    </section>`;
  }
  const sum = d && d.ok ? d.summary : null;
  const ov = sum ? upOverallMeta(sum.overall) : null;
  const mons = (d && d.ok) ? upFlattenMonitors(d).slice(0, 6) : [];
  const emptyText = !d ? (upState.error || t('up_err'))
    : (d.configured === false ? t('up_unconfigured') : (d.ok ? t('up_empty') : (d.error || t('up_err'))));

  return `<section class="card hm-card hm-link" data-go="status">
    <div class="hm-card-head">
      <span class="hm-card-title">${ICON.globe}<span>${t('nav_status')}</span></span>
      <div class="hm-head-right">
        ${ov ? `<span class="up-tag ${ov.pill}"><i class="up-dot ${ov.dot}"></i>${t(ov.key)} · ${esc(upOnlineText(sum))}</span>` : ''}
        <a class="hm-more" href="#status">${t('hm_more')}</a>
      </div>
    </div>
    ${!mons.length ? `<div class="empty">${ICON.empty}<span>${esc(emptyText)}</span></div>` : `
      <div class="hm-up-grid">
        ${mons.map(m => {
          const st = upStatusMeta(m.status);
          const pct = upFmtPct(m.uptime24);
          const ping = upFmtPing(m.ping);
          const metaCls = pct ? ('up-pct ' + upPctLevel(m.uptime24)) : (ping ? ('up-ping ' + upPingLevel(m.ping)) : 'up-ping');
          const meta = pct || ping || t(st.key);
          return `<div class="hm-up-item">
            <div class="hm-up-top">
              <span class="hm-up-name" title="${esc(m.name)}"><i class="up-dot ${st.cls}"></i>${esc(m.name)}</span>
              <span class="hm-up-meta ${metaCls}">${esc(meta)}</span>
            </div>
            ${upBarsHtml(m.beats, 15, false)}
          </div>`;
        }).join('')}
      </div>`}
  </section>`;
}

function pageStatusInnerHtml() {
  const d = upState.data;
  if (upState.loading && !d) {
    return `<div class="up-page">
      <section class="card up-hero"><div class="mu-skel" style="height:44px;width:100%"></div></section>
      <section class="card up-group"><div class="mu-skel" style="height:120px;width:100%"></div></section>
    </div>`;
  }
  const pubUrl = (d && d.publicUrl) || '';
  const actionsHtml = `<div class="up-hero-actions">
    <button class="up-btn" type="button" id="upRefreshBtn">${ICON.refresh}<span>${t('st_refresh')}</span></button>
    ${pubUrl ? `<a class="up-btn" href="${esc(pubUrl)}" target="_blank" rel="noopener noreferrer">${ICON.external}<span>${t('up_open')}</span></a>` : ''}
  </div>`;

  if (!d || !d.ok) {
    const msg = !d ? (upState.error || t('up_err'))
      : (d.configured === false ? t('up_unconfigured') : (d.error || t('up_err')));
    return `<div class="up-page">
      <section class="card up-hero">
        <div class="up-hero-l">
          <i class="up-hero-dot"></i>
          <div>
            <div class="up-hero-title">${t('nav_status')}</div>
            <div class="up-hero-sub">${esc(msg)}</div>
          </div>
        </div>
        ${actionsHtml}
      </section>
    </div>`;
  }

  const sum = d.summary || {};
  const ov = upOverallMeta(sum.overall);
  const updMatch = d.updated ? String(d.updated).match(/T(\d{2}:\d{2}:\d{2})/) : null;
  const subParts = [upOnlineText(sum), d.title || '', updMatch ? updMatch[1] + ' UTC' : ''].filter(Boolean);
  const inc = d.incident;
  const groups = Array.isArray(d.groups) ? d.groups : [];

  return `<div class="up-page">
    <section class="card up-hero">
      <div class="up-hero-l">
        <i class="up-hero-dot ${ov.dot}"></i>
        <div>
          <div class="up-hero-title">${t(ov.key)}</div>
          <div class="up-hero-sub">${esc(subParts.join(' · '))}</div>
        </div>
      </div>
      ${actionsHtml}
    </section>
    ${inc ? `<section class="card up-incident${inc.style === 'danger' ? ' is-danger' : ''}">
      ${inc.title ? `<div class="up-incident-t">${esc(inc.title)}</div>` : ''}
      ${inc.content ? `<div class="up-incident-b">${esc(inc.content)}</div>` : ''}
    </section>` : ''}
    ${!groups.length ? `<section class="card up-group"><div class="empty">${ICON.empty}<span>${t('up_empty')}</span></div></section>` : groups.map(g => {
      const mons = Array.isArray(g.monitors) ? g.monitors : [];
      const gUp = mons.filter(m => m.status === 1).length;
      return `<section class="card up-group">
        <div class="up-group-head">
          <span class="up-group-title">${esc(g.name || t('nav_status'))}</span>
          <span class="up-group-cnt">${gUp}/${mons.length}</span>
        </div>
        <div class="up-mon-list">
          ${mons.map(m => {
            const st = upStatusMeta(m.status);
            const pct = upFmtPct(m.uptime24);
            const ping = upFmtPing(m.ping);
            return `<div class="up-mon">
              <div class="up-mon-top">
                <span class="up-mon-l">
                  <i class="up-dot ${st.cls}"></i>
                  <span class="up-mon-name">${esc(m.name)}</span>
                </span>
                <span class="up-mon-r">
                  ${ping ? `<span class="up-ping ${upPingLevel(m.ping)}">${esc(ping)}</span>` : ''}
                  ${pct ? `<span class="up-pct ${upPctLevel(m.uptime24)}">${esc(pct)}</span>` : ''}
                  <span class="up-tag ${st.cls === 'is-pend' ? 'is-warn' : st.cls}">${t(st.key)}</span>
                </span>
              </div>
              ${upBarsHtml(m.beats, 30, true)}
              ${m.msg ? `<div class="up-mon-msg" title="${esc(m.msg)}">${esc(m.msg)}</div>` : ''}
            </div>`;
          }).join('')}
        </div>
      </section>`;
    }).join('')}
  </div>`;
}

function pageStatus() {
  return `<div id="upRoot">${pageStatusInnerHtml()}</div>`;
}

function paintUptimeViews() {
  const hb = document.getElementById('hmUptimeBox');
  if (hb) hb.innerHTML = homeUptimeCardHtml();
  const rootEl = document.getElementById('upRoot');
  if (rootEl) {
    rootEl.innerHTML = pageStatusInnerHtml();
    const btn = document.getElementById('upRefreshBtn');
    if (btn) btn.addEventListener('click', () => loadUptime('refresh'));
  }
  if (window.HL_UI && window.HL_UI.getMode() === 'x2' && window.HL_X2) {
    window.HL_X2.paint();
  }
}

async function loadUptime(force = false) {
  if (upState.loading) return;
  const hard = force === 'refresh';
  if (!force && upState.data && (Date.now() - upState.at) < 20000) {
    paintUptimeViews();
    return;
  }
  upState.loading = true;
  upState.error = null;
  paintUptimeViews();
  try {
    const r = await fetch(hard ? 'api/uptime?refresh=1' : 'api/uptime', { credentials: 'same-origin', cache: 'no-store' });
    const d = await r.json();
    upState.data = d;
    upState.at = Date.now();
  } catch (e) {
    upState.error = e.message || String(e);
  } finally {
    upState.loading = false;
    paintUptimeViews();
  }
}

function x2UptimeSummaryHtml(zh) {
  const d = upState.data;
  if (upState.loading && !d) {
    return `<div style="color:var(--os-muted)">${zh ? '正在读取服务状态…' : '読み込み中…'}</div>`;
  }
  if (!d || !d.ok) {
    const msg = !d ? (upState.error || t('up_err'))
      : (d.configured === false ? t('up_unconfigured') : (d.error || t('up_err')));
    return `<div style="color:var(--os-muted)">${esc(msg)}</div>`;
  }
  const sum = d.summary || {};
  const ov = upOverallMeta(sum.overall);
  const mons = upFlattenMonitors(d).slice(0, 4);
  return `<div class="x2-up-head">
    <span style="display:inline-flex;align-items:center;gap:6px;font-weight:600"><i class="up-dot ${ov.dot}"></i>${t(ov.key)}</span>
    <span style="color:var(--os-muted)">${esc(upOnlineText(sum))}</span>
  </div>
  ${!mons.length ? `<div style="color:var(--os-muted)">${t('up_empty')}</div>` : `<div class="x2-up-list">
    ${mons.map(m => {
      const st = upStatusMeta(m.status);
      const ping = upFmtPing(m.ping);
      const pct = upFmtPct(m.uptime24);
      const val = ping || pct || t(st.key);
      const valCls = ping ? ('up-ping ' + upPingLevel(m.ping)) : ('up-pct ' + upPctLevel(m.uptime24));
      return `<div class="x2-up-row">
        <span class="x2-up-name"><i class="up-dot ${st.cls}"></i>${esc(m.name)}</span>
        <span class="x2-up-spark">${upBarsHtml(m.beats, 10, false)}</span>
        <span class="x2-up-val ${valCls}">${esc(val)}</span>
      </div>`;
    }).join('')}
  </div>`}`;
}
