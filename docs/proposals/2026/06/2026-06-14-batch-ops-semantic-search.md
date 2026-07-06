# Batch operations and semantic search for feedback

| | |
|---|---|
| **Issue** | #30 |
| **Status** | Implemented |
| **Started** | 2026-06-14 CST |
| **Related** | #25 (embedding clustering — provides vector infrastructure), #28/#117 (tags — existing batch pattern), #29/#118 (workflow status — existing batch transition pattern), #19 (proto IDL contract) |

## Problem

Two Console UX gaps limit operator efficiency at scale:

1. **No unified batch operations** — Tag batch and workflow batch exist separately,
   but there's no way to:
   - Delete feedback in bulk
   - Apply multiple operations atomically
   - Operate on "all matching current filter" without selecting IDs manually
   - Guarantee idempotency on retry

2. **No semantic search** — Operators cannot find "all feedback about checkout
   failures" without reading every row. The embedding infrastructure from #25
   exists but is only used for clustering, not for search.

Current state:
- `POST /console/feedback/tags/batch` — tag add/remove, max 100 IDs
- `POST /console/feedback/transition/batch` — workflow transition, max 100 IDs
- No delete batch
- No idempotency tokens
- No query-based batch ("update all matching filter")
- No semantic search endpoint

## Industry benchmarking

Researched **10 top-tier products** across issue tracking, customer support,
search infrastructure, and vector databases.

### Batch operation patterns

| Product | Batch endpoint | Max items | Execution | Partial failure | Idempotency |
|---------|---------------|-----------|-----------|-----------------|-------------|
| **Zendesk** | `PUT /tickets/update_many` | 100 | Async job + poll | Per-item `success`/`error` in results array | `Idempotency-Key` header, 2h TTL |
| **Jira** | `POST /bulk/issues/*` | 1000 | Async taskId + poll | Per-item errors in task result | None |
| **Sentry** | `PUT /issues/` | ~1000 | Sync | Silent skip for invalid IDs, 204 for no match | None |
| **Linear** | `issueBatchUpdate` GraphQL | Unlimited | Sync | GraphQL errors array | None (client-side CRDT) |
| **GitHub** | None native | N/A | Loop client-side | N/A | N/A |
| **Intercom** | None native | N/A | Loop client-side | N/A | N/A |

#### Key patterns adopted

**1. Dual-mode batch (Sentry/Zendesk pattern)**

```bash
# Mode A: Explicit IDs
PUT /issues/?id=123&id=456&id=789
# Mode B: Query-based (update all matching)
PUT /issues/?query=is:unresolved+level:error
```

When `id` is omitted, the operation applies to **all rows matching the query**.
This solves "select all 500 matching filter" without sending 500 IDs.

**2. Async job model (Zendesk/Jira)**

For large batches, return a `job_id` immediately and poll for results:

```json
{
  "job_status": {
    "id": "abc123",
    "status": "completed",
    "total": 100,
    "progress": 100,
    "results": [
      {"id": 123, "success": true},
      {"id": 456, "success": false, "error": "UpdateConflict"}
    ]
  }
}
```

**3. Optimistic locking (Zendesk)**

```json
{
  "safe_update": true,
  "updated_stamp": "2026-06-14T10:00:00Z"
}
```

Detects concurrent modifications. If the row was updated after `updated_stamp`,
returns `UpdateConflict` instead of blindly overwriting.

**4. Idempotency keys (Stripe standard)**

- Client generates UUID, passes in header
- Server caches response (including failures) for 24h
- Same key + different params = 409 Conflict (detected via request hash)
- Enables safe retry without double-apply

**5. Job cancellation and heartbeat**

Long-running jobs need:
- Heartbeat mechanism to detect stuck workers
- Cancel endpoint for user-initiated abort
- `Retry-After` header for polling guidance

### Semantic search patterns

| Product | Technology | Public API | Hybrid search |
|---------|------------|------------|---------------|
| **Linear** | Vector embeddings + keywords (2025.04) | Yes (`issueSearch`) | Yes (default) |
| **Algolia** | NeuralSearch (neural hashing) | Yes | Yes (default) |
| **Notion** | Turbopuffer + RAG | **No** (title-only API) | Internal only |
| **GitHub** | Copilot semantic (2026.05) | **No** (Chat UI only) | Internal only |
| **Jira** | Rovo AI | **No** (UI only) | Internal only |
| **Zendesk** | Keywords + AI triage | Keyword only | No |

#### Key patterns adopted

**1. Hybrid search (Algolia/Linear pattern)**

Combine keyword score + semantic score:

```
final_score = keyword_weight * keyword_score + semantic_weight * semantic_score
```

When user searches "checkout problems", return:
- Exact matches for "checkout" (keyword)
- Semantic matches for "payment failed", "cart error" (embedding similarity)

**2. Model consistency (pgvector best practice)**

> "Cross-model comparison is garbage; similarity search filters to same model"

Different embedding models produce incompatible vectors. Query must filter:
```sql
WHERE embedding_model = $current_model
```

**3. Graceful degradation with keyword fallback**

When semantic search returns 0 results or embedding API fails, fall back to
PostgreSQL full-text search (`tsvector`). The UI indicates which results are
semantic vs keyword.

**4. pgvector performance**

| Rows | Index | Expected latency |
|------|-------|------------------|
| 100k | HNSW | < 10ms (pure vector) |
| 100k | HNSW + filters | < 50ms (with tenant/tag/workflow filters) |

With `hnsw.ef_search = 100` (up from default 40) for better recall. Use CTE
pattern to force HNSW scan before applying selective filters.

### Idempotency deep dive

| Provider | Mechanism | Scope | TTL | Caches errors |
|----------|-----------|-------|-----|---------------|
| **Stripe** | `Idempotency-Key` header | Per-request | 24h | Yes |
| **AWS** | `ClientToken` param | Per-request | Varies | Yes |
| **Google** | `request_id` param (AIP-155) | Per-request | Unspecified | Yes |
| **Zendesk** | `Idempotency-Key` header | Per-request | 2h | Yes |

**Request hash for conflict detection:**
Store SHA-256 hash of canonicalized request params. On retry, compare hash —
different hash = 409 Conflict. Prevents misuse of stale idempotency keys.

**Composite keys for batch** (for future per-item retry):
```
{tenant_id}:{batch_request_id}:{item_index}
```

## Proposal

### Data model changes

Migration `031_batch_ops.sql`:

