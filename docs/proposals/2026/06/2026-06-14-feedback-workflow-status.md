# Customisable feedback workflow status

| | |
|---|---|
| **Issue** | #29 |
| **Status** | Proposed |
| **Started** | 2026-06-14 CST |
| **Related** | #28 (manual tags — orthogonal labelling layer), #39 (generic audit log — the `feedback_audit_log` table introduced here is designed to serve both), #19 (proto IDL contract) |

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
filtering, SLA behaviour, and board views — they are the system-meaningful
anchor that custom names alone cannot provide.

Attune adopts **3 categories**: `open` (new / backlog), `active` (in
progress / being worked), `closed` (terminal — resolved, won't-fix, etc.).
Three is the minimum that preserves semantic value — it matches Sentry
(unresolved / resolved / archived) and PagerDuty (triggered / acknowledged /
resolved) while leaving room for arbitrary custom names within each.

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
reporting. Sentry's transitions are system-driven (auto-escalate,
auto-regress). Attune adopts **strict enforcement** — invalid transitions
return 409 with the list of allowed next states.

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

CREATE UNIQUE INDEX IF NOT EXISTS idx_ws_tenant_default
    ON tenant_workflow_states (tenant_id)
    WHERE is_default AND archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ws_tenant
    ON tenant_workflow_states (tenant_id)
    WHERE archived_at IS NULL;
