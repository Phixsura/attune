# LLM-generated empathetic reply draft per feedback

| | |
|---|---|
| **Issue** | #26 |
| **Status** | Accepted |
| **Started** | 2026-06-13 CST |
| **Related** | #25 (embedding outbox/worker paradigm reused), #24 (LLM confidence — gating signal — and `llm_audit` cost table), #10 (per-tenant prompt template, closed), #109 (managed LLM routing) |

## Problem

Operators replying to customer feedback spend 5–10 minutes per row drafting a
polite, contextual response from scratch. Pre-generating a draft at enrichment
time — so the operator only edits and sends — cuts that to ~1 minute per row.

The current pipeline classifies each feedback with a single LLM call and stops:

```
Ingest → Triage → LLM Classification (1 call) → Persist → Dispatch
```

`internal/service/enrich/enricher.go:247` is the only `llm.Complete` call in the
enrich path. There is no generative assist; every reply is hand-written
downstream. This proposal adds a **second, optional LLM step** that produces a
ready-to-edit reply draft, persisted on the feedback row and surfaced in Console.

Because a draft is a discrete incremental LLM call **for every enriched row**, it
roughly **doubles LLM cost** for tenants that enable it — so the feature must be
opt-in, default-off, and cost-observable.

## Industry benchmarking

Verified via a multi-source review (21 sources, 96 extracted claims, 25
adversarially verified 3-vote, 4 refuted). The strongest backend-architecture
evidence is open-source (vendor SaaS only exposes product/billing docs, not
internal queue/worker design).

| System | Trigger | Execution architecture | Storage | opt-in | Auto-send |
|---|---|---|---|---|---|
| **Chatwoot Captain** | on-demand (copilot) | **async Sidekiq job + failure isolation** (verified against `develop` branch source) | — | per-inbox assistant (JSONB config) | No¹ |
| **Zendesk suggested first reply** | **pre-generated** (only on high-confidence match, first reply only) | product docs only | overwrite (ghost-text) | admin default-off + per-group | No |
| **Help Scout AI Drafts** | **hybrid**: on-demand + opt-in workflow action pre-fills queue | product docs only | overwrite (keep-history claim refuted 1-2) | opt-in, **$50 / 100 conversations** metered | No |
| **FreeScout GPT** | on-demand (keystroke) | client-side extension, no backend | overwrite `editor.innerHTML` | install plugin | No |
| **Zammad Smart Assist** | on-demand (text selection) | — | — | — | No (mandatory Approve) |
| **Intercom AI Compose** | on-demand | product docs only | accept / undo / try-again | (off-by-default claim refuted 0-3) | No |

¹ Chatwoot's auto-send path is a separate autonomous-agent product
(`ResponseBuilderJob`), not the draft feature.

Five conclusions that shaped this design:

1. **Human-in-the-loop is near-universal consensus** (7 sources, 3-0). Reply
   drafts are agent-facing suggestions and are **never auto-sent**; every vendor
   that auto-sends does so through a separate, distinctly-branded autonomous
   agent. → attune's draft must **never** enter `notify`/`outbox`.
