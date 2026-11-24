'use strict';
// 首页面板：健康 / 此刻 / 音乐 / 系统 / 番剧卡片
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。
/* ---------------- panels ---------------- */
const dlState = { loading: false, data: null, error: null };
let dlTimer = null;

function stopDlLive() {
  if (dlTimer) {
    clearInterval(dlTimer);
    dlTimer = null;
  }
}

function fmtSleepMin(min) {
  const m = Math.max(0, Math.round(Number(min) || 0));
  const h = Math.floor(m / 60);
  const r = m % 60;
  return h > 0 ? `${h}h ${String(r).padStart(2, '0')}m` : `${r}m`;
}

function healthCardHtml() {
  if (dlState.loading && !dlState.data) {
    return `<section class="card hm-health hm-link" data-go="tweets">
      <div class="hm-health-head">
        <span class="hm-health-title">${ICON.heart}<span>${t('hl_title')}</span></span>
        <span class="hm-health-meta">${t('hl_src')}</span>
      </div>
      <div class="hm-health-grid">
        ${Array.from({ length: 4 }, () => `<div class="mu-skel" style="height:74px"></div>`).join('')}
      </div>
    </section>`;
  }
  const d = dlState.data;
  const h = d && d.health;
  if (!d || !d.ok || !h || !h.available) {
    return `<section class="card hm-health hm-link" data-go="tweets">
      <div class="hm-health-head">
        <span class="hm-health-title">${ICON.heart}<span>${t('hl_title')}</span></span>
        <span class="hm-health-meta">${t('hl_src')}</span>
      </div>
      <div class="empty">${ICON.empty}<span>${esc(dlState.error || t('hl_empty'))}</span></div>
    </section>`;
  }

  const dates = h.dates || {};
  const tiles = [];
  if (h.sleep_min != null) {
    tiles.push(`<div class="hm-stat">
      <div class="hm-stat-k"><span>${t('hl_sleep')}</span><span>${esc(dates.sleep_min || '')}</span></div>
      <div class="hm-stat-v">${esc(fmtSleepMin(h.sleep_min))}</div>
      ${h.sleep_score != null ? `<div class="hm-stat-sub">${t('hl_score')} ${esc(String(h.sleep_score))}</div>` : ''}
    </div>`);
  } else if (h.sleep_score != null) {
    tiles.push(`<div class="hm-stat">
      <div class="hm-stat-k"><span>${t('hl_score')}</span><span>${esc(dates.sleep_score || '')}</span></div>
      <div class="hm-stat-v">${esc(String(h.sleep_score))}</div>
    </div>`);
  }

  if (h.hr_live != null) {
    tiles.push(`<div class="hm-stat">
      <div class="hm-stat-k"><span>${t('hl_hr')}</span><span>${esc(dates.hr_live || '')}</span></div>
      <div class="hm-stat-v">${esc(String(h.hr_live))} bpm</div>
    </div>`);
  } else if (h.hr_last_text) {
    tiles.push(`<div class="hm-stat">
      <div class="hm-stat-k"><span>${t('hl_hr')}</span></div>
      <div class="hm-stat-v is-sm">${esc(h.hr_last_text)}</div>
    </div>`);
  }

  if (h.stress != null) {
    tiles.push(`<div class="hm-stat">
      <div class="hm-stat-k"><span>${t('hl_stress')}</span><span>${esc(dates.stress || '')}</span></div>
      <div class="hm-stat-v">${esc(String(h.stress))}</div>
    </div>`);
  }

  if (h.spo2 != null) {
    tiles.push(`<div class="hm-stat">
      <div class="hm-stat-k"><span>${t('hl_spo2')}</span><span>${esc(dates.spo2 || '')}</span></div>
      <div class="hm-stat-v">${esc(String(h.spo2))}%</div>
    </div>`);
  }

  const updMatch = h.updated ? String(h.updated).match(/T(\d{2}:\d{2})/) : null;
  const upd = updMatch ? updMatch[1] : '';
  return `<section class="card hm-health hm-link" data-go="tweets">
    <div class="hm-health-head">
      <span class="hm-health-title">${ICON.heart}<span>${t('hl_title')}</span></span>
      <span class="hm-health-meta">${t('hl_src')}${upd ? ` · ${esc(upd)}` : ''}</span>
    </div>
    <div class="hm-health-grid">${tiles.join('')}</div>
  </section>`;
}

const hmState = {
  musicNow: null,
  musicLoading: false,
  sys: null,
  sysLoading: false,
};
let hmTimer = null;

