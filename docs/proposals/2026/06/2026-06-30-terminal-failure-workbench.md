# Terminal Failure Workbench

| | |
|---|---|
| **Issue** | #159 |
| **Status** | Implemented |
| **Started** | 2026-06-30T10:42:31+08:00 |
| **Related** | #81 (terminal failure visibility base), #48 (terminal failure observability metric) |

---

## Problem

`main` already ships the row-level terminal-failure primitives:

- list/detail can filter `terminal_failed_only`
- row detail exposes `enrichment_status`, `enrichment_attempts`, and `enrichment_next_retry_at`
- `RetryEnrichmentResponse` and the retry action already exist
- `attune_enrichment_terminal_failures_total{tenant}` is already wired
- Console already has a terminal failure entry in the feedback surface

Issue #159 is not about inventing terminal failures from scratch. The remaining gap is a cluster-level workbench that lets an operator answer, without SQL:

1. Which failure class dominates?
2. Which model/channel snapshot is implicated?
3. Which tenant config snapshot is common across the cluster?
4. How old is the cluster?
5. Which rows should be retried or investigated together?

The generic feedback stats surface is not enough for this. It reports monthly ingest and dimension summaries, but it does not provide terminal-failure clustering.

## Goals / Non-goals

| Category | Goal |
|---|---|
| **Discoverability** | Add a terminal-failure workbench entry inside the existing feedback area that opens directly on terminal rows. |
| **Explainability** | Group terminal rows by bounded, failure-time snapshot keys instead of raw exception text. |
| **Remediation** | Reuse the existing audited retry path for single-row and selected-batch retries, and expose navigation-only links to the relevant config or evidence surface. |
| **Safety** | Keep terminal semantics as `failed + exhausted + no next retry`; do not introduce a new `dead` status or mutate the retry policy. |
| **Observability** | Publish bounded terminal-failure counters that can drive dashboards without raw error labels or unbounded config labels. |
| **Tests** | Cover the normalization helpers, summary aggregation, retry drill-down, and workbench UI flows. |

| Scope | Non-goal |
|---|---|
| **Retry policy changes** | Do not change the retry budget, backoff algorithm, or sweeper semantics. |
| **New terminal state** | Do not add a separate `dead` or `terminal` persistence status. |
| **Full attempt history** | Do not add an append-only attempt history table. |
| **Automatic repair** | Do not auto-edit tenant config or auto-retry all failures on deploy. Remediation stays operator-driven. |
| **Unbounded metrics** | Do not emit raw error messages as metric labels. |

## Proposal

### 1. Keep the existing row-level behavior and add a cluster view on top

The workbench should live inside the existing feedback experience, not as a separate product area. It starts from the terminal slice that already exists in `main` and adds a cluster view above the row list.

The new view should show:

- total terminal failures in the active window
- oldest terminal row age
- top clusters for each dimension
- a drill-down path back to the existing detail sheet and retry controls

The view is an operator workbench, not a new lifecycle state. Row-level list/detail/retry remain the source of truth for individual records.

### 2. Add a dedicated terminal summary RPC

Add a dedicated summary RPC for terminal failures instead of extending the generic feedback stats surface.

Rationale:

- `GetFeedbackStats` is monthly and generic
- the workbench needs a different shape: cluster counts, samples, and remediation hints
- keeping the endpoint dedicated avoids coupling terminal clustering to unrelated ingest or dimension stats

Request/response contract:

- request is tenant-scoped through auth and uses the same time window as the existing stats surface by default: current calendar month UTC
- response includes `period_start`, `period_end`, `total_terminal_failures`, `oldest_created_at`, and a bounded list of cluster summaries
- each cluster summary includes:
  - `dimension` (`reason_class`, `model_channel`, `tenant_config_fingerprint`, or `age_bucket`)
  - `key`
  - `count`
  - `sample_feedback_ids` with a hard cap of 3
  - `remediation_hint` when the backend can derive one safely

Boundedness:

