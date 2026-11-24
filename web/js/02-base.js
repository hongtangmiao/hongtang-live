'use strict';
// t() 取文案、主题、提示、图标、数据、小工具、网易云音乐 API
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。

const t = (k) => {
  const s = (I18N[lang] && I18N[lang][k]) || I18N.ja[k] || k;
  return s.indexOf('{name}') >= 0 ? s.split('{name}').join(SITE.name) : s;
};
const pick = (v) => (typeof v === 'string' ? v : (v[lang] != null ? v[lang] : v.ja));

/* ---------------- theme ---------------- */
const qTheme = q.get('theme');
let theme = (qTheme === 'dark' || qTheme === 'light')
  ? qTheme
  : (store.get('amk-theme')
      || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'));
root.dataset.theme = theme;

$('#themeBtn').addEventListener('click', () => {
  theme = theme === 'light' ? 'dark' : 'light';
  root.dataset.theme = theme;
  store.set('amk-theme', theme);
});

/* ---------------- toast ---------------- */
let toastTimer;
function toast(msg) {
  const el = $('#toast');
  el.textContent = msg;
  el.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove('show'), 1800);
}

/* ---------------- icons (Tabler 系の線画) ---------------- */
const svg = (d, extra = '') =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"${extra}>${d}</svg>`;

