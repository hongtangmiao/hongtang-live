'use strict';
// 番剧页（bgm.tv + Jellyfin 海报墙）
/* ---------------- videos（bgm.tv + Jellyfin 番剧海报墙） ---------------- */
let viState = {
  data: null,
  loading: false,
  error: null,
  statusFilter: 'all',
  sourceFilter: 'all',
};

function viFallbackImg(img) {
  if (!img) return;
  const fb = img.dataset.fallback || '';
  if (fb && img.getAttribute('src') !== fb) {
    img.dataset.fallback = '';
    img.src = fb;
  } else {
    img.style.opacity = '0';
  }
}

function viBindFallbackTimers(scope) {
  $$('img[data-fallback]', scope || document).forEach(img => {
    const fb = img.dataset.fallback || '';
    if (!fb || img.dataset.fbTimer) return;
    img.dataset.fbTimer = '1';
    setTimeout(() => {
      if (!img.isConnected) return;
      if (!img.complete || img.naturalWidth === 0) viFallbackImg(img);
    }, 3500);
  });
}

// 观看集数以 BGM 为准，没有的保底才用 JF 的
function viEpText(it) {
  if (it.status === 'watching') {
    if (it.epTotal > 0) return `${it.epWatched || 0}/${it.epTotal}${t('vi_ep')}`;
    if (it.epWatched > 0) return `${it.epWatched}${t('vi_ep')}`;
    return it.jfNextEp || '';
  }
  return it.epTotal > 0 ? `${it.epTotal}${t('vi_ep')}` : '';
}

