#!/bin/sh
# 单元测试（gofmt 检查 + go mod tidy + go vet + go test），单线程。
# 退出码：任何一步失败都返回 1。输出只截一部分给人看，退出码按完整结果算。
# 别写成「命令 | tail」：那样退出码是 tail 的，测试失败也会返回 0。
cd "$(dirname "$0")/server" || exit 1
docker run --rm -v "$PWD":/src -w /src -v hl-gomod:/go/pkg/mod -v hl-gocache:/root/.cache/go-build \
  -e GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" -e CGO_ENABLED=0 -e GOMAXPROCS=1 -e GOFLAGS=-p=1 -e TZ=Asia/Shanghai golang:alpine sh -c '
  st=0
  bad="$(gofmt -l . 2>&1)"
  if [ -n "$bad" ]; then st=1; echo "✗ 这些文件没 gofmt（先跑 gofmt -w 再提交）："; echo "$bad"; fi
  go mod tidy >/tmp/tidy.log 2>&1 || { st=1; echo "✗ go mod tidy 失败"; }
  grep -vE "^go: (finding|downloading|found)" /tmp/tidy.log | tail -5
  go vet ./... >/tmp/vet.log 2>&1 || { st=1; echo "✗ go vet 失败"; }
  head -40 /tmp/vet.log
  if go test -count=1 . >/tmp/test.log 2>&1; then
    tail -40 /tmp/test.log
  else
    st=1
    echo "✗ go test 失败："
    grep -E "^(--- FAIL|FAIL|panic)|_test\.go:[0-9]+" /tmp/test.log | head -40
    echo "（完整输出的最后几行）"; tail -15 /tmp/test.log
  fi
  exit $st'