const ICON = {
  home: svg('<path d="M5 12H3l9-9 9 9h-2"/><path d="M5 12v7a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-7"/><path d="M9 21v-6a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v6"/>'),
  tweets: svg('<path d="M3 20l1.3-3.9A8 8 0 1 1 12 20a8.4 8.4 0 0 1-3.4-.7L3 20"/>'),
  music: svg('<circle cx="6" cy="17" r="3"/><circle cx="16" cy="17" r="3"/><path d="M9 17V4h10v13"/><path d="M9 8h10"/>'),
  illust: svg('<circle cx="12" cy="12" r="9"/><path d="M12 21a9 9 0 0 1 0-18c4.97 0 9 3.58 9 8 0 2.2-1.8 4-4 4h-2a2 2 0 0 0-1 3.75A1.3 1.3 0 0 1 12 21"/><circle cx="7.5" cy="10.5" r="1"/><circle cx="12" cy="7.5" r="1"/><circle cx="16.5" cy="10.5" r="1"/>'),
  videos: svg('<path d="M7 4v16l13-8z"/>'),
  user: svg('<circle cx="12" cy="8" r="4"/><path d="M6 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2"/>'),
  chevron: svg('<path d="M9 6l6 6-6 6"/>'),
  chevronUp: svg('<path d="M6 15l6-6 6 6"/>'),
  external: svg('<path d="M12 6H6a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-6"/><path d="M11 13l9-9"/><path d="M15 4h5v5"/>'),
  refresh: svg('<path d="M20 11a8.1 8.1 0 0 0-15.5-2M4 5v4h4"/><path d="M4 13a8.1 8.1 0 0 0 15.5 2M20 19v-4h-4"/>'),
  info: svg('<circle cx="12" cy="12" r="9"/><path d="M12 9h.01"/><path d="M11 12h1v4h1"/>'),
  history: svg('<path d="M12 8v4l3 3"/><path d="M3.05 11a9 9 0 1 1 .5 4"/><path d="M3 21v-5h5"/>'),
  globe: svg('<circle cx="12" cy="12" r="9"/><path d="M3.6 9h16.8M3.6 15h16.8"/><path d="M11.5 3a17 17 0 0 0 0 18M12.5 3a17 17 0 0 1 0 18"/>'),
  file: svg('<path d="M14 3v4a1 1 0 0 0 1 1h4"/><path d="M17 21H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7l5 5v11a2 2 0 0 1-2 2z"/><path d="M9 13h6M9 17h6"/>'),
  key: svg('<circle cx="8" cy="15" r="4"/><path d="M10.85 12.15L19 4"/><path d="M18 5l2 2"/><path d="M15 8l2 2"/>'),
  mail: svg('<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7l9 6 9-6"/>'),
  devices: svg('<rect x="13" y="8" width="8" height="12" rx="1"/><path d="M18 8V5a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h9"/>'),
  chip: svg('<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9.5" y="9.5" width="5" height="5" rx="1"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/>'),
  gpu: svg('<rect x="2" y="6" width="20" height="12" rx="2"/><circle cx="8" cy="12" r="2.6"/><path d="M14 10h4M14 14h4"/>'),
  mem: svg('<rect x="2" y="7" width="20" height="9" rx="2"/><path d="M6 16v3M12 16v3M18 16v3"/><path d="M7 10v3M11 10v3M15 10v3"/>'),
  disk: svg('<ellipse cx="12" cy="6" rx="7.5" ry="3"/><path d="M4.5 6v12c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3V6"/><path d="M4.5 12c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3"/>'),
  eth: svg('<rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 13V9a5 5 0 0 1 10 0v4"/><path d="M16.5 16.5h.01"/>'),
  win: svg('<path d="M4 5.2l7-1v7.3H4zM13 4l7-1v8.5h-7zM4 12.5h7v7.3l-7-1zM13 12.5h7V21l-7-1z"/>'),
  drive: svg('<rect x="3" y="8" width="18" height="8" rx="2"/><path d="M3 12h18"/><path d="M17 14.5h1"/>'),
  chevronDown: svg('<path d="M6 9l6 6 6-6"/>'),
  empty: svg('<rect x="3" y="7" width="18" height="13" rx="2"/><path d="M3 7l2-3h14l2 3"/><path d="M9 12h6"/>'),
  pin: svg('<path d="M9 4h6l-1 6 3 3v2H7v-2l3-3z"/><path d="M12 15v5"/>'),
  star: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 3.4l2.7 5.5 6 .9-4.3 4.2 1 5.9-5.4-2.8-5.4 2.8 1-5.9L3.3 9.8l6-.9z"/></svg>',
  verified: '<svg viewBox="0 0 24 24" width="14" height="14"><path d="M12 2.4l2.5 1.9 3.1.2 1 2.9 2.3 2.1-1 2.9 1 2.9-2.3 2.1-1 2.9-3.1.2L12 21.6l-2.5-1.9-3.1-.2-1-2.9L3.1 14.5l1-2.9-1-2.9 2.3-2.1 1-2.9 3.1-.2z" fill="#1d9bf0"/><path d="M8.4 12.2l2.4 2.4 4.8-4.9" fill="none" stroke="#fff" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  dots: svg('<circle cx="5" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.4" fill="currentColor" stroke="none"/>'),
  repost: svg('<path d="M4 9V7a2 2 0 0 1 2-2h9"/><path d="M13 3l2.5 2.5L13 8"/><path d="M20 15v2a2 2 0 0 1-2 2H9"/><path d="M11 21l-2.5-2.5L11 16"/>'),
  heart: svg('<path d="M12 20s-7-4.4-7-9.3A4.2 4.2 0 0 1 12 7.4 4.2 4.2 0 0 1 19 10.7C19 15.6 12 20 12 20z"/>'),
  bookmark: svg('<path d="M7 4h10v16l-5-3.5L7 20z"/>'),
  funnel: svg('<path d="M4 5h16l-6 7v6l-4 2v-8z"/>'),
  grid: svg('<rect x="4" y="4" width="6.5" height="6.5" rx="1"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1"/>'),
  sliders: svg('<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>'),
  clock: svg('<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 1.8"/>'),
  disc: svg('<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="2.2"/>'),
  mic: svg('<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5.5 11.5a6.5 6.5 0 0 0 13 0"/><path d="M12 18v3"/>'),
  trend: svg('<path d="M3 16.5l5.5-5.5 3.5 3.5 6-6"/><path d="M14 8h4.5V12.5"/>'),
  play: svg('<path d="M8 5.5v13l11-6.5z"/>'),
  list: svg('<path d="M8 6h12M8 12h12M8 18h12"/><circle cx="4" cy="6" r="1.1" fill="currentColor" stroke="none"/><circle cx="4" cy="12" r="1.1" fill="currentColor" stroke="none"/><circle cx="4" cy="18" r="1.1" fill="currentColor" stroke="none"/>'),
};

/* ---------------- helpers ---------------- */
function avatarSelf(size = 40) {
  return `<img class="self-avatar" src="img/avatar.png" width="${size}" height="${size}" alt="" loading="lazy">`;
}
const esc = (s) => String(s).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

