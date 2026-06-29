# Tenant Isolation Contract Suite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a three-layer tenant isolation test suite (repo contract, per-domain edge cases, HTTP black-box) proving cross-tenant data never leaks across any auth surface.

**Architecture:** Layer A is a centralized table-driven contract at the repo layer. Layer B adds per-domain boundary tests in existing integration packages. Layer C starts a real HTTP server with all three auth surfaces and verifies end-to-end isolation. All layers share a `Fixture` that seeds two fully-populated tenants.

**Tech Stack:** Go 1.23+, pgxpool, testdb (testcontainers), chi, httptest, session.Signer, apikey.WithAuthForTest

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `test/integration/postgres/isolation/doc.go` | Package doc + build tag |
| Create | `test/integration/postgres/isolation/fixture.go` | Two-tenant fixture factory |
| Create | `test/integration/postgres/isolation/contract_test.go` | Layer A: table-driven repo isolation |
| Create | `test/integration/postgres/isolation/http_test.go` | Layer C: HTTP black-box isolation |
| Create | `test/integration/postgres/workflowstate/isolation_test.go` | Layer B: workflow state isolation |
| Create | `test/integration/postgres/apikey/isolation_test.go` | Layer B: API key isolation |
| Create | `test/integration/postgres/auditlog/isolation_test.go` | Layer B: audit log isolation |
| Create | `test/integration/postgres/gdpr/isolation_test.go` | Layer B: GDPR isolation |
| Create | `test/integration/postgres/feedbacktag/isolation_test.go` | Layer B: feedback tag isolation |
| Modify | `CHANGELOG.md` | Add entry under `[Unreleased] ### Added` |

---

### Task 1: Shared fixture — package scaffold and two-tenant seeder

**Files:**
- Create: `test/integration/postgres/isolation/doc.go`
- Create: `test/integration/postgres/isolation/fixture.go`

- [ ] **Step 1: Create package doc with build tag**

```go
// file: test/integration/postgres/isolation/doc.go
//go:build integration

// Package isolation provides a three-layer tenant isolation contract suite.
// Layer A: table-driven repo-level isolation. Layer B: per-domain edge cases.
// Layer C: HTTP black-box through all auth surfaces.
package isolation
```

- [ ] **Step 2: Create the Fixture struct and NewFixture factory**

Create `test/integration/postgres/isolation/fixture.go` with:

```go
//go:build integration

package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/feedback"
	feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/gdpr"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/testdb"
)

// TenantData holds all seeded IDs for one tenant.
type TenantData struct {
	TenantID    string
	Slug        string
	FeedbackID  int64
	TagID       string
	WorkflowID  string
	OutboxID    int64
	APIKeyID    uuid.UUID
	APIKeyHash  []byte
	NotifyID    string
}

// Fixture provides two fully-seeded tenants on an isolated DB.
type Fixture struct {
	Pool    *pgxpool.Pool
	TenantA TenantData
	TenantB TenantData
	Ctx     context.Context

	// Repos exposed for direct use in contract tests.
	Feedback      *feedback.FeedbackRepo
	Tags          *feedbacktagrepo.Repo
	Workflow      *workflowstate.Repo
	Outbox        *outboxrepo.OutboxRepo
	AuditLog      *auditlog.Repo
	APIKeys       *apikeyrepo.APIKeyRepo
	NotifyTargets *notifytarget.NotifyTargetRepo
	GDPR          *gdpr.Repo
}

// NewFixture creates an isolated Postgres database and seeds two tenants with
// data across all domains. Each test calling NewFixture gets its own database.
func NewFixture(t *testing.T) *Fixture {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenants := tenant.NewTenant(pool)
	tidA, err := tenants.Create(ctx, "iso-tenant-a", "Tenant A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "iso-tenant-b", "Tenant B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	f := &Fixture{
		Pool: pool,
		Ctx:  ctx,
		TenantA: TenantData{TenantID: tidA, Slug: "iso-tenant-a"},
		TenantB: TenantData{TenantID: tidB, Slug: "iso-tenant-b"},
		Feedback:      feedback.NewFeedback(pool),
		Tags:          feedbacktagrepo.New(pool),
		Workflow:      workflowstate.New(pool),
		Outbox:        outboxrepo.NewOutbox(pool),
		AuditLog:      auditlog.New(pool),
		APIKeys:       apikeyrepo.NewAPIKey(pool),
		NotifyTargets: notifytarget.NewNotifyTarget(pool),
		GDPR:          gdpr.New(pool),
	}

	f.seedTenant(t, &f.TenantA)
	f.seedTenant(t, &f.TenantB)
	return f
}
```

