#!/bin/sh
# 托盘程序：测试 + 交叉编译（Windows exe、Linux amd64/arm64）。单线程，免得把 NAS 压垮。
#   agent/build.sh          → dist/hl-agent.exe、dist/hl-agent-linux-amd64、dist/hl-agent-linux-arm64
#   vet / 测试任何一步失败就停下、返回非 0，dist/ 里的程序不会被换掉。
set -e
cd "$(dirname "$0")"
VER="$(date +%Y.%m.%d)-$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
docker run --rm -v "$PWD":/src -w /src -v hl-gomod:/go/pkg/mod -v hl-gocache:/root/.cache/go-build \
  -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" -e CGO_ENABLED=0 -e GOMAXPROCS=2 -e GOFLAGS=-p=1 -e TZ=Asia/Shanghai -e VER="$VER" golang:alpine sh -ec '
  # 每步先把输出存进文件再截给人看：别写成「命令 | tail」，那样失败时退出码是 tail 的，set -e 拦不住
  go mod tidy >/tmp/o 2>&1 || { tail -5 /tmp/o; echo "✗ go mod tidy 失败"; exit 1; }
  grep -vE "^go: (finding|downloading|found)" /tmp/o | tail -5
  echo "== vet linux";   GOOS=linux go vet ./... >/tmp/o 2>&1 || { tail -20 /tmp/o; echo "✗ vet linux 失败"; exit 1; }; tail -20 /tmp/o
  echo "== vet windows"; GOOS=windows go vet ./... >/tmp/o 2>&1 || { tail -20 /tmp/o; echo "✗ vet windows 失败"; exit 1; }; tail -20 /tmp/o
  echo "== test linux";  go test -count=1 -v . >/tmp/o 2>&1 || { grep -E "^(--- |ok|FAIL|panic)|_test.go" /tmp/o | tail -40; echo "✗ test linux 失败"; exit 1; }
  grep -E "^(--- |ok|FAIL|panic)|_test.go" /tmp/o | tail -40
  echo "== test windows（只编译）"; GOOS=windows go test -c -o /dev/null . >/tmp/o 2>&1 || { tail -10 /tmp/o; echo "✗ test windows 编译失败"; exit 1; }
  mkdir -p dist
  LD="-s -w -X main.version=$VER"
  GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LD" -o dist/hl-agent-linux-amd64 .
  GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LD" -o dist/hl-agent-linux-arm64 .
  # exe 图标 + 清单（DPI 感知）；工具装不上就算了
  ./dist/hl-agent-linux-amd64 __icon /tmp/icon.png
  if go run github.com/tc-hib/go-winres@v0.3.3 simply --icon /tmp/icon.png --manifest gui --product-name "红糖似了吗？采集程序" \
      --file-description "hongtang-live agent" --file-version "1.0.0.0" --product-version "1.0.0.0" --out rsrc >/tmp/winres.log 2>&1; then
    echo "exe 图标 OK"
  else
    echo "exe 图标跳过："; tail -3 /tmp/winres.log
  fi
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LD -H windowsgui" -o dist/hl-agent.exe .
  rm -f rsrc_windows_*.syso
  chown -R 1000:1001 dist
  sha256sum dist/*
'
echo "BUILD_OK $VER"
