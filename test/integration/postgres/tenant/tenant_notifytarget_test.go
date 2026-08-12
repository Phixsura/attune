//go:build integration

package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_TenantAndNotifyTargetCRUD(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenants := tenant.NewTenant(pool)
	tenantID := createTenant(t, ctx, tenants, "tenant-crud", "Tenant CRUD")
	createTenant(t, ctx, tenants, "aaa-first", "First")
	assertTenantLookups(t, ctx, tenants, tenantID)

	targets := notifytarget.NewNotifyTarget(pool)
	targetID := insertNotifyTarget(t, ctx, targets, notifytarget.NotifyTarget{
		TenantID:        tenantID,
		DestinationType: notifytarget.DestRawWebhook,
		Audience:        notifytarget.AudiencePool,
		URL:             "http://127.0.0.1:18080/hook",
		Secret:          "0123456789abcdef",
		TimeoutSeconds:  3,
	})
	assertNotifyConflict(t, ctx, targets, tenantID, targetID)
	assertNotifyUpdateAndAlert(t, ctx, targets, tenantID, targetID)
}

func createTenant(t *testing.T, ctx context.Context, repo *tenant.TenantRepo, slug, name string) string {
	t.Helper()
	id, err := repo.Create(ctx, slug, name)
	if err != nil {
		t.Fatalf("Create tenant %s: %v", slug, err)
	}
	return id
}

func assertTenantLookups(t *testing.T, ctx context.Context, repo *tenant.TenantRepo, tenantID string) {
	t.Helper()
	if _, err := repo.Create(ctx, "tenant-crud", "Duplicate"); !errors.Is(err, tenant.ErrTenantSlugTaken) {
		t.Fatalf("duplicate tenant: got %v want %v", err, tenant.ErrTenantSlugTaken)
	}
	resolved, err := repo.ResolveSlug(ctx, "tenant-crud")
	if err != nil {
		t.Fatalf("ResolveSlug: %v", err)
	}
	if resolved != tenantID {
		t.Fatalf("ResolveSlug id: got %q want %q", resolved, tenantID)
	}
	first, err := repo.FirstActiveID(ctx)
	if err != nil {
		t.Fatalf("FirstActiveID: %v", err)
	}
	if first == tenantID {
		t.Fatalf("FirstActiveID should prefer lexicographically first slug, got tenant-crud id")
	}
	tz, err := repo.GetTimezone(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetTimezone: %v", err)
	}
	if tz == "" {
		t.Fatal("GetTimezone must return the tenant's zone")
	}
	got, err := repo.GetByID(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Slug != "tenant-crud" || got.Name != "Tenant CRUD" || !got.IsActive {
		t.Fatalf("tenant row mismatch: %#v", got)
	}
}

func insertNotifyTarget(
	t *testing.T, ctx context.Context, repo *notifytarget.NotifyTargetRepo, row notifytarget.NotifyTarget,
) string {
	t.Helper()
	id, _, err := repo.Insert(ctx, row)
	if err != nil {
		t.Fatalf("Insert notify target: %v", err)
	}
	return id.String()
}

func assertNotifyConflict(
	t *testing.T, ctx context.Context, repo *notifytarget.NotifyTargetRepo, tenantID, targetID string,
) {
	t.Helper()
	dup := notifytarget.NotifyTarget{
		TenantID:        tenantID,
		DestinationType: notifytarget.DestRawWebhook,
		Audience:        notifytarget.AudiencePool,
		URL:             "http://127.0.0.1:18082/hook",
		Secret:          "duplicate-secret",
		TimeoutSeconds:  4,
	}
	if _, _, err := repo.Insert(ctx, dup); !errors.Is(err, notifytarget.ErrNotifyTargetConflict) {
		t.Fatalf("duplicate notify target: got %v want %v", err, notifytarget.ErrNotifyTargetConflict)
	}
	radar := dup
	radar.Audience = notifytarget.AudienceRadar
	if _, _, err := repo.Insert(ctx, radar); err != nil {
		t.Fatalf("insert radar notify target: %v", err)
	}
	target, err := repo.GetByID(ctx, tenantID, mustUUID(t, targetID))
	if err != nil {
		t.Fatalf("GetByID notify target: %v", err)
	}
	target.Audience = notifytarget.AudienceRadar
	if err := repo.UpdateByID(ctx, tenantID, target.ID, ptrext.Indirect(target)); !errors.Is(err, notifytarget.ErrNotifyTargetConflict) {
		t.Fatalf("conflicting update: got %v want %v", err, notifytarget.ErrNotifyTargetConflict)
	}
}

func assertNotifyUpdateAndAlert(
	t *testing.T, ctx context.Context, repo *notifytarget.NotifyTargetRepo, tenantID, targetID string,
) {
	t.Helper()
	id := mustUUID(t, targetID)
	updated := notifytarget.NotifyTarget{
		Audience:       notifytarget.AudienceAll,
		URL:            "http://127.0.0.1:18081/hook",
		Secret:         "fedcba9876543210",
		TimeoutSeconds: 7,
	}
	if err := repo.UpdateByID(ctx, tenantID, id, updated); err != nil {
		t.Fatalf("UpdateByID: %v", err)
	}
	got, err := repo.GetByTenantAudience(ctx, tenantID, notifytarget.DestRawWebhook, notifytarget.AudienceAll)
	if err != nil {
		t.Fatalf("GetByTenantAudience: %v", err)
	}
	if got.URL != updated.URL || got.Secret != updated.Secret || got.TimeoutSeconds != updated.TimeoutSeconds {
		t.Fatalf("updated target mismatch: %#v", got)
	}
	assertNotifyFailureLifecycle(t, ctx, repo, tenantID, got)
	if err := repo.Delete(ctx, tenantID, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, tenantID, id); !errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
		t.Fatalf("GetByID deleted: got %v want %v", err, notifytarget.ErrNotifyTargetNotFound)
	}
}

func assertNotifyFailureLifecycle(
	t *testing.T, ctx context.Context, repo *notifytarget.NotifyTargetRepo, tenantID string, row *notifytarget.NotifyTarget,
) {
	t.Helper()
	if err := repo.TouchFailure(ctx, tenantID, row.DestinationType, row.URL, row.Audience, "boom"); err != nil {
		t.Fatalf("TouchFailure: %v", err)
	}
	failed, _ := repo.GetByID(ctx, tenantID, row.ID)
	if failed.LastFailureAt == nil || failed.LastError != "boom" {
		t.Fatalf("failure state not stored: %#v", failed)
	}
	if err := repo.ClearFailure(ctx, tenantID, row.DestinationType, row.URL, row.Audience); err != nil {
		t.Fatalf("ClearFailure: %v", err)
	}
	cleared, _ := repo.GetByID(ctx, tenantID, row.ID)
	if cleared.LastFailureAt != nil || cleared.LastError != "" {
		t.Fatalf("failure state not cleared: %#v", cleared)
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