function stopHomeLive() {
  if (hmTimer) {
    clearInterval(hmTimer);
    hmTimer = null;
  }
}

function homeNowCardHtml() {
  const d = dlState.data;
  const v = d && d.ok ? d.view : null;
  if (dlState.loading && !v) {
    return `<section class="card hm-card hm-link" data-go="tweets">
      <div class="hm-card-head">
        <span class="hm-card-title">${ICON.devices}<span>${t('dl_now')}</span></span>
        <a class="hm-more" href="#tweets">${t('hm_more')}</a>
      </div>
      <div class="mu-skel" style="height:68px"></div>
    </section>`;
  }
  const activeApps = (d && d.ok && Array.isArray(d.apps)) ? d.apps.slice(0, 3) : [];
  return `<section class="card hm-card hm-link" data-go="tweets">
    <div class="hm-card-head">
      <span class="hm-card-title">${ICON.devices}<span>${t('dl_now')}</span></span>
      <div class="hm-head-right">
        ${v ? `<span class="dl-pill${v.stale ? ' is-idle' : ''}">${v.stale ? t('dl_offline') : t('dl_online')}</span>` : ''}
        <a class="hm-more" href="#tweets">${t('hm_more')}</a>
      </div>
    </div>
    ${!v ? `<div class="empty">${ICON.empty}<span>${esc(dlState.error || t('dl_empty'))}</span></div>` : `
      <div class="dl-now-row">
        <div class="dl-face">${esc(v.face || 'ฅ^•ﻌ•^ฅ')}</div>
        <div class="dl-now-info">
          <div class="dl-now-title">${esc(v.now || '—')}</div>
          <div class="dl-now-sub">${esc([v.dur_text, v.cat, d.summary && d.summary.total ? `${t('dl_total_dur')} ${d.summary.total}` : ''].filter(Boolean).join(' · ') || '—')}</div>
        </div>
      </div>
      ${activeApps.length ? `<div class="hm-app-pills">
        ${activeApps.map(a => `<span class="hm-app-pill"><i style="background:${esc(a.color || '#94a3b8')}"></i><b>${esc(a.label || '')}</b><small>${esc(a.total || '')}</small></span>`).join('')}
      </div>` : ''}`}
  </section>`;
}

function homeMusicCardHtml() {
  const n = hmState.musicNow || (mxState && mxState.now);
  if (hmState.musicLoading && !n) {
    return `<section class="card hm-card hm-link" data-go="music">
      <div class="hm-card-head">
        <span class="hm-card-title">${ICON.music}<span>${t('mx_playing')}</span></span>
        <a class="hm-more" href="#music">${t('hm_more')}</a>
      </div>
      <div class="mu-skel" style="height:68px"></div>
    </section>`;
  }
  const hasSong = n && n.title;
  const playing = hasSong && n.state === 'playing';
  const tail = (hasSong && !playing && n.ago != null) ? ' · ' + agoText(n.ago) : '';
  const cover = hasSong && n.cover ? pic(n.cover, 120) : '';
  return `<section class="card hm-card hm-link" data-go="music">
    <div class="hm-card-head">
      <span class="hm-card-title">${ICON.music}<span>${playing ? t('mx_playing') : t('mx_last')}</span></span>
      <div class="hm-head-right">
        ${hasSong ? `<span class="dl-pill${playing ? '' : ' is-idle'}">${playing ? t('pill_playing') : t('pill_paused')}</span>` : ''}
        <a class="hm-more" href="#music">${t('hm_more')}</a>
      </div>
    </div>
    ${!hasSong ? `<div class="empty">${ICON.empty}<span>${t('ne_empty')}</span></div>` : `
      <div class="hm-mx-row">
        ${cover
          ? `<img class="hm-mx-cover" src="${esc(cover)}" alt="" loading="lazy" onerror="this.style.opacity='0'">`
          : `<span class="hm-mx-cover is-glyph">♪</span>`}
        <div class="hm-mx-body">
          <div class="hm-mx-title" title="${esc(n.title)}">${esc(n.title)}</div>
          <div class="hm-mx-sub" title="${esc(n.sub || '')}">${esc((n.sub || '—') + tail)}</div>
        </div>
      </div>`}
  </section>`;
}

