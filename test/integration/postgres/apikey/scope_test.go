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

func TestPG_APIKeyScopes_InsertAndGet(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "scope-test", "Scope Test Org")
	require.NoError(t, err)

	repo := apikey.NewAPIKey(pool)

	hash := []byte("testhash0123456789abcdef")
	keyID, err := repo.Insert(ctx, tenantID, hash, "fbk_live_tes", "test key")
	require.NoError(t, err)

	scopes := []domain.Scope{domain.ScopeFeedbackRead, domain.ScopeIngestWrite}
	err = repo.InsertScopes(ctx, keyID, scopes)
	require.NoError(t, err)

	got, err := repo.GetScopes(ctx, keyID)
	require.NoError(t, err)
	assert.ElementsMatch(t, scopes, got)
}

func TestPG_APIKeyScopes_EmptyScopes(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "scope-empty", "Empty Scope Org")
	require.NoError(t, err)

	repo := apikey.NewAPIKey(pool)

	hash := []byte("emptyhash123456789abcdef")
	keyID, err := repo.Insert(ctx, tenantID, hash, "fbk_live_emp", "empty key")
	require.NoError(t, err)

	err = repo.InsertScopes(ctx, keyID, nil)
	require.NoError(t, err)

	got, err := repo.GetScopes(ctx, keyID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPG_APIKeyScopes_GetScopesByHash(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "scope-hash", "Hash Scope Org")
	require.NoError(t, err)

	repo := apikey.NewAPIKey(pool)

	hash := []byte("hashscope123456789abcdef")
	keyID, err := repo.Insert(ctx, tenantID, hash, "fbk_live_hsh", "hash key")
	require.NoError(t, err)

	scopes := []domain.Scope{domain.ScopeLLMRead, domain.ScopeAuditRead}
	err = repo.InsertScopes(ctx, keyID, scopes)
	require.NoError(t, err)

	got, err := repo.GetScopesByHash(ctx, hash)
	require.NoError(t, err)
	assert.ElementsMatch(t, scopes, got)
}
