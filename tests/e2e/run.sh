#!/bin/sh
# 在 Playwright 容器里跑 pages.mjs（单进程，NAS 内存小）。用法：tests/e2e/run.sh [网址] [截图目录]
# SPEC=admin.mjs 跑设置页测试（见 admin.mjs 开头）；默认 pages.mjs。
# 设了 EXPECT_RULES=<数> 时，本站样式规则总数对不上就算失败（拆 / 合并 CSS 时用）。
# 先用 ./run-test.sh 起 8091 测试实例。第一次跑会把 playwright-core 装进 docker 卷 hl-pw-npm。
H="$(cd "$(dirname "$0")/../.." && pwd)"
URL="${1:-http://127.0.0.1:8091}"
OUT="${2:-$H/tests/e2e/screenshots}"
mkdir -p "$OUT"
docker run --rm -v hl-pw-npm:/pw alpine:latest chown "$(id -u):$(id -g)" /pw   # 新建的卷属于 root
docker run --rm --network host --user "$(id -u):$(id -g)" -e HOME=/tmp -e NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmjs.org}" -e EXPECT_RULES="${EXPECT_RULES:-}" -e EXPECT_UI="${EXPECT_UI:-}" -e ADMIN_PW="${ADMIN_PW:-}" -e ADMIN_CODE="${ADMIN_CODE:-}" \
  -v hl-pw-npm:/pw -v "$H/tests/e2e:/e2e:ro" -v "$OUT:/out" \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c '
  [ -d /pw/node_modules/playwright-core ] || npm i --prefix /pw --no-audit --no-fund --registry="$NPM_REGISTRY" playwright-core@1.49.1 >/dev/null 2>&1
  cp /e2e/"$1" /pw/"$1" && cd /pw && node "$1" "$0" /out' "$URL" "${SPEC:-pages.mjs}"
