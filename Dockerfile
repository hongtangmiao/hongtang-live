# hongtang-live：个人状态页 + 数字生活日报（多阶段构建，支持 linux/amd64 与 linux/arm64）
# 直接构建：docker build -t hongtang-live:latest .
# 低内存 NAS 本地单线程构建仍可使用：./deploy/build-image.sh

FROM --platform=$BUILDPLATFORM golang:alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/hongtang-live .

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/hongtang-live /app/hongtang-live
COPY web /app/web

ENV DATA_DIR=/data WEB_DIR=/app/web PORT=8787 HOST=0.0.0.0
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8787/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/hongtang-live"]
