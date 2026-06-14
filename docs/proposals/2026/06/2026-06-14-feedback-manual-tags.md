# Manual tagging on feedback rows

| | |
|---|---|
| **Issue** | #28 |
| **Status** | Implemented |
| **Started** | 2026-06-14 CST |
| **Related** | #19 (proto IDL contract — tag endpoints follow the same proto-first flow), #114 (embedding clustering — `enriched_attrs` is the AI-generated counterpart; manual tags live separately), `2026-06-07-flat-labels.md` (metadata-driven Dimensions model — the existing AI classification layer this feature complements) |

## Problem

Feedback rows today are classified by the **Dimensions model** (`enriched_attrs`
JSONB, migration 014) — a per-tenant, metadata-driven taxonomy where the LLM
enricher fills values automatically. This works for structured classification
(`type`, `severity`, `sentiment`, `labels`) but operators routinely need
domain-specific annotations that **no AI enricher should produce**:

- `needs-customer-followup` — an action state, not a classification.
- `duplicate-of-bug-X` — cross-referencing, requires operator context.
- `demo-prep` — internal workflow, invisible to the enricher's input.
- `已确认` / `待修复` — Chinese operator vocabulary.

These are **human-applied, auditable labels with operator identity + timestamp**
— fundamentally different from AI-generated `enriched_attrs`. The issue's premise
("only filtered by `kind`, `severity`, `modules`") is outdated — those hardcoded
fields were replaced by the generic Dimensions model in migration 014 — but the
core need (manual tagging separate from AI classification) is valid.

## Code reconciliation (issue text vs. verified code)

| Issue says | Verified reality | Decision |
|---|---|---|
| "filtered by `kind`, `severity`, `modules`" | Gone since migration 014; now `enriched_attrs` JSONB with operator-defined Dimensions | Rewrite the problem statement; the need for manual tagging is real but orthogonal to Dimensions |
| "`feedback_tags(feedback_id, tag, …)`" — tag string as PK | No tag registry; no color, no autocomplete, no archive | Add a **tag registry** table (every top-tier product has one) + junction table with audit trail |
| "Tag string constraint: lowercase, kebab-case, max 32 chars" | Console is Chinese-primary (`zh-CN.json`); operators will need `需要跟进`, `重复问题` | **Drop kebab-case constraint.** Allow Unicode; max 48 chars; trim whitespace; reject control chars. |
| No mention of proto | CLAUDE.md §11: "edit the `.proto`, run `make proto`" | Proto-first — define `FeedbackTagService` messages before handler code |
| "`GET /fb/v1/console/feedback?tag=foo`" — filter by tag name string | Dimension filters use `?dim=value` (attr containment); tags are a separate axis | Filter by `tag_id` (UUID) — a name-string filter requires an extra lookup and is fragile on rename |

## Industry benchmarking

Benchmarked the label/tag subsystem across eight top-tier products spanning issue
trackers, feedback tools, and customer support — three axis: data model, API
shape, and auto-vs-manual separation.

### Data model: registry + junction is the consensus

| System | Registry | Association | Pre-create? | Metadata |
|---|---|---|---|---|
| **GitHub** | `labels` (repo-scoped, name-unique) | issue ↔ label M:M | Required | name, color (hex6 no `#`), description (max 100) |
| **Gitea** | `label` (repo OR org scoped, `Exclusive` flag) | `issue_label` junction, `UNIQUE(issue_id, label_id)` | Required | name, color (`#rrggbb`), description, `archived_unix`, exclusive + order |
| **GitLab** | `labels` (project or group, inherited) | M:M | Required | name, color (hex6 or CSS name), description, priority, `text_color` (auto), `archived` |
| **Linear** | `IssueLabel` (workspace or team) | M:M via `labelIds` | Required | name, color, description, group (1-level mutex) |
| **Canny** | `tags` (board-scoped) | post ↔ tag via `add_tag`/`remove_tag` | Required | name (1–30 chars), board, `postCount` |
| **Zendesk** | **None** (flat strings) | inline array on ticket | Ad-hoc | *(none — just strings)* |
| **Intercom** | `tag` (workspace-scoped) | attach/detach endpoints | Upsert (auto-create) | name, `applied_by`, `applied_at` |
| **Sentry** | **None** (key-value on event) | SDK-only at capture time | Ad-hoc | key, value (200 chars each) |

**6 of 8 use a registry.** Zendesk and Sentry are the exceptions — both trade
governance for friction-free entry, but Zendesk's autocomplete is limited to
"top 15 from last 60 days" (no rename, no dedup), a known pain point at scale.