```

**`tenant_workflow_transitions`** — allowed transition edges

```sql
CREATE TABLE IF NOT EXISTS tenant_workflow_transitions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    from_state_id UUID NOT NULL REFERENCES tenant_workflow_states(id) ON DELETE CASCADE,
    to_state_id   UUID NOT NULL REFERENCES tenant_workflow_states(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_wt_no_self_loop
        CHECK (from_state_id != to_state_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_wt_tenant_edge
    ON tenant_workflow_transitions (tenant_id, from_state_id, to_state_id);

CREATE INDEX IF NOT EXISTS idx_wt_from
    ON tenant_workflow_transitions (from_state_id);
```

**`feedback_audit_log`** — field-level generic audit (serves #29 + #39)

```sql
CREATE TABLE IF NOT EXISTS feedback_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    feedback_id BIGINT NOT NULL,
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
tenant creation or when a tenant first enables workflow features:

```
category=open    待处理     #6b7280  is_default=true  position=0
category=open    已分拣     #3b82f6                   position=1
category=active  处理中     #f59e0b                   position=2
category=closed  已修复     #10b981                   position=3
category=closed  不处理     #ef4444                   position=4

Transitions:
  待处理 → 已分拣
  待处理 → 不处理
  已分拣 → 处理中
  已分拣 → 不处理
  处理中 → 已修复
  处理中 → 不处理
  已修复 → 待处理   (reopen)
  不处理 → 待处理   (reopen)
```

### Category semantics

| Category | Meaning | Behaviour |
|---|---|---|
| `open` | New / backlog / untriaged | Default list view; SLA clock ticking |
| `active` | In progress / being worked | Default list view; SLA clock ticking |
| `closed` | Terminal — resolved, won't fix, etc. | Hidden from default list; SLA clock stopped |

The 3-category model matches the semantic core of every benchmarked product:
Sentry (unresolved/resolved/archived), PagerDuty (triggered/acknowledged/
resolved), Shortcut (unstarted/started/done). Finer granularity is expressed
through custom state names within each category.

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

#### State management

| Method | Path | Body / Response |
|---|---|---|
| GET | `/fb/v1/console/workflow/states` | → `{ states: WorkflowState[] }` (includes archived) |
| POST | `/fb/v1/console/workflow/states` | `{ name, color, category, position }` → `WorkflowState` |
| PUT | `/fb/v1/console/workflow/states/{id}` | `{ name?, color?, position?, is_default? }` → `WorkflowState` |
| DELETE | `/fb/v1/console/workflow/states/{id}` | → 204 (soft-archive; 409 if state has active feedback) |

#### Transition graph management

| Method | Path | Body / Response |
|---|---|---|
| GET | `/fb/v1/console/workflow/transitions` | → `{ transitions: WorkflowTransition[] }` |
| PUT | `/fb/v1/console/workflow/transitions` | `{ transitions: [{from_state_id, to_state_id}] }` → `{ transitions }` |

`PUT` replaces the full graph atomically — the transition graph is an
indivisible unit. Partial add/remove invites inconsistency.

#### Feedback status transition

| Method | Path | Body / Response |
|---|---|---|
| POST | `/fb/v1/console/feedback/{id}/transition` | `{ to_state_id, comment? }` → `{ feedback_id, from_state, to_state }` |
| POST | `/fb/v1/console/feedback/transition/batch` | `{ feedback_ids[], to_state_id, comment? }` → `{ succeeded, failed[] }` |

Error codes:
- `409 INVALID_TRANSITION` — transition not in graph; response includes
  `allowed: WorkflowState[]`
- `404 STATE_NOT_FOUND` — target state archived or does not exist
- `422 FEEDBACK_NO_STATE` — feedback has no current workflow state
  (workflow not yet enabled for this tenant)

Batch transition uses **partial-success** semantics: each feedback is
validated independently. The response reports succeeded count and per-item
failures.

#### Audit timeline

| Method | Path | Response |
|---|---|---|
| GET | `/fb/v1/console/feedback/{id}/audit` | `{ entries: AuditEntry[] }` ordered by created_at DESC |

### Backend layering

```
handlers/console/workflow/         → HTTP: state CRUD + transition graph
handlers/console/feedback/         → HTTP: transition + batch-transition + audit
service/workflow/                  → WorkflowService: validation, seed, transition logic
repo/workflowstate/                → State + transition CRUD
repo/feedbackaudit/                → Audit log write + query
```

**Package placement follows existing conventions:**
- `handlers/console/workflow/` parallels `handlers/console/tag/`
- `service/workflow/` is a new service package (no existing `service/` package to extend)
- `repo/workflowstate/` parallels `repo/feedbacktag/`
- `repo/feedbackaudit/` parallels `repo/feedbacktagassignment/`

**`WorkflowService` core methods:**

```go
type WorkflowService struct {
    stateRepo *workflowstate.Repo
    auditRepo *feedbackaudit.Repo
    fbRepo    *feedback.Repo
}

func (s *WorkflowService) Transition(ctx, tenantID, feedbackID, toStateID, byUser, comment) error
func (s *WorkflowService) BatchTransition(ctx, tenantID, feedbackIDs, toStateID, byUser, comment) (BatchResult, error)
func (s *WorkflowService) SeedDefaults(ctx, tenantID) error
func (s *WorkflowService) ValidateTransition(ctx, tenantID, fromStateID, toStateID) (bool, []WorkflowState, error)
```

**Transaction boundary for `Transition`:**

```
BEGIN
  SELECT workflow_state_id FROM user_feedback WHERE id = $1 FOR UPDATE
  SELECT 1 FROM tenant_workflow_transitions
    WHERE tenant_id = $2 AND from_state_id = $current AND to_state_id = $target
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
with the enrichment pipeline's `TryClaim` pattern.

### Proto definition

New file `proto/attune/v1/workflow.proto`:

```protobuf
syntax = "proto3";
package attune.v1;

message WorkflowState {
  string id          = 1;
  string name        = 2;
  string color       = 3;
  string category    = 4;  // "open" | "active" | "closed"
  int32  position    = 5;
  bool   is_default  = 6;
  bool   archived    = 7;
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
```

Extend `ingest.proto` `Feedback` and `FeedbackDetail` messages:

```protobuf
optional WorkflowState workflow_state = 17;  // in Feedback
optional WorkflowState workflow_state = 25;  // in FeedbackDetail
```

### Console UI

#### Feedback list page

- **Status column** (new): `WorkflowStateBadge` — coloured pill showing
  state name + category icon. Clicking opens a dropdown of valid next
  states (queried from transition graph).
- **Status filter** (new): single-select dropdown in filter bar, filtering
  by `workflow_state_id`. Default view excludes `closed`-category states.
- **Bulk transition**: extend `SelectionActionBar` with a "转换状态"
  button that opens a combobox of target states. Validates that all selected
  feedback share at least one common valid transition.

#### Feedback detail sheet

- **Status section** (new, above tags): current state badge + dropdown to
  transition. Optional comment textarea shown on click.
- **Audit timeline** (new, below tags): chronological list of all field
  changes from `feedback_audit_log`. Each entry shows: timestamp, actor,
  field, old→new, comment.

#### Settings → Workflow page

- **State list**: table of states with name, color, category, position.
  Drag-to-reorder, inline edit, archive button.
- **Transition editor**: visual matrix or adjacency list showing which
  transitions are allowed. Toggle cells to enable/disable edges. Save
  submits `PUT /workflow/transitions` atomically.

#### i18n additions

All new UI strings added to `zh-CN.json` under `workflow.*` namespace.

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
| **Orphaned feedback**: archiving a state that feedback rows reference | `ON DELETE RESTRICT` on FK prevents deletion; archive (soft-delete) keeps the state readable but hidden from new-state selection. |
| **Seed conflicts**: `SeedDefaults` called multiple times | Idempotent — `ON CONFLICT DO NOTHING` on (tenant_id, name). |
| **Performance**: transition validation adds a query per transition | Single indexed lookup on `(tenant_id, from_state_id, to_state_id)` — microseconds. Batch transitions loop but each is a point query. |
| **Migration weight**: 3 new tables + 2 altered columns | One migration file, all DDL idempotent (`IF NOT EXISTS`). No data migration — `workflow_state_id` is nullable for existing rows. |

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

- [ ] State machine unit tests: exhaustive coverage of allowed/denied
  transitions for the default workflow seed
- [ ] Repo integration tests (PG test container): state CRUD, transition
  CRUD, transition enforcement (valid + 409), audit log writes
- [ ] Handler tests: 200 path, 409 invalid transition, 404 archived state,
  422 no-workflow feedback, batch partial-success
- [ ] Console vitest: WorkflowStateBadge rendering, transition dropdown,
  filter, bulk transition, audit timeline
- [ ] End-to-end via httptest: create tenant → seed defaults → ingest
  feedback → transition through full lifecycle → verify audit trail
- [ ] Real-environment run: start server with workflow seed, use Console to
  transition feedback, verify audit timeline renders

## References

- ServiceNow State Model: https://docs.servicenow.com/en-US/bundle/utah-it-service-management/page/product/incident-management/concept/c_IncidentManagementStateModel.html
- Jira Workflows: https://confluence.atlassian.com/adminjiraserver/working-with-workflows-938847362.html
- Linear Configuring Workflows: https://linear.app/docs/configuring-workflows
- Sentry Issue States: https://docs.sentry.io/product/issues/states-triage/
- Zendesk Custom Ticket Statuses: https://support.zendesk.com/hc/en-us/articles/4412575841306
- Intercom Ticket States: https://www.intercom.com/help/en/articles/9730130-how-ticket-states-work
- Shortcut Workflows: https://help.shortcut.com/hc/en-us/articles/115001100606
- GitHub state_reason: https://docs.github.com/en/rest/issues/issues#update-an-issue
- Zendesk Ticket Audits API: https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_audits/
- ServiceNow sys_audit: https://docs.servicenow.com/bundle/vancouver-platform-security/page/administer/security/concept/c_UnderstandingTheSysAuditTable.html
