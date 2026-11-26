'use strict';
// 2-UI 切换器：PC 端默认 X2 macOS，手机端 (<=860px) 固定像素经典
(function () {
  const K_UI = 'hl-ui-mode';
  const K_X2 = 'hl-x2-tone';
  const r = document.documentElement;
  const isMobile = () => window.innerWidth <= 860;

  const params = new URLSearchParams(location.search);
  let prefUi = params.get('ui') || localStorage.getItem(K_UI) || 'x2';
  if (prefUi !== 'classic' && prefUi !== 'x2') prefUi = 'x2';
  let x2Tone = localStorage.getItem(K_X2) || 'dark';
  let activeUi = isMobile() ? 'classic' : prefUi;

  function setMode(next) {
    if (next !== 'classic' && next !== 'x2') return;
    if (isMobile() && next === 'x2') return;
    if (activeUi === 'x2' && next !== 'x2' && window.HL_X2) window.HL_X2.exitFullscreen();
    prefUi = next;
    localStorage.setItem(K_UI, prefUi);
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

  /* 经典模式侧栏左下角 ▶ OS 像素彩蛋按钮（仅 PC 端显示） */
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

  /* 隐藏彩蛋：PC 端像素经典模式双击颜文字也能启动桌面 OS */
  document.addEventListener('dblclick', e => {
    if (activeUi !== 'classic' || isMobile()) return;
    if (e.target.closest('.dl-face, .nw-face')) setMode('x2');
  });

  function applyUi() {
    const next = isMobile() ? 'classic' : prefUi;
    if (activeUi === 'x2' && next !== 'x2' && window.HL_X2) window.HL_X2.exitFullscreen();
    activeUi = next;
    r.dataset.ui = activeUi;
    r.dataset.x2Tone = x2Tone;
    ensureClassicTrigger();
    if (activeUi === 'x2' && window.HL_X2) {
      if (typeof loadHome === 'function') loadHome();
      window.HL_X2.paint();
    }
  }

  window.HL_UI = {
    getMode: () => activeUi,
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
      if (activeUi === 'x2' && window.HL_X2) window.HL_X2.paint();
      return res;
    };
  }
  window.addEventListener('resize', () => {
    const next = isMobile() ? 'classic' : prefUi;
    if (next !== activeUi) applyUi();
    else if (activeUi === 'x2' && window.HL_X2) window.HL_X2.onResize();
  });

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', applyUi);
  else applyUi();
})();