The exact seed methods depend on each repo's Insert signature. We'll add them in
the next step after verifying exact signatures.

- [ ] **Step 3: Implement seedTenant with per-domain seed helpers**

Add seed methods to `fixture.go`. Each helper inserts one row per domain for the
given tenant. Use raw SQL where repo Insert methods require complex dependencies
(like outbox needing a pgx.Tx from feedback insert). Key patterns:

```go
func (f *Fixture) seedTenant(t *testing.T, td *TenantData) {
	t.Helper()
	td.FeedbackID = f.seedFeedback(t, td.TenantID)
	td.TagID = f.seedTag(t, td.TenantID)
	td.WorkflowID = f.seedWorkflowState(t, td.TenantID)
	td.OutboxID = f.seedOutbox(t, td.TenantID, td.FeedbackID)
	td.APIKeyID, td.APIKeyHash = f.seedAPIKey(t, td.TenantID)
	td.NotifyID = f.seedNotifyTarget(t, td.TenantID)
	f.seedAuditLog(t, td.TenantID)
}

func (f *Fixture) seedFeedback(t *testing.T, tenantID string) int64 {
	t.Helper()
	var id int64
	err := f.Pool.QueryRow(f.Ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'seed-user', 'api', 'isolation test content')
		RETURNING id`, tenantID).Scan(&id)
	if err != nil {
		t.Fatalf("seed feedback for %s: %v", tenantID, err)
	}
	return id
}

func (f *Fixture) seedTag(t *testing.T, tenantID string) string {
	t.Helper()
	tag, err := f.Tags.Create(f.Ctx, feedbacktagrepo.CreateParams{
		TenantID: tenantID,
		Key:      "iso-tag-" + tenantID[:8],
		Label:    "Isolation Tag",
		Color:    "#FF0000",
	})
	if err != nil {
		t.Fatalf("seed tag for %s: %v", tenantID, err)
	}
	return tag.ID
}

func (f *Fixture) seedWorkflowState(t *testing.T, tenantID string) string {
	t.Helper()
	ws, err := f.Workflow.Create(f.Ctx, workflowstate.WorkflowState{
		TenantID: tenantID,
		Label:    "Isolation State",
		Color:    "#00FF00",
	})
	if err != nil {
		t.Fatalf("seed workflow state for %s: %v", tenantID, err)
	}
	return ws.ID
}

func (f *Fixture) seedOutbox(t *testing.T, tenantID string, feedbackID int64) int64 {
	t.Helper()
	var id int64
	err := f.Pool.QueryRow(f.Ctx, `
		INSERT INTO notify_outbox
			(tenant_id, feedback_id, dest_type, audience, payload, status)
		VALUES ($1, $2, 'raw_webhook', 'pool', '{}', 'pending')
		RETURNING id`, tenantID, feedbackID).Scan(&id)
	if err != nil {
		t.Fatalf("seed outbox for %s: %v", tenantID, err)
	}
	return id
}

func (f *Fixture) seedAPIKey(t *testing.T, tenantID string) (uuid.UUID, []byte) {
	t.Helper()
	hash := []byte("fakehash-" + tenantID[:8])
	id, err := f.APIKeys.Insert(f.Ctx, tenantID, hash, "attn_iso", "isolation-key")
	if err != nil {
		t.Fatalf("seed api key for %s: %v", tenantID, err)
	}
	return id, hash
}

func (f *Fixture) seedNotifyTarget(t *testing.T, tenantID string) string {
	t.Helper()
	var id string
	err := f.Pool.QueryRow(f.Ctx, `
		INSERT INTO notify_targets
			(tenant_id, dest_type, audience, url, secret, timeout_seconds, is_active)
		VALUES ($1, 'raw_webhook', 'pool', 'http://127.0.0.1:19999/hook', '0123456789abcdef', 3, true)
		RETURNING id`, tenantID).Scan(&id)
	if err != nil {
		t.Fatalf("seed notify target for %s: %v", tenantID, err)
	}
	return id
}

