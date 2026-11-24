package xyz.hongtang.live;

import android.bluetooth.BluetoothAdapter;
import android.bluetooth.BluetoothDevice;
import android.bluetooth.BluetoothGatt;
import android.bluetooth.BluetoothGattCallback;
import android.bluetooth.BluetoothGattCharacteristic;
import android.bluetooth.BluetoothGattDescriptor;
import android.bluetooth.BluetoothGattService;
import android.bluetooth.BluetoothManager;
import android.bluetooth.le.BluetoothLeScanner;
import android.bluetooth.le.ScanCallback;
import android.bluetooth.le.ScanResult;
import android.bluetooth.le.ScanSettings;
import android.content.Context;
import android.os.Build;
import android.os.Handler;
import android.os.ParcelUuid;

import java.util.List;
import java.util.UUID;

/**
 * 心率：接手表 / 手环的标准蓝牙心率广播（0x180D），5 秒交一次给 AgentService 上报。
 * 搬自原来的 daylog 心率接收器：扫描 → 连接 → 订阅，断了自动重连。
 */
final class HrModule {
    interface Sink { void onBpm(int bpm); }

    private static final UUID SVC_HR = UUID.fromString("0000180d-0000-1000-8000-00805f9b34fb");
    private static final UUID CHR_HR = UUID.fromString("00002a37-0000-1000-8000-00805f9b34fb");
    private static final UUID CCCD = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb");

    static volatile String status = "没开";
    static volatile int lastBpm = 0;
    static volatile long lastBpmAt = 0;

    private final Context ctx;
    private final Handler h;
    private final Sink sink;
    private BluetoothAdapter adapter;
    private BluetoothLeScanner scanner;
    private BluetoothGatt gatt;
    private boolean scanning, connecting, stopped;
    private int failures;
    private long lastPush;

    HrModule(Context c, Handler handler, Sink s) {
        ctx = c.getApplicationContext();
        h = handler;
        sink = s;
    }

    void start() {
        stopped = false;
        BluetoothManager bm = (BluetoothManager) ctx.getSystemService(Context.BLUETOOTH_SERVICE);
        adapter = bm != null ? bm.getAdapter() : null;
        h.post(this::startScan);
    }

    void stop() {
        stopped = true;
        h.removeCallbacksAndMessages(this);
        stopScan();
        closeGatt();
        status = "没开";
    }

    private void retry(long ms) {
        if (stopped) return;
        stopScan();
        h.postAtTime(() -> {
            if (stopped) return;
            failures++;
            if (failures < 3 && gatt != null) {
                try {
                    if (gatt.connect()) return;
                } catch (Exception ignored) {
                }
            }
            startScan();
        }, this, android.os.SystemClock.uptimeMillis() + ms);
    }

    private void startScan() {
        if (stopped || scanning) return;
        if (adapter == null || !adapter.isEnabled()) {
            status = "蓝牙没开";
            retry(10000);
            return;
        }
        scanner = adapter.getBluetoothLeScanner();
        if (scanner == null) {
            status = "蓝牙没准备好";
            retry(10000);
            return;
        }
        try {
            scanner.startScan(null, new ScanSettings.Builder().setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY).build(), cb);
            scanning = true;
            status = "找手表中…";
        } catch (Exception e) {
            status = "扫描失败：" + e.getMessage();
            retry(10000);
        }
    }

    private void stopScan() {
        if (!scanning) return;
        scanning = false;
        try {
            if (scanner != null) scanner.stopScan(cb);
        } catch (Exception ignored) {
        }
    }

    private final ScanCallback cb = new ScanCallback() {
        @Override
        public void onScanResult(int type, ScanResult r) {
            String want = Prefs.sp(ctx).getString("hr_name", "").trim().toLowerCase();
            String name = r.getScanRecord() != null ? r.getScanRecord().getDeviceName() : null;
            boolean hit = !want.isEmpty()
                    ? name != null && name.toLowerCase().contains(want)
                    : hasHr(r) || (name != null && name.toLowerCase().contains("watch"));
            if (!hit) return;
            final BluetoothDevice dev = r.getDevice();
            final String shown = name != null ? name : dev.getAddress();
            h.post(() -> {
                if (connecting || stopped) return;
                stopScan();
                status = "找到 " + shown + "，连接中";
                connect(dev);
            });
        }

        @Override
        public void onScanFailed(int code) {
            h.post(() -> {
                scanning = false;
                status = "扫描出错 " + code;
                retry(10000);
            });
        }
    };

    private boolean hasHr(ScanResult r) {
        if (r.getScanRecord() == null) return false;
        List<ParcelUuid> u = r.getScanRecord().getServiceUuids();
        if (u == null) return false;
        for (ParcelUuid x : u) if (SVC_HR.equals(x.getUuid())) return true;
        return false;
    }

    private void connect(BluetoothDevice dev) {
        connecting = true;
        try {
            gatt = Build.VERSION.SDK_INT >= 23
                    ? dev.connectGatt(ctx, false, gcb, BluetoothDevice.TRANSPORT_LE)
                    : dev.connectGatt(ctx, false, gcb);
        } catch (Exception e) {
            connecting = false;
            status = "连接出错：" + e.getMessage();
            retry(5000);
        }
    }

    private void closeGatt() {
        connecting = false;
        try {
            if (gatt != null) gatt.close();
        } catch (Exception ignored) {
        }
        gatt = null;
    }

    private final BluetoothGattCallback gcb = new BluetoothGattCallback() {
        @Override
        public void onConnectionStateChange(BluetoothGatt g, int st, int state) {
            h.post(() -> {
                if (state == BluetoothGatt.STATE_CONNECTED) {
                    failures = 0;
                    status = "已连接";
                    g.discoverServices();
                } else if (state == BluetoothGatt.STATE_DISCONNECTED) {
                    closeGatt();
                    status = "断开了，重连中";
                    retry(2000);
                }
            });
        }

        @Override
        public void onServicesDiscovered(BluetoothGatt g, int st) {
            h.post(() -> {
                BluetoothGattService s = g.getService(SVC_HR);
                BluetoothGattCharacteristic c = s == null ? null : s.getCharacteristic(CHR_HR);
                if (c == null) {
                    status = "这个设备没有心率广播（手表上要先打开「心率广播」）";
                    closeGatt();
                    retry(15000);
                    return;
                }
                g.setCharacteristicNotification(c, true);
                BluetoothGattDescriptor d = c.getDescriptor(CCCD);
                if (d != null) {
                    d.setValue(BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE);
                    g.writeDescriptor(d);
                }
                status = "已订阅心率";
            });
        }

        @Override
        public void onCharacteristicChanged(BluetoothGatt g, BluetoothGattCharacteristic c) {
            byte[] v = c.getValue();
            if (v == null || v.length < 2) return;
            int flags = v[0] & 0xFF;
            int bpm = (flags & 0x01) != 0 && v.length >= 3 ? (v[1] & 0xFF) | ((v[2] & 0xFF) << 8) : v[1] & 0xFF;
            if (bpm <= 0 || bpm > 250) return;
            lastBpm = bpm;
            lastBpmAt = System.currentTimeMillis();
            long now = System.currentTimeMillis();
            if (now - lastPush < 5000) return;
            lastPush = now;
            status = "接收中";
            sink.onBpm(bpm);
        }
    };
}
