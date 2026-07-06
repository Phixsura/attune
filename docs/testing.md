# Testing

attune follows a test pyramid with extra gates for contracts, observability, and
release packaging. Most confidence should come from fast unit and integration
tests; browser and runtime tests are fewer, but they cover the production seams
that cheap tests cannot see.

| Tier | Command | Default? | Cost | What it covers |
|---|---|---|---|---|
| **L0 fast local** | `make fast-check` | local opt-in | $0 | Fast Go unit sweep plus Console typecheck and Vitest. |
| **L1 CI preflight** | `make ci-check` | PR / before push | $0 | Go race unit tests, lint, complexity, duplication, Console type/build/test/arch checks, and local secret scan when installed. |
| **L2 contract** | `make proto-lint`, `make proto-breaking`, `make proto` | CI on proto changes | network for Buf remote plugins | Protobuf/OpenAPI/SDK contract shape and generation consistency. |
| **L3 integration** | `make test-integration` | CI on Go changes; local opt-in | Docker only | Real pgvector PostgreSQL migrations, repos, service/repo transaction paths, restore drills, and queue/outbox smoke tests. |
| **L4 browser** | `cd console && pnpm test:e2e:a11y` | CI on Console changes | browser install | Critical Console routes in real Chromium with API mocks, accessibility, overflow, console-error, and interaction coverage. |
| **L5 release runtime** | `make runtime-smoke` | pre-release opt-in | Docker only | Built image boots against throwaway pgvector Postgres; health/readiness, Console assets, metrics, migrations, Control Tower routing, and quality schemas are verified. |
| **L6 live** | `make test-live` | manual only | real API calls | LLM provider round-trips and outbound provider smoke deliveries, all env-gated. |

Use `make release-smoke` before release candidates or large production-facing
changes. It runs `ci-check`, PostgreSQL integration, proto lint/breaking checks,
observability rule/dashboard validation, Compose parsing, whitespace checks, and
the runtime image smoke.

## Unit and fast local tier

Default behaviour. Nothing to set up.

```bash
go test ./...          # full Go unit sweep
go test -short ./...   # CI default for the unit tier
make fast-check        # Go unit + Console typecheck + Console Vitest
make ci-check          # full local PR preflight
```

`make ci-check` mirrors the repository quality gate and also tolerates a local
`console/pnpm-workspace.yaml` file by invoking Console commands with
`pnpm --ignore-workspace`.

## Adversarial and property tier — `make adversarial-check`

Use this tier when changing parser, query binding, aggregation, audit payload,
or normalization code. It combines focused adversarial unit tests with short
Go fuzz runs:

```bash
make adversarial-check
FUZZTIME=30s make adversarial-check
```

The current suite targets classification-quality aggregation and checks that
malformed JSON, duplicate values, illegal dimension names, non-positive
diagnostic counts, non-finite thresholds, non-positive sample IDs, long Unicode
values, and invalid UTF-8 cannot produce panics, negative counters, unbounded
sample lists, or invalid display strings. Seed cases still run during ordinary
`go test`; the make target adds time-boxed mutation. The PostgreSQL integration
suite also scans persisted quality buckets for impossible count relationships,
oversized or non-positive sample IDs, and value-display bound violations after
real refreshes.

## Integration tier — `make test-integration`

The integration tier is gated with `//go:build integration`, so the
default unit sweep stays offline. PostgreSQL suites live under
`test/integration/postgres/<area>` and use `internal/testdb` to open a
real `pgxpool`, run every embedded migration before each smoke test,
and isolate test data with one temporary database or container per
test.

```bash
make test-integration
```

Requirements:

- Docker daemon running locally.
- By default, local runs start `pgvector/pgvector:pg17` with
  testcontainers-go, matching the CI service-container image and the private
  deploy Compose stack.
- To reuse an already-running Postgres instance, set
  `ATTUNE_TEST_DATABASE_URL`; the harness connects to that admin
  database, creates a temporary database per test, runs migrations
  there, and drops it during cleanup.

CI uses the second path: `.github/workflows/ci.yml` runs an
`integration-postgres` job with a GitHub Actions `pgvector/pgvector:pg17`
service container and exports `ATTUNE_TEST_DATABASE_URL` for
`make test-integration`.

`make test-integration` runs packages with `-p 1`. That keeps local
testcontainers fallback runs from starting many Postgres containers at
once; test isolation still comes from a fresh container locally or a
fresh temporary database in CI.

Layout:

- `internal/testdb` owns the reusable Postgres harness only.
- `test/integration/postgres/<area>` contains the PostgreSQL smoke
  suites.
- Do not add package-adjacent `*_io_test.go` files or
  `//go:build integration` tests under business packages.
- Handler-level suites should prefer public routers or public package
  constructors over importing package-private test seams, so they can
  stay in the repository-level integration tree.
- Every integration-suite package has an untagged `doc.go`, so
  `go vet ./...` can enumerate the package when the `integration` tag is
  not set.