func (f *Fixture) seedAuditLog(t *testing.T, tenantID string) {
	t.Helper()
	err := f.AuditLog.Insert(f.Ctx, auditlog.Entry{
		TenantID:   tenantID,
		ActorType:  "user",
		ActorID:    "seed-user",
		Action:     "tag.create",
		TargetType: "tag",
		TargetID:   "seed-target",
		Summary:    "isolation test audit entry",
	})
	if err != nil {
		t.Fatalf("seed audit log for %s: %v", tenantID, err)
	}
}
```

- [ ] **Step 4: Verify fixture compiles**

Run:
```bash
go build -tags=integration ./test/integration/postgres/isolation/...
```
Expected: 0 errors. Fix any import path or signature mismatches.

- [ ] **Step 5: Commit**

```bash
git add test/integration/postgres/isolation/doc.go test/integration/postgres/isolation/fixture.go
git commit -m "test(isolation): add two-tenant fixture for #154 contract suite

Closes #154 (partial) — shared fixture factory that seeds feedback, tags,
workflow states, outbox, API keys, notify targets, and audit log for two
isolated tenants."
```

---

### Task 2: Layer A — table-driven repo isolation contract

**Files:**
- Create: `test/integration/postgres/isolation/contract_test.go`

- [ ] **Step 1: Write the contract test with table cases**

```go
//go:build integration

package isolation

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

// isolationCase defines one repo-level cross-tenant access attempt.
type isolationCase struct {
	Domain    string // "feedback", "tags", "workflow", ...
	Operation string // "get", "list", "update", "delete"
	// Exec runs the operation using Tenant A's identity against Tenant B's
	// resource. Returns nil only if the operation succeeds — which is a breach.
	Exec func(ctx context.Context, f *Fixture) error
}