**Conclusion.** A **tag registry + junction table** is the industry default.
Intercom's **upsert semantic** (auto-create on first apply) offers the best of
both: operators get autocomplete and metadata without a mandatory pre-creation
step.

### API shape: dedicated add/remove endpoints

| System | Add | Replace-all | Remove one | Remove all |
|---|---|---|---|---|
| **GitHub** | `POST /issues/{id}/labels` | `PUT` (replace set) | `DELETE /…/labels/{name}` | `DELETE /…/labels` |
| **Gitea** | `POST /issues/{idx}/labels` | `PUT` (replace set) | `DELETE /…/labels/{id}` | `DELETE /…/labels` |
| **GitLab** | via issue `PUT` (`add_labels` param) | via issue `PUT` (`labels` param) | via issue `PUT` (`remove_labels` param) | — |
| **Canny** | `POST /posts/add_tag` | — | `POST /posts/remove_tag` | — |
| **Intercom** | `POST /conversations/{id}/tags` | — | `DELETE /…/tags/{id}` | — |

**Conclusion.** Dedicated `POST` (add) and `DELETE` (remove) on the parent
resource's `/tags` sub-path is the dominant pattern. Replace-all (`PUT`) is
useful but can be deferred. attune should follow the Canny/Intercom shape (add +
remove) — simpler, less risk of accidental bulk removal.

### Auto vs manual tag separation

Only **Sentry** explicitly separates auto-generated from user-applied tags in the
UI (filter tabs: "Custom" / "Application" / "Other"). **Intercom** tracks
`applied_by` (admin vs. bot) per association. All others treat all labels
uniformly.

**Conclusion.** Structural separation (different storage) is cleaner than
flag-on-same-table. attune already has `enriched_attrs` for AI-generated
classification — manual tags live in a **separate table** with per-assignment
`created_by` / `created_at` audit. The UI renders both side-by-side (dimension
chips + tag chips) but they are visually distinguishable and independently
filterable.

### Database pattern: junction table wins for auditable tagging

| Pattern | Per-tag audit (`who`, `when`) | Tag metadata (color, description) | Rename cost | Query perf |
|---|---|---|---|---|
| **Junction table** | Natural (extra columns on junction) | Natural (columns on registry) | 1-row UPDATE | JOIN (acceptable at attune's scale) |
| **TEXT[] array + GIN** | Impossible | Impossible (no registry) | Full table UPDATE | ~7× faster reads (Crunchy Data benchmark, 10M rows) |
| **JSONB** | Impossible | Awkward | Full table UPDATE | Medium |

attune's feedback volume is bounded by tenant ingest rate (typically hundreds to
low thousands per day). The junction-table JOIN overhead is negligible at this
scale. The array pattern breaks down precisely where attune needs it: **per-tag
audit trail** (who tagged, when). The TEXT[] approach also can't store color or
description, forcing a hybrid. Start with the clean relational model; if attune
reaches 10M+ feedback rows per tenant, the migration to a materialized `tag_ids
INT[]` column with GIN is a single ALTER + trigger.

**Gitea's schema** is the closest analog: `label` table (id, repo_id/org_id,
name, color, description, exclusive, archived_unix) + `issue_label` junction
(issue_id, label_id, `UNIQUE`). attune mirrors this but scopes to tenant, adds
per-assignment audit columns, and adds the **exclusive scope** concept (Gitea's
`Exclusive` + `/` separator, GitLab's `::` scoped labels).

### Exclusive / scoped labels

| System | Mechanism | Separator | Behavior |
|---|---|---|---|
| **Gitea** | `Exclusive` bool + `ExclusiveScope()` | `/` (last occurrence) | Adding one label auto-removes sibling in same scope |
| **GitLab** | Scoped label syntax | `::` (last occurrence) | Same — mutual exclusivity within scope |
| **Linear** | Label groups | parent-child | Only one label per group per issue |

attune's Dimensions model already has `single-kind` dimensions (mutual
exclusivity within a dimension). Manual tags should support the same concept: a
tag with scope `status` (e.g., `status/待确认`, `status/已修复`) auto-removes its
sibling when applied. Implementation: an **`exclusive_scope`** column on the
registry — NULL means non-exclusive; a non-NULL string groups tags for
mutual-exclusion enforcement at the handler layer (not a DB constraint — Gitea
does this in application code too).

## Goals / Non-goals

**Goals**

1. **Tag registry** — per-tenant named tag entities with color, description,
   optional exclusive scope, and archival; upsert-on-first-apply semantic.
2. **Junction table** — per-assignment audit trail (`created_by` = session
   `UserID`, `created_at`), M:M between feedback and tags.
