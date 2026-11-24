'use strict';
/* daylog 设置：日报、隐私规则、健康（单独保存）（由 admin.js 按原顺序拆出，admin.html 按 00→03 顺序加载） */
/* ---------------- daylog（日报、隐私规则、健康）：单独保存 ---------------- */
const lines = a => (a || []).join('\n');
const unlines = s => String(s || '').split('\n').map(x => x.trim()).filter(Boolean);

function renderDaylog(box, S) {
  const d = S.daylog || {}, set = S.secrets_set || {}, st = S.status || {};
  const g = (o, ...k) => k.reduce((x, y) => (x && x[y] != null ? x[y] : undefined), o);
  const v = {};
  const f = (label, id, value, opts = {}) => { const [w, i] = field(label, { id, value, ...opts }); v[id] = i; return w; };
  const chk = (label, id, on) => { const i = h('input', { type: 'checkbox', checked: !!on }); v[id] = i; return h('label', { class: 'chk' }, i, label); };
  const dsec = {};
  const dsecret = (label, name, hint) => {
    const input = h('input', { type: 'password', autocomplete: 'new-password', spellcheck: 'false', placeholder: set[name] ? '已保存，留空不改' : '未设置' });
    const clear = set[name] ? h('input', { type: 'checkbox' }) : null;
    dsec[name] = { input, clear };
    return h('div', {}, h('label', { text: label }), input, clear ? h('label', { class: 'chk' }, clear, '清空这一项') : null,
      hint ? h('div', { class: 'hint', text: hint }) : null);
  };

  /* 状态 + 手动任务 */
  const logBox = h('textarea', { readonly: true, spellcheck: 'false', style: 'min-height:180px' });
  const loadLog = async () => { try { const r = await api('daylog/log'); logBox.value = r.log || '（还没有日志）'; logBox.scrollTop = logBox.scrollHeight; } catch (e) { logBox.value = e.message; } };
  const run = (what, label) => h('button', { class: 'small', text: label, disabled: !st.writer && what !== 'test_tg', onclick: async () => {
    try { await api('daylog/run', { what }); toast('已在后台开始，日志里看进度'); setTimeout(loadLog, 1500); setTimeout(loadLog, 8000); }
    catch (e) { toast(e.message, true); }
  } });
  const tokBox = h('div');
  const statusWin = h('section', { class: 'win' },
    h('h2', { text: 'daylog · 状态' }),
    h('div', { class: 'status' },
      '接管：', st.writer ? h('span', { class: 'ok', text: '这台服务器负责写库、生成日报' })
        : h('span', { class: 'gold', text: '还没接管（只读）：日报和上报仍由原 daylog 容器处理，下面的定时任务不会跑' }),
      h('br'), '数据目录：', h('b', { text: st.root || '（没配置）' }), st.root && !st.root_ok ? h('span', { class: 'bad', text: '（打不开）' }) : null,
      ' · 数据库：', st.db_ready ? h('span', { class: 'ok', text: '已连接' }) : h('span', { class: 'bad', text: '打不开' }),
      h('br'), `inbox 待处理 ${st.inbox || 0} 天`, st.failed ? h('span', { class: 'bad', text: ` · 处理失败 ${st.failed} 个（.json.failed）` }) : null,
      ` · 最新日报 ${st.last_report || '—'}`, st.busy ? h('span', { class: 'gold', text: ' · 有任务正在跑' }) : null,
      st.status_texts_error ? [h('br'), h('span', { class: 'bad', text: 'status-texts.json 解析失败：' + st.status_texts_error })] : null),
    h('div', { style: 'margin:8px 0' }, chk('接管 daylog 写库与定时任务（单容器全新部署请保持开启；与旧 daylog 共用库时关闭）', 'dl_writer', st.writer)),
    h('div', { class: 'row' }, run('pending', '处理 inbox'), run('weekly', '生成周报'), run('icu', '同步 Intervals.icu'), run('test_tg', '发一条测试消息'),
      h('button', { class: 'small', text: '刷新日志', onclick: loadLog })),
    h('div', { style: 'margin-top:10px' }, h('label', { text: '日志（logs/app.log 最后一段）' }), logBox));
  loadLog();

  /* 日报 */
  const report = h('section', { class: 'win' },
    h('h2', { text: 'daylog · 日报' }),
    h('p', { class: 'hint', text: '每天定时把前一天的 ActivityWatch 数据交给 DeepSeek 写日报，再推到 Telegram。' }),
    h('div', { class: 'grid' },
      h('div', { class: 'full' }, chk('生成 AI 分析', 'an_on', g(d, 'analysis', 'enabled'))),
      f('API 地址', 'an_base', g(d, 'analysis', 'api_base'), { placeholder: 'https://api.deepseek.com' }),
      f('模型', 'an_model', g(d, 'analysis', 'model'), { placeholder: 'deepseek-flash' }),
      dsecret('DeepSeek API Key', 'analysis.api_key', '服务器环境变量 DEEPSEEK_API_KEY 有值时优先用环境变量'),
      f('最大输出 token', 'an_tok', g(d, 'analysis', 'max_output_tokens'), { type: 'number', min: 500, max: 16000, step: 500 }),
      f('日报时间（每天）', 'an_daily', g(d, 'analysis', 'daily_time'), { placeholder: '00:10' }),
      f('周报时间', 'an_weekly', g(d, 'analysis', 'weekly_time'), { placeholder: 'Mon 00:40', hint: '星期用英文缩写：Mon Tue Wed Thu Fri Sat Sun' }),
      f('temperature', 'an_temp', g(d, 'analysis', 'temperature'), { type: 'number', min: 0, max: 2, step: 0.05 }),
      h('div', { class: 'full' }, chk('推送到 Telegram', 'tg_on', g(d, 'telegram', 'enabled')), ' ',
        chk('日报', 'tg_daily', g(d, 'telegram', 'send_daily')), ' ', chk('周报', 'tg_weekly', g(d, 'telegram', 'send_weekly'))),
      dsecret('Telegram Bot Token', 'telegram.bot_token'),
      f('Telegram chat id', 'tg_chat', g(d, 'telegram', 'chat_id'), { placeholder: '123456789 或 @频道名' })));

  /* 公开页与隐私 */
  const names = Object.entries(d.app_names || {}).map(([k, x]) => `${k} = ${x}`).join('\n');
  const privacy = h('section', { class: 'win' },
    h('h2', { text: 'daylog · 公开页与隐私' }),
    h('p', { class: 'hint', text: '「此刻」页和日报里怎么称呼你在用的东西。命中规则的窗口不会把标题写进库里。' }),
    h('div', { class: 'grid' },
      f('名字（「红糖 不在电脑前」）', 'site_owner', g(d, 'site', 'owner')),
      h('div', { class: 'full' }, chk('公开页显示窗口标题（默认只显示应用名 / 网站域名）', 'show_title', g(d, 'live', 'public_show_title')), ' ',
        chk('公开页显示健康卡片', 'health_pub', g(d, 'health', 'public'))),
      f('多久没上报算「不在电脑前」（秒）', 'stale', g(d, 'live', 'stale_sec'), { type: 'number', min: 30, max: 3600 }),
      f('「此刻」页刷新间隔（秒）', 'poll', g(d, 'live', 'poll_sec'), { type: 'number', min: 5, max: 120 }),
      f('同一应用间隔多久内算同一段（秒）', 'gap', g(d, 'live', 'segment_gap_sec'), { type: 'number', min: 30, max: 900 }),
      h('div'),
      f('私密应用（一行一个：只记时长，名字显示为「私密应用」）', 'p_secret', lines(g(d, 'privacy', 'secret_apps')), { textarea: true }),
      f('隐藏标题的应用（一行一个：只显示应用名）', 'p_hide', lines(g(d, 'privacy', 'hide_title_apps')), { textarea: true }),
      f('浏览器（一行一个：只显示域名）', 'p_browser', lines(g(d, 'privacy', 'browser_apps')), { textarea: true }),
      f('应用改名（一行一个：进程名 = 显示名）', 'p_names', names, { textarea: true }),
      f('分类规则（JSON）', 'p_cats', JSON.stringify(d.categories || [], null, 1), { textarea: true, full: true,
        hint: '[{"name": "写代码", "patterns": ["(?i)vscode|cursor"]}]，按顺序匹配，第一个命中的算数；规则是正则（Python 写法）' })));

  /* 健康与音乐 */
  const health = h('section', { class: 'win' },
    h('h2', { text: 'daylog · 健康与音乐' }),
    h('div', { class: 'grid' },
      h('div', { class: 'full' }, chk('从 Intervals.icu 拉睡眠 / 静息心率 / HRV', 'icu_on', g(d, 'health', 'icu', 'enabled'))),
      f('Intervals.icu athlete id', 'icu_id', g(d, 'health', 'icu', 'athlete_id'), { placeholder: 'i123456' }),
      dsecret('Intervals.icu API Key', 'health.icu.api_key'),
      f('拉取间隔（分钟）', 'icu_poll', g(d, 'health', 'icu', 'poll_min'), { type: 'number', min: 5, max: 1440 }),
      f('每次拉最近几天', 'icu_days', g(d, 'health', 'icu', 'days'), { type: 'number', min: 1, max: 90 }),
      h('div', { class: 'full' }, chk('记录正在听的歌（封面和链接从下面的 API 查）', 'mu_on', g(d, 'music', 'enabled'))),
      f('音乐 API 地址（NeteaseCloudMusicApi）', 'mu_base', g(d, 'music', 'api_base'), { full: true })));

  const msg = h('span', { class: 'muted', text: '这几块单独保存。' });
  const save = h('button', { class: 'primary', text: '保存 daylog 设置' });
  save.addEventListener('click', async () => {
    let cats;
    try { cats = JSON.parse(v.p_cats.value || '[]'); if (!Array.isArray(cats)) throw new Error('要是一个列表'); }
    catch (e) { return toast('分类规则不是有效的 JSON：' + e.message, true); }
    const appNames = {};
    for (const ln of unlines(v.p_names.value)) {
      const i = ln.indexOf('=');
      if (i <= 0) return toast('应用改名这一行没有「=」：' + ln, true);
      appNames[ln.slice(0, i).trim()] = ln.slice(i + 1).trim();
    }
    const num = id => Number(v[id].value) || 0;
    const body = {
      daylog: {
        site: { title: g(d, 'site', 'title') || '', subtitle: g(d, 'site', 'subtitle') || '', owner: v.site_owner.value },
        analysis: { enabled: v.an_on.checked, api_base: v.an_base.value, model: v.an_model.value, max_output_tokens: num('an_tok'),
          temperature: Number(v.an_temp.value), daily_time: v.an_daily.value.trim(), weekly_time: v.an_weekly.value.trim() },
        telegram: { enabled: v.tg_on.checked, chat_id: v.tg_chat.value, send_daily: v.tg_daily.checked, send_weekly: v.tg_weekly.checked },
        live: { public_show_title: v.show_title.checked, stale_sec: num('stale'), poll_sec: num('poll'), segment_gap_sec: num('gap') },
        privacy: { secret_apps: unlines(v.p_secret.value), hide_title_apps: unlines(v.p_hide.value), browser_apps: unlines(v.p_browser.value) },
        health: { public: v.health_pub.checked, icu: { enabled: v.icu_on.checked, athlete_id: v.icu_id.value, poll_min: num('icu_poll'), days: num('icu_days') } },
        music: { enabled: v.mu_on.checked, api_base: v.mu_base.value },
        app_names: appNames, categories: cats, has_categories: true,
      },
      writer: v.dl_writer.checked,
      secrets: {}, clear: [],
    };
    for (const [name, s] of Object.entries(dsec)) {
      if (s.clear && s.clear.checked) body.clear.push(name);
      else if (s.input.value.trim()) body.secrets[name] = s.input.value.trim();
    }
    save.disabled = true;
    try {
      const r = await api('daylog/settings', body);
      toast('daylog 设置已保存');
      renderDaylog(box, r.settings);
    } catch (e) { toast(e.message, true); msg.textContent = '没保存：' + e.message; msg.className = 'bad'; }
    finally { save.disabled = false; }
  });
  const wins = h('div', {}, report, privacy, health);
  const markDirty = () => { msg.textContent = 'daylog 设置有改动还没保存'; msg.className = 'gold'; };
  wins.addEventListener('input', markDirty);
  statusWin.addEventListener('change', markDirty);
  box.replaceChildren(statusWin, wins, h('section', { class: 'win' }, h('div', { class: 'row' }, save, msg)));
}

async function loadDaylogSettings(box) {
  try { const r = await api('daylog/settings'); renderDaylog(box, r.settings); }
  catch (e) { box.replaceChildren(h('section', { class: 'win' }, h('h2', { text: 'daylog' }), h('p', { class: 'bad', text: '读取 daylog 设置失败：' + e.message }))); }
}

