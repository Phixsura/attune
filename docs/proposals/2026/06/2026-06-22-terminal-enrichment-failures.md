# Surface Terminal Enrichment Failures

| | |
|---|---|
| **Issue** | #81 |
| **Status** | Implemented |
| **Started** | 2026-06-22 |
| **Related** | #48 (observability — metric already landed), #66 (channel-agnostic inbound — enrichment pipeline), #93 (MCP server — potential tool exposure) |

---

## Problem

attune's enrichment pipeline has robust retry mechanics: exponential backoff, bounded attempts (max 5), and automatic scheduling. When a feedback row exhausts its retry budget, it remains in `enrichment_status='failed'` with `enrichment_next_retry_at=NULL` — a **terminal failure**.

### Current gaps

1. **Operator blind spot** — Console has no way to filter for terminal failures. Operators must write SQL to find rows where `enrichment_status='failed' AND enrichment_attempts >= 5 AND enrichment_next_retry_at IS NULL`.

2. **Missing metadata in UI** — `enrichment_attempts` and `enrichment_next_retry_at` are not exposed to Console list or detail views. Operators cannot see how many attempts were made or when the next retry is scheduled.

3. **No recovery path** — Once a row reaches terminal failure, there is no Console/API action to retry it. The only option is direct database UPDATE.

4. **Implicit terminal semantics** — The distinction between "failed and will retry" vs "failed permanently" is encoded in a compound condition, not a first-class concept.

### Impact

- **Ops friction** — Debugging enrichment failures requires SQL access; non-technical operators are blocked.
- **Data quality risk** — Terminal failures accumulate silently; feedback that could be enriched after a fix deployment stays stuck.
- **Audit gap** — No Console-level visibility into retry history or terminal state.

---

## Goals

| Category | Goal |
|----------|------|
| **Discoverability** | Console list view supports filtering by `enrichment_status` and `terminal_failed_only`. |
| | Detail view shows `enrichment_attempts`, `enrichment_next_retry_at`, and computed `is_terminal_failed`. |
| **Recovery** | Console/API supports manual retry of terminal failures (reset attempts, schedule immediate retry). |
| **Observability** | Existing metric `attune_enrichment_terminal_failures_total{tenant}` confirmed and documented. |
| **Semantics** | Document the "failed + attempt cap" model; no new `dead` status column. |
| **Tests** | Integration tests verify below-cap retry, at-cap terminal, and manual retry behavior. |

---

## Non-goals

| Scope | Rationale |
|-------|-----------|
| **Bulk retry API** | v0.6 scope is single-row retry; bulk retry ("retry all terminal failures") is a follow-up. |
| **Retry history table** | Industry survey found structured per-attempt history is uncommon; current `enrichment_attempts` counter is sufficient. |
| **Automatic retry on code deploy** | Requires deployment hooks; out of scope. |
| **New `dead` status** | The existing `failed` status with compound condition is clearer than introducing a fourth status. |
| **MCP tool exposure** | `retry_enrichment` MCP tool can be added later via #93 registry. |

---

## Industry survey

Research covered 10 systems: Sidekiq, Celery, BullMQ, Temporal, Airflow, Kafka Connect, AWS SQS, RabbitMQ, Hangfire, Resque.

### Key patterns adopted

| Pattern | Industry examples | attune adoption |
|---------|-------------------|-----------------|
| **Dedicated terminal state API** | Sidekiq `DeadSet`, BullMQ `getFailed()` | `enrichment_status` filter + `terminal_failed_only` |
| **Attempt count + error in list view** | Hangfire, BullMQ | Add `enrichment_attempts` to list/detail |
| **Manual retry API** | All 10 systems | `POST /fb/v1/console/feedback/{id}/retry-enrichment` |
| **Retry resets attempt counter** | BullMQ `resetAttemptsMade` | Reset `enrichment_attempts=0`, set `next_retry_at=NOW()` |
| **Terminal = failed + exhausted** | Celery `FAILURE`, Airflow `failed` | Keep `failed` status, derive terminal from compound condition |