`scripts/lint-integration-layout.sh` enforces this layout in
pre-commit, `scripts/check.sh`, and CI.

Current coverage includes migration idempotency, the destructive
Lark-delete preflight guard, feedback JSONB queries, API key
issue/lookup/revoke, tenant + notify-target CRUD, admin bootstrap /
lockout state, inbound source repo state, console inbound delete
branches that require a real `pgxpool`, DB-managed LLM channel/ability/route
CRUD with encrypted write-only credentials, shared Tink key registry startup
checks, enrichment retry/backoff state, and the
ingest → enrich → outbox queue → outbox drain path.

## Browser tier — Console Playwright

The browser tier is intentionally small and focused on user-visible Console
contracts. It uses Playwright against the built Vite preview server, with
deterministic API route mocks:

```bash
cd console
pnpm test:e2e:a11y
```

Guidelines:

- Prefer role, label, text, and explicit test-id locators over CSS structure.
- Use Playwright web-first assertions such as `toBeVisible` and `toHaveURL`.
- Keep API calls mocked unless the test is explicitly a runtime smoke.
- Add browser coverage only for workflows that cannot be trusted to lower-level
  tests, or for regressions found in a real browser.

## Release runtime smoke — `make runtime-smoke`

`make runtime-smoke` builds the production Docker image and runs
`scripts/runtime-smoke.sh` against a throwaway pgvector PostgreSQL container.
The script:

- generates a throwaway Tink keyset using the image under test;
- boots attune from the image with a private temporary config;
- waits for `/readyz`;
- checks `/healthz`, `/readyz`, and `/startupz`;
- verifies `/console/control-tower`,
  `/console/analytics/classification-quality`, and referenced JS/CSS assets;
- verifies `/metrics` exposes Go or attune series;
- checks pgvector is installed, migrations reached at least version 96, and the
  classification-quality tables, classification-quality indexes, and
  feedback-quality action table exist.

You can smoke a prebuilt image without rebuilding:

```bash
ATTUNE_RUNTIME_SMOKE_IMAGE=ghcr.io/phixsura/attune:tag \
  bash scripts/runtime-smoke.sh
```

## Developer parity loop — demo workspace

The demo workspace commands keep the Control Tower walk-through repeatable on a
fresh local install:

```bash
make demo-bootstrap   # clear any old demo rows, then rebuild the baseline
make demo-reset       # remove the demo-seeded rows without reseeding
make demo-seed        # refresh the canonical demo data in place
```

The same flow is available through the CLI:

```bash
docker compose run --rm attune demo bootstrap
docker compose run --rm attune demo reset
docker compose run --rm attune demo seed
```

## Live tier — `make test-live`

The live tier is segregated three ways so it cannot accidentally run:

1. **Separate directory** — `test/live/llmclient/` and
   `test/live/outbound/` (not next to unit tests under `internal/`).
2. **Build tag** — every file in `test/live/...` carries
   `//go:build live`; without the tag, `go test ./...` literally
   compiles to zero test functions in those files.
3. **Env-var skip** — even with `-tags live`, an individual backend
   that has no `KEY` env var calls `t.Skipf`, so partial runs are
   first-class.

### LLM env-var matrix

| Backend | KEY (required) | BASE (optional) | MODEL (optional, default) |
|---|---|---|---|
| `openai-compat`     | `E2E_OPENAI_COMPAT_KEY`     | `E2E_OPENAI_COMPAT_BASE` †     | `E2E_OPENAI_COMPAT_MODEL` (`gpt-4o-mini`) |
| `openai-responses`  | `E2E_OPENAI_RESPONSES_KEY`  | `E2E_OPENAI_RESPONSES_BASE`    | `E2E_OPENAI_RESPONSES_MODEL` (`gpt-4o-mini`) |
| `anthropic`         | `E2E_ANTHROPIC_KEY`         | `E2E_ANTHROPIC_BASE`           | `E2E_ANTHROPIC_MODEL` (`claude-sonnet-4-5`) |
| `gemini`            | `E2E_GEMINI_KEY`            | `E2E_GEMINI_BASE`              | `E2E_GEMINI_MODEL` (`gemini-2.0-flash`) |

† `openai-compat` has no vendor default — `BASE` is required (point it
at `https://api.openai.com` or your vLLM / ollama / oneapi host).

The three SDK-backed backends inherit the vendor's default host when
`BASE` is empty (`api.openai.com` / `api.anthropic.com` /
`generativelanguage.googleapis.com`).

### Outbound env-var matrix

These tests post real provider messages. Use sandbox channels/webhooks and
repository fixtures; never point them at a customer-facing channel.

