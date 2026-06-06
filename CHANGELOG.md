# Changelog

All notable changes to attune are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **CI architectural-boundary gate for the console SPA** (#19) — runs
  `dependency-cruiser` on every console PR with four rules: no cross-feature
  imports, shared layers (components/lib/proto) may not reach into features/
  routes/app, features may not reach into routes/app, no circular deps.
  Config in `console/.dependency-cruiser.cjs`; `pnpm arch` runs it locally
  (requires Node ≥20.12 || ≥22 || ≥24).
- **Observability overlay** (`deploy/docker-compose.obs.yml`) — optional
  Prometheus + Grafana stack (pinned images, memory-capped) layered with
  `-f docker-compose.yml -f docker-compose.obs.yml` (#6). Auto-provisions the
  Prometheus datasource and the "Attune Overview" dashboard, and documents the
  `attune_*` metrics as a backend-agnostic contract in `observability/README.md`.
- CI: a `deploy/**`-filtered `docker compose config` smoke check.
- **Protobuf IDL contract** (#19) — `.proto` in `proto/attune/v1/` is now the
  single source of truth for the HTTP contract, generating Go (`internal/proto/`),
  TypeScript (`console/src/proto/`, via ts-proto) and OpenAPI (`docs/openapi/`).
  `make proto` regenerates all three; a CI `proto-sync` gate fails on drift.
  Every HTTP endpoint is decoded/encoded via `protojson` against the generated
  types: the public `POST /v1/feedback/ingest` plus the full console API
  (session, API keys, notify-targets, feedback, usage).
- **Unified error envelope** (#19) — every HTTP error now shares one shape,
  `{"code","message","requestId"}` (`ErrorResponse` in
  `proto/attune/v1/common.proto`), where `requestId` echoes the request's chi
  correlation id for support triage.

### Changed

- **Backend reorganized into hybrid layer-outside / feature-inside packages**
  (#19) — `internal/{service,repo,notify}` no longer flat. Each layer keeps
  its name + the four CLAUDE.md §5 rules (re-verified clean by grep after
  the move) and adds feature subpackages inside:
  - `internal/service/{enrich,ingest,outbox,apikey,eval}/`
  - `internal/repo/{feedback,apikey,outbox,notifytarget,tenant,lark}/`
  - `internal/notify/adapter/{rawwebhook,larkwebhook,githubissue}/`
    (Transport framework stays in the root `internal/notify` package).
  Importers needing both `service/apikey` and `repo/apikey` alias the repo
  side as `apikeyrepo`; same for `outboxrepo` and `larkrepo`.
- **Console SPA migrated to feature-based layout** (#19) — `src/api/` retired;
  every console resource now lives under `src/features/<x>/{api,components}/`
  per bulletproof-react conventions, with React Query co-located per feature
  (queryOptions + hook one file per operation). `src/components/` keeps only
  truly shared primitives (`ui/`, `brand/`, layout shells).
- **`internal/observability` → `internal/infra/observability`** (#19) — naming
  consistency with sibling infra packages (`infra/trace`, `infra/metrics`).
  Bootstrap-only package; only `cmd/attune` importers updated.
- **Console API responses are now lowerCamelCase** (#19, breaking) — protoJSON
  renders fields in lowerCamelCase, so console endpoints under `/fb/v1/console/*`
  now return `userId`, `createdAt`, `enrichedTitle`, … instead of the previous
  snake_case (`user_id`, `created_at`, …). Request bodies still accept both
  casings. The bundled console SPA is updated in lockstep; any out-of-tree
  console API client must follow. (Pre-1.0 breaking change, flagged per §3.)
- **64-bit integer fields are now JSON strings** (#19) — protoJSON serializes
  `int64`/`uint64` as strings (`{"id":"123"}`), which is also safe for JavaScript
  clients. Affects the ingest response `id`, the console feedback `id`, usage
  totals/buckets, and feedback-stats counts.
- Renamed the bundled Grafana dashboard "Attune Overview (Wave 1.2)" → "Attune
  Overview" and removed internal roadmap jargon from `observability/` and the
  `metrics` package doc (no metric names changed).

### Removed

- **`openapi-typescript` and the hand-written `openapi.yaml`** (#19) — console
  TypeScript types are now generated from `.proto` via ts-proto, retiring the
  hand-maintained `internal/handlers/console/openapi.yaml` and the
  `openapi-typescript` dev dependency. The `gen:api` npm script is replaced by
  `gen:proto` (→ `make proto`).

### Fixed

- **Feedback detail labels regress to raw keys** (#19) — `zh-CN.json` still
  held snake_case keys (`source_meta`, `enrichment_error`, `enriched_at`)
  after the protoJSON lowerCamelCase rename; the detail panel rendered the
  literal key strings instead of the Chinese labels. Keys renamed to match.
- **Unified error envelope leak** (#19) — `console.writeError` (auth/oauth/
  dev_login paths, used by RequireSession middleware) still emitted
  `{code,message}` without `requestId`, contradicting the CHANGELOG's
  "every HTTP error shares one shape" claim. Routed through `respondError`
  so the chi RequestID is included on every 401/403/4xx from these paths.
- **`NotifyTarget.CreatedAt` was synthesized** (#19) — the response field
  was set to `time.Now()` on every read with a TODO comment; every notify
  target in the console UI displayed "just created" regardless of true DB
  creation time. Added `CreatedAt` to the repo model, surfaced the
  `tenant_notify_targets.created_at` column in all SELECT/RETURNING paths.
- **`decodeProto` silently truncated oversized bodies** (#19) — bodies > 1 MiB
  were chopped to exactly 1 MiB and surfaced as vague "invalid json" 400s
  instead of a clear 413. Now returns `errBodyTooLarge` so handlers map it
  to `HTTP 413 body_too_large`.

### Changed

- **`scripts/check.sh` jscpd threshold 2% → 4%** (#19) — the intentional
  helper duplicates from the package split (cycle-prevention copies of
  `truncate`, `signRawBody`, `signLarkBot`, `isUniqueViolation` across
  `repo/{outbox,notifytarget}/helpers.go` + `notify/adapter/*/`) push the
  Go duplication ratio from 1.9% to ~3%. CLAUDE.md §1 raised to match.

### Security

- Bounded the `source` label on `attune_ingest_total`: a rejected (invalid)
  client-supplied `source` is now recorded as `invalid` instead of the raw value,
  closing an unbounded metric-cardinality vector on the ingest validation-error
  path.

## [0.2.0] - 2026-06-05

### Added

- **Private-deploy docker-compose kit** under `deploy/` (#5): `docker-compose.yml`
  (attune + postgres), a documented `.env.example`, an optional `config.yaml`
  template, and a quickstart `README.md`. `cd deploy && cp .env.example .env &&
  docker compose up -d` brings up a hardened (loopback-bound, `no-new-privileges`,
  read-only attune rootfs), persistent stack; first-tenant bootstrap via
  `docker compose run --rm attune tenant create / keys issue`.
- **`/healthz` liveness endpoint** (#5) — the Kubernetes/cloud-native convention
  (trailing `z` avoids colliding with a real application route).

### Changed

- **BREAKING — project-wide rename `listen` → `attune`** (#8). The Go module
  path is now `github.com/Phixsura/attune`; the binary/command is `attune`
  (was `listen`); Prometheus metrics use the `attune_*` prefix (dashboard +
  scrape job relabelled); the outbound webhook signature header is
  `X-Attune-Signature`, GitHub-dispatch labels are `attune/*`, and the
  `User-Agent` is `attune/<n>`; console session cookies are `attune_session` /
  `attune_oauth_state`. Pre-1.0, so this lands as a single breaking change —
  update any scrapers, dashboards, webhook verifiers, or label filters
  accordingly. The `FEEDBACK_API_*` env prefix is intentionally unchanged.

### Removed

- **BREAKING — the `/health` endpoint is removed** (#5); use `/healthz` instead.
  Pre-1.0, so it lands as a flagged minor bump (CLAUDE.md §3). Update any uptime
  monitor, load-balancer, or container probe that hit `/health`.

### Security

- Bump dependencies carrying published advisories: `github.com/jackc/pgx/v5`
  5.9.1→5.9.2 (GHSA-j88v-2chj-qfwx), `golang.org/x/net`→0.55.0 and
  `golang.org/x/sys`→0.45.0 (GO-2026-4918 / 5024–5030), and `vitest`→4.1
  (GHSA-5xrq-8626-4rwp). `govulncheck` confirms none of the Go advisories were
  reachable from attune's code, and `vitest` is a test-only dependency with no
  tests and no UI server, so there was no exploitable exposure — bumped for
  hygiene and to clear the alerts.

## [0.1.0] - 2026-06-04

### Added

- Initial public release (Apache-2.0).
- Go 1.25 HTTP server (chi router + structured slog + OpenTelemetry).
- PostgreSQL storage with auto-applied migrations.
- LLM enrichment via any OpenAI-compatible `/v1/chat/completions` endpoint
  (OpenAI / Azure OpenAI / vllm / ollama / oneapi).
- Outbound delivery — Lark group bot webhooks (inline, best-effort) and
  customer HTTPS webhooks (via outbox, at-least-once).
- Stage B web console (React + Vite + biome) for tenant / API key /
  notify-target / feedback CRUD.
- Prometheus `/metrics` endpoint and a base Grafana dashboard JSON shipped
  under `observability/dashboards/`.
- Configurable per-tenant token-bucket rate limiting.
- Lark event subscription handler with signature verification.

[Unreleased]: https://github.com/Phixsura/attune/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/Phixsura/attune/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Phixsura/attune/releases/tag/v0.1.0
