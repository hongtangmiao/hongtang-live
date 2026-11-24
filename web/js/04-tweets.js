'use strict';
// 首页 daylog 数据拉取与统计面板占位（动态页 #tweets 已移除）
function pageStats() {
  /* 中身は index.html の静的な #panel-stats（本家 stats.js が描画する）。ここでは空。 */
  return '';
}

async function loadDaylog(force = false) {
  if (dlState.loading) return;
  if (!force && dlState.data && (Date.now() - (dlState.at || 0)) < 15000) {
    paintHomeHealth();
    return;
  }
  dlState.loading = true;
  dlState.error = null;
  paintHomeHealth();
  try {
    const r = await fetch('api/daylog/feed', { credentials: 'same-origin', cache: 'no-store' });
    const d = await r.json();
    if (!r.ok || !d || !d.ok) throw new Error((d && d.error) || 'HTTP ' + r.status);
    dlState.data = d;
    dlState.at = Date.now();
  } catch (e) {
    dlState.error = e.message || String(e);
  } finally {
    dlState.loading = false;
    paintHomeHealth();
  }
}

/* 从后台切回来时立刻刷新一次，不用等下一个 20 秒 */
document.addEventListener('visibilitychange', () => {
  if (document.hidden) return;
  if (hmTimer && document.getElementById('hmHealthBox')) { loadDaylog(true); loadHomeMusic(true); loadHomeSys(true); }
});

