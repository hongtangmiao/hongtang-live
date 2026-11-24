'use strict';
// 路由，统计面板的中文化与初始化
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。
/* ---------------- router ---------------- */
const PANELS = {
  home:      { key: 'nav_home',      sub: null,          render: pageHome },
  now:       { key: 'nav_now',       sub: 'nw_sub',      render: pageNow },
  tweets:    { key: 'nav_tweets',    sub: 'tw_sub',      render: pageTweets },
  music:     { key: 'nav_music',     sub: 'mu_sub',      render: pageMusic },
  illust:    { key: 'nav_illust',    sub: 'il_sub',      render: pageIllust },
  videos:    { key: 'nav_videos',    sub: 'vi_sub',      render: pageVideos },
  account:   { key: 'nav_account',   sub: null,          render: pageAccount },
  stats:     { key: 'nav_stats',     sub: null,          render: pageStats, crumb: 'crumb_server' },
};
const ALIAS = { tsubuyaki: 'tweets', video: 'videos', illusts: 'illust', about: 'account', ima: 'now', live: 'now' };

function currentRoute() {
  const def = window.HL_DEFAULT_ROUTE === 'now' ? 'now' : 'home';   // live.xxx（原 daylog 的域名）默认进「此刻」
  const raw = (location.hash || '#' + def).slice(1) || def;
  const id = ALIAS[raw] || raw;
  return PANELS[id] ? id : 'home';
}

function applyStatic() {
  root.lang = lang === 'zh' ? 'zh-CN' : 'ja';
  $$('[data-i18n]').forEach(el => { el.textContent = t(el.dataset.i18n); });
  $$('.lang-toggle [data-lang]').forEach(el => el.classList.toggle('on', el.dataset.lang === lang));
  const tb = $('#themeBtn');
  if (tb) {
    const label = lang === 'zh' ? '切换主题' : 'テーマを切り替え';
    tb.title = label; tb.setAttribute('aria-label', label);
  }
}

function syncNav(route) {
  const brand = $('.brand');
  if (brand) brand.classList.toggle('active', route === 'account');
  $$('.nav-item').forEach(a => a.classList.toggle('active', a.dataset.tab === route));
  $$('.bottom-nav-item').forEach(a => a.classList.toggle('active', a.dataset.tab === route));
}

/* 统计面板初始化与重绘（stats.js 原生支持日/中双语） */
let statsInited = false;
function ensureStatsInit() {
  if (statsInited) return;
  if (typeof initStats !== 'function') {
    setTimeout(ensureStatsInit, 200);
    return;
  }
  statsInited = true;
  try { initStats(); } catch (e) { console.error(e); }
}

function render(keepScroll) {
  const prevY = view.scrollTop;
  const route = currentRoute();
  const panel = PANELS[route];

  applyStatic();
  stopMxLive();

  /* 統計パネルは静的 DOM を付け外しして使い回す（stats.js が参照を掴んでいるため） */
  const statsPanel = document.getElementById('panel-stats');
  const hold = document.getElementById('stats-hold');
  if (statsPanel && hold && statsPanel.classList.contains('live')) {
    statsPanel.classList.remove('live');
    hold.appendChild(statsPanel);
  }

  const titleEl = $('#topbarTitle');
  if (panel.crumb) {
    titleEl.innerHTML = `<a class="crumb" href="#account">${t(panel.crumb)}</a>` +
      `<span class="crumb-sep">›</span><span>${t(panel.key)}</span>`;
  } else {
    titleEl.textContent = t(panel.key);
  }
  const sub = panel.sub ? t(panel.sub) : '';
  const subEl = $('#topbarSub');
  subEl.textContent = sub;
  subEl.style.display = sub ? '' : 'none';
  const tail = SITE.domain || SITE.title || SITE.name;
  document.title = route === 'home'
    ? ([SITE.title || SITE.name, SITE.domain].filter(Boolean).join(' | ') || t(panel.key))
    : [t(panel.key), tail].filter(Boolean).join(' | ');

  view.innerHTML = panel.render();
  if (route === 'stats' && statsPanel && hold) {
    view.appendChild(statsPanel);
    statsPanel.classList.add('live');
    ensureStatsInit();
    if (typeof window.rerenderStats === 'function') window.rerenderStats();
  }
  view.classList.remove('in');
  void view.offsetWidth;
  view.classList.add('in');
  syncNav(route);

  view.scrollTop = keepScroll ? prevY : 0;

  /* パネル固有の後処理 */
  if (route === 'home') loadHome();
  if (route === 'music') loadMusic();
  if (route === 'videos') loadVideos();
  if (route === 'tweets') { loadDaylog(true); startDlLive(); }
  if (route === 'now') { loadNow(); startNowLive(); } else stopNowLive();

  /* 网易云ログイン導線 */
  $$('[data-ne-open], [data-ne-login]').forEach(b => b.addEventListener('click', openNeModal));
  $$('[data-ne-logout]').forEach(b => b.addEventListener('click', async () => {
    await NET.logout();
    toast(t('ne_logout_ok'));
    render(true);
  }));
  if (route === 'account') {
    $$('[data-go]').forEach(b => b.addEventListener('click', () => { location.hash = '#' + b.dataset.go; }));
    $$('[data-href]').forEach(b => b.addEventListener('click', () => {
      const u = b.dataset.href;
      if (/^https?:\/\//.test(u)) window.open(u, '_blank', 'noopener');
      else location.href = u; // mailto:
    }));
  }
}

