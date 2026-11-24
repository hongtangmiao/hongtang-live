# 部署说明

单容器 `hongtang-live` 同时提供个人状态页、管理后台（`/admin`）、设备上报接口（`/api/v1/*`）以及数字生活日报流水线。

| 项目 | 说明 |
|---|---|
| 镜像 | `hongtang-live:1`（`deploy/build-image.sh` 构建，同时打 `hongtang-live:<日期>-<提交>`） |
| 起停 | `cd deploy && docker compose up -d` / `down` |
| 端口 | 容器内监听 `8787`，默认映射宿主机 `8090` |
| 配置 | `data/config.json`（权限 `600`）：首次启动自动生成，之后全部在 `/admin` 修改 |
| 设备 | `data/devices.json`（权限 `600`，只存口令 SHA-256 哈希） |
| 日报数据 | 挂载到容器 `/daylog`（SQLite 库、inbox、raw、reports、logs） |
| 设置页 | `https://<你的域名>/admin`（首次启动从 `docker logs hongtang-live` 获取一次性设置码） |

> **安全提醒**：公网访问请务必经过 HTTPS 反向代理，切勿直接将 8090 端口以明文 HTTP 暴露到公网。

## 首次初始化流程

1. 构建镜像并启动：
   ```sh
   ./test.sh && ./deploy/build-image.sh && (cd deploy && docker compose up -d)
   ```
2. 查看一次性设置码：
   ```sh
   docker logs hongtang-live | grep 设置码
   ```
3. 打开 `/admin`，输入设置码并设置管理员密码，按需填写站点资料、网易云 API、Bangumi / Jellyfin、DeepSeek / Telegram / Intervals.icu 等配置。
4. 在「设备 → 添加设备」生成配对码，配合 `hl-agent`（电脑托盘程序）或安卓 App 完成配对。

## 每天的流程

- 电脑端 `hl-agent` 每 30 秒上报活动 / 正在听、每 4 秒上报系统资源，并定时上传前一天的 ActivityWatch 导出（`/api/v1/aw` → `inbox/`）。
- 启用 `daylog_writer` 后，每天 00:10 自动处理 inbox（聚合 → AI 日报 → 入库与 Telegram 推送），周一 00:40 生成周报。
- 配置 Intervals.icu 后，每 30 分钟同步一次健康数据。

## 更新与回滚

更新：
```sh
./test.sh && ./deploy/build-image.sh && (cd deploy && docker compose up -d)
```

回滚到上一个镜像版本：
```sh
docker images hongtang-live
docker tag hongtang-live:<上一个日期-提交> hongtang-live:1
cd deploy && docker compose up -d
```

桌面托盘程序：`agent/build.sh` → `agent/dist/hl-agent.exe`（Windows）、`hl-agent-linux-amd64` / `-arm64`。

## 重算旧日报统计

```sh
docker exec hongtang-live /app/hongtang-live recompute-reports           # 预览
docker exec hongtang-live /app/hongtang-live recompute-reports --apply   # 写入
```
