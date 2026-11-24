'use strict';
// 音乐页：渲染（正在听、最近、排行、歌单、喜欢）
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。
/* ---------------- music（网易云 + 正在听） ---------------- */
const mxTile = (icon, value, label, cls) => `
  <div class="mx-tile${cls ? ' ' + cls : ''}"><span class="mi">${icon}</span><b>${value}</b><small>${label}</small></div>`;

const mxHero = o => `
  <div class="mx-hero">
    ${o.cover
      ? `<img class="mx-cover" onerror="this.style.opacity='0'" src="${esc(o.cover)}" alt="" loading="lazy">`
      : `<span class="mx-cover mx-cover-glyph">${o.glyph || '♪'}</span>`}
    <div class="mx-hbody">
      <div class="mx-label">${o.labelIcon ? `<i>${o.labelIcon}</i>` : ''}${o.label || ''}</div>
      <div class="mx-h1">${o.title || '—'}</div>
      <div class="mx-sub">${o.sub || ''}</div>
    </div>
    ${o.side ? `<div class="mx-hside"><b>${o.side[0]}</b><small>${o.side[1]}</small></div>` : ''}
  </div>`;

const mxSec = (icon, text, right) => `
  <div class="mx-sec"><span class="mx-sec-t"><i>${icon}</i>${text}</span>${right || ''}</div>`;

const mxThumb = src => src
  ? `<img class="th" onerror="this.style.opacity='0'" src="${esc(src)}" alt="" loading="lazy">`
  : `<span class="th glyph">♪</span>`;

const mxEmpty = () => `<div class="empty">${ICON.empty}<span>${t('ne_empty')}</span></div>`;

function neRecordRows(list) {
  return (list || []).map(it => {
    const s = it && it.song;
    if (!s) return null;
    const al = (s.al && typeof s.al === 'object') ? s.al : (s.album && typeof s.album === 'object' ? s.album : null);
    return {
      title: s.name || '',
      artist: songArtists(s),
      cover: (al && al.picUrl) || s.picUrl || '',
      plays: Number(it.playCount) || 1,
      score: Number(it.score) || 0,
    };
  }).filter(Boolean).sort((a, b) => b.plays - a.plays || b.score - a.score);
}

const timeOf = iso => {
  const d = new Date(iso);
  return isNaN(d) ? '' : d.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit', hour12: false });
};

/* 现在播放（来自 live 站；没有就退回最近播放的第一首） */
function mxNowHero(st) {
  const n = st.now;
  if (n && n.title) {
    const playing = n.state === 'playing';
    const tail = (!playing && n.ago != null) ? '・' + agoText(n.ago) : '';
    return {
      cover: n.cover ? pic(n.cover, 140) : '',
      glyph: '♪',
      labelIcon: playing ? ICON.music : ICON.clock,
      label: (playing ? t('mx_playing') : t('mx_last')) + tail,
      title: esc(n.title),
      sub: esc(n.sub || ''),
    };
  }
  if (st.recentSongs && st.recentSongs.length) {
    const s = st.recentSongs[0];
    return {
      cover: songCover(s, 140),
      glyph: '♪',
      labelIcon: ICON.clock,
      label: t('mx_last'),
      title: esc(s.name || '—'),
      sub: esc(songArtists(s)),
    };
  }
  return { glyph: '♪', label: t('mx_playing'), title: t('ne_empty'), sub: '' };
}

const agoText = m => {
  if (m == null) return '';
  if (m < 1) return t('mx_just_now');
  if (m < 60) return m + t('mx_min_ago');
  const h = Math.round(m / 60);
  return (h < 24 ? h + t('mx_hour_ago') : Math.round(h / 24) + t('mx_day_ago'));
};

