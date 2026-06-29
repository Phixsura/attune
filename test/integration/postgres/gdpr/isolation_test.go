//go:build integration

package gdpr_test

import (
	"context"
	"errors"
	"testing"

	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_Export_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "gdpr-iso-a", "GDPR Iso A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "gdpr-iso-b", "GDPR Iso B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	subjectKey := "shared-user@example.com"

	// Seed feedback for B only.
	insertFeedback(t, ctx, pool, tidB,
		"ext_api:"+subjectKey, subjectKey, subjectKey, "hash-shared",
		"B's private feedback")

	repo := gdprrepo.New(pool)

	// Export from B should find the data.
	dataB, err := repo.Export(ctx, tidB, subjectKey)
	if err != nil {
		t.Fatalf("Export(tenantB): %v", err)
	}
	if dataB.Counts.FeedbackCount != 1 {
		t.Fatalf("expected 1 feedback row for B, got %d", dataB.Counts.FeedbackCount)
	}

	// Export from A for the same subject key must return ErrSubjectNotFound.
	_, err = repo.Export(ctx, tidA, subjectKey)
	if err == nil {
		t.Error("ISOLATION BREACH: Export(tenantA) succeeded for subject belonging to tenant B")
	} else if !errors.Is(err, gdprrepo.ErrSubjectNotFound) {
		t.Errorf("expected ErrSubjectNotFound, got: %v", err)
	}
}