- cap each dimension to top 10 clusters
- order by `count desc`, then oldest `created_at` for tie-breaking
- return sample row IDs only, not sample payloads
- compute the summary server-side over terminal rows only

### 3. Group by stable, failure-time metadata

The workbench must group rows using a failure-time snapshot, not the live tenant config.

Dimension definitions:

| Dimension | Definition | Values / rules |
|---|---|---|
| `reason_class` | Stable error class derived from the existing enrich error wrappers and parser. | Start with `llm_err`, `parse_err`, and `other_err`. Fallback to `other_err` when the chain is unclear. |
| `model_channel` | Canonical route or channel key captured in the enrich snapshot. | Group by the normalized route key, not by raw provider text or per-row error text. |
| `tenant_config_fingerprint` | Deterministic hash of the terminal row's enrichment snapshot. | Hash the stored prompt-policy snapshot using canonical JSON; include fields that affect classification, such as policy version, prompt version, prompt fingerprint, schema fingerprint, mode, prompt source, template language, and normalized policy config. |
| `age_bucket` | Coarse age of the terminal row at query time. | Use `now - created_at` and bucket into `0-1h`, `1-24h`, `1-7d`, and `7d+`. |

Why this shape:

- it keeps grouping stable across config edits
- it matches the failure metadata that already exists in the enrich path
- it stays bounded enough for SQL and UI rendering
- it avoids the trap of grouping on raw exception strings

### 4. Keep remediation explicit and auditable

The workbench should support the same retry actions that already exist, but present them in a cluster-first workflow.

Operator flow:

1. inspect the cluster summary
2. drill into a sample row or cluster slice
3. choose a retry action for one row or the selected set
4. optionally jump to the relevant config or audit surface

Remediation links are navigation-only. They can point to:

- the tenant enrichment config surface
- the prompt-policy or routing surface that produced the failure-time snapshot
- the audit log or feedback audit trail for evidence review

State-changing actions must continue to go through the existing audited retry path. The workbench does not add an automatic repair loop and does not bypass the current retry policy.

### 5. Keep observability bounded

Keep the existing tenant-only counter:

- `attune_enrichment_terminal_failures_total{tenant}`

Add one bounded breakdown counter for dashboards:

- `attune_enrichment_terminal_failures_by_reason_total{tenant, reason_class}`

Do not add `model_channel`, fingerprints, or raw error text as Prometheus labels. Those dimensions belong in the summary RPC and the UI, not in metric labels.

Age distribution stays in the summary RPC and the workbench UI. It should not become a time-series label.

### 6. Preserve current terminal semantics

The workbench is a read and remediate layer on top of the existing terminal definition.

Terminal remains:

- `enrichment_status = 'failed'`
- attempts exhausted
- no next retry scheduled

No new persisted status is introduced, and existing list/detail/retry paths keep their current meaning.

## Alternatives considered

### A. Extend the generic feedback stats endpoint

Rejected. That endpoint already serves monthly ingest and dimension summaries. Terminal clustering needs a different shape and a stricter bounded-response contract.

### B. Group by raw error text

Rejected. Raw error text is unstable, noisy, and too high-cardinality for both UI grouping and metrics.

### C. Group by live tenant config

Rejected. Live config changes would re-bucket historical failures and make the workbench drift over time. The failure-time snapshot is the stable source of truth.

### D. Add model/channel to Prometheus labels

Rejected. That would create a cardinality risk and turn operational detail into a metrics-design problem. Keep it in SQL / RPC responses instead.

### E. Add a new terminal state

Rejected. The derived terminal condition already exists and is sufficient. A new state would duplicate meaning and complicate compatibility.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| **Reason classes may be too coarse** | Keep the raw error visible in the existing detail view and expand the taxonomy only when a stable wrapper exists. |
| **Config fingerprints may be hard to read** | Show the human-readable config summary next to the fingerprint and link back to the config surface. |
| **Summary queries may get expensive** | Query only terminal rows, cap the window, and return top N clusters with fixed sample limits. |
| **Batch retry may cause retry storms** | Reuse the existing retry policy, keep the action explicit, and preserve audit logging. |
| **Metric cardinality may grow** | Keep Prometheus on tenant + reason class only; put the richer grouping in the summary RPC. |