### Patterns not adopted (with rationale)

| Pattern | Why not |
|---------|---------|
| **Structured retry history array** | Only RabbitMQ `x-death[]` has this; most systems use a counter. Overkill for v0.6. |
| **SQL-like query language** | Temporal's Visibility is powerful but complex. Simple enum filter is sufficient for enrichment status. |
| **Bulk retry with rate limiting** | Sidekiq/BullMQ support this, but adds API complexity. Single-row retry first. |
| **Poison pill detection** | Kafka/BlazingMQ have auto-detection; attune's 5-attempt cap already handles this. |

---

## Proposal

### 1. Proto changes (additive)

```protobuf
// ingest.proto — ListFeedbackRequest
message ListFeedbackRequest {
  // existing fields...
  
  // Filter by enrichment status: "pending" | "enriching" | "done" | "failed"
  optional string enrichment_status = 9;
  // If true, only return rows where enrichment_status='failed' AND
  // enrichment_attempts >= maxEnrichmentAttempts. Combines with enrichment_status
  // if both set (AND logic).
  optional bool terminal_failed_only = 10;
}

// ingest.proto — Feedback (list row)
message Feedback {
  // existing fields 1-17...
  
  // Number of enrichment attempts made (0 = never attempted)
  optional int32 enrichment_attempts = 18;
  // RFC3339 timestamp of next scheduled retry; absent if terminal or not failed
  optional string enrichment_next_retry_at = 19;
}

// ingest.proto — FeedbackDetail
message FeedbackDetail {
  // existing fields 1-27...
  
  // Number of enrichment attempts made
  optional int32 enrichment_attempts = 28;
  // RFC3339 timestamp of next scheduled retry; absent if terminal or not failed
  optional string enrichment_next_retry_at = 29;
}
```

### 2. Repo layer changes

```go
// feedback_console.go — ConsoleListOpts
type ConsoleListOpts struct {
    // existing fields...
    
    EnrichmentStatus   *string // "pending" | "enriching" | "done" | "failed"
    TerminalFailedOnly *bool   // true = failed AND attempts >= 5 AND next_retry_at IS NULL
}

// feedback_console.go — ConsoleListRow
type ConsoleListRow struct {
    // existing fields...
    
    EnrichmentAttempts    int        // 0-5
    EnrichmentNextRetryAt *time.Time // nil if terminal or not failed
}

// feedback_console.go — ConsoleDetailRow
type ConsoleDetailRow struct {
    ConsoleListRow
    // existing fields...
    
    // EnrichmentAttempts and EnrichmentNextRetryAt inherited from ConsoleListRow
}
```

SQL changes in `ListForConsole` and `GetForConsole`:
- Add `enrichment_attempts`, `enrichment_next_retry_at` to SELECT
- Add WHERE clause for `enrichment_status` filter
- Add WHERE clause for terminal-only filter:
  ```sql
  AND enrichment_status = 'failed'
  AND enrichment_attempts >= 5
  AND enrichment_next_retry_at IS NULL
  ```

### 3. Manual retry API

**Endpoint:** `POST /fb/v1/console/feedback/{id}/retry-enrichment`

**Request:** Empty body (id in path)

**Response:**
```protobuf
message RetryEnrichmentResponse {
  int64 id = 1;
  string enrichment_status = 2;  // will be "failed" (sweeper picks it up)
  int32 enrichment_attempts = 3; // will be 0
  string enrichment_next_retry_at = 4; // will be NOW()
}
```

**Behavior:**
1. Verify row exists and belongs to tenant
2. Verify `enrichment_status = 'failed'` (reject if pending/enriching/done)
3. Execute:
   ```sql
   UPDATE user_feedback
   SET enrichment_attempts = 0,
       enrichment_next_retry_at = NOW(),
       enrichment_error = NULL
   WHERE id = $1 AND tenant_id = $2
   RETURNING id, enrichment_status, enrichment_attempts, enrichment_next_retry_at
   ```
