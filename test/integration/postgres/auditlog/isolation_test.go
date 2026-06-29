//go:build integration

package auditlog_test

import (
	"context"
	"testing"
	"time"

	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
)

func TestPG_ListAuditLog_TenantIsolation(t *testing.T) {
	pool := sharedPool
	ctx := context.Background()

	tidA := insertTenant(t, ctx, pool, "audit-iso-a")
	tidB := insertTenant(t, ctx, pool, "audit-iso-b")

	base := time.Now().UTC()
	actions := []string{"member.invite", "api_key.create", "member.invite"}
	for i, action := range actions {
		insertAuditRow(t, ctx, pool, auditRow{
			tenantID:   tidA,
			actorType:  "admin",
			actorID:    "user-a",
			action:     action,
			targetType: "member",
			targetID:   "target-a-" + string(rune('0'+i)),
			createdAt:  base.Add(time.Duration(i) * time.Second),
		})
	}
	for i, action := range actions {
		insertAuditRow(t, ctx, pool, auditRow{
			tenantID:   tidB,
			actorType:  "admin",
			actorID:    "user-b",
			action:     action,
			targetType: "member",
			targetID:   "target-b-" + string(rune('0'+i)),
			createdAt:  base.Add(time.Duration(i) * time.Second),
		})
	}

	repo := auditlogrepo.New(pool)

	result, err := repo.List(ctx, auditlogrepo.ListFilter{
		TenantID:  tidA,
		Unbounded: true,
	})
	if err != nil {
		t.Fatalf("List(tenantA): %v", err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items for tenant A, got %d", len(result.Items))
	}
	for _, item := range result.Items {
		if item.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: List(tenantA) returned entry %d belonging to tenant %q", item.ID, item.TenantID)
		}
	}

	resultB, err := repo.List(ctx, auditlogrepo.ListFilter{
		TenantID:  tidB,
		Unbounded: true,
	})
	if err != nil {
		t.Fatalf("List(tenantB): %v", err)
	}
	if len(resultB.Items) != 3 {
		t.Fatalf("expected 3 items for tenant B, got %d", len(resultB.Items))
	}
	for _, item := range resultB.Items {
		if item.TenantID != tidB {
			t.Errorf("ISOLATION BREACH: List(tenantB) returned entry %d belonging to tenant %q", item.ID, item.TenantID)
		}
	}
}