/* ---------------- 网易云音乐 API ---------------- */
const NET = {
  base: SITE.ne_api,
  proxy: false,          // 有 server.js 时走同源 /api/ne/*
  session: null,
  bindKey: new URLSearchParams(location.search).get('bind_key') || '',
  KEY: 'ne-cookie',
  PKEY: 'ne-profile',

  get cookie() { return localStorage.getItem(this.KEY) || ''; },
  set cookie(v) { v ? localStorage.setItem(this.KEY, v) : localStorage.removeItem(this.KEY); },
  get profile() {
    if (this.proxy) return (this.session && this.session.profile) || null;
    try { return JSON.parse(localStorage.getItem(this.PKEY) || 'null'); } catch (e) { return null; }
  },
  set profile(p) { p ? localStorage.setItem(this.PKEY, JSON.stringify(p)) : localStorage.removeItem(this.PKEY); },
  get logged() { return this.proxy ? !!(this.session && this.session.logged) : !!this.cookie; },
  clear() { this.cookie = ''; this.profile = null; this.session = null; },

  /* 同源网关是否可用（静态托管时为 false，退回直连） */
  async detect() {
    try {
      const r = await fetch('api/ne/session', { credentials: 'same-origin' });
      if (!r.ok) return false;
      const d = await r.json();
      if (d && d.ok) { this.proxy = true; this.session = d; this.bound = !!d.bound; return true; }
    } catch (e) { /* 没有后端 */ }
    return false;
  },

  async refreshSession() {
    if (!this.proxy) return null;
    try {
      const r = await fetch('api/ne/session', { credentials: 'same-origin' });
      const d = await r.json();
      if (d && d.ok) { this.session = d; this.bound = !!d.bound; }
      return this.session;
    } catch (e) { return this.session; }
  },

  url(path, params) {
    const rel = String(path).replace(/^\//, '');
    const u = this.proxy ? new URL('api/ne/' + rel, location.href) : new URL(rel, this.base);
    Object.entries(params || {}).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') u.searchParams.set(k, v);
    });
    if (!this.proxy && this.cookie) u.searchParams.set('cookie', this.cookie);
    if (this.bindKey) u.searchParams.set('bind_key', this.bindKey);
    u.searchParams.set('timestamp', String(Date.now()));
    return u.toString();
  },

  async api(path, params) {
    const res = await fetch(this.url(path, params), {
      mode: 'cors',
      credentials: this.proxy ? 'same-origin' : 'omit',
    });
    if (!res.ok) {
      let msg = 'HTTP ' + res.status;
      try { const j = await res.json(); if (j && j.error) msg = j.error; } catch (e) {}
      throw new Error(msg);
    }
    const data = await res.json();
    if (data && (data.code === 301 || data.code === 401)) {
      const err = new Error('需要登录'); err.code = 401; throw err;
    }
    return data;
  },

  async post(path, params) {
    const body = new URLSearchParams();
    Object.entries(params || {}).forEach(([k, v]) => { if (v != null && v !== '') body.set(k, v); });
    if (!this.proxy && this.cookie) body.set('cookie', this.cookie);
    body.set('timestamp', String(Date.now()));
    const res = await fetch(this.proxy ? 'api/ne/' + String(path).replace(/^\//, '') : this.base + String(path).replace(/^\//, ''), {
      method: 'POST', credentials: this.proxy ? 'same-origin' : 'omit',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    return res.json();
  },

  async qr() {
    if (this.proxy) {
      const r = await fetch('api/ne/qr', { credentials: 'same-origin' });
      const d = await r.json();
      if (!d || !d.ok || !d.image) throw new Error((d && d.error) || 'qr');
      return { key: d.key, image: d.image };
    }
    const k = await this.api('login/qr/key');
    const key = k.data.unikey;
    const q = await this.api('login/qr/create', { key, qrimg: 1 });
    if (!q || !q.data || !q.data.qrimg) throw new Error('qr');
    return { key, image: q.data.qrimg };
  },

  async whoami() {
    if (this.proxy) {
      const d = await this.refreshSession();
      return (d && d.profile) || null;
    }
    const d = await this.api('user/account');
    if (d && d.profile) { this.profile = d.profile; return d.profile; }
    return null;
  },

  async logout() {
    try { await this.api('logout'); } catch (e) { /* 無視 */ }
    if (this.proxy) { this.session = null; await this.refreshSession(); }
    this.cookie = ''; this.profile = null;
  },
};

const pic = (url, size = 80) => (url ? String(url).replace(/^http:/, 'https:') + '?param=' + size + 'y' + size : '');
const fmtCount = (n) => {
  const v = Number(n) || 0;
  if (v >= 100000000) return (v / 100000000).toFixed(1) + ' 億';
  if (v >= 10000) return (v / 10000).toFixed(1) + ' 万';
  return String(v);
};
const songArtists = (song) => ((song && (song.ar || song.artists)) || []).map(a => a.name).join(' / ');
const songCover = (song, size = 80) => {
  if (!song) return '';
  const al = (song.al && typeof song.al === 'object') ? song.al : song.al;
  const alb = (song.album && typeof song.album === 'object') ? song.album : null;
  const u = (al && al.picUrl) || (alb && alb.picUrl) || song.picUrl || '';
  return u ? pic(u, size) : '';
};

