# syntax=docker/dockerfile:1
#
# Standalone, multi-stage, multi-arch build for the listen service.
# Context = repo root (the Go module itself). buildx cross-compiles to the
# target arch on the native builder, so linux/amd64 + linux/arm64 build
# without qemu emulation.

# ── Stage 1: build (cross-compiled, static) ──
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

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
    go build -trimpath -ldflags="-s -w" -o /listen ./cmd/listen

# ── Stage 2: runtime (distroless static, nonroot, no shell) ──
# ca-certificates + tzdata are baked in; runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /listen /app/listen

WORKDIR /app
EXPOSE 8090
# Overridable; the prod compose mounts a yaml here.
ENV FEEDBACK_API_CONFIG=/app/config.yaml

ENTRYPOINT ["/app/listen"]
CMD ["server"]