func TestRepoIsolationContract(t *testing.T) {
	f := NewFixture(t)

	cases := []isolationCase{
		// --- Feedback ---
		{
			Domain: "feedback", Operation: "sample_enriched",
			Exec: func(ctx context.Context, f *Fixture) error {
				rows, err := f.Feedback.SampleEnrichedByTenant(ctx, f.TenantA.TenantID,
					time.Now().Add(-time.Hour), 100)
				if err != nil {
					return err
				}
				for _, r := range rows {
					if r.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("returned row from tenant %s", r.TenantID)
					}
				}
				return nil
			},
		},

		// --- Tags ---
		{
			Domain: "tags", Operation: "get_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Tags.GetByID(ctx, f.TenantA.TenantID, f.TenantB.TagID)
				if err != nil {
					return nil // Expected: not found
				}
				return fmt.Errorf("got tenant B's tag from tenant A")
			},
		},
		{
			Domain: "tags", Operation: "list_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				tags, err := f.Tags.ListByTenant(ctx, f.TenantA.TenantID)
				if err != nil {
					return err
				}
				for _, tag := range tags {
					if tag.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("list returned tag from tenant %s", tag.TenantID)
					}
				}
				return nil
			},
		},

		// --- Workflow States ---
		{
			Domain: "workflow", Operation: "get_by_tenant_and_id",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Workflow.GetByTenantAndID(ctx, f.TenantA.TenantID, f.TenantB.WorkflowID)
				if err != nil {
					return nil // Expected: not found
				}
				return fmt.Errorf("got tenant B's workflow state from tenant A")
			},
		},
		{
			Domain: "workflow", Operation: "list_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				states, err := f.Workflow.List(ctx, f.TenantA.TenantID, true)
				if err != nil {
					return err
				}
				for _, s := range states {
					if s.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("list returned state from tenant %s", s.TenantID)
					}
				}
				return nil
			},
		},

		// --- Outbox ---
		{
			Domain: "outbox", Operation: "get_by_id",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Outbox.GetByID(ctx, f.TenantA.TenantID, f.TenantB.OutboxID)
				if err != nil {
					return nil // Expected: not found
				}
				return fmt.Errorf("got tenant B's outbox row from tenant A")
			},
		},
		{
			Domain: "outbox", Operation: "list_by_status",
			Exec: func(ctx context.Context, f *Fixture) error {
				rows, err := f.Outbox.ListByStatus(ctx, f.TenantA.TenantID,
					[]string{"pending", "delivered", "failed", "dead"}, 100, 0)
				if err != nil {
					return err
				}
				for _, r := range rows {
					if r.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("list returned outbox row from tenant %s", r.TenantID)
					}
				}
				return nil
			},
		},

		// --- Audit Log ---
		{
			Domain: "audit_log", Operation: "list_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				result, err := f.AuditLog.List(ctx, auditlog.ListFilter{
					TenantID: f.TenantA.TenantID,
					Limit:    100,
				})
				if err != nil {
					return err
				}
				for _, e := range result.Entries {
					if e.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("list returned audit entry from tenant %s", e.TenantID)
					}
				}
				return nil
			},
		},

		// --- API Keys ---
		{
			Domain: "api_keys", Operation: "get_by_id",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.APIKeys.GetByID(ctx, f.TenantA.TenantID, f.TenantB.APIKeyID)
				if err != nil {
					return nil // Expected: not found
				}
				return fmt.Errorf("got tenant B's API key from tenant A")
			},
		},
		{
			Domain: "api_keys", Operation: "list_by_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				keys, err := f.APIKeys.ListByTenant(ctx, f.TenantA.TenantID)
				if err != nil {
					return err
				}
				for _, k := range keys {
					if k.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("list returned key from tenant %s", k.TenantID)
					}
				}
				return nil
			},
		},
		{
			Domain: "api_keys", Operation: "revoke_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				err := f.APIKeys.Revoke(ctx, f.TenantA.TenantID, f.TenantB.APIKeyID)
				if err != nil {
					return nil // Expected: not found / no rows affected
				}
				return fmt.Errorf("revoked tenant B's API key from tenant A")
			},
		},

		// --- Notify Targets ---
		{
			Domain: "notify_targets", Operation: "get_by_id",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.NotifyTargets.GetByID(ctx, f.TenantA.TenantID, mustUUID(t, f.TenantB.NotifyID))
				if err != nil {
					return nil // Expected: not found
				}
				return fmt.Errorf("got tenant B's notify target from tenant A")
			},
		},
		{
			Domain: "notify_targets", Operation: "delete_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				err := f.NotifyTargets.Delete(ctx, f.TenantA.TenantID, mustUUID(t, f.TenantB.NotifyID))
				if err != nil {
					return nil // Expected: not found
				}
				return fmt.Errorf("deleted tenant B's notify target from tenant A")
			},
		},

		// --- GDPR ---
		{
			Domain: "gdpr", Operation: "export_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				data, err := f.GDPR.Export(ctx, f.TenantA.TenantID, "seed-user")
				if err != nil {
					return nil // Expected: not found or empty
				}
				if data != nil && data.Counts.FeedbackCount > 0 {
					return fmt.Errorf("GDPR export returned data from cross-tenant query")
				}
				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Domain+"/"+tc.Operation, func(t *testing.T) {
			err := tc.Exec(f.Ctx, f)
			if err != nil {
				t.Errorf("ISOLATION BREACH: domain=%s op=%s: %v",
					tc.Domain, tc.Operation, err)
			}
		})
	}
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}
```

Note: the exact imports and type references must match the actual repo package
APIs. The implementing agent must verify each repo method signature by reading
the source before writing the test. In particular:

- `feedbacktagrepo.Repo` — check if it has `GetByID(ctx, tenantID, tagID)` or
  a different signature. Adjust accordingly.
- `notifytarget.NotifyTargetRepo` — `GetByID` takes `uuid.UUID` for the target
  ID. The fixture stores it as `string`, so parse with `uuid.Parse`.
- `auditlog.ListFilter` — verify the exact struct fields.
- `gdpr.Repo.Export` — verify return type and how to check emptiness.

- [ ] **Step 2: Run the contract test**

```bash
go test -tags=integration -count=1 -timeout=5m -run TestRepoIsolationContract ./test/integration/postgres/isolation/...
```
Expected: All subtests PASS. If any fail with compile errors, fix the repo
method signatures to match the actual code.

- [ ] **Step 3: Mutation smoke test — verify the contract catches a real breach**

Temporarily break isolation in one repo method (e.g., remove `AND tenant_id = $2`
from `workflowstate.GetByTenantAndID`), re-run the test, confirm it fails with
`ISOLATION BREACH: domain=workflow op=get_by_tenant_and_id`. Then revert the
change.

- [ ] **Step 4: Commit**

```bash
git add test/integration/postgres/isolation/contract_test.go
git commit -m "test(isolation): add Layer A repo-level contract table (#154)

