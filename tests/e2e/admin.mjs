// 设置页（/admin）浏览器测试：走一遍首次设置或登录，进到设置页后检查三块（主设置、daylog、设备）都出来了。
// 用法（NAS 上）：SPEC=admin.mjs ADMIN_PW=<密码> [ADMIN_CODE=<设置码>] [EXPECT_UI=<数>] tests/e2e/run.sh [网址] [截图目录]
//   测试实例的管理员密码清空时页面是「首次设置」，要给 ADMIN_CODE（容器日志里的设置码）；有密码时直接登录。
//   只看页面能不能显示，不点任何「保存」。
//
// 算失败：pageerror、console.error（含 CSP 拦截）、本站脚本没加载到、该有的段落标题缺了、
//         设了 EXPECT_UI 时页面控件总数（input/select/textarea/button/h2）对不上。
// 另外打印 UI_COUNTS，拆分 / 合并设置页脚本前后应该一样。
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';

const BASE = (process.argv[2] || 'http://127.0.0.1:8091').replace(/\/$/, '');
const OUT = process.argv[3] || path.join(path.dirname(new URL(import.meta.url).pathname), 'screenshots');
const PW = process.env.ADMIN_PW || '';
const CODE = process.env.ADMIN_CODE || '';
const MUST_H2 = ['站点与个人资料', '系统资源', '管理员密码', 'daylog · 日报', 'daylog · 公开页与隐私', 'daylog · 健康与音乐', '设备'];
const SETTLE_MS = 3000;

fs.mkdirSync(OUT, { recursive: true });
const origin = new URL(BASE).origin;
const fails = [];
let step = '启动';
if (!PW) { console.log('要先设 ADMIN_PW'); process.exit(2); }

const browser = await chromium.launch({ args: ['--disable-dev-shm-usage', '--no-sandbox'] });
const page = await browser.newPage({ viewport: { width: 1280, height: 860 }, locale: 'zh-CN' });
page.on('pageerror', e => fails.push(`[${step}] 未捕获异常：${e.name}: ${e.message}`));
page.on('console', m => { if (m.type() === 'error') fails.push(`[${step}] console.error：${m.text()}`); });
page.on('requestfailed', r => { if (r.url().startsWith(origin)) fails.push(`[${step}] 请求失败 ${r.url()} ${r.failure()?.errorText || ''}`); });
page.on('response', r => {
  if (r.status() >= 400 && r.url().startsWith(origin + '/admin/')) fails.push(`[${step}] HTTP ${r.status()} ${r.url()}`);
});

let n = 0;
const shot = async (s, full = true) => page.screenshot({ path: path.join(OUT, `admin-${String(++n).padStart(2, '0')}-${s}.png`), fullPage: full });

step = '打开 /admin';
await page.goto(`${BASE}/admin`, { waitUntil: 'load', timeout: 30000 });
await page.waitForTimeout(1500);
const scripts = await page.evaluate(() => [...document.scripts].map(s => s.getAttribute('src')).filter(Boolean));
console.log('页面加载的脚本：', scripts.join(' '));

if (await page.locator('#code').count()) {
  step = '首次设置';
  if (!CODE) fails.push('[首次设置] 页面要设置码，但没给 ADMIN_CODE');
  await shot('setup', false);
  await page.fill('#code', CODE);
  await page.fill('#pw', PW);
  await page.fill('#pw2', PW);
  await page.click('button.primary');
} else {
  step = '登录';
  await shot('login', false);
  await page.fill('#pw', PW);
  await page.click('button.primary');
}
console.log(`走的是：${step}`);

step = '设置页';
await page.waitForTimeout(SETTLE_MS);
const info = await page.evaluate(() => {
  const app = document.getElementById('app');
  const q = s => app.querySelectorAll(s).length;
  return {
    h2: [...app.querySelectorAll('h2')].map(x => x.textContent.trim()),
    counts: { input: q('input'), select: q('select'), textarea: q('textarea'), button: q('button'), h2: q('h2') },
    logoutVisible: !document.getElementById('logout').hidden,
  };
});
const total = Object.values(info.counts).reduce((a, b) => a + b, 0);
console.log('段落标题：', info.h2.join(' | '));
console.log(`UI_COUNTS: ${JSON.stringify(info.counts)} 合计 ${total}`);
for (const t of MUST_H2) if (!info.h2.includes(t)) fails.push(`[设置页] 缺了「${t}」这一段`);
if (!info.logoutVisible) fails.push('[设置页] 没显示「退出登录」，可能没登录成功');
if (process.env.EXPECT_UI && Number(process.env.EXPECT_UI) !== total)
  fails.push(`[设置页] 控件总数 ${total}，应该是 ${process.env.EXPECT_UI}（少加载了哪个脚本？）`);
await shot('settings');

step = '手机宽度';
await page.setViewportSize({ width: 390, height: 844 });
await page.reload({ waitUntil: 'load' });
await page.waitForTimeout(SETTLE_MS);
await shot('settings-mobile');

await browser.close();
console.log(`\n截图：${OUT}（${n} 张）`);
console.log(`\n报错（算失败）：${fails.length} 条`);
for (const x of fails) console.log('  ✗ ' + x);
console.log(fails.length ? '\nRESULT: FAIL' : '\nRESULT: PASS');
process.exit(fails.length ? 1 : 0);