/* 第 1 列：最近再生（正在听 + 今日の曲） */
function mxColRecent(st) {
  const h = st.hist || {};
  const stats = h.stats || {};
  const today = h.today_rows || [];
  return `
  <section class="mx-col">
    <a class="mx-src" href="${st.prof ? 'https://music.163.com/#/user/home?id=' + st.prof.userId : 'https://music.163.com/'}"
       target="_blank" rel="noreferrer">
      ${t('mx_src_ne')}${st.prof ? ' (' + esc(st.prof.nickname || '') + ')' : ''}${ICON.external}
    </a>
    <div id="mxNow">${mxHero(mxNowHero(st))}</div>
    <div class="mx-tiles">
      ${mxTile(ICON.music, stats.todaySongs != null ? String(stats.todaySongs) : '—', t('mx_today_songs'), 'blue')}
      ${mxTile(ICON.disc, stats.todayPlays != null ? String(stats.todayPlays) : '—', t('mx_today_plays'))}
    </div>
    ${mxSec(ICON.list, t('mx_today'), `<span class="mx-cnt-label">${esc(h.today || '')}</span>`)}
    <div class="mx-card">
      ${today.length ? today.slice(0, 12).map((r, i) => `
        <div class="mx-row">
          <span class="n">${i + 1}</span>
          ${mxThumb(r.cover ? pic(r.cover, 80) : '')}
          <div class="b">
            <div class="t">${esc(r.title)}</div>
            <div class="a">${esc(r.artist || '')}</div>
          </div>
          <span class="cnt">${timeOf(r.last_ts)}</span>
        </div>`).join('') : mxEmpty()}
    </div>
  </section>`;
}

/* 第 2 列：ランキング（默认 7 天以网易云为主 + 30 天 daylog + 所有时间网易云） */
function mxColRank(st) {
  const h = st.hist || {};
  const win = st.mxRankWin || 'd7';
  const stats = h.stats || {};
  const neWeek = neRecordRows(st.recWeek);
  const neAll = neRecordRows(st.recAll);

  let rows = [];
  let isNeSource = false;
  let periodLabel = '';
  let songCount = null;
  let playCount = null;

  if (win === 'd7') {
    periodLabel = '7' + t('mx_days');
    if (neWeek.length) {
      isNeSource = true;
      rows = neWeek.slice(0, 15);
      songCount = neWeek.length;
      playCount = neWeek.reduce((acc, r) => acc + (r.plays || 0), 0);
    } else {
      rows = (h.top && h.top.d7) || [];
      songCount = stats.weekSongs;
      playCount = stats.weekPlays;
    }
  } else if (win === 'all') {
    isNeSource = true;
    periodLabel = t('mx_all_time');
    rows = neAll.slice(0, 15);
    songCount = neAll.length || null;
    playCount = neAll.length ? neAll.reduce((acc, r) => acc + (r.plays || 0), 0) : null;
  } else {
    periodLabel = '30' + t('mx_days');
    rows = (h.top && h.top.d30) || [];
    songCount = stats.monthSongs;
    playCount = stats.monthPlays;
  }

  const maxPlays = rows.length ? rows[0].plays : 1;
  const first = rows[0];
  const srcHref = isNeSource
    ? (st.prof ? 'https://music.163.com/#/user/songs/rank?id=' + st.prof.userId : 'https://music.163.com/')
    : '#now';
  const srcLabel = isNeSource ? t('mx_src_ne') : t('mx_src_daylog');

  const chip = (k, label) =>
    `<button class="mx-chip${win === k ? ' on' : ''}" type="button" data-mx-rank="${k}">${label}</button>`;

  return `
  <section class="mx-col">
    <a class="mx-src" href="${srcHref}"${isNeSource ? ' target="_blank" rel="noreferrer"' : ''}>${srcLabel}${ICON.external}</a>
    ${first ? mxHero({
      cover: first.cover ? pic(first.cover, 140) : '', glyph: '♪',
      labelIcon: ICON.trend, label: `${t('mx_rank')}・${periodLabel}`,
      title: esc(first.title), sub: esc(first.artist || ''),
      side: [String(first.plays), t('mx_play_short')],
    }) : mxHero({ glyph: '♪', label: `${t('mx_rank')}・${periodLabel}`, title: '—', sub: '' })}
    <div class="mx-tiles">
      ${mxTile(ICON.list, songCount != null ? String(songCount) : '—', t('mx_rank_songs'), 'blue')}
      ${mxTile(ICON.disc, playCount != null ? String(playCount) : '—', t('mx_rank_plays'))}
    </div>
    ${mxSec(ICON.music, t('mx_rank'), `<div class="mx-chips">
      ${chip('d7', '7' + t('mx_days'))}${chip('d30', '30' + t('mx_days'))}${chip('all', t('mx_all_time'))}
    </div>`)}
    <div class="mx-card">
      ${rows.length ? rows.map((r, i) => `
        <div class="mx-row">
          <span class="n">${i + 1}</span>
          ${mxThumb(r.cover ? pic(r.cover, 80) : '')}
          <div class="b">
            <div class="t">${esc(r.title)}<em>${r.plays} ${t('mx_times')}</em></div>
            <div class="mx-bar"><i style="width:${Math.max(6, Math.round(r.plays / maxPlays * 100))}%"></i></div>
          </div>
        </div>`).join('') : mxEmpty()}
    </div>
    ${(h.artists || []).length ? `
      ${mxSec(ICON.user, t('mx_top_artists'))}
      <div class="mx-card">
        ${(() => {
          const maxA = h.artists[0].plays || 1;
          return h.artists.slice(0, 8).map((a, i) => `
            <div class="mx-row">
              <span class="n">${i + 1}</span>
              ${mxThumb(a.cover ? pic(a.cover, 80) : '')}
              <div class="b">
                <div class="t">${esc(a.name)}<em>${a.plays} ${t('mx_times')}</em></div>
                <div class="mx-bar"><i style="width:${Math.max(6, Math.round(a.plays / maxA * 100))}%"></i></div>
              </div>
            </div>`).join('');
        })()}
      </div>` : ''}
  </section>`;
}