```sql
-- 1. Add updated_at and deleted_at to user_feedback
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- Trigger for auto-updating updated_at
CREATE OR REPLACE FUNCTION update_user_feedback_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_user_feedback_updated_at ON user_feedback;
CREATE TRIGGER trg_user_feedback_updated_at
    BEFORE UPDATE ON user_feedback
    FOR EACH ROW
    EXECUTE FUNCTION update_user_feedback_updated_at();

-- Index for soft-deleted filtering
CREATE INDEX IF NOT EXISTS idx_user_feedback_deleted
    ON user_feedback (tenant_id, deleted_at)
    WHERE deleted_at IS NOT NULL;

-- Index for semantic search with workflow filter
CREATE INDEX IF NOT EXISTS idx_uf_embedding_workflow
    ON user_feedback (tenant_id, workflow_state_id, embedding_model)
    WHERE embedding IS NOT NULL;

-- 2. Idempotency keys table
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key          TEXT NOT NULL,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    request_hash BYTEA NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    response_code INTEGER,
    response_body JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours',
    
    PRIMARY KEY (tenant_id, key),
    CHECK (status IN ('pending', 'completed', 'failed')),
    CHECK (length(key) BETWEEN 8 AND 64),
    CHECK (key ~ '^[a-zA-Z0-9_-]+$')
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires
    ON idempotency_keys (expires_at);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_tenant_count
    ON idempotency_keys (tenant_id, created_at);

-- 3. Batch jobs table (async execution)
CREATE TABLE IF NOT EXISTS batch_jobs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'queued',
    request        JSONB NOT NULL,
    total          INTEGER NOT NULL DEFAULT 0,
    progress       INTEGER NOT NULL DEFAULT 0,
    result         JSONB,
    error          TEXT,
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    claimed_at     TIMESTAMPTZ,
    last_heartbeat TIMESTAMPTZ,
    
    CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_pending
    ON batch_jobs (status, created_at)
    WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant
    ON batch_jobs (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_heartbeat
    ON batch_jobs (last_heartbeat)
    WHERE status = 'running';

-- 4. Query embedding cache table
CREATE TABLE IF NOT EXISTS query_embedding_cache (
    cache_key    TEXT PRIMARY KEY,
    embedding    vector(256) NOT NULL,
    model        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '5 minutes'
);

CREATE INDEX IF NOT EXISTS idx_query_embed_cache_expires
    ON query_embedding_cache (expires_at);
```

### Soft delete semantics

All existing `ListForConsole` and related queries must add:
```sql
WHERE deleted_at IS NULL
```

Soft-deleted feedback:
- Hidden from list views
- Still accessible via direct ID lookup (for audit trail)
- Can be restored by admin (future feature)

Hard delete:
- Permanently removes row
- Cascades to `feedback_audit_log`, `feedback_tag_assignments`
- Admin-only operation

### API design

#### Shared filter message

Extract common filters used by both listing and batch operations:

```protobuf
// In common.proto
message FeedbackFilter {
  repeated AttrFilter attrs = 1;
  optional bool urgent = 2;
  optional string q = 3;
  optional string tag_id = 4;
  optional string workflow_state_id = 5;
  optional string workflow_category = 6;
}
```

Update `ListFeedbackRequest` to embed this message (backwards compatible via
field number preservation).

#### Unified batch endpoint

```protobuf
// POST /fb/v1/console/feedback/batch
message BatchFeedbackRequest {
  // Mode A: Explicit IDs (max 100)
  repeated int64 feedback_ids = 1;
  
  // Mode B: Query-based (when feedback_ids is empty)
  optional FeedbackFilter query = 2;
  
  // Server-side safety limit for query mode (required for delete)
  optional int32 max_affected = 3;
  
  // For delete: must match total_matched from dry_run (prevents stale-filter attacks)
  optional int32 confirm_count = 4;
  
  // Preview mode: return total_matched without executing
  bool dry_run = 5;
  
  // Operation to perform
  BatchOperation operation = 6;
  
  // Idempotency (optional, recommended)
  optional string idempotency_key = 7;
  
  // Optimistic locking (optional)
  optional string if_unmodified_since = 8;  // RFC3339 timestamp
}

message BatchOperation {
  oneof op {
    BatchTagOp tag = 1;
    BatchWorkflowOp workflow = 2;
    BatchDeleteOp delete = 3;
  }
}

message BatchTagOp {
  repeated string add_tag_ids = 1;
  repeated string remove_tag_ids = 2;
}

message BatchWorkflowOp {
  string to_state_id = 1;
  string comment = 2;
}

message BatchDeleteOp {
  // Default (false) = soft delete (archive). True = permanent delete (admin only).
  bool hard = 1;
}

message BatchFeedbackResponse {
  int32 total_matched = 1;   // rows matching query/IDs
  int32 succeeded = 2;
  int32 skipped = 3;         // optimistic lock conflicts
  repeated BatchItemFailure failed = 4;
  
  // For async mode (> 100 items via query)
  optional string job_id = 5;
  optional string job_status_url = 6;
}

message BatchItemFailure {
  int64 feedback_id = 1;
  // ErrorCode enum name: CONFLICT, INVALID_TRANSITION, NOT_FOUND, FORBIDDEN
  string code = 2;
  string message = 3;
  // For CONFLICT, enables smart retry
  optional string current_updated_at = 4;
}
```

**Validation rules:**
- If `feedback_ids` is non-empty AND `query` has any non-empty field → 400 VALIDATION error
- If `feedback_ids` is empty AND `query` is empty → 400 VALIDATION error
- If `operation.delete` is set AND `query` is used AND `confirm_count` != `total_matched` → 400 VALIDATION error
- If `operation.delete.hard` is true AND user is not admin → 403 FORBIDDEN
- Query mode with > 10,000 matches AND no `max_affected` → 400 VALIDATION error (safety limit)

**Execution modes:**
- `dry_run = true`: Return `total_matched` without executing, HTTP 200
- `feedback_ids` provided (≤ 100): Sync execution, immediate response
- `query` provided, matches ≤ 100: Sync execution
- `query` provided, matches > 100: Async job, return `job_id` for polling

**Atomicity model:**
Batch operations use **best-effort semantics** — each item is processed
independently. Items 1-49 committed before item 50 fails remain committed.
HTTP 200 returned if any item succeeded; HTTP 400 if all failed validation;
HTTP 500 for infrastructure failure. Client inspects `succeeded` + `failed`
counts to determine partial failure.

#### Semantic search endpoint

