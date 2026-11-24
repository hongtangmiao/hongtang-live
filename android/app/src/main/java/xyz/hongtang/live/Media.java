package xyz.hongtang.live;

import android.content.ComponentName;
import android.content.Context;
import android.media.MediaMetadata;
import android.media.session.MediaController;
import android.media.session.MediaSessionManager;
import android.media.session.PlaybackState;
import android.provider.Settings;

import org.json.JSONObject;

import java.util.List;

/**
 * 正在播放：系统的媒体会话（要「通知使用权」才能读，App 本身不读也不存任何通知）。
 * 判断「是不是音乐」和电脑端一样：已知的音乐 App 才算，视频 App 一律当视频。
 */
final class Media {
    private static final String[] MUSIC = {"cloudmusic", "netease", "qqmusic", "kugou", "kuwo", "spotify", "music", "migu",
            "xiami", "poweramp", "musicolet", "salt", "apple.android.music", "neteasecloudmusic", "yesplaymusic",
            "mediacenter", "com.huawei.music"};
    private static final String[] VIDEO = {"tv.danmaku.bili", "bilibili", "com.ss.android.ugc", "youtube", "qqlive", "youku", "iqiyi",
            "mgtv", "kuaishou", "com.netflix", "video", "player", "douyu", "huya", "twitch", "tencent.mm", "com.tencent.mobileqq"};

    static boolean permitted(Context c) {
        String s = Settings.Secure.getString(c.getContentResolver(), "enabled_notification_listeners");
        return s != null && s.contains(new ComponentName(c, MediaListener.class).flattenToString());
    }

    static final class Now {
        JSONObject payload;
        String text = "";
    }

    static Now query(Context c) {
        Now n = new Now();
        n.payload = new JSONObject();
        try {
            if (!permitted(c)) {
                n.payload.put("playing", false).put("reason", "unknown");
                return n;
            }
            MediaSessionManager msm = (MediaSessionManager) c.getSystemService(Context.MEDIA_SESSION_SERVICE);
            List<MediaController> list = msm.getActiveSessions(new ComponentName(c, MediaListener.class));
            MediaController pick = null;
            for (MediaController m : list) {
                PlaybackState st = m.getPlaybackState();
                if (st != null && st.getState() == PlaybackState.STATE_PLAYING) {
                    pick = m;
                    break;
                }
            }
            if (pick == null) {
                n.payload.put("playing", false).put("reason", list.isEmpty() ? "none" : "paused");
                return n;
            }
            MediaMetadata md = pick.getMetadata();
            String pkg = pick.getPackageName() == null ? "" : pick.getPackageName().toLowerCase();
            String title = md == null ? "" : nz(md.getString(MediaMetadata.METADATA_KEY_TITLE));
            String artist = md == null ? "" : nz(md.getString(MediaMetadata.METADATA_KEY_ARTIST));
            if (artist.isEmpty() && md != null) artist = nz(md.getString(MediaMetadata.METADATA_KEY_ALBUM_ARTIST));
            String album = md == null ? "" : nz(md.getString(MediaMetadata.METADATA_KEY_ALBUM));
            if (title.isEmpty()) {
                n.payload.put("playing", false).put("reason", "unknown");
                return n;
            }
            boolean music = has(pkg, MUSIC) || (!artist.isEmpty() && !has(pkg, VIDEO));
            if (has(pkg, VIDEO) && !has(pkg, MUSIC)) music = false;
            if (!music) {
                n.payload.put("playing", false).put("reason", "video");
                return n;
            }
            n.payload.put("playing", true).put("title", title).put("artist", artist).put("album", album)
                    .put("source", pkg).put("reason", "music");
            n.text = title + (artist.isEmpty() ? "" : " — " + artist);
        } catch (SecurityException e) {
            try {
                n.payload.put("playing", false).put("reason", "unknown");
            } catch (Exception ignored) {
            }
        } catch (Exception e) {
            try {
                n.payload.put("playing", false).put("reason", "unknown");
            } catch (Exception ignored) {
            }
        }
        return n;
    }

    private static boolean has(String pkg, String[] keys) {
        for (String k : keys) if (pkg.contains(k)) return true;
        return false;
    }

    private static String nz(String s) { return s == null ? "" : s.trim(); }

    private Media() {}
}