/* 第 3 列：プレイリスト（登录=我的歌单，游客=推荐歌单） */
function mxSoonCol(label) {
  return `<section class="mx-col">
    <span class="mx-src">&nbsp;</span>
    <div class="mx-soon-card">
      ${ICON.empty}
      <b>${t('coming_soon')}</b>
      <button class="btn" type="button" data-ne-login>${t('ne_login')}</button>
    </div>
  </section>`;
}

function mxColPlaylists(st) {
  if (!st.prof) return mxSoonCol(t('mx_playlists'));
  const pls = st.playlists || [];
  const first = pls[0] || null;
  const total = pls.reduce((a, p) => a + (p.count || 0), 0);
  return `
  <section class="mx-col">
    <a class="mx-src" href="https://music.163.com/" target="_blank" rel="noreferrer">${t('mx_open_ne')}${ICON.external}</a>
    ${first ? mxHero({
      cover: first.cover ? pic(first.cover, 140) : '', glyph: '♪',
      labelIcon: ICON.star, label: st.prof ? t('mx_mine') : t('ne_recommend'),
      title: esc(first.name), sub: `${t('mx_library')}・${first.count || 0} ${t('ne_songs')}`,
    }) : mxHero({ glyph: '♪', label: t('ne_playlists'), title: '—', sub: '' })}
    <div class="mx-tiles">
      ${mxTile(ICON.list, String(pls.length), t('mx_playlists'), 'blue')}
      ${mxTile(ICON.music, total.toLocaleString('en-US'), t('mx_pl_total'))}
    </div>
    ${mxSec(ICON.list, t('mx_list'), `<a class="mx-link" href="https://music.163.com/" target="_blank" rel="noreferrer">${t('mx_open_ne')}${ICON.external}</a>`)}
    <div class="mx-card">
      ${pls.length ? pls.slice(0, 12).map(p => `
        <div class="mx-row pl" data-mx-pl="${p.id}" data-mx-name="${esc(p.name)}">
          ${mxThumb(p.cover ? pic(p.cover, 80) : '')}
          <div class="b">
            <div class="t">${esc(p.name)}</div>
            <div class="a">${t('mx_library')}・${p.count || 0} ${t('ne_songs')}</div>
          </div>
          <span class="ch">${ICON.chevronDown}</span>
        </div>`).join('') : mxEmpty()}
    </div>
  </section>`;
}

