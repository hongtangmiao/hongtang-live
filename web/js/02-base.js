'use strict';
// t() 取文案、主题、提示、图标、数据、小工具、网易云音乐 API

const t = (k) => {
  const s = (I18N[lang] && I18N[lang][k]) || I18N.ja[k] || k;
  return s.indexOf('{name}') >= 0 ? s.split('{name}').join(SITE.name) : s;
};

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
  music: svg('<circle cx="6" cy="17" r="3"/><circle cx="16" cy="17" r="3"/><path d="M9 17V4h10v13"/><path d="M9 8h10"/>'),
  videos: svg('<path d="M7 4v16l13-8z"/>'),
  user: svg('<circle cx="12" cy="8" r="4"/><path d="M6 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2"/>'),
  chevron: svg('<path d="M9 6l6 6-6 6"/>'),
  external: svg('<path d="M12 6H6a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-6"/><path d="M11 13l9-9"/><path d="M15 4h5v5"/>'),
  refresh: svg('<path d="M20 11a8.1 8.1 0 0 0-15.5-2M4 5v4h4"/><path d="M4 13a8.1 8.1 0 0 0 15.5 2M20 19v-4h-4"/>'),
  info: svg('<circle cx="12" cy="12" r="9"/><path d="M12 9h.01"/><path d="M11 12h1v4h1"/>'),
  globe: svg('<circle cx="12" cy="12" r="9"/><path d="M3.6 9h16.8M3.6 15h16.8"/><path d="M11.5 3a17 17 0 0 0 0 18M12.5 3a17 17 0 0 1 0 18"/>'),
  mail: svg('<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7l9 6 9-6"/>'),
  devices: svg('<rect x="13" y="8" width="8" height="12" rx="1"/><path d="M18 8V5a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h9"/>'),
  chevronDown: svg('<path d="M6 9l6 6 6-6"/>'),
  empty: svg('<rect x="3" y="7" width="18" height="13" rx="2"/><path d="M3 7l2-3h14l2 3"/><path d="M9 12h6"/>'),
  star: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 3.4l2.7 5.5 6 .9-4.3 4.2 1 5.9-5.4-2.8-5.4 2.8 1-5.9L3.3 9.8l6-.9z"/></svg>',
  heart: svg('<path d="M12 20s-7-4.4-7-9.3A4.2 4.2 0 0 1 12 7.4 4.2 4.2 0 0 1 19 10.7C19 15.6 12 20 12 20z"/>'),
  clock: svg('<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 1.8"/>'),
  disc: svg('<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="2.2"/>'),
  trend: svg('<path d="M3 16.5l5.5-5.5 3.5 3.5 6-6"/><path d="M14 8h4.5V12.5"/>'),
  list: svg('<path d="M8 6h12M8 12h12M8 18h12"/><circle cx="4" cy="6" r="1.1" fill="currentColor" stroke="none"/><circle cx="4" cy="12" r="1.1" fill="currentColor" stroke="none"/><circle cx="4" cy="18" r="1.1" fill="currentColor" stroke="none"/>'),
};

/* ---------------- helpers ---------------- */
function avatarSelf(size = 40) {
  return `<img class="self-avatar" src="img/avatar.png" width="${size}" height="${size}" alt="" loading="lazy">`;
}
const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

/* ---------------- 网易云音乐 API ---------------- */
const NET = {
  base: SITE.ne_api,
  proxy: false,          // 有 Go 服务端时走同源 /api/ne/*
  session: null,
  bindKey: new URLSearchParams(location.search).get('bind_key') || '',
  KEY: 'ne-cookie',
  PKEY: 'ne-profile',

  get cookie() { return store.get(this.KEY) || ''; },
  set cookie(v) { v ? store.set(this.KEY, v) : store.remove(this.KEY); },
  get profile() {
    if (this.proxy) return (this.session && this.session.profile) || null;
    try { return JSON.parse(store.get(this.PKEY) || 'null'); } catch (e) { return null; }
  },
  set profile(p) { p ? store.set(this.PKEY, JSON.stringify(p)) : store.remove(this.PKEY); },
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

const pic = (url, size = 80) => {
  if (!url) return '';
  const u = String(url).replace(/^http:/, 'https:');
  return u.includes('?') ? u : u + '?param=' + size + 'y' + size;
};
const songArtists = (song) => ((song && (song.ar || song.artists)) || []).map(a => a.name).join(' / ');
const songCover = (song, size = 80) => {
  if (!song) return '';
  const al = (song.al && typeof song.al === 'object') ? song.al : song.al;
  const alb = (song.album && typeof song.album === 'object') ? song.album : null;
  const u = (al && al.picUrl) || (alb && alb.picUrl) || song.picUrl || '';
  return u ? pic(u, size) : '';
};

