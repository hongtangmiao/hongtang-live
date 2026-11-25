'use strict';
// 临时 3-UI 切换器：经典像素 (classic) / AC 融合版 (ac) / X2 macOS 桌面 (x2)
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

  const safe = s => String(s == null ? '' : s).replace(/[&<>"']/g, m => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));

  /* ---------- AC 右侧弹簧滑块 & 鼠标镜面反光 ---------- */
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
    const active = document.querySelector('.sidebar .nav-item.active') || document.querySelector('.sidebar .brand.active') || document.querySelector('.sidebar .nav-item');
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

  /* ---------- X2 macOS 桌面工作台（实时读取站点真实数据） ---------- */
  let x2Z = 20;
  const x2Wins = [
    { id: 'now', icon: '◉', title: '此刻状态', x: 28, y: 48, w: 390 },
    { id: 'health', icon: '♥', title: '身体健康', x: 438, y: 48, w: 300 },
    { id: 'music', icon: '♪', title: '音乐', x: 758, y: 48, w: 320 },
    { id: 'hosts', icon: '▤', title: '活动监视器', x: 28, y: 344, w: 390 },
    { id: 'anime', icon: '✦', title: '追番书架', x: 438, y: 244, w: 640 },
  ];
  const x2State = {};
  x2Wins.forEach(w => { x2State[w.id] = { open: true, max: false }; });

  function ensureX2Dom() {
    let rootEl = document.getElementById('x2Root');
    if (rootEl) return rootEl;
    rootEl = document.createElement('div');
    rootEl.id = 'x2Root';
    const siteName = (window.HL_SITE && window.HL_SITE.name) || '红糖';
    rootEl.innerHTML = `
      <header class="x2-menubar">
        <div class="x2-mb-l">
          <span class="x2-mb-app">⌘ ${safe(siteName)} OS</span>
          <button class="x2-mb-btn" id="x2ResetTop" type="button">还原布局</button>
        </div>
        <div class="x2-mb-r">
          <span id="x2MbStatus"><i class="x2-dot"></i>在线</span>
          <span id="x2MbHr"></span>
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
        <div class="x2-tbar">
          <div class="x2-lights">
            <button class="x2-l-r" type="button" data-act="close" data-id="${w.id}" title="关闭"></button>
            <button class="x2-l-y" type="button" data-act="min" data-id="${w.id}" title="最小化"></button>
            <button class="x2-l-g" type="button" data-act="max" data-id="${w.id}" title="缩放"></button>
          </div>
          <span class="x2-wtitle">${w.title}</span>
          <span style="width:42px"></span>
        </div>
        <div class="x2-body" id="x2b-${w.id}"></div>
      </section>`).join('');

    rootEl.querySelector('#x2Dock').innerHTML = x2Wins.map(w =>
      `<button class="x2-ditem open" type="button" data-id="${w.id}">${w.icon}<span>${w.title}</span></button>`
    ).join('') + `<div class="x2-dsep"></div><button class="x2-ditem" type="button" id="x2DockReset">↺<span>还原窗口</span></button>`;

    desk.querySelectorAll('.x2-win').forEach(win => {
      win.addEventListener('pointerdown', () => { win.style.zIndex = ++x2Z; });
      const bar = win.querySelector('.x2-tbar');
      bar.addEventListener('pointerdown', e => {
        if (e.target.closest('.x2-lights') || window.innerWidth <= 860 || win.classList.contains('is-max')) return;
        win.classList.add('dragging');
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

    desk.addEventListener('click', e => {
      const b = e.target.closest('button[data-act]');
      if (!b) return;
      const id = b.dataset.id, act = b.dataset.act;
      const win = document.getElementById('x2w-' + id);
      if (act === 'close' || act === 'min') {
        x2State[id].open = false;
        win.classList.add('is-hidden');
        win.classList.remove('is-max');
      } else if (act === 'max') {
        x2State[id].max = !x2State[id].max;
        win.classList.toggle('is-max', x2State[id].max);
        win.style.zIndex = ++x2Z;
      }
      syncX2Dots();
    });

    rootEl.querySelector('#x2Dock').addEventListener('click', e => {
      const it = e.target.closest('.x2-ditem');
      if (!it) return;
      if (it.id === 'x2DockReset') { resetX2(); return; }
      const id = it.dataset.id;
      const win = document.getElementById('x2w-' + id);
      x2State[id].open = true;
      win.classList.remove('is-hidden');
      win.style.zIndex = ++x2Z;
      syncX2Dots();
    });

    rootEl.querySelector('#x2ResetTop').addEventListener('click', resetX2);
    rootEl.querySelector('#x2ToneTop').addEventListener('click', toggleX2Tone);
    return rootEl;
  }

  function syncX2Dots() {
    x2Wins.forEach(w => {
      const el = document.querySelector(`#x2Dock .x2-ditem[data-id="${w.id}"]`);
      if (el) el.classList.toggle('open', !!x2State[w.id].open);
    });
  }

  function resetX2() {
    x2Wins.forEach((w, i) => {
      x2State[w.id] = { open: true, max: false };
      const win = document.getElementById('x2w-' + w.id);
      if (!win) return;
      win.classList.remove('is-hidden', 'is-max');
      win.style.left = w.x + 'px';
      win.style.top = w.y + 'px';
      win.style.zIndex = i + 1;
    });
    syncX2Dots();
  }

  function paintX2() {
    if (ui !== 'x2') return;
    ensureX2Dom();
    const nowTime = new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
    const ck = document.getElementById('x2MbClock');
    if (ck) ck.textContent = nowTime;
    const tb = document.getElementById('x2ToneTop');
    if (tb) tb.textContent = x2Tone === 'light' ? '深色壁纸' : '浅色壁纸';

    const d = (typeof dlState !== 'undefined' && dlState.data && dlState.data.ok) ? dlState.data : null;
    const v = d ? d.view : null;
    const h = d ? d.health : null;

    const stEl = document.getElementById('x2MbStatus');
    if (stEl) stEl.innerHTML = `<i class="x2-dot" style="background:${v && v.stale ? '#989ea8' : 'var(--os-green)'}"></i>${v && v.stale ? '离开' : '在线'}`;
    const hrEl = document.getElementById('x2MbHr');
    if (hrEl) hrEl.textContent = (h && h.hr_live != null) ? `♥ ${h.hr_live} bpm` : '';

    // 1. 此刻状态窗
    const bNow = document.getElementById('x2b-now');
    if (bNow) {
      const apps = (d && Array.isArray(d.apps)) ? d.apps.slice(0, 5) : [];
      bNow.innerHTML = !v ? '<div style="color:var(--os-muted)">正在同步实时状态…</div>' : `
        <div style="display:flex;align-items:center;gap:12px;margin-bottom:12px">
          <div style="font-size:21px;font-weight:700;background:var(--os-in);padding:8px 12px;border-radius:9px !important;white-space:nowrap">${safe(v.face || 'ฅ^•ﻌ•^ฅ')}</div>
          <div><div style="font-size:15px;font-weight:700">${safe(v.now || '—')}</div>
          <div style="color:var(--os-muted);font-size:12px">${safe([v.dur_text, v.cat, d.summary && d.summary.total ? '今日 ' + d.summary.total : ''].filter(Boolean).join(' · '))}</div></div>
        </div>
        ${apps.map(a => `<div class="x2-row"><span><b style="color:${safe(a.color || '#94a3b8')}">■</b> ${safe(a.label || '')}</span><b>${safe(a.total || '')}</b></div>`).join('')}`;
    }

    // 2. 身体健康窗
    const bHealth = document.getElementById('x2b-health');
    if (bHealth) {
      if (!h || !h.available) {
        bHealth.innerHTML = '<div style="color:var(--os-muted)">暂无健康数据</div>';
      } else {
        const sleepTxt = (typeof fmtSleepMin === 'function' && h.sleep_min != null) ? fmtSleepMin(h.sleep_min) : '—';
        bHealth.innerHTML = `<div class="x2-sgrid">
          <div class="x2-stat"><small>心率</small><b>${h.hr_live != null ? safe(h.hr_live) + ' bpm' : safe(h.hr_last_text || '—')}</b></div>
          <div class="x2-stat"><small>睡眠${h.sleep_score != null ? ' (' + h.sleep_score + '分)' : ''}</small><b>${safe(sleepTxt)}</b></div>
          <div class="x2-stat"><small>压力</small><b>${h.stress != null ? safe(h.stress) : '—'}</b></div>
          <div class="x2-stat"><small>血氧</small><b>${h.spo2 != null ? safe(h.spo2) + '%' : '—'}</b></div>
        </div>`;
      }
    }

    // 3. 音乐窗
    const bMusic = document.getElementById('x2b-music');
    if (bMusic) {
      const m = (typeof hmState !== 'undefined' && hmState && hmState.musicNow) || (typeof mxState !== 'undefined' && mxState && mxState.now);
      if (!m || !m.title) {
        bMusic.innerHTML = '<div style="color:var(--os-muted)">暂无播放记录</div>';
      } else {
        const cov = m.cover && typeof pic === 'function' ? pic(m.cover, 140) : (m.cover || '');
        bMusic.innerHTML = `<div style="display:flex;align-items:center;gap:12px">
          ${cov ? `<img src="${safe(cov)}" referrerpolicy="no-referrer" style="width:58px;height:58px;border-radius:9px !important;object-fit:cover" alt="">` : '<div style="width:58px;height:58px;border-radius:9px !important;background:var(--os-in);display:grid;place-items:center;font-size:22px">♪</div>'}
          <div style="min-width:0">
            <div style="font-size:15px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${safe(m.title)}</div>
            <div style="color:var(--os-muted);font-size:12px">${safe(m.sub || '—')}</div>
            <div style="color:var(--os-green);font-size:12px;margin-top:3px">${m.state === 'playing' ? '● 正在播放' : '已暂停'}</div>
          </div>
        </div>`;
      }
    }

    // 4. 活动监视器窗
    const bHosts = document.getElementById('x2b-hosts');
    if (bHosts) {
      const hosts = (typeof hmState !== 'undefined' && hmState.sys && Array.isArray(hmState.sys.hosts)) ? hmState.sys.hosts : [];
      bHosts.innerHTML = !hosts.length ? '<div style="color:var(--os-muted)">正在读取设备状态…</div>' :
        hosts.map(x => `<div class="x2-row">
          <span><b>${safe(x.name)}</b></span>
          <span style="color:var(--os-muted)">${x.online ? `CPU ${Math.round(x.cpu || 0)}% · MEM ${Math.round(x.mem || 0)}%${x.gpu != null ? ' · GPU ' + Math.round(x.gpu) + '%' : ''}` : '离线'}</span>
        </div>`).join('');
    }

    // 5. 追番书架窗
    const bAnime = document.getElementById('x2b-anime');
    if (bAnime) {
      const items = (typeof viState !== 'undefined' && viState.data && Array.isArray(viState.data.items))
        ? viState.data.items.filter(x => x.status === 'watching').slice(0, 4)
        : [];
      bAnime.innerHTML = !items.length ? '<div style="color:var(--os-muted)">正在读取追番列表…</div>' :
        `<div class="x2-agrid">${items.map(it => {
          const cov = it.cover || it.fallbackCover || '';
          const ep = it.epTotal > 0 ? `${it.epWatched || 0}/${it.epTotal}话` : '';
          const sc = it.rate > 0 ? `★${it.rate}` : (it.score > 0 ? `★${Number(it.score).toFixed(1)}` : '');
          return `<a class="x2-vi" href="${safe(it.bgmUrl || '#videos')}" ${it.bgmUrl ? 'target="_blank" rel="noreferrer"' : ''}>
            ${cov ? `<img src="${safe(cov)}" referrerpolicy="no-referrer" loading="lazy" alt="">` : '<div style="aspect-ratio:2/3;background:var(--os-in);border-radius:7px !important"></div>'}
            <b>${safe(it.title || '')}</b>
            <small>${safe([ep, sc].filter(Boolean).join(' · '))}</small>
          </a>`;
        }).join('')}</div>`;
    }
  }

  /* ---------- 切换器 UI 浮条 ---------- */
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
      : (ui === 'x2' ? `<span class="hl-ui-sep"></span><button class="hl-ui-sub" type="button" data-sub="x2">${x2Tone === 'light' ? '深壁纸' : '浅壁纸'}</button>` : '');
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
    if (ui === 'x2') {
      if (typeof loadHome === 'function') loadHome();
      paintX2();
    }
  }

  /* 挂钩 paintHomeCards 和 syncNav，数据更新/切页时同步 X2 与 AC 滑块 */
  if (typeof window.paintHomeCards === 'function') {
    const origPaint = window.paintHomeCards;
    window.paintHomeCards = function () {
      const res = origPaint.apply(this, arguments);
      if (ui === 'x2') paintX2();
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
  window.addEventListener('resize', syncAcPill);
  window.addEventListener('hashchange', () => { if (ui === 'ac') setTimeout(syncAcPill, 30); });
  setInterval(() => { if (ui === 'x2') paintX2(); }, 5000);

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', applyUi);
  else applyUi();
})();