/* 第 4 列：好きな曲（登录=我喜欢的音乐，游客=官方榜单） */
function mxColFav(st) {
  if (!st.prof) return mxSoonCol(t('ne_liked'));
  if (st.prof) {
    const songs = st.likes || [];
    const first = songs[0] || null;
    const artistSet = new Set();
    songs.forEach(s => (s.ar || []).forEach(a => artistSet.add(a.id || a.name)));
    return `
    <section class="mx-col">
      <a class="mx-src" href="${esc(st.likesHref || 'https://music.163.com/#/my/m/music/playlist')}" target="_blank" rel="noreferrer">${t('mx_open_ne')}${ICON.external}</a>
      ${first ? mxHero({
        cover: songCover(first, 140), glyph: '♥',
        labelIcon: ICON.heart, label: t('mx_first_fav'),
        title: esc(first.name), sub: esc(songArtists(first)),
      }) : mxHero({ glyph: '♥', label: t('ne_liked'), title: '—', sub: '' })}
      <div class="mx-tiles">
        ${mxTile(ICON.heart, String(st.likesTotal || songs.length), t('mx_likes_total'), 'blue')}
        ${mxTile(ICON.user, String(artistSet.size), t('mx_artists'))}
      </div>
      ${mxSec(ICON.list, t('mx_tracks'))}
      <div class="mx-card">
        ${songs.length ? songs.slice(0, 20).map((s, i) => `
          <div class="mx-row">
            <span class="n">${i + 1}</span>
            ${mxThumb(songCover(s))}
            <div class="b">
              <div class="t">${esc(s.name || '')}</div>
              <div class="a">${esc(songArtists(s))}</div>
            </div>
            <span class="mx-acts">
              <span class="on">${ICON.heart}</span>
              <a href="https://music.163.com/song?id=${s.id}" target="_blank" rel="noreferrer" aria-label="open">${ICON.external}</a>
            </span>
          </div>`).join('') : mxEmpty()}
      </div>
    </section>`;
  }

  const charts = st.charts || [];
  return `
  <section class="mx-col">
    <a class="mx-src" href="https://music.163.com/#/discover/toplist" target="_blank" rel="noreferrer">${t('mx_open_ne')}${ICON.external}</a>
    ${charts[0] ? mxHero({
      cover: charts[0].cover ? pic(charts[0].cover, 140) : '', glyph: '♪',
      labelIcon: ICON.trend, label: t('ne_charts'),
      title: esc(charts[0].name), sub: esc(charts[0].sub || ''),
    }) : mxHero({ glyph: '♪', label: t('ne_charts'), title: '—', sub: '' })}
    <div class="mx-tiles">
      ${mxTile(ICON.list, String(charts.length), t('mx_charts_count'), 'blue')}
      ${mxTile(ICON.disc, charts[0] && charts[0].plays ? fmtCount(charts[0].plays) : '—', t('mx_total_plays'))}
    </div>
    ${mxSec(ICON.list, t('ne_charts'))}
    <div class="mx-card">
      ${charts.length ? charts.slice(0, 12).map((c, i) => `
        <div class="mx-row">
          <span class="n">${i + 1}</span>
          ${mxThumb(c.cover ? pic(c.cover, 80) : '')}
          <div class="b">
            <div class="t">${esc(c.name)}</div>
            <div class="a">${esc(c.sub || '')}</div>
          </div>
          <span class="cnt">${c.plays ? fmtCount(c.plays) : ''}</span>
        </div>`).join('') : mxEmpty()}
    </div>
  </section>`;
}

let mxState = null;

function musicSkeleton() {
  return `<div class="mx-grid">
    ${[0, 1, 2, 3].map(() => `<div class="mx-col">
      <div class="mu-skel" style="height:78px"></div>
      <div class="mu-skel" style="height:76px"></div>
      <div class="mu-skel" style="height:26px"></div>
      <div class="mu-skel" style="height:210px"></div>
    </div>`).join('')}
  </div>`;
}

function musicPage(st) {
  const neOnline = !!(st.prof && (st.self || NET.bound));
  const bar = st.prof ? `
    <div class="mu-user">
      <img onerror="this.style.opacity='0'" src="${esc(pic(st.prof.avatarUrl, 80))}" alt="" decoding="async" referrerpolicy="no-referrer">
      <div>
        <b>${esc(st.prof.nickname || '')}</b>
        <span class="mu-status"><i class="vi-dot${neOnline ? ' on' : ''}"></i>netease</span>
      </div>
      ${st.self ? `<button class="btn" type="button" data-ne-logout>${t('ne_logout')}</button>` : ''}
    </div>` : '';

  return `${bar}
  <div class="mx-grid">
    ${mxColRecent(st)}
    ${mxColRank(st)}
    ${mxColPlaylists(st)}
    ${mxColFav(st)}
  </div>`;
}

function mxPaint() {
  const box = $('#muRoot');
  if (!box || !mxState) return;
  box.innerHTML = musicPage(mxState);
  bindMusic();
}

