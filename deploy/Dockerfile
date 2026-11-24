# hongtang-live：个人状态页 + 数字生活日报
# 构建：deploy/build-image.sh（上下文里只放二进制和 web/，不会把 data/ 里的密钥打进镜像）
FROM alpine:latest
COPY hongtang-live /app/hongtang-live
COPY web /app/web
WORKDIR /app
ENV DATA_DIR=/data WEB_DIR=/app/web PORT=8787 HOST=0.0.0.0
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8787/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/hongtang-live"]