function homeSysCardHtml() {
  const s = hmState.sys;
  if (hmState.sysLoading && !s) {
    return `<section class="card hm-card hm-link" data-go="stats">
      <div class="hm-card-head">
        <span class="hm-card-title"><span>${t('hm_sys')}</span></span>
        <a class="hm-more" href="#stats">${t('hm_more')}</a>
      </div>
      <div class="mu-skel" style="height:82px"></div>
    </section>`;
  }
  const hosts = (s && Array.isArray(s.hosts)) ? s.hosts : [];
  return `<section class="card hm-card hm-link" data-go="stats">
    <div class="hm-card-head">
      <span class="hm-card-title"><span>${t('hm_sys')}</span></span>
      <a class="hm-more" href="#stats">${t('hm_more')}</a>
    </div>
    ${!hosts.length ? `<div class="empty">${ICON.empty}<span>—</span></div>` : `
      <div class="hm-sys-list">
        ${hosts.map(h => {
          const metrics = [
            { k: t('hm_cpu'), v: h.cpu },
            { k: t('hm_mem'), v: h.mem },
            ...(h.gpu != null ? [{ k: t('hm_gpu'), v: h.gpu }] : []),
          ];
          return `<div class="hm-sys-host">
            <div class="hm-sys-top">
              <span class="hm-sys-name"><i class="dl-dot${h.online ? '' : ' is-off'}"></i>${esc(h.name)}</span>
              ${!h.online
                ? `<span class="hm-sys-off">${t('hm_sys_offline')}</span>`
                : (h.memText ? `<span class="hm-sys-mem">${esc(h.memText)}</span>` : '')}
            </div>
            <div class="hm-sys-bars">
              ${metrics.map(m => {
                const pct = m.v != null ? Math.max(0, Math.min(100, Math.round(Number(m.v)))) : null;
                return `<div class="hm-bar-item">
                  <div class="hm-bar-lbl"><span>${m.k}</span><b>${pct != null ? pct + '%' : '—'}</b></div>
                  <div class="hm-bar-trk"><i style="width:${pct != null ? pct : 0}%"></i></div>
                </div>`;
              }).join('')}
            </div>
          </div>`;
        }).join('')}
      </div>`}
  </section>`;
}

function homeAnimeCardHtml() {
  if (viState.loading && !viState.data) {
    return `<section class="card hm-card hm-link is-wide" data-go="videos">
      <div class="hm-card-head">
        <span class="hm-card-title">${ICON.videos}<span>${t('hm_watching')}</span></span>
        <a class="hm-more" href="#videos">${t('hm_more')}</a>
      </div>
      <div class="hm-vi-grid">
        ${Array.from({ length: 6 }, () => `<div class="mu-skel" style="aspect-ratio:3/4"></div>`).join('')}
      </div>
    </section>`;
  }
  const items = (viState.data && Array.isArray(viState.data.items))
    ? viState.data.items.filter(x => x.status === 'watching').slice(0, 6)
    : [];
  return `<section class="card hm-card hm-link is-wide" data-go="videos">
    <div class="hm-card-head">
      <span class="hm-card-title">${ICON.videos}<span>${t('hm_watching')}</span></span>
      <a class="hm-more" href="#videos">${t('hm_more')}</a>
    </div>
    ${!items.length ? `<div class="empty">${ICON.empty}<span>${t('hm_watching_empty')}</span></div>` : `
      <div class="hm-vi-grid">
        ${items.map(viCardHtml).join('')}
      </div>`}
  </section>`;
}

function paintHomeCards() {
  const hb = $('#hmHealthBox');
  if (hb) hb.innerHTML = healthCardHtml();
  const nb = $('#hmNowBox');
  if (nb) nb.innerHTML = homeNowCardHtml();
  const mb = $('#hmMusicBox');
  if (mb) mb.innerHTML = homeMusicCardHtml();
  const sb = $('#hmSysBox');
  if (sb) sb.innerHTML = homeSysCardHtml();
  const vb = $('#hmAnimeBox');
  if (vb) {
    vb.innerHTML = homeAnimeCardHtml();
    viBindFallbackTimers(vb);
  }
}

function paintHomeHealth() {
  paintHomeCards();
}

async function loadHomeMusic(force = false) {
  if (hmState.musicLoading) return;
  if (!force && hmState.musicNow && (Date.now() - (hmState.musicAt || 0)) < 15000) {
    paintHomeCards();
    return;
  }
  hmState.musicLoading = true;
  paintHomeCards();
  try {
    const n = await fetchNow();
    if (n) {
      hmState.musicNow = n;
      hmState.musicAt = Date.now();
    }
  } finally {
    hmState.musicLoading = false;
    paintHomeCards();
  }
}

