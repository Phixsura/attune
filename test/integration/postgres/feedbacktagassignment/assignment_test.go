//go:build integration

package feedbacktagassignment_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	tags        *feedbacktag.Repo
	assignments *feedbacktagassignment.Repo
	tenantID    string
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "assign-io", "Assign IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return env{
		ctx:         ctx,
		pool:        pool,
		tags:        feedbacktag.New(pool),
		assignments: feedbacktagassignment.New(pool),
		tenantID:    tenantID,
	}
}

func (e env) createTag(t *testing.T, name string) *feedbacktag.Tag {
	t.Helper()
	tag, err := e.tags.Create(e.ctx, feedbacktag.Tag{
		TenantID: e.tenantID, Name: name, Color: "#000000",
	})
	if err != nil {
		t.Fatalf("create tag %q: %v", name, err)
	}
	return tag
}

func (e env) insertFeedback(t *testing.T, content string) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content)
		 VALUES ($1, 'u1', 'api', $2) RETURNING id`,
		e.tenantID, content,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return id
}

func TestPG_AssignmentAddRemove(t *testing.T) {
	e := setup(t)
	tag := e.createTag(t, "bug")
	fbID := e.insertFeedback(t, "crash on login")

	inserted, err := e.assignments.Add(e.ctx, e.tenantID, fbID, tag.ID, "user-1")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !inserted {
		t.Fatal("Add: expected inserted=true")
	}

	dup, err := e.assignments.Add(e.ctx, e.tenantID, fbID, tag.ID, "user-1")
	if err != nil {
		t.Fatalf("Add duplicate: %v", err)
	}
	if dup {
		t.Fatal("duplicate Add should return inserted=false")
	}

	tags, err := e.assignments.ListByFeedback(e.ctx, e.tenantID, fbID)
	if err != nil {
		t.Fatalf("ListByFeedback: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "bug" {
		t.Fatalf("unexpected tags: %+v", tags)
	}

	removed, err := e.assignments.Remove(e.ctx, e.tenantID, fbID, tag.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !removed {
		t.Fatal("Remove: expected removed=true")
	}

	removed2, err := e.assignments.Remove(e.ctx, e.tenantID, fbID, tag.ID)
	if err != nil {
		t.Fatalf("Remove again: %v", err)
	}
	if removed2 {
		t.Fatal("second Remove should return removed=false")
	}

	tags, err = e.assignments.ListByFeedback(e.ctx, e.tenantID, fbID)
	if err != nil {
		t.Fatalf("ListByFeedback after remove: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(tags))
	}
}

func TestPG_RemoveByScopeExcluding(t *testing.T) {
	e := setup(t)
	hi := e.createTag(t, "priority-high")
	lo := e.createTag(t, "priority-low")
	fbID := e.insertFeedback(t, "slow page")

	// update both tags to have exclusive scope "priority"
	_, err := e.tags.Update(e.ctx, feedbacktag.Tag{
		ID: hi.ID, TenantID: e.tenantID,
		Name: hi.Name, Color: hi.Color, ExclusiveScope: ptrext.Of("priority"),
	})
	if err != nil {
		t.Fatalf("update hi scope: %v", err)
	}
	_, err = e.tags.Update(e.ctx, feedbacktag.Tag{
		ID: lo.ID, TenantID: e.tenantID,
		Name: lo.Name, Color: lo.Color, ExclusiveScope: ptrext.Of("priority"),
	})
	if err != nil {
		t.Fatalf("update lo scope: %v", err)
	}

	if _, err := e.assignments.Add(e.ctx, e.tenantID, fbID, hi.ID, "u1"); err != nil {
		t.Fatalf("Add hi: %v", err)
	}
	if _, err := e.assignments.Add(e.ctx, e.tenantID, fbID, lo.ID, "u1"); err != nil {
		t.Fatalf("Add lo: %v", err)
	}

	// keep lo, remove others in "priority" scope
	removed, err := e.assignments.RemoveByScopeExcluding(e.ctx, e.tenantID, fbID, "priority", lo.ID)
	if err != nil {
		t.Fatalf("RemoveByScopeExcluding: %v", err)
	}
	if len(removed) != 1 || removed[0] != hi.ID {
		t.Fatalf("expected [%s] removed, got %v", hi.ID, removed)
	}

	tags, err := e.assignments.ListByFeedback(e.ctx, e.tenantID, fbID)
	if err != nil {
		t.Fatalf("ListByFeedback after scope cleanup: %v", err)
	}
	if len(tags) != 1 || tags[0].TagID != lo.ID {
		t.Fatalf("expected only lo tag remaining, got %+v", tags)
	}
}

func TestPG_ListByFeedbackBatch(t *testing.T) {
	e := setup(t)
	tag1 := e.createTag(t, "ux")
	tag2 := e.createTag(t, "perf")
	fb1 := e.insertFeedback(t, "feedback one")
	fb2 := e.insertFeedback(t, "feedback two")

	if _, err := e.assignments.Add(e.ctx, e.tenantID, fb1, tag1.ID, "u1"); err != nil {
		t.Fatalf("Add fb1/tag1: %v", err)
	}
	if _, err := e.assignments.Add(e.ctx, e.tenantID, fb2, tag1.ID, "u1"); err != nil {
		t.Fatalf("Add fb2/tag1: %v", err)
	}
	if _, err := e.assignments.Add(e.ctx, e.tenantID, fb2, tag2.ID, "u1"); err != nil {
		t.Fatalf("Add fb2/tag2: %v", err)
	}

	m, err := e.assignments.ListByFeedbackBatch(e.ctx, e.tenantID, []int64{fb1, fb2})
	if err != nil {
		t.Fatalf("ListByFeedbackBatch: %v", err)
	}
	if len(m[fb1]) != 1 {
		t.Fatalf("fb1 tags = %d want 1", len(m[fb1]))
	}
	if len(m[fb2]) != 2 {
		t.Fatalf("fb2 tags = %d want 2", len(m[fb2]))
	}
}
