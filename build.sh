#!/bin/sh
# 编译 Linux 版服务端到 bin/hongtang-live（单线程，减少 NAS 内存占用）
# 退出码：编译失败返回 1，而且不留旧的 bin/hongtang-live（免得把上次的二进制当成这次的打进镜像）。
# go vet 只提示不拦（拦在 test.sh 里）。别写成「命令 | head」：那样编译失败也返回 0。
cd "$(dirname "$0")" || exit 1
mkdir -p bin
docker run --rm -v "$PWD":/w -w /w/server -v hl-gomod:/go/pkg/mod -v hl-gocache:/root/.cache/go-build \
  -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" -e CGO_ENABLED=0 -e GOMAXPROCS=1 -e GOFLAGS=-p=1 golang:alpine sh -c '
  rm -f /w/bin/hongtang-live
  go mod tidy >/tmp/tidy.log 2>&1 || echo "✗ go mod tidy 失败"
  grep -vE "^go: (finding|downloading|found)" /tmp/tidy.log | tail -5
  go vet ./... >/tmp/vet.log 2>&1 || echo "⚠ go vet 有问题（不拦编译，test.sh 会拦）："
  head -60 /tmp/vet.log
  go build -trimpath -ldflags="-s -w" -o /w/bin/hongtang-live . >/tmp/build.log 2>&1; st=$?
  head -60 /tmp/build.log
  exit $st'
st=$?
if [ "$st" -eq 0 ] && [ -f bin/hongtang-live ]; then
  echo "BUILD_OK $(du -h bin/hongtang-live | cut -f1)"
else
  echo "BUILD_FAIL"
  exit 1
fi