2. **OSS execution architecture is async worker + failure isolation** (Chatwoot
   `ResponseBuilderJob.perform_later`, `rescue StandardError` without re-raise →
   log → graceful degrade, never breaking the ticket; `retry_on` with backoff
   for transient errors). → matches attune's existing embedding outbox+worker.
   Scoping caveat: this evidence is from Captain's *autonomous* auto-reply job,
   not its human-in-the-loop draft path — it proves "a mature OSS system runs its
   conversational-LLM call on an async worker with failure isolation," which is
   exactly attune's embedding paradigm, not that the draft feature itself must be
   async (that conclusion rests on attune's own pattern + the cost-isolation goal).
3. **Pre-generation is an opt-in path, not the baseline.** Help Scout's hybrid is
   the closest precedent: opt-in workflow pre-fill (= attune enrichment-time
   generation) **plus** an on-demand path (= attune Console Regenerate).
4. **The only cost levers in the wild** are opt-in enablement, per-scope gating,
   **confidence-gating** (Zendesk only drafts on a high-confidence match), and
   spend limits. No vendor uses caching/batching to amortize per-draft cost.
   Help Scout's per-conversation metering confirms each draft is a real
   incremental cost event.
5. **Storage is overwrite-into-field, not versioned.** The one claim that Help
   Scout preserves regenerate history was refuted; FreeScout overwrites. → single
   column + `llm_audit` for call history; no draft-version table.

Honest limits: no backend-architecture evidence was retrievable for Front,
Gorgias, Freshdesk, or Dosu; the "async" conclusion rests on OSS source plus
attune's own established pattern, not whole-industry measurement.

## Goals

- After classification, **pre-generate** a reply draft for each feedback row of
  an opt-in tenant, off the main enrich path.
- Run generation through an **async outbox + worker** so a draft-LLM failure is
  fully isolated from — and never rolls back — the classification result.
- **Abstract a generic task-outbox** (`internal/repo/taskoutbox`) shared by the
  existing `embedding_task` and the new `reply_draft_task`, eliminating
  duplicated claim/retry SQL (CLAUDE.md §6; keeps `jscpd < 4%`).
- Gate generation per tenant: opt-in `reply_draft_enabled` (default off) plus a
  configurable **confidence threshold** `reply_draft_min_confidence`.
- Record token usage / cost per draft call in `llm_audit` (`purpose='reply_draft'`).
- Persist the draft on `user_feedback.reply_draft`; surface it in Console with
  **Copy** and **Regenerate** (Regenerate = synchronous re-generate endpoint).
- Support a per-tenant `reply_draft_prompt_template` override.

## Non-goals

- **Drafts are never auto-sent.** No path from `reply_draft` into
  `notify`/`outbox`. The draft is operator-facing only.
- No draft version history table (overwrite single column; `llm_audit` keeps the
  call-level trail). No external precedent for keep-history.
- No batching/caching cost-amortization (no industry precedent; opt-in +
  confidence-gating are the cost levers).
- No global `config.yaml` on/off knob — enablement is purely per-tenant, matching
  the clustering (#25) precedent.
- No change to the classification call, schema, or `IsUrgent` derivation.
- No gating signals beyond classification confidence in this PR (kind/severity
  gating is a future option).

## Decision Record

| Detail | Decision |
|---|---|
| Trigger architecture | **Async outbox** (`reply_draft_task`) + worker. Not synchronous-inline, not fire-and-forget. |
| Outbox abstraction | New `internal/repo/taskoutbox` generic queue (parameterized table name + per-tenant enable column). `embedding_task` refactored onto it; `reply_draft_task` newly built on it. |
| Draft storage | Overwrite `user_feedback.reply_draft TEXT` + `reply_draft_generated_at TIMESTAMPTZ`. No version table. |
| Call history / cost | Draft calls reuse the existing `llmaudit.Client` wrapper — the same `e.llm` the enricher already uses (wired `rawLLM → llmguard → llmaudit → enricher` in `server.go:110`). Setting `Guard.Purpose='reply_draft'` + `Guard.FeedbackID` **auto-records** a `llm_audit` row (tokens from `resp.Usage`, cost from `PriceUsage`) + LLM metrics. No explicit `writeAudit` (that is embedding's pattern only because it bypasses the wrapper via `router.Embed`). Zero schema change. |
| Opt-in | Per-tenant `tenants.reply_draft_enabled BOOLEAN DEFAULT FALSE`. No global config bool. |
| Confidence gating | Per-tenant `tenants.reply_draft_min_confidence DOUBLE PRECISION DEFAULT 0` (0 = no gate). `NULL` confidence is **not** admitted when threshold > 0 (no self-rating → don't spend). |
| Prompt | `renderDraftPrompt(content, enriched, language)`; `sentiment` read from the `user_feedback.enriched_attrs` JSONB (`enriched_attrs['sentiment']`, a configurable dimension, not a fixed column). Per-tenant `reply_draft_prompt_template` overrides the default. |
| LLM call | Reuse the wrapped `llmclient.Complete` (`e.llm`); temperature ≈ 0.4 (natural tone, vs 0.0 for classification); output in the detected language; freeform text output (no JSON schema). |
| Regenerate | Synchronous `POST` endpoint → `ReplyDrafter.Generate` → `UPDATE` + audit → returns new draft. Worker and endpoint share the same `Generate` core. |
| Failure mode | `MarkFailed` with exponential backoff; never blocks or rolls back enrichment; degrades to an empty draft (Chatwoot-style graceful degrade). |
| Metrics | `attune_reply_draft_{generated_total,errors_total,duration_seconds,queue_depth}`. |
| Console | Draft card + Copy + Regenerate; `GET` feedback carries `reply_draft`. |

## Proposal

### System flow

```
Ingest → Triage → LLM Classification (#1) ─┐
                                           ▼  persistEnriched (single tx)
                              MarkDone + taskoutbox.CreateTx ──┐ gate: reply_draft_enabled
                                           │                   │   AND confidence ≥ min_confidence
                                           ▼                   ▼
                                     user_feedback       reply_draft_task (pending)
                                                               │
   ┌───────────────────────────────────────────────────────────┘
   ▼  reply-draft worker (own goroutine; mirrors embedding worker)
 TryClaim → LoadForDraft(content + enriched_attrs + title + language)
   → renderDraftPrompt → llm.Complete (#2, via wrapped llmaudit.Client,
       Guard.Purpose='reply_draft' → token/cost audit auto-recorded)
   → UPDATE user_feedback.reply_draft → MarkDone
   └─ any step fails → MarkFailed(backoff); user_feedback row untouched

 Console: draft card + Copy + Regenerate
 Regenerate: POST → ReplyDrafter.Generate (sync) → UPDATE + audit → returns draft
```

### Generic task-outbox (`internal/repo/taskoutbox`)

`embedding_task` and `reply_draft_task` differ only in three places: the table
name, the per-tenant enable column (`clustering_enabled` vs
`reply_draft_enabled`), and the domain-specific result write (`UpdateEmbedding`
vs `UpdateReplyDraft`). The outbox skeleton — `Task` struct, `TryClaim` (with the
tenant enable-join and `FOR UPDATE SKIP LOCKED`), `MarkDone`, `MarkFailed`
(exponential backoff), `ResetStaleClaims`, `QueueDepth` — is identical (~150 lines
of SQL today).

```go
package taskoutbox

type Task struct {
    ID, FeedbackID int64
    TenantID, Status, LastError string
    Attempts int
    ClaimedAt, NextRetryAt, CompletedAt *time.Time
    CreatedAt time.Time
}

// Queue wraps one outbox table. table + enableColumn are controlled internal
// constants (never user input), interpolated into SQL by the constructor.
type Queue struct { pool *pgxpool.Pool; table, enableColumn string }

func New(pool *pgxpool.Pool, table, enableColumn string) *Queue
func (q *Queue) TryClaim(ctx, staleDuration) (*Task, error)
func (q *Queue) MarkDone(ctx, taskID) error
func (q *Queue) MarkFailed(ctx, taskID, err, maxAttempts) error
func (q *Queue) ResetStaleClaims(ctx, staleDuration) (int64, error)
func (q *Queue) QueueDepth(ctx, tenantID) (int64, error)
```

The unified `TryClaim`/`MarkDone`/`MarkFailed`/`ResetStaleClaims`/`QueueDepth` are
the bulk of the duplicated SQL. **Enqueue (`CreateTx`) stays per-caller** because
the gate differs: embedding gates on `clustering_enabled` only; reply-draft adds
the `confidence ≥ min_confidence` clause. Enqueue is ~5 lines, so the residual
duplication is below the jscpd threshold. `embedding_task`'s domain methods
(`FindSimilar`, cluster queries, `UpdateEmbedding`) are unaffected and stay in
`internal/repo/embedding`.

This is the §6 "abstract before the 3rd implementation" call made one early: it
is required here anyway because cloning ~150 lines of identical SQL would trip the
`jscpd < 4%` gate.

### Data model (migration 026)

```sql
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS reply_draft TEXT,
    ADD COLUMN IF NOT EXISTS reply_draft_generated_at TIMESTAMPTZ;

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS reply_draft_enabled        BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS reply_draft_min_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reply_draft_prompt_template TEXT;
-- CHECK (reply_draft_min_confidence >= 0 AND reply_draft_min_confidence <= 1)

CREATE TABLE IF NOT EXISTS reply_draft_task (
    id            BIGSERIAL PRIMARY KEY,
    feedback_id   BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    tenant_id     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    attempts      INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claimed_at    TIMESTAMPTZ,
    next_retry_at TIMESTAMPTZ,
    last_error    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ,
    CHECK (status IN ('pending','processing','done','failed')),
    UNIQUE (feedback_id)
);
CREATE INDEX IF NOT EXISTS idx_reply_draft_task_pending
    ON reply_draft_task (created_at) WHERE status IN ('pending','failed');
```

### Confidence gating

The gate lives in the enqueue `CreateTx` (single tx with `MarkDone`):

```sql
INSERT INTO reply_draft_task (feedback_id, tenant_id, status)
SELECT $1, $2, 'pending'
WHERE EXISTS (
    SELECT 1 FROM tenants
    WHERE id = $2 AND reply_draft_enabled = TRUE
      AND ($3::float8 IS NOT NULL AND $3 >= reply_draft_min_confidence
           OR reply_draft_min_confidence = 0)
)
ON CONFLICT (feedback_id) DO NOTHING
```

`$3` is `classification_confidence` (nullable). Threshold 0 (default) admits all
enabled rows including `NULL`-confidence; any threshold > 0 admits only rows whose
model self-rating meets it — `NULL` is excluded, so unrated rows don't burn
tokens.

### Reply drafter (`internal/service/replydraft`)

A new package mirroring `internal/service/embedding`: `enrich` only gains
`SetDraftTask` + the enqueue call; the drafter and worker live in `replydraft`.

`ReplyDrafter.Generate(ctx, feedbackID) (draft string, err error)` is the shared
core called by **both** the worker (outbox-orchestrated) and the Regenerate
endpoint (synchronous):

1. `LoadForDraft(feedbackID)` — a **new** repo method (the embedding worker's
   `GetFeedbackContent` reads only `content`). Returns `content`, the
   `enriched_attrs` JSONB (→ title / kind / severity / modules /
   `enriched_attrs['sentiment']`), and the detected `language`, plus the
   per-tenant template.
2. `renderDraftPrompt` → default template, or the tenant override. Default:

   > You are a support operator. Draft a brief, empathetic reply to the feedback
   > below. Write 2–3 sentences in `{language}`. Acknowledge the issue, show you
   > understand, and set one clear next-step expectation. Do not promise timelines
   > or fixes you cannot guarantee.
   > Feedback: `{content}`
   > Context: kind=`{kind}`, severity=`{severity}`, modules=`{modules}`, sentiment=`{sentiment}`

3. `llm.Complete` via the wrapped `llmaudit.Client` with `Guard.Purpose='reply_draft'`
   + `Guard.FeedbackID` → token/cost audit + metrics auto-recorded (no explicit
   `writeAudit`). Temp ≈ 0.4, freeform; parse = trim + strip any leading
   "Here's a draft:"-style preamble + length guard.
4. Worker path: `UpdateReplyDraft` + `MarkDone`. Endpoint path: same
   `UpdateReplyDraft`, returns synchronously. Both share the `Generate` core.
   Concurrent first-gen vs Regenerate: last write wins (acceptable — operator
   sees the latest; both calls are independently audited).

### Console

`GET` feedback carries `reply_draft` + `reply_draft_generated_at`. Detail view
shows a draft card with **Copy** and **Regenerate**. Regenerate calls the
synchronous endpoint, replaces the card content, and (per issue) the new call
writes a fresh `llm_audit` row — the prior draft is overwritten; its generation is
retained only as the prior audit row.

## Alternatives considered

- **Synchronous inline second call** — simplest, strongly consistent, but doubles
  enrich latency and couples draft-LLM failure to the main ingest flow, violating
  "draft failure must never affect classification." No researched system chains a
  second LLM into the main request path. Rejected.
- **Fire-and-forget goroutine** — lighter, no new table, but loses in-flight work
  on restart, has no durable retry/backoff or backpressure. No OSS precedent for
  reliable AI generation this way. Rejected.
- **Clone the embedding outbox SQL** — fastest, lowest churn, but ~150 lines of
  near-identical SQL would trip `jscpd < 4%` and violate §6. Rejected in favor of
  the generic `taskoutbox`.
- **Helper-only (shared claim helper, no package)** — viable middle ground;
  rejected in favor of the cleaner full `taskoutbox` package abstraction.
- **Draft version-history table** — no external precedent (keep-history claim was
  refuted); `llm_audit` already retains the per-call trail. Rejected (YAGNI).
- **Global `config.yaml` on/off knob (issue's literal wording)** — inconsistent
  with the clustering precedent and redundant with per-tenant opt-in. Rejected;
  per-tenant `reply_draft_enabled` is the opt-in mechanism.
- **On-demand-only generation (industry-mainstream)** — attune's enrichment-time
  pre-generation matches its existing ingest-time LLM pipeline and the operator's
  "open and it's ready" goal; the on-demand path is still covered by Console
  Regenerate. Pre-generation chosen as default, on-demand retained for refresh.

## Risks / tradeoffs

- **Refactoring `embedding_task` onto `taskoutbox` touches just-merged #25 code.**
  Mitigation: the existing `clustering_test.go` integration test guards behavior;
  do the refactor as its own step and keep it green before adding reply-draft.
- **Cost doubling.** Mitigation: opt-in default-off + per-tenant confidence gate;
  `llm_audit` makes spend visible per call.
- **Draft quality / hallucination.** Mitigation: human-in-the-loop (operator must
  review/edit/send; never auto-sent); confidence gating skips low-confidence rows;
  prompt forbids unguaranteed promises.
- **Cross-language quality.** Mitigation: reuse existing language detection; prompt
  snapshot tests cover zh/en/ja input dimensions.
- **Worker backlog under load.** Mitigation: `queue_depth` metric + backoff; opt-in
  scope limits volume.

## Implementation plan

1. `internal/repo/taskoutbox` generic queue; refactor `embedding_task` onto it;
   keep `clustering_test.go` green. (pure refactor — changelog-exempt if shipped
   as its own PR; otherwise covered by the feature changelog in step 9)
2. Migration `026_reply_draft.sql` (feedback columns, tenant columns + CHECK,
   `reply_draft_task`).
3. Repo: `reply_draft_task` queue via `taskoutbox`; `feedback.UpdateReplyDraft`;
   new `feedback.LoadForDraft` (content + `enriched_attrs` + title + language).
4. Service: new `internal/service/replydraft` (mirrors `internal/service/embedding`)
   — `ReplyDrafter` (`renderDraftPrompt` + `Generate` core) and `DraftWorker`;
   `writeAudit(purpose='reply_draft')`.
5. Enricher: `SetDraftTask` + enqueue (with gate) inside `persistEnriched` tx.
6. `cmd/attune`: `startReplyDraftWorker` wiring (mirror `startEmbeddingWorker`).
7. Handlers/console: `reply_draft` on the feedback read; synchronous Regenerate
   `POST` endpoint (proto IDL per §11 if it lands on the contract).
8. Console UI: draft card + Copy + Regenerate.
9. Metrics, `CHANGELOG.md` `### Added`, proposal `Status → Implemented`.

## Verification

- **Unit/component**: prompt snapshot across all input dimensions (kind, severity,
  modules, sentiment, language) incl. tenant-template override; parse+storage
  round-trip; `taskoutbox` claim/backoff/stale-reset; gating predicate (enabled
  off → no task; threshold > 0 + NULL confidence → no task; threshold met → task).
- **Token/cost audit**: mocked LLM with token counts → one `llm_audit` row,
  `purpose='reply_draft'`, tokens/cost match.
- **End-to-end (mocked LLM)**: ingest → enrich → task enqueued → worker → draft
  stored → Console read shows it.
- **Regenerate**: synchronous call replaces draft + writes a new audit row.
- **Integration (Postgres)**: `reply_draft_task` enqueue+claim+gate under
  `make test-integration`.
- **Gates**: `go test -race`, `lizard` CCN ≤ 15 / NLOC ≤ 100, **`jscpd < 4%`
  (confirms the abstraction removed duplication)**, `golangci-lint`, `biome`,
  `vitest`, proto-sync if the endpoint lands on the contract.

## References

- Industry benchmarking (deep-research, 2025–2026 product state):
  - Chatwoot Captain async worker + failure isolation — https://deepwiki.com/chatwoot/chatwoot/9.1-captain-ai-system
  - Help Scout AI Drafts (hybrid trigger) — https://docs.helpscout.com/article/1570-ai-drafts ; pricing/metering — https://docs.helpscout.com/article/1539-ai-drafts-pricing-billing ; workflow pre-fill — https://docs.helpscout.com/article/1399-automatic-workflows
  - Zendesk suggested first reply (confidence-gated pre-generation, opt-in) — https://support.zendesk.com/hc/en-us/articles/7041677653914 ; https://support.zendesk.com/hc/en-us/articles/8037936748570
  - FreeScout GPT (overwrite storage) — https://github.com/verygoodplugins/freescout-gpt-assistant
  - Zammad Smart Assist (human-in-the-loop) — https://github.com/zammad/zammad/issues/5599
  - Intercom Inbox AI — https://www.intercom.com/help/en/articles/6955446-ai-features-available-in-the-inbox
- attune code anchors: `internal/service/enrich/enricher.go:247` (sole enrich LLM
  call), `internal/repo/embedding/task.go` (outbox to generalize),
  `internal/service/embedding/worker.go` (worker paradigm), `cmd/attune/server.go`
  `startEmbeddingWorker` (wiring), `internal/repo/llmaudit` (`purpose` cost table),
  migration `025_embedding_clustering.sql` (per-tenant flag precedent).
- Issue #26; CLAUDE.md §6 (abstraction), §7 (observability), §10 (proposals),
  §11 (proto IDL).
