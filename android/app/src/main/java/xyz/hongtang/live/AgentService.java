package xyz.hongtang.live;

import android.app.KeyguardManager;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.os.BatteryManager;
import android.os.Build;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.IBinder;
import android.os.PowerManager;

import org.json.JSONObject;

import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;

/**
 * 常驻服务（前台通知）：
 *   亮屏时每 30 秒报一次「在用什么 + 正在听 + 电量」，熄屏 / 锁屏时报一次「离开」，之后 5 分钟报一次电量
 *   开了心率的话，手表的心率 5 秒报一次
 * 所有网络请求都在自己的线程里，按顺序发。
 */
public class AgentService extends Service implements HrModule.Sink {
    static volatile boolean running;
    static volatile long lastReportAt;
    static volatile String lastError = "";
    static volatile String lastActivity = "";
    static volatile String lastMusic = "";
    static volatile String lastResults = "";
    static volatile boolean needPair;
    static volatile boolean writer = true;

    private HandlerThread thread;
    private Handler h;
    private Apps apps;
    private HrModule hr;
    private PowerManager.WakeLock wake;
    private long deviceAt;
    private boolean wasIdle;

    static void start(Context c) {
        Intent i = new Intent(c, AgentService.class);
        try {
            c.startService(i);
        } catch (Exception ignored) {
        }
    }

    /** 改了设置 / 刚配对：马上报一次 */
    static void kick(Context c) {
        Intent i = new Intent(c, AgentService.class).setAction("kick");
        try {
            c.startService(i);
        } catch (Exception ignored) {
        }
    }

    static void stop(Context c) { c.stopService(new Intent(c, AgentService.class)); }

    @Override
    public void onCreate() {
        super.onCreate();
        running = true;
        startForeground(1, notification("启动中…"));
        thread = new HandlerThread("hl-agent");
        thread.start();
        h = new Handler(thread.getLooper());
        apps = new Apps(this);
        IntentFilter f = new IntentFilter();
        f.addAction(Intent.ACTION_SCREEN_ON);
        f.addAction(Intent.ACTION_SCREEN_OFF);
        f.addAction(Intent.ACTION_USER_PRESENT);
        registerReceiver(screen, f);
        applyHr();
        h.post(loop);
    }

