package xyz.hongtang.live;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/** 开机 / App 更新后自动开始上报 */
public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context c, Intent i) {
        String a = i.getAction();
        if (!Intent.ACTION_BOOT_COMPLETED.equals(a) && !Intent.ACTION_MY_PACKAGE_REPLACED.equals(a)) return;
        if (Prefs.paired(c) && Prefs.on(c, "autostart", true)) AgentService.start(c);
    }
}
