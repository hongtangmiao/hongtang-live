'use strict';
// X2 macOS 窗口化总结内容渲染（由 13-ui-macos.js 调用）
(function () {
  const safe = s => String(s == null ? '' : s).replace(/[&<>"']/g, m => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));

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
            <div style="font-size:21px;font-weight:700;background:var(--os-in);padding:8px 12px;border-radius:9px !important;white-space:nowrap">${safe(v.face || 'ฅ^•ﻌ•^ฅ')}</div>
            <div><div style="font-size:15px;font-weight:700">${safe(v.now || '—')}</div>
            <div style="color:var(--os-muted);font-size:12px">${safe([v.dur_text, v.cat, d.summary && d.summary.total ? '今日 ' + d.summary.total : ''].filter(Boolean).join(' · '))}</div></div>
          </div>
          ${apps.map(a => `<div class="x2-row"><span><b style="color:${safe(a.color || '#94a3b8')}">■</b> ${safe(a.label || '')}</span><b>${safe(a.total || '')}</b></div>`).join('')}`;
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
            <div class="x2-stat"><small>${zh ? '心率' : '心拍数'}</small><b${hrSm}>${safe(hrTxt)}</b></div>
            <div class="x2-stat"><small>睡眠${h.sleep_score != null ? ' (' + h.sleep_score + ')' : ''}</small><b>${safe(sleepTxt)}</b></div>
            <div class="x2-stat"><small>${zh ? '压力' : 'ストレス'}</small><b>${h.stress != null ? safe(h.stress) : '—'}</b></div>
            <div class="x2-stat"><small>${zh ? '血氧' : '血中酸素'}</small><b>${h.spo2 != null ? safe(h.spo2) + '%' : '—'}</b></div>
          </div>`;
        }
      }
    }

    if (x2MaxId !== 'music') {
      const bMusic = document.getElementById('x2b-music');
      if (bMusic) {
        const m = (typeof hmState !== 'undefined' && hmState && hmState.musicNow) || (typeof mxState !== 'undefined' && mxState && mxState.now);
        if (!m || !m.title) {
          bMusic.innerHTML = `<div style="color:var(--os-muted)">${zh ? '暂无播放记录' : '再生履歴なし'}</div>`;
        } else {
          const cov = m.cover && typeof pic === 'function' ? pic(m.cover, 140) : (m.cover || '');
          bMusic.innerHTML = `<div style="display:flex;align-items:center;gap:12px">
            ${cov ? `<img src="${safe(cov)}" referrerpolicy="no-referrer" style="width:58px;height:58px;border-radius:9px !important;object-fit:cover" alt="">` : '<div style="width:58px;height:58px;border-radius:9px !important;background:var(--os-in);display:grid;place-items:center;font-size:20px"><i class="ti ti-music"></i></div>'}
            <div style="min-width:0">
              <div style="font-size:15px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${safe(m.title)}</div>
              <div style="color:var(--os-muted);font-size:12px">${safe(m.sub || '—')}</div>
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
                <b>${safe(x.name)}</b>
                <span style="color:${x.online ? 'var(--os-muted)' : '#ef4444'}">${x.online ? safe(x.memText || (zh ? '在线' : 'オンライン')) : (zh ? '离线' : 'オフライン')}</span>
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
            const ep = it.epTotal > 0 ? `${it.epWatched || 0}/${it.epTotal}${zh ? '话' : '話'}` : '';
            const sc = it.rate > 0 ? `★${it.rate}` : (it.score > 0 ? `★${Number(it.score).toFixed(1)}` : '');
            return `<a class="x2-vi" href="${safe(it.bgmUrl || '#videos')}" ${it.bgmUrl ? 'target="_blank" rel="noreferrer"' : ''}>
              ${cov ? `<img src="${safe(cov)}" referrerpolicy="no-referrer" loading="lazy" alt="">` : '<div style="aspect-ratio:2/3;background:var(--os-in);border-radius:7px !important"></div>'}
              <b>${safe(it.title || '')}</b>
              <small>${safe([ep, sc].filter(Boolean).join(' · '))}</small>
            </a>`;
          }).join('')}</div>`;
      }
    }
  }

  window.HL_X2_VIEWS = { renderSummaries };
})();
