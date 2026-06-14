# Feedback Manual Tags Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manual tagging system for feedback rows — tag registry with colors + exclusive scopes, junction-table assignments with audit trail, proto-first API, and full Console UI (tag management Settings page + inline tag picker on feedback detail).

**Architecture:** Proto-first (`tag.proto` → `make proto`), migration 029 (registry + junction), two new repo packages (`feedbacktag`, `feedbacktagassignment`), two new handler packages (`tag`, `tagassignment`), shared Console UI components (`src/components/tag/`), a new Settings section, and feedback list/detail integration.

**Tech Stack:** Go + pgx, protobuf (buf), chi router + dispatcher, React + TanStack Router/Query + Radix UI, i18next, Vitest.

---

## File map

### New files

| File | Responsibility |
|---|---|
| `proto/attune/v1/tag.proto` | `FeedbackTagService` — 7 RPCs (ListTags, CreateTag, UpdateTag, ArchiveTag, AddFeedbackTag, RemoveFeedbackTag, BatchUpdateFeedbackTags) |
| `internal/infra/database/migrations/029_feedback_tags.sql` | `tenant_feedback_tags` + `feedback_tag_assignments` tables |
| `internal/repo/feedbacktag/feedbacktag.go` | Tag registry repo (List, Create, Update, Archive, GetByName, GetByID, IncrementUsage, DecrementUsage) |
| `internal/repo/feedbacktagassignment/assignment.go` | Junction repo (Add, Remove, RemoveByScopeExcluding, ListByFeedback, ListByFeedbackBatch) |
| `internal/handlers/console/tag/handler.go` | Tag CRUD handlers (List, Create, Update, Archive) |
| `internal/handlers/console/tagassignment/handler.go` | Assignment handlers (Add, Remove, BatchUpdate) |
| `console/src/components/tag/tag-chip.tsx` | Colored tag chip with optional remove button |
| `console/src/components/tag/tag-picker.tsx` | Combobox-based tag selector with create-on-type |
| `console/src/components/tag/color-picker.tsx` | 12-swatch palette popover |
| `console/src/features/tag-management/api/list-tags.ts` | `tagsQuery()` — GET /fb/v1/console/tags |
| `console/src/features/tag-management/api/create-tag.ts` | `useCreateTag()` — POST /fb/v1/console/tags |
| `console/src/features/tag-management/api/update-tag.ts` | `useUpdateTag()` — PATCH /fb/v1/console/tags/{id} |
| `console/src/features/tag-management/api/archive-tag.ts` | `useArchiveTag()` — DELETE /fb/v1/console/tags/{id} |
| `console/src/features/tag-management/components/tag-management-page.tsx` | Settings section: tag table with inline editing |
| `console/src/features/feedback/api/add-feedback-tag.ts` | `useAddFeedbackTag()` |
| `console/src/features/feedback/api/remove-feedback-tag.ts` | `useRemoveFeedbackTag()` |
| `test/integration/postgres/feedbacktag/doc.go` | Build-tag for integration test package |
| `test/integration/postgres/feedbacktag/feedbacktag_test.go` | Full lifecycle integration test |

### Modified files

| File | Change |
|---|---|
| `proto/attune/v1/ingest.proto` | Add `repeated Tag tags = 25` to `FeedbackDetail`, `optional string tag_id = 6` to `ListFeedbackRequest` |
| `internal/handlers/console/router.go` | Add `tag` + `tagassignment` fields to `Router`, `mountTags()`, tag routes under `mountFeedback()` |
| `internal/handlers/console/router_inventory_test.go` | Add expected routes |
| `cmd/attune/setup.go` | Wire tag repos + handlers |
| `internal/handlers/console/feedback/feedback_list.go` | Parse `tag` query param, pass to repo |
| `internal/handlers/console/feedback/feedback.go` | Add `tagAssignmentRepo` interface + constructor change |
| `internal/handlers/console/feedback/feedback_get.go` | Batch-load tags into detail response |
| `internal/repo/feedback/feedback_console.go` | Add `TagID` to `ConsoleListOpts`, JOIN when set |
| `console/src/routes/_authed.settings.tsx` | Add `'tags'` section |
| `console/src/features/feedback/components/detail-sheet.tsx` | Add tag picker + tag chips |
| `console/src/i18n/zh-CN.json` | Tag i18n keys |
| `CHANGELOG.md` | `### Added` entry |

---

### Task 1: Proto contract (`tag.proto` + `ingest.proto` additions)

**Files:**
- Create: `proto/attune/v1/tag.proto`
- Modify: `proto/attune/v1/ingest.proto`

- [ ] **Step 1: Create `tag.proto`**

```protobuf
syntax = "proto3";

package attune.v1;

import "google/api/annotations.proto";

service FeedbackTagService {
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

message AddFeedbackTagRequest {
  int64 feedback_id = 1;
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

- [ ] **Step 2: Add fields to `ingest.proto`**

In `proto/attune/v1/ingest.proto`, add to `FeedbackDetail` (after field 24):

```protobuf
  repeated attune.v1.Tag tags = 25;
```

And add an import at the top:

```protobuf
import "attune/v1/tag.proto";
```

In `ListFeedbackRequest` (after field 5):

```protobuf
  optional string tag_id = 6;
```

- [ ] **Step 3: Run code generation**

Run: `make proto`

Expected: generates Go files in `internal/proto/attune/v1/`, TS files in `console/src/proto/attune/v1/`, and OpenAPI docs in `docs/openapi/`.

- [ ] **Step 4: Verify generated files**

Run: `git diff --stat`

Expected: new `tag.pb.go`, `tag.ts` files, updated `ingest.pb.go`, `ingest.ts` with the new fields.

- [ ] **Step 5: Commit**

```bash
git add proto/ internal/proto/ console/src/proto/ docs/openapi/
git commit -m "feat(proto): define FeedbackTagService and add tag fields to FeedbackDetail (#28)"
```

---

### Task 2: Database migration

**Files:**
- Create: `internal/infra/database/migrations/029_feedback_tags.sql`

- [ ] **Step 1: Write migration**

```sql
-- Tag registry: one row per named tag per tenant.
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
CREATE INDEX IF NOT EXISTS idx_feedback_tag_assignments_tag
    ON feedback_tag_assignments (tag_id, feedback_id);
```

- [ ] **Step 2: Verify migration applies**

Run: `make test-integration` (or start a fresh local DB and run migrations)

Expected: migration applies cleanly, no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/infra/database/migrations/029_feedback_tags.sql
git commit -m "feat(db): add tag registry and assignment tables (#28)"
```

---

### Task 3: Tag registry repo

**Files:**
- Create: `internal/repo/feedbacktag/feedbacktag.go`

- [ ] **Step 1: Write the tag repo**

```go
package feedbacktag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrNotFound = errors.New("tag not found")
var ErrNameConflict = errors.New("tag name already exists for tenant")

type Tag struct {
	ID             uuid.UUID
	TenantID       string
	Name           string
	Color          string
	Description    string
	ExclusiveScope *string
	ArchivedAt     *time.Time
	UsageCount     int
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const selectCols = `id, tenant_id, name, color, description, exclusive_scope,
	archived_at, usage_count, created_by, created_at, updated_at`

func scanTag(row pgx.Row, t *Tag) error {
	return row.Scan(
		&t.ID, &t.TenantID, &t.Name, &t.Color, &t.Description, &t.ExclusiveScope,
		&t.ArchivedAt, &t.UsageCount, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
}

func (r *Repo) List(ctx context.Context, tenantID string, includeArchived bool) ([]Tag, error) {
	where := "WHERE tenant_id = $1"
	if !includeArchived {
		where += " AND archived_at IS NULL"
	}
	query := "SELECT " + selectCols + " FROM tenant_feedback_tags " + where +
		" ORDER BY usage_count DESC, name ASC"
	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := scanTag(rows, &t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repo) Create(ctx context.Context, t Tag) (*Tag, error) {
	var created Tag
	err := scanTag(r.pool.QueryRow(ctx,
		`INSERT INTO tenant_feedback_tags (tenant_id, name, color, description, exclusive_scope, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+selectCols,
		t.TenantID, t.Name, t.Color, t.Description, t.ExclusiveScope, t.CreatedBy,
	), &created)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrNameConflict
		}
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return ptrext.Of(created), nil
}

