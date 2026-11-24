package xyz.hongtang.live;

import android.content.Context;
import android.net.Uri;
import android.os.Build;

import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;

/** 服务器接口（docs/device-api.md）。都是阻塞调用，只在后台线程用。 */
final class Api {
    static final class Res {
        int code;          // HTTP 状态码；连不上是 0
        JSONObject json;   // 解析不了是空对象
        String error = ""; // 给人看的

        boolean ok() { return code == 200; }
    }

    static Res call(String base, String path, String token, String method, JSONObject body) {
        Res r = new Res();
        r.json = new JSONObject();
        HttpURLConnection c = null;
        try {
            c = (HttpURLConnection) new URL(base + path).openConnection();
            c.setRequestMethod(method);
            c.setConnectTimeout(10000);
            c.setReadTimeout(15000);
            c.setRequestProperty("User-Agent", "hl-android/" + BuildInfo.version);
            c.setRequestProperty("Accept", "application/json");
            if (token != null && !token.isEmpty()) c.setRequestProperty("Authorization", "Bearer " + token);
            if (body != null) {
                byte[] b = body.toString().getBytes(StandardCharsets.UTF_8);
                c.setDoOutput(true);
                c.setRequestProperty("Content-Type", "application/json; charset=utf-8");
                c.setFixedLengthStreamingMode(b.length);
                try (OutputStream os = c.getOutputStream()) {
                    os.write(b);
                }
            }
            r.code = c.getResponseCode();
            InputStream in = r.code >= 400 ? c.getErrorStream() : c.getInputStream();
            String text = in == null ? "" : readAll(in);
            try {
                r.json = new JSONObject(text);
            } catch (Exception ignored) {
            }
            if (!r.ok()) {
                String e = r.json.optString("error", "");
                r.error = "HTTP " + r.code + (e.isEmpty() ? "" : "：" + e);
            }
        } catch (Exception e) {
            r.code = 0;
            r.error = "连不上服务器：" + e.getClass().getSimpleName() + (e.getMessage() == null ? "" : " " + e.getMessage());
        } finally {
            if (c != null) c.disconnect();
        }
        return r;
    }

    private static String readAll(InputStream in) throws Exception {
        ByteArrayOutputStream bo = new ByteArrayOutputStream();
        byte[] buf = new byte[8192];
        int n;
        while ((n = in.read(buf)) > 0) {
            bo.write(buf, 0, n);
            if (bo.size() > (1 << 20)) break;
        }
        in.close();
        return new String(bo.toByteArray(), StandardCharsets.UTF_8);
    }

    static Res pair(String server, String code, String name) {
        JSONObject b = new JSONObject();
        try {
            b.put("code", code);
            b.put("name", name);
            b.put("kind", "android");
            b.put("host", hostName());
            b.put("app_version", "hl-android/" + BuildInfo.version);
        } catch (Exception ignored) {
        }
        return call(server, "/api/v1/pair", null, "POST", b);
    }

    static Res device(Context c) {
        return call(Prefs.server(c), "/api/v1/device", Prefs.token(c), "GET", null);
    }

    static Res report(Context c, JSONObject body) {
        return call(Prefs.server(c), "/api/v1/report", Prefs.token(c), "POST", body);
    }

    static Res unpair(Context c) {
        return call(Prefs.server(c), "/api/v1/unpair", Prefs.token(c), "POST", new JSONObject());
    }

    /** daylog 里区分设备用：手机型号，只留字母数字 */
    static String hostName() {
        String m = (Build.MODEL == null ? "phone" : Build.MODEL).replaceAll("[^A-Za-z0-9._-]+", "-").replaceAll("^-+|-+$", "");
        return m.isEmpty() ? "phone" : m;
    }

    /** hlpair://pair?s=<服务器>&c=<配对码> → {服务器, 配对码}；不是就返回 null */
    static String[] parsePairLink(String s) {
        if (s == null) return null;
        s = s.trim();
        if (!s.toLowerCase().startsWith("hlpair:")) return null;
        try {
            Uri u = Uri.parse(s);
            String server = u.getQueryParameter("s"), code = u.getQueryParameter("c");
            if (server == null || code == null) return null;
            return new String[]{server.trim().replaceAll("/+$", ""), code.trim()};
        } catch (Exception e) {
            return null;
        }
    }

    static String checkServer(String s) {
        s = s == null ? "" : s.trim().replaceAll("/+$", "");
        if (!s.startsWith("https://") && !s.startsWith("http://")) return null;
        return s;
    }

    private Api() {}
}