```protobuf
// POST /fb/v1/console/feedback/search
message SemanticSearchRequest {
  string q = 1;                       // natural language query
  optional int32 limit = 2;           // default 20, max 100
  optional float min_similarity = 3;  // default 0.7
  
  // Additional filters (AND with semantic results)
  optional FeedbackFilter filter = 4;
  
  // Hybrid search weights (default: semantic=0.7, keyword=0.3)
  optional float semantic_weight = 5;
  optional float keyword_weight = 6;
}

message SemanticSearchResponse {
  repeated SemanticSearchHit hits = 1;
  string embedding_model = 2;         // model used for query embedding
  int32 total_with_embeddings = 3;    // rows that could be searched
  bool used_keyword_fallback = 4;     // true if semantic failed/empty
}

message SemanticSearchHit {
  Feedback feedback = 1;
  float similarity = 2;       // 0.0 - 1.0 (semantic score)
  float keyword_score = 3;    // 0.0 - 1.0 (tsvector match score)
  string match_type = 4;      // "semantic", "keyword", "hybrid"
}
```

Using POST (not GET) because the request body can be complex with nested
`FeedbackFilter`.

**Internal flow:**
1. Validate `clustering_enabled` for tenant → 501 FEATURE_DISABLED if false
2. Check query embedding cache (key = `sha256(normalize(q) + embedding_model)`)
3. If cache miss, call LLM embedding API (with timeout)
4. If embedding API fails, set `used_keyword_fallback = true` and use tsvector
5. Execute pgvector similarity search with CTE pattern:
   ```sql
   WITH candidates AS (
     SELECT id, embedding, 1 - (embedding <=> $query_vec) AS similarity
     FROM user_feedback
     WHERE tenant_id = $tenant_id
       AND embedding IS NOT NULL
       AND embedding_model = $current_model
       AND deleted_at IS NULL
     ORDER BY embedding <=> $query_vec
     LIMIT 200  -- oversample for filter headroom
   )
   SELECT c.similarity, f.*
   FROM candidates c
   JOIN user_feedback f ON f.id = c.id
   WHERE c.similarity >= $min_similarity
     [AND f.workflow_state_id = ...  -- apply filters on small candidate set]
   ORDER BY c.similarity DESC
   LIMIT $limit;
   ```
6. If semantic returns 0 results, fall back to keyword search:
   ```sql
   SELECT *, ts_rank(to_tsvector('simple', content), plainto_tsquery('simple', $q)) AS keyword_score
   FROM user_feedback
   WHERE tenant_id = $tenant_id
     AND deleted_at IS NULL
     AND to_tsvector('simple', content) @@ plainto_tsquery('simple', $q)
   ORDER BY keyword_score DESC
   LIMIT $limit;
   ```
7. Hydrate results with tags, workflow state (batch load pattern)
8. Return with similarity scores and `match_type`

#### Job management endpoints

```protobuf
// Job status enum
enum JobStatus {
  JOB_STATUS_UNSPECIFIED = 0;
  JOB_STATUS_QUEUED = 1;
  JOB_STATUS_RUNNING = 2;
  JOB_STATUS_COMPLETED = 3;
  JOB_STATUS_FAILED = 4;
  JOB_STATUS_CANCELLED = 5;
}

// GET /fb/v1/console/feedback/jobs/{job_id}
message JobStatusResponse {
  string job_id = 1;
  JobStatus status = 2;
  int32 total = 3;
  int32 progress = 4;
  optional BatchFeedbackResponse result = 5;  // when completed
  optional string error = 6;                   // when failed
  string created_at = 7;
  optional string started_at = 8;
  optional string completed_at = 9;
  optional string updated_at = 10;             // last progress update
  int32 retry_after_seconds = 11;              // polling hint
}

// GET /fb/v1/console/feedback/jobs
message ListJobsRequest {
  optional string status = 1;  // filter by status
  optional int32 limit = 2;    // default 20
}

message ListJobsResponse {
  repeated JobStatusResponse jobs = 1;
}

// POST /fb/v1/console/feedback/jobs/{job_id}/cancel
message CancelJobRequest {
  string job_id = 1;
}

message CancelJobResponse {
  JobStatus status = 1;
  string message = 2;
}
```

**Retry-After header:** Job status responses include `Retry-After: N` HTTP
header where N starts at 1 second and increases to 5 seconds as the job runs
longer. Clients should respect this for polling.

### Error codes

New values added to `ErrorCode` enum in `proto/attune/v1/common.proto`:

```protobuf
FEATURE_DISABLED      = 47;  // Tenant-level feature not enabled (e.g., clustering_enabled)
IDEMPOTENCY_CONFLICT  = 48;  // Same idempotency key with different params
REQUEST_IN_PROGRESS   = 49;  // Idempotency key is being processed
JOB_NOT_FOUND         = 50;  // Async job ID not found
JOB_ALREADY_CANCELLED = 51;  // Job was already cancelled
```

### Backend layering

```
handlers/console/feedback/batch.go      → HTTP: unified batch endpoint
handlers/console/feedback/search.go     → HTTP: semantic search endpoint
handlers/console/feedbackjob/           → HTTP: job status, list, cancel
service/feedbackbatch/                  → BatchService: orchestrates operations
service/semanticsearch/                 → SearchService: embedding + pgvector
repo/feedback/                          → Extended with batch update/delete, similarity search
repo/idempotency/                       → Idempotency key CRUD
repo/feedbackjob/                       → Async job tracking
```

**Interface declarations:**