Table-driven test covering feedback, tags, workflow, outbox, audit log,
API keys, notify targets, and GDPR domains. Each case attempts cross-tenant
access and asserts isolation."
```

---

### Task 3: Layer B — per-domain edge-case isolation tests

**Files:**
- Create: `test/integration/postgres/workflowstate/isolation_test.go`
- Create: `test/integration/postgres/apikey/isolation_test.go`
- Create: `test/integration/postgres/auditlog/isolation_test.go`
- Create: `test/integration/postgres/gdpr/isolation_test.go`
- Create: `test/integration/postgres/feedbacktag/isolation_test.go`

- [ ] **Step 1: Workflow state isolation edge cases**

```go
//go:build integration

package workflowstate_test

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_ListStates_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "wf-iso-a", "WF A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "wf-iso-b", "WF B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := workflowstate.New(pool)

	_, err = repo.Create(ctx, workflowstate.WorkflowState{
		TenantID: tidA, Label: "State A", Color: "#AA0000",
	})
	if err != nil {
		t.Fatalf("create state A: %v", err)
	}
	_, err = repo.Create(ctx, workflowstate.WorkflowState{
		TenantID: tidB, Label: "State B", Color: "#BB0000",
	})
	if err != nil {
		t.Fatalf("create state B: %v", err)
	}

	statesA, err := repo.List(ctx, tidA, true)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	for _, s := range statesA {
		if s.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: tenant A list returned state from %s", s.TenantID)
		}
	}

	statesB, err := repo.List(ctx, tidB, true)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	for _, s := range statesB {
		if s.TenantID != tidB {
			t.Errorf("ISOLATION BREACH: tenant B list returned state from %s", s.TenantID)
		}
	}
}

func TestPG_ArchiveState_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "wf-arch-a", "WF Arch A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "wf-arch-b", "WF Arch B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := workflowstate.New(pool)
	stateB, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID: tidB, Label: "B Only", Color: "#CC0000",
	})
	if err != nil {
		t.Fatalf("create state B: %v", err)
	}

	err = repo.Archive(ctx, tidA, stateB.ID)
	if err == nil {
		t.Error("ISOLATION BREACH: tenant A archived tenant B's workflow state")
	}
}
```

- [ ] **Step 2: API key isolation edge cases**

```go
//go:build integration

package apikey_test

import (
	"context"
	"testing"

	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_LookupByHash_TenantLeak(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "ak-iso-a", "AK A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "ak-iso-b", "AK B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := apikeyrepo.NewAPIKey(pool)
	hashB := []byte("hash-for-tenant-b-isolation")
	_, err = repo.Insert(ctx, tidB, hashB, "attn_biso", "b-key")
	if err != nil {
		t.Fatalf("insert B key: %v", err)
	}

	// LookupByHash is not tenant-scoped (it's the auth lookup path).
	// The returned row must carry the correct TenantID so the middleware
	// can scope the request. Verify TenantID is tidB, not tidA.
	row, err := repo.LookupByHash(ctx, hashB)
	if err != nil {
		t.Fatalf("lookup by hash: %v", err)
	}
	if row.TenantID != tidB {
		t.Errorf("ISOLATION BREACH: LookupByHash returned TenantID=%s, want %s", row.TenantID, tidB)
	}

	// GetByID with wrong tenant must fail.
	_, err = repo.GetByID(ctx, tidA, row.ID)
	if err == nil {
		t.Error("ISOLATION BREACH: GetByID with wrong tenant returned a key")
	}

	// Revoke with wrong tenant must not affect the key.
	err = repo.Revoke(ctx, tidA, row.ID)
	if err == nil {
		// Verify key still exists for correct tenant.
		_, err2 := repo.GetByID(ctx, tidB, row.ID)
		if err2 != nil {
			t.Error("ISOLATION BREACH: Revoke with wrong tenant actually revoked the key")
		}
	}
}

func TestPG_ListByTenant_Isolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "ak-list-a", "AK List A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "ak-list-b", "AK List B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := apikeyrepo.NewAPIKey(pool)
	_, err = repo.Insert(ctx, tidA, []byte("hash-a-list"), "attn_alis", "a-key")
	if err != nil {
		t.Fatalf("insert A key: %v", err)
	}
	_, err = repo.Insert(ctx, tidB, []byte("hash-b-list"), "attn_blis", "b-key")
	if err != nil {
		t.Fatalf("insert B key: %v", err)
	}

	keysA, err := repo.ListByTenant(ctx, tidA)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	for _, k := range keysA {
		if k.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: tenant A list returned key from %s", k.TenantID)
		}
	}
}
```

- [ ] **Step 3: Audit log isolation edge cases**

```go
//go:build integration

