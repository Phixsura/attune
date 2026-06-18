//go:build integration

package apikey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_APIKeyScopes_InsertWithScopesAndGet(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "scope-test", "Scope Test Org")
	require.NoError(t, err)

	repo := apikey.NewAPIKey(pool)

	hash := []byte("testhash0123456789abcdef")
	scopes := []domain.Scope{domain.ScopeFeedbackRead, domain.ScopeIngestWrite}
	keyID, err := repo.InsertWithScopes(ctx, tenantID, hash, "fbk_live_tes", "test key", scopes)
	require.NoError(t, err)

	got, err := repo.GetScopes(ctx, keyID)
	require.NoError(t, err)
	assert.ElementsMatch(t, scopes, got)
}

func TestPG_APIKeyScopes_InsertWithEmptyScopes(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "scope-empty", "Empty Scope Org")
	require.NoError(t, err)

	repo := apikey.NewAPIKey(pool)

	hash := []byte("emptyhash123456789abcdef")
	keyID, err := repo.InsertWithScopes(ctx, tenantID, hash, "fbk_live_emp", "empty key", nil)
	require.NoError(t, err)

	got, err := repo.GetScopes(ctx, keyID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPG_APIKeyScopes_AtomicRollback(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "scope-atomic", "Atomic Scope Org")
	require.NoError(t, err)

	repo := apikey.NewAPIKey(pool)

	hash := []byte("atomichash12345678abcdef")
	scopes := []domain.Scope{domain.ScopeFeedbackRead, domain.ScopeIngestWrite}
	keyID, err := repo.InsertWithScopes(ctx, tenantID, hash, "fbk_live_atm", "atomic key", scopes)
	require.NoError(t, err)

	got, err := repo.GetScopes(ctx, keyID)
	require.NoError(t, err)
	assert.Len(t, got, 2, "both scopes should be inserted atomically")
}