```go
// In service/feedbackbatch/interfaces.go
type FeedbackStore interface {
    BatchUpdateTags(ctx context.Context, tx pgx.Tx, tenantID string, ids []int64, add, remove []string) (int, error)
    BatchUpdateWorkflow(ctx context.Context, tx pgx.Tx, tenantID string, ids []int64, toStateID string) (int, error)
    BatchSoftDelete(ctx context.Context, tx pgx.Tx, tenantID string, ids []int64) (int, error)
    BatchHardDelete(ctx context.Context, tx pgx.Tx, tenantID string, ids []int64) (int, error)
    ResolveQuery(ctx context.Context, tenantID string, filter FeedbackFilter, limit int) ([]int64, error)
    GetUpdatedAt(ctx context.Context, tenantID string, ids []int64) (map[int64]time.Time, error)
}

type IdempotencyStore interface {
    Claim(ctx context.Context, tenantID, key string, requestHash []byte) error
    Get(ctx context.Context, tenantID, key string) (*IdempotencyRecord, error)
    Complete(ctx context.Context, tenantID, key string, code int, body []byte) error
    CountByTenant(ctx context.Context, tenantID string) (int, error)
}

type AuditWriter interface {
    WriteBatch(ctx context.Context, tx pgx.Tx, entries []AuditEntry) error
}

type JobStore interface {
    Create(ctx context.Context, job *BatchJob) error
    Claim(ctx context.Context, workerID string) (*BatchJob, error)
    UpdateProgress(ctx context.Context, jobID string, progress int) error
    Heartbeat(ctx context.Context, jobID string) error
    Complete(ctx context.Context, jobID string, result *BatchFeedbackResponse) error
    Fail(ctx context.Context, jobID string, err string) error
    Cancel(ctx context.Context, jobID string) error
    Get(ctx context.Context, jobID string) (*BatchJob, error)
    ListByTenant(ctx context.Context, tenantID string, status string, limit int) ([]*BatchJob, error)
    CleanupStuck(ctx context.Context, timeout time.Duration) (int, error)
}

// In service/semanticsearch/interfaces.go
type EmbeddingClient interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Model() string
}

type EmbeddingCache interface {
    Get(ctx context.Context, key string) ([]float32, bool)
    Set(ctx context.Context, key, model string, vec []float32) error
}

type FeedbackSearcher interface {
    SimilaritySearch(ctx context.Context, tenantID string, vec []float32, opts SimilarityOpts) ([]SimilarityHit, error)
    KeywordSearch(ctx context.Context, tenantID, query string, filter FeedbackFilter, limit int) ([]KeywordHit, error)
}
```

**Sentinel errors:**

```go
// In service/feedbackbatch/errors.go
var (
    ErrIdempotencyConflict     = errors.New("idempotency key params mismatch")
    ErrRequestInProgress       = errors.New("request with this idempotency key in progress")
    ErrIdempotencyQuotaExceeded = errors.New("too many active idempotency keys")
    ErrOptimisticLockConflict  = errors.New("feedback modified since selection")
    ErrQueryTooLarge           = errors.New("query matches too many rows")
    ErrConfirmCountMismatch    = errors.New("confirm_count does not match total_matched")
    ErrJobNotFound             = errors.New("batch job not found")
    ErrJobAlreadyCancelled     = errors.New("batch job already cancelled")
)

// In service/semanticsearch/errors.go
var (
    ErrClusteringDisabled = errors.New("semantic search requires clustering_enabled")
    ErrEmbeddingFailed    = errors.New("failed to generate query embedding")
)
```

### Async job execution

**Worker pool:**
- 4-8 workers per instance (configurable via `--batch-workers`)
- Each worker polls job queue, processes in 1000-item chunks
- Heartbeat every 30 seconds while processing
- Jobs with `status=running` and `last_heartbeat > 5 minutes ago` are marked stuck

**Job lifecycle:**
```
queued → running → completed
              ↘ failed
              ↘ cancelled
```

**Startup recovery:**
On server start, scan for stuck jobs:
```sql
UPDATE batch_jobs
SET status = 'failed', error = 'worker timeout', completed_at = NOW()
WHERE status = 'running'
  AND last_heartbeat < NOW() - INTERVAL '5 minutes'
RETURNING id;
```

**Concurrent job limits:**
- Max 5 running jobs per tenant (matches Jira pattern)
- 6th job returns 429 with `Retry-After: 60`
- Jobs retained 24h, then purged by cleanup worker

**Concurrent job overlap handling:**
If two jobs operate on overlapping feedback IDs, last-write-wins semantics
apply. This is documented as known limitation. Each item is processed
independently with its own `FOR UPDATE` lock, so no data corruption occurs,
but the final state depends on execution order.

### Optimistic locking

When `if_unmodified_since` is provided:

```sql
UPDATE user_feedback
SET workflow_state_id = $new_state, updated_at = NOW()
WHERE id = $id
  AND tenant_id = $tenant_id
  AND updated_at = $if_unmodified_since  -- exact match, not <=
RETURNING id;
```

If no rows returned, the feedback was modified → report as `CONFLICT` in
failures array with the current `updated_at` value from a follow-up SELECT.

### Idempotency implementation

```go
func (s *BatchService) Execute(ctx context.Context, req BatchFeedbackRequest) (*BatchFeedbackResponse, error) {
    const where = "service.feedbackbatch.Execute"
    
    if req.IdempotencyKey != "" {
        // Check tenant quota (max 1000 active keys)
        count, err := s.idempotency.CountByTenant(ctx, req.TenantID)
        if err != nil {
            return nil, err
        }
        if count >= 1000 {
            return nil, ErrIdempotencyQuotaExceeded
        }
        
        // Compute request hash
        hash := sha256.Sum256(canonicalize(req))
        
        // Atomic claim via INSERT ON CONFLICT DO NOTHING
        err = s.idempotency.Claim(ctx, req.TenantID, req.IdempotencyKey, hash[:])
        if errors.Is(err, ErrIdempotencyConflict) {
            // Key exists, check if it's ours
            record, err := s.idempotency.Get(ctx, req.TenantID, req.IdempotencyKey)
            if err != nil {
                return nil, err
            }
            if !bytes.Equal(record.RequestHash, hash[:]) {
                return nil, ErrIdempotencyConflict  // 409: different params
            }
            if record.Status == "pending" {
                // Check if pending > 5 minutes (abandoned)
                if time.Since(record.CreatedAt) > 5*time.Minute {
                    // Reclaim the key
                    // ... update status to pending with new created_at
                } else {
                    return nil, ErrRequestInProgress  // 409: in progress
                }
            }
            if record.Status == "completed" || record.Status == "failed" {
                // Return cached response
                return unmarshalResponse(record.ResponseBody), nil
            }
        }
        
        defer func() {
            // Store result on completion
            body, _ := json.Marshal(resp)
            status := "completed"
            if respErr != nil {
                status = "failed"
            }
            s.idempotency.Complete(ctx, req.TenantID, req.IdempotencyKey, status, respCode, body)
        }()
    }
    
    // Execute operation...
}
```

**Pending timeout:** Keys in `pending` state for > 5 minutes are treated as
abandoned and can be re-claimed. This handles server crashes mid-processing.

### Audit logging

All batch operations write to `feedback_audit_log`:

