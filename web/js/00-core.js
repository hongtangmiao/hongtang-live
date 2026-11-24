/* =========================================================
   hongtang-live — core & i18n (ja)
   ja / zh-CN · テーマ / 高DPI 対応
   ========================================================= */
'use strict';
// 基础：$ / $$、SITE、语言判断、日语文案（I18N.ja）
// 原来是 web/app.js 的一段；index.html 按文件名顺序加载，后面的文件可以用前面的名字。

const $  = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
const view = $('#view');
const root = document.documentElement;

/* 站点名字、个人资料：服务端在 index.html 里注入（设置页里改），这里不再写死 */
const SITE = Object.assign({ title: '', domain: '', name: '', handle: '', email: '', links: [], ne_api: '' },
  window.HL_SITE || {});

/* ---------------- i18n ---------------- */
const LANGS = ['ja', 'zh'];
const q = new URLSearchParams(location.search);
const qLang = q.get('lang');
// 隐私模式 / 禁用存储时 localStorage 会抛错，读写都兜一下
const store = {
  get(k) { try { return localStorage.getItem(k); } catch (e) { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* 存不了就只在本页生效 */ } },
};
let lang = LANGS.indexOf(qLang) >= 0 ? qLang : (store.get('amk-lang') || 'ja');

const I18N = {
  ja: {
    nav_home: 'ホーム', nav_tweets: 'つぶやき', nav_music: '音楽',
    nav_illust: 'イラスト', nav_videos: 'アニメ',
    nav_account: 'アカウント', nav_stats: 'サーバー統計',

    /* いま */
    nav_now: 'いま', nw_sub: 'いま何をしているか・今日使ったアプリ',
    nw_today: '今日', nw_that_day: 'この日', nw_apps: 'アプリ', nw_last: '最終活動',
    nw_date: '{m}月{d}日', nw_total_on: 'PC の前に {d}', nw_wd: '月,火,水,木,金,土,日',
    nw_listening: '再生中', nw_live: 'ライブ配信', nw_netease: 'NetEase', nw_open: '開く',
    nw_sec_ago: '{n} 秒前', nw_hr_none: '心拍データ未受信', nw_sleep_goal: '目標 8 時間',
    nw_spo2_sub: '夜間平均', nw_stress_sub: '低いほどリラックス',
    nw_prev: '前の日', nw_next: '次の日', nw_back_today: '今日に戻る',
    nw_list_empty: 'この日の記録はまだありません', nw_total_x: '計 {d}',
    nw_refresh_in: '{n} 秒後に更新', nw_viewers: '{n} 人が閲覧中', nw_bat: '🔋{n}%', nw_bat_chg: '⚡{n}%',
    rp_title: 'AI 日報', rp_admin_only: '管理者のみ', rp_none: 'この日の日報はありません', rp_list: '最近の日報',
    rp_active: 'PC の前', rp_focus: '最長集中', rp_switch: '切り替え', rp_keys: 'キー入力', rp_span: '活動時間帯',
    rp_timeline: '時間割の解説', rp_patterns: '傾向', rp_leaks: '時間泥棒', rp_music: '今日の音楽',
    rp_styles: 'ジャンル', rp_obsession: '一番ハマった曲', rp_songs: '聴いた曲', rp_health: '体のデータ',
    rp_quality: 'データ品質', rp_tg: 'Telegram 送信済み', rp_min: '{n} 分', rp_plays: '{n} 回',
    rp_no_ai: 'AI 分析はまだありません（統計のみ）',

    /* home */
    profile: 'プロフィール', height: '身長', weight: '体重', foot: '足のサイズ',

    /* home health & daylog tweets */
    hl_title: 'ヘルスケア', hl_src: 'Huawei Health', hl_sleep: '睡眠時間', hl_score: '睡眠スコア',
    hl_hr: '心拍数', hl_stress: 'ストレス', hl_spo2: '血中酸素',
    hm_more: 'すべて見る', hm_watching: '視聴中のアニメ', hm_watching_empty: '視聴中の作品はありません',
    hm_sys: 'システムリソース', hm_sys_offline: 'デバイスオフライン', hm_cpu: 'CPU', hm_mem: 'メモリ', hm_gpu: 'GPU',
    hl_empty: 'ヘルスデータがありません',
    dl_now: 'いまの活動', dl_online: 'オンライン', dl_offline: '離席中',
    dl_today: '今日のアクティビティ', dl_total_dur: '累計時間', dl_app_cnt: '使用アプリ数',
    dl_live: '視聴中の配信', dl_apps: 'アプリ使用時間',
    dl_empty: '今日のアクティビティ記録はまだありません', dl_err: 'daylog の読み込みに失敗しました',
    /* tweets */
    tw_sub: 'daylog のリアルタイム状態・アプリ使用時間・AI レポート・配信視聴のまとめ。',
    filter: 'フィルター', updated: '最終更新', all: 'すべて',

    /* music */
    mu_sub: 'いま再生中・今日の曲 (daylog)、再生ランキング、プレイリスト、お気に入りのまとめ。',
    pill_playing: '再生中', pill_paused: '一時停止',
    mx_playing: 'いま再生中', mx_last: '最後に聴いた曲', mx_src_ne: '网易云 で 見る',
    mx_total_plays: '総再生回数',
    mx_top_artists: 'よく聴いているアーティスト',
    mx_times: '回',
    mx_open_ne: '网易云 で開く', mx_playlists: 'プレイリスト', mx_pl_total: '曲数の合計',
    mx_mine: 'マイプレイリスト', mx_list: '一覧', mx_library: 'ライブラリ',
    mx_first_fav: 'いちばん人気', mx_likes_total: '曲の合計', mx_artists: 'アーティスト',
    mx_tracks: '曲一覧', mx_charts_count: 'ランキング',
    mx_src_daylog: 'daylog で 見る', mx_today: '今日の曲', mx_today_songs: '曲（今日）', mx_today_plays: '再生（今日）',
    mx_rank: 'よく聴く曲', mx_rank_songs: '曲数', mx_rank_plays: '再生回数',
    mx_days: '日', mx_all_time: '全期間', mx_play_short: '再生', mx_owner_view: 'シェア中',
    mx_just_now: 'たった今', mx_min_ago: '分前', mx_hour_ago: '時間前', mx_day_ago: '日前',
    ne_login: 'ログイン', ne_logout: 'ログアウト', ne_close: '閉じる', ne_group: '連携サービス',
    ne_account: '网易云音楽', ne_account_sub: '最近再生・プレイリスト・お気に入りを同期',
    ne_playlists: 'プレイリスト', ne_liked: 'お気に入り', ne_songs: '曲',
    ne_recommend: 'おすすめプレイリスト', ne_charts: '公式ランキング', ne_signed: 'ログイン中',
    ne_empty: 'データがありません', ne_error: '読み込みに失敗しました',
    ne_qr: 'QR コード', ne_phone: '電話番号', ne_phone_ph: '電話番号', ne_pass_ph: 'パスワード',
    ne_qr_hint: 'NetEase Cloud Music アプリでスキャンしてください', ne_qr_wait: 'スキャン待ち…',
    ne_qr_scanned: 'スマホで確認してください', ne_qr_expired: 'QR コードの有効期限が切れました',
    ne_login_ok: 'ログインしました', ne_logout_ok: 'ログアウトしました',
    ne_login_fail: 'ログインに失敗しました', ne_qr_fail: 'QR コードを取得できませんでした（サーバーが混雑しています）', ne_retry: '再試行', ne_bound_hint: 'すでに連携済みです。再連携は ?bind_key= 付きの URL からどうぞ。', ne_cookie_warn: 'サーバーがログイン情報を受け付けませんでした',
    k_song: '課題曲',
    /* illust / videos */
    il_sub: '描いたイラストを載せるページです。',
    vi_sub: 'bgm.tv と Jellyfin の視聴記録・アニメウォール。',
    vi_watching: '視聴中',
    vi_watched: '見た',
    vi_total: '合計',
    vi_all: 'すべて',
    vi_src_all: '全ソース',
    vi_refresh: '更新',
    vi_empty: '該当する作品がありません。',
    vi_err: '視聴記録を読み込めませんでした',
    vi_ep: '話',
    vi_all_rate: 'みんなの評価', vi_my_rate: '自分の評価', vi_bgm_score: 'bgm 評価', vi_jf_score: 'Jellyfin 評価',
    coming_soon: '準備中です。もう少しお待ちください',

    /* account */
    guest: 'ゲスト', not_logged: '未ログイン',
    login: 'ログイン',
    ac_info: '{name} の情報', mail: 'メール',
    ac_server: 'サーバー情報',
    ac_stats: 'サーバー統計', ac_stats_sub: 'CPU・メモリ・ディスク・GPU の稼働状況',


    /* stats */
    crumb_server: 'サーバー情報',
    st_refresh: '更新',


  },
};