func (r *Repo) Update(ctx context.Context, t Tag) (*Tag, error) {
	var updated Tag
	err := scanTag(r.pool.QueryRow(ctx,
		`UPDATE tenant_feedback_tags
		 SET name = $3, color = $4, description = $5, exclusive_scope = $6, updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2
		 RETURNING `+selectCols,
		t.ID, t.TenantID, t.Name, t.Color, t.Description, t.ExclusiveScope,
	), &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrNameConflict
		}
		return nil, fmt.Errorf("update tag: %w", err)
	}
	return ptrext.Of(updated), nil
}

func (r *Repo) Archive(ctx context.Context, tenantID string, tagID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tenant_feedback_tags SET archived_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2 AND archived_at IS NULL`,
		tagID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("archive tag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) GetByID(ctx context.Context, tenantID string, tagID uuid.UUID) (*Tag, error) {
	var t Tag
	err := scanTag(r.pool.QueryRow(ctx,
		"SELECT "+selectCols+" FROM tenant_feedback_tags WHERE id = $1 AND tenant_id = $2",
		tagID, tenantID,
	), &t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tag by id: %w", err)
	}
	return ptrext.Of(t), nil
}

func (r *Repo) GetByName(ctx context.Context, tenantID, name string) (*Tag, error) {
	var t Tag
	err := scanTag(r.pool.QueryRow(ctx,
		"SELECT "+selectCols+" FROM tenant_feedback_tags WHERE tenant_id = $1 AND name = $2",
		tenantID, name,
	), &t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tag by name: %w", err)
	}
	return ptrext.Of(t), nil
}

func (r *Repo) IncrementUsage(ctx context.Context, tagID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE tenant_feedback_tags SET usage_count = usage_count + 1 WHERE id = $1", tagID)
	if err != nil {
		return fmt.Errorf("increment usage: %w", err)
	}
	return nil
}

func (r *Repo) DecrementUsage(ctx context.Context, tagID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE tenant_feedback_tags SET usage_count = GREATEST(usage_count - 1, 0) WHERE id = $1",
		tagID)
	if err != nil {
		return fmt.Errorf("decrement usage: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (errors.As(err, new(interface{ SQLState() string })) &&
		err.(interface{ SQLState() string }).SQLState() == "23505")
}
```

Note: the `isUniqueViolation` helper checks the pgx `23505` error code. Check if the project already has a shared helper for this — if so, use it instead. If not, keep the local one. A pgconn-based check may be cleaner:

```go
import "github.com/jackc/pgx/v5/pgconn"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
```

Use whichever pattern existing code uses (grep for `pgconn.PgError` or `23505` in the repo).

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/repo/feedbacktag/...`

Expected: compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/repo/feedbacktag/
git commit -m "feat(repo): add feedbacktag registry repo (#28)"
```

---

### Task 4: Tag assignment repo

**Files:**
- Create: `internal/repo/feedbacktagassignment/assignment.go`

- [ ] **Step 1: Write the assignment repo**

```go
package feedbacktagassignment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type Assignment struct {
	FeedbackID int64
	TagID      uuid.UUID
	CreatedBy  string
	CreatedAt  time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) Add(ctx context.Context, feedbackID int64, tagID uuid.UUID, createdBy string) (inserted bool, err error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO feedback_tag_assignments (feedback_id, tag_id, created_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (feedback_id, tag_id) DO NOTHING`,
		feedbackID, tagID, createdBy,
	)
	if err != nil {
		return false, fmt.Errorf("add tag assignment: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) Remove(ctx context.Context, feedbackID int64, tagID uuid.UUID) (removed bool, err error) {
	tag, err := r.pool.Exec(ctx,
		"DELETE FROM feedback_tag_assignments WHERE feedback_id = $1 AND tag_id = $2",
		feedbackID, tagID,
	)
	if err != nil {
		return false, fmt.Errorf("remove tag assignment: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) RemoveByScopeExcluding(
	ctx context.Context, feedbackID int64, scope string, excludeTagID uuid.UUID,
) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`DELETE FROM feedback_tag_assignments
		 WHERE feedback_id = $1
		   AND tag_id != $3
		   AND tag_id IN (
		     SELECT id FROM tenant_feedback_tags
		     WHERE exclusive_scope = $2
		   )
		 RETURNING tag_id`,
		feedbackID, scope, excludeTagID,
	)
	if err != nil {
		return nil, fmt.Errorf("remove by scope: %w", err)
	}
	defer rows.Close()
	var removed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan removed tag id: %w", err)
		}
		removed = append(removed, id)
	}
	return removed, rows.Err()
}

type TagInfo struct {
	TagID          uuid.UUID
	Name           string
	Color          string
	Description    string
	ExclusiveScope *string
	UsageCount     int
	Archived       bool
	CreatedBy      string
	TagCreatedAt   time.Time
	TagUpdatedAt   time.Time
	AssignedBy     string
	AssignedAt     time.Time
}

