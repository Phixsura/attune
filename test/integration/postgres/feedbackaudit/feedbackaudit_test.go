//go:build integration

package feedbackaudit_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/testdb"
)

func setup(t *testing.T) (context.Context, *feedbackaudit.Repo, *pgxpool.Pool, string, int64) {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()

	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('audit-io','Audit IO')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	var feedbackID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', 'test content')
		RETURNING id`, tenantID).Scan(&feedbackID) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert feedback: %v", err)
	}

	return ctx, feedbackaudit.New(pool), pool, tenantID, feedbackID
}

func TestPG_WriteAndList(t *testing.T) {
	ctx, repo, _, tenantID, feedbackID := setup(t)

	if err := repo.Write(ctx, feedbackaudit.Entry{
		TenantID:   tenantID,
		FeedbackID: feedbackID,
		EntityType: "workflow_state",
		FieldName:  "state_id",
		OldValue:   nil,
		NewValue:   ptrext.Of("new-state-id"),
		Comment:    "initial triage",
		ChangedBy:  "user-1",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries, nextCursor, err := repo.List(ctx, feedbackID, "", 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List len = %d want 1", len(entries))
	}
	if nextCursor != "" {
		t.Fatalf("unexpected cursor: %q", nextCursor)
	}
	e := entries[0]
	if e.EntityType != "workflow_state" || e.FieldName != "state_id" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.OldValue != nil {
		t.Fatalf("OldValue should be nil, got %q", ptrext.Indirect(e.OldValue))
	}
	if ptrext.Indirect(e.NewValue) != "new-state-id" {
		t.Fatalf("NewValue = %q want %q", ptrext.Indirect(e.NewValue), "new-state-id")
	}
	if e.Comment != "initial triage" || e.ChangedBy != "user-1" {
		t.Fatalf("comment/changed_by mismatch: %+v", e)
	}
}

func TestPG_Pagination(t *testing.T) {
	ctx, repo, _, tenantID, feedbackID := setup(t)

	for i := 0; i < 5; i++ {
		if err := repo.Write(ctx, feedbackaudit.Entry{
			TenantID:   tenantID,
			FeedbackID: feedbackID,
			EntityType: "workflow_state",
			FieldName:  "state_id",
			NewValue:   ptrext.Of("state-" + string(rune('A'+i))),
			ChangedBy:  "user-1",
		}); err != nil {
			t.Fatalf("Write[%d]: %v", i, err)
		}
	}

	page1, cursor1, err := repo.List(ctx, feedbackID, "", 3)
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len = %d want 3", len(page1))
	}
	if cursor1 == "" {
		t.Fatal("expected cursor after page1")
	}

	page2, cursor2, err := repo.List(ctx, feedbackID, cursor1, 3)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d want 2", len(page2))
	}
	if cursor2 != "" {
		t.Fatalf("expected empty cursor after page2, got %q", cursor2)
	}

	if page1[0].ID <= page1[1].ID {
		t.Fatal("entries should be ordered newest-first")
	}
}

func TestPG_WriteTx(t *testing.T) {
	ctx, repo, pool, tenantID, feedbackID := setup(t)
	_ = repo

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := feedbackaudit.New(pool).WriteTx(ctx, tx, feedbackaudit.Entry{
		TenantID:   tenantID,
		FeedbackID: feedbackID,
		EntityType: "workflow_state",
		FieldName:  "state_id",
		NewValue:   ptrext.Of("via-tx"),
		ChangedBy:  "user-1",
	}); err != nil {
		t.Fatalf("WriteTx: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	entries, _, err := repo.List(ctx, feedbackID, "", 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List len = %d want 1", len(entries))
	}
	if ptrext.Indirect(entries[0].NewValue) != "via-tx" {
		t.Fatalf("NewValue = %q want %q", ptrext.Indirect(entries[0].NewValue), "via-tx")
	}
}