3. **Proto-first API** — `FeedbackTagService` in `tag.proto` (single service,
   matching the 1-service-per-file convention) with `google.api.http`
   annotations; generated Go + TS + OpenAPI. Existing `FeedbackDetail` gains
   `repeated Tag tags = 25`; `ListFeedbackRequest` gains
   `optional string tag_id = 6`.
4. **Console UI** — tag chips on feedback list/detail (colored dots, distinct from
   dimension chips), inline combobox picker with autocomplete + create-on-type,
   tag management section under Settings (`?section=tags`, matching the existing
   section-based layout). New shared components: `src/components/ui/combobox.tsx`
   (Radix Popover + Command pattern) and a 12-swatch color picker (no
   third-party dependency).
5. **Filter by tag** — `?tag=<tag_id>` on the existing
   `GET /fb/v1/console/feedback` list, AND-composed with dimension filters.
6. **Exclusive scopes** — adding a scoped tag auto-removes its sibling.
7. **Batch apply/remove** — operate on multiple feedback rows in one request.

**Non-goals**

- Replacing or modifying the Dimensions model (`enriched_attrs`). Manual tags
  and AI dimensions are orthogonal systems that happen to render side-by-side.
- Tag-based automation or triggers (e.g., "when tagged X, notify Y"). Future
  scope for #34 outbound adapter framework.
- Tag hierarchy beyond one-level exclusive scopes. No nested groups.
- Cross-tenant shared tags. Each tenant's tags are fully isolated.
- Tag import/export. Deferred until multi-tenant management tooling.
- Tag-scoped stats (`UsageByDay`, `UrgentCount`, `TopValuesByDim` filtered
  by tag). The tag registry's `usage_count` covers basic usage visibility;
  stats integration is deferred.

## Proposal

### Architecture

```
Console UI                     Console handlers                  Repo layer
─────────                      ────────────────                  ──────────
Tag Manager (Settings)    ───► TagHandler.List/Create/           tagRepo (tenant_feedback_tags)
  color picker, archive        Update/Archive
                                                                 
Feedback list/detail      ───► FeedbackHandler.List              feedbackRepo.ListForConsole
  tag chips (colored)          (adds ?tag= filter)                 + LEFT JOIN junction
  inline tag picker       ───► TagAssignmentHandler.             tagAssignmentRepo
  combobox + create            Add/Remove/BatchUpdate              (feedback_tag_assignments)
```

No new worker or background process. All operations are synchronous HTTP —
tagging is an operator action, not an async pipeline.

### Data model

**`029_feedback_tags.sql`** — the tag registry + junction table.

```sql
-- Tag registry: one row per named tag per tenant.
-- Upsert semantic: handlers auto-create on first apply if the name doesn't exist.
CREATE TABLE IF NOT EXISTS tenant_feedback_tags (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        TEXT         NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name             TEXT         NOT NULL,
    color            VARCHAR(7)   NOT NULL DEFAULT '#6b7280',
    description      TEXT         NOT NULL DEFAULT '',
    exclusive_scope  TEXT,
    archived_at      TIMESTAMPTZ,
    usage_count      INT          NOT NULL DEFAULT 0 CHECK (usage_count >= 0),
    created_by       TEXT         NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_tenant_feedback_tags_tenant
    ON tenant_feedback_tags (tenant_id) WHERE archived_at IS NULL;

-- Validation constraints.
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_name_length
    CHECK (length(name) BETWEEN 1 AND 48);
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_name_no_ctrl
    CHECK (name !~ '[\x00-\x1f\x7f]');
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_color_hex
    CHECK (color ~ '^#[0-9a-f]{6}$');
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_description_length
    CHECK (length(description) <= 200);
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_scope_length
    CHECK (exclusive_scope IS NULL OR length(exclusive_scope) BETWEEN 1 AND 32);

-- Junction table: per-assignment audit trail.
CREATE TABLE IF NOT EXISTS feedback_tag_assignments (
    feedback_id      BIGINT       NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    tag_id           UUID         NOT NULL REFERENCES tenant_feedback_tags(id) ON DELETE CASCADE,
    created_by       TEXT         NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (feedback_id, tag_id)
);
-- Reverse lookup: "all feedback with tag X" — the dominant query.
CREATE INDEX IF NOT EXISTS idx_feedback_tag_assignments_tag
    ON feedback_tag_assignments (tag_id, feedback_id);
```

**Design decisions:**