    private final BroadcastReceiver screen = new BroadcastReceiver() {
        @Override
        public void onReceive(Context c, Intent i) {
            h.removeCallbacks(loop);
            h.postDelayed(loop, Intent.ACTION_SCREEN_ON.equals(i.getAction()) ? 3000 : 500);
        }
    };

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && "kick".equals(intent.getAction()) && h != null) {
            h.post(this::applyHr);
            h.removeCallbacks(loop);
            h.post(loop);
        }
        return START_STICKY; // 被系统杀了会自己回来
    }

    /** 心率开关：开着才连蓝牙、拿唤醒锁（插着电的固定设备用；随身手机一般不开） */
    private void applyHr() {
        boolean want = Prefs.paired(this) && Prefs.on(this, "hr", false) && Prefs.scope(this, "health");
        if (want && hr == null) {
            hr = new HrModule(this, h, this);
            hr.start();
            PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
            wake = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "hongtang-live:hr");
            wake.setReferenceCounted(false);
            wake.acquire();
        } else if (!want && hr != null) {
            hr.stop();
            hr = null;
            if (wake != null && wake.isHeld()) wake.release();
            wake = null;
        }
    }

    private final Runnable loop = new Runnable() {
        @Override
        public void run() {
            long next;
            try {
                next = reportOnce();
            } catch (Exception e) {
                lastError = "出错了：" + e;
                next = 60_000;
            }
            h.removeCallbacks(this);
            h.postDelayed(this, next);
        }
    };

    private boolean interactive() {
        PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
        KeyguardManager km = (KeyguardManager) getSystemService(Context.KEYGUARD_SERVICE);
        boolean on = pm != null && pm.isInteractive();
        boolean locked = km != null && km.isKeyguardLocked();
        return on && !locked;
    }

    static String iso() {
        return new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ssXXX", Locale.US).format(new Date());
    }

    /** 报一次，返回下一次的间隔（毫秒） */
    private long reportOnce() throws Exception {
        if (!Prefs.paired(this)) {
            notifyText("还没配对");
            return 60_000;
        }
        if (System.currentTimeMillis() - deviceAt > 30 * 60_000L) refreshDevice();
        boolean on = interactive();
        long next = on ? Prefs.reportSec(this) * 1000L : 5 * 60_000L;
        if (Prefs.paused(this)) {
            notifyText("已暂停");
            return Math.min(next, Math.max(5000, Prefs.pausedUntil(this) - System.currentTimeMillis()));
        }
        JSONObject body = new JSONObject().put("ts", iso());
        String actText = null;
        if (Prefs.on(this, "activity", true) && Prefs.scope(this, "activity")) {
            if (!on) {
                if (!wasIdle) body.put("activity", new JSONObject().put("idle", true)); // 熄屏只报一次
                wasIdle = true;
                actText = "熄屏 / 锁屏：不在用手机";
            } else {
                wasIdle = false;
                Apps.Fg fg = apps.foreground();
                if (fg == null) {
                    actText = Apps.permitted(this) ? "还不知道前台是什么" : "没有「使用情况访问」权限";
                } else if (fg.launcher) {
                    body.put("activity", new JSONObject().put("idle", true));
                    actText = "在桌面";
                } else if (!fg.skip) {
                    body.put("activity", new JSONObject().put("app", fg.label).put("package", fg.pkg));
                    actText = fg.label;
                }
            }
        }
        if (Prefs.on(this, "music", true) && Prefs.scope(this, "music") && on) {
            Media.Now m = Media.query(this);
            body.put("music", m.payload);
            lastMusic = m.text;
        }
        if (Prefs.on(this, "battery", true) && Prefs.scope(this, "battery")) {
            Intent b = registerReceiver(null, new IntentFilter(Intent.ACTION_BATTERY_CHANGED));
            if (b != null) {
                int lv = b.getIntExtra(BatteryManager.EXTRA_LEVEL, -1), sc = b.getIntExtra(BatteryManager.EXTRA_SCALE, 100);
                int st = b.getIntExtra(BatteryManager.EXTRA_STATUS, -1);
                if (lv >= 0 && sc > 0) {
                    body.put("battery", new JSONObject().put("level", Math.round(lv * 100f / sc))
                            .put("charging", st == BatteryManager.BATTERY_STATUS_CHARGING || st == BatteryManager.BATTERY_STATUS_FULL));
                }
            }
        }
        if (actText != null) lastActivity = actText;
        if (body.length() <= 1) {
            notifyText(lastActivity.isEmpty() ? "运行中" : lastActivity);
            return next;
        }
        send(body);
        return next;
    }

    private void send(JSONObject body) {
        Api.Res r = Api.report(this, body);
        if (r.code == 401) {
            needPair = true;
            lastError = "服务器不认这台设备了（被停用或删除），请重新配对";
            notifyText("需要重新配对");
            return;
        }
        if (!r.ok()) {
            lastError = r.error;
            notifyText("连不上服务器");
            return;
        }
        needPair = false;
        lastError = "";
        lastReportAt = System.currentTimeMillis();
        JSONObject server = r.json.optJSONObject("server");
        if (server != null) writer = server.optBoolean("writer", true);
        JSONObject res = r.json.optJSONObject("results");
        StringBuilder sb = new StringBuilder();
        if (res != null) {
            JSONObject act = res.optJSONObject("activity");
            if (act != null && act.optBoolean("ok") && !act.optString("label").isEmpty()) lastActivity = act.optString("label");
            java.util.Iterator<String> it = res.keys();
            while (it.hasNext()) {
                String k = it.next();
                JSONObject v = res.optJSONObject(k);
                boolean ok = v != null && v.optBoolean("ok");
                sb.append(label(k)).append(ok ? "✓ " : "✗ ");
                if (!ok && v != null) sb.append(v.optString("error")).append(' ');
            }
        }
        lastResults = sb.toString().trim();
        notifyText(lastActivity.isEmpty() ? "运行中" : lastActivity);
    }

    static String label(String k) {
        switch (k) {
            case "activity": return "在用什么";
            case "music": return "正在听";
            case "health": return "心率";
            case "battery": return "电量";
            default: return k;
        }
    }

    private void refreshDevice() {
        Api.Res r = Api.device(this);
        if (r.ok()) {
            deviceAt = System.currentTimeMillis();
            JSONObject d = r.json.optJSONObject("device");
            if (d != null) Prefs.saveDevice(this, d, r.json.optJSONObject("server"));
            needPair = false;
        } else if (r.code == 401) {
            needPair = true;
            deviceAt = System.currentTimeMillis();
        }
    }

    /** HrModule 回调（在同一个线程里） */
    @Override
    public void onBpm(int bpm) {
        if (!Prefs.paired(this) || Prefs.paused(this)) return;
        try {
            JSONObject body = new JSONObject().put("ts", iso())
                    .put("health", new JSONObject().put("values", new JSONObject().put("heart_rate_live", bpm)));
            send(body);
        } catch (Exception ignored) {
        }
    }

    /* ---------------- 通知 ---------------- */

    private String lastNote = "";

    private void notifyText(String s) {
        if (s.equals(lastNote)) return;
        lastNote = s;
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (nm != null) nm.notify(1, notification(s));
    }

    private Notification notification(String text) {
        if (Build.VERSION.SDK_INT >= 26) {
            NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
            if (nm != null && nm.getNotificationChannel("hl") == null) {
                NotificationChannel ch = new NotificationChannel("hl", "上报状态", NotificationManager.IMPORTANCE_MIN);
                ch.setShowBadge(false);
                nm.createNotificationChannel(ch);
            }
        }
        PendingIntent pi = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class),
                Build.VERSION.SDK_INT >= 23 ? PendingIntent.FLAG_IMMUTABLE : 0);
        Notification.Builder b = Build.VERSION.SDK_INT >= 26 ? new Notification.Builder(this, "hl") : new Notification.Builder(this);
        int icon = getResources().getIdentifier("ic_stat", "drawable", getPackageName());
        b.setContentTitle("红糖似了吗？").setContentText(text).setSmallIcon(icon != 0 ? icon : android.R.drawable.stat_notify_sync)
                .setContentIntent(pi).setOngoing(true).setShowWhen(false);
        if (Build.VERSION.SDK_INT < 26) b.setPriority(Notification.PRIORITY_MIN);
        return b.build();
    }

    @Override
    public void onDestroy() {
        running = false;
        try {
            unregisterReceiver(screen);
        } catch (Exception ignored) {
        }
        if (hr != null) hr.stop();
        if (wake != null && wake.isHeld()) wake.release();
        if (thread != null) thread.quitSafely();
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) { return null; }
}
