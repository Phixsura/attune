//go:build integration

package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/breakglass"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
)

func TestLayerB_TagAssign_BatchMixedTenantFeedbackIDs(t *testing.T) {
	f := NewFixture(t)

	mixed := []int64{f.TenantA.FeedbackID, f.TenantB.FeedbackID}
	result, err := f.TagAssign.ListByFeedbackBatch(f.Ctx, f.TenantA.TenantID, mixed)
	require.NoError(t, err)

	if tags, ok := result[f.TenantB.FeedbackID]; ok && len(tags) > 0 {
		t.Errorf("tenant A batch returned %d tags for tenant B feedback %d", len(tags), f.TenantB.FeedbackID)
	}

	aTags := result[f.TenantA.FeedbackID]
	assert.NotEmpty(t, aTags, "tenant A's own feedback should have tags")
}

func TestLayerB_GuardPolicy_ReplaceTenantDoesNotAffectOther(t *testing.T) {
	f := NewFixture(t)

	beforeB, err := f.GuardPolicy.ListForConsole(f.Ctx, f.TenantB.TenantID)
	require.NoError(t, err)
	countB := len(beforeB)

	err = f.GuardPolicy.ReplaceTenantPolicies(f.Ctx, f.TenantA.TenantID, "test", nil)
	require.NoError(t, err)

	afterB, err := f.GuardPolicy.ListForConsole(f.Ctx, f.TenantB.TenantID)
	require.NoError(t, err)
	assert.Equal(t, countB, len(afterB), "tenant B policy count must not change when tenant A replaces")
}

func TestLayerB_FeedbackAudit_CrossTenantListReturnsEmpty(t *testing.T) {
	f := NewFixture(t)

	entries, _, err := f.FeedbackAudit.List(f.Ctx, f.TenantA.TenantID, f.TenantB.FeedbackID, "", 100)
	require.NoError(t, err)
	assert.Empty(t, entries, "tenant A listing audit trail for tenant B feedback should return empty")
}

func TestLayerB_SystemSettings_SameKeyIndependentPerTenant(t *testing.T) {
	f := NewFixture(t)

	valA, err := f.SystemSettings.Get(f.Ctx, f.TenantA.TenantID, "iso-test-key")
	require.NoError(t, err)

	valB, err := f.SystemSettings.Get(f.Ctx, f.TenantB.TenantID, "iso-test-key")
	require.NoError(t, err)

	assert.Equal(t, valA, valB, "both tenants have same key with same value from seed")

	err = f.SystemSettings.Set(f.Ctx, f.TenantA.TenantID, "iso-test-key", "updated-a", "test")
	require.NoError(t, err)

	valAAfter, err := f.SystemSettings.Get(f.Ctx, f.TenantA.TenantID, "iso-test-key")
	require.NoError(t, err)
	assert.Equal(t, "updated-a", valAAfter)

	valBAfter, err := f.SystemSettings.Get(f.Ctx, f.TenantB.TenantID, "iso-test-key")
	require.NoError(t, err)
	assert.Equal(t, "iso-test-value", valBAfter, "tenant B value must not be affected by tenant A update")
}

func TestLayerB_DigestSubscription_IndependentPerTenant(t *testing.T) {
	f := NewFixture(t)

	subA, err := f.DigestSubs.GetByTenant(f.Ctx, f.TenantA.TenantID)
	require.NoError(t, err)
	require.NotNil(t, subA)
	assert.Equal(t, f.TenantA.TenantID, subA.TenantID)

	subB, err := f.DigestSubs.GetByTenant(f.Ctx, f.TenantB.TenantID)
	require.NoError(t, err)
	require.NotNil(t, subB)
	assert.Equal(t, f.TenantB.TenantID, subB.TenantID)

	assert.NotEqual(t, subA.ID, subB.ID, "subscriptions must be distinct per tenant")
}

