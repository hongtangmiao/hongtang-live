# 设备接口（第 3 阶段）

托盘程序（Windows / Linux）和安卓 App 都用这一套。所有请求和返回都是 JSON（UTF-8）。

## 1. 配对

在设置页「设备 → 添加设备」生成配对码（10 分钟有效，只能用一次），同时给出：

- 二维码 / 配对链接：`hlpair://pair?s=<服务器地址，URL 编码>&c=<配对码>`
- 或者分别填：服务器地址 + 配对码（`ABCD-EFGH`，大小写、空格、横杠都不讲究）

```
POST /api/v1/pair
{"code": "ABCD-EFGH", "name": "书房电脑", "kind": "windows", "host": "studio", "app_version": "hl-tray/0.1"}

200 {"ok": true, "token": "hld_…", "device": {"id", "name", "kind", "host", "scopes"}, "server": {…}}
403 配对码不对或已过期      429 同一 IP 10 分钟错了 10 次
```

- `kind`：`windows` / `linux` / `android` / `other`。管理员生成配对码时指定过名字、类型的，以管理员的为准。
- `host`：daylog 时间线里区分设备用，只保留 `A-Z a-z 0-9 . _ -`；和别的设备撞了会自动加后缀。
- **`token` 只返回这一次**，设备自己存好（Windows 用 DPAPI，安卓用 EncryptedSharedPreferences）。服务器只存它的 SHA-256。

之后所有请求都带 `Authorization: Bearer hld_…`。返回 401 = 口令失效（被停用 / 删除），提示用户重新配对；429 = 错太多次，等 10 分钟。

## 2. 我是谁

```
GET /api/v1/device
200 {"ok": true, "device": {…}, "server": {"name", "report_sec": 30, "aw_daily": true, "writer": true, "timezone": "+08:00"}}
```

`report_sec` 是服务器希望的上报间隔。`writer: false` 表示服务器还没接管 daylog，活动会返回 503，但不用当成错误。

## 3. 上报（一次报多样）

```
POST /api/v1/report
{
  "ts": "2026-11-22T20:30:00+08:00",          // 可选；格式不对 / 比现在晚一天以上会被换成服务器时间
  "activity": {"app": "Code.exe", "title": "…", "url": "…"},   // 或 {"idle": true}（锁屏 / 离开）
  "music":    {"playing": true, "title": "…", "artist": "…", "album": "…"},
              // 没在放：{"playing": false, "reason": "paused"}；读不到：{"reason": "unknown"}（保留上一次）
  "health":   {"values": {"heart_rate_live": 72}, "ts": "…"},
  "battery":  {"level": 85, "charging": true},
  "stats":    { … 系统资源快照，格式同旧的 /api/sys/report … }
}

200 {"ok": true, "results": {"activity": {"ok": true, "label": "在用 VS Code", "cat": "写代码"},
                             "music": {"ok": true}, "battery": {"ok": true}, …}, "server": {…}}
```

- 每一项各自成功或失败，失败的是 `{"ok": false, "error": "…", "status": 403}`，其他项照常写入。
- 只报 `music` 不报 `activity`：只更新「正在听」，不算一段应用使用。
- 窗口标题、网址在服务器上按隐私规则处理，设备端不用自己过滤（但别报密码框之类的内容）。
- 每一项都要权限（设置页可改）。默认：
  - 电脑：`activity` `music` `stats` `aw`
  - 安卓：`activity` `music` `health` `battery`

## 4. ActivityWatch 每日导出（电脑）

```
POST /api/v1/aw           Content-Encoding: gzip（建议）
{"date": "2026-11-21", "hostname": "…", "buckets": {"window": […], "afk": […], "web": […], "input": […]}, …}

200 {"ok": true, "day": "2026-11-21", "report_exists": false}
```

格式同原来的 `windows/export_day.ps1`。服务器每天定时把前一天的交给 DeepSeek 写日报；当天的先存着，第二天再处理。同一天重复上传会覆盖，在日报生成之前都没问题。

## 5. 解除配对

```
POST /api/v1/unpair   →  200 {"ok": true}
```

## 旧接口

`/api/ingest/live`、`/api/ingest/health`、`/api/ingest/aw`（`X-Daylog-Token`）和 `/api/sys/report`（`x-pc-key`）照常可用。设置页「设备 → 旧口令」可以关掉，关掉后返回 410。
