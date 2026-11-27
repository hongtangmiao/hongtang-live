// 浏览器冒烟测试：在真 Chromium 里把每个页面点一遍，JS 一报错就算失败，每一步截一张图。
// 用法（NAS 上）：tests/e2e/run.sh [网址] [截图目录]，默认测 8091 测试实例（./run-test.sh 起的）。
//
// 算失败：pageerror（未捕获异常）、console.error（网络加载失败除外）、本站 .js / .css / 字体没加载到、
//         页面主体 #view 是空的、切换语言后 <html lang> 没变、图标字体没加载成功、
//         手机宽度下底部导航不可见、有文字小于 12px；设了环境变量 EXPECT_RULES 时，本站样式规则总数和它不一样也算失败。
// 另外打印 STYLE_RULES（本站样式表的规则总数，@media 里的也算），拆分 / 合并 CSS 前后应该一样。
// 只列出、不算失败：接口取不到数据（测试实例没登录、没外网缓存时常见）。
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';

const BASE = (process.argv[2] || 'http://127.0.0.1:8091').replace(/\/$/, '');
const OUT = process.argv[3] || path.join(path.dirname(new URL(import.meta.url).pathname), 'screenshots');
const ROUTES = ['home', 'now', 'music', 'videos', 'status', 'account', 'stats'];
const SETTLE_MS = 2500;

fs.mkdirSync(OUT, { recursive: true });
const origin = new URL(BASE).origin;
const fails = [];
const network = [];
let step = '启动';
const isOwnJS = u => u.startsWith(origin) && /\.(js|css|woff2?)(\?|$)/.test(u); // 本站脚本、样式、字体

const browser = await chromium.launch({ args: ['--disable-dev-shm-usage', '--no-sandbox'] });
let page = await browser.newPage({ viewport: { width: 1280, height: 860 }, locale: 'ja-JP' });

function watch(page) {
page.on('pageerror', e => fails.push(`[${step}] 未捕获异常：${e.name}: ${e.message}`));
page.on('console', m => {
  if (m.type() !== 'error') return;
  const text = m.text();
  if (/^Failed to load resource/.test(text)) network.push(`[${step}] ${text} ${m.location().url || ''}`);
  else fails.push(`[${step}] console.error：${text}`);
});
page.on('requestfailed', r => {
  const msg = `[${step}] 请求失败 ${r.url()} ${r.failure()?.errorText || ''}`;
  (isOwnJS(r.url()) ? fails : network).push(msg);
});
page.on('response', r => {
  if (r.status() < 400) return;
  const msg = `[${step}] HTTP ${r.status()} ${r.url()}`;
  (isOwnJS(r.url()) ? fails : network).push(msg);
});
}
watch(page);

let n = 0;
const shot = async s => page.screenshot({ path: path.join(OUT, `${String(++n).padStart(2, '0')}-${s}.png`) });
async function viewNotEmpty(name) {
  const len = await page.evaluate(() => (document.getElementById('view')?.innerText || '').trim().length);
  if (len === 0) fails.push(`[${name}] 页面主体 #view 是空的`);
  return len;
}

step = '打开首页（桌面默认 X2）';
await page.goto(`${BASE}/?lang=ja#home`, { waitUntil: 'load', timeout: 30000 });
await page.waitForTimeout(SETTLE_MS);
const x2Len = await page.evaluate(() => (document.getElementById('x2Root')?.innerText || '').trim().length);
if (x2Len === 0) fails.push('[x2] 桌面默认 X2 视图 #x2Root 是空的');
await shot('x2-desktop');
console.log(`桌面默认 X2：正文 ${x2Len} 字`);
await page.evaluate(() => { if (window.HL_UI) window.HL_UI.setMode('classic'); });
await page.waitForTimeout(400);
const scripts = await page.evaluate(() => [...document.scripts].map(s => s.getAttribute('src')).filter(Boolean));
console.log('页面加载的脚本：', scripts.join(' '));
const sheets = await page.evaluate(() => [...document.querySelectorAll('link[rel=stylesheet]')].map(l => l.getAttribute('href')));
console.log('页面加载的样式：', sheets.join(' '));
const rules = await page.evaluate(() => {
  const count = list => [...list].reduce((s, r) => s + 1 + (r.cssRules ? count(r.cssRules) : 0), 0);
  let total = 0;
  for (const sh of document.styleSheets) {
    if (!sh.href || new URL(sh.href).origin !== location.origin) continue;
    try { total += count(sh.cssRules); } catch { /* 跨域表读不到 */ }
  }
  return total;
});
console.log(`STYLE_RULES: ${rules}`);
if (process.env.EXPECT_RULES && Number(process.env.EXPECT_RULES) !== rules)
  fails.push(`[样式] 规则总数 ${rules}，应该是 ${process.env.EXPECT_RULES}（少加载了哪个 css？）`);

