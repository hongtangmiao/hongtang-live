package xyz.hongtang.live;

import android.service.notification.NotificationListenerService;

/**
 * 只为了拿到「读媒体会话」的权限（系统规定要有一个通知监听服务）。
 * 这里不读、不存、不上报任何通知内容。
 */
public class MediaListener extends NotificationListenerService {
}
