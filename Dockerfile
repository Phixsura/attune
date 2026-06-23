# syntax=docker/dockerfile:1
#
# Standalone, multi-stage, multi-arch build for the attune service.
# Context = repo root (the Go module itself). buildx cross-compiles to the
# target arch on the native builder, so linux/amd64 + linux/arm64 build
# without qemu emulation.

# ── Stage 1: build Console static assets ──
FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:cb3143549582cc5f74f26f0992cdef4a422b22128cb517f94173a5f910fa4ee7 AS console-builder

WORKDIR /src/console
RUN corepack enable
COPY console/package.json console/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY console/ ./
RUN pnpm exec vite build

# ── Stage 2: build Go binary (cross-compiled, static) ──
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
ARG VERSION=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w -X 'github.com/Phixsura/attune/internal/infra/database.Version=${VERSION}'" \
    -o /attune ./cmd/attune

# ── Stage 3: runtime (distroless static, nonroot, no shell) ──
# ca-certificates + tzdata are baked in; runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:d093aa3e30dbadd3efe1310db061a14da60299baff8450a17fe0ccc514a16639

COPY --from=builder /attune /app/attune
COPY --from=console-builder /src/console/dist /app/console

WORKDIR /app
EXPOSE 8090

ENTRYPOINT ["/app/attune"]
CMD ["--config", "/app/config.yaml", "server"]