4. Audit log: `action='retry_enrichment'`, `actor_type='console_user'`
5. Sweeper's next `ListPending()` will pick up the row

**Error cases:**
- 404: Row not found or wrong tenant
- 409: Row not in `failed` status (code `INVALID_STATE`)

### 4. Handler wiring

```go
// handlers/console/feedback/feedback_retry.go
func (h *Handler) RetryEnrichment(w http.ResponseWriter, r *http.Request) {
    // Extract id from path, tenant from session
    // Call repo.RetryEnrichment(ctx, tenantID, id)
    // Write audit log
    // Return proto response
}

// router.go
r.Post("/fb/v1/console/feedback/{id}/retry-enrichment", h.Feedback.RetryEnrichment)
```

### 5. Repo method for retry

```go
// feedback.go
func (r *FeedbackRepo) RetryEnrichment(ctx context.Context, tenantID string, id int64) (*RetryResult, error) {
    var result RetryResult
    err := r.pool.QueryRow(ctx, `
        UPDATE user_feedback
        SET enrichment_attempts = 0,
            enrichment_next_retry_at = NOW(),
            enrichment_error = NULL
        WHERE id = $1 AND tenant_id = $2 AND enrichment_status = 'failed'
        RETURNING id, enrichment_status, enrichment_attempts, enrichment_next_retry_at
    `, id, tenantID).Scan(&result.ID, &result.Status, &result.Attempts, &result.NextRetryAt)
    
    if errors.Is(err, pgx.ErrNoRows) {
        // Could be: not found, wrong tenant, or not in failed status
        // Check which case for proper error
        ...
    }
    return &result, nil
}
```

### 6. File layout

```
internal/
├── repo/feedback/
│   ├── feedback.go           # add RetryEnrichment method
│   └── feedback_console.go   # add filters, add fields to rows
├── handlers/console/feedback/
│   ├── feedback.go           # existing list handler (add filter params)
│   ├── feedback_get.go       # existing detail handler (add fields)
│   └── feedback_retry.go     # NEW: retry-enrichment handler
proto/attune/v1/
└── ingest.proto              # add fields + new RPC
```

---

## Alternatives considered

### A. Introduce `dead` status

**Rejected.** Would require:
- New enum value in proto
- Migration to update existing terminal rows
- Two ways to identify terminal failure (status='dead' OR the compound condition)

The compound condition (`failed` + `attempts >= 5` + `next_retry_at IS NULL`) is already well-defined and used by `ListPending`. Adding a separate status creates ambiguity.

### B. Expose retry history as JSON array

**Rejected.** Industry survey found only RabbitMQ has structured per-attempt history (`x-death[]`). Most systems (Sidekiq, BullMQ, Hangfire, Celery) use a simple counter. The counter is sufficient for "how many times did we try?" without the storage/complexity overhead.

### C. Bulk retry API

**Deferred.** Single-row retry is the MVP. Bulk retry requires:
- Rate limiting (BullMQ caps at `count` parameter)
- Confirmation UI ("Are you sure you want to retry 847 rows?")
- Background job pattern (async with progress)

Can be added in a follow-up.

### D. Auto-retry on deployment

**Deferred.** Would require:
- Deployment hook integration
- "Retry all terminal failures created before this deploy" logic
- Risk of retry storms after a deploy

Out of scope for this issue.

---

## Risks / tradeoffs

| Risk | Mitigation |
|------|------------|
| **Retry storms** | Single-row API only; bulk retry deferred. Sweeper's `ListPending()` has built-in batch limits. |
| **Stale terminal rows** | Operators must actively query and retry. Consider a scheduled "terminal failure digest" email (future). |
| **Proto field bloat** | Only 4 new optional fields; well within acceptable growth. |
| **UI complexity** | Filter adds one dropdown; detail view adds one section. Minimal UX impact. |

---

## Implementation plan

### Phase 1: Repo + Proto (1 PR)

