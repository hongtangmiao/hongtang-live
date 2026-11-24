'use strict';
// 音乐页：取数据、正在听轮询、歌单展开、事件绑定
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。
/* 正在听：读 daylog 的 devices.music */
async function fetchNow() {
  if (!NET.proxy) return null;
  try {
    const r = await fetch('api/music/now', { credentials: 'same-origin' });
    if (r.ok) {
      const d = await r.json();
      if (d && d.ok && d.now && d.now.title) {
        return {
          title: d.now.title, sub: d.now.sub, cover: d.now.cover, url: d.now.url,
          state: d.now.playing ? 'playing' : 'paused', ago: d.now.ago,
        };
      }
    }
  } catch (e) { /* 忽略网络错误 */ }
  return null;
}

/* 听歌历史（daylog 的 SQLite，由网关读出） */
async function fetchHistory() {
  try {
    if (!NET.proxy) return null;
    const r = await fetch('api/music/history', { credentials: 'same-origin' });
    if (!r.ok) return null;
    const d = await r.json();
    return d && d.ok ? d : null;
  } catch (e) { return null; }
}

let mxLiveTimer = null;
function stopMxLive() {
  if (mxLiveTimer) { clearInterval(mxLiveTimer); mxLiveTimer = null; }
}
function startMxLive() {
  stopMxLive();
  stopDlLive();
  if (!NET.proxy) return;
  mxLiveTimer = setInterval(async () => {
    if (document.hidden) return;
    if (!document.getElementById('mxNow')) { stopMxLive(); return; }
    if (!mxState) return;
    const now = await fetchNow();
    if (!now) return;
    const prev = mxState.now;
    if (prev && prev.title === now.title && prev.state === now.state && prev.cover === now.cover) return;
    mxState.now = now;
    const box = document.getElementById('mxNow');
    if (box) box.innerHTML = mxHero(mxNowHero(mxState));
  }, 20000);
}

const NOISE_TRACK_ARTIST_RE = /(?:\bR18\b|🔞|\bNSFW\b|\bFANBOX\b|\bFANTIA\b|\bCI-EN\b|\bSKBMMDer\b)/i;
const NOISE_TRACK_TITLE_RE = /(?:\bSD:[A-Za-z0-9_-]+|\bNo sound\b|\bNSFW\b|販売開始|フル動画)/i;
function isNoiseSongItem(item) {
  if (!item) return false;
  const s = (item && item.song) ? item.song : ((item && item.data) ? item.data : item);
  const title = String((s && (s.name || s.title)) || '');
  const artists = ((s && (s.ar || s.artists)) || []).map(a => a && a.name).filter(Boolean).join(', ');
  return NOISE_TRACK_ARTIST_RE.test(artists) || NOISE_TRACK_TITLE_RE.test(title);
}

const mxSoon = () => `<div class="mx-soon">${ICON.empty}<span>${t('coming_soon')}</span></div>`;