```go
func (s *BatchService) recordAudit(ctx context.Context, tx pgx.Tx, tenantID string, 
    feedbackIDs []int64, entityType, fieldName, oldValue, newValue, comment, changedBy string) error {
    
    entries := make([]AuditEntry, len(feedbackIDs))
    for i, id := range feedbackIDs {
        entries[i] = AuditEntry{
            TenantID:   tenantID,
            FeedbackID: id,
            EntityType: entityType,
            FieldName:  fieldName,
            OldValue:   oldValue,
            NewValue:   newValue,
            Comment:    comment,
            ChangedBy:  changedBy,
        }
    }
    return s.audits.WriteBatch(ctx, tx, entries)
}
```

**Audit fields by operation:**

| Operation | entity_type | field_name | old_value | new_value |
|-----------|-------------|------------|-----------|-----------|
| Tag add | `tag` | `tag_id` | `""` | `{tag_id}` |
| Tag remove | `tag` | `tag_id` | `{tag_id}` | `""` |
| Workflow | `workflow` | `workflow_state_id` | `{old_state_id}` | `{new_state_id}` |
| Soft delete | `feedback` | `deleted_at` | `""` | `{timestamp}` |
| Hard delete | `feedback` | `hard_deleted` | `""` | `true` |

Query-based batch logs the original query in `comment` field for forensics.

### Rate limiting

Implemented via sliding window in Redis (or in-process fallback):

| Limit | Value | Scope | On exceed |
|-------|-------|-------|-----------|
| Semantic search | 60/min | Per tenant | 429 + `Retry-After: 60` |
| Batch requests | 30/min | Per tenant | 429 + `Retry-After: 60` |
| Delete operations | 10/min | Per tenant | 429 + `Retry-After: 60` |
| Concurrent async jobs | 5 | Per tenant | 429 + `Retry-After: 60` |

```go
// In internal/infra/ratelimit/
type Limiter interface {
    Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error)
}
```

### Console UI

#### Batch selection

**SelectionActionBar enhancements:**

```tsx
export function SelectionActionBar({
  count,
  totalMatching,  // NEW: total rows matching current filter
  selectionMode,  // NEW: 'ids' | 'query'
  availableTags,
  removableTags,
  workflowStates,
  isPending,      // NEW: loading state
  onBatchAdd,
  onBatchRemove,
  onBatchTransition,
  onBatchDelete,  // NEW
  onSelectAllMatching,  // NEW
  onCancel,
}: Props) {
  // ...
}
```

1. **Current:** Checkboxes on each row, count display, tag/workflow actions
2. **New:** "全选当前页" checkbox in table header
3. **New:** When filters active and `selectionMode === 'ids'`, show link:
   "选择全部 {totalMatching} 条匹配项"
4. **New:** Delete button (trash icon) with confirmation dialog
5. **New:** Loading state (`isPending`) disables all actions

**"Select all matching" flow:**
1. User applies filters (workflow=open, tag=urgent)
2. User clicks "选择全部 847 条匹配项"
3. Confirmation dialog shows:
   - Title: "确认选择全部匹配项"
   - Preview: First 5-10 items with content snippet
   - Breakdown: "47 条待处理, 12 条处理中, 3 条已关闭"
   - Warning: "批量操作将应用于所有 847 条匹配的反馈"
   - Buttons: [取消] [确认选择]
4. On confirm, `selectionMode = 'query'` with current filter params
5. Batch request sends `query` instead of `feedback_ids`

**Delete confirmation dialog:**

```tsx
<Dialog>
  <DialogHeader>
    <DialogTitle>删除反馈</DialogTitle>
  </DialogHeader>
  <DialogContent>
    <p>确定要删除 {count} 条反馈？</p>
    <RadioGroup value={deleteMode} onValueChange={setDeleteMode}>
      <RadioGroupItem value="soft" label="归档（可恢复）" />
      {isAdmin && (
        <RadioGroupItem value="hard" label="永久删除（不可恢复）" />
      )}
    </RadioGroup>
    {selectionMode === 'query' && (
      <Alert variant="warning">
        <AlertDescription>
          此操作将删除所有匹配当前筛选条件的反馈，请确认筛选条件正确。
        </AlertDescription>
      </Alert>
    )}
  </DialogContent>
  <DialogFooter>
    <Button variant="outline" disabled={isPending}>取消</Button>
    <Button variant="destructive" disabled={isPending}>
      {isPending ? <><Loader2 className="animate-spin mr-2" />删除中...</> : '确认删除'}
    </Button>
  </DialogFooter>
</Dialog>
```

#### Async job progress

For batches > 100 items:

```tsx
// Persistent toast with progress
function BatchJobToast({ jobId }: { jobId: string }) {
  const { data: job, refetch } = useJobStatus(jobId, {
    refetchInterval: (data) => {
      if (!data || data.status === 'completed' || data.status === 'failed') return false
      return data.retryAfterSeconds * 1000 || 2000
    }
  })
  
  if (!job) return null
  
  const progress = job.total > 0 ? (job.progress / job.total) * 100 : 0
  
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span>批量操作进行中</span>
        <Button size="sm" variant="ghost" onClick={() => cancelJob(jobId)}>
          取消
        </Button>
      </div>
      <Progress value={progress} />
      <span className="text-sm text-muted-foreground">
        {job.progress} / {job.total} ({Math.round(progress)}%)
      </span>
    </div>
  )
}
```

**Job history:** Accessible from settings page, showing recent jobs and outcomes.

#### Semantic search

**Search bar placement:** Above filter row, full width, distinct from keyword filter

```tsx
function SemanticSearchBar({ onSearch, isSearching }: Props) {
  const [query, setQuery] = useState('')
  const debouncedQuery = useDebounce(query, 500)  // 500ms debounce
  
  // Only search on Enter or explicit button click
  const handleSearch = () => {
    if (query.trim()) onSearch(query)
  }
  
  return (
    <div className="relative">
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
        placeholder={t('feedback.search.placeholder')}
        className="pl-10"
      />
      <div className="absolute left-3 top-1/2 -translate-y-1/2">
        {isSearching ? (
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        ) : (
          <Sparkles className="h-4 w-4 text-muted-foreground" />
        )}
      </div>
      {query && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute right-1 top-1/2 -translate-y-1/2"
          onClick={() => { setQuery(''); onSearch(''); }}
        >
          <X className="h-4 w-4" />
        </Button>
      )}
    </div>
  )
}
```

**Similarity indicator (matching ConfidenceIndicator pattern):**

