'use strict';
// 2-UI 切换器：像素经典 (classic) / X2 macOS 桌面 (x2)
(function () {
  const K_UI = 'hl-ui-mode';
  const K_X2 = 'hl-x2-tone';
  const r = document.documentElement;

  const params = new URLSearchParams(location.search);
  let ui = params.get('ui') || localStorage.getItem(K_UI) || 'classic';
  if (ui !== 'classic' && ui !== 'x2') ui = 'classic';
  let x2Tone = localStorage.getItem(K_X2) || 'dark';

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
        const b = e.target.closest('button[data-ui]');
        if (!b) return;
        if (ui === 'x2' && window.HL_X2) window.HL_X2.exitFullscreen();
        ui = b.dataset.ui;
        localStorage.setItem(K_UI, ui);
        applyUi();
      });
    }
    bar.innerHTML = `
      <button type="button" data-ui="classic" class="${ui === 'classic' ? 'on' : ''}">像素经典</button>
      <button type="button" data-ui="x2" class="${ui === 'x2' ? 'on' : ''}">X2 macOS</button>`;
  }

  function applyUi() {
    r.dataset.ui = ui;
    r.dataset.x2Tone = x2Tone;
    paintBar();
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
  window.addEventListener('resize', () => {
    if (ui === 'x2' && window.HL_X2) window.HL_X2.onResize();
  });

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', applyUi);
  else applyUi();
})();