| Provider | Required env | Optional env | Behavior |
|---|---|---|---|
| `raw-webhook` | `E2E_OUTBOUND_RAW_WEBHOOK_URL` | `E2E_OUTBOUND_RAW_WEBHOOK_SECRET` | POSTs the generic Attune event payload and accepts any adapter-success 2xx response. |
| `slack` | `E2E_OUTBOUND_SLACK_WEBHOOK_URL` | — | POSTs one Block Kit event message to the webhook's channel. |
| `discord` | `E2E_OUTBOUND_DISCORD_WEBHOOK_URL` | — | POSTs one embed event message to the webhook's channel. |
| `lark` | `E2E_OUTBOUND_LARK_WEBHOOK_URL` | `E2E_OUTBOUND_LARK_SECRET` | POSTs one interactive card; when the secret is set, the adapter signs the request. |
| `github-issue` | `E2E_OUTBOUND_GITHUB_REPO_URL`, `E2E_OUTBOUND_GITHUB_TOKEN`, `E2E_OUTBOUND_GITHUB_CREATE_ISSUE=1` | — | Creates one issue through the adapter, verifies GitHub's response, then closes the issue as completed. |

The GitHub test requires the extra `E2E_OUTBOUND_GITHUB_CREATE_ISSUE=1`
acknowledgement because it mutates a real repository even though cleanup closes
the created issue.

### Recipes

**Only one backend, against the vendor default:**

```bash
E2E_ANTHROPIC_KEY=sk-ant-... \
  make test-live
```

The other three are silently skipped because their `KEY` env vars are
unset. The test report will say `--- SKIP: TestLive_OpenAICompat_*`
etc., which is the intended UX.

**All four against a LiteLLM Proxy that translates protocols:**

```bash
E2E_OPENAI_COMPAT_BASE=http://localhost:4000     E2E_OPENAI_COMPAT_KEY=sk-litellm-... \
E2E_OPENAI_RESPONSES_BASE=http://localhost:4000  E2E_OPENAI_RESPONSES_KEY=sk-litellm-... \
E2E_ANTHROPIC_BASE=http://localhost:4000         E2E_ANTHROPIC_KEY=sk-litellm-... \
E2E_GEMINI_BASE=http://localhost:4000            E2E_GEMINI_KEY=sk-litellm-... \
  make test-live
```

**Override the model** (e.g. against a vLLM-served custom model):

```bash
E2E_OPENAI_COMPAT_BASE=http://my-vllm:8000 \
E2E_OPENAI_COMPAT_KEY=any-string \
E2E_OPENAI_COMPAT_MODEL=Qwen2.5-7B-Instruct \
  make test-live
```

**See what would run given the current env:**

```bash
make test-live-list
```

**Run only outbound live smoke tests:**

```bash
E2E_OUTBOUND_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... \
  go test -tags=live -count=1 -run '^TestLive_Outbound' ./test/live/outbound
```

## Cost guardrails

Live tests are inexpensive by design but not free.

LLM tests:

- Uses a small, cheap default model (`gpt-4o-mini`,
  `claude-sonnet-4-5`, `gemini-2.0-flash`). Override with the
  per-backend `MODEL` env if you need to.
- Sets `MaxTokens` = 512 (1024 for OpenAI Responses) — bounded
  generation length.
- Sets `Temperature` = 0.1 — near-deterministic output.
- Has a per-test `context.WithTimeout` of 90–180s — bounded blast
  radius if the upstream hangs.

A full 8-backend sweep against the named vendor defaults costs roughly
US$0.01–0.05 at 2026-06 rates. Always re-check before pointing at a
flagship model.

Outbound tests:

- Send exactly one provider request per configured webhook.
- Use sandbox content with no customer data and no active mentions.
- Mutate GitHub only when `E2E_OUTBOUND_GITHUB_CREATE_ISSUE=1` is set, and close
  the issue during test cleanup.
- Use a 20s HTTP timeout per provider request.

## Why not in CI?

Live tests do **not** run in PR or push CI. Industry convergence
(AWS SDK Go v2, sashabaranov/go-openai, langchaingo) is that paid
calls and provider webhooks only run on a dedicated workflow — never on every
contributor's push. attune does not yet ship that workflow; when it does it will
be a `workflow_dispatch`-triggered file under `.github/workflows/` referencing
the `E2E_*` secrets.

If you have a sandbox key and want to add this workflow, open an issue
referencing this section and the multi-protocol LLM client proposal
(`docs/proposals/2026-06-06-enricher-per-tenant-prompt.md`).

## CI gates documented in CLAUDE.md §1

These are the unit-tier-or-static gates every PR must pass:

| Check | Bar |
|---|---|
| `go build ./...` | 0 errors |
| `go vet ./...` | 0 warnings |
| `go test -short ./...` | all pass on changed code |
| `lizard . -l go -C 15 -T nloc=100` | CCN ≤ 15, NLOC ≤ 100 per function |
| `npx -y jscpd . --pattern '**/*.go' --threshold 5` | duplication < 4 % |
| `bash scripts/lint-slog.sh` | rule-1 / rule-2 / rule-3 all clean |
| `buf generate` (no diff) | proto contract in sync |

See `scripts/check.sh` for a one-shot runner.