func (r *Repo) ListByFeedback(ctx context.Context, feedbackID int64) ([]TagInfo, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT t.id, t.name, t.color, t.description, t.exclusive_scope,
		        t.usage_count, t.archived_at IS NOT NULL,
		        t.created_by, t.created_at, t.updated_at,
		        a.created_by, a.created_at
		 FROM feedback_tag_assignments a
		 JOIN tenant_feedback_tags t ON t.id = a.tag_id
		 WHERE a.feedback_id = $1
		 ORDER BY a.created_at`,
		feedbackID,
	)
	if err != nil {
		return nil, fmt.Errorf("list by feedback: %w", err)
	}
	defer rows.Close()
	var out []TagInfo
	for rows.Next() {
		var ti TagInfo
		if err := rows.Scan(
			&ti.TagID, &ti.Name, &ti.Color, &ti.Description, &ti.ExclusiveScope,
			&ti.UsageCount, &ti.Archived,
			&ti.CreatedBy, &ti.TagCreatedAt, &ti.TagUpdatedAt,
			&ti.AssignedBy, &ti.AssignedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tag info: %w", err)
		}
		out = append(out, ti)
	}
	return out, rows.Err()
}

func (r *Repo) ListByFeedbackBatch(ctx context.Context, feedbackIDs []int64) (map[int64][]TagInfo, error) {
	if len(feedbackIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.feedback_id,
		        t.id, t.name, t.color, t.description, t.exclusive_scope,
		        t.usage_count, t.archived_at IS NOT NULL,
		        t.created_by, t.created_at, t.updated_at,
		        a.created_by, a.created_at
		 FROM feedback_tag_assignments a
		 JOIN tenant_feedback_tags t ON t.id = a.tag_id
		 WHERE a.feedback_id = ANY($1)
		 ORDER BY a.feedback_id, a.created_at`,
		feedbackIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list by feedback batch: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]TagInfo)
	for rows.Next() {
		var fbID int64
		var ti TagInfo
		if err := rows.Scan(
			&fbID,
			&ti.TagID, &ti.Name, &ti.Color, &ti.Description, &ti.ExclusiveScope,
			&ti.UsageCount, &ti.Archived,
			&ti.CreatedBy, &ti.TagCreatedAt, &ti.TagUpdatedAt,
			&ti.AssignedBy, &ti.AssignedAt,
		); err != nil {
			return nil, fmt.Errorf("scan batch tag info: %w", err)
		}
		out[fbID] = append(out[fbID], ti)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/repo/feedbacktagassignment/...`

Expected: compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/repo/feedbacktagassignment/
git commit -m "feat(repo): add feedbacktagassignment junction repo (#28)"
```

---

### Task 5: Tag CRUD handlers + router wiring

**Files:**
- Create: `internal/handlers/console/tag/handler.go`
- Modify: `internal/handlers/console/router.go`
- Modify: `internal/handlers/console/router_inventory_test.go`

- [ ] **Step 1: Write the tag handler**

```go
package tag

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
)

var defaultPalette = [12]string{
	"#ef4444", "#f97316", "#eab308", "#22c55e", "#14b8a6", "#3b82f6",
	"#6366f1", "#8b5cf6", "#ec4899", "#f43f5e", "#0ea5e9", "#84cc16",
}

type tagRepo interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]feedbacktag.Tag, error)
	Create(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	Update(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	Archive(ctx context.Context, tenantID string, tagID uuid.UUID) error
}

type Handler struct {
	repo tagRepo
}

func NewHandler(r tagRepo) *Handler {
	return ptrext.Of(Handler{repo: r})
}

func (h *Handler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListTagsRequest,
) (dispatcher.Result[*attunev1.ListTagsResponse], error) {
	const where = "console.TagHandler.List"
	auth := ctx.Auth
	tags, err := h.repo.List(ctx, auth.TenantID, req.GetIncludeArchived())
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListTagsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list tags")
	}
	items := make([]*attunev1.Tag, 0, len(tags))
	for _, t := range tags {
		items = append(items, toProto(t))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListTagsResponse{Tags: items}))
}

func (h *Handler) Create(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateTagRequest,
) (dispatcher.Result[*attunev1.Tag], error) {
	const where = "console.TagHandler.Create"
	auth := ctx.Auth
	name := strings.TrimSpace(req.GetName())
	if err := validateName(name); err != nil {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, err.Error())
	}
	color := normalizeColor(req.Color)
	if color == "" {
		all, _ := h.repo.List(ctx, auth.TenantID, true)
		color = defaultPalette[len(all)%len(defaultPalette)]
	}
	desc := strings.TrimSpace(req.GetDescription())
	if utf8.RuneCountInString(desc) > 200 {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "description exceeds 200 characters")
	}
	var scope *string
	if req.ExclusiveScope != nil {
		s := strings.TrimSpace(req.GetExclusiveScope())
		if s != "" {
			if utf8.RuneCountInString(s) > 32 {
				return dispatcher.Fail[*attunev1.Tag](
					http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "scope exceeds 32 characters")
			}
			scope = ptrext.Of(s)
		}
	}
	created, err := h.repo.Create(ctx, feedbacktag.Tag{
		TenantID:       auth.TenantID,
		Name:           name,
		Color:          color,
		Description:    desc,
		ExclusiveScope: scope,
		CreatedBy:      auth.UserID,
	})
	if errors.Is(err, feedbacktag.ErrNameConflict) {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusConflict, attunev1.ErrorCode_ALREADY_EXISTS, "tag name already exists")
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] create failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create tag")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,tag_id:%s,name:%s", where, auth.TenantID, created.ID, created.Name)
	return dispatcher.OK(toProto(*created))
}

func (h *Handler) Update(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateTagRequest,
) (dispatcher.Result[*attunev1.Tag], error) {
	const where = "console.TagHandler.Update"
	auth := ctx.Auth
	tagID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "invalid tag id")
	}
	name := strings.TrimSpace(req.GetName())
	if name != "" {
		if err := validateName(name); err != nil {
			return dispatcher.Fail[*attunev1.Tag](
				http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, err.Error())
		}
	}
	color := normalizeColor(req.Color)
	desc := strings.TrimSpace(req.GetDescription())
	if utf8.RuneCountInString(desc) > 200 {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "description exceeds 200 characters")
	}
	var scope *string
	if req.ExclusiveScope != nil {
		s := strings.TrimSpace(req.GetExclusiveScope())
		if s == "" {
			scope = nil
		} else {
			if utf8.RuneCountInString(s) > 32 {
				return dispatcher.Fail[*attunev1.Tag](
					http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "scope exceeds 32 characters")
			}
			scope = ptrext.Of(s)
		}
	}
	updated, err := h.repo.Update(ctx, feedbacktag.Tag{
		ID:             tagID,
		TenantID:       auth.TenantID,
		Name:           name,
		Color:          color,
		Description:    desc,
		ExclusiveScope: scope,
	})
	if errors.Is(err, feedbacktag.ErrNotFound) {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tag not found")
	}
	if errors.Is(err, feedbacktag.ErrNameConflict) {
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusConflict, attunev1.ErrorCode_ALREADY_EXISTS, "tag name already exists")
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, err.Error())
		return dispatcher.Fail[*attunev1.Tag](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update tag")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,tag_id:%s", where, auth.TenantID, tagID)
	return dispatcher.OK(toProto(*updated))
}

func (h *Handler) Archive(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ArchiveTagRequest,
) (dispatcher.Result[*attunev1.ArchiveTagResponse], error) {
	const where = "console.TagHandler.Archive"
	auth := ctx.Auth
	tagID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ArchiveTagResponse](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "invalid tag id")
	}
	if err := h.repo.Archive(ctx, auth.TenantID, tagID); err != nil {
		if errors.Is(err, feedbacktag.ErrNotFound) {
			return dispatcher.Fail[*attunev1.ArchiveTagResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "tag not found or already archived")
		}
		logext.Errorf(ctx, "[%s] archive failed,tenant_id:%s,tag_id:%s,err:%+v",
			where, auth.TenantID, tagID, err.Error())
		return dispatcher.Fail[*attunev1.ArchiveTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to archive tag")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,tag_id:%s", where, auth.TenantID, tagID)
	return dispatcher.OK(ptrext.Of(attunev1.ArchiveTagResponse{}))
}

func toProto(t feedbacktag.Tag) *attunev1.Tag {
	p := ptrext.Of(attunev1.Tag{
		Id:          t.ID.String(),
		Name:        t.Name,
		Color:       t.Color,
		Description: t.Description,
		UsageCount:  int32(t.UsageCount),
		Archived:    t.ArchivedAt != nil,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if t.ExclusiveScope != nil {
		p.ExclusiveScope = t.ExclusiveScope
	}
	return p
}

func validateName(name string) error {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 48 {
		return errors.New("name must be 1-48 characters")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("name contains control characters")
		}
	}
	return nil
}

func normalizeColor(c *string) string {
	if c == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*c))
}
```

- [ ] **Step 2: Add Router field, `mountTags`, and constructor re-export**

In `internal/handlers/console/router.go`:

1. Add import: `consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"`
2. Add field `tags *consoletag.Handler` to the `Router` struct (after `digestSubscription`).
3. Add `tags` param to `NewRouter` and assign it.
4. Add `NewTagHandler = consoletag.NewHandler` to the `var` block.
5. Add `r.mountTags(m)` call in `mountSession` (after `r.mountClusters(m)`).
6. Add `mountTags` method:

```go
func (r *Router) mountTags(m chi.Router) {
	if r.tags == nil {
		return
	}
	m.Route("/tags", func(t chi.Router) {
		t.Get("/", dispatcher.Bind(
			"console.TagHandler.List",
			dispatcher.Query(
				func() *attunev1.ListTagsRequest { return ptrext.Of(attunev1.ListTagsRequest{}) },
				func(_ *http.Request, req *attunev1.ListTagsRequest) error { return nil },
			),
			r.tags.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListTagsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.Post("/", dispatcher.Bind(
			"console.TagHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateTagRequest { return ptrext.Of(attunev1.CreateTagRequest{}) }),
			r.tags.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.Patch("/{id}", dispatcher.Bind(
			"console.TagHandler.Update",
			dispatcher.Combine(
				func() *attunev1.UpdateTagRequest { return ptrext.Of(attunev1.UpdateTagRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateTagRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateTagRequest, id string) {
					req.Id = id
				}),
			),
			r.tags.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.Delete("/{id}", dispatcher.Bind(
			"console.TagHandler.Archive",
			dispatcher.Path(
				func() *attunev1.ArchiveTagRequest { return ptrext.Of(attunev1.ArchiveTagRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveTagRequest, id string) {
					req.Id = id
				}),
			),
			r.tags.Archive,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}
```

- [ ] **Step 3: Update router inventory test**

In `router_inventory_test.go`, add `consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"` to imports, add `tags: &consoletag.Handler{}` to the Router literal, and add these to the `expected` slice:

```go
"GET /tags/",
"POST /tags/",
"PATCH /tags/{id}",
"DELETE /tags/{id}",
```

- [ ] **Step 4: Verify it compiles and test passes**

Run: `go build ./internal/handlers/console/... && go test ./internal/handlers/console/ -run TestRouterInventory -v`

Expected: compiles, inventory test passes with new routes.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/console/tag/ internal/handlers/console/router.go internal/handlers/console/router_inventory_test.go
git commit -m "feat(handlers): add tag CRUD handlers and mount routes (#28)"
```

---

### Task 6: Tag assignment handlers + router wiring

**Files:**
- Create: `internal/handlers/console/tagassignment/handler.go`
- Modify: `internal/handlers/console/router.go`
- Modify: `internal/handlers/console/router_inventory_test.go`

- [ ] **Step 1: Write the tag assignment handler**

```go
package tagassignment

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
)

