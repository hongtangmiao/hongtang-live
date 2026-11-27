'use strict';
// 底部导航、全局事件与启动（boot）
/* ---------------- bottom nav (mobile) ---------------- */
function buildBottomNav() {
  const items = [['home', 'nav_home'], ['now', 'nav_now'], ['music', 'nav_music'], ['videos', 'nav_videos']];
  const nav = document.createElement('nav');
  nav.className = 'bottom-nav';
  nav.setAttribute('aria-label', 'ボトムナビゲーション');
  nav.innerHTML = items.map(([id, key]) => `
    <button class="bottom-nav-item" type="button" data-tab="${id}">
      <img class="nav-ico" src="icons/${id === 'videos' ? 'video' : id}.png" alt="">
      <span data-i18n="${key}">${t(key)}</span>
    </button>`).join('');
  document.body.appendChild(nav);
  $$('.bottom-nav-item').forEach(b => b.addEventListener('click', () => {
    location.hash = '#' + b.dataset.tab;
  }));
}

/* ---------------- boot ---------------- */
buildBottomNav();
$$('[data-i18n]').forEach(el => { if (!el.textContent.trim()) el.textContent = t(el.dataset.i18n); });

// 侧栏和手机顶栏各一个切换按钮，行为一样
$$('.lang-toggle').forEach(btn => btn.addEventListener('click', () => {
  lang = lang === 'ja' ? 'zh' : 'ja';
  store.set('amk-lang', lang);
  if (q.has('lang')) {            // 网址里带了 ?lang= 的话一起改掉，免得刷新又变回去
    q.set('lang', lang);
    history.replaceState(null, '', '?' + q + location.hash);
  }
  render(true);
  if (typeof window.rerenderStats === 'function') window.rerenderStats();
}));

/* サイドバーのブランド（アバター＋名前）→ アカウント */
$$('[data-route]').forEach(el => el.addEventListener('click', () => {
  const to = '#' + el.dataset.route;
  if (location.hash === to) render(false);
  else location.hash = to;
}));

document.addEventListener('click', (e) => {
  const viGo = e.target.closest('[data-vi-go]');
  if (viGo && typeof viState !== 'undefined') {
    viState.statusFilter = viGo.dataset.viGo || 'all';
  }
  const hmCard = e.target.closest('.hm-link[data-go]');
  if (hmCard && !e.target.closest('a[href], button')) {
    location.hash = '#' + hmCard.dataset.go;
  }
});

/* 画像の読み込み失敗はグレーのプレースホルダに落とす */
document.addEventListener('error', (e) => {
  const el = e.target;
  if (el && el.tagName === 'IMG' && el.dataset.fb !== '1') { el.dataset.fb = '1'; el.style.visibility = 'hidden'; }
}, true);

window.addEventListener('hashchange', () => render(false));

let __booted = false;
async function boot() {
  if (__booted) return;
  __booted = true;
  await NET.detect();      // 有 Go 服务端就走同源网关，否则直连上游
  render(false);
  // X2 模式在 13-ui-macos.js 同步初始化时 NET.detect() 尚未完成，非 #home 入口需补拉一次音乐状态
  if (window.HL_UI && window.HL_UI.getMode() === 'x2') {
    if (typeof loadHomeMusic === 'function') loadHomeMusic(true);
    if (typeof loadUptime === 'function') loadUptime();
  }
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
else boot();
/* デバッグ用フック（?debug=1 のときだけ公開） */
if (q.get('debug') === '1') {
  window.__amk = {
    NET, musicPage, bindMusic,
    getState: () => mxState,
    setState: (s) => { mxState = s; },
    paint: () => { const b = document.getElementById('muRoot'); b.innerHTML = musicPage(mxState); bindMusic(); },
  };
}