```tsx
function SimilarityIndicator({ similarity, matchType }: { similarity: number; matchType: string }) {
  const tier = useMemo(() => {
    if (matchType === 'keyword') return { label: '关键词匹配', tone: 'default' }
    if (similarity >= 0.9) return { label: '极相似', tone: 'emerald' }
    if (similarity >= 0.8) return { label: '高度相关', tone: 'emerald' }
    return { label: '相关', tone: 'amber' }
  }, [similarity, matchType])
  
  return (
    <span 
      className={`inline-flex items-center gap-1 text-xs`}
      title={`${Math.round(similarity * 100)}% ${tier.label}`}
    >
      <span className={`h-2 w-2 rounded-full bg-${tier.tone}-500`} />
      <span className={`text-${tier.tone}-700`}>{tier.label}</span>
    </span>
  )
}
```

**Empty states:**
- No embeddings for tenant: "语义搜索未启用。请联系管理员开启反馈聚类功能。"
- Embedding API failed: "语义搜索暂时不可用，已切换到关键词搜索。"
- No results above threshold: "未找到相似反馈。" + 显示关键词搜索结果（如有）

#### Error feedback

**Tiered feedback based on failure ratio:**

```tsx
function BatchResultFeedback({ result }: { result: BatchFeedbackResponse }) {
  const failureRatio = result.failed.length / (result.succeeded + result.failed.length)
  
  if (result.failed.length === 0) {
    toast.success(t('feedback.batch.success', { count: result.succeeded }))
    return null
  }
  
  if (failureRatio < 0.1) {
    // < 10% failures: warning toast with expandable
    toast.warning(
      <div>
        <p>{t('feedback.batch.partial_success', { succeeded: result.succeeded, failed: result.failed.length })}</p>
        <Button variant="link" size="sm" onClick={() => setShowDetails(true)}>
          {t('feedback.batch.view_failures')}
        </Button>
      </div>
    )
  } else if (failureRatio < 0.5) {
    // 10-50% failures: inline Alert
    return (
      <Alert variant="warning">
        <AlertTitle>{t('feedback.batch.partial_title')}</AlertTitle>
        <AlertDescription>
          {t('feedback.batch.partial_success', { succeeded: result.succeeded, failed: result.failed.length })}
          <FailureList failures={result.failed} />
        </AlertDescription>
        <Button variant="outline" size="sm" onClick={handleRetryFailed}>
          {t('feedback.batch.retry_failed')}
        </Button>
      </Alert>
    )
  } else {
    // > 50% failures: full dialog
    return <BatchFailureDialog result={result} onRetry={handleRetryFailed} />
  }
}
```

**Retry failed items:** Pre-select only the failed feedback IDs for next batch.

#### i18n additions

```json
{
  "feedback.batch.select_all_page": "全选当前页",
  "feedback.batch.select_all_matching": "选择全部 {{count}} 条匹配项",
  "feedback.batch.confirm_select_all_title": "确认选择全部匹配项",
  "feedback.batch.confirm_select_all_warning": "批量操作将应用于所有 {{count}} 条匹配的反馈",
  "feedback.batch.confirm_select_all_breakdown": "{{open}} 条待处理, {{active}} 条处理中, {{closed}} 条已关闭",
  "feedback.batch.preview_items": "预览（前 {{count}} 条）",
  "feedback.batch.delete": "删除",
  "feedback.batch.delete_confirm_title": "删除反馈",
  "feedback.batch.delete_confirm": "确定要删除 {{count}} 条反馈？",
  "feedback.batch.delete_soft": "归档（可恢复）",
  "feedback.batch.delete_hard": "永久删除（不可恢复）",
  "feedback.batch.delete_query_warning": "此操作将删除所有匹配当前筛选条件的反馈，请确认筛选条件正确。",
  "feedback.batch.conflict": "{{count}} 条反馈在选择后被修改，已跳过",
  "feedback.batch.success": "成功操作 {{count}} 条反馈",
  "feedback.batch.partial_success": "{{succeeded}} 条成功，{{failed}} 条失败",
  "feedback.batch.partial_title": "部分操作未完成",
  "feedback.batch.view_failures": "查看失败项",
  "feedback.batch.retry_failed": "重试失败项",
  "feedback.batch.job_started": "批量操作已开始，处理 {{count}} 条反馈...",
  "feedback.batch.job_progress": "进度: {{progress}}/{{total}} ({{percent}}%)",
  "feedback.batch.job_cancel": "取消",
  "feedback.batch.job_cancelled": "批量操作已取消",
  "feedback.batch.job_failed": "批量操作失败：{{error}}",
  "feedback.search.placeholder": "语义搜索：输入自然语言描述，按 Enter 搜索",
  "feedback.search.searching": "搜索中...",
  "feedback.search.similarity_very_high": "极相似",
  "feedback.search.similarity_high": "高度相关",
  "feedback.search.similarity_medium": "相关",
  "feedback.search.similarity_keyword": "关键词匹配",
  "feedback.search.no_embeddings": "语义搜索未启用。请联系管理员开启反馈聚类功能。",
  "feedback.search.api_failed": "语义搜索暂时不可用，已切换到关键词搜索。",
  "feedback.search.no_results": "未找到相似反馈",
  "feedback.search.showing_keyword_fallback": "显示关键词搜索结果",
  "feedback.search.clear": "清除搜索"
}
```

### Permissions

| Operation | Required role | Enforcement |
|-----------|---------------|-------------|
| Batch tag add/remove | Session user | Handler checks `auth.UserID` exists |
| Batch workflow transition | Session user | Handler checks `auth.UserID` exists |
| Batch soft delete (archive) | Session user | Handler checks `auth.UserID` exists |
| Batch hard delete | Admin only | Handler checks `admin.Role == "admin"` |
| Semantic search | Session user (if tenant has clustering_enabled) | Service checks tenant flag |
| Job status polling | Session user (own jobs) | Handler checks `job.TenantID == auth.TenantID` |
| Job cancellation | Session user (own jobs) | Handler checks `job.TenantID == auth.TenantID` |

### Observability

**Metrics (in `internal/infra/metrics/`):**

