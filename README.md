# Attune

> **The open-source signal layer between your users and your team.**
>
> Apache-2.0 · Go 1.25 · self-hosted · OpenAI-compatible

Most products bury their best feedback under noise — bug reports stuck in support email, feature requests scattered across Slack DMs, sentiment lost in app store reviews. The gap between *what users say* and *what your team hears* is a lossy channel.

**Attune makes that channel lossless.** It ingests user signal from anywhere, classifies it with whatever LLM you trust, clusters duplicates so 100 reports of the same bug become one row, drafts a reply so your team closes the loop in a minute instead of an hour, and routes the high-signal ones into where your team actually works.

Listening is passive. Attunement is active alignment.

## What it does

```
[user signals]
     │
     ▼
HTTP / Lark / email   ·   API-key auth · rate-limited · deduped
     │
     ▼
┌──────────────────────────────────────────────────────┐
│ enricher                                             │
│   ① classify    kind · severity · modules            │
│                 sentiment · language                 │
│   ② cluster     embedding similarity grouping        │
│   ③ draft reply LLM-suggested response in your tone  │
└──────────────────────────────────────────────────────┘
     │
     ▼
PostgreSQL   ·   single source of truth · your data, your control
     │
     ▼
┌──────────────────────────────────────────────────────┐
│ fan-out                                              │
│   · inline:   Lark group bot                         │
│   · outbox:   customer HTTPS webhooks (at-least-once)│
│   · daily:    LLM-summarized theme digest            │
└──────────────────────────────────────────────────────┘
     │
     ▼
[your team acts · console UI for triage + reply + workflow]
```

## Quickstart

Self-host with the docker-compose kit (attune + Postgres):

```bash
cd deploy
cp .env.example .env        # set POSTGRES_PASSWORD + FEEDBACK_API_LLM_OPENAI_API_KEY
docker compose up -d
curl http://localhost:8090/healthz                                     # -> ok
docker compose run --rm attune tenant create --slug <slug> --name <name>
docker compose run --rm attune keys issue --tenant <slug> --label <s>  # mint an API key
```

See [`deploy/README.md`](deploy/README.md) for the full walk-through. Or build from source:

```bash
go build ./cmd/attune
go run ./cmd/attune server                                  # start HTTP server
```

Every field in [`config.example.yaml`](config.example.yaml) has an env-var override — see [`internal/infra/config/env.go`](internal/infra/config/env.go) for the full table.

| Required | Env var | Notes |
|---|---|---|
| `database_url` | `FEEDBACK_API_DATABASE_URL` | PostgreSQL DSN, e.g. `postgres://<user>:<password>@<host>:5432/attune` |
| `llm_openai_base_url` | `FEEDBACK_API_LLM_OPENAI_BASE_URL` | Any OpenAI-compatible endpoint |
| `llm_openai_api_key` | `FEEDBACK_API_LLM_OPENAI_API_KEY` | Bearer token (blank OK for local ollama) |

## Architecture

| Layer | Tech | Notes |
|---|---|---|
| HTTP server | Go 1.25, chi router, structured slog | Single static binary |
| Storage | PostgreSQL 14+ | pgvector for clustering (v0.5+) |
| LLM enrichment | Any OpenAI-compatible `/v1/chat/completions` | Default OpenAI; Anthropic + Gemini in v0.4 |
| Outbound | Lark group bot · customer HTTPS webhooks | Slack / Discord / email in v0.6 |
| Console | React + Vite + biome (`console/`) | Triage UI, served as static files |
| Observability | OpenTelemetry + Prometheus `/metrics` | Grafana dashboards in `observability/dashboards/` |

### Package layout

```
cmd/attune/                  Bootstrap: DI + signals + CLI subcommands
internal/
  domain/                    Pure types: IngestInput / Snapshot / Enriched
  repo/                      Data access — all SQL lives here
    feedback/  apikey/  outbox/  notifytarget/  tenant/  lark/
  service/                   Business logic + orchestration
    enrich/  ingest/  outbox/  apikey/  eval/
  handlers/                  HTTP routes + parameter parsing
    console/                 Stage B console API
  notify/                    Outbound webhooks + Transport framework
    adapter/                 Per-destination senders
      rawwebhook/  larkwebhook/  githubissue/
  infra/
    apikey/                  HTTP middleware + context keys
    config/                  YAML + env override
    database/                Schema migrations
    llmclient/               OpenAI-compatible HTTP client
    lark/                    Inbound Lark protocol
    observability/           Vendored OTel + slog helpers
console/                     React triage UI (feature-based: src/features/*)
```

**Layering rule** — handlers never write SQL; service never writes HTTP; notify never imports service; infra never imports service or repo. A reverse import is a rejection-grade lint.

