package xyz.hongtang.live;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.Build;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;

import org.json.JSONArray;
import org.json.JSONObject;

import java.nio.charset.StandardCharsets;
import java.security.KeyStore;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * 设置：服务器地址、设备口令（Android 6+ 用系统密钥库加密，只有这个 App 能解开）、设备信息、各项开关。
 * 都存在 App 自己的私有目录里，不备份（allowBackup=false）。
 */
final class Prefs {
    private static final String ALIAS = "hongtang-live-token";

    static SharedPreferences sp(Context c) {
        return c.getApplicationContext().getSharedPreferences("hl", Context.MODE_PRIVATE);
    }

    static String server(Context c) { return sp(c).getString("server", ""); }

    static boolean paired(Context c) {
        return !server(c).isEmpty() && !sp(c).getString("token_enc", "").isEmpty();
    }

    /* ---------------- 开关 ---------------- */

    static boolean on(Context c, String key, boolean def) { return sp(c).getBoolean("on_" + key, def); }

    static void set(Context c, String key, boolean v) { sp(c).edit().putBoolean("on_" + key, v).apply(); }

    static long pausedUntil(Context c) { return sp(c).getLong("paused_until", 0); }

    static boolean paused(Context c) { return System.currentTimeMillis() < pausedUntil(c); }

    /* ---------------- 设备信息（配对 / 定期刷新时服务器给的） ---------------- */

    static JSONObject device(Context c) {
        try {
            return new JSONObject(sp(c).getString("device", "{}"));
        } catch (Exception e) {
            return new JSONObject();
        }
    }

    static boolean scope(Context c, String s) {
        JSONArray a = device(c).optJSONArray("scopes");
        if (a == null) return false;
        for (int i = 0; i < a.length(); i++) if (s.equals(a.optString(i))) return true;
        return false;
    }

    static int reportSec(Context c) {
        int n = sp(c).getInt("report_sec", 30);
        return Math.max(10, Math.min(600, n));
    }

    static void savePairing(Context c, String server, String token, JSONObject device, JSONObject hints) throws Exception {
        SharedPreferences.Editor e = sp(c).edit()
                .putString("server", server)
                .putString("token_enc", protect(token))
                .putString("device", device.toString());
        if (hints != null) e.putInt("report_sec", hints.optInt("report_sec", 30));
        e.apply();
    }

    static void saveDevice(Context c, JSONObject device, JSONObject hints) {
        SharedPreferences.Editor e = sp(c).edit().putString("device", device.toString());
        if (hints != null) e.putInt("report_sec", hints.optInt("report_sec", 30));
        e.apply();
    }

    static void clearPairing(Context c) {
        sp(c).edit().remove("server").remove("token_enc").remove("device").apply();
    }

    static String token(Context c) {
        String enc = sp(c).getString("token_enc", "");
        if (enc.isEmpty()) return "";
        try {
            return unprotect(enc);
        } catch (Exception e) {
            return ""; // 密钥丢了（恢复出厂 / 换机）：重新配对
        }
    }

    /* ---------------- 口令加密 ---------------- */

    private static SecretKey key() throws Exception {
        KeyStore ks = KeyStore.getInstance("AndroidKeyStore");
        ks.load(null);
        if (!ks.containsAlias(ALIAS)) {
            KeyGenerator kg = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
            kg.init(new KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .build());
            kg.generateKey();
        }
        return ((KeyStore.SecretKeyEntry) ks.getEntry(ALIAS, null)).getSecretKey();
    }

    static String protect(String s) throws Exception {
        if (Build.VERSION.SDK_INT < 23) return "plain:" + s; // Android 5 没有可用的密钥库：靠 App 私有目录
        Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
        c.init(Cipher.ENCRYPT_MODE, key());
        byte[] ct = c.doFinal(s.getBytes(StandardCharsets.UTF_8));
        return "ks:" + Base64.encodeToString(c.getIV(), Base64.NO_WRAP) + ":" + Base64.encodeToString(ct, Base64.NO_WRAP);
    }

    static String unprotect(String enc) throws Exception {
        if (enc.startsWith("plain:")) return enc.substring(6);
        String[] p = enc.split(":");
        if (p.length != 3 || !"ks".equals(p[0])) throw new IllegalStateException("bad token");
        Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
        c.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, Base64.decode(p[1], Base64.NO_WRAP)));
        return new String(c.doFinal(Base64.decode(p[2], Base64.NO_WRAP)), StandardCharsets.UTF_8);
    }

    private Prefs() {}
}
