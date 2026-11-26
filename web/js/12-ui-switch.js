'use strict';
// 临时 3-UI 切换器（经典像素 / AC 融合 / X2 macOS）+ AC 弹簧滑块与鼠标镜面反光
(function () {
  const K_UI = 'hl-ui-mode';
  const K_AC = 'hl-ac-tone';
  const K_X2 = 'hl-x2-tone';
  const r = document.documentElement;

  const params = new URLSearchParams(location.search);
  let ui = params.get('ui') || localStorage.getItem(K_UI) || 'classic';
  if (!['classic', 'ac', 'x2'].includes(ui)) ui = 'classic';
  let acTone = localStorage.getItem(K_AC) || 'paper';
  let x2Tone = localStorage.getItem(K_X2) || 'dark';

  function ensureAcPill() {
    const sb = document.querySelector('.sidebar');
    if (!sb) return null;
    let pill = sb.querySelector('.ac-rail-pill');
    if (!pill) {
      pill = document.createElement('div');
      pill.className = 'ac-rail-pill';
      sb.insertBefore(pill, sb.firstChild);
    }
    return pill;
  }

  function syncAcPill() {
    if (ui !== 'ac') return;
    const pill = ensureAcPill();
    const active = document.querySelector('.sidebar .nav-item.active')
      || document.querySelector('.sidebar .brand.active')
      || document.querySelector('.sidebar .nav-item');
    if (!pill || !active) return;
    pill.style.transform = `translate(${active.offsetLeft - 8}px, ${active.offsetTop - 8}px)`;
  }

  document.addEventListener('pointermove', e => {
    if (ui !== 'ac') return;
    const card = e.target.closest('.card, .mx-col');
    if (!card) return;
    const rect = card.getBoundingClientRect();
    card.style.setProperty('--mx', (e.clientX - rect.left) + 'px');
    card.style.setProperty('--my', (e.clientY - rect.top) + 'px');
  }, { passive: true });

  function toggleAcTone() {
    acTone = acTone === 'graphite' ? 'paper' : 'graphite';
    localStorage.setItem(K_AC, acTone);
    applyUi();
  }

  function toggleX2Tone() {
    x2Tone = x2Tone === 'light' ? 'dark' : 'light';
    localStorage.setItem(K_X2, x2Tone);
    applyUi();
  }

  function paintBar() {
    let bar = document.getElementById('hlUiBar');
    if (!bar) {
      bar = document.createElement('div');
      bar.id = 'hlUiBar';
      bar.setAttribute('aria-label', 'UI 风格切换');
      document.body.appendChild(bar);
      bar.addEventListener('click', e => {
        const b = e.target.closest('button');
        if (!b) return;
        if (b.dataset.ui) {
          if (ui === 'x2' && window.HL_X2) window.HL_X2.exitFullscreen();
          ui = b.dataset.ui;
          localStorage.setItem(K_UI, ui);
          applyUi();
        } else if (b.dataset.sub === 'ac') {
          toggleAcTone();
        } else if (b.dataset.sub === 'x2') {
          toggleX2Tone();
        }
      });
    }
    const sub = ui === 'ac'
      ? `<span class="hl-ui-sep"></span><button class="hl-ui-sub" type="button" data-sub="ac">${acTone === 'graphite' ? '暖纸' : '石墨'}</button>`
      : '';
    bar.innerHTML = `
      <button type="button" data-ui="classic" class="${ui === 'classic' ? 'on' : ''}">像素经典</button>
      <button type="button" data-ui="ac" class="${ui === 'ac' ? 'on' : ''}">AC 融合</button>
      <button type="button" data-ui="x2" class="${ui === 'x2' ? 'on' : ''}">X2 macOS</button>
      ${sub}`;
  }

  function applyUi() {
    r.dataset.ui = ui;
    r.dataset.acTone = acTone;
    r.dataset.x2Tone = x2Tone;
    paintBar();
    if (ui === 'ac') requestAnimationFrame(syncAcPill);
    if (ui === 'x2' && window.HL_X2) {
      if (typeof loadHome === 'function') loadHome();
      window.HL_X2.paint();
    }
  }

  window.HL_UI = {
    getMode: () => ui,
    getX2Tone: () => x2Tone,
    toggleX2Tone,
    applyUi,
  };

  if (typeof window.paintHomeCards === 'function') {
    const origPaint = window.paintHomeCards;
    window.paintHomeCards = function () {
      const res = origPaint.apply(this, arguments);
      if (ui === 'x2' && window.HL_X2) window.HL_X2.paint();
      return res;
    };
  }
  if (typeof window.syncNav === 'function') {
    const origSync = window.syncNav;
    window.syncNav = function () {
      const res = origSync.apply(this, arguments);
      if (ui === 'ac') requestAnimationFrame(syncAcPill);
      return res;
    };
  }
  window.addEventListener('resize', () => {
    syncAcPill();
    if (ui === 'x2' && window.HL_X2) window.HL_X2.onResize();
  });
  window.addEventListener('hashchange', () => { if (ui === 'ac') setTimeout(syncAcPill, 30); });

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', applyUi);
  else applyUi();
})();
