'use strict';
/* 主设置表单 renderSettings（由 admin.js 按原顺序拆出，admin.html 按 00→03 顺序加载） */
function renderSettings(cfg, st) {
  $('#head').textContent = '设置';
  $('#logout').hidden = false;
  for (const k of Object.keys(secrets)) delete secrets[k];
  const set = cfg.secrets_set || {};
  const v = {};                       // id -> input
  const f = (label, id, value, opts = {}) => { const [w, i] = field(label, { id, value, ...opts }); v[id] = i; return w; };

  /* 站点与资料 */
  const avatarImg = h('img', { src: '/img/avatar.png?t=' + Date.now(), alt: '' });
  const avatarFile = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp', hidden: true });
  avatarFile.addEventListener('change', async () => {
    const file = avatarFile.files[0];
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) return toast('图片超过 2MB', true);
    try { await api('avatar', file); avatarImg.src = '/img/avatar.png?t=' + Date.now(); toast('头像已更新'); }
    catch (e) { toast(e.message, true); }
    avatarFile.value = '';
  });
  const links = h('div', { class: 'list' }, (cfg.profile.links || []).map(linkRow));
  const site = h('section', { class: 'win' },
    h('h2', { text: '站点与个人资料' }),
    h('p', { class: 'hint', text: '首页、账户页、浏览器标题里显示的内容。' }),
    h('div', { class: 'grid' },
      f('站点名字', 'site_title', cfg.site.title),
      f('域名（显示在名字旁边）', 'site_domain', cfg.site.domain, { placeholder: 'example.com' }),
      f('站点描述（搜索引擎看到的）', 'site_desc', cfg.site.description, { full: true }),
      f('你的名字', 'p_name', cfg.profile.name),
      f('ID（不带 @）', 'p_handle', cfg.profile.handle),
      f('邮箱（账户页显示，留空就不显示）', 'p_email', cfg.profile.email, { type: 'email', full: true }),
      h('div', { class: 'full' }, h('label', { text: '头像' }),
        h('div', { class: 'avatar' }, avatarImg, h('div', { class: 'row' },
          h('button', { class: 'small', text: '上传新头像', onclick: () => avatarFile.click() }),
          st.custom_avatar ? h('button', { class: 'small danger', text: '恢复默认', onclick: async () => {
            try { await api('avatar', undefined, 'DELETE'); avatarImg.src = '/img/avatar.png?t=' + Date.now(); toast('已恢复默认头像'); }
            catch (e) { toast(e.message, true); }
          } }) : null, avatarFile)),
        h('div', { class: 'hint', text: 'PNG / JPEG / WebP，最大 2MB。透明背景的 PNG 效果最好。' })),
      h('div', { class: 'full' }, h('label', { text: '账户页的链接（B 站、GitHub 之类）' }), links,
        h('div', { class: 'row', style: 'margin-top:8px' }, h('button', { class: 'small', text: '＋ 加一个链接', onclick: () => links.append(linkRow()) }))),
    ));
  /* 网易云 */
  const ne = st.netease || {};
  const neStatus = h('div', { class: 'status' },
    ne.bound ? ['绑定账号：', h('b', { text: `${ne.nickname || '?'}（${ne.uid || '?'}）` })] : h('span', { class: 'bad', text: '还没有绑定账号' }),
    h('br'),
    `快照 ${ne.entries || 0} 项 · 上次成功刷新 ${fmtTime(ne.lastOk)} · 本轮刷新 ${ne.refreshed || 0} 项、失败 ${ne.failed || 0} 项`,
    ne.note ? [h('br'), h('span', { class: 'bad', text: ne.note })] : null);
  const netease = h('section', { class: 'win' },
    h('h2', { text: '网易云音乐' }),
    h('p', { class: 'hint', text: '访客打开音乐页只读服务器上的快照，不会去请求网易云。' }),
    neStatus,
    h('div', { class: 'grid' },
      f('API 地址（NeteaseCloudMusicApi）', 'ne_api', cfg.netease.api, { placeholder: 'https://…' }),
      f('歌单 / 喜欢的刷新间隔（小时）', 'ne_hours', cfg.netease.refresh_hours, { type: 'number', min: 1, max: 168, step: 1,
        hint: '最近播放固定 1 小时，排行 6 小时' }),
      secretField('重新绑定口令 bind_key（可选）', 'netease.bind_key', set['netease.bind_key'], '登录了设置页就不需要它；只在没登录时临时换绑才用'),
      h('div', {}, h('label', { text: '账号' }), h('div', { class: 'row' },
        ne.bound
          ? h('button', { class: 'small danger', text: '解绑', onclick: async () => {
              if (!confirm('解绑后音乐页的歌单、喜欢会先空着，直到重新扫码绑定。确定？')) return;
              try { await api('ne/unbind', {}); toast('已解绑'); boot(); } catch (e) { toast(e.message, true); }
            } })
          : null,
        h('a', { href: '/#account', text: ne.bound ? '去换绑（首页 → 账户）' : '去扫码绑定（首页 → 账户）' })))));

  /* 番剧 */
  const an = (st.anime && st.anime.sources) || {};
  const srcLine = (name, s, ready) => !ready ? h('span', { class: 'muted', text: `${name}：没配置` })
    : !s ? h('span', { text: `${name}：已配置（还没拉取过）` })
    : s.ok ? h('span', { class: 'ok', text: `${name}：正常，${s.count} 部` }) : h('span', { class: 'bad', text: `${name}：拉取失败` });
  const anime = h('section', { class: 'win' },
    h('h2', { text: '番剧墙' }),
    h('p', { class: 'hint', text: '两边都填的话会自动合并，同一部只显示一次。' }),
    h('div', { class: 'status' }, srcLine('Bangumi', an.bgm, st.anime && st.anime.bgmReady), ' · ',
      srcLine('Jellyfin', an.jellyfin, st.anime && st.anime.jfReady)),
    h('div', { class: 'grid' },
      f('Bangumi 用户名', 'bgm_user', cfg.bangumi.user, { hint: 'bgm.tv/user/ 后面那一段' }),
      secretField('Bangumi Access Token（可选）', 'bangumi.token', set['bangumi.token'], '想显示私密收藏时才需要：next.bgm.tv/demo/access-token'),
      f('Jellyfin 地址', 'jf_url', cfg.jellyfin.url, { placeholder: 'http://192.168.1.10:8096' }),
      secretField('Jellyfin API Key', 'jellyfin.key', set['jellyfin.key'], 'Jellyfin 控制台 → API 密钥'),
      f('Jellyfin 用户名', 'jf_user', cfg.jellyfin.user, { hint: '留空就用第一个用户' })));

  /* 系统资源 / daylog */
  const disks = h('div', { class: 'list' }, (cfg.stats.disks || []).map(diskRow));
  const pcLine = st.pc && st.pc.last
    ? h('span', { class: st.pc.online ? 'ok' : 'bad', text: (st.pc.online ? '在线' : '离线') + ' · 最后上报 ' + fmtTime(st.pc.last) })
    : h('span', { class: 'muted', text: '还没收到过上报' });
  const stats = h('section', { class: 'win' },
    h('h2', { text: '系统资源' }),
    h('p', { class: 'hint', text: '服务器统计页的两台机器：一台是 PC（采集器上报），一台是这台服务器自己。' }),
    h('div', { class: 'status' }, 'PC：', pcLine),
    h('div', { class: 'grid' },
      f('PC 显示名', 'pc_name', cfg.stats.pc_name),
      f('服务器显示名', 'server_name', cfg.stats.server_name),
      f('服务器 ID（网址里的 host=）', 'server_id', cfg.stats.server_id, { hint: '改了以后旧链接里的 host= 会认不出来' }),
      f('/proc 挂载点（只读）', 'host_proc', cfg.stats.host_proc, { readonly: true }),
      h('div', { class: 'full' }, h('label', { text: '要显示的磁盘' }), disks,
        h('div', { class: 'row', style: 'margin-top:8px' }, h('button', { class: 'small', text: '＋ 加一个磁盘', onclick: () => disks.append(diskRow()) })))));
  const daylog = h('section', { class: 'win' },
    h('h2', { text: 'daylog' }),
    h('div', { class: 'status' }, '数据库：', st.daylog_ready ? h('span', { class: 'ok', text: '已连接' }) : h('span', { class: 'bad', text: '打不开' })),
    h('div', { class: 'grid' },
      f('daylog 数据库路径（容器里）', 'daylog_db', cfg.daylog_db, { placeholder: '/daylog/data/app.db' }),
      f('时区', 'tz', cfg.timezone, { placeholder: 'Asia/Shanghai' })));

  /* 安全 */
  const [fOld, pOld] = field('原密码', { type: 'password', autocomplete: 'current-password' });
  const [fNew, pNew] = field('新密码（至少 8 位）', { type: 'password', autocomplete: 'new-password' });
  const [fNew2, pNew2] = field('再输一次', { type: 'password', autocomplete: 'new-password' });
  const security = h('section', { class: 'win' },
    h('h2', { text: '管理员密码' }),
    h('p', { class: 'hint', text: '改完以后，其他设备上的登录会全部失效。' }),
    h('div', { class: 'grid' }, fOld, h('div'), fNew, fNew2),
    h('div', { class: 'row', style: 'margin-top:10px' }, h('button', { text: '修改密码', onclick: async () => {
      if (pNew.value !== pNew2.value) return toast('两次新密码不一样', true);
      try { await api('password', { old: pOld.value, new: pNew.value }); toast('密码已修改'); pOld.value = pNew.value = pNew2.value = ''; }
      catch (e) { toast(e.message, true); }
    } })));

  /* 保存条 */
  const msg = h('span', { class: 'msg', text: '改动不会自动保存。' });
  const save = h('button', { class: 'primary', text: '保存' });
  save.addEventListener('click', async () => {
    const body = {
      config: {
        site: { title: v.site_title.value, domain: v.site_domain.value, description: v.site_desc.value },
        profile: {
          name: v.p_name.value, handle: v.p_handle.value, email: v.p_email.value,
          links: [...links.children].map(r => r._get()),
        },
        timezone: v.tz.value,
        netease: { api: v.ne_api.value, refresh_hours: Number(v.ne_hours.value) || 0 },
        bangumi: { user: v.bgm_user.value },
        jellyfin: { url: v.jf_url.value, user: v.jf_user.value },
        daylog_db: v.daylog_db.value,
        stats: { pc_name: v.pc_name.value, server_id: v.server_id.value, server_name: v.server_name.value,
          disks: [...disks.children].map(r => r._get()) },
      },
      secrets: {}, clear: [],
    };
    for (const [name, s] of Object.entries(secrets)) {
      if (s.clear && s.clear.checked) body.clear.push(name);
      else if (s.input.value.trim()) body.secrets[name] = s.input.value.trim();
    }
    save.disabled = true;
    try {
      const d = await api('config', body);
      toast('已保存，马上生效');
      renderSettings(d.config, d.status);
      window.scrollTo(0, 0);
    } catch (e) { toast(e.message, true); msg.textContent = '没保存：' + e.message; msg.className = 'msg bad'; }
    finally { save.disabled = false; }
  });
  const bar = h('div', { class: 'savebar' }, h('div', { class: 'in' }, msg, save));
  const forms = h('div', {}, site, netease, anime, stats, daylog);
  forms.addEventListener('input', () => { msg.textContent = '有改动还没保存'; msg.className = 'msg gold'; });
  const dlBox = h('div', {}, h('p', { class: 'muted', text: 'daylog 设置加载中…' }));
  const devBox = h('div', {}, h('p', { class: 'muted', text: '设备加载中…' }));
  app.replaceChildren(devBox, forms, dlBox, security, bar);
  loadDevices(devBox);
  loadDaylogSettings(dlBox);
}

