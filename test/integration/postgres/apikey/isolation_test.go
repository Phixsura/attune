//go:build integration

package apikey_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_LookupByHash_TenantLeak(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "ak-leak-a", "AK Leak A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "ak-leak-b", "AK Leak B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := apikey.NewAPIKey(pool)

	hashB := sha256.Sum256([]byte("secret-key-for-b"))
	keyIDB, err := repo.Insert(ctx, tidB, hashB[:], "att_b_", "B's key")
	if err != nil {
		t.Fatalf("Insert key for B: %v", err)
	}

	// LookupByHash is tenant-agnostic — it returns whichever tenant owns the
	// hash. Verify it returns B's tenant, not A's.
	row, err := repo.LookupByHash(ctx, hashB[:])
	if err != nil {
		t.Fatalf("LookupByHash: %v", err)
	}
	if row.TenantID != tidB {
		t.Errorf("ISOLATION BREACH: LookupByHash returned tenant %q, want %q", row.TenantID, tidB)
	}

	// GetByID with A's tenant must fail — key belongs to B.
	_, err = repo.GetByID(ctx, tidA, keyIDB)
	if err == nil {
		t.Error("ISOLATION BREACH: GetByID succeeded with wrong tenant")
	} else if !errors.Is(err, apikey.ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound, got: %v", err)
	}

	// Revoke with A's tenant must fail.
	err = repo.Revoke(ctx, tidA, keyIDB)
	if err == nil {
		t.Error("ISOLATION BREACH: Revoke succeeded with wrong tenant")
	} else if !errors.Is(err, apikey.ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound from cross-tenant Revoke, got: %v", err)
	}

	// Key must still be active for B.
	rowAfter, err := repo.GetByID(ctx, tidB, keyIDB)
	if err != nil {
		t.Fatalf("GetByID for B after cross-tenant revoke attempt: %v", err)
	}
	if !rowAfter.IsActive {
		t.Error("ISOLATION BREACH: key was revoked despite wrong tenant ID")
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

	repo := apikey.NewAPIKey(pool)

	hashA := sha256.Sum256([]byte("key-a"))
	if _, err := repo.Insert(ctx, tidA, hashA[:], "att_a_", "A's key"); err != nil {
		t.Fatalf("Insert key for A: %v", err)
	}

	hashB := sha256.Sum256([]byte("key-b"))
	if _, err := repo.Insert(ctx, tidB, hashB[:], "att_b_", "B's key"); err != nil {
		t.Fatalf("Insert key for B: %v", err)
	}

	rows, err := repo.ListByTenant(ctx, tidA)
	if err != nil {
		t.Fatalf("ListByTenant(A): %v", err)
	}
	for _, r := range rows {
		// APIKeyListRow does not expose TenantID directly, so verify via
		// GetByID that each returned key belongs to A.
		if _, err := repo.GetByID(ctx, tidA, r.ID); err != nil {
			t.Errorf("ISOLATION BREACH: ListByTenant(A) returned key %s that does not belong to A: %v", r.ID, err)
		}
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 key for A, got %d", len(rows))
	}

	rowsB, err := repo.ListByTenant(ctx, tidB)
	if err != nil {
		t.Fatalf("ListByTenant(B): %v", err)
	}
	if len(rowsB) != 1 {
		t.Errorf("expected 1 key for B, got %d", len(rowsB))
	}
}