async function loadHomeSys(force = false) {
  if (hmState.sysLoading) return;
  if (!force && hmState.sys && (Date.now() - (hmState.sysAt || 0)) < 10000) {
    paintHomeCards();
    return;
  }
  hmState.sysLoading = true;
  paintHomeCards();
  try {
    const devRes = await fetch('api/stats/devices', { credentials: 'same-origin', cache: 'no-store' }).then(r => r.json()).catch(() => null);
    const devs = (devRes && devRes.success && Array.isArray(devRes.data)) ? devRes.data : [];
    const hosts = await Promise.all(devs.map(async dev => {
      const host = encodeURIComponent(dev.ip);
      const [cpuR, memR, gpuR] = await Promise.all([
        fetch(`api/stats/cpu?host=${host}`, { credentials: 'same-origin', cache: 'no-store' }).then(r => r.json()).catch(() => null),
        fetch(`api/stats/memory?host=${host}`, { credentials: 'same-origin', cache: 'no-store' }).then(r => r.json()).catch(() => null),
        dev.os === 'windows'
          ? fetch(`api/stats/gpu?host=${host}`, { credentials: 'same-origin', cache: 'no-store' }).then(r => r.json()).catch(() => null)
          : Promise.resolve(null),
      ]);
      const cd = (cpuR && cpuR.success && cpuR.data) ? cpuR.data : null;
      const cpuRaw = cd ? (cd.usagePercent ?? cd.usage) : null;
      const cpu = cpuRaw != null ? Number(cpuRaw) : null;
      const md = (memR && memR.success && memR.data) ? memR.data : null;
      const memRaw = md ? (md.usagePercent ?? md.usage ?? (md.used != null && md.total > 0 ? (Number(md.used) / Number(md.total)) * 100 : null)) : null;
      const mem = memRaw != null ? Number(memRaw) : null;
      const memText = (md && md.used != null && md.total != null)
        ? `${(Number(md.used) / 1073741824).toFixed(1)}/${(Number(md.total) / 1073741824).toFixed(0)} GB`
        : '';
      const gd = (gpuR && gpuR.success && gpuR.data) ? gpuR.data : null;
      let gpuRaw = gd ? (gd.usagePercent ?? gd.usage) : null;
      if (gpuRaw == null && gd && Array.isArray(gd.engines) && gd.engines.length) {
        gpuRaw = gd.engines.reduce((mx, e) => Math.max(mx, Number(e.usagePercent) || 0), 0);
      }
      const gpu = gpuRaw != null ? Number(gpuRaw) : null;
      const online = !!dev.online && !(cpuR && cpuR.stale);
      const memTotalGB = (md && md.total != null) ? (Number(md.total) / 1073741824).toFixed(0) : null;
      return {
        ip: dev.ip,
        name: dev.name || dev.ip,
        online,
        cpu: online ? cpu : 0,
        mem: online ? mem : 0,
        memText: online ? memText : (memTotalGB != null ? `0.0/${memTotalGB} GB` : ''),
        gpu: online ? gpu : (dev.os === 'windows' ? 0 : null),
      };
    }));
    hmState.sys = { hosts };
    hmState.sysAt = Date.now();
  } finally {
    hmState.sysLoading = false;
    paintHomeCards();
  }
}

function loadHome() {
  loadDaylog();
  loadHomeMusic();
  loadHomeSys();
  if (!viState.data && !viState.loading) {
    loadVideos().then(() => paintHomeCards());
  }
  stopHomeLive();
  hmTimer = setInterval(() => {
    if (document.hidden) return;   // 后台标签页不轮询
    if (!document.getElementById('hmHealthBox')) {
      stopHomeLive();
      return;
    }
    loadDaylog(true);
    loadHomeMusic(true);
    loadHomeSys(true);
  }, 20000);
}

function pageHome() {
  return `
  <div class="home-grid">
    <section class="card profile-card hm-link" data-go="account">
      <div class="profile-avatar">${avatarSelf(96)}</div>
      <div class="profile-name">${esc(SITE.name)}</div>
    </section>
    <div id="hmHealthBox" class="hm-slot">${healthCardHtml()}</div>
  </div>
  <div class="home-showcase">
    <div id="hmNowBox" class="hm-slot">${homeNowCardHtml()}</div>
    <div id="hmMusicBox" class="hm-slot">${homeMusicCardHtml()}</div>
    <div id="hmSysBox" class="hm-slot">${homeSysCardHtml()}</div>
    <div id="hmAnimeBox" class="hm-slot is-wide">${homeAnimeCardHtml()}</div>
  </div>`;
}
