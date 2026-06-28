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
HTTP webhook · email IMAP · API client   ·   HMAC / cookie auth · rate-limited · deduped
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
│   · outbox:   customer HTTPS webhooks (at-least-once)│
│   · digest:   LLM-summarized theme digest (#34)      │
└──────────────────────────────────────────────────────┘
     │
     ▼
[your team acts · console UI for triage + reply + workflow]
```

## Quickstart

Self-host with the docker-compose kit (attune + Postgres):

```bash
cd deploy
cp .env.example .env        # set POSTGRES_PASSWORD
docker compose run --rm attune secrets generate-keyset
# paste the keyset into config.yaml, then set database.url + console.*
docker compose up -d
curl http://localhost:8090/healthz                                     # -> ok
docker compose run --rm attune tenant create --slug <slug> --name <name>
docker compose run --rm attune keys issue --tenant <slug> --label <s>  # mint an API key
docker compose run --rm attune llm channels create \
  --name openai --protocol openai-compat --base-url https://api.openai.com \
  --api-key sk-...
docker compose run --rm attune llm channels test \
  --id <channel-id> --provider-model gpt-4o-mini
docker compose run --rm attune llm abilities upsert \
  --channel <channel-id> --logical-model enrich-default --provider-model gpt-4o-mini
docker compose run --rm attune llm routes upsert \
  --purpose enrich --logical-model enrich-default
```

See [`deploy/README.md`](deploy/README.md) for the compose-kit quick-reference,
the full [private deployment guide](docs/private-deploy.md) for a step-by-step
walk-through with monitoring, SSL, upgrades, and troubleshooting, or
[`docs/k8s-deploy.md`](docs/k8s-deploy.md) for the Helm/Kubernetes install path.
Or build from source:

```bash
go build ./cmd/attune
go run ./cmd/attune server                                  # start HTTP server
```

### Sending feedback

Ingest is `POST /v1/feedback/ingest` with an `X-API-Key` (`ingest:write` scope).
Use the official Node/TypeScript client, [`@phixsura/attune`](sdk/node/) (ESM +
CJS, zero deps, browser-safe), which handles retries and idempotency for you:

```ts
import { Client } from '@phixsura/attune'
const client = new Client({ baseURL: 'https://attune.example.com', apiKey })
const { id } = await client.ingest({ content: 'the export button is broken' })
```

…or the official Go client, [`github.com/Phixsura/attune/sdk/go`](sdk/go/)
(proto-generated types, same retry + idempotency contract):

```go
import attune "github.com/Phixsura/attune/sdk/go"

client, _ := attune.New("https://attune.example.com", apiKey)
res, _ := client.Ingest(ctx, attune.IngestInput{Content: "the export button is broken"})
```

…or any HTTP client:

```bash
curl -X POST https://attune.example.com/v1/feedback/ingest \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"content":"the export button is broken"}'
```

Repeated delivery is safe: pass an `Idempotency-Key` header (the SDK sends one
per call) and a replay returns the original id instead of a duplicate row. See
the [SDK README](sdk/node/README.md) for retries, errors, and browser use.

If you ingest directly from a browser on a different origin, enable first-party
CORS only for the publishable ingest surface with:

```yaml
ingest:
  cors_allowed_origins:
    - "https://app.example.com"
```

Only `/v1/feedback/ingest` is intended for browser cross-origin use. Keep
management APIs server-side.

Attune is config-first: process config is loaded from one private YAML file
(`--config ./config.yaml`) and env-var overrides are intentionally unsupported.
LLM provider channels and routes are runtime state managed in Postgres through
Console/API/CLI. The authenticated Console includes `/console/llm-config` for
channel, model-ability, route, and channel-test operations.

| Required config | Notes |
|---|---|
| `database.url` | PostgreSQL DSN, e.g. `postgres://<user>@<host>:5432/attune` |
| `secrets.tink_keyset` | Shared Tink AEAD keyset from `attune secrets generate-keyset`; all replicas need the same keyset. |
| `secrets.legacy_inbound_master_key` | Optional migration-only old inbound master key, hex/base64; remove after `attune secrets reencrypt --apply`. |
| `console.base_url` + `console.session_key` | Console origin and >=32-char session signing secret. |
| `console.bootstrap_admin` | First admin credentials, used only while `admins` is empty. |

Runtime secret material is encrypted with the shared Tink keyset. Rotate it in
distributed deployments with `attune secrets add-key`, `set-primary`,
`reencrypt --apply`, `retire-key --apply`, and `delete-key`; see
[`docs/private-deploy.md`](docs/private-deploy.md#rotating-the-tink-keyset).

> ⚠️ **Upgrading from a v0.2 install with Lark data?** v0.3 hard-deletes
> `user_feedback` rows where `source LIKE 'lark-%'`. Set
> `migrations.confirm_lark_delete: true` to opt in, or `pg_dump` and export
> those rows first. See [`docs/private-deploy.md`](docs/private-deploy.md)
> for the full preflight runbook.

## Architecture

| Layer | Tech | Notes |
|---|---|---|
| HTTP server | Go 1.25, chi router, structured slog | Single static binary |
| Storage | PostgreSQL 14+ | pgvector for clustering (v0.5+) |
| LLM enrichment | DB-managed OpenAI Chat / OpenAI Responses / Anthropic / Gemini channels | Multi-protocol with structured output + [guardrails](docs/guardrails.md) |
| Outbound | customer HTTPS webhooks · GitHub Issues | Slack / Discord / email in v0.6 (#34) |
| Console | React + Vite + biome (`console/`) | Triage UI, served as static files |
| Observability | OpenTelemetry + Prometheus `/metrics` | Grafana dashboards in `observability/dashboards/` |

### Package layout

```
cmd/attune/                  Bootstrap: DI + signals + CLI subcommands
internal/
  domain/                    Pure types: IngestInput / Snapshot / Enriched
  repo/                      Data access — all SQL lives here
    feedback/  apikey/  outbox/  notifytarget/  tenant/  admin/  inboundsource/
  service/                   Business logic + orchestration
    enrich/  ingest/  outbox/  apikey/  eval/
  handlers/                  HTTP routes + parameter parsing
    console/                 Console API (auth, feedback, settings, inbound source mgmt)
  inbound/                   #66 channel-agnostic ingest framework
    adapter/                 Per-channel inbound adapters
      webhook/  email/
    inboundtest/             Conformance suite + shared fakes
  notify/                    Outbound webhooks + Transport framework
    adapter/                 Per-destination senders
      rawwebhook/  githubissue/
  infra/
    apikey/                  HTTP middleware + context keys
    config/                  Config-first YAML loader
    database/                Schema migrations
    llmclient/               Multi-protocol LLM client (OpenAI / Anthropic / Gemini)
    observability/           Vendored OTel + slog helpers
console/                     React triage UI (feature-based: src/features/*)
sdk/node/                    Node/TypeScript ingest client (@phixsura/attune)
sdk/go/                      Go ingest client (github.com/Phixsura/attune/sdk/go)
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
| [v0.6](https://github.com/Phixsura/attune/milestone/5) | 2026-11-04 | **Multi-channel** — Slack, Discord, email ingest, Adapter SDK; Node SDK [`@phixsura/attune`](sdk/node/) (#37) and Go SDK [`sdk/go`](sdk/go/) (#36) shipped early |
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

Full engineering contract: [CLAUDE.md](CLAUDE.md). Every code-changing PR must add a `[Unreleased]` entry in [CHANGELOG.md](CHANGELOG.md). Testing tiers (unit / live LLM): [docs/testing.md](docs/testing.md).

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