| Decision | Rationale |
|---|---|
| `UNIQUE(tenant_id, name)` — name-unique per tenant | Matches GitHub/Gitea; prevents confusing duplicates |
| `color DEFAULT '#6b7280'` (gray-500) | Sensible fallback; handler rotates a 12-color default palette on create |
| `usage_count` on registry, not computed | Avoids a `COUNT(*)` on every tag list; maintained by repo layer on add/remove (±1 in the same tx) |
| `archived_at` instead of hard delete | Gitea/GitLab pattern; archived tags remain on existing feedback but are hidden from pickers |
| `exclusive_scope` nullable | NULL = non-exclusive (default); non-NULL groups tags for mutual exclusion — Gitea's `Exclusive` + scope extraction, simplified |
| `created_by` = session `UserID` | `AuthCtx.UserID` is the surrogate UUID from `tenant_users` (`session.go:43-46`) — stable, non-PII |
| Junction PK `(feedback_id, tag_id)` | Composite PK = dedup constraint + efficient "tags for feedback Y" scan |
| tenant FK `ON DELETE RESTRICT` | Aligns with `user_feedback`'s tenant FK (`001_init.sql:53-54`, "compliance & audit trail"). Tags carry audit data (`created_by`, `created_at`) that must survive until feedback is explicitly archived. |
| junction FKs `ON DELETE CASCADE` | Deleting a feedback row removes its tag assignments; hard-deleting a tag (only via tenant deletion cascade) removes assignments. Archiving a tag preserves assignments. |
| No `ON DELETE SET NULL` | Unlike inbound_source's nullable FK, tag assignments have no meaning without both ends |

**Default color palette** (12 colors, handler rotates by tag creation order):

```
#ef4444 #f97316 #eab308 #22c55e #14b8a6 #3b82f6
#6366f1 #8b5cf6 #ec4899 #f43f5e #0ea5e9 #84cc16
```

### Proto contract

**`proto/attune/v1/tag.proto`** — new file (single service, matching 1-service-per-file convention):

```protobuf
syntax = "proto3";
package attune.v1;

import "google/api/annotations.proto";

// Tag registry + assignment operations (single service per file convention).
service FeedbackTagService {
  // Registry CRUD.
  rpc ListTags(ListTagsRequest) returns (ListTagsResponse) {
    option (google.api.http) = {get: "/fb/v1/console/tags"};
  }
  rpc CreateTag(CreateTagRequest) returns (Tag) {
    option (google.api.http) = {post: "/fb/v1/console/tags" body: "*"};
  }
  rpc UpdateTag(UpdateTagRequest) returns (Tag) {
    option (google.api.http) = {patch: "/fb/v1/console/tags/{id}" body: "*"};
  }
  rpc ArchiveTag(ArchiveTagRequest) returns (ArchiveTagResponse) {
    option (google.api.http) = {delete: "/fb/v1/console/tags/{id}"};
  }
  // Assignments on feedback rows.
  rpc AddFeedbackTag(AddFeedbackTagRequest) returns (AddFeedbackTagResponse) {
    option (google.api.http) = {post: "/fb/v1/console/feedback/{feedback_id}/tags" body: "*"};
  }
  rpc RemoveFeedbackTag(RemoveFeedbackTagRequest) returns (RemoveFeedbackTagResponse) {
    option (google.api.http) = {delete: "/fb/v1/console/feedback/{feedback_id}/tags/{tag_id}"};
  }
  rpc BatchUpdateFeedbackTags(BatchUpdateFeedbackTagsRequest) returns (BatchUpdateFeedbackTagsResponse) {
    option (google.api.http) = {post: "/fb/v1/console/feedback/batch/tags" body: "*"};
  }
}

// Existing proto additions (ingest.proto):
//   message FeedbackDetail  { repeated Tag tags = 25; }
//   message ListFeedbackRequest { optional string tag_id = 6; }

message Tag {
  string id = 1;
  string name = 2;
  string color = 3;
  string description = 4;
  optional string exclusive_scope = 5;
  int32 usage_count = 6;
  bool archived = 7;
  string created_by = 8;
  string created_at = 9;
  string updated_at = 10;
}

// --- Tag registry ---

message ListTagsRequest {
  bool include_archived = 1;
}
message ListTagsResponse {
  repeated Tag tags = 1;
}

message CreateTagRequest {
  string name = 1;
  optional string color = 2;
  optional string description = 3;
  optional string exclusive_scope = 4;
}

message UpdateTagRequest {
  string id = 1;
  optional string name = 2;
  optional string color = 3;
  optional string description = 4;
  optional string exclusive_scope = 5;
}

message ArchiveTagRequest {
  string id = 1;
}
message ArchiveTagResponse {}

// --- Tag assignments ---

message AddFeedbackTagRequest {
  int64 feedback_id = 1;
  // Either tag_id (existing) or tag_name (upsert). If both provided, tag_id wins.
  optional string tag_id = 2;
  optional string tag_name = 3;
  optional string tag_color = 4;
}
message AddFeedbackTagResponse {
  Tag tag = 1;
}

message RemoveFeedbackTagRequest {
  int64 feedback_id = 1;
  string tag_id = 2;
}
message RemoveFeedbackTagResponse {}

message BatchUpdateFeedbackTagsRequest {
  repeated int64 feedback_ids = 1;
  repeated string add_tag_ids = 2;
  repeated string remove_tag_ids = 3;
}
message BatchUpdateFeedbackTagsResponse {
  int32 affected = 1;
}
```

