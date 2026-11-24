# hongtang-live

自托管的「个人状态页 + 数字生活日报」，单 Go 容器运行。

- `server/`：Go 服务端（网站、接口、设置页、日报流水线、定时任务），全部在 `package main`
- `web/`：首页前端，原生 JS / CSS，没有打包工具
- `agent/`：电脑上的托盘程序（Go，Windows / Linux）；`android/`：安卓 App（Java）
- `deploy/`：镜像、compose；`tests/e2e/`：浏览器测试；`scripts/hooks/`：提交前钩子

部署细节看 `docs/deployment.md`，设备上报接口看 `docs/device-api.md`。

## 常用命令

```
./test.sh                                  # gofmt + vet + 单元测试（server/），失败返回 1
./build.sh                                 # 编译到 bin/hongtang-live
./deploy/build-image.sh && (cd deploy && docker compose up -d)   # 上线
agent/build.sh                             # 托盘程序：测试 + 编译到 agent/dist/
./run-test.sh                              # 用工作区代码在 8091 起测试实例（数据用 /tmp/hl-test/data 副本）
tests/e2e/run.sh [网址]                     # 首页浏览器测试，默认测 8091
SPEC=admin.mjs ADMIN_PW=… ADMIN_CODE=… tests/e2e/run.sh   # 设置页浏览器测试
```

回滚：`docker tag hongtang-live:<上一个日期-提交> hongtang-live:1 && cd deploy && docker compose up -d`（`docker images hongtang-live` 能看到旧 tag）。

## 这个项目特有的坑

- **编译测试都在 docker `golang:alpine` 里跑**，单线程（`GOMAXPROCS=1`，低内存设备友好）。国内网络可按需传 `GOPROXY=https://goproxy.cn,direct`。
- **`test.sh` 要 `TZ=Asia/Shanghai`**，脚本里已经带上；自己写 docker 命令跑测试时别漏。
- **`data/` 里有密钥**（`config.json` 权限 600），不进 git。测试实例只用副本，用完删掉副本和测试容器。线上配置以 `data/config.json` 为准，`.env` 只在第一次启动时读。
- **测试实例用 8091**（`./run-test.sh`），不要直接改生产容器的数据。
- **网页静态文件有白名单**：`server/static.go` 的 `staticOK`。往 `web/` 加新文件，不改它就是 404（`static_test.go` 守着）。
- **设置页嵌在程序里**（`server/adminui/`，`go:embed`）。改了要重新 `./build.sh` 才生效，`/admin/js/` 也有白名单（`adminpage_test.go`）。
- **`web/js/*.js`、`server/adminui/js/*.js` 是按顺序加载的普通脚本**，顶层名字全是全局的：两个文件声明同名的 `const`/`function` 会直接报错；加载时不能调用后面文件里才定义的函数。
- **时区只有一个来源**：设置页的「时区」（`config.timezone`，代码里的全局 `loc`）。不要再加第二个。
- **浏览器名单有两份**：`server/dlconfig.go` 的 `privacy.browser_apps` 和 `agent/agent.go` 的 `browserApps`（两个程序共用不了代码），改一边要改另一边。
- **日报定时任务**只在 `daylog_writer=true` 时跑，每天 00:10 生成并推送 Telegram，别在测试里真推。
- **每个 clone 都要启用一次钩子**：`git config core.hooksPath scripts/hooks`。

## 写代码之前

- 写新函数、新规则之前，先在项目里搜有没有现成的。
- 改网页样式之前先读 `DESIGN.md`：配色、字号档位、哪些元素不能删，都写在那。
- 提交被钩子拒了，照报错按功能把文件拆小再提交；不要绕过钩子，也不要往 `size-baseline.txt` 里加新文件。
- 拆文件或搬代码时只切行、不顺手改逻辑，改逻辑另做一次提交。

## 交差之前

- 跑 `./test.sh`，看退出码。
- 改了网页或设置页：`./run-test.sh` 起测试实例，跑对应的浏览器测试，JS 报错必须是 0。
- 拆 / 合 CSS 时带上 `EXPECT_RULES=<原来的规则数>`，拆 / 合设置页脚本时带上 `EXPECT_UI=<原来的控件数>`：
  少加载一个文件时页面不一定报错，截图也不一定看得出来，靠这两个数拦。
- 上线后对线上（`http://127.0.0.1:8090`）再跑一遍浏览器测试，看 `docker logs hongtang-live` 有没有报错。

## 规则表

| 规则 | 由什么拦住 |
|---|---|
| 提交的文本文件不超过 20 KB（约 5000 token） | `scripts/hooks/pre-commit` |
| Go 代码 gofmt、vet、单元测试都过 | `./test.sh`（失败返回 1） |
| 编译失败不打镜像、不换掉旧程序 | `build.sh` / `deploy/build-image.sh` / `agent/build.sh`（失败返回 1） |
| 脚本的退出码按完整结果算，别写成「命令 \| tail」 | 无，靠交差前故意弄坏一次看退出码 |
| 网页静态文件、设置页脚本的放行范围 | `server/static_test.go`、`server/adminpage_test.go` |
| 网页没有 JS 报错，样式和字体都加载到 | `tests/e2e/run.sh`（`pages.mjs`） |
| 拆 / 合 CSS 后样式规则一条不少 | `EXPECT_RULES` |
| 设置页各段都在、控件一个不少，没有 CSP 拦截 | `SPEC=admin.mjs` + `EXPECT_UI` |
| 两份浏览器名单一致 | 无，靠两边代码里的注释 |
| 网页文字不小于 12px（图形尺寸、标题除外） | `tests/e2e/run.sh` 的「字号」一步 |