package auditlog_test

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_ListAuditLog_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "audit-iso-a", "Audit A")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "audit-iso-b", "Audit B")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	repo := auditlog.New(pool)

	// Seed entries for both tenants.
	for _, tid := range []string{tidA, tidB} {
		for i := 0; i < 3; i++ {
			err := repo.Insert(ctx, auditlog.Entry{
				TenantID:   tid,
				ActorType:  "user",
				ActorID:    "actor-" + tid[:8],
				Action:     "tag.create",
				TargetType: "tag",
				TargetID:   "target",
				Summary:    "isolation test",
			})
			if err != nil {
				t.Fatalf("insert audit for %s: %v", tid, err)
			}
		}
	}

	result, err := repo.List(ctx, auditlog.ListFilter{
		TenantID: tidA,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(result.Entries) == 0 {
		t.Fatal("expected audit entries for tenant A")
	}
	for _, e := range result.Entries {
		if e.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: tenant A audit list returned entry from %s (action=%s)",
				e.TenantID, e.Action)
		}
	}
}
```

- [ ] **Step 4: GDPR isolation edge cases**

```go
//go:build integration

package gdpr_test

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/repo/gdpr"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_Export_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "gdpr-iso-a", "GDPR A")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "gdpr-iso-b", "GDPR B")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Seed feedback for tenant B with a known user.
	_, err = pool.Exec(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'gdpr-subject', 'web', 'sensitive data')`, tidB)
	if err != nil {
		t.Fatalf("seed B feedback: %v", err)
	}

	repo := gdpr.New(pool)

	// Export from tenant A for the same subject key must not return B's data.
	data, err := repo.Export(ctx, tidA, "gdpr-subject")
	if err != nil {
		t.Fatalf("export A: %v", err)
	}
	if data != nil && data.Counts.FeedbackCount > 0 {
		t.Errorf("ISOLATION BREACH: GDPR export for tenant A returned %d feedback rows (subject exists only in B)",
			data.Counts.FeedbackCount)
	}
}
```

- [ ] **Step 5: Feedback tag assignment isolation**

```go
//go:build integration

package feedbacktag_test

import (
	"context"
	"testing"

	feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_ListTags_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "ft-iso-a", "FT A")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "ft-iso-b", "FT B")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	repo := feedbacktagrepo.New(pool)

	_, err = repo.Create(ctx, feedbacktagrepo.CreateParams{
		TenantID: tidA, Key: "tag-a", Label: "A", Color: "#AA0000",
	})
	if err != nil {
		t.Fatalf("create tag A: %v", err)
	}
	_, err = repo.Create(ctx, feedbacktagrepo.CreateParams{
		TenantID: tidB, Key: "tag-b", Label: "B", Color: "#BB0000",
	})
	if err != nil {
		t.Fatalf("create tag B: %v", err)
	}

	tagsA, err := repo.ListByTenant(ctx, tidA)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	for _, tag := range tagsA {
		if tag.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: tenant A tag list returned tag from %s (key=%s)",
				tag.TenantID, tag.Key)
		}
	}
}
```

- [ ] **Step 6: Run all Layer B tests**