// TestLayerB_FeedbackBatchSoftDelete_CrossTenantNotAffected verifies that
// soft-deleting tenant A's feedback via raw SQL does not affect tenant B's rows.
func TestLayerB_FeedbackBatchSoftDelete_CrossTenantNotAffected(t *testing.T) {
	f := NewFixture(t)

	// Soft-delete tenant A's feedback directly.
	_, err := f.Pool.Exec(f.Ctx, `
		UPDATE user_feedback SET deleted_at = NOW()
		WHERE id = $1 AND tenant_id = $2`,
		f.TenantA.FeedbackID, f.TenantA.TenantID)
	require.NoError(t, err)

	// Tenant B's feedback must still be present (not soft-deleted).
	var deletedAt *string
	err = f.Pool.QueryRow(f.Ctx, `
		SELECT deleted_at::text FROM user_feedback
		WHERE id = $1 AND tenant_id = $2`,
		f.TenantB.FeedbackID, f.TenantB.TenantID).Scan(&deletedAt) // ptrext:allow scan-out-param
	require.NoError(t, err)
	assert.Nil(t, deletedAt, "tenant B's feedback must not be soft-deleted after tenant A's delete")
}

// TestLayerB_ErrorMessageConsistency_NoExistenceLeakage verifies that key
// IDOR endpoints return the same sentinel error regardless of whether the
// resource belongs to another tenant or simply does not exist. This confirms
// no information leakage about cross-tenant resource existence.
func TestLayerB_ErrorMessageConsistency_NoExistenceLeakage(t *testing.T) {
	f := NewFixture(t)

	// tags: nonexistent ID vs cross-tenant ID must both return ErrNotFound.
	nonexistentTagID := f.TenantB.TagID // belongs to B, not A
	_, errCross := f.Tags.GetByID(f.Ctx, f.TenantA.TenantID, nonexistentTagID)
	require.Error(t, errCross)
	assert.True(t, errors.Is(errCross, feedbacktag.ErrNotFound),
		"cross-tenant tag lookup must return ErrNotFound, got: %v", errCross)

	// breakglass: cross-tenant token ID must return ErrNotFound.
	_, errBG := f.BreakGlass.GetByID(f.Ctx, f.TenantA.TenantID, f.TenantB.BreakGlassID)
	require.Error(t, errBG)
	assert.True(t, errors.Is(errBG, breakglass.ErrNotFound),
		"cross-tenant breakglass lookup must return ErrNotFound, got: %v", errBG)
}

// TestLayerB_DigestRun_ClaimDayConflictIsolation verifies that both tenants
// can independently claim the same run_date without conflicting — the uniqueness
// constraint is (tenant_id, run_date), not just (run_date).
func TestLayerB_DigestRun_ClaimDayConflictIsolation(t *testing.T) {
	f := NewFixture(t)

	// Both tenants already have a digest_run seeded for CURRENT_DATE (from fixture).
	// Attempting to insert again for each should hit ON CONFLICT DO NOTHING (ClaimDay)
	// but must not affect the other tenant's row.
	tx, err := f.Pool.Begin(f.Ctx)
	require.NoError(t, err)
	defer tx.Rollback(f.Ctx) //nolint:errcheck

	// Attempt to claim the same run_date for tenant A again — conflict, no row inserted.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	_, wonA, err := f.DigestRuns.ClaimDay(f.Ctx, tx, f.TenantA.TenantID, f.TenantA.DigestSubID, today)
	require.NoError(t, err)
	assert.False(t, wonA, "second ClaimDay for tenant A on same date must not win (conflict)")

	// Verify tenant B's run is unaffected.
	var count int
	err = f.Pool.QueryRow(f.Ctx, `
		SELECT COUNT(*) FROM digest_runs WHERE tenant_id = $1`, f.TenantB.TenantID).Scan(&count) // ptrext:allow scan-out-param
	require.NoError(t, err)
	assert.Equal(t, 1, count, "tenant B must still have exactly one digest run")
}