### Handler layer

Two new handler packages under `internal/handlers/console/` (wired via
`mountTags` for registry CRUD, and tag assignment routes nested under the
existing `mountFeedback`):

**`tag/`** — registry CRUD:
- `List` — all tags for tenant (filter `archived_at IS NULL` unless
  `include_archived`), ordered by `usage_count DESC, name ASC`.
- `Create` — validate name (1–48 chars, no control chars, trimmed),
  normalize color to lowercase hex6 with `#` (DB constraint is lowercase-only),
  description (≤200 chars); rotate default palette color if none provided;
  check `UNIQUE(tenant_id, name)`.
- `Update` — partial update (name, color, description, exclusive_scope);
  name rename checks uniqueness; name change cascades to all existing
  assignments (no action needed — junction references `tag_id`, not name).
- `Archive` — set `archived_at = NOW()`; do NOT cascade-delete assignments
  (tags remain on feedback, but hidden from pickers).

**`tagassignment/`** — feedback ↔ tag operations:
- `Add` — resolve tag by `tag_id` or upsert by `tag_name` (Intercom pattern);
  insert junction row; if tag has `exclusive_scope`, delete sibling
  assignments in the same scope (Gitea pattern); increment `usage_count` in
  same tx.
- `Remove` — delete junction row; decrement `usage_count` in same tx.
- `BatchUpdate` — for each feedback_id × add_tag_ids: add; for each
  feedback_id × remove_tag_ids: remove. Bounded to 100 feedback_ids × 20
  tags per request.

**Existing handler changes:**
- `feedback/feedback_list.go` — add `tag` to the reserved query params; parse
  as UUID; pass to repo as a new `TagID *uuid.UUID` field on `ConsoleListOpts`.
- `feedback/feedback_get.go` — include tag assignments in the detail response
  (new `repeated Tag tags` field on `FeedbackDetail`).

### Repo layer

**`internal/repo/feedbacktag/`** — tag registry:
- `Tag` domain struct: ID, TenantID, Name, Color, Description,
  ExclusiveScope, UsageCount, ArchivedAt, CreatedBy, CreatedAt, UpdatedAt.
- `List(ctx, tenantID, includeArchived)` — SELECT ordered by usage_count DESC.
- `Create(ctx, tag)` — INSERT RETURNING.
- `Update(ctx, tag)` — UPDATE by id + tenant_id.
- `Archive(ctx, tenantID, tagID)` — SET archived_at.
- `GetByName(ctx, tenantID, name)` — for upsert lookup.
- `GetByID(ctx, tenantID, tagID)` — for validation.

**`internal/repo/feedbacktagassignment/`** — junction:
- `Add(ctx, feedbackID, tagID, createdBy)` — INSERT ON CONFLICT DO NOTHING +
  increment usage_count; returns whether newly inserted.
- `Remove(ctx, feedbackID, tagID)` — DELETE + decrement usage_count (clamp ≥0).
- `RemoveByScopeExcluding(ctx, feedbackID, scope, excludeTagID)` — for
  exclusive scope enforcement: DELETE assignments for tags in the same scope
  except the one being added.
- `ListByFeedback(ctx, feedbackID)` — for detail view.
- `ListByFeedbackBatch(ctx, feedbackIDs)` — for list view (batch load tags for
  a page of feedback; avoids N+1).

**Existing repo changes:**
- `feedback/feedback_console.go` — `ConsoleListOpts` adds `TagID *uuid.UUID`;
  when set, add `JOIN feedback_tag_assignments fta ON fta.feedback_id = uf.id
  WHERE fta.tag_id = $N` to the query.

### Console UI

**Tag management page** (`console/src/features/tag-management/`):
- Settings section: `/_authed/settings?section=tags` (matches the existing
  section-based layout in `_authed.settings.tsx` — add `'tags'` to the
  `SettingsSection` union type + `SETTINGS_SECTIONS` array + sidebar).