```bash
go test -tags=integration -count=1 -timeout=5m \
  -run "TestPG_.*Isolation" \
  ./test/integration/postgres/workflowstate/... \
  ./test/integration/postgres/apikey/... \
  ./test/integration/postgres/auditlog/... \
  ./test/integration/postgres/gdpr/... \
  ./test/integration/postgres/feedbacktag/...
```
Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add test/integration/postgres/workflowstate/isolation_test.go \
  test/integration/postgres/apikey/isolation_test.go \
  test/integration/postgres/auditlog/isolation_test.go \
  test/integration/postgres/gdpr/isolation_test.go \
  test/integration/postgres/feedbacktag/isolation_test.go
git commit -m "test(isolation): add Layer B per-domain isolation edge cases (#154)

Covers workflow state list/archive, API key lookup/list/revoke, audit log
list, GDPR export, and feedback tag list. Each test seeds two tenants and
verifies cross-tenant access is denied."
```

---

### Task 4: Layer C — HTTP black-box isolation tests

**Files:**
- Create: `test/integration/postgres/isolation/http_test.go`

This is the most complex layer. We start a real `httptest.Server` with the full
console router + API-key admin routes, backed by real Postgres, and exercise
cross-tenant requests through each auth surface.

- [ ] **Step 1: Write the HTTP test infrastructure**

The test needs to:
1. Create a `testdb.NewPool(t)` for an isolated DB.
2. Build the console `Router` with real handler instances backed by the pool.
3. Mount the `MountAPIKeyAdminRoutes` with a real `apikey.Verifier`.
4. Create a `session.Signer` for cookie-based auth.
5. Start `httptest.Server`.

Since constructing the full `NewRouter` requires ~25 handler instances, we build
a minimal server that mounts only the routes we test. This keeps the test
focused and avoids nil panics from unwired handlers.

```go
//go:build integration

package isolation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// httpEnv bundles the test HTTP server with auth helpers for both tenants.
type httpEnv struct {
	Server  *httptest.Server
	SignerA *session.Signer
	CookieA string // signed session cookie value for tenant A
	CookieB string // signed session cookie value for tenant B
	APIKeyA string // raw API key for tenant A
	APIKeyB string // raw API key for tenant B
	Fixture *Fixture
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	f := NewFixture(t)

	// Build a session signer.
	signer, err := session.NewSigner("test-session-signing-key-32chars!!")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	// Build chi router with the API-key admin routes (tags, workflow, outbox,
	// audit-log, GDPR, MCP clients — the full admin surface).
	mux := chi.NewRouter()
	mux.Use(middleware.Recoverer)

	// Create a stub API key verifier that maps raw keys to tenant IDs.
	verifier := &stubVerifier{
		keys: map[string]apikey.AuthCtx{},
	}

	// Register API keys for both tenants.
	rawA := "attn_test_key_tenant_a_isolation"
	rawB := "attn_test_key_tenant_b_isolation"
	verifier.keys[rawA] = apikey.AuthCtx{
		TenantID: f.TenantA.TenantID,
		KeyID:    f.TenantA.APIKeyID,
		Scopes:   domain.AllScopes(),
	}
	verifier.keys[rawB] = apikey.AuthCtx{
		TenantID: f.TenantB.TenantID,
		KeyID:    f.TenantB.APIKeyID,
		Scopes:   domain.AllScopes(),
	}

	console.MountAPIKeyAdminRoutes(mux, f.Pool, verifier, 0,
		ratelimit.NewPerKeyLimiter(0), console.APIKeyAdminRouteOptions{})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &httpEnv{
		Server:  srv,
		APIKeyA: rawA,
		APIKeyB: rawB,
		Fixture: f,
	}
}

// stubVerifier is a test double for apikey.Verifier that returns pre-configured
// auth contexts based on the raw key string.
type stubVerifier struct {
	keys map[string]apikey.AuthCtx
}