function viCardHtml(it, preferMine = false) {
  const srcs = Array.isArray(it.sources) ? it.sources : [];
  const badges = [
    srcs.includes('bgm') ? `<span class="vi-src-tag bgm">BGM</span>` : '',
    srcs.includes('jellyfin') ? `<span class="vi-src-tag jf">JF</span>` : '',
  ].filter(Boolean).join('');

  // 评分：默认显示大众分（bgm.tv / Jellyfin），鼠标移到卡片上换成自己的分；
  // 推荐区（preferMine=true）反过来默认显示我的评分，悬停换大众分。
  const onlyJf = !srcs.includes('bgm');
  const scoreTxt = it.score > 0 ? Number(it.score).toFixed(1) : '';
  const scoreLabel = onlyJf ? t('vi_jf_score') : t('vi_bgm_score');
  let rating = '';
  if (scoreTxt && it.rate > 0) {
    const tip = preferMine
      ? `${t('vi_my_rate')} ${it.rate} · ${scoreLabel} ${scoreTxt}`
      : `${t('vi_all_rate')} ${scoreLabel} ${scoreTxt} · ${t('vi_my_rate')} ${it.rate}`;
    rating = `<span class="vi-rate both${preferMine ? ' prefer-mine' : ''}" title="${esc(tip)}"><span class="vi-r all">★ ${scoreTxt}</span><span class="vi-r mine">★ ${esc(String(it.rate))}</span></span>`;
  } else if (it.rate > 0) {
    rating = `<span class="vi-rate mine" title="${esc(t('vi_my_rate') + ' ' + it.rate)}">★ ${esc(String(it.rate))}</span>`;
  } else if (scoreTxt) {
    rating = `<span class="vi-rate dim" title="${esc(scoreLabel + ' ' + scoreTxt)}">★ ${scoreTxt}</span>`;
  }

  const epText = viEpText(it);

  const cover = it.cover || it.fallbackCover || '';
  const fallback = (it.cover && it.fallbackCover && it.cover !== it.fallbackCover) ? it.fallbackCover : '';

  // 封面单独成一个框，上面不压任何东西：评分和来源标签都排到下面同一行
  const inner = `
    <div class="vi-poster">
      <div class="vi-poster-glyph">${ICON.videos}</div>
      ${cover ? `<img src="${esc(cover)}" data-fallback="${esc(fallback)}" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer" onerror="viFallbackImg(this)">` : ''}
    </div>
    <div class="vi-info">
      <div class="vi-head">
        <span class="vi-title" title="${esc(it.title || '')}">${esc(it.title || '')}</span>
        ${epText ? `<span class="vi-ep" title="${esc(epText)}">${esc(epText)}</span>` : ''}
      </div>
      ${it.origTitle ? `<div class="vi-orig" title="${esc(it.origTitle)}">${esc(it.origTitle)}</div>` : ''}
      ${(badges || rating) ? `<div class="vi-meta">${badges ? `<span class="vi-badges">${badges}</span>` : ''}${rating}</div>` : ''}
    </div>`;

  if (it.bgmUrl) {
    return `<a class="vi-card" href="${esc(it.bgmUrl)}" target="_blank" rel="noreferrer">${inner}</a>`;
  }
  return `<div class="vi-card">${inner}</div>`;
}

function viRecommendedItems(list) {
  return (list || [])
    .filter(x => Number(x.rate) > 0)
    .slice()
    .sort((a, b) =>
      (Number(b.rate) - Number(a.rate))
      || ((Number(b.score) || 0) - (Number(a.score) || 0))
      || String(b.updatedAt || '').localeCompare(String(a.updatedAt || ''))
    );
}

function videosContent() {
  if (viState.loading && !viState.data) {
    return `<div class="vi-grid">
      ${Array.from({ length: 12 }, () => `<div class="mu-skel" style="aspect-ratio:2/3"></div>`).join('')}
    </div>`;
  }
  const d = viState.data;
  if (!d || !d.ok) {
    return `<div class="empty">${ICON.empty}<span>${esc(viState.error || t('vi_err'))}</span></div>`;
  }

  const stats = d.stats || {};
  const srcs = d.sources || {};
  const bgmOn = !!(srcs.bgm && srcs.bgm.ok);
  const jfOn = !!(srcs.jellyfin && srcs.jellyfin.ok);
  const allItems = d.items || [];
  const recTotal = allItems.filter(x => Number(x.rate) > 0).length;

  const bySrc = allItems.filter(it =>
    viState.sourceFilter === 'all' || (it.sources || []).includes(viState.sourceFilter)
  );
  const recommended = viRecommendedItems(bySrc);
  const watching = (viState.statusFilter === 'all' || viState.statusFilter === 'watching')
    ? bySrc.filter(x => x.status === 'watching') : [];
  const watched = (viState.statusFilter === 'all' || viState.statusFilter === 'watched')
    ? bySrc.filter(x => x.status === 'watched') : [];
  const recShown = viState.statusFilter === 'recommended'
    ? recommended
    : (viState.statusFilter === 'all' ? recommended.slice(0, 12) : []);
  const hasAny = watching.length || recShown.length || watched.length;

  const stChip = (k, label) =>
    `<button class="pill${viState.statusFilter === k ? ' on' : ''}" type="button" data-vi-status="${k}">${label}</button>`;
  const srcChip = (k, label) =>
    `<button class="mx-chip${viState.sourceFilter === k ? ' on' : ''}" type="button" data-vi-src="${k}">${label}</button>`;
  const recMore = (viState.statusFilter === 'all' && recommended.length > recShown.length)
    ? `<button class="mx-chip" type="button" data-vi-status="recommended">${t('hm_more')}</button>` : '';

  return `
    <div class="vi-head">
      <div class="vi-stats">
        ${mxTile(ICON.videos, String(stats.watching || 0), t('vi_watching'), 'blue')}
        ${mxTile(ICON.star, String(stats.watched || 0), t('vi_watched'))}
        ${mxTile(ICON.list, String(stats.total || 0), t('vi_total'))}
      </div>
      <div class="vi-meta">
        ${d.bgmProfileUrl
          ? `<a class="vi-src-pill" href="${esc(d.bgmProfileUrl)}" target="_blank" rel="noreferrer"><i class="vi-dot${bgmOn ? ' on' : ''}"></i>bgm.tv${ICON.external}</a>`
          : `<span class="vi-src-pill"><i class="vi-dot${bgmOn ? ' on' : ''}"></i>bgm.tv</span>`}
        <span class="vi-src-pill"><i class="vi-dot${jfOn ? ' on' : ''}"></i>Jellyfin</span>
        <button class="vi-refresh${viState.loading ? ' is-spin' : ''}" type="button" id="viRefresh">${ICON.refresh}<span>${t('vi_refresh')}</span></button>
      </div>
    </div>
    <div class="vi-bar">
      <div class="pill-group">
        ${stChip('all', `${t('vi_all')} (${stats.total || 0})`)}
        ${stChip('watching', `${t('vi_watching')} (${stats.watching || 0})`)}
        ${stChip('recommended', `${t('vi_recommend')} (${recTotal})`)}
        ${stChip('watched', `${t('vi_watched')} (${stats.watched || 0})`)}
      </div>
      <div class="mx-chips vi-src-chips">
        ${srcChip('all', t('vi_src_all'))}
        ${srcChip('bgm', 'bgm.tv')}
        ${srcChip('jellyfin', 'Jellyfin')}
      </div>
    </div>
    ${!hasAny ? `<div class="empty">${ICON.empty}<span>${t('vi_empty')}</span></div>` : `
      <div class="vi-body">
        ${watching.length ? `
          ${mxSec(ICON.videos, `${t('vi_watching')} (${watching.length})`)}
          <div class="vi-grid">${watching.map(viCardHtml).join('')}</div>
        ` : ''}
        ${recShown.length ? `
          ${mxSec(ICON.heart, `${t('vi_recommend')} (${recommended.length})`, recMore)}
          <div class="vi-grid">${recShown.map(it => viCardHtml(it, true)).join('')}</div>
        ` : ''}
        ${watched.length ? `
          ${mxSec(ICON.star, `${t('vi_watched')} (${watched.length})`)}
          <div class="vi-grid">${watched.map(viCardHtml).join('')}</div>
        ` : ''}
      </div>`}`;
}

function paintVideos() {
  const box = $('#viRoot');
  if (!box) return;
  box.innerHTML = videosContent();
  viBindFallbackTimers(box);
  $$('[data-vi-status]', box).forEach(b => b.addEventListener('click', () => {
    viState.statusFilter = b.dataset.viStatus;
    paintVideos();
  }));
  $$('[data-vi-src]', box).forEach(b => b.addEventListener('click', () => {
    viState.sourceFilter = b.dataset.viSrc;
    paintVideos();
  }));
  const rf = $('#viRefresh', box);
  if (rf) rf.addEventListener('click', () => loadVideos(true));
}

async function loadVideos(force = false) {
  if (viState.loading) return;
  if (!force && viState.data) {
    paintVideos();
    return;
  }
  viState.loading = true;
  viState.error = null;
  paintVideos();
  try {
    const r = await fetch('api/videos/wall' + (force ? '?refresh=1' : ''), { credentials: 'same-origin' });
    const d = await r.json();
    if (!r.ok || !d || !d.ok) throw new Error((d && d.error) || 'HTTP ' + r.status);
    viState.data = d;
  } catch (e) {
    viState.error = e.message || String(e);
  } finally {
    viState.loading = false;
    paintVideos();
  }
}

function pageVideos() {
  return `<div id="viRoot">${videosContent()}</div>`;
}

function acctCard({ icon, iconSvg, avatar, title, sub, trail, route, href }) {
  const attr = href ? `data-href="${esc(href)}"` : route ? `data-go="${route}"` : '';
  return `<button class="acct-card" type="button" ${attr}>
    ${avatar ? `<span class="acct-card-avatar">${avatar}</span>`
             : `<span class="acct-card-icon">${iconSvg || ICON[icon]}</span>`}
    <span class="acct-card-text"><b>${esc(title)}</b>${sub ? `<small>${esc(sub)}</small>` : ''}</span>
    <span class="acct-trail">${trail}</span>
  </button>`;
}

/* 账户页「关于我」：设置页里填的链接 + 邮箱，都没填就整块不显示 */
function aboutCards() {
  const cards = (SITE.links || []).filter(l => l && l.url).map(l => acctCard({
    avatar: l.avatar ? avatarSelf(28) : null, icon: l.avatar ? null : 'globe',
    title: l.title || l.url, sub: l.sub || '', trail: ICON.external, href: l.url,
  }));
  if (SITE.email) {
    cards.push(acctCard({ icon: 'mail', title: t('mail'), sub: SITE.email, trail: ICON.external, href: 'mailto:' + SITE.email }));
  }
  if (!cards.length) return '';
  return `<div class="acct-label">${esc(t('ac_info'))}</div>
  <div class="acct-list">${cards.join('')}</div>`;
}

function pageAccount() {
  return `
  <div class="acct-head">
    <div class="acct-avatar is-user">${avatarSelf(96)}</div>
    <div class="acct-id">
      <div class="acct-name">${t('guest')}</div>
      <div class="acct-sub">${t('not_logged')}</div>
    </div>
  </div>

  <div class="acct-label">${t('ne_group')}</div>
  <div class="acct-list">${neCard()}</div>

  ${aboutCards()}

  <div class="acct-label">${t('ac_server')}</div>
  <div class="acct-list">
    ${acctCard({ icon: 'info', title: t('ac_stats'), sub: t('ac_stats_sub'), trail: ICON.chevron, route: 'stats' })}
    ${acctCard({ icon: 'globe', title: t('ac_status'), sub: t('ac_status_sub'), trail: ICON.chevron, route: 'status' })}
  </div>`;
}
