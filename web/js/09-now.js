'use strict';
// 「いま / 此刻」页
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。
/* ---------------- いま / 此刻（原 daylog 首页：此刻在干嘛 + 今天用过的应用；管理员可翻历史、看 AI 日报） ---------------- */
const nwState = { data: null, day: '', loading: false, error: '', timer: null, left: 0, reports: null, rep: {}, repErr: {} };
const NW_HR_EMPTY = '无数据，疑似是似了喵~';   // 状态文案：日语模式下也保持中文

const tf = (k, vars) => {
  let s = t(k);
  Object.keys(vars || {}).forEach(n => { s = s.split('{' + n + '}').join(String(vars[n])); });
  return s;
};
const nwColor = c => (/^#[0-9a-fA-F]{6}$/.test(c || '') ? c : '#94a3b8');
const nwUrl = u => (/^https?:\/\//i.test(u || '') ? u : '');
function nwWeekday(day) {
  const d = new Date(day + 'T12:00:00');
  const i = (d.getDay() + 6) % 7;
  return (lang === 'zh' ? '周' : '') + t('nw_wd').split(',')[i] + (lang === 'zh' ? '' : '曜日');
}
function nwDur(sec) {
  const s = Math.max(0, Math.round(Number(sec) || 0));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h ? `${h}h${String(m).padStart(2, '0')}m` : `${m}m`;
}

function nwHeroHtml(d) {
  let title, meta;
  if (d.is_today) {
    const v = d.view || {};
    title = v.now || '—';
    meta = [v.dur_text, v.cat].filter(Boolean).join(' · ');
  } else {
    const [, mo, da] = d.day.split('-').map(Number);
    title = tf('nw_date', { m: mo, d: da });
    meta = nwWeekday(d.day) + ' · ' + tf('nw_total_on', { d: d.total });
  }
  return `<section class="card nw-hero">
    <div class="nw-face" aria-hidden="true">${esc(d.face || '')}</div>
    <div class="nw-title${d.stale ? ' is-off' : ''}" role="heading" aria-level="2">${esc(title)}</div>
    <p class="nw-meta">${esc(meta || '—')}</p>
    <div class="nw-kpis">
      <div><small>${t(d.is_today ? 'nw_today' : 'nw_that_day')}</small><b>${esc(d.total || '0s')}</b></div>
      <div><small>${t('nw_apps')}</small><b>${esc(String(d.app_count || 0))}</b></div>
      <div><small>${t('nw_last')}</small><b>${esc(d.last_clock || '—')}</b></div>
    </div>
  </section>`;
}

function nwMediaHtml(d) {
  const items = (d.media || []).slice(0, 2);
  if (!items.length) return '';
  return `<div class="nw-media" data-count="${items.length}">${items.map(it => {
    const music = it.kind === 'music';
    const coverSrc = nwUrl(it.cover) ? pic(it.cover, 140) : '';
    const cover = coverSrc
      ? `<img src="${esc(coverSrc)}" alt="" referrerpolicy="no-referrer" loading="lazy">`
      : `<span>${music ? '♪' : '▶'}</span>`;
    const url = nwUrl(it.url);
    return `<article class="card nw-mcard" data-kind="${music ? 'music' : 'live'}">
      <div class="nw-mcover">${cover}</div>
      <div class="nw-mbody">
        <div class="nw-mkicker">${t(music ? 'nw_listening' : 'nw_live')}</div>
        <div class="nw-mtitle" title="${esc(it.title || '')}">${esc(it.title || '')}</div>
        <div class="nw-msub">${esc(it.sub || '')}</div>
      </div>
      ${url ? `<a class="nw-mlink" href="${esc(url)}" target="_blank" rel="noreferrer">${t(music ? 'nw_netease' : 'nw_open')}</a>` : ''}
    </article>`;
  }).join('')}</div>`;
}

function nwHealthHtml(d) {
  const h = d.health;
  if (!h || !h.available) return '';
  const dates = h.dates || {};
  const tile = (label, date, val, sub, extra = '', cls = '') => `<div class="hm-stat nw-htile${cls}">
      <div class="hm-stat-k"><span>${label}</span><span>${esc(date || '')}</span></div>
      <div class="hm-stat-v">${val}</div>${extra}
      <div class="hm-stat-sub">${sub}</div>
    </div>`;
  const tiles = [];
  const fresh = h.hr_live != null;
  tiles.push(tile(t('hl_hr'), dates.hr_live,
    fresh ? `${esc(String(h.hr_live))} <i>bpm</i>` : esc(NW_HR_EMPTY),
    fresh ? esc(tf('nw_sec_ago', { n: h.hr_live_age != null ? h.hr_live_age : 0 }))
          : esc(h.hr_last_text || t('nw_hr_none')), '', fresh ? '' : ' is-empty'));
  if (h.sleep_min) {
    const p = Math.max(0, Math.min(1, h.sleep_min / 480));
    tiles.push(tile(t('hl_sleep'), dates.sleep_min || dates.sleep, esc(fmtSleepMin(h.sleep_min)),
      h.sleep_score ? esc(`${t('hl_score')} ${h.sleep_score}`) : t('nw_sleep_goal'),
      `<div class="nw-hbar"><i style="width:${(p * 100).toFixed(1)}%"></i></div>`));
  }
  if (h.spo2 != null) tiles.push(tile(t('hl_spo2'), dates.spo2, `${esc(String(h.spo2))}<i>%</i>`, t('nw_spo2_sub')));
  if (h.stress != null) tiles.push(tile(t('hl_stress'), dates.stress, esc(String(h.stress)), t('nw_stress_sub')));
  const upd = h.updated ? String(h.updated).replace('T', ' ').slice(5, 16) : '';
  return `<section class="card nw-health">
    <div class="dl-card-head">
      <span class="dl-card-title">${ICON.heart}<span>${t('hl_title')}</span></span>
      <span class="dl-stamp">${t('hl_src')}${upd ? ' · ' + esc(upd) : ''}</span>
    </div>
    <div class="hm-health-grid">${tiles.join('')}</div>
  </section>`;
}

function nwDayBarHtml(d) {
  const btn = (day, dir) => day
    ? `<button class="nw-nav" type="button" data-nw-day="${esc(day)}" aria-label="${t(dir)}">${dir === 'nw_prev' ? '‹' : '›'}</button>`
    : `<span class="nw-nav is-off" aria-hidden="true">${dir === 'nw_prev' ? '‹' : '›'}</span>`;
  const back = (!d.is_today && d.admin) ? `<button class="nw-today" type="button" data-nw-day="">${t('nw_back_today')}</button>` : '';
  return `<div class="nw-daybar">
    ${btn(d.prev, 'nw_prev')}
    <div class="nw-daylabel"><b>${esc(d.day)}</b> ${esc(nwWeekday(d.day))}</div>
    ${btn(d.next, 'nw_next')}
    ${back}
  </div>`;
}

function nwAppsHtml(d) {
  const apps = d.apps || [];
  if (!apps.length) return `<section class="card dl-card"><div class="empty">${ICON.empty}<span>${t('nw_list_empty')}</span></div></section>`;
  const liveOK = d.is_today && !d.stale;
  return `<section class="card dl-card nw-apps">
    <div class="dl-app-list">
      ${apps.map(a => `<div class="dl-app-row${a.cur && liveOK ? ' is-cur' : ''}">
        <span class="nw-dot" style="background:${nwColor(a.color)}"></span>
        <span class="dl-app-name" title="${esc(a.label || '')}">${esc(a.label || '')}</span>
        ${a.cur && liveOK ? `<span class="dl-app-cur">LIVE</span>` : ''}
        <span class="dl-app-cat">${esc(a.cat || '')}</span>
        <span class="dl-app-dur">${esc(tf('nw_total_x', { d: a.total || '' }))}</span>
      </div>`).join('')}
    </div>
  </section>`;
}

function nwMetaHtml(d) {
  const devs = (d.devices || []).map(x => {
    const bat = x.battery != null ? ` <b>${esc(tf(x.charging ? 'nw_bat_chg' : 'nw_bat', { n: x.battery }))}</b>` : '';
    return `<span class="nw-dev${x.online ? ' is-on' : ''}"><i></i>${esc(x.name || '')}${bat}</span>`;
  }).join('');
  const chips = (d.cats || []).map(c => `<span class="nw-chip"><i style="background:${nwColor(c.color)}"></i>${esc(c.name)} <b>${esc(c.dur)}</b></span>`).join('');
  if (!devs && !chips) return '';
  return `<div class="nw-metarow">${devs}${devs && chips ? '<span class="nw-sep">·</span>' : ''}${chips}</div>`;
}

function nwFootHtml(d) {
  if (!d.is_today) return '';
  const poll = d.poll || 10;
  const left = Math.max(0, nwState.left);
  return `<div class="nw-foot">
    <span class="nw-ring" style="--p:${(left / poll).toFixed(3)}"></span>
    <span id="nwCd">${esc(tf('nw_refresh_in', { n: left }))}</span>
    ${d.viewers ? `<span class="nw-sep">·</span><span>${esc(tf('nw_viewers', { n: d.viewers }))}</span>` : ''}
  </div>`;
}

/* AI 日报（只有管理员能看到；内容是模型写的中文，原样显示） */
function nwReportHtml(d) {
  if (!d.admin) return '';
  let body = '';
  if (!d.is_today) {
    const r = nwState.rep[d.day];
    if (!d.has_report) body = `<div class="empty">${ICON.empty}<span>${t('rp_none')}</span></div>`;
    else if (nwState.repErr[d.day]) body = `<div class="empty">${ICON.empty}<span>${esc(nwState.repErr[d.day])}</span></div>`;
    else if (!r) body = `<div class="mu-skel" style="height:180px"></div>`;
    else body = nwReportBody(r);
  }
  const list = (nwState.reports || []).slice(0, 14);
  const chips = list.length ? `<div class="nw-replist"><small>${t('rp_list')}</small>${list.map(x =>
    `<button type="button" class="nw-repchip${x.day === d.day ? ' on' : ''}" data-nw-day="${esc(x.day)}">${esc(x.day.slice(5))}${x.telegram_sent ? ' ✓' : ''}</button>`).join('')}</div>` : '';
  if (!body && !chips) return '';
  return `<section class="card dl-card nw-report">
    <div class="dl-card-head">
      <span class="dl-card-title">${ICON.list}<span>${t('rp_title')}</span></span>
      <span class="dl-stamp">${t('rp_admin_only')}</span>
    </div>
    ${body}${chips}
  </section>`;
}

function nwReportBody(r) {
  const st = r.stats || {}, an = r.analysis || {}, tt = st.totals || {};
  const arr = v => (Array.isArray(v) ? v : []);
  const kv = (label, val) => `<div class="dl-kpi"><b>${esc(val)}</b><small>${label}</small></div>`;
  const sec = (title, inner) => inner ? `<div class="nw-rsec"><h4>${title}</h4>${inner}</div>` : '';
  const ul = items => items.length ? `<ul>${items.join('')}</ul>` : '';
  const head = an.headline ? `<p class="nw-rhead">${esc(an.headline)}</p>` : '';
  const kpis = `<div class="dl-sum-kpis nw-rkpis">
    ${kv(t('rp_active'), nwDur(tt.active_sec))}${kv(t('rp_focus'), nwDur(tt.longest_focus_sec))}
    ${kv(t('rp_switch'), String(tt.switches || 0))}${kv(t('rp_keys'), String(tt.keys || 0))}
    ${kv(t('rp_span'), (tt.first || '—') + '–' + (tt.last || '—'))}
  </div>`;
  const narr = String(an.narrative || '').split(/\n\s*\n/).map(x => x.trim()).filter(Boolean).map(p => `<p>${esc(p)}</p>`).join('');
  const cats = arr(st.categories).slice(0, 8).map(c => `<span class="nw-chip">${esc(c.name)} <b>${esc(nwDur(c.sec))}</b></span>`).join('');
  const notes = ul(arr(an.timeline_notes).map(n => `<li><code>${esc(n.time || '')}</code> ${esc(n.what || '')} —— ${esc(n.note || '')}</li>`));
  const pats = ul(arr(an.patterns).map(p => `<li><b>${esc(p.pattern || '')}</b> ${esc(p.evidence || '')}</li>`));
  const leaks = ul(arr(an.leaks).map(x => `<li>${esc(x.target || '')}（${esc(tf('rp_min', { n: parseInt(x.minutes, 10) || 0 }))}）：${esc(x.why || '')}</li>`));
  const mu = an.music && typeof an.music === 'object' ? an.music : {};
  const music = [mu.summary ? `<p>${esc(mu.summary)}</p>` : '',
    arr(mu.styles).length ? `<p>${t('rp_styles')}：${esc(arr(mu.styles).slice(0, 6).join('、'))}</p>` : '',
    mu.obsession ? `<p>${t('rp_obsession')}：${esc(mu.obsession)}</p>` : ''].join('');
  const songs = ul(arr(st.listened).slice(0, 12).map(x => `<li>${esc(x.title || '')} — ${esc(x.artist || '')}（${esc(tf('rp_plays', { n: x.plays || 1 }))}）</li>`));
  const health = st.health && typeof st.health === 'object'
    ? ul(Object.keys(st.health).map(k => `<li>${esc(k)}：${esc(String(st.health[k]))}</li>`)) : '';
  const meta = [r.created_at ? esc(String(r.created_at).replace('T', ' ').slice(0, 16)) : '', r.telegram_sent ? t('rp_tg') : '']
    .filter(Boolean).join(' · ');
  return `${head}${kpis}
    ${cats ? `<div class="nw-metarow">${cats}</div>` : ''}
    ${narr ? `<div class="nw-rnarr">${narr}</div>` : `<p class="nw-rmuted">${t('rp_no_ai')}</p>`}
    ${sec(t('rp_timeline'), notes)}${sec(t('rp_patterns'), pats)}${sec(t('rp_leaks'), leaks)}
    ${sec(t('rp_music'), music)}${sec(t('rp_songs'), songs)}${sec(t('rp_health'), health)}
    ${an.question ? `<blockquote class="nw-rq">${esc(an.question)}</blockquote>` : ''}
    ${an.data_quality ? `<p class="nw-rmuted">${t('rp_quality')}：${esc(an.data_quality)}</p>` : ''}
    ${meta ? `<p class="nw-rmuted">${meta}</p>` : ''}`;
}

function nowContent() {
  const d = nwState.data;
  if (!d) {
    if (nwState.error) return `<div class="empty">${ICON.empty}<span>${esc(nwState.error)}</span></div>`;
    return `<div class="nw-wrap">${[150, 90, 300].map(h => `<div class="mu-skel" style="height:${h}px"></div>`).join('')}</div>`;
  }
  return `<div class="nw-wrap">
    <div data-nw="hero">${nwHeroHtml(d)}</div>
    <div data-nw="media">${nwMediaHtml(d)}</div>
    <div data-nw="health">${nwHealthHtml(d)}</div>
    <div data-nw="day">${nwDayBarHtml(d)}</div>
    <div data-nw="apps">${nwAppsHtml(d)}</div>
    <div data-nw="meta">${nwMetaHtml(d)}</div>
    <div data-nw="report">${nwReportHtml(d)}</div>
    <div data-nw="foot">${nwFootHtml(d)}</div>
  </div>`;
}

function pageNow() {
  return `<div id="nwRoot">${nowContent()}</div>`;
}

/* 只替换内容变了的那一块，轮询时封面图不闪、列表不重排 */
function paintNow() {
  const root = $('#nwRoot');
  if (!root) return;
  const d = nwState.data;
  if (!d || !root.querySelector('[data-nw]')) { root.innerHTML = nowContent(); return; }
  const parts = { hero: nwHeroHtml, media: nwMediaHtml, health: nwHealthHtml, day: nwDayBarHtml,
                  apps: nwAppsHtml, meta: nwMetaHtml, report: nwReportHtml, foot: nwFootHtml };
  Object.keys(parts).forEach(k => {
    const el = root.querySelector(`[data-nw="${k}"]`);
    const html = parts[k](d);
    if (el && el.dataset.sig !== html) { el.innerHTML = html; el.dataset.sig = html; }
  });
}

async function loadNow() {
  if (nwState.loading) return;
  nwState.loading = true;
  try {
    const q = nwState.day ? '?d=' + encodeURIComponent(nwState.day) : '';
    const r = await fetch('api/now' + q, { credentials: 'same-origin', cache: 'no-store' });
    const d = await r.json();
    if (!r.ok || !d || !d.ok) throw new Error((d && d.error) || 'HTTP ' + r.status);
    nwState.data = d;
    nwState.error = '';
    if (d.admin) {
      if (!nwState.reports) loadNowReports();
      if (!d.is_today && d.has_report && !nwState.rep[d.day]) loadNowReport(d.day);
    }
  } catch (e) {
    nwState.error = (e && e.message) || String(e);
  } finally {
    nwState.loading = false;
    nwState.left = (nwState.data && nwState.data.poll) || 10;
    paintNow();
  }
}

async function loadNowReports() {
  try {
    const r = await fetch('api/admin/daylog/reports', { credentials: 'same-origin', cache: 'no-store' });
    const d = await r.json();
    nwState.reports = (d && d.ok && d.reports) || [];
  } catch (e) { nwState.reports = []; }
  paintNow();
}

async function loadNowReport(day) {
  try {
    const r = await fetch('api/admin/daylog/report?d=' + encodeURIComponent(day), { credentials: 'same-origin', cache: 'no-store' });
    const d = await r.json();
    if (!r.ok || !d || !d.ok) throw new Error((d && d.error) || 'HTTP ' + r.status);
    nwState.rep[day] = d;
  } catch (e) {
    nwState.repErr[day] = (e && e.message) || String(e);
  }
  paintNow();
}

function stopNowLive() {
  if (nwState.timer) { clearInterval(nwState.timer); nwState.timer = null; }
}

/* 每秒走一格倒计时；归零就拉一次。看历史 / 标签页在后台时不拉 */
function startNowLive() {
  stopNowLive();
  nwState.timer = setInterval(() => {
    if (!document.getElementById('nwRoot')) { stopNowLive(); return; }
    const d = nwState.data;
    if (document.hidden || !d || !d.is_today) return;
    nwState.left -= 1;
    if (nwState.left <= 0) { loadNow(); return; }
    const foot = $('#nwRoot [data-nw="foot"]');
    if (foot) { const html = nwFootHtml(d); foot.innerHTML = html; foot.dataset.sig = html; }
  }, 1000);
}

document.addEventListener('visibilitychange', () => {
  if (!document.hidden && nwState.timer && document.getElementById('nwRoot')) loadNow();
});

document.addEventListener('click', (e) => {
  const b = e.target.closest('#nwRoot [data-nw-day]');
  if (!b) return;
  nwState.day = b.dataset.nwDay || '';
  nwState.data = nwState.data && { ...nwState.data };   // 保留旧画面直到新数据回来
  loadNow();
});