```go
var (
    // Batch operations
    BatchOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "attune_batch_ops_total",
        Help: "Total batch operations",
    }, []string{"tenant_id", "operation", "mode", "status"})
    
    BatchItemFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "attune_batch_item_failures_total",
        Help: "Batch item failures by error code",
    }, []string{"tenant_id", "operation", "code"})
    
    BatchRequestSize = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "attune_batch_request_size",
        Help:    "Batch request size (number of items)",
        Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
    })
    
    BatchDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "attune_batch_duration_seconds",
        Help:    "Batch operation duration",
        Buckets: prometheus.DefBuckets,
    }, []string{"operation", "mode"})
    
    // Async jobs
    BatchJobQueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "attune_batch_job_queue_depth",
        Help: "Number of pending/running batch jobs",
    }, []string{"tenant_id", "status"})
    
    BatchJobDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "attune_batch_job_duration_seconds",
        Help:    "Async batch job duration",
        Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
    }, []string{"operation"})
    
    // Semantic search
    SemanticSearchTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "attune_semantic_search_total",
        Help: "Total semantic search requests",
    }, []string{"tenant_id", "status", "embedding_model", "match_type"})
    
    SemanticSearchLatencySeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "attune_semantic_search_latency_seconds",
        Help:    "Semantic search latency",
        Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
    }, []string{"phase"})  // phases: embed, search, hydrate, total
    
    SemanticSearchHitsCount = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "attune_semantic_search_hits_count",
        Help:    "Number of search results returned",
        Buckets: []float64{0, 1, 5, 10, 20, 50, 100},
    })
    
    SemanticSearchTopSimilarity = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "attune_semantic_search_top_similarity",
        Help:    "Top-1 result similarity score",
        Buckets: []float64{0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 1.0},
    })
    
    // Caches
    IdempotencyCacheTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "attune_idempotency_cache_total",
        Help: "Idempotency cache operations",
    }, []string{"tenant_id", "result"})  // hit, miss, conflict, expired
    
    QueryEmbedCacheTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "attune_query_embed_cache_total",
        Help: "Query embedding cache operations",
    }, []string{"tenant_id", "result"})  // hit, miss
)
```

**Logging (via `logext`, never `slog` directly):**

```go
const where = "service.feedbackbatch.Execute"

// Batch start
logext.Infof(ctx, "[%s] batch start,tenant_id:%s,op:%s,mode:%s,count:%d,idempotency_key:%s",
    where, tenantID, op, mode, count, truncateKey(idempotencyKey))

// Batch complete
logext.Infof(ctx, "[%s] OK,tenant_id:%s,succeeded:%d,failed:%d,skipped:%d,duration_ms:%d",
    where, tenantID, result.Succeeded, len(result.Failed), result.Skipped, durMs)

// Optimistic lock conflict
logext.Warnf(ctx, "[%s] conflict,tenant_id:%s,feedback_id:%d,requested:%s,actual:%s",
    where, tenantID, feedbackID, requestedTime, actualTime)

// Idempotency hit
logext.Infof(ctx, "[%s] idempotency hit,tenant_id:%s,key_prefix:%s",
    where, tenantID, key[:8])

// Search
logext.Infof(ctx, "[%s] search,tenant_id:%s,query_len:%d,query_hash:%s,results:%d,match_type:%s,latency_ms:%d",
    where, tenantID, len(query), hashPrefix(query), len(results), matchType, latencyMs)
```

**Never log:**
- Full query text (PII risk)
- Full idempotency key (truncate to first 8 chars)

**Tracing (OTel spans):**

```go
// Batch operation span
ctx, span := tracer.Start(ctx, "attune.batch.execute")
defer span.End()
span.SetAttributes(
    attribute.String("tenant_id", tenantID),
    attribute.String("operation", op),
    attribute.String("mode", mode),
    attribute.Int("count", count),
)

// Child spans
_, resolveSpan := tracer.Start(ctx, "attune.batch.resolve_query")
// ...
resolveSpan.End()

_, executeSpan := tracer.Start(ctx, "attune.batch.execute_items")
// ...
executeSpan.End()

// Semantic search span
ctx, span := tracer.Start(ctx, "attune.search.semantic")
defer span.End()

_, embedSpan := tracer.Start(ctx, "attune.search.embed_query")
// embed query...
embedSpan.End()

_, pgSpan := tracer.Start(ctx, "attune.search.pgvector")
// pgvector search...
pgSpan.End()
```

For async jobs, propagate `trace_id` through the job record (same pattern as
outbox `inbound_trace_id`) for cross-request correlation.

**Alerting recommendations:**

| Alert | Condition | Severity |
|-------|-----------|----------|
| Batch job queue depth | `attune_batch_job_queue_depth > 50` for 5m | Warning |
| Batch job age | oldest job age > 10m | Critical |
| Semantic search p95 latency | `histogram_quantile(0.95, ...) > 1s` | Warning |
| Semantic search error rate | error rate > 5% | Warning |
| Query embed cache miss rate | miss rate > 90% for 1h | Info |
| Idempotency conflict rate | > 10/5m | Warning |
| Batch partial failure rate | > 20% of batches | Info |

## Alternatives considered

### A. Separate endpoints per operation (current state)

Keep `/tags/batch`, `/transition/batch`, add `/delete/batch`.

**Rejected because:**
- No unified idempotency story
- No query-based batch mode
- Three endpoints to maintain instead of one
- Client needs to know which endpoint for which operation

### B. GraphQL batch mutations (Linear pattern)

Use GraphQL with aliases for batching.

**Rejected because:**
- attune is REST-first (proto IDL contract)
- No server-side batching — each mutation executes independently
- Doesn't solve query-based batch

### C. Always async (Jira pattern)

All batch operations return job_id, even for 10 items.

**Rejected because:**
- Adds latency for common small batches
- Requires polling for every operation
- Sync is fine for ≤ 100 items

### D. Semantic-only search (no hybrid)

Only use embedding similarity, no keyword fallback.

**Rejected because:**
- Exact string matches may not rank highest
- Embedding API failures block search entirely
- Industry best practice is hybrid (Algolia, Linear)

### E. Separate delete endpoint

Follow industry pattern of dedicated delete endpoint.

**Considered but unified because:**
- Permission differentiation (soft=user, hard=admin) still exists in operation
- Idempotency and query-mode benefit from unified endpoint
- Audit logging is consistent across all operations
- Trade-off acknowledged: delete in batch endpoint is less discoverable

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| **Query-based batch deletes wrong data** | Server-side `max_affected` limit; `confirm_count` must match; confirmation dialog with preview; soft delete by default; hard delete admin-only |
| **Async jobs pile up** | 5 concurrent jobs per tenant; 24h retention; heartbeat timeout detection; monitoring alerts |
| **Embedding API latency/failure** | Query embedding cache (5min TTL); timeout with keyword fallback; 503 with Retry-After |
| **Model mismatch after upgrade** | `embedding_model` filter in search; cache key includes model; backfill worker for model upgrades |
| **Idempotency storage grows unbounded** | 24h TTL; daily cleanup job; 1000 keys per tenant quota; monitoring for table size |
| **Idempotency key stuck in pending** | 5-minute pending timeout; startup recovery marks stuck keys as failed |
| **Optimistic lock conflicts confusing** | Clear UI message; return `current_updated_at` for smart retry; "Retry failed" button |
| **Concurrent jobs operate on overlapping items** | Documented as last-write-wins; per-item `FOR UPDATE` lock prevents corruption |
| **Worker crash mid-execution** | Heartbeat mechanism; startup recovery marks stuck jobs failed; items already processed remain committed |