// The exact method name depends on the apikey.Verifier interface. Check the
// actual interface and implement all required methods. The implementing agent
// must read internal/infra/apikey/middleware.go to find the exact interface.
```

- [ ] **Step 2: Write HTTP isolation test cases for API-key surface**

```go
func TestHTTP_APIKey_CrossTenantDenied(t *testing.T) {
	env := newHTTPEnv(t)

	// Test cases: use tenant A's API key to access tenant B's resources.
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list_tags", "GET", "/tags"},
		{"list_workflow_states", "GET", "/workflow/states"},
		{"list_workflow_transitions", "GET", "/workflow/transitions"},
		// Outbox, audit-log, GDPR, MCP client list endpoints go here.
		// The implementing agent must check the exact route paths from
		// apikey_admin.go and add them.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, env.Server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("X-API-Key", env.APIKeyA)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			// List endpoints should return 200 but contain ONLY tenant A's
			// data, never tenant B's IDs.
			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
			}

			// Verify no tenant B IDs appear in the response body.
			if containsAny(body,
				env.Fixture.TenantB.TenantID,
				env.Fixture.TenantB.TagID,
				env.Fixture.TenantB.WorkflowID,
				env.Fixture.TenantB.APIKeyID.String(),
			) {
				t.Errorf("ISOLATION BREACH: response contains tenant B data: %s", body)
			}
		})
	}
}

// containsAny returns true if body contains any of the given substrings.
func containsAny(body []byte, needles ...string) bool {
	s := string(body)
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Write HTTP isolation test for single-resource access with wrong tenant**

```go
func TestHTTP_APIKey_GetResourceWrongTenant(t *testing.T) {
	env := newHTTPEnv(t)

	// Use tenant A's key to GET tenant B's specific resource by ID.
	// These should return 404 (resource not found for this tenant).
	cases := []struct {
		name string
		path string
	}{
		{"get_tag", "/tags/" + env.Fixture.TenantB.TagID},
		{"get_workflow_state", "/workflow/states/" + env.Fixture.TenantB.WorkflowID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", env.Server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("X-API-Key", env.APIKeyA)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			// Expect 404 or 403, never 200 with tenant B's data.
			if resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("ISOLATION BREACH: got 200 for wrong-tenant resource: %s", body)
			}
		})
	}
}
```

- [ ] **Step 4: Verify the stub Verifier matches the real interface**

The implementing agent must read `internal/infra/apikey/middleware.go` to find
the exact `Verifier` interface and implement all methods on `stubVerifier`.
The interface likely includes:
- `LookupWithScopesAndIP(ctx, raw, clientIP string) (tenantID string, keyID uuid.UUID, scopes []domain.Scope, rpm *int, err error)`

Adjust `stubVerifier` to implement this exact signature.

- [ ] **Step 5: Run Layer C tests**

```bash
go test -tags=integration -count=1 -timeout=5m \
  -run "TestHTTP_" \
  ./test/integration/postgres/isolation/...
```
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add test/integration/postgres/isolation/http_test.go
git commit -m "test(isolation): add Layer C HTTP black-box isolation tests (#154)

Starts a real httptest.Server with API-key admin routes backed by real
Postgres. Verifies that cross-tenant requests through API-key auth surface
return 404 / empty, never the other tenant's data."
```

---

### Task 5: CHANGELOG and final verification

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Update CHANGELOG**

Add under `[Unreleased]`:

```markdown
### Added

- Three-layer tenant isolation contract suite (#154): repo-level contract table
  (Layer A), per-domain edge-case tests (Layer B), and HTTP black-box tests
  through API-key auth surface (Layer C). Covers feedback, tags, workflow, outbox,
  audit log, API keys, notify targets, GDPR, and feedback tags.
```

- [ ] **Step 2: Run full integration suite**

```bash
make test-integration
```
Expected: All tests pass, including all new isolation tests.

- [ ] **Step 3: Run quality gates**

```bash
go vet ./...
go build ./...
```
Expected: 0 warnings, 0 errors.

- [ ] **Step 4: Check code duplication**

```bash
npx -y jscpd . --silent
```
Expected: < 5% duplication.

- [ ] **Step 5: Check complexity**

```bash
lizard . -l go -C 15 -T nloc=100 --warnings_only
```
Expected: 0 warnings.

- [ ] **Step 6: Commit CHANGELOG**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): add tenant isolation contract suite entry (#154)"
```

- [ ] **Step 7: Final verification summary**

Cite the output of each gate run. Verify the test count increased. Report the
exact number of isolation test cases added across all three layers.