## Implementation plan

### Phase 1: server summary shape

1. Add a terminal-failure summary query in the feedback repo.
2. Add a dedicated console handler / RPC for the workbench summary.
3. Implement the failure-time grouping helpers for reason class, route/channel, config fingerprint, and age bucket.

### Phase 2: console workbench

1. Add the terminal workbench entry in the feedback area.
2. Render the summary clusters and sample drill-downs.
3. Reuse the existing detail sheet and retry controls.
4. Add navigation-only remediation links to the matching config and audit surfaces.

### Phase 3: observability and safety

1. Keep `attune_enrichment_terminal_failures_total{tenant}` intact.
2. Add the bounded reason-class counter.
3. Verify the summary query does not expose raw error strings or unbounded config data.

### Phase 4: tests

1. Add unit tests for fingerprint canonicalization and reason-class normalization.
2. Add handler tests for the summary RPC and retry drill-down.
3. Add Console tests for cluster rendering, sample drill-down, and retry flow.

## Verification

- `go test ./internal/repo/feedback ./internal/handlers/console/feedback ./internal/service/enrich`
- `go test -race ./internal/...` for the changed Go packages
- `pnpm tsc -b --noEmit`
- `pnpm biome check`
- `pnpm vitest run` for the affected Console feature tests
- `make proto` if the workbench introduces a new proto surface
- `make test-integration` for PostgreSQL-backed feedback integration coverage

## References

### Internal

- [Issue #159](https://github.com/Phixsura/attune/issues/159)
- [Proposal #81: Surface Terminal Enrichment Failures](./2026-06-22-terminal-enrichment-failures.md)
- [`internal/handlers/console/feedback/feedback_list.go`](../../../../internal/handlers/console/feedback/feedback_list.go)
- [`internal/handlers/console/feedback/feedback_stats.go`](../../../../internal/handlers/console/feedback/feedback_stats.go)
- [`internal/infra/metrics/metrics.go`](../../../../internal/infra/metrics/metrics.go)
- [`internal/service/enrich/enricher.go`](../../../../internal/service/enrich/enricher.go)
- [`internal/service/enrich/prompt_policy.go`](../../../../internal/service/enrich/prompt_policy.go)
- [`internal/repo/feedback/feedback.go`](../../../../internal/repo/feedback/feedback.go)
- [`console/src/features/feedback/components/feedback-page.tsx`](../../../../console/src/features/feedback/components/feedback-page.tsx)
- [`console/src/features/feedback/components/detail-sheet.tsx`](../../../../console/src/features/feedback/components/detail-sheet.tsx)

### External research

- [Sidekiq Error Handling](https://github.com/sidekiq/sidekiq/wiki/Error-Handling)
- [BullMQ: Retrying failing jobs](https://docs.bullmq.io/guide/retrying-failing-jobs)
- [BullMQ: Retrying jobs](https://docs.bullmq.io/guide/jobs/retrying-job)
- [Celery Tasks](https://docs.celeryq.dev/en/stable/userguide/tasks.html)
- [Temporal Retry Policies](https://docs.temporal.io/encyclopedia/retry-policies)
- [Temporal Web UI](https://docs.temporal.io/web-ui)
- [Airflow Tasks](https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/tasks.html)
- [Kafka Connect configs](https://kafka.apache.org/41/configuration/kafka-connect-configs/)
- [AWS SQS dead-letter queues](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-dead-letter-queues.html)
- [RabbitMQ DLX](https://www.rabbitmq.com/docs/dlx)
- [Hangfire exceptions](https://docs.hangfire.io/en/latest/background-processing/dealing-with-exceptions.html)
- [Resque README](https://github.com/resque/resque)
