'use strict';
/* 设置页公共部分：$/h/toast/api 等工具、首次设置、登录、设置页用的小组件（由 admin.js 按原顺序拆出，admin.html 按 00→03 顺序加载） */
/* 设置页：首次设置 / 登录 / 配置。所有数据都用 textContent 填，不拼 HTML */

const $ = (s, r = document) => r.querySelector(s);
const app = $('#app');

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'value') el.value = v;
    else if (k === 'checked') el.checked = !!v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of kids.flat()) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

let toastTimer;
function toast(msg, err) {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'show' + (err ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = ''; }, err ? 5000 : 2200);
}

async function api(path, body, method) {
  const opt = { method: method || (body !== undefined ? 'POST' : 'GET'), headers: {}, credentials: 'same-origin' };
  if (opt.method !== 'GET') opt.headers['X-HL-Admin'] = '1';
  if (body instanceof Blob) { opt.body = body; opt.headers['Content-Type'] = body.type || 'application/octet-stream'; }
  else if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
  const r = await fetch('/api/admin/' + path, opt);
  let d = {};
  try { d = await r.json(); } catch (e) { /* 空 */ }
  if (!r.ok || d.ok === false) {
    const e = new Error(d.error || ('HTTP ' + r.status));
    e.status = r.status;
    throw e;
  }
  return d;
}

/* 一个输入框：返回 [外层, input] */
function field(label, opts = {}) {
  const input = opts.textarea
    ? h('textarea', { id: opts.id, placeholder: opts.placeholder || '', value: opts.value || '', spellcheck: 'false' })
    : h('input', { id: opts.id, type: opts.type || 'text', placeholder: opts.placeholder || '', value: opts.value ?? '',
      autocomplete: opts.autocomplete || 'off', readonly: opts.readonly, min: opts.min, max: opts.max, step: opts.step, spellcheck: 'false' });
  if (opts.textarea) input.value = opts.value || '';
  const wrap = h('div', { class: opts.full ? 'full' : '' }, h('label', { for: opts.id, text: label }), input,
    opts.hint ? h('div', { class: 'hint', text: opts.hint }) : null);
  return [wrap, input];
}

function fmtTime(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d)) return '—';
  const p = n => String(n).padStart(2, '0');
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/* ---------------- 首次设置 ---------------- */
function renderSetup() {
  $('#head').textContent = '首次设置';
  const [fCode, code] = field('设置码', { id: 'code', placeholder: 'XXXX-XXXX', hint: '在服务器日志里：docker logs <容器名> | grep 设置码' });
  const [fPw, pw] = field('管理员密码（至少 8 位）', { id: 'pw', type: 'password', autocomplete: 'new-password' });
  const [fPw2, pw2] = field('再输一次', { id: 'pw2', type: 'password', autocomplete: 'new-password' });
  const [fTitle, title] = field('站点名字（可以之后再改）', { id: 'title', placeholder: '比如：红糖似了吗？' });
  const [fName, name] = field('你的名字（显示在首页）', { id: 'name' });
  const btn = h('button', { class: 'primary', text: '完成设置' });
  const go = async () => {
    if (pw.value !== pw2.value) return toast('两次密码不一样', true);
    btn.disabled = true;
    try {
      await api('setup', { code: code.value, password: pw.value, title: title.value, name: name.value });
      toast('设置好了');
      boot();
    } catch (e) { toast(e.message, true); } finally { btn.disabled = false; }
  };
  btn.addEventListener('click', go);
  [code, pw, pw2, title, name].forEach(i => i.addEventListener('keydown', e => { if (e.key === 'Enter') go(); }));
  app.replaceChildren(h('div', { class: 'center' }, h('section', { class: 'win' },
    h('h2', { text: '欢迎' }),
    h('p', { class: 'hint', text: '第一次打开：先设一个管理员密码。设置码只在服务器日志里出现，确保只有你能完成这一步。' }),
    h('div', { class: 'list' }, fCode, fPw, fPw2, fTitle, fName, h('div', { class: 'row' }, btn)))));
  code.focus();
}

/* ---------------- 登录 ---------------- */
function renderLogin() {
  $('#head').textContent = '登录';
  const [fPw, pw] = field('管理员密码', { id: 'pw', type: 'password', autocomplete: 'current-password' });
  const btn = h('button', { class: 'primary', text: '登录' });
  const go = async () => {
    btn.disabled = true;
    try { await api('login', { password: pw.value }); boot(); }
    catch (e) { toast(e.message, true); pw.select(); }
    finally { btn.disabled = false; }
  };
  btn.addEventListener('click', go);
  pw.addEventListener('keydown', e => { if (e.key === 'Enter') go(); });
  app.replaceChildren(h('div', { class: 'center' }, h('section', { class: 'win' },
    h('h2', { text: '管理员登录' }), h('div', { class: 'list' }, fPw, h('div', { class: 'row' }, btn)))));
  pw.focus();
}

/* ---------------- 设置 ---------------- */
const secrets = {};   // name -> { input, clear }

function secretField(label, name, set, hint) {
  const input = h('input', { type: 'password', autocomplete: 'new-password', spellcheck: 'false',
    placeholder: set ? '已保存，留空不改' : '未设置' });
  const clear = set ? h('input', { type: 'checkbox' }) : null;
  secrets[name] = { input, clear };
  return h('div', {}, h('label', { text: label }), input,
    clear ? h('label', { class: 'chk' }, clear, '清空这一项') : null,
    hint ? h('div', { class: 'hint', text: hint }) : null);
}

function linkRow(l = {}) {
  const row = h('div', { class: 'item link' });
  const [a] = field('标题', { value: l.title });
  const [b] = field('副标题', { value: l.sub });
  const [c] = field('地址', { value: l.url, placeholder: 'https:// 或 mailto:' });
  const av = h('input', { type: 'checkbox', checked: !!l.avatar });
  row.append(a, b, c, h('label', { class: 'chk', title: '用自己的头像当图标' }, av, '头像'),
    h('button', { class: 'small danger', text: '删除', onclick: () => row.remove() }));
  row._get = () => ({ title: a.querySelector('input').value, sub: b.querySelector('input').value,
    url: c.querySelector('input').value, avatar: av.checked });
  return row;
}

function diskRow(d = {}) {
  const row = h('div', { class: 'item disk' });
  const [a] = field('显示名', { value: d.label, placeholder: '/vol1' });
  const [b] = field('容器里的路径', { value: d.path, placeholder: '/host/vol1' });
  row.append(a, b, h('button', { class: 'small danger', text: '删除', onclick: () => row.remove() }));
  row._get = () => ({ label: a.querySelector('input').value, path: b.querySelector('input').value });
  return row;
}

