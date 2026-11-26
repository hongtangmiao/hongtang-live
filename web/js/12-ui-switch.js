'use strict';
// 2-UI 切换器：像素经典 (classic，侧栏 ▶ OS 按钮 + 双击颜文字彩蛋) ↔ X2 macOS (控制中心 + 左上角 OS 菜单)
(function () {
  const K_UI = 'hl-ui-mode';
  const K_X2 = 'hl-x2-tone';
  const r = document.documentElement;

  const params = new URLSearchParams(location.search);
  let ui = params.get('ui') || localStorage.getItem(K_UI) || 'classic';
  if (ui !== 'classic' && ui !== 'x2') ui = 'classic';
  let x2Tone = localStorage.getItem(K_X2) || 'dark';

  function setMode(next) {
    if (next !== 'classic' && next !== 'x2') return;
    if (ui === 'x2' && next !== 'x2' && window.HL_X2) window.HL_X2.exitFullscreen();
    ui = next;
    localStorage.setItem(K_UI, ui);
    applyUi();
  }

  function setX2Tone(tone) {
    x2Tone = tone === 'light' ? 'light' : 'dark';
    localStorage.setItem(K_X2, x2Tone);
    applyUi();
  }

  function toggleX2Tone() {
    setX2Tone(x2Tone === 'light' ? 'dark' : 'light');
  }

  /* 经典模式侧栏左下角 ▶ OS 像素彩蛋按钮 */
  function ensureClassicTrigger() {
    const tools = document.querySelector('.sidebar .side-tools');
    if (!tools || document.getElementById('hlOsEgg')) return;
    const btn = document.createElement('button');
    btn.id = 'hlOsEgg';
    btn.className = 'hl-os-egg';
    btn.type = 'button';
    btn.textContent = '▶ OS';
    btn.title = '启动桌面 OS 模式';
    btn.addEventListener('click', () => setMode('x2'));
    tools.insertBefore(btn, tools.firstChild);
  }

  /* 隐藏彩蛋：在像素经典模式双击颜文字也能启动桌面 OS */
  document.addEventListener('dblclick', e => {
    if (ui !== 'classic') return;
    if (e.target.closest('.dl-face, .nw-face')) setMode('x2');
  });

  function applyUi() {
    r.dataset.ui = ui;
    r.dataset.x2Tone = x2Tone;
    ensureClassicTrigger();
    if (ui === 'x2' && window.HL_X2) {
      if (typeof loadHome === 'function') loadHome();
      window.HL_X2.paint();
    }
  }

  window.HL_UI = {
    getMode: () => ui,
    setMode,
    getX2Tone: () => x2Tone,
    setX2Tone,
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