async function loadMusic() {
  const root = $('#muRoot');
  if (!root) return;

  const keepWin = (mxState && mxState.mxRankWin) || 'd7';
  mxState = {
    prof: null, now: null, hist: null,
    self: !(NET.session && NET.session.owner),
    recentSongs: [], recWeek: [], recAll: [],
    playlists: [], likes: [],
    mxRankWin: keepWin,
  };
  root.innerHTML = musicSkeleton();
  startMxLive();

  const nowP = fetchNow();
  const histP = fetchHistory();

  try {
    // 站点账号资料已在 session 里（秒开），先挂上，首屏就能显示账号名
    mxState.prof = NET.profile || null;

    // 听歌记录是本地 daylog 数据（~0.1s），先渲染首屏：正在听 / 今日 / 排行
    mxState.hist = await histP;
    mxState.now = (mxState.hist && mxState.hist.now) || await nowP;
    if (mxState.hist) mxPaint();

    if (!NET.logged) {
      // 没有绑定账号：能看「正在听 / 今日 / 排行 / 歌手」，歌单与喜欢显示準備中
      if (mxState.hist) return;
      stopMxLive();
      root.innerHTML = mxSoon();
      return;
    }

    let prof = NET.profile;
    if (!prof) prof = await NET.whoami();
    if (!prof) {
      NET.clear();
      if (mxState.hist) return;
      stopMxLive();
      root.innerHTML = mxSoon();
      return;
    }
    mxState.prof = prof;
    const uid = prof.userId;

    // 网易云数据走网关缓存；每个请求独立回填，谁先到先画，不再整页干等最慢的一个
    const paint = () => { mxPaint(); };
    NET.api('record/recent/song', { limit: 30 }).then(r => {
      mxState.recentSongs = ((r && r.data && r.data.list) || [])
        .filter(x => !isNoiseSongItem(x))
        .map(x => x.song || x.data || x).filter(Boolean);
      if (!mxState.now && mxState.recentSongs.length) {
        const s = mxState.recentSongs[0];
        mxState.now = { title: s.name || '', sub: songArtists(s), cover: (s.al && s.al.picUrl) || s.picUrl || '', state: 'paused', ago: null };
      }
      paint();
    }).catch(() => {});
    NET.api('user/record', { uid, type: 1 }).then(r => {
      mxState.recWeek = ((r && r.weekData) || []).filter(x => !isNoiseSongItem(x));
      mxState.loadedAt = Date.now();
      paint();
    }).catch(() => {});
    NET.api('user/record', { uid, type: 0 }).then(r => {
      mxState.recAll = ((r && r.allData) || []).filter(x => !isNoiseSongItem(x));
      mxState.loadedAt = Date.now();
      paint();
    }).catch(() => {});
    NET.api('user/playlist', { uid, limit: 20 }).then(r => {
      const raw = (r && r.playlist) || [];
      mxState.playlists = (raw
        .filter(p => p.trackCount > 0)
        .map(p => ({ id: p.id, name: p.name, cover: p.coverImgUrl, count: p.trackCount, plays: p.playCount }))); paint();
      // 「喜欢的音乐」用歌单本身（specialType 5）：顺序和网易云里看到的一致；likelist 的 ids 顺序对不上
      const fav = raw.find(p => p.specialType === 5 && p.creator && p.creator.userId === uid);
      if (!fav) return;
      mxState.likesHref = 'https://music.163.com/#/playlist?id=' + fav.id;
      NET.api('playlist/detail', { id: fav.id }).then(d => {
        const tracks = (d && d.playlist && d.playlist.tracks) || [];
        if (!tracks.length) return;
        mxState.likes = tracks.filter(s => !isNoiseSongItem(s));
        mxState.likesTotal = fav.trackCount || tracks.length;
        mxState.likesFromPl = true;
        paint();
      }).catch(() => {});
    }).catch(() => {});
    // 兜底：找不到「喜欢的音乐」歌单时才用 likelist（顺序不保证）
    NET.api('likelist', { uid }).then(async r => {
      const ids = ((r && r.ids) || []).slice(0, 30);
      if (!ids.length || mxState.likesFromPl) return;
      const det = await NET.api('song/detail', { ids: ids.join(',') }).catch(() => null);
      if (mxState.likesFromPl) return;
      const by = new Map(((det && det.songs) || []).map(x => [x.id, x]));
      mxState.likes = ids.map(id => by.get(id)).filter(x => x && !isNoiseSongItem(x));
      mxState.likesTotal = ((r && r.ids) || []).length;
      paint();
    }).catch(() => {});
  } catch (e) {
    if (e && e.code === 401) {
      stopMxLive();
      root.innerHTML = mxSoon();
    } else {
      root.innerHTML = `<div class="empty">${ICON.empty}<span>${t('ne_error')}（${esc(e.message || e)}）</span></div>`;
    }
  }
}

/* 歌单行内展开 */
async function mxTogglePlaylist(row) {
  const open = row.classList.contains('open');
  const next = row.nextElementSibling;
  if (next && next.classList.contains('mx-tracks')) next.remove();
  row.classList.toggle('open', !open);
  if (open) return;

  const holder = document.createElement('div');
  holder.className = 'mx-tracks';
  holder.innerHTML = `<div class="mu-skel" style="height:44px;margin:6px 0"></div>`;
  row.after(holder);

  const d = await NET.api('playlist/detail', { id: row.dataset.mxPl }).catch(() => null);
  const tracks = (d && d.playlist && d.playlist.tracks) || [];
  if (!holder.isConnected) return;
  holder.innerHTML = tracks.length
    ? tracks.slice(0, 60).map(sg => `
      <div class="mx-tr">
        ${mxThumb(songCover(sg, 60))}
        <div class="b">
          <div class="t">${esc(sg.name || '')}</div>
          <div class="a">${esc(songArtists(sg))}</div>
        </div>
      </div>`).join('')
    : mxEmpty();
}

function bindRankChips(scope) {
  $$('[data-mx-rank]', scope || document).forEach(b => b.addEventListener('click', () => {
    if (!mxState) return;
    mxState.mxRankWin = b.dataset.mxRank;
    mxPaint('rank');
  }));
}

function bindMusic() {
  bindRankChips();
  $$('[data-mx-pl]').forEach(r => r.addEventListener('click', () => mxTogglePlaylist(r)));
}

function pageMusic() {
  return `<div id="muRoot">${musicSkeleton()}</div>`;
}

function pageIllust() {
  return `<div class="empty">${ICON.empty}<span>${t('coming_soon')}</span></div>`;
}
