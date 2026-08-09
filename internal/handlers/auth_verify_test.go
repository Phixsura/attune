// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

type fakeAPIKeyDetailStore struct {
	row *apikeyrepo.APIKeyListRow
	err error

	tenantID string
	keyID    uuid.UUID
}

func (s *fakeAPIKeyDetailStore) GetByID(_ context.Context, tenantID string, id uuid.UUID) (*apikeyrepo.APIKeyListRow, error) {
	s.tenantID = tenantID
	s.keyID = id
	if s.err != nil {
		return nil, s.err
	}
	return s.row, nil
}

func TestNewAuthVerifyHandlerStoresRepoWhenPresent(t *testing.T) {
	require.NotNil(t, NewAuthVerifyHandler(nil))
	require.Nil(t, NewAuthVerifyHandler(nil).repo)
	require.NotNil(t, NewAuthVerifyHandler(apikeyrepo.NewAPIKey(nil)).repo)
}

func TestAuthVerifyHandlerVerifySuccess(t *testing.T) {
	keyID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	expiresAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	rpm := 120
	store := ptrext.Of(fakeAPIKeyDetailStore{row: ptrext.Of(apikeyrepo.APIKeyListRow{
		ID:           keyID,
		KeyPrefix:    "ak_live_123",
		Label:        "Primary",
		ExpiresAt:    ptrext.Of(expiresAt),
		RateLimitRPM: ptrext.Of(rpm),
	})})
	h := ptrext.Of(AuthVerifyHandler{repo: store})
	ctx := ptrext.Of(dispatcher.RequestContext[*apikey.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(apikey.AuthCtx{
			TenantID: "tenant-1",
			KeyID:    keyID,
			Scopes:   []domain.Scope{domain.ScopeIngestWrite, domain.ScopeFeedbackRead},
		}),
	})

	result, err := h.Verify(ctx, ptrext.Of(attunev1.VerifyApiKeyRequest{}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.True(t, result.Body.GetValid())
	require.Equal(t, "ak_live_123", result.Body.GetKeyPrefix())
	require.Equal(t, "Primary", result.Body.GetLabel())
	require.Equal(t, []string{"ingest:write", "feedback:read"}, result.Body.GetScopes())
	require.Equal(t, "2026-07-16T04:00:00Z", result.Body.GetExpiresAt())
	require.Equal(t, int32(120), result.Body.GetRateLimitRpm())
	require.Equal(t, "tenant-1", store.tenantID)
	require.Equal(t, keyID, store.keyID)
}

func TestAuthVerifyHandlerVerifyRepoError(t *testing.T) {
	keyID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	h := ptrext.Of(AuthVerifyHandler{repo: ptrext.Of(fakeAPIKeyDetailStore{err: errors.New("database unavailable")})})
	ctx := ptrext.Of(dispatcher.RequestContext[*apikey.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(apikey.AuthCtx{
			TenantID: "tenant-1",
			KeyID:    keyID,
		}),
	})

	_, err := h.Verify(ctx, ptrext.Of(attunev1.VerifyApiKeyRequest{}))

	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
}

type fakeTenantNameStore struct {
	t   *tenant.Tenant
	err error
}

func (s *fakeTenantNameStore) GetByID(_ context.Context, _ string) (*tenant.Tenant, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.t, nil
}

func TestAuthVerifyHandlerTenantDisplayName(t *testing.T) {
	keyID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	store := ptrext.Of(fakeAPIKeyDetailStore{row: ptrext.Of(apikeyrepo.APIKeyListRow{
		ID: keyID, KeyPrefix: "ak_live_123", Label: "zapier",
	})})
	h := ptrext.Of(AuthVerifyHandler{repo: store})
	h.SetTenantStore(ptrext.Of(fakeTenantNameStore{t: ptrext.Of(tenant.Tenant{Name: "Acme Inc"})}))
	ctx := ptrext.Of(dispatcher.RequestContext[*apikey.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(apikey.AuthCtx{TenantID: "tenant-1", KeyID: keyID}),
	})

	result, err := h.Verify(ctx, ptrext.Of(attunev1.VerifyApiKeyRequest{}))
	require.NoError(t, err)
	require.Equal(t, "Acme Inc", result.Body.GetTenantDisplayName())

	// tenant lookup failure is best-effort — verify still succeeds, label empty
	h2 := ptrext.Of(AuthVerifyHandler{repo: store})
	h2.SetTenantStore(ptrext.Of(fakeTenantNameStore{err: errors.New("boom")}))
	result2, err := h2.Verify(ctx, ptrext.Of(attunev1.VerifyApiKeyRequest{}))
	require.NoError(t, err)
	require.Empty(t, result2.Body.GetTenantDisplayName())
}