type tagRepo interface {
	GetByID(ctx context.Context, tenantID string, tagID uuid.UUID) (*feedbacktag.Tag, error)
	GetByName(ctx context.Context, tenantID, name string) (*feedbacktag.Tag, error)
	Create(ctx context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error)
	IncrementUsage(ctx context.Context, tagID uuid.UUID) error
	DecrementUsage(ctx context.Context, tagID uuid.UUID) error
}

type assignmentRepo interface {
	Add(ctx context.Context, feedbackID int64, tagID uuid.UUID, createdBy string) (bool, error)
	Remove(ctx context.Context, feedbackID int64, tagID uuid.UUID) (bool, error)
	RemoveByScopeExcluding(ctx context.Context, feedbackID int64, scope string, excludeTagID uuid.UUID) ([]uuid.UUID, error)
}

var defaultPalette = [12]string{
	"#ef4444", "#f97316", "#eab308", "#22c55e", "#14b8a6", "#3b82f6",
	"#6366f1", "#8b5cf6", "#ec4899", "#f43f5e", "#0ea5e9", "#84cc16",
}

type Handler struct {
	tags        tagRepo
	assignments assignmentRepo
}

func NewHandler(tags tagRepo, assignments assignmentRepo) *Handler {
	return ptrext.Of(Handler{tags: tags, assignments: assignments})
}