- Table: name (editable inline), color swatch (click to pick from 12-color
  palette — no third-party color picker dependency; a simple popover with
  12 swatches matching the default palette), description, usage count,
  archive button.
- Create: inline row at top with name input + color picker + optional scope.
- Archive: soft-delete with confirmation; shows "N feedback affected" count.

**Shared UI components** (must live in `src/components/`, NOT in a feature
directory — the `no-cross-feature` dependency-cruiser rule forbids
`features/feedback/` from importing `features/tag-management/`):
- `src/components/ui/combobox.tsx` — searchable select built on Radix
  Popover + a filtered list (the console already has `@radix-ui/react-popover`
  via the existing Radix dependency; no new package needed). Reusable by
  any feature that needs autocomplete.
- `src/components/tag/tag-chip.tsx` — colored chip with optional `×` remove
  button. Distinct from `src/components/dim/dimension-chips.tsx` — uses a
  colored dot instead of the `enum_badge` renderer's icon+tone pattern.
- `src/components/tag/tag-picker.tsx` — combobox that lists tenant tags,
  supports create-on-type (upsert), and enforces exclusive scope (shows a
  toast on replacement).

**Feedback list tag chips** (`console/src/features/feedback/`):
- After dimension chips, render `TagChip` components (from shared layer).
- Tags loaded in the feedback list handler: after `ListForConsole` returns
  a page of rows, the handler calls `ListByFeedbackBatch(ctx, ids)` with
  the page's feedback IDs (single batched query, not N+1), then merges
  tags into the response. This is a **new batch-load pattern** for the
  feedback list — the handler currently does a single query, and this adds
  a second query per page.
- Click on tag chip → filter by that tag (add `?tag=<id>` to URL).

**Feedback detail tag picker**:
- `TagPicker` combobox (from shared layer) below the dimension chips section.
- Autocomplete from tenant's non-archived tags.
- Type a new name → "Create tag `<name>`" option at bottom (upsert).
- Exclusive scope enforcement: adding a scoped tag shows a toast "Replaced
  `status/待确认` with `status/已修复`".
- Each tag chip has an `×` to remove.

**Stats endpoints** — v1 does NOT add tag filtering to `UsageByDay`,
`UrgentCount`, or `TopValuesByDim`. Tag-scoped stats are deferred; the tag
management page shows `usage_count` (from the registry) as the primary
tag usage metric.

**i18n keys** (add to `zh-CN.json`):

```json
{
  "nav.tags": "标签管理",
  "tags": {
    "title": "标签管理",
    "subtitle": "创建和管理反馈标签。标签由运营手动添加，与 AI 分类独立。",
    "name": "名称",
    "color": "颜色",
    "description": "描述",
    "scope": "互斥分组",
    "scope_hint": "同一分组内只能选一个标签（如 status/待确认 和 status/已修复）",
    "usage": "使用次数",
    "create": "新建标签",
    "archive": "归档",
    "archive_confirm": "归档后标签不再出现在选择器中，但已标记的反馈不受影响。确定归档？",
    "archived": "已归档",
    "show_archived": "显示已归档",
    "empty": "还没有标签",
    "empty_hint": "在反馈详情页直接输入即可创建标签。"
  },
  "feedback.tags": {
    "add": "添加标签",
    "remove": "移除标签",
    "search": "搜索或创建标签...",
    "create_new": "创建标签「{{name}}」",
    "replaced": "已替换：{{old}} → {{new}}",
    "batch_add": "批量添加标签",
    "batch_remove": "批量移除标签"
  }
}
```

## Alternatives considered

### A. Extend Dimensions model with a `manual` kind

Add a new `DimensionKind = "manual"` — operators type values into `enriched_attrs`
directly, enricher skips dimensions of this kind.

