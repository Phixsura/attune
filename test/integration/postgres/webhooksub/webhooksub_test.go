//go:build integration

package webhooksub_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/repo/webhooksub"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_SubscriptionInsertListDisable(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	repo := webhooksub.New(pool)

	sub, err := repo.Insert(ctx, webhooksub.Subscription{
		TenantID:   "t1",
		TargetURL:  "https://hooks.zapier.com/x",
		Secret:     "s3cr3t-16chars-min",
		EventTypes: []string{"feedback.created"},
		Consumer:   webhooksub.ConsumerZapier,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if sub.ID == uuid.Nil {
		t.Fatal("Insert: want non-nil id")
	}
	if sub.Status != webhooksub.StatusActive {
		t.Fatalf("Insert status: got %q want %q", sub.Status, webhooksub.StatusActive)
	}

	active, err := repo.ListActiveByTenantEvent(ctx, "t1", "feedback.created")
	if err != nil {
		t.Fatalf("ListActiveByTenantEvent: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active count: got %d want 1", len(active))
	}

	// event filter excludes non-subscribed types
	none, err := repo.ListActiveByTenantEvent(ctx, "t1", "request.created")
	if err != nil {
		t.Fatalf("ListActiveByTenantEvent(request.created): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("non-subscribed event should match nothing, got %d", len(none))
	}

	n, err := repo.CountByTenant(ctx, "t1")
	if err != nil {
		t.Fatalf("CountByTenant: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountByTenant: got %d want 1", n)
	}

	assertDisableAndDelete(t, ctx, repo, sub.ID)
}

func assertDisableAndDelete(t *testing.T, ctx context.Context, repo *webhooksub.Repo, id uuid.UUID) {
	t.Helper()
	if err := repo.Disable(ctx, id, webhooksub.ReasonGone); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	got, err := repo.GetByID(ctx, "t1", id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != webhooksub.StatusDisabled || got.DisabledReason != webhooksub.ReasonGone {
		t.Fatalf("after Disable: got status=%q reason=%q", got.Status, got.DisabledReason)
	}

	// disabled rows drop out of the active list
	active, err := repo.ListActiveByTenantEvent(ctx, "t1", "feedback.created")
	if err != nil {
		t.Fatalf("ListActiveByTenantEvent after disable: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("disabled sub should not be listed, got %d", len(active))
	}

	ok, err := repo.Delete(ctx, "t1", id)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !ok {
		t.Fatal("Delete: want true")
	}
	ok, err = repo.Delete(ctx, "t1", id)
	if err != nil {
		t.Fatalf("Delete twice: %v", err)
	}
	if ok {
		t.Fatal("second Delete: want false")
	}
}

func TestPG_SubscriptionTenantIsolationAndTx(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	repo := webhooksub.New(pool)

	sub, err := repo.Insert(ctx, webhooksub.Subscription{
		TenantID:   "t1",
		TargetURL:  "https://hooks.zapier.com/y",
		Secret:     "s3cr3t-16chars-min",
		EventTypes: []string{"request.created", "request.status_changed"},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if sub.Consumer != webhooksub.ConsumerGeneric {
		t.Fatalf("default consumer: got %q want %q", sub.Consumer, webhooksub.ConsumerGeneric)
	}

	// wrong tenant → not found
	if _, err := repo.GetByID(ctx, "t2", sub.ID); !errors.Is(err, webhooksub.ErrSubscriptionNotFound) {
		t.Fatalf("cross-tenant GetByID: got %v want ErrSubscriptionNotFound", err)
	}
	ok, err := repo.Delete(ctx, "t2", sub.ID)
	if err != nil {
		t.Fatalf("cross-tenant Delete: %v", err)
	}
	if ok {
		t.Fatal("cross-tenant Delete must not remove the row")
	}

	// GetByIDAny is the worker's trusted, tenant-free lookup
	any, err := repo.GetByIDAny(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetByIDAny: %v", err)
	}
	if any.TenantID != "t1" {
		t.Fatalf("GetByIDAny tenant: got %q want t1", any.TenantID)
	}

	// Tx variant sees rows inside the same tx
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	subs, err := repo.ListActiveByTenantEventTx(ctx, tx, "t1", "request.status_changed")
	if err != nil {
		t.Fatalf("ListActiveByTenantEventTx: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("tx list: got %d want 1", len(subs))
	}
}