func (h *Handler) Add(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.AddFeedbackTagRequest,
) (dispatcher.Result[*attunev1.AddFeedbackTagResponse], error) {
	const where = "console.TagAssignmentHandler.Add"
	auth := ctx.Auth
	feedbackID := req.GetFeedbackId()

	tag, err := h.resolveTag(ctx, auth, req)
	if err != nil {
		logext.Errorf(ctx, "[%s] resolve tag failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, err.Error())
	}

	if tag.ExclusiveScope != nil {
		removed, err := h.assignments.RemoveByScopeExcluding(ctx, feedbackID, *tag.ExclusiveScope, tag.ID)
		if err != nil {
			logext.Errorf(ctx, "[%s] scope cleanup failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
			return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to enforce exclusive scope")
		}
		for _, rid := range removed {
			_ = h.tags.DecrementUsage(ctx, rid)
		}
	}

	inserted, err := h.assignments.Add(ctx, feedbackID, tag.ID, auth.UserID)
	if err != nil {
		logext.Errorf(ctx, "[%s] add failed,tenant_id:%s,feedback_id:%d,tag_id:%s,err:%+v",
			where, auth.TenantID, feedbackID, tag.ID, err.Error())
		return dispatcher.Fail[*attunev1.AddFeedbackTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to add tag")
	}
	if inserted {
		_ = h.tags.IncrementUsage(ctx, tag.ID)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,tag_id:%s,inserted:%t",
		where, auth.TenantID, feedbackID, tag.ID, inserted)
	return dispatcher.OK(ptrext.Of(attunev1.AddFeedbackTagResponse{
		Tag: toProto(*tag),
	}))
}

func (h *Handler) Remove(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RemoveFeedbackTagRequest,
) (dispatcher.Result[*attunev1.RemoveFeedbackTagResponse], error) {
	const where = "console.TagAssignmentHandler.Remove"
	auth := ctx.Auth
	tagID, err := uuid.Parse(req.GetTagId())
	if err != nil {
		return dispatcher.Fail[*attunev1.RemoveFeedbackTagResponse](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "invalid tag id")
	}
	removed, err := h.assignments.Remove(ctx, req.GetFeedbackId(), tagID)
	if err != nil {
		logext.Errorf(ctx, "[%s] remove failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.RemoveFeedbackTagResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to remove tag")
	}
	if removed {
		_ = h.tags.DecrementUsage(ctx, tagID)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,tag_id:%s",
		where, auth.TenantID, req.GetFeedbackId(), tagID)
	return dispatcher.OK(ptrext.Of(attunev1.RemoveFeedbackTagResponse{}))
}

func (h *Handler) BatchUpdate(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.BatchUpdateFeedbackTagsRequest,
) (dispatcher.Result[*attunev1.BatchUpdateFeedbackTagsResponse], error) {
	const where = "console.TagAssignmentHandler.BatchUpdate"
	auth := ctx.Auth
	if len(req.GetFeedbackIds()) > 100 {
		return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "max 100 feedback ids per batch")
	}
	if len(req.GetAddTagIds())+len(req.GetRemoveTagIds()) > 20 {
		return dispatcher.Fail[*attunev1.BatchUpdateFeedbackTagsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_INVALID_ARGUMENT, "max 20 tag operations per batch")
	}

	var affected int32
	for _, fbID := range req.GetFeedbackIds() {
		for _, addID := range req.GetAddTagIds() {
			tagID, err := uuid.Parse(addID)
			if err != nil {
				continue
			}
			inserted, err := h.assignments.Add(ctx, fbID, tagID, auth.UserID)
			if err != nil {
				logext.Warnf(ctx, "[%s] batch add skipped,feedback_id:%d,tag_id:%s,err:%+v",
					where, fbID, addID, err.Error())
				continue
			}
			if inserted {
				_ = h.tags.IncrementUsage(ctx, tagID)
				affected++
			}
		}
		for _, rmID := range req.GetRemoveTagIds() {
			tagID, err := uuid.Parse(rmID)
			if err != nil {
				continue
			}
			removed, err := h.assignments.Remove(ctx, fbID, tagID)
			if err != nil {
				logext.Warnf(ctx, "[%s] batch remove skipped,feedback_id:%d,tag_id:%s,err:%+v",
					where, fbID, rmID, err.Error())
				continue
			}
			if removed {
				_ = h.tags.DecrementUsage(ctx, tagID)
				affected++
			}
		}
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,affected:%d", where, auth.TenantID, affected)
	return dispatcher.OK(ptrext.Of(attunev1.BatchUpdateFeedbackTagsResponse{Affected: affected}))
}

func (h *Handler) resolveTag(
	ctx context.Context, auth *session.AuthCtx, req *attunev1.AddFeedbackTagRequest,
) (*feedbacktag.Tag, error) {
	if req.TagId != nil {
		tagID, err := uuid.Parse(req.GetTagId())
		if err != nil {
			return nil, errors.New("invalid tag id")
		}
		return h.tags.GetByID(ctx, auth.TenantID, tagID)
	}
	if req.TagName == nil || strings.TrimSpace(req.GetTagName()) == "" {
		return nil, errors.New("either tag_id or tag_name is required")
	}
	name := strings.TrimSpace(req.GetTagName())
	existing, err := h.tags.GetByName(ctx, auth.TenantID, name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, feedbacktag.ErrNotFound) {
		return nil, err
	}
	color := "#6b7280"
	if req.TagColor != nil {
		color = strings.ToLower(strings.TrimSpace(req.GetTagColor()))
	}
	created, err := h.tags.Create(ctx, feedbacktag.Tag{
		TenantID:  auth.TenantID,
		Name:      name,
		Color:     color,
		CreatedBy: auth.UserID,
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func toProto(t feedbacktag.Tag) *attunev1.Tag {
	p := ptrext.Of(attunev1.Tag{
		Id:          t.ID.String(),
		Name:        t.Name,
		Color:       t.Color,
		Description: t.Description,
		UsageCount:  int32(t.UsageCount),
		Archived:    t.ArchivedAt != nil,
		CreatedBy:   t.CreatedBy,
	})
	if t.ExclusiveScope != nil {
		p.ExclusiveScope = t.ExclusiveScope
	}
	return p
}
```

- [ ] **Step 2: Add assignment routes to router**

In `router.go`:

1. Add import: `consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"`
2. Add field `tagAssignments *consoletagassignment.Handler` to `Router` (after `tags`).
3. Add param + assignment in `NewRouter`.
4. Add `NewTagAssignmentHandler = consoletagassignment.NewHandler` to the `var` block.
5. In `mountFeedback`, add tag assignment routes inside the `f.Route` block, after the `/{id}/reply-draft/regenerate` route:

```go
f.Post("/{id}/tags", dispatcher.Bind(
	"console.TagAssignmentHandler.Add",
	dispatcher.Combine(
		func() *attunev1.AddFeedbackTagRequest { return ptrext.Of(attunev1.AddFeedbackTagRequest{}) },
		dispatcher.JSONBody[*attunev1.AddFeedbackTagRequest],
		dispatcher.ParamInt64("id", func(req *attunev1.AddFeedbackTagRequest, id int64) {
			req.FeedbackId = id
		}, "id must be an integer"),
	),
	r.tagAssignments.Add,
	dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AddFeedbackTagRequest) (*session.AuthCtx, error) {
		return session.FromContext(r.Context()), nil
	}),
))
f.Delete("/{id}/tags/{tag_id}", dispatcher.Bind(
	"console.TagAssignmentHandler.Remove",
	dispatcher.Path(
		func() *attunev1.RemoveFeedbackTagRequest { return ptrext.Of(attunev1.RemoveFeedbackTagRequest{}) },
		dispatcher.ParamInt64("id", func(req *attunev1.RemoveFeedbackTagRequest, id int64) {
			req.FeedbackId = id
		}, "id must be an integer"),
		dispatcher.Param("tag_id", func(req *attunev1.RemoveFeedbackTagRequest, id string) {
			req.TagId = id
		}),
	),
	r.tagAssignments.Remove,
	dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RemoveFeedbackTagRequest) (*session.AuthCtx, error) {
		return session.FromContext(r.Context()), nil
	}),
))
```

Also, add the batch route. Since it's at `/feedback/batch/tags`, it must come BEFORE `/{id}` in the chi route definition. Add this inside the `f.Route("/feedback", ...)` block, between `/stats` and `/{id}`:

```go
f.Post("/batch/tags", dispatcher.Bind(
	"console.TagAssignmentHandler.BatchUpdate",
	dispatcher.JSON(func() *attunev1.BatchUpdateFeedbackTagsRequest {
		return ptrext.Of(attunev1.BatchUpdateFeedbackTagsRequest{})
	}),
	r.tagAssignments.BatchUpdate,
	dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchUpdateFeedbackTagsRequest) (*session.AuthCtx, error) {
		return session.FromContext(r.Context()), nil
	}),
))
```

- [ ] **Step 3: Update router inventory test**

Add `consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"` to imports, `tagAssignments: &consoletagassignment.Handler{}` to the Router literal, and these routes to `expected`:

```go
"POST /feedback/{id}/tags",
"DELETE /feedback/{id}/tags/{tag_id}",
"POST /feedback/batch/tags",
```

- [ ] **Step 4: Verify it compiles and test passes**

Run: `go build ./internal/handlers/console/... && go test ./internal/handlers/console/ -run TestRouterInventory -v`

Expected: compiles, inventory test passes with all new routes.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/console/tagassignment/ internal/handlers/console/router.go internal/handlers/console/router_inventory_test.go
git commit -m "feat(handlers): add tag assignment handlers and mount routes (#28)"
```

---

### Task 7: Feedback list/detail integration (tag filter + tag loading)

**Files:**
- Modify: `internal/repo/feedback/feedback_console.go`
- Modify: `internal/handlers/console/feedback/feedback.go`
- Modify: `internal/handlers/console/feedback/feedback_list.go`
- Modify: `internal/handlers/console/feedback/feedback_get.go`

- [ ] **Step 1: Add `TagID` to `ConsoleListOpts` and JOIN in query**

In `internal/repo/feedback/feedback_console.go`, add to `ConsoleListOpts` struct:

```go
TagID *string // UUID string; nil = no filter
```

In `ListForConsole`, after the `opts.Q` block (around line 99), add:

```go
if opts.TagID != nil {
	where += " AND EXISTS (SELECT 1 FROM feedback_tag_assignments fta WHERE fta.feedback_id = uf.id AND fta.tag_id = " + addArg(*opts.TagID) + "::uuid)"
}
```

Also: the main query references the table as `user_feedback` without alias. Add `uf` alias: change `FROM user_feedback` to `FROM user_feedback uf` in the query (line ~111), and update the WHERE clause from `WHERE tenant_id` to `WHERE uf.tenant_id`. This is needed for the subquery to reference `uf.id` unambiguously.

- [ ] **Step 2: Add `tag` to reserved query params and parse it**

In `internal/handlers/console/feedback/feedback_list.go`:

Add `"tag": {}` to `listFeedbackReservedQuery`.

In `BindListRequest`, after the `urgent` parsing block, add:

```go
if v := q.Get("tag"); v != "" {
	req.TagId = ptrext.Of(v)
}
```

In the `List` handler method, after building `opts`, add:

```go
if req.TagId != nil {
	opts.TagID = req.TagId
}
```

- [ ] **Step 3: Add tag assignment repo dependency to FeedbackHandler**

In `internal/handlers/console/feedback/feedback.go`:

Add a `tagAssignments` interface field and wiring:

```go
type tagAssignmentReader interface {
	ListByFeedback(ctx context.Context, feedbackID int64) ([]feedbacktagassignment.TagInfo, error)
	ListByFeedbackBatch(ctx context.Context, feedbackIDs []int64) (map[int64][]feedbacktagassignment.TagInfo, error)
}
```

Add `tagAssignments tagAssignmentReader` field to `FeedbackHandler`. Add a setter method:

```go
func (h *FeedbackHandler) SetTagAssignments(r tagAssignmentReader) { h.tagAssignments = r }
```

Add import for `feedbacktagassignment` package.

- [ ] **Step 4: Load tags in List handler**

In `feedback_list.go`, after the `items` loop (around line 96), add tag batch loading:

```go
if h.tagAssignments != nil && len(rows) > 0 {
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	tagMap, err := h.tagAssignments.ListByFeedbackBatch(ctx, ids)
	if err != nil {
		logext.Warnf(ctx, "[%s] tag batch load failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
	} else {
		for _, item := range items {
			if tags, ok := tagMap[item.GetId()]; ok {
				for _, ti := range tags {
					item.Tags = append(item.Tags, tagInfoToProto(ti))
				}
			}
		}
	}
}
```

Note: `Feedback` message in `ingest.proto` currently has no `tags` field. You'll need to add `repeated attune.v1.Tag tags = 16` to `Feedback` message too (after field 15). This means another proto edit — add it along with the `FeedbackDetail.tags = 25` in Task 1, or add it now and re-run `make proto`.

Add the `tagInfoToProto` helper in `feedback.go`:

```go
func tagInfoToProto(ti feedbacktagassignment.TagInfo) *attunev1.Tag {
	p := ptrext.Of(attunev1.Tag{
		Id:         ti.TagID.String(),
		Name:       ti.Name,
		Color:      ti.Color,
		Description: ti.Description,
		UsageCount: int32(ti.UsageCount),
		Archived:   ti.Archived,
		CreatedBy:  ti.CreatedBy,
		CreatedAt:  ti.TagCreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  ti.TagUpdatedAt.UTC().Format(time.RFC3339),
	})
	if ti.ExclusiveScope != nil {
		p.ExclusiveScope = ti.ExclusiveScope
	}
	return p
}
```

- [ ] **Step 5: Load tags in Get handler**

In `feedback_get.go`, after building `detail` (around line 59), add:

```go
if h.tagAssignments != nil {
	tagInfos, err := h.tagAssignments.ListByFeedback(ctx, id)
	if err != nil {
		logext.Warnf(ctx, "[%s] tag load failed,tenant_id:%s,id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
	} else {
		for _, ti := range tagInfos {
			detail.Tags = append(detail.Tags, tagInfoToProto(ti))
		}
	}
}
```

- [ ] **Step 6: Verify it compiles**

Run: `go build ./internal/handlers/console/feedback/...`

Expected: compiles with no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/repo/feedback/feedback_console.go internal/handlers/console/feedback/
git commit -m "feat(feedback): integrate tag filter and batch-load tags into list/detail (#28)"
```

---

### Task 8: Wire repos and handlers in `setup.go`

**Files:**
- Modify: `cmd/attune/setup.go`

- [ ] **Step 1: Wire tag repos and handlers**

In `buildConsoleRouter`:

1. Add imports:
   ```go
   feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
   feedbacktagassignmentrepo "github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
   ```

2. After `feedbackRepo := feedback.NewFeedback(pool)`, add:
   ```go
   tagRepo := feedbacktagrepo.New(pool)
   tagAssignmentRepo := feedbacktagassignmentrepo.New(pool)
   ```

3. After `feedback := console.NewFeedbackHandler(...)`, add:
   ```go
   feedback.SetTagAssignments(tagAssignmentRepo)
   ```

4. Before the `return console.NewRouter(...)` call, add:
   ```go
   tagHandler := console.NewTagHandler(tagRepo)
   tagAssignmentHandler := console.NewTagAssignmentHandler(tagRepo, tagAssignmentRepo)
   ```

5. Update the `console.NewRouter(...)` call to pass `tagHandler` and `tagAssignmentHandler` in the correct parameter positions.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/attune/...`

Expected: compiles with no errors.

- [ ] **Step 3: Run full Go checks**

Run: `go vet ./... && go build ./...`

Expected: zero warnings, zero errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/attune/setup.go
git commit -m "feat(setup): wire tag repos and handlers into console router (#28)"
```

---

### Task 9: Integration tests

**Files:**
- Create: `test/integration/postgres/feedbacktag/doc.go`
- Create: `test/integration/postgres/feedbacktag/feedbacktag_test.go`

- [ ] **Step 1: Write doc.go**

```go
// Package feedbacktag contains PostgreSQL integration tests for tag storage.
package feedbacktag
```

- [ ] **Step 2: Write the integration test**

```go
//go:build integration

package feedbacktag_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	"github.com/Phixsura/attune/internal/testdb"
)

func seedTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO tenants (slug, name) VALUES ('tag-test','Tag Test Co')
		 ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedFeedback(t *testing.T, pool *pgxpool.Pool, tenantID, content string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO user_feedback (tenant_id, user_id, source, content)
		 VALUES ($1, 'u1', 'api', $2) RETURNING id`,
		tenantID, content).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestPG_TagLifecycle(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID := seedTenant(t, pool)
	fbID := seedFeedback(t, pool, tenantID, "test feedback for tags")
	ctx := context.Background()
	tagRepo := feedbacktag.New(pool)
	assignRepo := feedbacktagassignment.New(pool)

	// Create tag
	created, err := tagRepo.Create(ctx, feedbacktag.Tag{
		TenantID:  tenantID,
		Name:      "需要跟进",
		Color:     "#ef4444",
		CreatedBy: "user-1",
	})
	require.NoError(t, err)
	require.Equal(t, "需要跟进", created.Name)
	require.Equal(t, "#ef4444", created.Color)
	require.Equal(t, 0, created.UsageCount)

	// List (non-archived)
	tags, err := tagRepo.List(ctx, tenantID, false)
	require.NoError(t, err)
	require.Len(t, tags, 1)

	// Add to feedback
	inserted, err := assignRepo.Add(ctx, fbID, created.ID, "user-1")
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, tagRepo.IncrementUsage(ctx, created.ID))

	// Verify assignment
	infos, err := assignRepo.ListByFeedback(ctx, fbID)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, "需要跟进", infos[0].Name)

	// Batch load
	batch, err := assignRepo.ListByFeedbackBatch(ctx, []int64{fbID})
	require.NoError(t, err)
	require.Len(t, batch[fbID], 1)

	// Duplicate add is no-op
	inserted2, err := assignRepo.Add(ctx, fbID, created.ID, "user-1")
	require.NoError(t, err)
	require.False(t, inserted2)

	// Remove
	removed, err := assignRepo.Remove(ctx, fbID, created.ID)
	require.NoError(t, err)
	require.True(t, removed)
	require.NoError(t, tagRepo.DecrementUsage(ctx, created.ID))

	// Update
	updated, err := tagRepo.Update(ctx, feedbacktag.Tag{
		ID:       created.ID,
		TenantID: tenantID,
		Name:     "已确认",
		Color:    "#22c55e",
	})
	require.NoError(t, err)
	require.Equal(t, "已确认", updated.Name)

	// Archive
	require.NoError(t, tagRepo.Archive(ctx, tenantID, created.ID))
	tagsAfter, err := tagRepo.List(ctx, tenantID, false)
	require.NoError(t, err)
	require.Len(t, tagsAfter, 0)
	tagsAll, err := tagRepo.List(ctx, tenantID, true)
	require.NoError(t, err)
	require.Len(t, tagsAll, 1)

	// Duplicate name conflict
	_, err = tagRepo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "unique-tag", Color: "#3b82f6", CreatedBy: "user-1",
	})
	require.NoError(t, err)
	_, err = tagRepo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "unique-tag", Color: "#3b82f6", CreatedBy: "user-1",
	})
	require.ErrorIs(t, err, feedbacktag.ErrNameConflict)
}

func TestPG_ExclusiveScope(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID := seedTenant(t, pool)
	fbID := seedFeedback(t, pool, tenantID, "exclusive scope test")
	ctx := context.Background()
	tagRepo := feedbacktag.New(pool)
	assignRepo := feedbacktagassignment.New(pool)

	// Create two scoped tags
	pending, err := tagRepo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "status/待确认", Color: "#eab308",
		ExclusiveScope: ptrext.Of("status"), CreatedBy: "user-1",
	})
	require.NoError(t, err)
	fixed, err := tagRepo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "status/已修复", Color: "#22c55e",
		ExclusiveScope: ptrext.Of("status"), CreatedBy: "user-1",
	})
	require.NoError(t, err)

	// Add pending
	_, err = assignRepo.Add(ctx, fbID, pending.ID, "user-1")
	require.NoError(t, err)

	// Add fixed — should remove pending via exclusive scope
	removed, err := assignRepo.RemoveByScopeExcluding(ctx, fbID, "status", fixed.ID)
	require.NoError(t, err)
	require.Len(t, removed, 1)
	require.Equal(t, pending.ID, removed[0])

	_, err = assignRepo.Add(ctx, fbID, fixed.ID, "user-1")
	require.NoError(t, err)

	// Verify only fixed remains
	infos, err := assignRepo.ListByFeedback(ctx, fbID)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, "status/已修复", infos[0].Name)
}
```

- [ ] **Step 3: Run integration tests**

Run: `make test-integration` (or `go test -tags integration ./test/integration/postgres/feedbacktag/ -v`)

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add test/integration/postgres/feedbacktag/
git commit -m "test(integration): add tag lifecycle and exclusive scope tests (#28)"
```

