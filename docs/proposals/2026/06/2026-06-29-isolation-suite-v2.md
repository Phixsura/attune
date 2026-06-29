# Tenant Isolation Contract Suite v2 — World-Class Redesign

| Field   | Value |
|---------|-------|
| Issue   | #154 |
| Status  | Implemented |
| Started | 2026-06-29 |
| Related | PR #200 (v1 implementation) |

---

## Problem

The v1 isolation suite (PR #200) covers the fundamentals but has five
structural weaknesses that prevent it from being a world-class safety net:

### P1. Existing test bugs (correctness)

| Bug | Impact |
|-----|--------|
| `GET /tags/{id}` and `GET /workflow/states/{id}` tested in `CrossTenantGetDenied` but these routes **do not exist** in `mountAPIKeyTags`/`mountAPIKeyWorkflow` — tests always 404, providing zero isolation signal | 2 of 5 get-test cases are vacuous |
| Cross-tenant write returning HTTP 200 logged as `t.Logf` (warning), not `t.Errorf` (failure) | A successful cross-tenant mutation passes the test |
| `systemsettings` contract case looks up key `iso-key-only-in-B` that was never seeded for tenant B — the test passes whether the repo is correct or broken | 1 of 27 contract cases is vacuous |

### P2. Heuristic assertions (false positives/negatives)

All Layer C tests use `containsAny(body, uuids...)` substring matching.

- **False positive**: UUID fragment appears in an error message, trace ID,
  or `jsonrpc: "2.0"` version string (already hit in MCP tests with
  feedback ID `"2"`).
- **False negative**: a truncated UUID, numeric ID, or differently-formatted
  value leaks without containing any of the checked strings.
- **Silent failure**: a query that returns empty results for both tenants
  passes the containsAny check — we can't distinguish "correctly isolated"
  from "broken query returning nothing".

### P3. Incomplete repo coverage (Layer A/B)

The v1 contract table covers 27 cases across 18 domains. A full audit of
`internal/repo/` reveals **~150 tenant-scoped methods** across 20+ packages.

**Packages entirely missing from Layer A/B:**

| Package | Tenant-scoped methods | Risk |
|---------|----------------------|------|
| `feedbacktagassignment` | `Add`, `Remove`, `ListByFeedback`, `ListByFeedbackBatch`, `RemoveByScopeExcluding` | High — MCP write path |
| `feedback` (batch ops) | `BatchUpdateTags`, `BatchUpdateWorkflow`, `BatchSoftDelete`, `BatchHardDelete`, `RestoreSoftDeleted`, `CountByFilter`, `ListIDsByFilter` | High — bulk mutation |
| `feedback` (analytics) | `UsageByDay`, `TopValuesByDim`, `UrgentCount`, `WindowStats`, `EnrichedInWindow`, `DailyCounts` | Medium |
| `guardpolicy` | `ListForConsole`, `CreateTenantPolicy`, `DeleteTenantPolicy` | Medium |
| `replydraft` | `QueueDepth`, `LoadForDraft`, `UpdateReplyDraft` | Medium |
| `inboundsource` | All methods (scoped by source ID, not tenantID directly) | Low |
| `embedding` (clusters) | `ListClusters`, `GetClusterMembers`, `GetClusterInfo`, `EmbeddingsInWindow`, `TopClustersInWindow` | Medium |

**High-risk methods missing from tested domains:**

| Domain | Missing methods |
|--------|----------------|
| `feedback` | `ListForConsole`, `GetForConsole`, `SetUrgent`, `RetryEnrichment` (only `SampleEnrichedByTenant` tested at Layer A; Layer B covers List/Get but not writes) |
| `feedbacktag` | `Create`, `Update`, `Archive`, `GetByName`, `IncrementUsage`, `DecrementUsage` |
| `workflowstate` | `Get` (no tenantID!), `Create`, `Update`, `CountActiveFeedback`, `CheckTransition`, `AllowedNext`, `ListTransitions`, `ReplaceTransitions` |
| `notifytarget` | `ListByTenant`, `ListActiveByTenant`, `UpdateByID`, `Upsert` |
| `apikey` | `Rotate`, `ListRequestLogs`, `ListServiceAccounts`, `ListEventSubscriptions`, etc. |
| `mcp` | `GetByID` (no tenantID — scoped by clientID only), `sessions.ListByClient` |
| `gdpr` | `Delete`, `ListRequests`, `GetOperationsSummary`, `CancelDeleteRequest`, `MarkExportJobDownloaded`, `RevokeExportJob` |

### P4. Incomplete HTTP/MCP endpoint coverage (Layer C)

**API Key endpoints not tested:**

| Category | Endpoints |
|----------|-----------|
| Create | `POST /tags`, `POST /workflow/states`, `POST /mcp/clients` |
| Read (missing) | `GET /audit-log/export.csv`, `GET /audit-log/evidence/{job_id}/download`, `GET /gdpr/exports/{job_id}/download` |
| Write (missing) | `POST /workflow/seed`, `PUT /workflow/transitions`, `PUT /mcp/clients/{id}/tool-policies`, `DELETE /mcp/clients/{id}/grants/{grant_id}`, `DELETE /mcp/clients/{id}/sessions/{session_id}` |
| GDPR write | `POST /gdpr/export`, `POST /gdpr/delete`, `POST /gdpr/requests/{request_id}/cancel`, `POST /gdpr/exports/{job_id}/revoke` |

**MCP JSON-RPC tools not tested:**

| Tool | Scope | Gap reason |
|------|-------|------------|
| `get_workflow_state` | `mcp:read` | Omitted in v1 |
| `remove_tag` | `mcp:write` | Omitted in v1 |
| `update_workflow_state` | `mcp:write` | `WorkflowTransitioner` needs adapter |
| `submit_feedback` | `mcp:ingest` | `RegisterIngestTools` never called |

### P5. Thin fixture (indistinguishable results)

Each domain seeds exactly 1 record per tenant. This means:

- A test checking "tenant A sees 0 results" can't distinguish "correctly
  isolated" from "broken query returning 0 for everyone".
- No variation in record state (archived, revoked, failed, enriched) — so
  filter/status-based queries are untestable.
- No enriched feedback → semantic search / embedding tests impossible.

---

## Goals

1. Fix all 3 existing bugs (P1)
2. Replace substring assertions with structural JSON parsing (P2)
3. Expand Layer A to cover all tenant-scoped repo methods worth testing (P3)
4. Expand Layer C to cover all existing HTTP and MCP endpoints (P4)
5. Enrich fixture with 3+ records per domain in varied states (P5)
6. Keep total integration test runtime under 60s

## Non-goals

- Testing global/worker methods (`ClaimBatch`, `PurgeExpired*`, etc.) —
  these are tenant-agnostic by design.
- Testing auth boundary (expired JWT, wrong scope, revoked client) — that's
  auth testing, not isolation testing.
- Concurrent isolation testing — deferred to a separate effort; the DB's
  row-level security model makes concurrent reads/writes the same as
  serial ones for isolation purposes (tenant_id is a WHERE clause, not a
  lock).

---

## Proposal

### 1. Bug fixes (P1)

**1a.** Remove `GET /tags/{id}` and `GET /workflow/states/{id}` from
`TestHTTP_APIKey_CrossTenantGetDenied` — routes don't exist. Replace with
routes that DO exist: `GET /mcp/clients/{id}`, `GET /audit-log/evidence/{job_id}`,
`GET /gdpr/exports/{job_id}` (already there).

**1b.** Change `t.Logf("WARNING: ...")` to `t.Errorf("ISOLATION BREACH: ...")`
for cross-tenant write 200 responses.

**1c.** Fix `systemsettings` contract case: seed key `iso-key-only-in-B` for
tenant B and `iso-key-only-in-A` for tenant A (not the same key), so the
cross-tenant lookup is meaningful.

### 2. Structural assertions (P2)

Replace `containsAny(body, uuids...)` with typed JSON parsing:

```go
// For HTTP responses:
type listResponse[T any] struct {
    Items []T `json:"items"`
    Data  []T `json:"data"`
}

func assertNoLeakedTenantID[T interface{ GetTenantID() string }](
    t *testing.T, body []byte, forbiddenTenantID string,
) {
    // Parse the response, iterate items, check each item's tenant_id
}

// For JSON-RPC responses:
type jsonrpcResponse struct {
    Result json.RawMessage `json:"result"`
    Error  *jsonrpcError   `json:"error"`
}
```

Key principle: parse the response structure, extract each record's
identifying fields (tenant_id, id, content), and assert none belong to the
forbidden tenant. Also assert that the correct tenant's data IS present
(positive assertion — proves the query works, not just that it's empty).

**Dual assertion**: for list endpoints, verify BOTH:
- Tenant A sees only A's records (positive)
- Tenant A sees none of B's records (negative)

### 3. Fixture enrichment (P5)

Expand `NewFixture` to seed 3 records per tenant per domain with varied states:

| Domain | Records per tenant | Variations |
|--------|-------------------|------------|
| `user_feedback` | 3 | 1 enriched, 1 pending, 1 urgent |
| `feedback_tag` | 2 | 1 active, 1 archived |
| `feedback_tag_assignment` | 2 | tag A1→feedback A1, tag A2→feedback A2 |
| `workflow_state` | 3 | 1 default, 1 active, 1 archived |
| `workflow_transition` | 2 | default→active, active→archived |
| `notify_outbox` | 2 | 1 pending, 1 delivered |
| `api_key` | 2 | 1 active, 1 revoked |
| `notify_target` | 2 | 1 active, 1 with failure |
| `audit_log` | 3 | different actions |
| `mcp_client` | 2 | 1 active, 1 revoked |
| `feedback_job` | 2 | 1 queued, 1 completed |
| `audit_evidence` | 1 | (same as v1) |
| `digest_subscription` | 1 | (same, only 1 per tenant allowed) |
| `system_settings` | 2 | 2 different keys (one unique per tenant) |
| `tenant_member` | 2 | 1 admin, 1 viewer |
| `llm_config` | 1 | (same) |
| `idempotency` | 1 | (same) |
| `feedback_audit` | 2 | different field changes |
| `embedding_task` | 1 | (same) |
| `guard_policy` | 1 | NEW |
| `reply_draft` | 1 | NEW (if table exists) |

Store record counts in `TenantData` so assertions can verify exact expected
counts:

```go
type TenantData struct {
    // ... existing IDs ...
    FeedbackCount      int
    TagCount           int
    ActiveTagCount     int
    WorkflowStateCount int
    // etc.
}
```

### 4. Layer A expansion (P3)

Reorganize the contract table by **isolation pattern** rather than raw method
count. Each repo method falls into one of 3 patterns:

| Pattern | What to test | Example |
|---------|-------------|---------|
| **list** | `List(tenantA)` returns only A's records, count matches expected | `feedbacktag.List` |
| **get-cross** | `Get(tenantA, resourceB)` returns error or empty | `feedbacktag.GetByID` |
| **mutate-cross** | `Mutate(tenantA, resourceB)` returns error, then verify B's data unchanged | `apikey.Revoke` |

**Expanded contract table** (target: ~60 cases, up from 27):

New cases to add:

| Domain | Pattern | Method |
|--------|---------|--------|
| `feedback` | list | `ListForConsole` |
| `feedback` | get-cross | `GetForConsole` |
| `feedback` | mutate-cross | `SetUrgent` |
| `feedback` | mutate-cross | `RetryEnrichment` |
| `feedback` | list | `CountByFilter` |
| `feedback` | mutate-cross | `BatchSoftDelete` |
| `feedbacktagassignment` | list | `ListByFeedback` |
| `feedbacktagassignment` | mutate-cross | `Add` (A's tag → B's feedback) |
| `feedbacktagassignment` | mutate-cross | `Remove` (A's tag from B's feedback) |
| `feedbacktag` | get-cross | `GetByName` |
| `feedbacktag` | mutate-cross | `Archive` |
| `workflowstate` | get-cross | `Get` (no tenantID — high risk!) |
| `workflowstate` | mutate-cross | `Archive` |
| `workflowstate` | list | `ListTransitions` |
| `workflowstate` | mutate-cross | `CheckTransition` (A's from → B's to) |
| `notifytarget` | list | `ListByTenant` |
| `notifytarget` | mutate-cross | `UpdateByID` |
| `gdpr` | mutate-cross | `Delete` |
| `gdpr` | list | `ListRequests` |
| `gdpr` | mutate-cross | `CancelDeleteRequest` |
| `mcp` | get-cross | `ClientsRepo.GetByID` (no tenantID!) |
| `apikey` | mutate-cross | `Rotate` |
| `apikey` | list | `ListRequestLogs` |
| `embedding` | list | `ListClusters` |
| `tenantmember` | list | `CountAdmins` |
| `tenantmember` | mutate-cross | `EnsureOIDCMember` |
| `guardpolicy` | list | `ListForConsole` |
| `guardpolicy` | mutate-cross | `DeleteTenantPolicy` |

### 5. Layer B expansion (P3)

Add isolation tests to these packages that currently have none:

| Package | Tests to add |
|---------|-------------|
| `feedbacktagassignment` | `TestPG_Add_TenantIsolation`, `TestPG_ListByFeedback_TenantIsolation` |
| `auditevidence` | `TestPG_GetJob_TenantIsolation` |
| `embedding` | `TestPG_QueueDepth_TenantIsolation`, `TestPG_ListClusters_TenantIsolation` |
| `feedbackaudit` | `TestPG_List_TenantIsolation` |
| `guardpolicy` | `TestPG_ListForConsole_TenantIsolation`, `TestPG_DeleteTenantPolicy_TenantIsolation` |
| `llmconfig` | `TestPG_ResolveCandidates_TenantIsolation`, `TestPG_DeleteRoute_TenantIsolation` |
| `digestsubscription` | `TestPG_GetByTenant_TenantIsolation` |
| `systemsettings` | `TestPG_Get_TenantIsolation` (with properly seeded data) |

Layer B tests use **structural assertions** (typed fields, exact counts,
error sentinels) — the existing Layer B tests already do this well.

### 6. Layer C — API Key expansion (P4)

**Fix existing tests:**
- Remove `GET /tags/{id}` and `GET /workflow/states/{id}` from
  `CrossTenantGetDenied` (routes don't exist)
- Change write-200 `t.Logf` to `t.Errorf`

**Add to `CrossTenantListsDenied`:**
- Already comprehensive (8 list endpoints)

**Add to `CrossTenantGetDenied`:**
- `GET /gdpr/operations` — returns per-tenant summary
- (keep existing: `audit-log/evidence/{job_id}`, `mcp/clients/{id}`,
  `gdpr/exports/{job_id}`)

**Add to `CrossTenantWritesDenied`:**
- `POST /tags` with `name` matching B's tag name → verify created under A
- `POST /workflow/states` with name matching B's state → verify under A
- `POST /gdpr/export` with B's subject key
- `POST /gdpr/delete` with B's subject key
- `POST /gdpr/requests/{B's request_id}/cancel`
- `POST /gdpr/exports/{B's job_id}/revoke`
- `PUT /mcp/clients/{B's id}/tool-policies`
- `DELETE /mcp/clients/{B's id}/grants/{B's grant_id}`
- `DELETE /mcp/clients/{B's id}/sessions/{B's session_id}`

**Convert all assertions to structural parsing** (parse JSON, check
tenant_id / resource IDs in typed structs, not substring matching).

### 7. Layer C — MCP OAuth expansion (P4)

**Complete tool coverage:**

| Tool | Test type | Deps needed |
|------|-----------|-------------|
| `list_feedback` | list isolation | `FeedbackReader` (repo) |
| `list_tags` | list isolation | `TagReader` (repo) |
| `list_workflow_states` | list isolation | `WorkflowStateReader` (repo) |
| `get_feedback` | get-cross | `FeedbackReader` (repo) |
| `get_workflow_state` | get-cross | `WorkflowStateReader` (repo) |
| `set_urgent` | write-cross | `FeedbackWriter` (repo) |
| `add_tag` | write-cross | `TagAssigner` (repo) |
| `remove_tag` | write-cross | `TagAssigner` (repo) |
| `update_workflow_state` | write-cross | `WorkflowTransitioner` (**adapter needed**) |
| `submit_feedback` | write-cross | `Ingestor` (**adapter needed**) |

**Adapter for `WorkflowTransitioner`:**
```go
type testWorkflowTransitioner struct {
    svc *workflowsvc.Service
}

func (a *testWorkflowTransitioner) Transition(
    ctx context.Context, tenantID string, feedbackID int64,
    toStateID, byUser, comment string,
) error {
    _, err := a.svc.Transition(ctx, tenantID, feedbackID, toStateID, byUser, comment)
    return err
}
```

This mirrors the production adapter in `cmd/attune/router.go:498`.

**Adapter for `Ingestor`:**
```go
type testIngestor struct {
    repo *feedback.FeedbackRepo
}

func (a *testIngestor) Ingest(
    ctx context.Context, tenantID, userID string, in domain.IngestInput,
) (int64, error) {
    return a.repo.Insert(ctx, tenantID, userID, "", "", "", in)
}
```

**Adapter for `AuditRecorder`** (noop for isolation tests):
```go
type noopAuditRecorder struct{}

func (noopAuditRecorder) Record(_ context.Context, _ tools.AuditEvent) error {
    return nil
}
```

**Convert all MCP assertions to structural parsing** (parse JSON-RPC
response, extract `result.items[]`, check each item's tenant fields).

### 8. Assertion helpers

```go
// assertListIsolation parses a JSON response body, extracts items from
// the given field path, and verifies each item's tenantID field matches
// the expected tenant. Also verifies the expected count to distinguish
// "correctly isolated" from "broken query returning empty".
func assertListIsolation(t *testing.T, body []byte, opts assertOpts) { ... }

// assertCrossTenantDenied verifies that a cross-tenant get/write returns
// an error (JSON-RPC error, HTTP 404/403, or empty result) and does NOT
// contain the forbidden tenant's data.
func assertCrossTenantDenied(t *testing.T, body []byte, httpStatus int, forbiddenTenantID string) { ... }
```

---

## Alternatives considered

**A. Test every repo method individually at Layer A** (~150 cases). Rejected:
methods sharing the same `WHERE tenant_id = $1` clause have identical
isolation guarantees. Testing by query pattern (list/get/mutate) per domain
is sufficient; Layer B deep-dives the interesting methods.

**B. Use RLS (Row Level Security) instead of WHERE clauses**. Rejected: this
is a schema change, not a test design decision. The test suite should verify
the current isolation mechanism, whatever it is.

**C. Mock repos in MCP tests**. Rejected: the whole point of Layer C is
end-to-end through real SQL. Mocks would duplicate Layer A without adding
signal.

---

## Risks / tradeoffs

- **Test runtime**: more DB seeding + 2x more test cases ≈ 20-30s additional
  runtime. Acceptable for the isolation signal gained.
- **Fixture complexity**: richer fixture is harder to maintain. Mitigated by
  keeping all seeding in `NewFixture` with clear comments on invariants.
- **Adapter duplication**: the `testWorkflowTransitioner` adapter duplicates
  `cmd/attune/router.go:498`. If the production adapter changes, the test
  adapter must change too. Mitigated by the adapter being 5 lines.

---

## Implementation plan

### Task 1: Bug fixes + assertion helpers
- Fix 3 existing bugs (vacuous routes, t.Logf, systemsettings)
- Write structural assertion helpers
- Migrate existing tests to structural assertions

### Task 2: Fixture enrichment
- Expand `NewFixture` to seed 3 records per domain with varied states
- Update `TenantData` with counts and additional IDs
- Verify existing tests still pass with richer fixture

### Task 3: Layer A expansion
- Add ~33 new contract cases (target ~60 total)
- Cover all missing high-risk domains

### Task 4: Layer B expansion
- Add `isolation_test.go` to 8 packages that lack them
- Structural assertions throughout

### Task 5: Layer C API Key expansion
- Add missing endpoints to write/get tests
- Add create-cross-tenant tests
- Convert all assertions to structural

### Task 6: Layer C MCP OAuth expansion
- Add adapters for WorkflowTransitioner, Ingestor, AuditRecorder
- Cover all 10 JSON-RPC tools
- Structural assertions for JSON-RPC responses

### Task 7: CHANGELOG, verification, squash
- Update changelog
- Run `make ci-check` subset
- Squash to single commit

---

## Verification

- `go test -tags=integration ./test/integration/postgres/isolation/ -v` — all pass
- `go test -tags=integration ./test/integration/postgres/... -v` — all pass (no regression)
- `go vet ./...` — clean
- `lizard` — no NLOC/CCN violations
- `scripts/lint-rawptr.sh` — clean
- CI green on PR
