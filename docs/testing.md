# Testing

attune follows a test pyramid with extra gates for contracts, observability, and
release packaging. Most confidence should come from fast unit and integration
tests; browser and runtime tests are fewer, but they cover the production seams
that cheap tests cannot see.

| Tier | Command | Default? | Cost | What it covers |
|---|---|---|---|---|
| **L0 fast local** | `make fast-check` | local opt-in | $0 | Fast Go unit sweep plus Console typecheck and Vitest. |
| **L1 CI preflight** | `make ci-check` | PR / before push | $0 | Go race unit tests, lint, helper-script tests, complexity, duplication, Console type/build/test/arch checks, and a required local TruffleHog secret scan. |
| **L2 contract** | `make proto-lint`, `make proto-breaking`, `make proto` | CI on proto changes | network for Buf remote plugins | Protobuf/OpenAPI/SDK contract shape and generation consistency. |
| **L3 integration** | `make test-integration` | CI on Go changes; local opt-in | Docker preferred; local PostgreSQL binaries fallback | Real pgvector PostgreSQL migrations, repos, service/repo transaction paths, restore drills, and queue/outbox smoke tests. |
| **L4 browser** | `cd console && pnpm test:e2e:a11y` | CI on Console changes | browser install | Critical Console routes in real Chromium with API mocks, accessibility, overflow, console-error, and interaction coverage. |
| **L5 release runtime** | `make runtime-smoke` | pre-release opt-in | Docker only | Built image boots against throwaway pgvector Postgres; health/readiness, Console assets, metrics, migrations, Control Tower routing, and quality schemas are verified. |
| **L6 live** | `make test-live` | manual only | real API calls | LLM provider round-trips and outbound provider smoke deliveries, all env-gated. |
| **L7 full-stack browser acceptance** | no single command; see checklist below | PR evidence for high-risk product workflows | Docker + real browser + provider mock or sandbox | Production image, real Postgres, real Console, mouse-driven browser actions, provider-side evidence, database evidence, and log review for workflows where scripts are not enough. |

Use `make release-smoke` before release candidates or large production-facing
changes. It runs `ci-check`, PostgreSQL integration, proto lint/breaking checks,
observability rule/dashboard validation, Compose parsing, whitespace checks,
the public board + Console browser smoke, and the runtime image smoke. L7
acceptance is intentionally separate because it requires a human-operated
browser session and saved evidence.

## Unit and fast local tier

Default behaviour. Nothing to set up.

```bash
go test ./...          # full Go unit sweep
go test -short ./...   # CI default for the unit tier
make fast-check        # Go unit + Console typecheck + Console Vitest
make script-tests      # Node tests for repository helper scripts
make ci-check          # full local PR preflight
```

`make ci-check` mirrors the repository quality gate and also tolerates a local
`console/pnpm-workspace.yaml` file by invoking Console commands with
`pnpm --ignore-workspace`. Node-based gates run through
`scripts/with-supported-node.sh`, which follows CI's Node 22 baseline while
accepting Node 20, 22, or 24+. Set `ATTUNE_NODE_BIN` or
`ATTUNE_NODE_SEARCH_PATHS` if the supported runtime is installed in a custom
location. Its secret scan uses a `trufflehog` binary from `PATH` when available,
or the pinned Docker image fallback from `scripts/secret-scan.sh`. The local
scan covers current checkout files, staged index files, and the current HEAD, or
commits since `origin/main` when the branch has local commits; set
`TRUFFLEHOG_BASE_REF` to compare against a different base.

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
and isolate test data with one temporary database per test.

```bash
make test-integration
```

Requirements:

- Docker daemon running locally is the preferred path.
- When Docker is available and `ATTUNE_TEST_DATABASE_URL` is unset, the make
  target starts one shared `pgvector/pgvector:pg17` service container, matching
  the CI service-container image and the private deploy Compose stack.
- When Docker is unavailable, the harness falls back to installed PostgreSQL
  binaries (`initdb` and `pg_ctl`) and still runs against a real temporary
  cluster on `127.0.0.1`.
- To reuse an already-running Postgres instance, set
  `ATTUNE_TEST_DATABASE_URL`; the harness connects to that admin
  database, creates a temporary database per test, runs migrations
  there, and drops it during cleanup.
- To override the Go test timeout, set `ATTUNE_TEST_INTEGRATION_TIMEOUT`.

CI uses the second path: `.github/workflows/ci.yml` runs an
`integration-postgres` job with a GitHub Actions `pgvector/pgvector:pg17`
service container and exports `ATTUNE_TEST_DATABASE_URL` for
`make test-integration`.

`make test-integration` runs packages with `-p 1`. That keeps local
fallback runs from stampeding the shared Postgres instance; test
isolation comes from a fresh temporary database in both local Docker
and CI service-container mode.

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

