# syntax=docker/dockerfile:1
#
# Standalone, multi-stage, multi-arch build for the attune service.
# Context = repo root (the Go module itself). buildx cross-compiles to the
# target arch on the native builder, so linux/amd64 + linux/arm64 build
# without qemu emulation.

# ── Stage 1: build (cross-compiled, static) ──
FROM --platform=$BUILDPLATFORM golang:1.25-alpine@sha256:c05ba4b73604069d376c4f41346b05374335b5ca0c46fb6dfede5a59f5196931 AS builder

# CI uses the default module proxy; local CN builds can override, e.g.
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct .
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /attune ./cmd/attune

# ── Stage 2: runtime (distroless static, nonroot, no shell) ──
# ca-certificates + tzdata are baked in; runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:d093aa3e30dbadd3efe1310db061a14da60299baff8450a17fe0ccc514a16639

COPY --from=builder /attune /app/attune

WORKDIR /app
EXPOSE 8090

# Stamped by the release workflow (--build-arg APP_VERSION=<tag>) so the running
# container reports its real version via OTel/telemetry; "dev" for local/CI builds.
ARG APP_VERSION=dev
# FEEDBACK_API_CONFIG is overridable; the prod compose mounts a yaml here.
ENV FEEDBACK_API_CONFIG=/app/config.yaml \
    APP_VERSION=${APP_VERSION}

ENTRYPOINT ["/app/attune"]
CMD ["server"]