## Implementation plan

| Phase | Scope | Depends on |
|-------|-------|------------|
| **T1** | Migration `031_batch_ops.sql` — all DDL (updated_at, deleted_at, idempotency_keys, batch_jobs, query_embedding_cache) | — |
| **T2** | Proto definitions: `FeedbackFilter`, batch request/response, search request/response, job status, new error codes | T1 |
| **T3** | `repo/idempotency/` — key CRUD, claim, complete, cleanup | T1 |
| **T4** | `repo/feedback/` — batch update, batch delete, updated_at trigger, deleted_at filter, similarity search, keyword search | T1 |
| **T5** | `repo/feedbackjob/` — job CRUD, claim, heartbeat, cleanup | T1 |
| **T6** | `service/feedbackbatch/` — orchestration, idempotency, optimistic lock, audit logging | T3, T4, T5 |
| **T7** | `service/semanticsearch/` — query embedding, cache, pgvector search, keyword fallback, hybrid merge | T4 |
| **T8** | `infra/ratelimit/` — sliding window rate limiter (Redis or in-process) | — |
| **T9** | `handlers/console/feedback/batch.go` — unified batch endpoint | T6, T8 |
| **T10** | `handlers/console/feedback/search.go` — semantic search endpoint | T7, T8 |
| **T11** | `handlers/console/feedbackjob/` — job status, list, cancel endpoints | T5 |
| **T12** | Async job worker pool (4-8 workers, chunked processing, heartbeat, recovery) | T5, T6 |
| **T13** | Integration tests: batch CRUD, idempotency, conflicts, search, keyword fallback | T9, T10 |
| **T14** | Console: SelectionActionBar enhancements (select-all-page, select-all-matching, delete button, isPending) | T9 |
| **T15** | Console: Batch confirmation dialogs (select-all preview, delete soft/hard) | T14 |
| **T16** | Console: Async job progress toast, job history | T11 |
| **T17** | Console: Semantic search bar (500ms debounce, Enter to search, loading state) | T10 |
| **T18** | Console: Similarity indicator (tier labels matching ConfidenceIndicator) | T17 |
| **T19** | Console: Batch result feedback (tiered toast/alert/dialog, retry failed) | T14 |
| **T20** | Console: i18n additions | T14-T19 |
| **T21** | Console vitest: batch selection hooks, search bar, delete dialog, job progress | T14-T19 |
| **T22** | Performance tests: 100k rows semantic search < 300ms with filters | T10 |
| **T23** | Observability: metrics, logging, tracing spans | T6, T7, T12 |
| **T24** | CHANGELOG.md update | T23 |

## Verification

- [ ] **Batch unit tests:** 100-item batch, partial failure, idempotency hit/miss/conflict, optimistic lock conflict, pending timeout, quota exceeded
- [ ] **Batch unit tests:** Empty feedback_ids + empty query validation, both provided validation, delete without confirm_count, hard delete non-admin
- [ ] **Batch integration tests (test/integration/postgres/feedbackbatch/):** Tag add/remove, workflow transition, soft delete, hard delete (admin), query-based batch, updated_at trigger
- [ ] **Idempotency integration tests (test/integration/postgres/idempotency/):** Key create/get/expire, request hash mismatch, pending reclaim after 5min
- [ ] **Job integration tests (test/integration/postgres/feedbackjob/):** Create, claim, progress, heartbeat, complete, fail, cancel, cleanup stuck
- [ ] **Search unit tests:** Query embedding mock, similarity filtering, model mismatch, min_similarity threshold, limit bounds, clustering_enabled check
- [ ] **Search integration tests (test/integration/postgres/semanticsearch/):** Real pgvector query, CTE pattern with filters, keyword fallback, hybrid merge
- [ ] **Performance tests:** 100k rows semantic search < 50ms pure HNSW, < 300ms with workflow+tag filters
- [ ] **Console vitest:** Select-all checkbox, select-all-matching mode, confirmation dialogs, delete soft/hard, async job toast, search bar debounce, similarity badge tiers
- [ ] **Permission tests:** Hard delete blocked for non-admin, search blocked when clustering disabled, job status blocked for other tenant's job
- [ ] **Rate limit tests:** 60/min search, 30/min batch, 10/min delete, 5 concurrent jobs
- [ ] **Observability tests:** Metrics increment correctly, logs contain required fields, spans have correct attributes
- [ ] **End-to-end:** Ingest 1000 feedback → batch tag → batch transition → batch delete → verify audit log → semantic search → job progress polling
- [ ] **Real-environment:** Start server with clustering enabled, use Console to batch-select and operate, test semantic search with real embeddings, test keyword fallback

## References

- [Zendesk Bulk Update API](https://developer.zendesk.com/api-reference/ticketing/tickets/tickets/#update-many-tickets)
- [Zendesk Job Statuses](https://developer.zendesk.com/api-reference/ticketing/ticket-management/job_statuses/)
- [Zendesk Idempotency Keys](https://developer.zendesk.com/documentation/api-basics/going-live/idempotency/)
- [Sentry Bulk Mutate Issues](https://docs.sentry.io/api/events/bulk-mutate-a-list-of-issues/)
- [Jira Bulk Operations](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-bulk-operations/)
- [Stripe Idempotent Requests](https://stripe.com/docs/api/idempotent_requests)
- [Brandur: Implementing Idempotency Keys in Postgres](https://brandur.org/idempotency-keys)
- [Linear Search Changelog (April 2025)](https://linear.app/changelog/2025-04-10-new-search)
- [Algolia NeuralSearch](https://www.algolia.com/doc/guides/ai-relevance/neuralsearch/get-started)
- [pgvector GitHub](https://github.com/pgvector/pgvector)
- [Supabase Semantic Search](https://supabase.com/docs/guides/ai/semantic-search)
- [Google AIP-155: Request identification](https://google.aip.dev/155)
