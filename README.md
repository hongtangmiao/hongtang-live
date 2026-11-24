# hongtang-live

自托管的 **个人状态页 + 数字生活日报** 一体化系统。单个 Go 容器整合了个人主页、实时状态面板（「此刻」）、多设备采集接入、AI 日报/周报流水线、影音展示与内嵌管理后台。

## 主要功能

- **个人状态页 & 像素风格前端（`web/`）**：原生 HTML / CSS / JS（无构建工具），像素 JRPG 窗口主题（`Fusion Pixel 12` 点阵字），支持中/日双语切换、首页概览、动态、音乐、番剧墙、设备统计与账户页。
- **「此刻」实时状态与健康数据**：展示当前活跃设备、正在使用的应用（经服务端隐私规则脱敏与分类）、正在听的音乐、设备电量以及 Intervals.icu 同步的健康指标。
- **数字生活日报 / 周报**：接收电脑端每日导出的 ActivityWatch 数据，按隐私规则清洗聚合后调用 DeepSeek 生成 Markdown 日报/周报并自动入库、推送至 Telegram。
- **影音与媒体集成**：支持网易云音乐（自建 API 代理同步）、Bangumi + Jellyfin 番剧墙。
- **多设备监控与配对（`/api/v1/*`）**：在后台生成一次性配对码 / 二维码（`hlpair://`），每设备独立 Bearer Token（服务端仅存 SHA-256 哈希）与细粒度权限（`activity` / `music` / `stats` / `health` / `battery` / `aw`）。
- **内嵌管理后台（`/admin`）**：所有站点资料、隐私规则、第三方 API 密钥、设备配对与权限管理均在网页完成，配置持久化到 `data/config.json`（权限 `600`）。

## 目录结构

| 目录 / 文件 | 说明 |
|---|---|
| `server/` | Go 服务端（API、静态资源、Daylog 隐私规则与入库、日报流水线、定时任务、内嵌 `adminui/`） |
| `web/` | 首页前端（原生 JS / CSS、字体、图标与像素风覆盖样式） |
| `agent/` | 电脑端托盘采集程序 `hl-agent`（Go，Windows / Linux：活动、音乐、系统资源、每日 AW 导出、本地设置页） |
| `android/` | 安卓端 App（Java，`xyz.hongtang.live`：前台应用、媒体、电量、心率、`hlpair://` 配对） |
| `deploy/` | `Dockerfile`、`compose.yml` 与镜像构建脚本 |
| `tests/` | Playwright 端到端测试（`tests/e2e/`）与后台接口测试（`tests/admintest.py`） |
| `docs/` | 部署说明（[docs/deployment.md](docs/deployment.md)）与设备接口文档（[docs/device-api.md](docs/device-api.md)） |
| `.env.example` | 可选的首次启动环境变量模板 |
| `DESIGN.md` | 界面视觉规范（配色、字号档位、组件约束） |
| `AGENTS.md` | 项目约定、常用命令与质量门禁说明 |

## 快速开始（Docker）

无需预先填写配置文件，首次启动后全部在 `/admin` 设置页完成配置（也可参考 `.env.example` 在仓库根目录放 `.env` 用于首次初始化）。

> **安全提醒**：公网部署务必通过 **HTTPS 反向代理**（如 Nginx / Caddy / Cloudflare Tunnel）暴露，切勿直接公网 HTTP 暴露，否则 `/admin` 登录密码与设备 Bearer Token 会以明文传输。

1. **拉取预构建镜像并启动**（支持 `linux/amd64` 与 `linux/arm64`，由 GitHub Actions 自动构建）：
   ```sh
   cd deploy && docker compose pull && docker compose up -d
   ```
   如需使用 Docker Hub 镜像或本地自行构建：
   ```sh
   # 直接使用根目录多阶段 Dockerfile 构建
   docker build -t hongtang-live:1 .
   # 或在低内存 NAS 上单线程测试并构建
   ./test.sh && ./deploy/build-image.sh
   # 使用本地镜像启动
   HONGTANG_IMAGE=hongtang-live:1 docker compose -f deploy/compose.yml up -d
   ```

2. **获取一次性设置码并初始化**：
   ```sh
   docker logs hongtang-live | grep 设置码
   ```
   浏览器打开 `http://<你的服务器地址>:8090/admin`，输入日志里的设置码并设置管理员密码，即可在页面内配置站点信息、网易云 / Bangumi / Jellyfin、DeepSeek / Telegram / Intervals.icu 等功能。

## 设备端采集

- **电脑端（Windows / Linux）**：运行 `agent/build.sh` 编译 `hl-agent`（产物在 `agent/dist/`），在 `/admin` 的「设备 → 添加设备」生成配对码后即可配对。
- **安卓端**：配置 `JAVA_HOME` / `ANDROID_HOME` 后使用 `android/build-apk.ps1` 构建 APK，通过配对链接或手动输入服务器地址与配对码完成绑定。

详细协议见 [docs/device-api.md](docs/device-api.md)，部署与运维细节见 [docs/deployment.md](docs/deployment.md)。

## 开发与测试

```sh
git config core.hooksPath scripts/hooks   # 启用提交前文件大小检查钩子
./test.sh                                 # gofmt + go vet + Go 单元测试
./build.sh                                # 编译服务端二进制到 bin/hongtang-live
./run-test.sh                             # 在 8091 端口启动并行测试实例
tests/e2e/run.sh http://127.0.0.1:8091    # 运行 Playwright 浏览器测试
```

## 开源协议与致谢

- 本项目代码采用 [MIT License](LICENSE) 开源。
- 界面像素字体使用 [Fusion Pixel Font](https://github.com/TakWolf/fusion-pixel-font)（SIL Open Font License 1.1，见 `web/fonts/OFL-fusion-pixel.txt`）。