---

### Task 10: Console tag API hooks

**Files:**
- Create: `console/src/features/tag-management/api/list-tags.ts`
- Create: `console/src/features/tag-management/api/create-tag.ts`
- Create: `console/src/features/tag-management/api/update-tag.ts`
- Create: `console/src/features/tag-management/api/archive-tag.ts`
- Create: `console/src/features/feedback/api/add-feedback-tag.ts`
- Create: `console/src/features/feedback/api/remove-feedback-tag.ts`

- [ ] **Step 1: Create tag management API hooks**

`console/src/features/tag-management/api/list-tags.ts`:
```typescript
import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListTagsResponse, Tag } from '@/proto/attune/v1/tag'

export type { Tag }

export const tagsQuery = (includeArchived = false) =>
  queryOptions({
    queryKey: ['console', 'tags', { includeArchived }],
    queryFn: async ({ signal }) => {
      const params = includeArchived ? '?include_archived=true' : ''
      const resp = await api<ListTagsResponse>(`/fb/v1/console/tags${params}`, { signal })
      return resp.tags ?? []
    },
    staleTime: 30_000,
  })
```

`console/src/features/tag-management/api/create-tag.ts`:
```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateTagRequest, Tag } from '@/proto/attune/v1/tag'

export function useCreateTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateTagRequest) =>
      api<Tag>('/fb/v1/console/tags', { method: 'POST', body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
```