## Roadmap

We ship monthly. Six milestones to v1.0 (full plan: [GitHub milestones](https://github.com/Phixsura/attune/milestones)):

| Release | Due | Theme |
|---|---|---|
| [v0.2](https://github.com/Phixsura/attune/milestone/1) | 2026-07-04 | **Trust** — CI, hooks, lint, IDL, module rename |
| [v0.3](https://github.com/Phixsura/attune/milestone/2) | 2026-08-04 | **Deployable** — docker-compose, observability overlay, PII redaction |
| [v0.4](https://github.com/Phixsura/attune/milestone/3) | 2026-09-04 | **AI Depth** — sentiment, multi-language, multi-LLM backend, confidence + cost |
| [v0.5](https://github.com/Phixsura/attune/milestone/4) | 2026-10-04 | **Operator Power** — clustering, daily digest, reply draft, batch ops |
| [v0.6](https://github.com/Phixsura/attune/milestone/5) | 2026-11-04 | **Multi-channel** — Slack, Discord, email ingest, Adapter SDK, Go + Node SDK |
| [v1.0](https://github.com/Phixsura/attune/milestone/6) | 2026-12-04 | **Enterprise-ready** — RBAC, audit log, SSO, Helm chart, GDPR |

### Pillars

Every issue carries a `pillar/*` label. Pick one if you want to contribute:

| Pillar | Focus | Browse |
|---|---|---|
| 🧠 **AI Intelligence** | classification quality, new dimensions, generative assists | [`pillar/ai`](https://github.com/Phixsura/attune/labels/pillar%2Fai) |
| ⚡ **Operator Efficiency** | dedup, digest, batch ops, workflow | [`pillar/ops`](https://github.com/Phixsura/attune/labels/pillar%2Fops) |
| 🔌 **Platform & Integrations** | IDL, SDKs, adapters, extensibility | [`pillar/platform`](https://github.com/Phixsura/attune/labels/pillar%2Fplatform) |
| 🏢 **Enterprise-ready** | deploy, observability, compliance, multi-tenant | [`pillar/enterprise`](https://github.com/Phixsura/attune/labels/pillar%2Fenterprise) |

## Quality gates

```bash
./scripts/check.sh    # build + vet + test + lizard + jscpd
```

| Check | Threshold |
|---|---|
| `go vet ./...` | 0 warnings |
| `go build ./...` | 0 errors |
| `go test -short ./...` | all pass on changed code |
| `lizard . -l go -C 15 -T nloc=100` | CCN ≤ 15, NLOC ≤ 100 per function |
| `npx -y jscpd . --pattern '**/*.go' --threshold 5` | duplication < 2 % |

Full engineering contract: [CLAUDE.md](CLAUDE.md). Every code-changing PR must add a `[Unreleased]` entry in [CHANGELOG.md](CHANGELOG.md).

## Local development

An optional, dependency-free pre-commit hook gives fast local feedback (~seconds)
on the quick §1 checks before you commit, complementing CI. **Enable it once per
clone:**

```bash
git config core.hooksPath .husky
```

On `git commit` it runs, on your **staged** changes only:

| Check | Blocks? | Notes |
|---|---|---|
| Large-file guard (> 500 KB) | yes | catches stray binaries / large blobs |
| `go vet` on touched packages | yes | |
| `scripts/lint-slog.sh` (slog / OTel) | no | warn-only |
| `biome` on staged `console/src` | yes¹ | ¹only if `console/node_modules` is installed |

The console step is best-effort — **Go-only contributors never need Node.** To get
local biome on console changes, install its deps once: `pnpm -C console install`.

**Bypass** (emergencies only): `git -c core.hooksPath=/dev/null commit …`

CI stays the authoritative gate; the hook is a fast subset of it, never stricter.
The hook file must remain executable (`+x`) — git silently skips a non-executable
hook (it's committed with the bit set, so fresh clones are fine).

## Contributing

PRs welcome. Before you start:

- Read [CLAUDE.md](CLAUDE.md) — quality gates, layering rules, changelog discipline, security baseline
- Pick an issue with [`status/ready`](https://github.com/Phixsura/attune/labels/status%2Fready) (or any [`good first issue`](https://github.com/Phixsura/attune/labels/good%20first%20issue) once they're tagged)
- Follow [Conventional Commits](https://www.conventionalcommits.org/) — `type(scope): subject`
- The PR template asks for a changelog entry; CI refuses PRs without one (unless purely `docs` / `chore` / `ci`)

## License

Apache-2.0 — see [LICENSE](LICENSE).

---

*Why "Attune"? Because the gap between what users say and what your team hears is a lossy channel. Listening is passive — attunement is active alignment.*
