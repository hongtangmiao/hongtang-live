package xyz.hongtang.live;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.bluetooth.BluetoothAdapter;
import android.bluetooth.BluetoothManager;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.provider.Settings;
import android.text.InputType;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import org.json.JSONObject;

import java.util.Calendar;

/** 唯一的界面：配对 / 状态 / 权限 / 开关。界面用代码拼，和网站一样的夜蓝 + 白框 + 金色 */
public class MainActivity extends Activity {
    private static final int BG = 0xFF0C1533, WIN = 0xFF14204A, LINE = 0xFFF4F4F0, GOLD = 0xFFFFD23F, DIM = 0xFFB9C2E0,
            MUTE = 0xFF7C86B0, RED = 0xFFFF7A6B, GREEN = 0xFF8CE99A;

    private final Handler ui = new Handler(Looper.getMainLooper());
    private LinearLayout root;
    private float dp;
    private TextView tStatus;
    private EditText eServer, eCode, eName;
    private String[] pendingLink; // 点配对链接打开的

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        dp = getResources().getDisplayMetrics().density;
        if (Build.VERSION.SDK_INT >= 21) getWindow().setStatusBarColor(BG);
        takeLink(getIntent());
        build();
        if (Prefs.paired(this)) AgentService.start(this);
        tick();
    }

    @Override
    protected void onNewIntent(Intent i) {
        super.onNewIntent(i);
        takeLink(i);
        build();
    }

    @Override
    protected void onResume() {
        super.onResume();
        build(); // 从系统设置回来：刷新权限状态
    }

    private void takeLink(Intent i) {
        if (i == null || i.getData() == null) return;
        String[] p = Api.parsePairLink(i.getData().toString());
        if (p != null) pendingLink = p;
    }

    /* ---------------- 小工具 ---------------- */

    private int px(float v) { return Math.round(v * dp); }

    private GradientDrawable box(int fill, int stroke, int w) {
        GradientDrawable g = new GradientDrawable();
        g.setColor(fill);
        if (w > 0) g.setStroke(px(w), stroke);
        return g;
    }

    private TextView text(String s, int color, float size) {
        TextView t = new TextView(this);
        t.setText(s);
        t.setTextColor(color);
        t.setTextSize(size);
        t.setLineSpacing(0, 1.25f);
        return t;
    }

    private LinearLayout card(String title) {
        LinearLayout c = new LinearLayout(this);
        c.setOrientation(LinearLayout.VERTICAL);
        c.setBackground(box(WIN, LINE, 3));
        c.setPadding(px(16), px(14), px(16), px(14));
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        lp.bottomMargin = px(16);
        c.setLayoutParams(lp);
        TextView t = text("■ " + title, GOLD, 15);
        t.setPadding(0, 0, 0, px(8));
        c.addView(t);
        root.addView(c);
        return c;
    }

    private Button button(ViewGroup parent, String s, boolean primary, View.OnClickListener l) {
        Button b = new Button(this);
        b.setText(s);
        b.setAllCaps(false);
        b.setTextSize(14);
        b.setTextColor(primary ? BG : LINE);
        b.setBackground(box(primary ? GOLD : WIN, primary ? GOLD : LINE, 2));
        b.setMinHeight(px(40));
        b.setPadding(px(14), 0, px(14), 0);
        b.setOnClickListener(l);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        lp.setMargins(0, px(6), px(8), 0);
        b.setLayoutParams(lp);
        parent.addView(b);
        return b;
    }

    private LinearLayout row(ViewGroup parent) {
        LinearLayout r = new LinearLayout(this);
        r.setOrientation(LinearLayout.HORIZONTAL);
        r.setGravity(Gravity.CENTER_VERTICAL);
        parent.addView(r);
        return r;
    }

    private EditText field(ViewGroup parent, String label, String value, String hint) {
        TextView l = text(label, DIM, 13);
        l.setPadding(0, px(8), 0, px(3));
        parent.addView(l);
        EditText e = new EditText(this);
        e.setSingleLine(true);
        e.setText(value);
        e.setHint(hint);
        e.setHintTextColor(MUTE);
        e.setTextColor(LINE);
        e.setTextSize(15);
        e.setBackground(box(BG, LINE, 2));
        e.setPadding(px(10), px(8), px(10), px(8));
        e.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        parent.addView(e);
        return e;
    }

    private CheckBox check(ViewGroup parent, String label, String key, boolean def) {
        CheckBox c = new CheckBox(this);
        c.setText(label);
        c.setTextColor(LINE);
        c.setTextSize(15);
        c.setChecked(Prefs.on(this, key, def));
        if (Build.VERSION.SDK_INT >= 21) c.setButtonTintList(android.content.res.ColorStateList.valueOf(GOLD));
        c.setOnCheckedChangeListener((v, on) -> {
            Prefs.set(this, key, on);
            if ("hr".equals(key) && on) askBle();
            AgentService.kick(this);
        });
        parent.addView(c);
        return c;
    }

    private void toast(String s) { Toast.makeText(this, s, Toast.LENGTH_LONG).show(); }

    /* ---------------- 界面 ---------------- */

    private String keepServer, keepCode, keepName;

    private void build() {
        // 从别的 App 抄完配对码回来会重建界面：先把已经填的留着
        if (eServer != null) {
            keepServer = eServer.getText().toString();
            keepCode = eCode.getText().toString();
            keepName = eName.getText().toString();
        }
        eServer = eCode = eName = null;
        tStatus = null;
        ScrollView sv = new ScrollView(this);
        sv.setBackgroundColor(BG);
        sv.setFillViewport(true);
        root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(px(16), px(18), px(16), px(28));
        sv.addView(root);

        TextView h = text("▶ 红糖似了吗？", LINE, 20);
        h.setTypeface(Typeface.DEFAULT_BOLD);
        root.addView(h);
        TextView v = text("手机采集程序 " + BuildInfo.version + " · " + Build.MODEL + " · Android " + Build.VERSION.RELEASE, MUTE, 12);
        v.setPadding(0, px(2), 0, px(14));
        root.addView(v);

        if (!Prefs.paired(this) || AgentService.needPair || pendingLink != null) buildPair();
        if (Prefs.paired(this)) buildStatus();
        buildPerms();
        buildSettings();
        setContentView(sv);
    }

    private void buildPair() {
        LinearLayout c = card(AgentService.needPair && Prefs.paired(this) ? "需要重新配对" : "配对");
        if (pendingLink != null) {
            TextView w = text("要把这台手机配对到\n" + pendingLink[0] + "\n吗？确认是你自己的网站再点「配对」。", GOLD, 14);
            c.addView(w);
        } else {
            c.addView(text("在网站的设置页 →「设备」→「添加设备」生成配对码。用手机打开那个页面的话，直接点页面上的配对链接就行。", MUTE, 13));
        }
        String sv0 = pendingLink != null ? pendingLink[0] : keepServer != null ? keepServer : Prefs.sp(this).getString("last_server", "https://");
        eServer = field(c, "服务器地址", sv0, "https://example.com");
        eServer.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        eCode = field(c, "配对码", pendingLink != null ? pendingLink[1] : keepCode != null ? keepCode : "", "XXXX-XXXX");
        eCode.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        eName = field(c, "这台手机在网站上叫什么", keepName != null ? keepName : "", Build.MODEL);
        LinearLayout r = row(c);
        button(r, "配对", true, x -> doPair());
        button(r, "粘贴配对链接", false, x -> {
            ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
            ClipData cd = cm == null ? null : cm.getPrimaryClip();
            String s = cd != null && cd.getItemCount() > 0 && cd.getItemAt(0).getText() != null ? cd.getItemAt(0).getText().toString() : "";
            String[] p = Api.parsePairLink(s);
            if (p == null) {
                toast("剪贴板里不是配对链接（hlpair:// 开头）");
                return;
            }
            eServer.setText(p[0]);
            eCode.setText(p[1]);
        });
    }

    private void doPair() {
        final String server = Api.checkServer(eServer.getText().toString());
        final String code = eCode.getText().toString().trim();
        final String name = eName.getText().toString().trim().isEmpty() ? Build.MODEL : eName.getText().toString().trim();
        if (server == null) {
            toast("服务器地址要以 https:// 或 http:// 开头");
            return;
        }
        if (code.replaceAll("[-\\s]", "").length() != 8) {
            toast("配对码是 8 位（XXXX-XXXX）");
            return;
        }
        toast("配对中…");
        new Thread(() -> {
            Api.Res r = Api.pair(server, code, name);
            String err = null;
            if (r.ok()) {
                try {
                    Prefs.savePairing(this, server, r.json.getString("token"), r.json.getJSONObject("device"), r.json.optJSONObject("server"));
                    Prefs.sp(this).edit().putString("last_server", server).apply();
                } catch (Exception e) {
                    err = "保存失败：" + e.getMessage();
                }
            } else {
                err = r.code == 0 ? r.error : r.json.optString("error", r.error);
            }
            final String fe = err;
            ui.post(() -> {
                if (fe != null) {
                    toast("配对失败：" + fe);
                    return;
                }
                pendingLink = null;
                keepServer = keepCode = keepName = null;
                eServer = null;
                AgentService.needPair = false;
                JSONObject d = Prefs.device(this);
                toast("配对成功：" + d.optString("name") + "（host=" + d.optString("host") + "）");
                AgentService.stop(this);
                AgentService.start(this);
                build();
            });
        }).start();
    }

    private void buildStatus() {
        LinearLayout c = card("状态");
        tStatus = text("", LINE, 14);
        c.addView(tStatus);
        LinearLayout r = row(c);
        if (Prefs.paused(this)) {
            button(r, "继续上报", true, x -> pause(0));
        } else {
            button(r, "暂停 30 分钟", false, x -> pause(30));
            button(r, "暂停到明早", false, x -> pause(-1));
        }
        LinearLayout r2 = row(c);
        button(r2, "立即上报", false, x -> {
            AgentService.kick(this);
            toast("已经让它报一次");
        });
        button(r2, "解除配对", false, x -> new AlertDialog.Builder(this)
                .setMessage("解除配对后这台手机就不再上报了，要用得重新配对。确定？")
                .setPositiveButton("解除", (d, w) -> new Thread(() -> {
                    Api.unpair(this);
                    Prefs.clearPairing(this);
                    ui.post(() -> {
                        AgentService.stop(this);
                        build();
                    });
                }).start())
                .setNegativeButton("算了", null).show());
        refreshStatus();
    }

    private void pause(int min) {
        long until = 0;
        if (min > 0) until = System.currentTimeMillis() + min * 60_000L;
        if (min < 0) {
            Calendar c = Calendar.getInstance();
            c.add(Calendar.DAY_OF_MONTH, 1);
            c.set(Calendar.HOUR_OF_DAY, 6);
            c.set(Calendar.MINUTE, 0);
            c.set(Calendar.SECOND, 0);
            until = c.getTimeInMillis();
        }
        Prefs.sp(this).edit().putLong("paused_until", until).apply();
        AgentService.kick(this);
        build();
    }

    private void refreshStatus() {
        if (tStatus == null) return;
        JSONObject d = Prefs.device(this);
        StringBuilder s = new StringBuilder();
        s.append("服务器：").append(Prefs.server(this)).append('\n');
        s.append("这台手机：").append(d.optString("name")).append("（host=").append(d.optString("host")).append("）\n");
        s.append("服务：").append(AgentService.running ? "运行中" : "没在运行").append('\n');
        if (Prefs.paused(this)) {
            s.append("已暂停到 ").append(android.text.format.DateFormat.format("MM-dd HH:mm", Prefs.pausedUntil(this))).append('\n');
        }
        long ago = AgentService.lastReportAt == 0 ? -1 : (System.currentTimeMillis() - AgentService.lastReportAt) / 1000;
        s.append("上次上报：").append(ago < 0 ? "还没有" : ago < 60 ? ago + " 秒前" : (ago / 60) + " 分钟前").append('\n');
        if (!AgentService.lastResults.isEmpty()) s.append("这一轮：").append(AgentService.lastResults).append('\n');
        s.append("网站上显示：").append(AgentService.lastActivity.isEmpty() ? "—" : AgentService.lastActivity).append('\n');
        s.append("正在听：").append(AgentService.lastMusic.isEmpty() ? "—" : AgentService.lastMusic).append('\n');
        if (Prefs.on(this, "hr", false)) {
            long hrAgo = HrModule.lastBpmAt == 0 ? -1 : (System.currentTimeMillis() - HrModule.lastBpmAt) / 1000;
            s.append("心率：").append(HrModule.status)
                    .append(hrAgo >= 0 && hrAgo < 120 ? " · " + HrModule.lastBpm + " bpm" : "").append('\n');
        }
        if (!AgentService.writer) s.append("服务器还没接管 daylog：「在用什么」暂时写不进去\n");
        if (!AgentService.lastError.isEmpty()) s.append("问题：").append(AgentService.lastError);
        tStatus.setText(s.toString().trim());
        tStatus.setTextColor(AgentService.lastError.isEmpty() ? LINE : RED);
    }

    /* ---------------- 权限 ---------------- */

    private void buildPerms() {
        LinearLayout c = card("系统权限");
        c.addView(text("这几项都要你在系统设置里手动打开；打开以后回到这里会自动刷新。", MUTE, 13));
        perm(c, "使用情况访问（在用什么）", Apps.permitted(this), () -> open(new Intent(Settings.ACTION_USAGE_ACCESS_SETTINGS)));
        perm(c, "通知使用权（正在听；只读播放器，不读通知）", Media.permitted(this),
                () -> open(new Intent("android.settings.ACTION_NOTIFICATION_LISTENER_SETTINGS")));
        if (Build.VERSION.SDK_INT >= 23) {
            PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
            boolean ign = pm != null && pm.isIgnoringBatteryOptimizations(getPackageName());
            perm(c, "不受电池优化限制（熄屏后也能报）", ign, () -> {
                Intent i = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:" + getPackageName()));
                if (!open(i)) open(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS));
            });
        }
        if (Prefs.on(this, "hr", false)) {
            perm(c, "蓝牙 + 位置（心率广播；系统规定扫蓝牙要位置权限）", bleOk(), this::askBle);
        }
        boolean huawei = (Build.MANUFACTURER + Build.BRAND).toLowerCase().matches(".*(huawei|honor).*");
        if (huawei) {
            c.addView(text("华为 / 鸿蒙：在「应用启动管理」里把这个 App 改成「手动管理」，自动启动、关联启动、后台活动三个都打开，不然熄屏一会儿就会被系统停掉。", GOLD, 13));
            button(row(c), "打开应用启动管理", false, x -> openHuaweiStartup());
        } else {
            c.addView(text("小米 / OPPO / vivo 这些手机还要在「自启动 / 后台运行」里允许它，不然熄屏一会儿就会被杀掉。", MUTE, 13));
        }
        button(row(c), "打开这个 App 的系统设置", false, x -> open(new Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                Uri.parse("package:" + getPackageName()))));
    }

    private void perm(LinearLayout c, String label, boolean ok, Runnable fix) {
        LinearLayout r = row(c);
        TextView t = text((ok ? "✓ " : "✗ ") + label, ok ? GREEN : RED, 14);
        t.setLayoutParams(new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        r.addView(t);
        if (!ok) button(r, "去打开", false, x -> fix.run());
    }

    /** 华为的「应用启动管理」：不同版本入口不一样，挨个试，都不行就打开本 App 的设置页 */
    private void openHuaweiStartup() {
        String[][] cs = {
                {"com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"},
                {"com.huawei.systemmanager", "com.huawei.systemmanager.appcontrol.activity.StartupAppControlActivity"},
                {"com.huawei.systemmanager", "com.huawei.systemmanager.optimize.process.ProtectActivity"},
        };
        for (String[] x : cs) {
            try {
                startActivity(new Intent().setComponent(new android.content.ComponentName(x[0], x[1])).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
                return;
            } catch (Exception ignored) {
            }
        }
        toast("打不开，请在「设置 → 应用和服务 → 应用启动管理」里找");
        open(new Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:" + getPackageName())));
    }

    private boolean open(Intent i) {
        try {
            startActivity(i);
            return true;
        } catch (Exception e) {
            toast("这台手机打不开这个设置页，请在系统设置里找一下");
            return false;
        }
    }

    private boolean bleOk() {
        BluetoothManager bm = (BluetoothManager) getSystemService(Context.BLUETOOTH_SERVICE);
        BluetoothAdapter ad = bm != null ? bm.getAdapter() : null;
        boolean loc = Build.VERSION.SDK_INT < 23
                || checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
                || checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED;
        return ad != null && ad.isEnabled() && loc;
    }

    private void askBle() {
        if (Build.VERSION.SDK_INT >= 23) {
            requestPermissions(new String[]{Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION}, 1);
        }
        BluetoothManager bm = (BluetoothManager) getSystemService(Context.BLUETOOTH_SERVICE);
        BluetoothAdapter ad = bm != null ? bm.getAdapter() : null;
        if (ad != null && !ad.isEnabled()) open(new Intent(BluetoothAdapter.ACTION_REQUEST_ENABLE));
    }

    @Override
    public void onRequestPermissionsResult(int code, String[] p, int[] r) {
        build();
        AgentService.kick(this);
    }

    /* ---------------- 开关 ---------------- */

    private void buildSettings() {
        LinearLayout c = card("上报什么");
        LinearLayout pr = row(c);
        button(pr, "随身手机", false, x -> preset(true, true, true, false));
        button(pr, "固定心率设备", false, x -> preset(false, false, true, true));
        c.addView(text("随身手机：在用什么 + 正在听 + 电量。固定心率设备：只报心率和电量（插着电放在手表旁边的那台）。", MUTE, 13));
        check(c, "在用什么（前台应用的名字）", "activity", true);
        check(c, "正在听（播放器里的歌名 / 歌手）", "music", true);
        check(c, "电量", "battery", true);
        check(c, "心率（接手表的蓝牙心率广播，插电的固定设备用）", "hr", false);
        check(c, "开机自动开始", "autostart", true);
        c.addView(text("这里关掉的就不采集；网站设置页里没给这台设备勾的，这里开着也不会报。", MUTE, 13));
        if (Prefs.on(this, "hr", false)) {
            final EditText n = field(c, "手表名字里的关键字（可以不填）", Prefs.sp(this).getString("hr_name", ""), "比如 HUAWEI WATCH");
            button(row(c), "保存关键字", false, x -> {
                Prefs.sp(this).edit().putString("hr_name", n.getText().toString().trim()).apply();
                AgentService.stop(this);
                AgentService.start(this);
                toast("已保存，重新找手表");
            });
        }
    }

    private void preset(boolean act, boolean music, boolean bat, boolean hrOn) {
        Prefs.set(this, "activity", act);
        Prefs.set(this, "music", music);
        Prefs.set(this, "battery", bat);
        Prefs.set(this, "hr", hrOn);
        if (hrOn) askBle();
        AgentService.kick(this);
        toast(hrOn ? "已设成固定心率设备" : "已设成随身手机");
        build();
    }

    private void tick() {
        refreshStatus();
        ui.postDelayed(this::tick, 2000);
    }

    @Override
    protected void onDestroy() {
        ui.removeCallbacksAndMessages(null);
        super.onDestroy();
    }
}
