//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