`console/src/features/tag-management/api/update-tag.ts`:
```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { Tag, UpdateTagRequest } from '@/proto/attune/v1/tag'

export function useUpdateTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateTagRequest & { id: string }) =>
      api<Tag>(`/fb/v1/console/tags/${id}`, { method: 'PATCH', body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
```

`console/src/features/tag-management/api/archive-tag.ts`:
```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export function useArchiveTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<void>(`/fb/v1/console/tags/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
```

- [ ] **Step 2: Create feedback tag API hooks**

`console/src/features/feedback/api/add-feedback-tag.ts`:
```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { AddFeedbackTagResponse } from '@/proto/attune/v1/tag'

interface AddTagInput {
  feedbackId: number
  tagId?: string
  tagName?: string
  tagColor?: string
}

export function useAddFeedbackTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ feedbackId, ...body }: AddTagInput) =>
      api<AddFeedbackTagResponse>(`/fb/v1/console/feedback/${feedbackId}/tags`, {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
```

`console/src/features/feedback/api/remove-feedback-tag.ts`:
```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

interface RemoveTagInput {
  feedbackId: number
  tagId: string
}

export function useRemoveFeedbackTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ feedbackId, tagId }: RemoveTagInput) =>
      api<void>(`/fb/v1/console/feedback/${feedbackId}/tags/${tagId}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd console && pnpm tsc -b --noEmit`

Expected: zero errors.

- [ ] **Step 4: Commit**

```bash
git add console/src/features/tag-management/api/ console/src/features/feedback/api/add-feedback-tag.ts console/src/features/feedback/api/remove-feedback-tag.ts
git commit -m "feat(console): add tag API hooks for management and feedback assignment (#28)"
```

---

### Task 11: Shared tag UI components

**Files:**
- Create: `console/src/components/tag/tag-chip.tsx`
- Create: `console/src/components/tag/tag-picker.tsx`
- Create: `console/src/components/tag/color-picker.tsx`

- [ ] **Step 1: Create TagChip**

`console/src/components/tag/tag-chip.tsx`:
```tsx
import { X } from 'lucide-react'

interface TagChipProps {
  name: string
  color: string
  onRemove?: () => void
  onClick?: () => void
}

export function TagChip({ name, color, onRemove, onClick }: TagChipProps) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium cursor-default"
      onClick={onClick}
      role={onClick ? 'button' : undefined}
    >
      <span
        className="inline-block h-2 w-2 rounded-full"
        style={{ backgroundColor: color }}
      />
      {name}
      {onRemove ? (
        <button
          type="button"
          className="ml-0.5 rounded-full p-0.5 hover:bg-muted"
          onClick={(e) => {
            e.stopPropagation()
            onRemove()
          }}
        >
          <X className="h-3 w-3" />
        </button>
      ) : null}
    </span>
  )
}
```

- [ ] **Step 2: Create ColorPicker**

`console/src/components/tag/color-picker.tsx`:
```tsx
import { useState } from 'react'
import { Check } from 'lucide-react'

const PALETTE = [
  '#ef4444', '#f97316', '#eab308', '#22c55e', '#14b8a6', '#3b82f6',
  '#6366f1', '#8b5cf6', '#ec4899', '#f43f5e', '#0ea5e9', '#84cc16',
]

interface ColorPickerProps {
  value: string
  onChange: (color: string) => void
}

export function ColorPicker({ value, onChange }: ColorPickerProps) {
  return (
    <div className="grid grid-cols-6 gap-1.5 p-2">
      {PALETTE.map((c) => (
        <button
          key={c}
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded-full border border-transparent hover:border-foreground/20"
          style={{ backgroundColor: c }}
          onClick={() => onChange(c)}
        >
          {value === c ? <Check className="h-3 w-3 text-white" /> : null}
        </button>
      ))}
    </div>
  )
}
```

- [ ] **Step 3: Create TagPicker**

`console/src/components/tag/tag-picker.tsx`:
```tsx
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Tag } from '@/proto/attune/v1/tag'

interface TagPickerProps {
  tags: Tag[]
  assignedTagIds: string[]
  onAdd: (input: { tagId?: string; tagName?: string }) => void
  loading?: boolean
}

export function TagPicker({ tags, assignedTagIds, onAdd, loading }: TagPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const available = tags.filter(
    (tag) => !tag.archived && !assignedTagIds.includes(tag.id),
  )
  const filtered = query
    ? available.filter((tag) =>
        tag.name.toLowerCase().includes(query.toLowerCase()),
      )
    : available

  const exactMatch = tags.some(
    (tag) => tag.name.toLowerCase() === query.toLowerCase(),
  )

  const handleSelect = (tagId: string) => {
    onAdd({ tagId })
    setQuery('')
    setOpen(false)
  }

  const handleCreate = () => {
    if (!query.trim()) return
    onAdd({ tagName: query.trim() })
    setQuery('')
    setOpen(false)
  }

  return (
    <div className="relative">
      <div className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          className="h-6 gap-1 px-2 text-xs"
          onClick={() => {
            setOpen(!open)
            setTimeout(() => inputRef.current?.focus(), 0)
          }}
          disabled={loading}
        >
          <Plus className="h-3 w-3" />
          {t('feedback.tags.add')}
        </Button>
      </div>
      {open ? (
        <div className="absolute top-full left-0 z-50 mt-1 w-56 rounded-md border bg-popover p-1 shadow-md">
          <Input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('feedback.tags.search')}
            className="h-7 text-xs"
            onKeyDown={(e) => {
              if (e.key === 'Escape') setOpen(false)
              if (e.key === 'Enter' && query.trim() && !exactMatch) handleCreate()
            }}
          />
          <div className="mt-1 max-h-40 overflow-y-auto">
            {filtered.map((tag) => (
              <button
                key={tag.id}
                type="button"
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1 text-xs hover:bg-accent"
                onClick={() => handleSelect(tag.id)}
              >
                <span
                  className="inline-block h-2 w-2 rounded-full"
                  style={{ backgroundColor: tag.color }}
                />
                {tag.name}
              </button>
            ))}
            {query.trim() && !exactMatch ? (
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1 text-xs text-muted-foreground hover:bg-accent"
                onClick={handleCreate}
              >
                <Plus className="h-3 w-3" />
                {t('feedback.tags.create_new', { name: query.trim() })}
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
```

- [ ] **Step 4: Verify TypeScript compiles and lint passes**

Run: `cd console && pnpm tsc -b --noEmit && pnpm biome check src/components/tag/`

Expected: zero errors.

- [ ] **Step 5: Commit**

```bash
git add console/src/components/tag/
git commit -m "feat(console): add shared TagChip, TagPicker, and ColorPicker components (#28)"
```

---

### Task 12: Tag management Settings page

**Files:**
- Create: `console/src/features/tag-management/components/tag-management-page.tsx`
- Modify: `console/src/routes/_authed.settings.tsx`

- [ ] **Step 1: Create tag management page**

`console/src/features/tag-management/components/tag-management-page.tsx`:
```tsx
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Archive, Plus } from 'lucide-react'
import { ColorPicker } from '@/components/tag/color-picker'
import { TagChip } from '@/components/tag/tag-chip'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { useArchiveTag } from '../api/archive-tag'
import { useCreateTag } from '../api/create-tag'
import { tagsQuery } from '../api/list-tags'
import { useUpdateTag } from '../api/update-tag'

export function TagManagementPage() {
  const { t } = useTranslation()
  const tags = useQuery(tagsQuery(true))
  const createTag = useCreateTag()
  const updateTag = useUpdateTag()
  const archiveTag = useArchiveTag()

  const [newName, setNewName] = useState('')
  const [newColor, setNewColor] = useState('#3b82f6')
  const [newScope, setNewScope] = useState('')
  const [showArchived, setShowArchived] = useState(false)

  const handleCreate = () => {
    if (!newName.trim()) return
    createTag.mutate(
      {
        name: newName.trim(),
        color: newColor,
        exclusiveScope: newScope.trim() || undefined,
      },
      {
        onSuccess: () => {
          setNewName('')
          setNewScope('')
          toast.success(t('common.saved'))
        },
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handleArchive = (id: string) => {
    if (!confirm(t('tags.archive_confirm'))) return
    archiveTag.mutate(id, {
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : 'failed'),
    })
  }

  const visibleTags = (tags.data ?? []).filter(
    (tag) => showArchived || !tag.archived,
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('tags.title')}</CardTitle>
        <CardDescription>{t('tags.subtitle')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <Input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={t('tags.name')}
              onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
            />
          </div>
          <ColorPicker value={newColor} onChange={setNewColor} />
          <div className="w-32">
            <Input
              value={newScope}
              onChange={(e) => setNewScope(e.target.value)}
              placeholder={t('tags.scope')}
            />
          </div>
          <Button onClick={handleCreate} disabled={createTag.isPending}>
            <Plus className="mr-1 h-4 w-4" />
            {t('tags.create')}
          </Button>
        </div>

        <div className="flex items-center justify-between">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
            />
            {t('tags.show_archived')}
          </label>
        </div>

        {visibleTags.length === 0 ? (
          <div className="py-8 text-center text-muted-foreground">
            <p>{t('tags.empty')}</p>
            <p className="text-sm">{t('tags.empty_hint')}</p>
          </div>
        ) : (
          <div className="space-y-2">
            {visibleTags.map((tag) => (
              <div
                key={tag.id}
                className="flex items-center gap-3 rounded border p-2"
              >
                <TagChip name={tag.name} color={tag.color} />
                {tag.exclusiveScope ? (
                  <span className="text-xs text-muted-foreground">
                    {tag.exclusiveScope}
                  </span>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('tags.usage')}: {tag.usageCount}
                </span>
                {tag.archived ? (
                  <span className="text-xs text-muted-foreground">
                    {t('tags.archived')}
                  </span>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleArchive(tag.id)}
                  >
                    <Archive className="h-4 w-4" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 2: Add tags section to settings**

In `console/src/routes/_authed.settings.tsx`:

1. Add `'tags'` to the `SettingsSection` union type.
2. Add `'tags'` to the `SETTINGS_SECTIONS` array.
3. Add import: `import { TagManagementPage } from '@/features/tag-management/components/tag-management-page'`
4. Add import: `import { Tag } from 'lucide-react'` (the icon)
5. In `SettingsSectionContent`, add: `{section === 'tags' ? <TagManagementPage /> : null}`
6. In `SettingsSidebar` `areas` array, add the tags entry:
```typescript
{
  section: 'tags',
  icon: Tag,
  title: t('settings.areas.tags.title'),
  body: t('settings.areas.tags.body'),
},
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd console && pnpm tsc -b --noEmit`

Expected: zero errors.

- [ ] **Step 4: Commit**

```bash
git add console/src/features/tag-management/components/ console/src/routes/_authed.settings.tsx
git commit -m "feat(console): add tag management Settings page (#28)"
```

---

### Task 13: Feedback detail tag integration

**Files:**
- Modify: `console/src/features/feedback/components/detail-sheet.tsx`

- [ ] **Step 1: Add tag chips and picker to feedback detail**

Read `detail-sheet.tsx` first to understand the current structure. Then add:

1. Import TagChip, TagPicker, and the API hooks:
```typescript
import { TagChip } from '@/components/tag/tag-chip'
import { TagPicker } from '@/components/tag/tag-picker'
import { useAddFeedbackTag } from '../api/add-feedback-tag'
import { useRemoveFeedbackTag } from '../api/remove-feedback-tag'
import { tagsQuery } from '@/features/tag-management/api/list-tags'
```

2. Inside the detail component, add hooks:
```typescript
const allTags = useQuery(tagsQuery())
const addTag = useAddFeedbackTag()
const removeTag = useRemoveFeedbackTag()
```

3. After the dimension chips section, render tag chips and picker:
```tsx
<div className="flex flex-wrap items-center gap-1">
  {(detail.tags ?? []).map((tag) => (
    <TagChip
      key={tag.id}
      name={tag.name}
      color={tag.color}
      onRemove={() => removeTag.mutate({ feedbackId: detail.id, tagId: tag.id })}
    />
  ))}
  <TagPicker
    tags={allTags.data ?? []}
    assignedTagIds={(detail.tags ?? []).map((t) => t.id)}
    onAdd={(input) =>
      addTag.mutate({ feedbackId: detail.id, ...input })
    }
    loading={addTag.isPending}
  />
</div>
```

Note: the dependency-cruiser `no-cross-feature` rule means `detail-sheet.tsx` (in `features/feedback/`) imports from `@/features/tag-management/api/list-tags.ts`. This is a cross-feature import. To avoid the violation, either:
- Move `tagsQuery` to `@/components/tag/` (shared layer), or
- Create a thin re-export in `@/features/feedback/api/` that wraps the same API call.

Choose whichever approach the codebase prefers — check `console/.dependency-cruiser.cjs` for exact rules. The safest option is to duplicate the `tagsQuery` in `@/features/feedback/api/list-tags.ts` as a self-contained query (same pattern, different file).

- [ ] **Step 2: Verify TypeScript compiles and lint passes**

Run: `cd console && pnpm tsc -b --noEmit && pnpm biome check src/features/feedback/`

Expected: zero errors.

- [ ] **Step 3: Run dependency-cruiser**

Run: `cd console && pnpm arch`

Expected: zero violations.

- [ ] **Step 4: Commit**

```bash
git add console/src/features/feedback/
git commit -m "feat(console): integrate tag chips and picker in feedback detail (#28)"
```

---

### Task 14: i18n keys

**Files:**
- Modify: `console/src/i18n/zh-CN.json`

- [ ] **Step 1: Add tag i18n keys**

Add the following top-level keys to `zh-CN.json`:

```json
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
}
```

Add under the existing `"feedback"` key or as a new `"feedback.tags"` section:

```json
"feedback": {
  ...existing keys...,
  "tags": {
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

Add under `"settings.areas"`:

```json
"tags": {
  "title": "标签管理",
  "body": "创建和管理反馈标签"
}
```

- [ ] **Step 2: Verify JSON is valid**

Run: `cd console && node -e "JSON.parse(require('fs').readFileSync('src/i18n/zh-CN.json','utf8'))"`

Expected: no error.

- [ ] **Step 3: Commit**

```bash
git add console/src/i18n/zh-CN.json
git commit -m "feat(i18n): add Chinese tag management keys (#28)"
```

---

### Task 15: CHANGELOG + proposal status

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/proposals/2026/06/2026-06-14-feedback-manual-tags.md`

- [ ] **Step 1: Add CHANGELOG entry**

Under `## [Unreleased]`, add to `### Added` (create the section if it doesn't exist):

```markdown
- **Manual tagging on feedback rows** — per-tenant tag registry with colors, descriptions, exclusive scopes, and archival; junction table with per-assignment audit trail (`created_by` / `created_at`); `FeedbackTagService` proto contract (7 RPCs); Console tag management Settings page and inline tag picker on feedback detail; filter-by-tag on feedback list (#28)
```

- [ ] **Step 2: Update proposal status**

In `docs/proposals/2026/06/2026-06-14-feedback-manual-tags.md`, change `Status` from `Proposed` to `Implemented`.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md docs/proposals/2026/06/2026-06-14-feedback-manual-tags.md
git commit -m "docs: add changelog entry and mark proposal as implemented (#28)"
```

---

### Task 16: Final verification

- [ ] **Step 1: Run all Go checks**

```bash
go vet ./...
go build ./...
golangci-lint run ./...
lizard . -l go -C 15 -T nloc=100 --warnings_only
npx -y jscpd . -f go -i '**/*.pb.go' -t 4 --silent
scripts/lint-slog.sh --strict
scripts/lint-rawptr.sh
scripts/lint-errorcode.sh
```

Expected: all pass.

- [ ] **Step 2: Run Go tests**

```bash
go test -race ./...
```

Expected: all pass.

- [ ] **Step 3: Run Console checks**

```bash
cd console
pnpm tsc -b --noEmit
pnpm biome check
pnpm exec vite build
pnpm vitest run
pnpm arch
```

Expected: all pass.

- [ ] **Step 4: Run integration tests**

```bash
make test-integration
```

Expected: all pass, including new tag tests.

- [ ] **Step 5: Proto sync check**

```bash
make proto
git diff --exit-code internal/proto/ console/src/proto/ docs/openapi/
```

Expected: no drift.
