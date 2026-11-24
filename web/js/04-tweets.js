'use strict';
// 动态页（daylog 动态流）
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。
function pageStats() {
  /* 中身は index.html の静的な #panel-stats（本家 stats.js が描画する）。ここでは空。 */
  return '';
}

function tweetsContent() {
  if (dlState.loading && !dlState.data) {
    return `<div class="dl-grid">
      ${Array.from({ length: 4 }, (_, i) => `<div class="mu-skel${i === 3 ? ' is-wide' : ''}" style="height:${i === 3 ? 220 : 148}px"></div>`).join('')}
    </div>`;
  }
  const d = dlState.data;
  if (!d || !d.ok) {
    return `<div class="empty">${ICON.empty}<span>${esc(dlState.error || t('dl_err'))}</span></div>`;
  }

  const v = d.view || {};
  const sum = d.summary || {};
  const cats = sum.cats || [];
  const apps = d.apps || [];
  // 排除健康(health)与音乐(music)，保留其余媒体(如直播 live)
  const medias = (d.media || []).filter(m => m && m.kind !== 'music');

  const nowCard = `
    <section class="card dl-card">
      <div class="dl-card-head">
        <span class="dl-card-title">${ICON.devices}<span>${t('dl_now')}</span></span>
        <span class="dl-pill${v.stale ? ' is-idle' : ''}">${v.stale ? t('dl_offline') : t('dl_online')}</span>
      </div>
      <div class="dl-now-row">
        <div class="dl-face">${esc(v.face || 'ฅ^•ﻌ•^ฅ')}</div>
        <div class="dl-now-info">
          <div class="dl-now-title">${esc(v.now || '—')}</div>
          <div class="dl-now-sub">${esc([v.dur_text, v.cat].filter(Boolean).join(' · ') || '—')}</div>
        </div>
      </div>
    </section>`;

  const sumCard = `
    <section class="card dl-card">
      <div class="dl-card-head">
        <span class="dl-card-title">${ICON.clock}<span>${t('dl_today')}</span></span>
        <span class="dl-stamp">${esc(d.today || '')}</span>
      </div>
      <div class="dl-sum-kpis">
        <div class="dl-kpi"><b>${esc(sum.total || '0m')}</b><small>${t('dl_total_dur')}</small></div>
        <div class="dl-kpi"><b>${esc(String(sum.app_count || 0))}</b><small>${t('dl_app_cnt')}</small></div>
      </div>
      ${cats.length ? `
        <div class="dl-cat-bar">
          ${cats.map(c => `<i style="width:${c.pct}%;background:${esc(c.color || '#94a3b8')}" title="${esc(c.name)} ${esc(c.dur)}"></i>`).join('')}
        </div>
        <div class="dl-cat-tags">
          ${cats.map(c => `<span class="dl-cat-tag"><i style="background:${esc(c.color || '#94a3b8')}"></i><span>${esc(c.name)}</span><b>${esc(c.dur)}</b></span>`).join('')}
        </div>` : ''}
    </section>`;

  const liveCard = medias.length ? `
    <section class="card dl-card is-wide">
      <div class="dl-card-head">
        <span class="dl-card-title">${ICON.videos}<span>${t('dl_live')}</span></span>
        <span class="dl-stamp">${medias.length}</span>
      </div>
      <div class="dl-media-list">
        ${medias.map(m => {
          const inner = `
            <div style="min-width:0;flex:1">
              <div style="font-weight:600;font-size:13.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(m.title || '')}</div>
              <div style="font-size:12px;color:var(--txt3)">${esc(m.sub || '')}</div>
            </div>
            ${m.url ? `<span style="color:var(--txt3);display:grid;place-items:center">${ICON.external}</span>` : ''}`;
          return m.url
            ? `<a class="dl-media-item" href="${esc(m.url)}" target="_blank" rel="noreferrer">${inner}</a>`
            : `<div class="dl-media-item">${inner}</div>`;
        }).join('')}
      </div>
    </section>` : '';

  const appsCard = `
    <section class="card dl-card is-wide">
      <div class="dl-card-head">
        <span class="dl-card-title">${ICON.list}<span>${t('dl_apps')}</span></span>
        <button class="vi-refresh${dlState.loading ? ' is-spin' : ''}" type="button" id="dlRefresh">${ICON.refresh}<span>${t('vi_refresh')}</span></button>
      </div>
      ${!apps.length ? `<div class="empty">${ICON.empty}<span>${t('dl_empty')}</span></div>` : `
        <div class="dl-app-list">
          ${apps.map(a => `
            <div class="dl-app-row">
              <span class="dl-app-dot" style="background:${esc(a.color || '#94a3b8')}"></span>
              <span class="dl-app-name" title="${esc(a.label || '')}">${esc(a.label || '')}</span>
              ${a.cur ? `<span class="dl-app-cur">LIVE</span>` : ''}
              <span class="dl-app-cat">${esc(a.cat || '')}</span>
              <span class="dl-app-dur">${esc(a.total || '')}</span>
            </div>`).join('')}
        </div>`}
    </section>`;

  return `<div class="dl-grid">${nowCard}${sumCard}${liveCard}${appsCard}</div>`;
}

function paintTweets() {
  const box = $('#twRoot');
  if (!box) return;
  box.innerHTML = tweetsContent();
  const rf = $('#dlRefresh', box);
  if (rf) rf.addEventListener('click', () => loadDaylog(true));
}

async function loadDaylog(force = false) {
  if (dlState.loading) return;
  if (!force && dlState.data && (Date.now() - (dlState.at || 0)) < 15000) {
    paintHomeHealth();
    paintTweets();
    return;
  }
  dlState.loading = true;
  dlState.error = null;
  paintHomeHealth();
  paintTweets();
  try {
    const r = await fetch('api/daylog/feed', { credentials: 'same-origin', cache: 'no-store' });
    const d = await r.json();
    if (!r.ok || !d || !d.ok) throw new Error((d && d.error) || 'HTTP ' + r.status);
    dlState.data = d;
    dlState.at = Date.now();
  } catch (e) {
    dlState.error = e.message || String(e);
  } finally {
    dlState.loading = false;
    paintHomeHealth();
    paintTweets();
  }
}

function startDlLive() {
  stopDlLive();
  stopHomeLive();
  dlTimer = setInterval(() => {
    if (document.hidden) return;
    if (!document.getElementById('twRoot')) {
      stopDlLive();
      return;
    }
    loadDaylog(true);
  }, 20000);
}

/* 从后台切回来时立刻刷新一次，不用等下一个 20 秒 */
document.addEventListener('visibilitychange', () => {
  if (document.hidden) return;
  if (hmTimer && document.getElementById('hmHealthBox')) { loadDaylog(true); loadHomeMusic(true); loadHomeSys(true); }
  else if (dlTimer && document.getElementById('twRoot')) { loadDaylog(true); }
});

function pageTweets() {
  return `<div id="twRoot">${tweetsContent()}</div>`;
}

