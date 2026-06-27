//go:build integration

package breakglass_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/repo/breakglass"
	"github.com/Phixsura/attune/internal/testdb"
)

func insertTenant(ctx context.Context, t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	tenantID := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name)
		VALUES ($1, $2, $3)
	`, tenantID, slug, "Break Glass Test")
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func TestPG_IssueAndGetByID(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-issue-get")

	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "hash-abc123",
		ExpiresAt:  expiresAt,
		IssuedBy:   "super-admin@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token.ID == "" {
		t.Fatal("token ID should be set")
	}
	if token.TenantID != tenantID {
		t.Errorf("tenant_id: got %q, want %q", token.TenantID, tenantID)
	}
	if token.AdminEmail != "admin@example.com" {
		t.Errorf("admin_email: got %q, want %q", token.AdminEmail, "admin@example.com")
	}
	if token.TokenHash != "hash-abc123" {
		t.Errorf("token_hash: got %q, want %q", token.TokenHash, "hash-abc123")
	}
	if token.UsedAt != nil {
		t.Error("used_at should be nil on fresh token")
	}
	if token.RevokedAt != nil {
		t.Error("revoked_at should be nil on fresh token")
	}
	if token.RevokedBy != nil {
		t.Error("revoked_by should be nil on fresh token")
	}
	if token.IssuedBy != "super-admin@example.com" {
		t.Errorf("issued_by: got %q, want %q", token.IssuedBy, "super-admin@example.com")
	}
	if token.IssuedAt.IsZero() {
		t.Error("issued_at should be set")
	}

	// GetByID should retrieve the same token
	got, err := repo.GetByID(ctx, tenantID, token.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != token.ID {
		t.Errorf("GetByID id: got %q, want %q", got.ID, token.ID)
	}
	if got.AdminEmail != token.AdminEmail {
		t.Errorf("GetByID admin_email: got %q, want %q", got.AdminEmail, token.AdminEmail)
	}
}

func TestPG_GetByID_NotFound(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-not-found")

	_, err := repo.GetByID(ctx, tenantID, uuid.NewString())
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("GetByID non-existent: got %v, want %v", err, breakglass.ErrNotFound)
	}
}

func TestPG_GetByID_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenant1 := insertTenant(ctx, t, pool, "bg-tenant-1")
	tenant2 := insertTenant(ctx, t, pool, "bg-tenant-2")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenant1,
		AdminEmail: "admin@tenant1.com",
		TokenHash:  "hash-tenant1",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@tenant1.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Should find with correct tenant
	_, err = repo.GetByID(ctx, tenant1, token.ID)
	if err != nil {
		t.Fatalf("GetByID correct tenant: %v", err)
	}

	// Should NOT find with wrong tenant
	_, err = repo.GetByID(ctx, tenant2, token.ID)
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("GetByID wrong tenant: got %v, want %v", err, breakglass.ErrNotFound)
	}
}

func TestPG_ListValidForAdmin(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-list-valid")
	adminEmail := "admin@example.com"

	// Issue a valid token
	valid1, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: adminEmail,
		TokenHash:  "valid-1",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue valid1: %v", err)
	}

	// Issue another valid token (expires later)
	valid2, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: adminEmail,
		TokenHash:  "valid-2",
		ExpiresAt:  time.Now().UTC().Add(2 * time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue valid2: %v", err)
	}

	// Issue a token for different admin
	_, err = repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "other@example.com",
		TokenHash:  "other-admin",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue other admin: %v", err)
	}

	// Issue an expired token
	_, err = repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: adminEmail,
		TokenHash:  "expired",
		ExpiresAt:  time.Now().UTC().Add(-time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue expired: %v", err)
	}

	// List valid tokens for admin
	tokens, err := repo.ListValidForAdmin(ctx, tenantID, adminEmail)
	if err != nil {
		t.Fatalf("ListValidForAdmin: %v", err)
	}

	if len(tokens) != 2 {
		t.Fatalf("expected 2 valid tokens, got %d", len(tokens))
	}

	// Should be ordered by expires_at ASC (valid1 expires first)
	if tokens[0].ID != valid1.ID {
		t.Errorf("first token should be valid1 (expires sooner), got %s", tokens[0].ID)
	}
	if tokens[1].ID != valid2.ID {
		t.Errorf("second token should be valid2 (expires later), got %s", tokens[1].ID)
	}
}

func TestPG_ListValidForAdmin_ExcludesUsedAndRevoked(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-list-exclude")
	adminEmail := "admin@example.com"

	// Issue a valid token
	valid, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: adminEmail,
		TokenHash:  "valid",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue valid: %v", err)
	}

	// Issue and mark as used
	used, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: adminEmail,
		TokenHash:  "used",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue used: %v", err)
	}
	if err := repo.MarkUsed(ctx, tenantID, used.ID); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}

	// Issue and revoke
	revoked, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: adminEmail,
		TokenHash:  "revoked",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue revoked: %v", err)
	}
	if err := repo.Revoke(ctx, tenantID, revoked.ID, "revoker@example.com"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// List should only include the valid token
	tokens, err := repo.ListValidForAdmin(ctx, tenantID, adminEmail)
	if err != nil {
		t.Fatalf("ListValidForAdmin: %v", err)
	}

	if len(tokens) != 1 {
		t.Fatalf("expected 1 valid token, got %d", len(tokens))
	}
	if tokens[0].ID != valid.ID {
		t.Errorf("expected valid token, got %s", tokens[0].ID)
	}
}

func TestPG_ListAll(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-list-all")

	// Issue several tokens
	for i := 0; i < 5; i++ {
		_, err := repo.Issue(ctx, breakglass.NewToken{
			TenantID:   tenantID,
			AdminEmail: "admin@example.com",
			TokenHash:  uuid.NewString(),
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			IssuedBy:   "issuer@example.com",
		})
		if err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
		// Small delay to ensure ordering
		time.Sleep(5 * time.Millisecond)
	}

	// ListAll should return all tokens ordered by issued_at DESC
	tokens, err := repo.ListAll(ctx, tenantID, 100)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(tokens) != 5 {
		t.Fatalf("expected 5 tokens, got %d", len(tokens))
	}

	// Verify DESC ordering
	for i := 1; i < len(tokens); i++ {
		if tokens[i].IssuedAt.After(tokens[i-1].IssuedAt) {
			t.Errorf("tokens not ordered by issued_at DESC: [%d]=%v > [%d]=%v",
				i, tokens[i].IssuedAt, i-1, tokens[i-1].IssuedAt)
		}
	}
}

func TestPG_ListAll_Limit(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-list-limit")

	for i := 0; i < 10; i++ {
		_, err := repo.Issue(ctx, breakglass.NewToken{
			TenantID:   tenantID,
			AdminEmail: "admin@example.com",
			TokenHash:  uuid.NewString(),
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			IssuedBy:   "issuer@example.com",
		})
		if err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
	}

	tokens, err := repo.ListAll(ctx, tenantID, 3)
	if err != nil {
		t.Fatalf("ListAll limit=3: %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens with limit=3, got %d", len(tokens))
	}
}

func TestPG_ListAll_DefaultLimit(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-list-default")

	// With limit <= 0, should default to 100
	tokens, err := repo.ListAll(ctx, tenantID, 0)
	if err != nil {
		t.Fatalf("ListAll limit=0: %v", err)
	}
	// No tokens yet, but the query should succeed (nil slice is acceptable)
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for fresh tenant, got %d", len(tokens))
	}
}

func TestPG_MarkUsed(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-mark-used")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "mark-used-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := repo.MarkUsed(ctx, tenantID, token.ID); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}

	// Verify token is now used
	got, err := repo.GetByID(ctx, tenantID, token.ID)
	if err != nil {
		t.Fatalf("GetByID after MarkUsed: %v", err)
	}
	if got.UsedAt == nil {
		t.Error("used_at should be set after MarkUsed")
	}
}

func TestPG_MarkUsed_AlreadyUsed(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-already-used")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "already-used-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := repo.MarkUsed(ctx, tenantID, token.ID); err != nil {
		t.Fatalf("MarkUsed first: %v", err)
	}

	err = repo.MarkUsed(ctx, tenantID, token.ID)
	if !errors.Is(err, breakglass.ErrAlreadyUsed) {
		t.Errorf("MarkUsed second: got %v, want %v", err, breakglass.ErrAlreadyUsed)
	}
}

func TestPG_MarkUsed_NotFound(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-mark-not-found")

	err := repo.MarkUsed(ctx, tenantID, uuid.NewString())
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("MarkUsed non-existent: got %v, want %v", err, breakglass.ErrNotFound)
	}
}

func TestPG_MarkUsed_Revoked(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-mark-revoked")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "mark-revoked-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := repo.Revoke(ctx, tenantID, token.ID, "revoker@example.com"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	err = repo.MarkUsed(ctx, tenantID, token.ID)
	if !errors.Is(err, breakglass.ErrAlreadyRevoked) {
		t.Errorf("MarkUsed revoked token: got %v, want %v", err, breakglass.ErrAlreadyRevoked)
	}
}

func TestPG_MarkUsed_Expired(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-mark-expired")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "mark-expired-hash",
		ExpiresAt:  time.Now().UTC().Add(-time.Hour), // Already expired
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	err = repo.MarkUsed(ctx, tenantID, token.ID)
	if !errors.Is(err, breakglass.ErrExpired) {
		t.Errorf("MarkUsed expired token: got %v, want %v", err, breakglass.ErrExpired)
	}
}

func TestPG_MarkUsed_ConcurrentRace(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-concurrent")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "concurrent-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	const concurrency = 10
	var wg sync.WaitGroup
	results := make(chan error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			results <- repo.MarkUsed(ctx, tenantID, token.ID)
		}()
	}
	wg.Wait()
	close(results)

	var successes, alreadyUsed int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, breakglass.ErrAlreadyUsed) {
			alreadyUsed++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
	if alreadyUsed != concurrency-1 {
		t.Errorf("expected %d already-used errors, got %d", concurrency-1, alreadyUsed)
	}
}

func TestPG_MarkUsed_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenant1 := insertTenant(ctx, t, pool, "bg-mark-tenant-1")
	tenant2 := insertTenant(ctx, t, pool, "bg-mark-tenant-2")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenant1,
		AdminEmail: "admin@example.com",
		TokenHash:  "tenant-iso-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// MarkUsed with wrong tenant should return NotFound
	err = repo.MarkUsed(ctx, tenant2, token.ID)
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("MarkUsed wrong tenant: got %v, want %v", err, breakglass.ErrNotFound)
	}

	// Token should still be unused
	got, err := repo.GetByID(ctx, tenant1, token.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.UsedAt != nil {
		t.Error("token should still be unused after wrong-tenant MarkUsed")
	}
}

func TestPG_Revoke(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-revoke")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "revoke-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	revokedBy := "security@example.com"
	if err := repo.Revoke(ctx, tenantID, token.ID, revokedBy); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	got, err := repo.GetByID(ctx, tenantID, token.ID)
	if err != nil {
		t.Fatalf("GetByID after Revoke: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("revoked_at should be set after Revoke")
	}
	if got.RevokedBy == nil || *got.RevokedBy != revokedBy {
		t.Errorf("revoked_by: got %v, want %q", got.RevokedBy, revokedBy)
	}
}

func TestPG_Revoke_AlreadyRevoked(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-revoke-twice")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "revoke-twice-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := repo.Revoke(ctx, tenantID, token.ID, "first@example.com"); err != nil {
		t.Fatalf("Revoke first: %v", err)
	}

	err = repo.Revoke(ctx, tenantID, token.ID, "second@example.com")
	if !errors.Is(err, breakglass.ErrAlreadyRevoked) {
		t.Errorf("Revoke second: got %v, want %v", err, breakglass.ErrAlreadyRevoked)
	}
}

func TestPG_Revoke_AlreadyUsed(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-revoke-used")

	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "revoke-used-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := repo.MarkUsed(ctx, tenantID, token.ID); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}

	err = repo.Revoke(ctx, tenantID, token.ID, "revoker@example.com")
	if !errors.Is(err, breakglass.ErrAlreadyUsed) {
		t.Errorf("Revoke used token: got %v, want %v", err, breakglass.ErrAlreadyUsed)
	}
}

func TestPG_Revoke_NotFound(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-revoke-not-found")

	err := repo.Revoke(ctx, tenantID, uuid.NewString(), "revoker@example.com")
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("Revoke non-existent: got %v, want %v", err, breakglass.ErrNotFound)
	}
}

func TestPG_CountValid(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-count-valid")

	// Initially zero
	count, err := repo.CountValid(ctx, tenantID)
	if err != nil {
		t.Fatalf("CountValid initial: %v", err)
	}
	if count != 0 {
		t.Errorf("initial count: got %d, want 0", count)
	}

	// Issue 3 valid tokens
	for i := 0; i < 3; i++ {
		_, err := repo.Issue(ctx, breakglass.NewToken{
			TenantID:   tenantID,
			AdminEmail: "admin@example.com",
			TokenHash:  uuid.NewString(),
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			IssuedBy:   "issuer@example.com",
		})
		if err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
	}

	count, err = repo.CountValid(ctx, tenantID)
	if err != nil {
		t.Fatalf("CountValid after issue: %v", err)
	}
	if count != 3 {
		t.Errorf("count after 3 issues: got %d, want 3", count)
	}

	// Issue an expired token - should not count
	_, err = repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "expired-count",
		ExpiresAt:  time.Now().UTC().Add(-time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue expired: %v", err)
	}

	count, err = repo.CountValid(ctx, tenantID)
	if err != nil {
		t.Fatalf("CountValid after expired: %v", err)
	}
	if count != 3 {
		t.Errorf("count should still be 3 after expired token: got %d", count)
	}
}

func TestPG_CountValid_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenant1 := insertTenant(ctx, t, pool, "bg-count-tenant-1")
	tenant2 := insertTenant(ctx, t, pool, "bg-count-tenant-2")

	// Issue 2 tokens for tenant1
	for i := 0; i < 2; i++ {
		_, err := repo.Issue(ctx, breakglass.NewToken{
			TenantID:   tenant1,
			AdminEmail: "admin@example.com",
			TokenHash:  uuid.NewString(),
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			IssuedBy:   "issuer@example.com",
		})
		if err != nil {
			t.Fatalf("Issue tenant1 %d: %v", i, err)
		}
	}

	// Issue 1 token for tenant2
	_, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenant2,
		AdminEmail: "admin@example.com",
		TokenHash:  uuid.NewString(),
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue tenant2: %v", err)
	}

	count1, err := repo.CountValid(ctx, tenant1)
	if err != nil {
		t.Fatalf("CountValid tenant1: %v", err)
	}
	if count1 != 2 {
		t.Errorf("tenant1 count: got %d, want 2", count1)
	}

	count2, err := repo.CountValid(ctx, tenant2)
	if err != nil {
		t.Fatalf("CountValid tenant2: %v", err)
	}
	if count2 != 1 {
		t.Errorf("tenant2 count: got %d, want 1", count2)
	}
}

func TestPG_Cleanup(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-cleanup")

	// Issue an old expired token (should be cleaned up)
	old, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "old-expired",
		ExpiresAt:  time.Now().UTC().Add(-48 * time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue old: %v", err)
	}

	// Issue a recently expired token (should NOT be cleaned up if cutoff is 24h ago)
	recent, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "recent-expired",
		ExpiresAt:  time.Now().UTC().Add(-1 * time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue recent: %v", err)
	}

	// Issue a valid token (should NOT be cleaned up)
	valid, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "still-valid",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue valid: %v", err)
	}

	// Cleanup tokens expired before 24 hours ago
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	deleted, err := repo.Cleanup(ctx, cutoff)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Verify old token is gone
	_, err = repo.GetByID(ctx, tenantID, old.ID)
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("old token should be deleted: got %v", err)
	}

	// Verify recent and valid tokens still exist
	_, err = repo.GetByID(ctx, tenantID, recent.ID)
	if err != nil {
		t.Errorf("recent token should still exist: %v", err)
	}
	_, err = repo.GetByID(ctx, tenantID, valid.ID)
	if err != nil {
		t.Errorf("valid token should still exist: %v", err)
	}
}

func TestPG_Cleanup_AllExpired(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-cleanup-all")

	// Issue several old expired tokens
	for i := 0; i < 5; i++ {
		_, err := repo.Issue(ctx, breakglass.NewToken{
			TenantID:   tenantID,
			AdminEmail: "admin@example.com",
			TokenHash:  uuid.NewString(),
			ExpiresAt:  time.Now().UTC().Add(-time.Duration(i+2) * 24 * time.Hour),
			IssuedBy:   "issuer@example.com",
		})
		if err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
	}

	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	deleted, err := repo.Cleanup(ctx, cutoff)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 5 {
		t.Errorf("expected 5 deleted, got %d", deleted)
	}

	// All tokens for this tenant should be gone
	tokens, err := repo.ListAll(ctx, tenantID, 100)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens after cleanup, got %d", len(tokens))
	}
}

func TestPG_Cleanup_NoOp(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-cleanup-noop")

	// Issue only valid tokens
	for i := 0; i < 3; i++ {
		_, err := repo.Issue(ctx, breakglass.NewToken{
			TenantID:   tenantID,
			AdminEmail: "admin@example.com",
			TokenHash:  uuid.NewString(),
			ExpiresAt:  time.Now().UTC().Add(time.Duration(i+1) * time.Hour),
			IssuedBy:   "issuer@example.com",
		})
		if err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
	}

	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	deleted, err := repo.Cleanup(ctx, cutoff)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted when no expired tokens, got %d", deleted)
	}
}

func TestPG_NULLHandling(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := breakglass.NewRepo(pool)
	ctx := context.Background()

	tenantID := insertTenant(ctx, t, pool, "bg-null-handling")

	// Fresh token should have NULLs for optional fields
	token, err := repo.Issue(ctx, breakglass.NewToken{
		TenantID:   tenantID,
		AdminEmail: "admin@example.com",
		TokenHash:  "null-test-hash",
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	got, err := repo.GetByID(ctx, tenantID, token.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	// Verify NULL fields are properly represented as nil
	if got.UsedAt != nil {
		t.Errorf("UsedAt should be nil, got %v", got.UsedAt)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt should be nil, got %v", got.RevokedAt)
	}
	if got.RevokedBy != nil {
		t.Errorf("RevokedBy should be nil, got %v", got.RevokedBy)
	}

	// After revoke, RevokedAt and RevokedBy should be set
	if err := repo.Revoke(ctx, tenantID, token.ID, "revoker@example.com"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	got, err = repo.GetByID(ctx, tenantID, token.ID)
	if err != nil {
		t.Fatalf("GetByID after revoke: %v", err)
	}

	if got.RevokedAt == nil {
		t.Error("RevokedAt should be set after revoke")
	}
	if got.RevokedBy == nil {
		t.Error("RevokedBy should be set after revoke")
	} else if *got.RevokedBy != "revoker@example.com" {
		t.Errorf("RevokedBy: got %q, want %q", *got.RevokedBy, "revoker@example.com")
	}
	// UsedAt should still be nil
	if got.UsedAt != nil {
		t.Errorf("UsedAt should still be nil after revoke, got %v", got.UsedAt)
	}
}