const sizes = new Set();
const smallText = [];
const scanFonts = () => page.evaluate(() => {
  const out = [];
  const walk = el => {
    const hasText = [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim());
    const r = el.getBoundingClientRect();
    if (hasText && r.width > 0 && r.height > 0) {
      // JS 自己按宽度缩放的（stats.js 的磁盘名）不算：那是运行时拟合，不是样式表里的字号
      if (el.style && el.style.fontSize) return;   // 整棵子树都按这个缩放，不再往下看
      const fs = parseFloat(getComputedStyle(el).fontSize);
      if (fs) out.push({
        fs,
        where: el.tagName.toLowerCase() + (el.className ? '.' + String(el.className).trim().split(/\s+/).slice(0, 2).join('.') : '')
      });
    }
    for (const c of el.children) walk(c);
  };
  walk(document.body);
  return out;
});
for (const r of ROUTES) {
  step = r;
  await page.evaluate(id => { location.hash = id; }, r);
  await page.waitForTimeout(SETTLE_MS);
  const len = await viewNotEmpty(r);
  await shot(r);
  console.log(`路由 #${r}：正文 ${len} 字`);
  for (const it of await scanFonts()) {
    sizes.add(it.fs);
    if (it.fs < 12) smallText.push(`#${r} ${it.where} ${it.fs}px`);
  }
}

step = '图标字体';
const fonts = await page.evaluate(async () => {
  await document.fonts.ready;
  return [...document.fonts].filter(f => /tabler/i.test(f.family)).map(f => f.status);
});
console.log(`图标字体 tabler-icons：${fonts.join(',') || '没有声明'}`);
if (!fonts.includes('loaded')) fails.push(`[图标字体] tabler-icons 没加载成功：${fonts.join(',') || '没有声明'}`);

step = '字号';
const tiers = [...sizes].sort((a, b) => a - b);
console.log(`字号档位：${tiers.map(x => x + 'px').join(' ')}（${tiers.length} 档）`);
const uniqSmall = [...new Set(smallText)].slice(0, 10);
if (uniqSmall.length) fails.push(`[字号] ${uniqSmall.length} 处小于 12px：${uniqSmall.join(' / ')}`);

step = '切换语言';
await page.evaluate(() => { location.hash = 'home'; });
await page.waitForTimeout(1000);
const before = await page.evaluate(() => document.documentElement.lang);
await page.locator('.lang-toggle:visible').first().click();
await page.waitForTimeout(SETTLE_MS);
const after = await page.evaluate(() => document.documentElement.lang);
if (before === after) fails.push(`[切换语言] <html lang> 没变：${before}`);
await viewNotEmpty(step);
await shot(`lang-${after}`);
console.log(`切换语言：${before} → ${after}`);

step = '切换语言后的音乐页';
await page.evaluate(() => { location.hash = 'music'; });
await page.waitForTimeout(SETTLE_MS);
await viewNotEmpty(step);
await shot(`music-${after}`);

// 手机宽度：窄屏样式（@media）和底部导航
await page.close();
page = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, locale: 'ja-JP' });
watch(page);
step = '手机：打开首页';
await page.goto(`${BASE}/?lang=ja#home`, { waitUntil: 'load', timeout: 30000 });
await page.waitForTimeout(SETTLE_MS);
for (const r of ['home', 'music', 'now', 'stats']) {
  step = `手机 ${r}`;
  await page.evaluate(id => { location.hash = id; }, r);
  await page.waitForTimeout(SETTLE_MS);
  const len = await viewNotEmpty(step);
  await shot(`m-${r}`);
  console.log(`手机 #${r}：正文 ${len} 字`);
}
step = '手机：底部导航';
const navOK = await page.locator('.bottom-nav').first().isVisible().catch(() => false);
if (!navOK) fails.push('[手机] 底部导航 .bottom-nav 不可见');
await page.evaluate(() => window.scrollTo(0, 0));
await shot('m-bottom-nav');
console.log(`手机底部导航可见：${navOK}`);

await browser.close();

console.log(`\n截图：${OUT}（${n} 张）`);
const net = [...new Set(network)];
console.log(`\n网络问题（只列出，不算失败）：${net.length} 条`);
for (const x of net.slice(0, 40)) console.log('  ' + x);
console.log(`\nJS 报错（算失败）：${fails.length} 条`);
for (const x of fails) console.log('  ✗ ' + x);
console.log(fails.length ? '\nRESULT: FAIL' : '\nRESULT: PASS');
process.exit(fails.length ? 1 : 0);
