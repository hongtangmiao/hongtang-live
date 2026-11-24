#!/bin/sh
# 编译服务器 + 打镜像 hongtang-live:<日期-提交>，同时打上 hongtang-live:1
set -e
H="$(cd "$(dirname "$0")/.." && pwd)"
cd "$H"
# 编译失败就停下：不打镜像，hongtang-live:1 不动（别写成「./build.sh | tail」，那样失败也会往下走）
if ! out="$(./build.sh 2>&1)"; then
  printf "%s\n" "$out" | tail -30
  echo "IMAGE_FAIL：编译没通过，没有打镜像"
  exit 1
fi
printf "%s\n" "$out" | tail -1
TAG="$(date +%Y%m%d)-$(git rev-parse --short HEAD)"
C=$(mktemp -d /tmp/hl-img.XXXXXX)
cp bin/hongtang-live "$C/" && cp -r web "$C/web" && cp deploy/Dockerfile "$C/"
docker build -q --pull=false -t hongtang-live:"$TAG" -t hongtang-live:1 "$C" >/dev/null
rm -rf "$C"
echo "IMAGE_OK hongtang-live:$TAG"