## Public board browser smoke — `make public-board-smoke`

Use this when changing the public portal board, the Console public-visibility
preview, quick filters, or roadmap semantics. It boots a temporary local
PostgreSQL cluster, starts attune with a throwaway config, seeds two demo
tenants, builds the Console bundle, logs into Console in Chromium, and runs
the public board plus the public-visibility page with real requests.

```bash
make public-board-smoke
cd console && pnpm test:e2e:public-board
```

This smoke verifies Console login, the public-visibility preview links back to
the live board and portal, list/detail navigation, vote and comment actions,
Console moderation approval, mobile layout, quick-filter empty states,
tenant-scoped visitor cookies, and the intentional split between
`/portal/{tenant}/requests` and `/portal/{tenant}/roadmap`. It also seeds
private and pending requests to confirm they stay out of the public board and
search results, checks that a pending comment is only visible to the visitor
who created it before moderation, and switches between two tenants in one
browser context to catch cookie leakage.

CI runs the same smoke on changes that touch the Go or Console surfaces.

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
- checks pgvector is installed, migrations reached at least version 112, and the
  classification-quality tables, classification-quality indexes, and
  feedback-quality action table exist;
- checks the production schema contains the external sync metadata required by
  managed GitHub Issue sync: `external_sync_runs.input_metadata`,
  `external_object_links.normalized_payload`, `external_object_comments`, and
  the `customer_request_issue_links.external_object_link_id` bridge.

You can smoke a prebuilt image without rebuilding:

```bash
ATTUNE_RUNTIME_SMOKE_IMAGE=ghcr.io/phixsura/attune:tag \
  bash scripts/runtime-smoke.sh
```

## Full-stack browser acceptance

Detailed runbook and copyable evidence template:
[`docs/full-stack-browser-acceptance.md`](full-stack-browser-acceptance.md).

Use this tier when a workflow crosses all of these boundaries:

- the production Docker image and embedded Console bundle;
- real PostgreSQL migrations and background workers;
- authenticated Console sessions;
- outbound provider HTTP, webhooks, retry, cursor, or dedupe behavior;
- a user-visible state change that lower-level tests can miss.

This tier is not a substitute for L0-L6. It is the final operator-style proof
that the built artifact works through the same surfaces a tenant will use.

Required setup:

- build the production image from the exact source state under review;
- run the image with a throwaway pgvector PostgreSQL database;
- use an HTTPS provider mock or a disposable provider sandbox;
- make the provider mock record method, path, query, and whether authorization
  was present, without logging secret values;
- keep the app, database, and mock running until evidence has been captured.

Required browser discipline:

- use a visible browser window;
- use mouse clicks, mouse scrolling, and ordinary form entry for the acceptance
  path;
- use read-only DOM inspection only to confirm what is visible or to copy exact
  evidence text;
- do not replace the acceptance path with Playwright locators, direct API
  mutations, or database writes.

Required evidence:

- deployed base URL, image tag or digest, and compose/project identifier;
- screenshots for the key before/after UI states;
- provider mock or sandbox evidence for every expected outbound request;
- database rows proving durable state, such as queued/succeeded runs, object
  links, delivery links, comments, or event dedupe rows;
- service logs for the same time window, including a note about unrelated
  warnings and every error found;
- teardown command, or an explicit note that the stack was intentionally left
  running for review.

After the mouse-driven path is complete, use
`scripts/collect-full-stack-evidence.sh` to collect read-only deployment,
database, provider, and log evidence. The collector is supporting evidence only;
it does not satisfy the mouse-driven browser requirement by itself.

Hard failures:

- the Console cannot be reached from the production image;
- the relevant workflow can pass only through direct API or database mutation;
- provider calls are absent, unsigned when signatures are required, missing
  authorization, or aimed at the wrong host;
- sync run status is `failed`, `dead`, or `partial` without an accepted product
  explanation;
- the UI does not show the durable state that the database contains;
- service logs show TLS, SSRF, refused connection, panic, migration, worker, or
  permission errors in the workflow under validation.

For GitHub Issue sync changes, the acceptance path must cover:

- Console login against the deployed image;
- creating or selecting a Customer Request in the real Console;
- creating a GitHub connection against an HTTPS mock or sandbox;
- testing the connection from the Console and confirming the provider saw a
  repository read with authorization;
- setting the Customer Request to Issue mapping to `bidirectional` or `push`;
- creating a GitHub Issue from the Customer Request detail page;
- confirming the provider saw `POST /repos/{owner}/{repo}/issues`;
- confirming any managed backlink/comment path, when enabled, is marker-deduped
  and visible in provider evidence;
- confirming the Customer Request detail page shows the issue link, external
  state, sync state, and timestamp;
- confirming the external sync page shows a succeeded `push` run with one seen
  record, one changed record, zero failures, and zero conflicts.

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