**Rejected.** (1) `enriched_attrs` has no per-value audit trail (who added, when);
the JSONB stores `{"manual_tags": ["foo", "bar"]}` but not who applied `foo`.
(2) The enricher writes the entire `enriched_attrs` blob on enrichment
(`MarkDone` sets the whole column) — a race between enrichment and manual tagging
would silently drop manual entries unless we split the write path. (3) The
semantic conflation ("enriched" attrs that aren't enriched) confuses both the
data model and the UI. Structural separation is cleaner and what the industry
converges on.

### B. TEXT[] array column on user_feedback

Add `manual_tags TEXT[]` with GIN index directly on the feedback table.

**Rejected for v1.** (1) No per-tag audit (who/when). (2) No tag metadata
(color, description, scope). (3) Tag rename requires full-table UPDATE.
(4) Crunchy Data benchmarks show arrays are ~7× faster for reads, but attune's
feedback volume (hundreds-thousands/day/tenant) makes the junction JOIN
negligible. The array approach is a valid **optimization** if query latency
becomes a bottleneck — but not the right starting point when audit and metadata
are requirements.

### C. Ad-hoc strings without registry (Zendesk model)

Store tag strings directly in the junction table, no registry.

**Rejected.** Zendesk's own UX suffers: autocomplete is "top 15 from last 60
days" (stale tags vanish), no rename, no dedup (`follow-up` vs `followup`),
no color. attune's operators manage tags for a small team — a lightweight
registry with upsert semantics (Intercom model) costs almost nothing and
prevents these sharp edges from day one.

## Review hardening

Self-review against actual code identified and fixed the following assumptions:

| # | Original assumption | Verified reality | Fix |
|---|---|---|---|
| H1 | Two services (`TagService` + `TagAssignmentService`) in one `.proto` | All 12 existing proto files are 1-service-per-file | Merged into single `FeedbackTagService` |
| M1 | `tenant_feedback_tags` tenant FK uses `ON DELETE CASCADE` | `user_feedback` uses `ON DELETE RESTRICT` (`001_init.sql:53-54`, "compliance & audit trail"); tags carry audit data too | Changed to `ON DELETE RESTRICT` |
| M2 | "Add `tags` field to `FeedbackDetail`" (no field number) | `FeedbackDetail` last field = `reply_draft_enabled = 24`; `ListFeedbackRequest` last = `q = 5` | Pinned: `tags = 25` on `FeedbackDetail`, `tag_id = 6` on `ListFeedbackRequest` |
| M3 | Tag picker + color picker components assumed to exist | `console/src/components/ui/` has no combobox or color picker; Radix primitives available but unwrapped | Added explicit step to create `combobox.tsx` (Radix Popover + filtered list) and 12-swatch palette picker (no third-party dep) |
| M4 | Settings route `/_authed/settings/tags` (nested route) | Settings uses section-based query param: `?section=tags` (`SettingsSection` union type + sidebar) | Changed to `?section=tags`; add to `SettingsSection` union + `SETTINGS_SECTIONS` array |
| M5 | Tag chips in `features/feedback/` import from `features/tag-management/` | `no-cross-feature` dependency-cruiser rule forbids cross-feature imports | Moved shared tag UI (chip, picker) to `src/components/tag/` |
| M6 | "Tags loaded via `ListByFeedbackBatch`" (orchestration unspecified) | Feedback list handler does single query today; no batch-load precedent | Specified: handler calls `ListForConsole` first, then `ListByFeedbackBatch(ctx, ids)` with the page's IDs — two queries per page |
| L1 | Stats endpoints not mentioned | `UsageByDay`, `UrgentCount`, `TopValuesByDim` could accept tag filter | Explicitly deferred to non-goals; `usage_count` on registry is the v1 metric |
| L2 | Color constraint `'^#[0-9a-f]{6}$'` (lowercase only) | Handler must normalize input to lowercase before storage | Noted in handler description (Create/Update normalize color) |
| L3 | "Missing index on `feedback_id` direction" (agent concern) | PK `(feedback_id, tag_id)` already covers forward direction; `idx_..._tag (tag_id, feedback_id)` covers reverse | False alarm — both directions indexed. No change needed. |

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| **L1: Two tag systems side-by-side (dimensions + manual tags) may confuse operators** | UI renders them in visually distinct sections ("AI 分类" vs "标签"); onboarding copy explains the difference. If operators try to replicate a dimension as a manual tag, the Settings page could suggest "this looks like a dimension — consider adding it in 分类设置." |
| **L2: `usage_count` drift if app crashes between junction INSERT and count UPDATE** | Both happen in the same transaction. If the tx fails, neither commits. Periodic reconciliation job (future, not v1) can fix any edge-case drift. |
| **L3: Exclusive scope enforcement at application layer, not DB** | Gitea does the same — DB constraints for mutual exclusion across rows are complex (would need a trigger or exclusion constraint on scope). Application-level enforcement in a single transaction is sufficient; a concurrent race (two operators adding conflicting scoped tags simultaneously) is benign — last writer wins, and the audit trail shows both actions. |
| **L4: Tag list in feedback list query adds a JOIN** | `ListByFeedbackBatch` batches the tag load for the entire page (single query with `WHERE feedback_id IN (...)`), not N+1. At typical page sizes (20–50 rows), this is sub-millisecond. |
| **M1: Exclusive scope naming convention** | Unlike Gitea (implicit `/` in name) or GitLab (implicit `::` in name), attune uses an **explicit `exclusive_scope` column** — the scope is not parsed from the tag name. This is intentional: Gitea's `ExclusiveScope()` method depends on `/` position, which breaks for names like `frontend/fix` that aren't meant to be scoped. An explicit column is unambiguous. |

## Implementation plan

Ordered by dependency; each step is independently testable and mergeable.

| Step | Package / file | Work |
|---|---|---|
| 1 | `proto/attune/v1/tag.proto` + `proto/attune/v1/ingest.proto` | Define `FeedbackTagService` (single service); add `tags = 25` to `FeedbackDetail`, `tag_id = 6` to `ListFeedbackRequest`; `make proto`; commit generated Go + TS + OpenAPI |
| 2 | `internal/infra/database/migrations/029_feedback_tags.sql` | Registry + junction tables, indexes, constraints; tenant FK uses `ON DELETE RESTRICT` |
| 3 | `internal/repo/feedbacktag/` | Tag registry repo: List, Create, Update, Archive, GetByName, GetByID |
| 4 | `internal/repo/feedbacktagassignment/` | Junction repo: Add (with scope enforcement), Remove, ListByFeedback, ListByFeedbackBatch, RemoveByScopeExcluding |
| 5 | `internal/handlers/console/tag/` | Tag CRUD handlers; wire in router.go `mountTags`; update `router_inventory_test.go` expected routes |
| 6 | `internal/handlers/console/tagassignment/` | Add/Remove/BatchUpdate handlers; wire in router.go under `mountFeedback`; update inventory test |
| 7 | `internal/handlers/console/feedback/` | Add `TagID` filter to `ConsoleListOpts`; batch-load tags in List handler; add `tags` to detail response |
| 8 | `console/src/components/ui/combobox.tsx` + `console/src/components/tag/` | Shared combobox (Radix Popover + filtered list), TagChip, TagPicker components |
| 9 | `console/src/features/tag-management/` | Settings section page: tag CRUD, 12-swatch color picker, archive |
| 10 | `console/src/features/feedback/` | Tag chips on list + detail, inline tag picker, filter-by-tag; update `_authed.settings.tsx` section union |
| 11 | `console/src/i18n/zh-CN.json` | i18n keys |
| 12 | `test/integration/postgres/feedbacktag/` | Full lifecycle: create tag → add to feedback → list by tag → exclusive scope → remove → archive (with `doc.go`) |
| 13 | `CHANGELOG.md` | `### Added` entry |

## Verification

- [ ] Migration applies cleanly on a fresh DB and on the current schema
- [ ] Tag registry CRUD: create, update (including rename), archive, list
      (with/without archived)
- [ ] Tag assignment: add (by ID), add (by name / upsert), remove
- [ ] Exclusive scope: adding a scoped tag removes sibling assignments
- [ ] Batch update: add/remove across multiple feedback rows
- [ ] Feedback list filter: `?tag=<id>` returns only tagged rows, composes
      with dimension filters
- [ ] Feedback detail: tags array populated in response
- [ ] Console tag management page: create, edit, archive, color picker
- [ ] Console feedback list: tag chips render with correct colors
- [ ] Console feedback detail: inline picker autocompletes, creates on type,
      shows exclusive scope replacement toast
- [ ] `usage_count` increments/decrements correctly and matches actual count
- [ ] `archived_at` hides tags from pickers but preserves on existing feedback
- [ ] Proto-generated Go + TS + OpenAPI committed and CI `proto-sync` passes
- [ ] All quality gates pass (golangci-lint, lizard, jscpd, biome, vitest,
      go test)
- [ ] Real end-to-end: create tenant → ingest feedback → create tag via
      Console → add tag → filter → verify

## References

- [GitHub Labels REST API](https://docs.github.com/en/rest/issues/labels)
- [Gitea `models/issues/label.go`](https://github.com/go-gitea/gitea/blob/main/models/issues/label.go) — registry + junction schema
- [GitLab Labels API](https://docs.gitlab.com/api/labels/) — scoped labels, priority, archive
- [Intercom Tags API](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Tags/) — upsert + `applied_by` audit
- [Crunchy Data: Tags and Postgres Arrays](https://www.crunchydata.com/blog/tags-aand-postgres-arrays-a-purrfect-combination) — junction vs array benchmarks
- [Discourse tags schema](https://github.com/discourse/discourse) — `tags` + `topic_tags` + `tag_groups` + `category_tag_stats`
- [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/)
- attune `2026-06-07-flat-labels.md` — metadata-driven Dimensions model (the AI counterpart)
- attune `internal/domain/dimension.go` — `DimensionKind`, `Taxonomy`, `RendererSpec`