1. Add `enrichment_attempts`, `enrichment_next_retry_at` to `ConsoleListRow`, `ConsoleDetailRow`
2. Add `EnrichmentStatus`, `TerminalFailedOnly` to `ConsoleListOpts`
3. Update `ListForConsole`, `GetForConsole` SQL
4. Add proto fields to `Feedback`, `FeedbackDetail`, `ListFeedbackRequest`
5. Run `make proto`
6. Integration tests for new filters

### Phase 2: Retry API (1 PR)

1. Add `RetryEnrichment` repo method
2. Add proto RPC `RetryEnrichment`
3. Add handler `feedback_retry.go`
4. Wire route
5. Add audit log entry
6. Integration tests for retry behavior

### Phase 3: Console UI (separate, may be done by frontend)

1. Add filter dropdown for enrichment status
2. Add "Terminal failures only" checkbox
3. Display attempts + next_retry_at in list/detail
4. Add "Retry" button on detail view for terminal failures

### Phase 4: Documentation

1. Update CHANGELOG.md
2. Update API docs (auto-generated from proto)
3. Add runbook entry for "investigating terminal enrichment failures"

---

## Verification

### Unit tests

- `ConsoleListOpts` filter logic (status filter, terminal-only filter)
- `RetryEnrichment` state transitions

### Integration tests (PostgreSQL)

| Test case | Assertion |
|-----------|-----------|
| Row with `attempts=3` and `next_retry_at` in future | Listed with `enrichment_status='failed'` filter, NOT listed with `terminal_failed_only=true` |
| Row with `attempts=5` and `next_retry_at=NULL` | Listed with both filters |
| `RetryEnrichment` on terminal row | `attempts` reset to 0, `next_retry_at` set to NOW, row picked up by `ListPending` |
| `RetryEnrichment` on `status='done'` row | Returns 409 INVALID_STATE |
| `RetryEnrichment` on non-existent row | Returns 404 |

### Manual verification

1. Ingest a feedback row
2. Mock LLM to always fail
3. Wait for 5 retry cycles (use short backoff in test config)
4. Verify row is terminal in Console
5. Click "Retry" button
6. Verify row is picked up and re-enriched

---

## References

### Industry sources (from deep research)

- [Sidekiq API Wiki — DeadSet](https://github.com/sidekiq/sidekiq/wiki/API)
- [BullMQ — Retrying Failing Jobs](https://docs.bullmq.io/guide/retrying-failing-jobs)
- [BullMQ API — Queue.retryJobs()](https://api.docs.bullmq.io/classes/v4.Queue.html)
- [Temporal — Visibility](https://docs.temporal.io/visibility)
- [Temporal — Detecting Workflow Failures](https://docs.temporal.io/encyclopedia/detecting-workflow-failures)
- [Airflow — Rerunning DAGs](https://www.astronomer.io/docs/learn/rerunning-dags)
- [AWS SQS — DLQ Redrive APIs](https://aws.amazon.com/blogs/aws/a-new-set-of-apis-for-amazon-sqs-dead-letter-queue-redrive/)
- [RabbitMQ — Dead Letter Exchanges](https://www.rabbitmq.com/docs/dlx)
- [Hangfire — Dealing with Exceptions](https://docs.hangfire.io/en/latest/background-processing/dealing-with-exceptions.html)
- [Resque — Failure Backends](https://github.com/resque/resque/wiki/Failure-Backends)
- [Kafka Connect — Error Handling and Dead Letter Queues](https://www.confluent.io/blog/kafka-connect-deep-dive-error-handling-dead-letter-queues/)

### attune internal

- [#81 Issue](https://github.com/Phixsura/attune/issues/81) — this proposal
- [#48 Observability](https://github.com/Phixsura/attune/issues/48) — `attune_enrichment_terminal_failures_total` metric
- [`internal/repo/feedback/feedback.go`](../../internal/repo/feedback/feedback.go) — `MarkFailed`, `ListPending`
- [`internal/service/enrich/enricher_helpers.go`](../../internal/service/enrich/enricher_helpers.go) — `markFailed` helper
