'use strict';
// 网易云登录
/* ---------------- 网易云：登录 ---------------- */
let nePoll = null;
const stopNePoll = () => { if (nePoll) { clearInterval(nePoll); nePoll = null; } };

function neCard() {
  const p = NET.profile;
  if (NET.logged && p) {
    return `<button class="acct-card" type="button" data-ne-open>
      <span class="acct-card-avatar"><img onerror="this.style.opacity='0'" src="${esc(pic(p.avatarUrl, 80))}" alt="" style="width:100%;height:100%;object-fit:cover"></span>
      <span class="acct-card-text"><b>${esc(p.nickname || '')}</b><small>${t('ne_account')} · ${t('ne_signed')}</small></span>
      <span class="acct-trail">${ICON.chevron}</span>
    </button>`;
  }
  return `<button class="acct-card" type="button" data-ne-open>
    <span class="acct-card-icon ne-ico">♪</span>
    <span class="acct-card-text"><b>${t('ne_account')}</b><small>${t('ne_account_sub')}</small></span>
    <span class="acct-trail">${ICON.chevron}</span>
  </button>`;
}

function closeNeModal() {
  stopNePoll();
  const ov = $('#neModal');
  if (ov) ov.remove();
}

async function startNeQr(ov) {
  const box = $('#neQr', ov), hint = $('#neHint', ov);
  box.innerHTML = '<div class="spinner"></div>';
  hint.textContent = t('ne_qr_wait');

  let q = null;
  for (let i = 0; i < 2 && !q; i++) {
    try { q = await NET.qr(); } catch (e) { await new Promise(r => setTimeout(r, 600)); }
  }
  if (!q) {
    box.innerHTML = `<div class="ne-qr-fail"><span>${NET.bound ? t('ne_bound_hint') : t('ne_qr_fail')}</span>
      <button class="btn" type="button" data-qr-retry>${t('ne_retry')}</button></div>`;
    hint.textContent = '';
    const btn = $('[data-qr-retry]', ov);
    if (btn) btn.addEventListener('click', () => startNeQr(ov));
    return;
  }

  box.innerHTML = `<img src="${esc(q.image)}" alt="QR">`;
  hint.textContent = t('ne_qr_hint');
  const key = q.key;
  stopNePoll();
  nePoll = setInterval(async () => {
    try {
      const st = await NET.api('login/qr/check', { key });
      if (st.code === 800) {
        stopNePoll();
        hint.textContent = t('ne_qr_expired');
        const btn = document.createElement('button');
        btn.className = 'btn'; btn.type = 'button'; btn.textContent = t('ne_retry');
        btn.addEventListener('click', () => startNeQr(ov));
        hint.after(btn);
      } else if (st.code === 802) {
        hint.textContent = t('ne_qr_scanned');
      } else if (st.code === 803) {
        stopNePoll();
        if (NET.proxy) {
          await NET.refreshSession();
        } else {
          NET.cookie = st.cookie || '';
          await NET.whoami();
        }
        if (!NET.logged) { hint.textContent = t('ne_cookie_warn'); return; }
        closeNeModal();
        toast(t('ne_login_ok'));
        render(true);
      }
    } catch (e) { /* ネットワークのゆらぎは無視 */ }
  }, 2000);
}

async function nePhoneLogin(ov) {
  const phone = ($('#nePhone', ov).value || '').trim();
  const pass = $('#nePass', ov).value || '';
  const hint = $('#nePhoneHint', ov);
  if (!phone || !pass) { hint.textContent = t('ne_login_fail'); return; }
  const btn = $('#neSubmit', ov);
  btn.disabled = true;
  hint.textContent = t('ne_qr_wait');
  try {
    const r = await NET.post('login/cellphone', { phone, password: pass, countrycode: '86' });
    if (r.code === 200 && r.cookie) {
      NET.cookie = r.cookie;
      const prof = await NET.whoami();
      if (!prof) { NET.clear(); hint.textContent = t('ne_cookie_warn'); return; }
      closeNeModal();
      toast(t('ne_login_ok'));
      render(true);
    } else {
      hint.textContent = r.message || r.msg || t('ne_login_fail');
    }
  } catch (e) {
    hint.textContent = t('ne_login_fail') + '（' + (e.message || e) + '）';
  } finally {
    btn.disabled = false;
  }
}

function openNeModal() {
  closeNeModal();
  const p = NET.profile;
  const viewer = !!(NET.session && NET.session.owner);   // 访客用的是站点账号
  const ov = document.createElement('div');
  ov.className = 'modal-ov';
  ov.id = 'neModal';
  ov.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-label="${t('ne_account')}">
    <div class="modal-hd"><b>${t('ne_account')}</b>
      <button class="modal-x" type="button" aria-label="${t('ne_close')}">✕</button></div>
    <div class="modal-bd">
      ${NET.logged && p ? `
        <div class="ne-user">
          <img onerror="this.style.opacity='0'" src="${esc(pic(p.avatarUrl, 80))}" alt="">
          <div><b>${esc(p.nickname || '')}</b><small>UID ${p.userId}</small></div>
        </div>
        ${viewer
          ? `<p class="ne-hint" style="margin-top:14px">${t('mx_owner_view')}</p>`
          : `<button class="btn" type="button" id="neLogout" style="margin-top:18px">${t('ne_logout')}</button>`}`
      : `
        <div class="ne-tabs">
          <button class="mt on" type="button" data-ne-tab="qr">${t('ne_qr')}</button>
          <button class="mt" type="button" data-ne-tab="phone">${t('ne_phone')}</button>
        </div>
        <div data-pane="qr">
          <div class="ne-qr" id="neQr"><div class="spinner"></div></div>
          <p class="ne-hint" id="neHint">${t('ne_qr_wait')}</p>
        </div>
        <div data-pane="phone" hidden>
          <input class="ne-input" id="nePhone" inputmode="tel" autocomplete="tel" placeholder="${t('ne_phone_ph')}">
          <input class="ne-input" id="nePass" type="password" autocomplete="current-password" placeholder="${t('ne_pass_ph')}">
          <button class="btn-accent" type="button" id="neSubmit" style="width:100%">${t('ne_login')}</button>
          <p class="ne-hint" id="nePhoneHint"></p>
        </div>`}
    </div>
  </div>`;
  document.body.appendChild(ov);

  ov.addEventListener('click', (e) => { if (e.target === ov) closeNeModal(); });
  $('.modal-x', ov).addEventListener('click', closeNeModal);

  if (NET.logged && p && !viewer) {
    $('#neLogout', ov).addEventListener('click', async () => {
      await NET.logout();
      closeNeModal();
      toast(t('ne_logout_ok'));
      render(true);
    });
  } else {
    $$('[data-ne-tab]', ov).forEach(b => b.addEventListener('click', () => {
      $$('[data-ne-tab]', ov).forEach(x => x.classList.toggle('on', x === b));
      $$('[data-pane]', ov).forEach(pane => { pane.hidden = pane.dataset.pane !== b.dataset.neTab; });
    }));
    $('#neSubmit', ov).addEventListener('click', () => nePhoneLogin(ov));
    startNeQr(ov);
  }
}


