package xyz.hongtang.live;

import android.app.AppOpsManager;
import android.app.usage.UsageEvents;
import android.app.usage.UsageStats;
import android.app.usage.UsageStatsManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;
import android.os.Process;

import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * 前台应用：系统的「使用情况访问」（UsageStatsManager）。只拿得到包名，拿不到界面里的内容，所以不会有标题。
 * 桌面（启动器）当成「不在用手机」，系统界面 / 自己跳过这一轮。
 */
final class Apps {
    static final class Fg {
        String pkg, label;
        boolean launcher, skip;
    }

    private static final Set<String> SKIP = new HashSet<>();

    static {
        for (String s : new String[]{"android", "com.android.systemui", "com.android.permissioncontroller",
                "com.google.android.permissioncontroller", "com.android.packageinstaller", "com.google.android.packageinstaller",
                "com.android.settings.intelligence", "com.huawei.systemmanager", "com.miui.securitycenter"}) SKIP.add(s);
    }

    private final Context ctx;
    private String lastPkg;
    private long lastQuery;
    private Set<String> launchers;
    private long launchersAt;
    private final Map<String, String> labels = new HashMap<>();

    Apps(Context c) { ctx = c.getApplicationContext(); }

    static boolean permitted(Context c) {
        try {
            AppOpsManager ao = (AppOpsManager) c.getSystemService(Context.APP_OPS_SERVICE);
            int mode = ao.checkOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), c.getPackageName());
            if (mode == AppOpsManager.MODE_DEFAULT) {
                return c.checkCallingOrSelfPermission("android.permission.PACKAGE_USAGE_STATS") == PackageManager.PERMISSION_GRANTED;
            }
            return mode == AppOpsManager.MODE_ALLOWED;
        } catch (Exception e) {
            return false;
        }
    }

    /** 现在在前台的应用；没有权限 / 不知道返回 null */
    Fg foreground() {
        if (!permitted(ctx)) return null;
        UsageStatsManager usm = (UsageStatsManager) ctx.getSystemService(Context.USAGE_STATS_SERVICE);
        if (usm == null) return null;
        long now = System.currentTimeMillis();
        long from = lastQuery > 0 ? Math.max(lastQuery - 5000, now - 30 * 60 * 1000L) : now - 30 * 60 * 1000L;
        try {
            UsageEvents ev = usm.queryEvents(from, now);
            UsageEvents.Event e = new UsageEvents.Event();
            while (ev.hasNextEvent()) {
                ev.getNextEvent(e);
                if (e.getEventType() == UsageEvents.Event.MOVE_TO_FOREGROUND) lastPkg = e.getPackageName();
            }
            if (lastPkg == null) { // 第一次、最近半小时没切过应用：用最后用过的那个
                List<UsageStats> st = usm.queryUsageStats(UsageStatsManager.INTERVAL_DAILY, now - 24 * 3600 * 1000L, now);
                long best = 0;
                if (st != null) for (UsageStats u : st) {
                    if (u.getLastTimeUsed() > best) {
                        best = u.getLastTimeUsed();
                        lastPkg = u.getPackageName();
                    }
                }
            }
        } catch (Exception ex) {
            return null;
        }
        lastQuery = now;
        if (lastPkg == null) return null;
        Fg f = new Fg();
        f.pkg = lastPkg;
        f.launcher = launchers().contains(lastPkg);
        f.skip = SKIP.contains(lastPkg) || lastPkg.equals(ctx.getPackageName());
        f.label = label(lastPkg);
        return f;
    }

    private Set<String> launchers() {
        if (launchers != null && System.currentTimeMillis() - launchersAt < 3600_000L) return launchers;
        Set<String> s = new HashSet<>();
        try {
            Intent i = new Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_HOME);
            List<ResolveInfo> l = ctx.getPackageManager().queryIntentActivities(i, 0);
            for (ResolveInfo r : l) s.add(r.activityInfo.packageName);
        } catch (Exception ignored) {
        }
        launchers = s;
        launchersAt = System.currentTimeMillis();
        return s;
    }

    String label(String pkg) {
        String l = labels.get(pkg);
        if (l != null) return l;
        try {
            PackageManager pm = ctx.getPackageManager();
            ApplicationInfo ai = pm.getApplicationInfo(pkg, 0);
            l = String.valueOf(pm.getApplicationLabel(ai)).trim();
        } catch (Exception e) {
            l = "";
        }
        if (l.isEmpty()) l = pkg;
        if (labels.size() > 300) labels.clear();
        labels.put(pkg, l);
        return l;
    }
}
