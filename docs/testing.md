# Testing

attune has three test tiers. Per-CLAUDE.md §1, every PR must pass the
unit tier. The PostgreSQL integration tier runs in CI for Go changes
and is opt-in locally because it needs Docker. The live tier is
**opt-in only** and runs against paid external APIs.

| Tier | Default? | Cost | What it covers |
|---|---|---|---|
| **Unit** | ✅ runs on `go test ./...` and in CI | $0 | Pure logic + handler/repo wiring + LLM client wire-shape via `httptest` mocks. |
| **Integration** | ✅ CI on Go changes; local via `make test-integration` | Docker only | Real PostgreSQL migrations, repos, service/repo transaction paths, and outbox drain smoke tests. |
| **Live** | ❌ opt-in (`make test-live` + `//go:build live`) | real LLM tokens | One round-trip per backend × {free-form, structured}, 8 tests total. |

## Unit tier

Default behaviour. Nothing to set up.

```bash
go test ./...                # full unit sweep
go test -short ./...         # CI default — same as above today, reserved
                             # for any future long-running unit tests
./scripts/check.sh           # build + vet + test + lizard + jscpd
```

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
- By default, local runs start `postgres:18` with testcontainers-go,
  matching the CI service-container image.
- To reuse an already-running Postgres instance, set
  `ATTUNE_TEST_DATABASE_URL`; the harness connects to that admin
  database, creates a temporary database per test, runs migrations
  there, and drops it during cleanup.

CI uses the second path: `.github/workflows/ci.yml` runs an
`integration-postgres` job with a GitHub Actions `postgres:18` service
container and exports `ATTUNE_TEST_DATABASE_URL` for
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
branches that require a real `pgxpool`, and the
ingest → enrich → outbox queue → outbox drain path.

## Live tier — `make test-live`

The live tier is segregated three ways so it cannot accidentally run:

1. **Separate directory** — `test/live/llmclient/` (not next to the
   unit tests under `internal/infra/llmclient/`).
2. **Build tag** — every file in `test/live/...` carries
   `//go:build live`; without the tag, `go test ./...` literally
   compiles to zero test functions in those files.
3. **Env-var skip** — even with `-tags live`, an individual backend
   that has no `KEY` env var calls `t.Skipf`, so partial runs are
   first-class.

### Env-var matrix (per backend)

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

## Cost guardrails

Live tests are inexpensive by design but not free. Each test:

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

## Why not in CI?

Live tests do **not** run in PR or push CI. Industry convergence
(AWS SDK Go v2, sashabaranov/go-openai, langchaingo) is that paid
calls only run on a dedicated workflow — never on every contributor's
push. attune does not yet ship that workflow; when it does it will be
a `workflow_dispatch`-triggered file under `.github/workflows/`
referencing the `E2E_*` secrets.

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
