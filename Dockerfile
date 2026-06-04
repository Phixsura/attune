# syntax=docker/dockerfile:1.6
#
# Multi-stage build for the listen service (formerly feedback-api).
# Build context is the monorepo root because listen/go.mod has
# `replace github.com/wanmuchengchuang/llmgateway => ../gateway`.

# ── Stage 1: Build ──
FROM golang:1.25-alpine AS builder

# Aliyun mirror — dl-cdn TLS 在 CN 网络偶发 EOF(commit 71c4c20 治本同款)。
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache git ca-certificates

WORKDIR /build

# Pre-download dependencies — gateway first because listen replaces it.
# (Observability is now vendored under listen/internal/observability, so
# no monorepo go_pkg/ COPY is needed any more — listen → 自包含路上的一步。)
COPY gateway/go.mod gateway/go.sum ./gateway/
RUN cd gateway && go mod download

COPY listen/go.mod listen/go.sum ./listen/
RUN cd listen && go mod download

# Sources.
COPY gateway/ ./gateway/
COPY listen/ ./listen/

RUN cd listen && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /listen \
    ./cmd/listen

# ── Stage 2: Runtime ──
FROM alpine:3.21

# Aliyun mirror + tzdata for Asia/Shanghai resolution inside container.
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache ca-certificates tzdata

COPY --from=builder /listen /app/listen

USER nobody
WORKDIR /app

EXPOSE 8090

# Config path can be overridden but the prod compose mounts a yaml here.
ENV FEEDBACK_API_CONFIG=/app/config.yaml

ENTRYPOINT ["/app/listen"]
CMD ["server"]
