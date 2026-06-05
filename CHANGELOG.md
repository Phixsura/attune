# Changelog

All notable changes to attune are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/Phixsura/attune/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Phixsura/attune/releases/tag/v0.1.0
