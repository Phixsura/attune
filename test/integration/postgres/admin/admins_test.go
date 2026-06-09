//go:build integration

package admin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_AdminRepoCreateLookupLockoutAndReset(t *testing.T) {
	repo := admin.NewRepo(testdb.NewPool(t))
	ctx := context.Background()

	row, err := repo.Create(ctx, admin.NewAdmin{
		Email:        "Owner@Example.com",
		PasswordHash: "hash-v1",
		DisplayName:  "Owner",
		Role:         "",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.Role != "admin" {
		t.Fatalf("default role: got %q want admin", row.Role)
	}
	assertAdminLookup(t, ctx, repo, row)
	assertAdminLockoutResetAndPasswordUpdate(t, ctx, repo, row.ID)
}

func TestPG_AdminRepoBootstrapOnlyOnce(t *testing.T) {
	repo := admin.NewRepo(testdb.NewPool(t))
	ctx := context.Background()
	first := admin.NewAdmin{Email: "first@example.com", PasswordHash: "hash", DisplayName: "First"}

	if err := repo.Bootstrap(ctx, first); err != nil {
		t.Fatalf("first Bootstrap: %v", err)
	}
	if err := repo.Bootstrap(ctx, first); !errors.Is(err, admin.ErrAlreadyBootstrapped) {
		t.Fatalf("second Bootstrap: got %v want %v", err, admin.ErrAlreadyBootstrapped)
	}
	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("admin count: got %d want 1", count)
	}
}

func assertAdminLookup(t *testing.T, ctx context.Context, repo *admin.Repo, row admin.Admin) {
	t.Helper()
	found, err := repo.GetByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("GetByEmail lower-case: %v", err)
	}
	if found.ID != row.ID || found.PasswordHash != "hash-v1" {
		t.Fatalf("lookup mismatch: %#v", found)
	}
}

func assertAdminLockoutResetAndPasswordUpdate(t *testing.T, ctx context.Context, repo *admin.Repo, id string) {
	t.Helper()
	for i := 0; i < 5; i++ {
		if err := repo.IncrementFailedAttempts(ctx, id); err != nil {
			t.Fatalf("IncrementFailedAttempts %d: %v", i+1, err)
		}
	}
	locked, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID locked: %v", err)
	}
	if locked.FailedAttempts != 5 || locked.LockedUntil == nil {
		t.Fatalf("lockout state: attempts=%d locked_until=%v", locked.FailedAttempts, locked.LockedUntil)
	}

	if err := repo.ResetFailedAttempts(ctx, id); err != nil {
		t.Fatalf("ResetFailedAttempts: %v", err)
	}
	if err := repo.UpdatePasswordHash(ctx, id, "hash-v2"); err != nil {
		t.Fatalf("UpdatePasswordHash: %v", err)
	}
	reset, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID reset: %v", err)
	}
	if reset.FailedAttempts != 0 || reset.LockedUntil != nil || reset.PasswordHash != "hash-v2" {
		t.Fatalf("reset/update state: %#v", reset)
	}
}
