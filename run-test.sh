#!/bin/sh
# 并行测试实例：8091 端口，数据用 /tmp/hl-test/data 副本，不做网易云后台刷新
H="$(cd "$(dirname "$0")" && pwd)"
docker rm -f hl-test >/dev/null 2>&1
mkdir -p /tmp/hl-test/data /tmp/hl-test/daylog
ENV_OPT=""
[ -f "$H/.env" ] && ENV_OPT="--env-file $H/.env"
docker run -d --name hl-test -p 8091:8787 --add-host host.docker.internal:host-gateway \
  $ENV_OPT \
  -e DATA_DIR=/data -e WEB_DIR=/app/web -e DAYLOG_DB=/daylog/data/app.db -e DAYLOG_ROOT=/daylog \
  -e HOST_PROC=/host/proc -e HOST_ROOT=/host/root -e ROOT_DISK=/data -e TZ=Asia/Shanghai -e HL_NE_SYNC=0 \
  -v "$H/bin/hongtang-live:/app/hongtang-live:ro" -v "$H/web:/app/web:ro" \
  -v /tmp/hl-test/data:/data -v /tmp/hl-test/daylog:/daylog \
  -v /proc:/host/proc:ro -v /etc/hostname:/host/root/etc/hostname:ro -v /etc/os-release:/host/root/etc/os-release:ro \
  -w /app alpine:latest /app/hongtang-live
