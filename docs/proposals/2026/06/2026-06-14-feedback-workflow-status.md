# Customisable feedback workflow status

| | |
|---|---|
| **Issue** | #29 |
| **Status** | Implemented |
| **Started** | 2026-06-14 CST |
| **Related** | #28 (manual tags — orthogonal labelling layer), #39 (generic audit log — the `feedback_audit_log` table introduced here is designed to serve both), #19 (proto IDL contract), #117 (tags — just merged; workflow status is orthogonal to tags) |

## Problem

`user_feedback.enrichment_status` tracks the LLM enrichment pipeline
(`pending → enriching → done / failed`). It is system-owned, not
operator-visible, and not mutable from the Console. Once enriched, a row
sits in `done` forever — there is no business-level lifecycle:

- No way to mark feedback as "under investigation" or "resolved".
- No way to triage and filter a backlog.
- No audit trail when an operator acts on a feedback item.
- No reporting on time-to-resolution or throughput.

Operators need a **separate, human-driven workflow status** with
customisable states, enforced transitions, and a full audit trail.

## Code reconciliation (issue text vs. verified code)

| Issue #29 says | Verified reality | Decision |
|---|---|---|
| "`feedback.status` only carries enrichment state" | Column is `enrichment_status`, not `status` | New column is `workflow_state_id` — a FK to a registry table, not an inline string |
| "status: `open → triaged → acknowledged → fixed`" | Fixed enum; no tenant customisation | **Full customisation**: per-tenant state registry + transition graph (ServiceNow/Jira model) |
| "`duplicate(other_id)` as a status value" | Mixing status with entity relationship | `duplicate` is a **close reason** annotation, not a status. Closed-category states carry the semantic; a separate `close_reason` pattern handles this (future #39 enhancement). For now the operator creates a "Duplicate" state under the `closed` category. |
| "Audit table `feedback_workflow_audit`" | Only `llm_audit` exists (LLM-call-level) | **Field-level generic audit table** (`feedback_audit_log`) — records any field change (old/new/who/when), not just workflow transitions. Paves the road for #39. |
| "Allowed transitions live as a constant table in `internal/domain/`" | Hard-coded Go constants | **Database-driven transition graph** (`tenant_workflow_transitions`). Transitions are per-tenant configuration, not compile-time constants. |

## Industry benchmarking

Benchmarked the workflow status subsystem across **ten top-tier products**
spanning issue trackers, customer support, incident management, project
management, and enterprise ITSM.

### Status model spectrum

| Model | Products | How it works |
|---|---|---|
| **Fixed enum** | Sentry (3+7 substatus), PagerDuty (3), GitHub (2+reason) | System-defined states; no tenant customisation |
| **Category-constrained custom** | Linear (6 cat), Zendesk (5 cat), Intercom (4 cat), Shortcut (4 types), GitLab (5 cat) | Fixed semantic categories; custom state names within each |
| **Fully custom + transition graph** | Jira (3 cat + DAG), ServiceNow (State Model + integer states), Salesforce (Support Processes) | Custom states, custom transitions, validation rules |

### Key consensus patterns

**1. Status categories are universal (8/10 products).**
Every product except Asana and PagerDuty groups custom states into a small
fixed set of semantic categories. Categories drive reporting, default
filtering, and board views — they are the system-meaningful anchor that
custom names alone cannot provide.

Attune adopts **3 categories**: `open` (new / backlog / untriaged),
`active` (in progress / being worked), `closed` (terminal — resolved,
won't-fix, etc.). Three is the minimum that preserves semantic value — it
matches Sentry (unresolved / resolved / archived) and PagerDuty (triggered /
acknowledged / resolved) while leaving room for arbitrary custom names
within each.

*Why not 4 categories (adding `triage`)?* Shortcut uses 4
(backlog/unstarted/started/done), Linear uses 6. The triage/backlog
distinction is real but in attune it is expressed as **two states within
`open`**: the default seed includes both "待处理" (untriaged) and "已分拣"
(triaged), both in the `open` category. Operators who need finer filtering
use the state-level filter, not the category filter. This avoids inflating
the category enum for a distinction that only some teams need — teams that
don't triage can archive the "已分拣" state and lose nothing.

**2. Status and tags are strictly orthogonal (10/10).**
No product conflates workflow status with tags. Status is single-select,
drives lifecycle behaviour, and is filterable as a first-class field. Tags
are multi-select, free-form metadata. Attune's `exclusive_scope` on tags
could theoretically model workflow status, but doing so would lose category
semantics, transition enforcement, audit granularity, and dedicated
reporting.

**3. Transition enforcement varies; strict is the enterprise default.**
ServiceNow and Jira enforce a directed graph — invalid transitions are
rejected. Linear and Shortcut allow free transitions but use categories for
reporting (the "free-within-category" middle ground). Sentry's transitions
are system-driven (auto-escalate, auto-regress). Attune adopts **strict
enforcement** — invalid transitions return 409 with the list of allowed
next states. Strict was chosen over free-within-category because the audit
trail is more meaningful when transitions are intentional, and the default
seed includes a generous transition set so operators don't hit 409s on
common paths. Operators who want free transitions can add all edges via the
transition editor.

**4. Resolution is a separate dimension (6/10).**
GitHub (`state_reason`), ServiceNow (`close_code` + `close_notes`), Sentry
(`statusDetails`), Jira (`resolution`), Salesforce (`Case Reason`), and
Zendesk (custom dropdown) all separate "why was this closed" from the
status itself. Attune handles this by allowing multiple states under the
`closed` category — "已修复", "不处理", "重复" are each a distinct closed
state, avoiding the status+resolution two-axis sync bugs that plague Jira.

**5. Audit trail is field-level (enterprise standard).**
Zendesk ticket audits and ServiceNow `sys_audit` record
`field_name + old_value + new_value + changed_by + timestamp` per change.
Attune adopts this granularity in `feedback_audit_log`.

**6. Default workflow template is standard (7/10).**
Shortcut ships a "Standard" workflow, Zendesk seeds 6 statuses, Intercom
seeds 4 states, ServiceNow ships 6 incident states. Attune seeds a 5-state
default workflow per tenant.

### Data model: registry + transition edge table

| System | State storage | Transition storage | Category field |
|---|---|---|---|
| **Jira** | `status` table (id, name, statusCategory) | `workflow_transition` (id, name, from, to, conditions) | `statusCategory` (3 values) |
| **ServiceNow** | `sys_choice` (integer key + label) | State Model admin (from/to pairs) | Implicit in integer ordering + `active` boolean |
| **Shortcut** | `workflow_state` (id, name, type, color, position) | Free (no constraint table) | `type` enum (backlog/unstarted/started/done) |
| **Zendesk** | `custom_status` (id, agent_label, end_user_label, status_category) | System-defined per category | `status_category` (5 values) |
| **attune (proposed)** | `tenant_workflow_states` (id, name, color, category, position) | `tenant_workflow_transitions` (from_state_id, to_state_id) | `category` enum (open/active/closed) |

**Conclusion.** A **state registry + edge table** is the relational
standard. Shortcut proves you can skip the edge table with free transitions,
but attune's requirement for strict enforcement makes the edge table
necessary.

### API shape: dedicated transition endpoint

| System | Transition API |
|---|---|
| **Jira** | `POST /issue/{key}/transitions` with `{ transition: { id } }` |
| **Sentry** | `PUT /issues/{id}/` with `{ status, substatus, statusDetails }` |
| **PagerDuty** | `PUT /incidents/{id}` with `{ status }` |
| **Zendesk** | `PUT /tickets/{id}` with `{ status }` or `{ custom_status_id }` |
| **ServiceNow** | `PATCH /table/incident/{sys_id}` with `{ state }` |
| **GitHub** | `PATCH /issues/{number}` with `{ state, state_reason }` |
| **attune (proposed)** | `POST /feedback/{id}/transition` with `{ to_state_id, comment }` |

Dedicated transition endpoint (not generic PATCH) because:
- Transition validation is non-trivial (graph lookup + audit insert).
- The `comment` field is transition-specific, not a generic update.
- Jira uses the same pattern — transitions are a sub-resource, not a field
  update.

## Proposal

### Data model

#### New tables

**`tenant_workflow_states`** — per-tenant state registry

```sql
CREATE TABLE IF NOT EXISTS tenant_workflow_states (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name        TEXT NOT NULL,
    color       VARCHAR(7) NOT NULL DEFAULT '#6b7280',
    category    TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    is_default  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ,

    CONSTRAINT chk_ws_name_length
        CHECK (length(name) BETWEEN 1 AND 48),
    CONSTRAINT chk_ws_name_no_ctrl
        CHECK (name !~ '[\x00-\x1f\x7f]'),
    CONSTRAINT chk_ws_color_hex
        CHECK (color ~ '^#[0-9a-f]{6}$'),
    CONSTRAINT chk_ws_category
        CHECK (category IN ('open', 'active', 'closed'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ws_tenant_name
    ON tenant_workflow_states (tenant_id, name)
    WHERE archived_at IS NULL;

-- Enforces "at most one" default per tenant among active states.
-- "At least one" is enforced at the application layer: SeedDefaults
-- guarantees an initial default, and the service layer rejects
-- archiving or un-defaulting the last active default.
CREATE UNIQUE INDEX IF NOT EXISTS idx_ws_tenant_default
    ON tenant_workflow_states (tenant_id)
    WHERE is_default AND archived_at IS NULL;
```

**`tenant_workflow_transitions`** — allowed transition edges

```sql
CREATE TABLE IF NOT EXISTS tenant_workflow_transitions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    from_state_id UUID NOT NULL REFERENCES tenant_workflow_states(id) ON DELETE RESTRICT,
    to_state_id   UUID NOT NULL REFERENCES tenant_workflow_states(id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_wt_no_self_loop
        CHECK (from_state_id != to_state_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_wt_tenant_edge
    ON tenant_workflow_transitions (tenant_id, from_state_id, to_state_id);

CREATE INDEX IF NOT EXISTS idx_wt_from
    ON tenant_workflow_transitions (from_state_id);

CREATE INDEX IF NOT EXISTS idx_wt_to
    ON tenant_workflow_transitions (to_state_id);
```

`ON DELETE RESTRICT` on both state FKs — states are soft-deleted
(archived), never hard-deleted. Archiving a state also removes its
transition edges (service-layer responsibility, not CASCADE).

**`feedback_audit_log`** — field-level generic audit (serves #29 + #39)

```sql
CREATE TABLE IF NOT EXISTS feedback_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    feedback_id BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    field_name  TEXT NOT NULL DEFAULT '',
    old_value   TEXT,
    new_value   TEXT,
    comment     TEXT NOT NULL DEFAULT '',
    changed_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fal_tenant_created
    ON feedback_audit_log (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_fal_feedback_created
    ON feedback_audit_log (feedback_id, created_at DESC);
```

#### Altered table

```sql
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS workflow_state_id UUID
        REFERENCES tenant_workflow_states(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS workflow_updated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_uf_workflow_state
    ON user_feedback (tenant_id, workflow_state_id, created_at DESC)
    WHERE workflow_state_id IS NOT NULL;
```

#### Default workflow seed

Executed by `WorkflowService.SeedDefaults(ctx, tenantID)` — called on
tenant creation or when a tenant first enables workflow features.

**Idempotency:** The entire seed runs inside a serializable transaction.
State inserts use `INSERT ... ON CONFLICT (tenant_id, name) WHERE
archived_at IS NULL DO UPDATE SET name = EXCLUDED.name RETURNING id` to
always retrieve the actual row UUID (whether newly inserted or
pre-existing). Transition inserts use `ON CONFLICT DO NOTHING` on the
unique edge index. If a tenant archives a state and re-seeds, the insert
creates a new active row (the partial unique index allows this).

```
category=open    待处理     #6b7280  is_default=true  position=0
category=open    已分拣     #3b82f6                   position=1
category=active  处理中     #f59e0b                   position=2
category=closed  已修复     #10b981                   position=3
category=closed  不处理     #ef4444                   position=4

Transitions (generous set — all common paths):
  待处理 → 已分拣
  待处理 → 处理中
  待处理 → 不处理
  已分拣 → 处理中
  已分拣 → 不处理
  处理中 → 已修复
  处理中 → 不处理
  处理中 → 待处理   (un-start / back to backlog)
  已修复 → 待处理   (reopen)
  不处理 → 待处理   (reopen)
```

### Category semantics

| Category | Meaning | Behaviour |
|---|---|---|
| `open` | New / backlog / untriaged | Included in default list view |
| `active` | In progress / being worked | Included in default list view |
| `closed` | Terminal — resolved, won't fix, etc. | Hidden from default list view |

The 3-category model matches the semantic core of every benchmarked product:
Sentry (unresolved/resolved/archived), PagerDuty (triggered/acknowledged/
resolved), Shortcut (unstarted/started/done). Finer granularity is expressed
through custom state names within each category.

**Future work:** SLA clock behaviour (ticking in open/active, stopped in
closed) and auto-transitions (auto-close on inactivity, auto-reopen on new
ingest) are natural extensions once the state machine is in place. Not in
scope for this PR.

### Relationship with existing systems

| System | Column | Owner | Mutable by operator? |
|---|---|---|---|
| Enrichment pipeline | `enrichment_status` | Enricher service | No |
| Manual tags | `feedback_tag_assignments` | Operator | Yes |
| **Workflow status** | `workflow_state_id` | **Operator** | **Yes** |

All three are orthogonal. A feedback row can be:
- `enrichment_status=done` + `workflow_state=待处理` (enriched but untriaged)
- `enrichment_status=failed` + `workflow_state=处理中` (enrichment failed, operator investigating)
- Tags `bug`, `需要跟进` + `workflow_state=已分拣` (triaged and tagged)

### API

#### Permissions

State/transition **management** (CRUD, graph editing) is **admin-only** —
routes mount under `requireAdmin` middleware (like LLM config). Individual
feedback **transitions** (including batch) and audit reads are available to
any authenticated session user (like tagging).

#### State management

| Method | Path | Body / Response |
|---|---|---|
| GET | `/fb/v1/console/workflow/states` | → `{ states: WorkflowState[] }` (includes archived) |
| POST | `/fb/v1/console/workflow/states` | `{ name, color, category, position }` → `WorkflowState` |
| PATCH | `/fb/v1/console/workflow/states/{id}` | `{ name?, color?, position?, is_default? }` → `WorkflowState` |
| DELETE | `/fb/v1/console/workflow/states/{id}` | → `ArchiveWorkflowStateResponse{}` (200, soft-archive; 409 if state has active feedback or is last default) |

PATCH (not PUT) for partial update — consistent with the tag update
endpoint pattern.

#### Transition graph management

| Method | Path | Body / Response |
|---|---|---|
| GET | `/fb/v1/console/workflow/transitions` | → `{ transitions: WorkflowTransition[] }` |
| PUT | `/fb/v1/console/workflow/transitions` | `{ transitions: [{from_state_id, to_state_id}] }` → `{ transitions }` |

`PUT` replaces the full graph atomically — the transition graph is an
indivisible unit. Partial add/remove invites inconsistency. An empty
`transitions` array clears the graph (all transitions removed — effectively
makes all status changes invalid until re-configured). No ETag /
optimistic concurrency — last writer wins, acceptable for a settings-level
resource edited infrequently by admins.

#### Feedback list filter extension

Add `workflow_state_id` and `workflow_category` to `ListFeedbackRequest`:

```protobuf
optional string workflow_state_id  = 7;  // filter by specific state
optional string workflow_category  = 8;  // "open" | "active" | "closed"
```

Default list view: `workflow_category` defaults to showing `open` + `active`
\+ feedback with `workflow_state_id IS NULL` (no state assigned). Explicitly
passing `workflow_category=closed` or `workflow_category=all` overrides.

#### Feedback status transition

| Method | Path | Body / Response |
|---|---|---|
| POST | `/fb/v1/console/feedback/{id}/transition` | `{ to_state_id, comment? }` → `{ feedback_id, from_state, to_state, allowed_next_states }` |
| POST | `/fb/v1/console/feedback/transition/batch` | `{ feedback_ids[], to_state_id, comment? }` → `{ succeeded, failed[] }` |

The single-transition response includes `allowed_next_states` — the valid
transitions from the **new** state. This lets the UI update the dropdown
without an extra round-trip. The `Feedback` and `FeedbackDetail` proto
messages also carry `repeated WorkflowState allowed_next_states` so the
list/detail views can render the dropdown without fetching the full graph.

**Error codes** — new values added to the `ErrorCode` enum in
`proto/attune/v1/common.proto`:

| Code | HTTP | Meaning |
|---|---|---|
| `INVALID_TRANSITION` | 409 | Transition not in graph; response includes `allowed: WorkflowState[]` |
| `WORKFLOW_STATE_NOT_FOUND` | 404 | Target state archived or does not exist |
| `NO_WORKFLOW_STATE` | 422 | Feedback has no current workflow state |

These follow the existing enum pattern (`BAD_ID`, `TENANT_NOT_FOUND`,
etc.) — not string literals. The lint-errorcode gate enforces this.

**Batch transition** uses **partial-success** semantics: each feedback is
validated independently. The response reports succeeded count and per-item
failures with individual error codes. This is a conscious divergence from
the existing `BatchUpdateFeedbackTagsResponse` (which returns only
`affected: int32`) — the richer shape is needed because transition failures
are expected (different source states → different valid targets), whereas
tag add/remove failures are exceptional. A follow-up may align the tag
batch response to this shape.

#### Audit timeline

| Method | Path | Response |
|---|---|---|
| GET | `/fb/v1/console/feedback/{id}/audit?cursor=&limit=` | `{ entries: AuditEntry[], next_cursor? }` ordered by created_at DESC |

Cursor-based pagination following the existing `ListFeedbackRequest`
pattern. Default `limit=50`.

### Backend layering

```
handlers/console/workflow/         → HTTP: state CRUD + transition graph (admin-only)
handlers/console/feedback/         → HTTP: transition + batch-transition + audit
service/workflow/                  → WorkflowService: validation, seed, transition logic
repo/workflowstate/                → State + transition CRUD
repo/feedbackaudit/                → Audit log write + query
```

**Package placement follows existing conventions:**
- `handlers/console/workflow/` parallels `handlers/console/tag/`
- `service/workflow/` is a new service package (justified: transition logic
  orchestrates multiple repos in a transaction — unlike tags which are
  handler-direct-to-repo)
- `repo/workflowstate/` parallels `repo/feedbacktag/`
- `repo/feedbackaudit/` parallels `repo/feedbacktagassignment/`

**Handler wiring:** The transition/batch-transition/audit endpoints live
under `handlers/console/feedback/` but need `WorkflowService`. Following
the existing setter pattern (`SetDrafter`, `SetTagAssignments`), add:

```go
func (h *FeedbackHandler) SetWorkflow(w workflowTransitioner) { h.workflow = w }
```

Where `workflowTransitioner` is a narrow interface:

```go
type workflowTransitioner interface {
    Transition(ctx context.Context, tenantID string, feedbackID int64,
        toStateID string, byUser string, comment string) (*TransitionResult, error)
    BatchTransition(ctx context.Context, tenantID string, feedbackIDs []int64,
        toStateID string, byUser string, comment string) (*BatchResult, error)
}
```

`setup.go` constructs `WorkflowService`, then calls
`feedbackHandler.SetWorkflow(workflowSvc)`. The `workflow/` handler is
registered via a new `mountWorkflow(m)` call in `mountSession` under the
`requireAdmin` middleware group.

**`WorkflowService` — interface-based for testability:**

```go
type StateStore interface {
    ListActive(ctx context.Context, tenantID string) ([]WorkflowState, error)
    Get(ctx context.Context, id string) (*WorkflowState, error)
    CheckTransition(ctx context.Context, tenantID, fromID, toID string) (bool, error)
    AllowedNext(ctx context.Context, tenantID, fromID string) ([]WorkflowState, error)
    // ... CRUD methods
}

type AuditWriter interface {
    Write(ctx context.Context, tx pgx.Tx, entry AuditEntry) error
    List(ctx context.Context, feedbackID int64, cursor string, limit int) ([]AuditEntry, string, error)
}

type WorkflowService struct {
    states StateStore
    audits AuditWriter
    pool   *pgxpool.Pool  // shared pool for cross-repo transactions
}
```

Service depends on interfaces, not concrete repo pointers — following the
`guardpolicy.Store` / `llmconfig.Repo` pattern. Unit tests mock `StateStore`
and `AuditWriter`; integration tests pass real repos.

**Core methods:**

```go
func (s *WorkflowService) Transition(ctx, tenantID, feedbackID, toStateID, byUser, comment) (*TransitionResult, error)
func (s *WorkflowService) BatchTransition(ctx, tenantID, feedbackIDs, toStateID, byUser, comment) (*BatchResult, error)
func (s *WorkflowService) SeedDefaults(ctx, tenantID) error
func (s *WorkflowService) ValidateTransition(ctx, tenantID, fromStateID, toStateID) (bool, []WorkflowState, error)
```

**Transaction boundary for `Transition`:**

The transaction spans feedback repo + state repo + audit repo. All three
repos share the same `pgxpool.Pool`, so a `pgx.Tx` obtained from the pool
can be passed to tx-aware methods on each repo. Pattern follows
`MarkDoneTx`:

```
BEGIN
  SELECT workflow_state_id FROM user_feedback WHERE id = $1 FOR UPDATE
  -- archived_at filter: reject transitions from/to archived states
  SELECT 1 FROM tenant_workflow_transitions wt
    JOIN tenant_workflow_states fs ON fs.id = wt.from_state_id
    JOIN tenant_workflow_states ts ON ts.id = wt.to_state_id
    WHERE wt.tenant_id = $2
      AND wt.from_state_id = $current AND wt.to_state_id = $target
      AND fs.archived_at IS NULL AND ts.archived_at IS NULL
  UPDATE user_feedback
    SET workflow_state_id = $target, workflow_updated_at = NOW()
    WHERE id = $1
  INSERT INTO feedback_audit_log (
    tenant_id, feedback_id, entity_type, field_name,
    old_value, new_value, comment, changed_by
  ) VALUES ($2, $1, 'workflow_state', 'workflow_state_id',
    $current_name, $target_name, $comment, $byUser)
COMMIT
```

`FOR UPDATE` row-lock prevents concurrent transition races — consistent
with the enrichment pipeline's `TryClaim` pattern. The transition validation
JOINs `tenant_workflow_states` to filter out archived states (C6 fix).

**Archiving a state:** The service layer handles this atomically:
1. Reject if the state has active feedback rows pointing to it (409).
2. Reject if the state is the last active default (409).
3. Delete all transition edges referencing the state from
   `tenant_workflow_transitions`.
4. Set `archived_at = NOW()` on the state.

**Error handling:** Sentinel errors in the service layer, mapped by
handlers:

```go
var (
    ErrInvalidTransition = errors.New("transition not allowed")
    ErrStateNotFound     = errors.New("workflow state not found")
    ErrNoWorkflowState   = errors.New("feedback has no workflow state")
    ErrLastDefault       = errors.New("cannot remove last default state")
)
```

Handlers use `errors.Is()` to map to `(httpStatus, ErrorCode_*)` — same
pattern as `feedbacktag.ErrNotFound` → `ErrorCode_NOT_FOUND`.

### Observability

**Metrics** (in `internal/infra/metrics/`):
- `attune_workflow_transitions_total` — counter, labels: `tenant_id`,
  `result` (`success` | `invalid` | `error`)
- `attune_workflow_batch_size` — histogram of batch transition request sizes

**Logging** (via `logext`, never `slog` directly):
- `logext.Infof` on successful transitions (tenant, feedback ID, from→to,
  actor)
- `logext.Warnf` on 409 invalid transitions (tenant, feedback ID, attempted
  from→to)
- `logext.Errorf` on unexpected failures

Handler-level logging uses the `const where = "console.WorkflowHandler.*"`
pattern.

### Proto definition

New file `proto/attune/v1/workflow.proto`:

```protobuf
syntax = "proto3";
package attune.v1;

message WorkflowState {
  string id          = 1;
  string name        = 2;
  string color       = 3;
  string category    = 4;  // "open" | "active" | "closed" — kept as string
                            // (not enum) for consistency with enrichment_status
                            // and other string-typed fixed sets in the project
  int32  position    = 5;
  bool   is_default  = 6;
  bool   archived    = 7;  // derived from archived_at != nil in the repo layer
  string created_at  = 8;
  string updated_at  = 9;
}

message WorkflowTransition {
  string id            = 1;
  string from_state_id = 2;
  string to_state_id   = 3;
}

message TransitionRequest {
  string to_state_id = 1;
  string comment     = 2;
}

message TransitionResponse {
  int64         feedback_id = 1;
  WorkflowState from_state  = 2;
  WorkflowState to_state    = 3;
}

message BatchTransitionRequest {
  repeated int64 feedback_ids = 1;
  string         to_state_id  = 2;
  string         comment      = 3;
}

message BatchTransitionFailure {
  int64  feedback_id = 1;
  string code        = 2;
  string message     = 3;
}

message BatchTransitionResponse {
  int32                          succeeded = 1;
  repeated BatchTransitionFailure failed   = 2;
}

message AuditEntry {
  int64  id          = 1;
  int64  feedback_id = 2;
  string entity_type = 3;
  string field_name  = 4;
  string old_value   = 5;
  string new_value   = 6;
  string comment     = 7;
  string changed_by  = 8;
  string created_at  = 9;
}

message ListAuditRequest {
  int64           feedback_id = 1;  // path param
  optional string cursor      = 2;
  optional int32  limit       = 3;  // default 50
}

message ListAuditResponse {
  repeated AuditEntry entries    = 1;
  optional string     next_cursor = 2;
}
```

Extend `ingest.proto`:

```protobuf
// In Feedback (list-level): last existing field is tags = 16
optional WorkflowState         workflow_state       = 17;
repeated WorkflowState         allowed_next_states  = 18;

// In FeedbackDetail: last existing field is tags = 25
optional WorkflowState         workflow_state       = 26;
repeated WorkflowState         allowed_next_states  = 27;

// In ListFeedbackRequest: last existing field is tag_id = 6
optional string workflow_state_id  = 7;
optional string workflow_category  = 8;  // "open"|"active"|"closed"|"all"
```

`allowed_next_states` on each feedback response lets the UI render the
transition dropdown without a separate graph fetch.

### Console UI

#### File placement

```
src/components/workflow/
  workflow-state-badge.tsx        — shared badge (used by feedback list + detail)
src/features/workflow/
  api/                            — query hooks (list-states, transitions, etc.)
  components/
    workflow-settings-page.tsx     — state list + transition matrix for settings
    state-form-dialog.tsx          — create/edit state dialog
    transition-matrix.tsx          — the matrix editor
src/features/feedback/
  components/
    workflow-transition-dropdown.tsx  — inline transition dropdown
    audit-timeline.tsx               — audit log timeline
```

`WorkflowStateBadge` lives in `src/components/` (shared layer) so both
`features/feedback/` and `features/workflow/` can import it without
violating the `no-cross-feature` dependency-cruiser rule. Query hooks in
`features/workflow/api/` are called from route files
(`routes/_authed.feedback.tsx`, `routes/_authed.settings.tsx`) and passed
down as props — exactly like `enrichConfigQuery` → `dims` today.

#### Feedback list page

- **Status column** (new): `WorkflowStateBadge` — coloured pill showing
  state name + category icon. Placed **before** the dimensions columns
  (high-frequency triage signal). To avoid horizontal overflow on 1366px
  viewports, the status badge renders as a compact icon + truncated name
  (max ~6rem). Clicking opens a dropdown of valid next states (from
  `allowed_next_states` on the `Feedback` response — no extra fetch).
- **Null state handling**: feedback with `workflow_state_id = null` shows
  "—" (em dash, matching existing `f.userId || '--'` pattern). Clicking
  the dash opens a dropdown with states in the `open` category (including
  the default state) as initial assignment targets.
- **Status filter** (new): dropdown in filter bar, filtering by
  `workflow_category`. Default: `open` + `active` + null (no state).
  Explicit "已关闭" and "全部" options available.
- **Bulk transition**: extend `SelectionActionBar` with a "转换状态"
  button. The dropdown shows the **intersection** of valid next states
  across all selected feedback. If the intersection is empty, the button
  is disabled with a tooltip: "所选反馈没有共同的可用状态转换". Feedback
  with `workflow_state_id = null` are excluded from the intersection
  computation and will fail individually in the batch response (reported
  as `NO_WORKFLOW_STATE` per-item errors).
- **Workflow visibility**: the entire status column, filter, and bulk
  transition button are hidden when the tenant has zero non-archived
  workflow states (detected via `GET /workflow/states` returning no
  active states).

#### Feedback detail sheet

- **Status section** (new, above tags): current state badge + dropdown to
  transition (same `allowed_next_states` source). Optional comment
  textarea appears on dropdown selection before confirming. For null-state
  feedback, shows "设置初始状态" prompt with open-category states.
- **Audit timeline** (new, below tags): vertical timeline with `border-l`
  connecting line (Tailwind, no library). Each entry: dot on the line,
  timestamp + actor on the left, field change description (old→new) on
  the right, comment below. Initial load: 50 entries. "加载更多" link
  triggers cursor-based pagination. `changed_by` renders as-is (session
  user display name stored at write time).

#### Settings → Workflow page

- **State list**: `Table` component showing name (editable inline), color
  swatch, category badge, position. **Up/down arrow buttons** for
  reordering (no DnD library dependency — avoids new dependency per
  CLAUDE.md §8). Archive button with confirmation dialog: "归档后，已
  归档状态在现有反馈上仍然可见，但不能分配给新条目。"
- **State form dialog**: reuses the existing `PALETTE` swatch pattern from
  `TagFormDialog` (12-colour fixed palette, not arbitrary hex input).
  Fields: name, category (select), color (swatch).
- **Transition matrix editor**: a `Table` where rows = from-states,
  columns = to-states, cells = `Checkbox` toggles. Diagonal cells are
  disabled (no self-loops). States are grouped by category for readability.
  Save button sends `PUT /workflow/transitions` with all checked cells.
  **Confirmation dialog** on save if removing edges that have active
  feedback in the from-state: "移除的转换路径中，有 N 条反馈处于起始状态。
  继续保存？" No external library needed — built from existing `Table` +
  `Checkbox` UI primitives.

#### i18n additions

All new UI strings added to `zh-CN.json`. Key groups:
- `workflow.states.*` — state CRUD labels
- `workflow.transitions.*` — transition editor labels
- `workflow.audit.*` — audit timeline labels
- `workflow.badge.*` — badge and dropdown labels
- `workflow.bulk.*` — bulk transition labels
- `workflow.filter.*` — filter dropdown labels

### Migration file

`030_workflow_states.sql` — contains all DDL from this proposal in a single
migration. Follows the conventions established by `029_feedback_tags.sql`:
`IF NOT EXISTS`, no explicit `BEGIN`/`COMMIT`, CHECK constraints with
explicit names, RESTRICT on tenant FK.

## Alternatives considered

### A. Reuse `exclusive_scope` tags as workflow status

The tag system already has `exclusive_scope` (one tag per scope per
feedback). A scope named `"workflow"` with tags "待处理", "处理中", "已修复"
would superficially work.

**Rejected because:**
- Tags have no category semantics — cannot distinguish "open" from "closed"
  for default filtering and reporting.
- Tags have no transition constraints — any tag can be applied at any time.
- Audit granularity differs — tag audit is per-assignment, not per-field.
- Conflating classification (tags) with lifecycle (status) contradicts the
  universal industry pattern (10/10 products separate them).

### B. Fixed enum in `user_feedback` column

Add `workflow_status TEXT CHECK (IN ('open','triaged','in_progress','fixed',
'wontfix'))` directly on the feedback table. No registry, no transitions.

**Rejected because:**
- Not tenant-customisable. Different teams have different workflows.
- Adding a new status requires a migration.
- No transition enforcement — free-form updates.
- This is the approach the original issue proposed, and it maps to the
  simplest end of the spectrum (GitHub's 2-state model). The user explicitly
  chose the fully customisable model.

### C. JSONB transition graph on state row

Store `allowed_next_ids JSONB` on each state row instead of a separate
transition table.

**Rejected because:**
- JSONB arrays cannot have FK constraints — deleting a state leaves
  dangling references in other rows' `allowed_next_ids`.
- Violates the project's relational-first convention.
- Makes "list all transitions" a full-table scan + JSON unnest instead of
  a simple `SELECT * FROM transitions`.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| **Complexity**: full state machine is significantly more code than a fixed enum | The data model is 3 tables; the service layer is one `Transition` method with a graph lookup. Complexity is bounded. |
| **Orphaned feedback**: archiving a state that feedback rows reference | `ON DELETE RESTRICT` on FK prevents deletion; archive (soft-delete) keeps the state readable but hidden from new-state selection. Service rejects archiving if active feedback references the state. |
| **Archived-state transitions**: transitions to/from archived states | Transition validation query JOINs `tenant_workflow_states` with `archived_at IS NULL` filter. Archiving a state also deletes its transition edges from `tenant_workflow_transitions`. |
| **Default state invariant**: "at least one default" not enforceable by index alone | Partial unique index enforces "at most one". Service layer rejects archiving or un-defaulting the last active default (returns 409 `LAST_DEFAULT`). |
| **Seed conflicts**: `SeedDefaults` called concurrently | Serializable transaction. State upserts use `ON CONFLICT DO UPDATE SET name = EXCLUDED.name RETURNING id` to always retrieve the actual row UUID, avoiding FK failures on transition inserts. |
| **Performance**: transition validation adds a query per transition | Single indexed lookup on `(tenant_id, from_state_id, to_state_id)` — microseconds. Batch transitions loop but each is a point query. |
| **Migration weight**: 3 new tables + 2 altered columns | One migration file, all DDL idempotent (`IF NOT EXISTS`). No data migration — `workflow_state_id` is nullable for existing rows. |
| **Batch partial-success UX**: mixed outcomes may confuse operators | Console shows a toast with "N 条成功, M 条失败" and per-item error reasons. Failed items remain in their original state (no partial corruption). |
| **Tag/workflow overlap**: operators might create tags with `exclusive_scope = "status"` | Documented as separate systems. Guard can be added later if confusion is observed; not enforcing a denylist in v1 to keep scope bounded. |

## Implementation plan

| Phase | Scope | Depends on |
|---|---|---|
| **T1** | Migration `030_workflow_states.sql` — all DDL | — |
| **T2** | Proto `workflow.proto` + `make proto` | T1 |
| **T3** | `repo/workflowstate/` — state + transition CRUD | T1 |
| **T4** | `repo/feedbackaudit/` — audit log write + query | T1 |
| **T5** | `service/workflow/` — SeedDefaults, Transition, BatchTransition, ValidateTransition | T3, T4 |
| **T6** | `handlers/console/workflow/` — state CRUD + transition graph endpoints | T5 |
| **T7** | `handlers/console/feedback/` — transition + batch-transition + audit endpoints | T5 |
| **T8** | Extend feedback list/detail queries to JOIN workflow state | T3 |
| **T9** | Integration tests (state CRUD, transition enforcement, batch, audit) | T5 |
| **T10** | Console: WorkflowStateBadge + status column + filter | T7 |
| **T11** | Console: detail sheet status section + transition dropdown | T7 |
| **T12** | Console: audit timeline in detail sheet | T7 |
| **T13** | Console: SelectionActionBar bulk transition | T7 |
| **T14** | Console: Settings → Workflow page (state list + transition editor) | T6 |
| **T15** | Console: i18n zh-CN additions | T10–T14 |
| **T16** | CHANGELOG.md update | T15 |

## Verification

- [ ] **Service unit tests** (mock `StateStore` / `AuditWriter`):
  exhaustive coverage of allowed/denied transitions, batch
  partial-success, SeedDefaults idempotency, archive-last-default
  rejection, archived-state transition rejection
- [ ] **Repo integration tests** (PG test container): state CRUD,
  transition CRUD, transition enforcement (valid + 409), audit log
  writes + cursor pagination, default-state uniqueness, FK cascade on
  feedback delete → audit cleanup
- [ ] **Handler tests**: 200 path, 409 invalid transition, 404 archived
  state, 422 no-workflow feedback, batch partial-success, permission
  gates (admin-only for state management)
- [ ] **Console vitest**: WorkflowStateBadge rendering, transition
  dropdown (with allowed_next_states), filter (default excludes closed),
  bulk transition (intersection logic, disabled when empty), audit
  timeline pagination, null-state handling
- [ ] **End-to-end via httptest**: create tenant → seed defaults → ingest
  feedback → transition through full lifecycle → verify audit trail →
  archive state → verify transition rejection
- [ ] **Real-environment run**: start server with workflow seed, use
  Console to transition feedback, verify audit timeline renders, test
  bulk transition, test settings page matrix editor

## References

- ServiceNow State Model: https://docs.servicenow.com/en-US/bundle/utah-it-service-management/page/product/incident-management/concept/c_IncidentManagementStateModel.html
- Jira Workflows: https://support.atlassian.com/jira-cloud-administration/docs/work-with-issue-workflows/
- Linear Configuring Workflows: https://linear.app/docs/configuring-workflows
- Sentry Issue States: https://docs.sentry.io/product/issues/states-triage/
- Zendesk Custom Ticket Statuses: https://support.zendesk.com/hc/en-us/articles/4412575841306
- Intercom Ticket States: https://www.intercom.com/help/en/articles/9730130-how-ticket-states-work
- Shortcut Workflows: https://help.shortcut.com/hc/en-us/articles/115001100606
- GitHub state_reason: https://docs.github.com/en/rest/issues/issues#update-an-issue
- Zendesk Ticket Audits API: https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_audits/
- ServiceNow sys_audit: https://docs.servicenow.com/bundle/vancouver-platform-security/page/administer/security/concept/c_UnderstandingTheSysAuditTable.html
