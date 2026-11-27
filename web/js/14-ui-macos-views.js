'use strict';
// X2 macOS 窗口化总结内容渲染（由 13-ui-macos.js 调用）
(function () {
  function renderSummaries(x2MaxId, zh) {
    const d = (typeof dlState !== 'undefined' && dlState.data && dlState.data.ok) ? dlState.data : null;
    const v = d ? d.view : null;
    const h = d ? d.health : null;

    if (x2MaxId !== 'now') {
      const bNow = document.getElementById('x2b-now');
      if (bNow) {
        const apps = (d && Array.isArray(d.apps)) ? d.apps.slice(0, 5) : [];
        bNow.innerHTML = !v ? `<div style="color:var(--os-muted)">${zh ? '正在同步实时状态…' : '読み込み中…'}</div>` : `
          <div style="display:flex;align-items:center;gap:12px;margin-bottom:12px">
            <div style="font-size:21px;font-weight:700;background:var(--os-in);padding:8px 12px;border-radius:9px !important;white-space:nowrap">${esc(v.face || 'ฅ^•ﻌ•^ฅ')}</div>
            <div><div style="font-size:15px;font-weight:700">${esc(v.now || '—')}</div>
            <div style="color:var(--os-muted);font-size:12px">${esc([v.dur_text, v.cat, d.summary && d.summary.total ? '今日 ' + d.summary.total : ''].filter(Boolean).join(' · '))}</div></div>
          </div>
          ${apps.map(a => `<div class="x2-row"><span><b style="color:${esc(a.color || '#94a3b8')}">■</b> ${esc(a.label || '')}</span><b>${esc(a.total || '')}</b></div>`).join('')}`;
      }
    }

    if (x2MaxId !== 'health') {
      const bHealth = document.getElementById('x2b-health');
      if (bHealth) {
        if (!h || !h.available) {
          bHealth.innerHTML = `<div style="color:var(--os-muted)">${zh ? '暂无健康数据' : 'データなし'}</div>`;
        } else {
          const sleepTxt = (typeof fmtSleepMin === 'function' && h.sleep_min != null) ? fmtSleepMin(h.sleep_min) : '—';
          const hrTxt = h.hr_live != null ? `${h.hr_live} bpm` : (h.hr_last_text || '—');
          const hrSm = h.hr_live == null && String(hrTxt).length > 8 ? ' class="is-sm"' : '';
          bHealth.innerHTML = `<div class="x2-sgrid">
            <div class="x2-stat"><small>${zh ? '心率' : '心拍数'}</small><b${hrSm}>${esc(hrTxt)}</b></div>
            <div class="x2-stat"><small>睡眠${h.sleep_score != null ? ' (' + h.sleep_score + ')' : ''}</small><b>${esc(sleepTxt)}</b></div>
            <div class="x2-stat"><small>${zh ? '压力' : 'ストレス'}</small><b>${h.stress != null ? esc(h.stress) : '—'}</b></div>
            <div class="x2-stat"><small>${zh ? '血氧' : '血中酸素'}</small><b>${h.spo2 != null ? esc(h.spo2) + '%' : '—'}</b></div>
          </div>`;
        }
      }
    }

    if (x2MaxId !== 'music') {
      const bMusic = document.getElementById('x2b-music');
      if (bMusic) {
        const m = currentMusicNow();
        if (!m || !m.title) {
          bMusic.innerHTML = `<div style="color:var(--os-muted)">${zh ? '暂无播放记录' : '再生履歴なし'}</div>`;
        } else {
          const cov = m.cover ? pic(m.cover, 140) : '';
          bMusic.innerHTML = `<div style="display:flex;align-items:center;gap:12px">
            ${cov ? `<img src="${esc(cov)}" referrerpolicy="no-referrer" style="width:58px;height:58px;border-radius:9px !important;object-fit:cover" alt="">` : '<div style="width:58px;height:58px;border-radius:9px !important;background:var(--os-in);display:grid;place-items:center;font-size:20px"><i class="ti ti-music"></i></div>'}
            <div style="min-width:0">
              <div style="font-size:15px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(m.title)}</div>
              <div style="color:var(--os-muted);font-size:12px">${esc(m.sub || '—')}</div>
              <div style="color:${m.state === 'playing' ? 'var(--os-green)' : 'var(--os-muted)'};font-size:12px;margin-top:3px">${m.state === 'playing' ? (zh ? '● 正在播放' : '● 再生中') : (zh ? '已暂停' : '一時停止')}</div>
            </div>
          </div>`;
        }
      }
    }

    if (x2MaxId !== 'hosts') {
      const bHosts = document.getElementById('x2b-hosts');
      if (bHosts) {
        const hosts = (typeof hmState !== 'undefined' && hmState.sys && Array.isArray(hmState.sys.hosts)) ? hmState.sys.hosts : [];
        bHosts.innerHTML = !hosts.length ? `<div style="color:var(--os-muted)">${zh ? '正在读取设备状态…' : '読み込み中…'}</div>` :
          hosts.map(x => {
            const cpu = Math.round(x.cpu || 0), mem = Math.round(x.mem || 0), gpu = x.gpu != null ? Math.round(x.gpu) : null;
            return `<div class="x2-host">
              <div style="display:flex;justify-content:space-between;font-size:12px">
                <b>${esc(x.name)}</b>
                <span style="color:${x.online ? 'var(--os-muted)' : '#ef4444'}">${x.online ? esc(x.memText || (zh ? '在线' : 'オンライン')) : (zh ? '离线' : 'オフライン')}</span>
              </div>
              <div class="x2-bars">
                <div><div class="x2-bar-lbl"><span>CPU</span><b>${cpu}%</b></div><div class="x2-bar-trk"><i style="width:${cpu}%"></i></div></div>
                <div><div class="x2-bar-lbl"><span>MEM</span><b>${mem}%</b></div><div class="x2-bar-trk"><i style="width:${mem}%"></i></div></div>
                <div><div class="x2-bar-lbl"><span>GPU</span><b>${gpu != null ? gpu + '%' : '—'}</b></div><div class="x2-bar-trk"><i style="width:${gpu || 0}%"></i></div></div>
              </div>
            </div>`;
          }).join('');
      }
    }

    if (x2MaxId !== 'status') {
      const bStatus = document.getElementById('x2b-status');
      if (bStatus && typeof x2UptimeSummaryHtml === 'function') {
        bStatus.innerHTML = x2UptimeSummaryHtml(zh);
      }
    }

    if (x2MaxId !== 'anime') {
      const bAnime = document.getElementById('x2b-anime');
      if (bAnime) {
        const limit = window.innerWidth >= 1440 ? 6 : 4;
        const items = (typeof viState !== 'undefined' && viState.data && Array.isArray(viState.data.items))
          ? viState.data.items.filter(x => x.status === 'watching').slice(0, limit)
          : [];
        bAnime.innerHTML = !items.length ? `<div style="color:var(--os-muted)">${zh ? '正在读取追番列表…' : '読み込み中…'}</div>` :
          `<div class="x2-agrid">${items.map(it => {
            const cov = it.cover || it.fallbackCover || '';
            const ep = viEpText(it);
            const sc = it.rate > 0 ? `★${it.rate}` : (it.score > 0 ? `★${Number(it.score).toFixed(1)}` : '');
            return `<a class="x2-vi" href="${esc(it.bgmUrl || '#videos')}" ${it.bgmUrl ? 'target="_blank" rel="noreferrer"' : ''}>
              ${cov ? `<img src="${esc(cov)}" referrerpolicy="no-referrer" loading="lazy" alt="">` : '<div style="aspect-ratio:2/3;background:var(--os-in);border-radius:7px !important"></div>'}
              <b>${esc(it.title || '')}</b>
              <small>${esc([ep, sc].filter(Boolean).join(' · '))}</small>
            </a>`;
          }).join('')}</div>`;
      }
    }
  }

  function renderOsPop(zh, resetAll) {
    const pop = document.getElementById('x2OsPop');
    if (!pop) return;
    pop.innerHTML = `
      <button type="button" data-os-act="reset">${zh ? '还原所有窗口' : 'ウィンドウを初期化'}</button>
      <div class="x2-os-sep"></div>
      <button type="button" data-os-act="classic">${zh ? '退出桌面 · 回到像素经典' : 'デスクトップ終了 · ピクセル版へ'}</button>`;
    pop.onclick = e => {
      const b = e.target.closest('button[data-os-act]');
      if (!b) return;
      pop.classList.remove('open');
      if (b.dataset.osAct === 'reset') resetAll();
      if (b.dataset.osAct === 'classic' && window.HL_UI) window.HL_UI.setMode('classic');
    };
  }

  function renderCcPop(zh, onLangChanged) {
    const pop = document.getElementById('x2CcPop');
    if (!pop || !window.HL_UI) return;
    const tone = window.HL_UI.getX2Tone();
    pop.innerHTML = `
      <div class="x2-cc-blk">
        <div class="x2-cc-lbl">${zh ? '界面模式' : 'インターフェース'}</div>
        <div class="x2-seg">
          <button type="button" class="on" data-cc-ui="x2">${zh ? '桌面 OS' : 'デスクトップ'}</button>
          <button type="button" data-cc-ui="classic">${zh ? '像素经典' : 'ピクセル'}</button>
        </div>
      </div>
      <div class="x2-cc-blk">
        <div class="x2-cc-lbl">${zh ? '外观壁纸' : '外観'}</div>
        <div class="x2-seg">
          <button type="button" class="${tone === 'dark' ? 'on' : ''}" data-cc-tone="dark">${zh ? '深色' : 'ダーク'}</button>
          <button type="button" class="${tone === 'light' ? 'on' : ''}" data-cc-tone="light">${zh ? '浅色' : 'ライト'}</button>
        </div>
      </div>
      <div class="x2-cc-blk">
        <div class="x2-cc-lbl">${zh ? '显示语言' : '言語'}</div>
        <div class="x2-seg">
          <button type="button" class="${zh ? 'on' : ''}" data-cc-lang="zh">中文</button>
          <button type="button" class="${!zh ? 'on' : ''}" data-cc-lang="ja">日本語</button>
        </div>
      </div>`;
    pop.onclick = e => {
      const uiB = e.target.closest('button[data-cc-ui]');
      if (uiB) { pop.classList.remove('open'); window.HL_UI.setMode(uiB.dataset.ccUi); return; }
      const tB = e.target.closest('button[data-cc-tone]');
      if (tB) { window.HL_UI.setX2Tone(tB.dataset.ccTone); renderCcPop(zh, onLangChanged); return; }
      const lB = e.target.closest('button[data-cc-lang]');
      if (lB && ((lB.dataset.ccLang === 'zh') !== zh)) {
        const btn = document.querySelector('.lang-toggle');
        if (btn) btn.click();
        setTimeout(() => { if (onLangChanged) onLangChanged(); }, 30);
      }
    };
  }

  function renderCalPop(zh, onOpenNow) {
    const pop = document.getElementById('x2CalPop');
    if (!pop) return;
    const now = new Date();
    const y = now.getFullYear(), m = now.getMonth(), today = now.getDate();
    const firstDay = new Date(y, m, 1).getDay();
    const days = new Date(y, m + 1, 0).getDate();
    const wds = nwWeekdaysSunFirst();
    const cells = wds.map(w => `<span class="hd">${w}</span>`);
    for (let i = 0; i < firstDay; i++) cells.push('<span></span>');
    for (let d = 1; d <= days; d++) cells.push(`<span class="${d === today ? 'today' : ''}">${d}</span>`);
    pop.innerHTML = `
      <div style="display:flex;justify-content:space-between;align-items:baseline;font-weight:700;font-size:13px">
        <span>${y} 年 ${m + 1} 月</span>
        <span style="font-size:12px;color:var(--os-muted);font-weight:400">今日 ${m + 1}/${today}</span>
      </div>
      <div class="x2-cal-grid">${cells.join('')}</div>
      <button class="x2-cal-go" type="button" id="x2CalGo">${zh ? '打开今日时间线与日报 →' : '今日のアクティビティ・日報を見る →'}</button>`;
    pop.querySelector('#x2CalGo').addEventListener('click', () => {
      pop.classList.remove('open');
      if (onOpenNow) onOpenNow();
    });
  }

  window.HL_X2_VIEWS = { renderSummaries, renderOsPop, renderCcPop, renderCalPop };
})();
