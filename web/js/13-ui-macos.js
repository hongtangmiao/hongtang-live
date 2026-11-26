'use strict';
// X2 macOS 桌面工作台：窗口化显示总结，绿色全屏显示完整详情，Tabler 图标 + 中日切换
(function () {
  const safe = s => String(s == null ? '' : s).replace(/[&<>"']/g, m => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));
  const isZh = () => (typeof lang !== 'undefined' ? lang === 'zh' : true);

  let x2Z = 20;
  let x2UserMoved = false;
  let x2MaxId = null;
  let x2Animating = false;

  const x2Wins = [
    { id: 'now',    ti: 'ti-clock',     zh: '此刻状态',   ja: 'いまの活動',     route: 'now',    x: 24,  y: 44,  w: 376 },
    { id: 'health', ti: 'ti-heartbeat', zh: '身体健康',   ja: 'ヘルスケア',     route: 'now',    x: 416, y: 44,  w: 420 },
    { id: 'music',  ti: 'ti-music',     zh: '音乐',       ja: '音楽',           route: 'music',  x: 852, y: 44,  w: 400 },
    { id: 'hosts',  ti: 'ti-cpu',       zh: '活动监视器', ja: 'システムリソース', route: 'stats',  x: 24,  y: 374, w: 376 },
    { id: 'anime',  ti: 'ti-device-tv', zh: '追番书架',   ja: 'アニメ',         route: 'videos', x: 416, y: 268, w: 836 },
  ];
  const x2State = {};
  x2Wins.forEach(w => { x2State[w.id] = { open: true, minimized: false, max: false }; });

  /* 计算窗口飞缩到 Dock 对应缩略图（或图标）的纯 GPU transform 偏移 */
  function setDockVector(id, win) {
    const target = document.querySelector(`#x2DockTray .x2-thumb[data-id="${id}"]`)
      || document.querySelector(`#x2Dock .x2-ditem[data-id="${id}"]`);
    if (!target || !win) return;
    const wr = win.getBoundingClientRect();
    const ir = target.getBoundingClientRect();
    const dx = Math.round((ir.left + ir.width / 2) - (wr.left + wr.width / 2));
    const dy = Math.round((ir.top + ir.height / 2) - (wr.top + wr.height / 2));
    win.style.setProperty('--min-x', dx + 'px');
    win.style.setProperty('--min-y', dy + 'px');
  }

  /* 渲染 Dock 右侧最小化窗口缩略图托盘 */
  function syncDockTray() {
    const tray = document.getElementById('x2DockTray');
    if (!tray) return;
    const mins = x2Wins.filter(w => x2State[w.id].minimized);
    tray.innerHTML = mins.map(w => `
      <button class="x2-thumb" type="button" data-thumb="${w.id}" data-id="${w.id}" title="${safe(winTitle(w))}">
        <span class="x2-thumb-tip">${safe(winTitle(w))}</span>
        <span class="x2-thumb-bar"><i style="background:#ff5f57"></i><i style="background:#febc2e"></i><i style="background:#28c840"></i></span>
        <span class="x2-thumb-body"><s style="width:72%"></s><s style="width:92%"></s><s style="width:55%"></s></span>
        <span class="x2-thumb-badge"><i class="ti ${w.ti}"></i></span>
      </button>`).join('');
  }

  const winTitle = w => (isZh() ? w.zh : w.ja);

  function layoutDefault() {
    if (window.innerWidth <= 860 || x2MaxId || x2Animating) return;
    const vw = window.innerWidth;
    const pad = 20, gap = 16;
    const maxW = Math.min(1680, vw - pad * 2);
    const ox = Math.max(pad, Math.round((vw - maxW) / 2));
    const leftW = Math.round(maxW * 0.30);
    const rightW = maxW - leftW - gap;
    const halfR = Math.floor((rightW - gap) / 2);

    const setBox = (id, x, y, w) => {
      const el = document.getElementById('x2w-' + id);
      if (!el || el.classList.contains('is-max')) return;
      el.style.left = x + 'px';
      el.style.top = y + 'px';
      el.style.width = w + 'px';
    };
    setBox('now', ox, 44, leftW);
    setBox('health', ox + leftW + gap, 44, halfR);
    setBox('music', ox + leftW + gap + halfR + gap, 44, rightW - halfR - gap);

    const wNow = document.getElementById('x2w-now');
    const wHealth = document.getElementById('x2w-health');
    const wMusic = document.getElementById('x2w-music');
    const leftY = wNow ? (44 + wNow.offsetHeight + gap) : 368;
    const rightY = Math.max(
      wHealth ? (44 + wHealth.offsetHeight + gap) : 250,
      wMusic ? (44 + wMusic.offsetHeight + gap) : 250
    );
    setBox('hosts', ox, leftY, leftW);
    setBox('anime', ox + leftW + gap, rightY, rightW);
  }

  function returnStatsToHold() {
    const sp = document.getElementById('panel-stats');
    const hold = document.getElementById('stats-hold');
    if (sp && hold && sp.parentElement !== hold) {
      sp.classList.remove('live');
      hold.appendChild(sp);
    }
  }

  function mountDetail(id) {
    const body = document.getElementById('x2b-' + id);
    if (!body) return;
    returnStatsToHold();
    const w = x2Wins.find(x => x.id === id);
    if (!w) return;
    const rt = w.route;

    if (rt === 'stats') {
      body.innerHTML = '<div class="x2-detail" id="x2DetailStats"></div>';
      const box = document.getElementById('x2DetailStats');
      const sp = document.getElementById('panel-stats');
      if (sp && box) {
        box.appendChild(sp);
        sp.classList.add('live');
        if (typeof ensureStatsInit === 'function') ensureStatsInit();
        if (typeof window.rerenderStats === 'function') window.rerenderStats();
      }
      return;
    }
    if (rt === 'now' && typeof pageNow === 'function') {
      body.innerHTML = `<div class="x2-detail">${pageNow()}</div>`;
      if (typeof loadNow === 'function') loadNow();
      return;
    }
    if (rt === 'music' && typeof pageMusic === 'function') {
      body.innerHTML = `<div class="x2-detail">${pageMusic()}</div>`;
      if (typeof loadMusic === 'function') loadMusic();
      return;
    }
    if (rt === 'videos' && typeof pageVideos === 'function') {
      body.innerHTML = `<div class="x2-detail">${pageVideos()}</div>`;
      if (typeof loadVideos === 'function') loadVideos();
    }
  }

  function updateTitlebarLabels() {
    x2Wins.forEach(w => {
      const btn = document.querySelector(`#x2w-${w.id} .x2-wact`);
      const ttl = document.querySelector(`#x2w-${w.id} .x2-wtitle`);
      if (ttl) ttl.textContent = winTitle(w);
      if (btn) btn.textContent = x2State[w.id].max ? (isZh() ? '退出全屏' : '戻る') : (isZh() ? '全屏详情' : '詳細');
    });
  }

  /* 纯 GPU FLIP 动画（只动画 transform，零重排；动画结束后再挂载重 DOM 详情，彻底消除掉帧） */
  function flipWindow(win, mutate, onDone) {
    const first = win.getBoundingClientRect();
    mutate();
    const last = win.getBoundingClientRect();
    const dx = first.left - last.left;
    const dy = first.top - last.top;
    const sx = last.width ? first.width / last.width : 1;
    const sy = last.height ? first.height / last.height : 1;
    win.style.transition = 'none';
    win.style.transform = `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})`;
    win.classList.add('is-flipping');
    void win.offsetWidth;
    win.style.transition = '';
    win.style.transform = '';
    setTimeout(() => {
      win.classList.remove('is-flipping');
      if (onDone) onDone();
    }, 330);
  }

  function toggleFullscreen(id) {
    const rootEl = document.getElementById('x2Root');
    const win = document.getElementById('x2w-' + id);
    if (!win) return;

    const entering = !x2State[id].max;
    x2Wins.forEach(w => {
      if (w.id !== id && x2State[w.id].max) {
        x2State[w.id].max = false;
        const el = document.getElementById('x2w-' + w.id);
        if (el) el.classList.remove('is-max');
      }
    });
    returnStatsToHold();

    if (entering) {
      x2Animating = true;
      x2MaxId = id;
      x2State[id].open = true;
      x2State[id].minimized = false;
      x2State[id].max = true;
      win.style.zIndex = ++x2Z;
      if (rootEl) rootEl.classList.add('has-max');
      syncDockTray();
      flipWindow(win, () => {
        win.classList.remove('is-closed', 'is-minimized');
        win.classList.add('is-max');
      }, () => {
        x2Animating = false;
        if (x2MaxId === id) mountDetail(id);
      });
    } else {
      x2Animating = true;
      x2MaxId = null;
      x2State[id].max = false;
      if (rootEl) rootEl.classList.remove('has-max');
      // 先切回轻量总结内容再跑 FLIP 缩小，零重 DOM 卡顿
      paint();
      flipWindow(win, () => {
        win.classList.remove('is-max');
      }, () => {
        x2Animating = false;
        if (!x2UserMoved && !x2MaxId) layoutDefault();
      });
    }
    updateTitlebarLabels();
    syncDots();
  }

  function ensureDom() {
    let rootEl = document.getElementById('x2Root');
    if (rootEl) return rootEl;
    rootEl = document.createElement('div');
    rootEl.id = 'x2Root';
    const siteName = (window.HL_SITE && window.HL_SITE.name) || '红糖';
    rootEl.innerHTML = `
      <header class="x2-menubar">
        <div class="x2-mb-l">
          <span class="x2-mb-app"><i class="ti ti-brand-apple"></i>${safe(siteName)} OS</span>
          <button class="x2-mb-btn" id="x2ResetTop" type="button"></button>
        </div>
        <div class="x2-mb-r">
          <span id="x2MbStatus"><i class="x2-dot"></i></span>
          <span id="x2MbHr"></span>
          <button class="x2-mb-btn" id="x2LangBtn" type="button"><i class="ti ti-language"></i><span id="x2LangTxt"></span></button>
          <button class="x2-mb-btn" id="x2ToneTop" type="button"></button>
          <span id="x2MbClock"></span>
        </div>
      </header>
      <div class="x2-desk" id="x2Desk"></div>
      <div class="x2-dock-wrap"><nav class="x2-dock" id="x2Dock"></nav></div>`;
    document.body.appendChild(rootEl);

    const desk = rootEl.querySelector('#x2Desk');
    desk.innerHTML = x2Wins.map((w, i) => `
      <section class="x2-win" id="x2w-${w.id}" style="left:${w.x}px;top:${w.y}px;width:${w.w}px;z-index:${i + 1}">
        <div class="x2-tbar" data-dbl="${w.id}">
          <div class="x2-lights">
            <button class="x2-l-r" type="button" data-act="close" data-id="${w.id}" title="关闭"></button>
            <button class="x2-l-y" type="button" data-act="min" data-id="${w.id}" title="最小化"></button>
            <button class="x2-l-g" type="button" data-act="max" data-id="${w.id}" title="全屏详情 / 还原总结"></button>
          </div>
          <span class="x2-wtitle">${winTitle(w)}</span>
          <button class="x2-wact" type="button" data-act="max" data-id="${w.id}"></button>
        </div>
        <div class="x2-body" id="x2b-${w.id}"></div>
      </section>`).join('');

    rootEl.querySelector('#x2Dock').innerHTML = x2Wins.map(w =>
      `<button class="x2-ditem open" type="button" data-id="${w.id}"><i class="ti ${w.ti}"></i><span>${winTitle(w)}</span></button>`
    ).join('') + `<div class="x2-dsep"></div><div class="x2-dock-tray" id="x2DockTray"></div><button class="x2-ditem" type="button" id="x2DockReset"><i class="ti ti-refresh"></i><span>还原窗口</span></button>`;

    desk.querySelectorAll('.x2-win').forEach(win => {
      win.addEventListener('pointerdown', () => { if (!win.classList.contains('is-max')) win.style.zIndex = ++x2Z; });
      const bar = win.querySelector('.x2-tbar');
      bar.addEventListener('pointerdown', e => {
        if (e.target.closest('button') || window.innerWidth <= 860 || win.classList.contains('is-max')) return;
        win.classList.add('dragging');
        x2UserMoved = true;
        const sx = e.clientX - win.offsetLeft;
        const sy = e.clientY - win.offsetTop;
        const mv = ev => {
          win.style.left = Math.max(8, ev.clientX - sx) + 'px';
          win.style.top = Math.max(36, ev.clientY - sy) + 'px';
        };
        const up = () => {
          win.classList.remove('dragging');
          window.removeEventListener('pointermove', mv);
          window.removeEventListener('pointerup', up);
        };
        window.addEventListener('pointermove', mv);
        window.addEventListener('pointerup', up);
      });
    });

    desk.addEventListener('dblclick', e => {
      const tb = e.target.closest('.x2-tbar[data-dbl]');
      if (!tb || e.target.closest('button')) return;
      toggleFullscreen(tb.dataset.dbl);
    });

    desk.addEventListener('click', e => {
      const b = e.target.closest('button[data-act]');
      if (!b) return;
      const id = b.dataset.id, act = b.dataset.act;
      const win = document.getElementById('x2w-' + id);
      if (act === 'close') {
        if (x2State[id].max) toggleFullscreen(id);
        x2State[id].open = false;
        x2State[id].minimized = false;
        win.classList.remove('is-minimized');
        win.classList.add('is-closed');
        syncDots();
      } else if (act === 'min') {
        if (x2State[id].max) toggleFullscreen(id);
        x2State[id].open = true;
        x2State[id].minimized = true;
        syncDockTray();
        setDockVector(id, win);
        win.classList.remove('is-closed');
        win.classList.add('is-minimized');
        syncDots();
      } else if (act === 'max') {
        toggleFullscreen(id);
      }
    });

    rootEl.querySelector('#x2Dock').addEventListener('click', e => {
      const thumb = e.target.closest('.x2-thumb[data-id]');
      if (thumb) {
        const id = thumb.dataset.id;
        const win = document.getElementById('x2w-' + id);
        x2State[id].minimized = false;
        win.classList.remove('is-minimized');
        win.style.zIndex = ++x2Z;
        syncDockTray();
        syncDots();
        return;
      }
      const it = e.target.closest('.x2-ditem');
      if (!it) return;
      if (it.id === 'x2DockReset') { resetAll(); return; }
      const id = it.dataset.id;
      const win = document.getElementById('x2w-' + id);
      if (x2MaxId && x2MaxId !== id) {
        toggleFullscreen(id);
        return;
      }
      if (x2State[id].minimized) {
        x2State[id].minimized = false;
        win.classList.remove('is-minimized');
        syncDockTray();
      } else if (!x2State[id].open) {
        x2State[id].open = true;
        win.classList.remove('is-closed');
      }
      win.style.zIndex = ++x2Z;
      syncDots();
    });

    rootEl.querySelector('#x2ResetTop').addEventListener('click', resetAll);
    rootEl.querySelector('#x2ToneTop').addEventListener('click', () => { if (window.HL_UI) window.HL_UI.toggleX2Tone(); });
    rootEl.querySelector('#x2LangBtn').addEventListener('click', () => {
      const btn = document.querySelector('.lang-toggle');
      if (btn) btn.click();
      setTimeout(() => {
        if (x2MaxId) mountDetail(x2MaxId);
        paint();
      }, 30);
    });
    return rootEl;
  }

  function syncDots() {
    x2Wins.forEach(w => {
      const el = document.querySelector(`#x2Dock .x2-ditem[data-id="${w.id}"]`);
      if (el) {
        el.classList.toggle('open', !!x2State[w.id].open && !x2State[w.id].minimized);
        el.classList.toggle('minimized', !!x2State[w.id].minimized);
        const sp = el.querySelector('span');
        if (sp) sp.textContent = winTitle(w);
      }
    });
  }

  function resetAll() {
    if (x2MaxId) toggleFullscreen(x2MaxId);
    x2UserMoved = false;
    x2Wins.forEach((w, i) => {
      x2State[w.id] = { open: true, minimized: false, max: false };
      const win = document.getElementById('x2w-' + w.id);
      if (!win) return;
      win.classList.remove('is-closed', 'is-minimized', 'is-max');
      win.style.zIndex = i + 1;
    });
    layoutDefault();
    syncDockTray();
    syncDots();
  }

  function paint() {
    if (!window.HL_UI || window.HL_UI.getMode() !== 'x2') return;
    ensureDom();
    const zh = isZh();
    const tone = window.HL_UI.getX2Tone();
    const nowTime = new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
    const ck = document.getElementById('x2MbClock');
    if (ck) ck.textContent = nowTime;
    const rs = document.getElementById('x2ResetTop');
    if (rs) rs.textContent = zh ? '还原布局' : 'レイアウト初期化';
    const lt = document.getElementById('x2LangTxt');
    if (lt) lt.textContent = zh ? '中 / 日' : '日 / 中';
    const tb = document.getElementById('x2ToneTop');
    if (tb) tb.textContent = tone === 'light' ? (zh ? '深色壁纸' : 'ダーク') : (zh ? '浅色壁纸' : 'ライト');
    updateTitlebarLabels();
    syncDots();

    const d = (typeof dlState !== 'undefined' && dlState.data && dlState.data.ok) ? dlState.data : null;
    const v = d ? d.view : null;
    const h = d ? d.health : null;

    const stEl = document.getElementById('x2MbStatus');
    if (stEl) stEl.innerHTML = `<i class="x2-dot" style="background:${v && v.stale ? '#989ea8' : 'var(--os-green)'}"></i>${v && v.stale ? (zh ? '离开' : '離席') : (zh ? '在线' : 'オンライン')}`;
    const hrEl = document.getElementById('x2MbHr');
    if (hrEl) hrEl.textContent = (h && h.hr_live != null) ? `${h.hr_live} bpm` : '';

    if (window.HL_X2_VIEWS) window.HL_X2_VIEWS.renderSummaries(x2MaxId, zh);
    if (!x2UserMoved && !x2MaxId) requestAnimationFrame(layoutDefault);
  }

  window.HL_X2 = {
    paint,
    exitFullscreen: () => { if (x2MaxId) toggleFullscreen(x2MaxId); },
    onResize: () => { if (!x2UserMoved && !x2MaxId) layoutDefault(); },
  };

  window.addEventListener('keydown', e => {
    if (e.key === 'Escape' && window.HL_UI && window.HL_UI.getMode() === 'x2' && x2MaxId) toggleFullscreen(x2MaxId);
  });
  setInterval(() => { if (window.HL_UI && window.HL_UI.getMode() === 'x2' && !x2MaxId) paint(); }, 5000);
  if (window.HL_UI && window.HL_UI.getMode() === 'x2') window.HL_UI.applyUi();
})();
