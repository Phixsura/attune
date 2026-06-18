// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package apikey

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
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

type fakeAPIKeysService struct {
	listRows []apikeyrepo.APIKeyListRow
	listErr  error

	issueRaw   string
	issueID    uuid.UUID
	issueErr   error
	issueLabel string

	revokeID  uuid.UUID
	revokeErr error

	listTenant   string
	issueTenant  string
	revokeTenant string

	scopes []domain.Scope
}

func (f *fakeAPIKeysService) List(_ context.Context, tenantID string) ([]apikeyrepo.APIKeyListRow, error) {
	f.listTenant = tenantID
	return f.listRows, f.listErr
}

func (f *fakeAPIKeysService) Issue(_ context.Context, tenantID, label string) (string, uuid.UUID, error) {
	f.issueTenant = tenantID
	f.issueLabel = label
	return f.issueRaw, f.issueID, f.issueErr
}

func (f *fakeAPIKeysService) IssueWithScopes(_ context.Context, tenantID, label string, scopes []domain.Scope) (string, uuid.UUID, error) {
	f.issueTenant = tenantID
	f.issueLabel = label
	f.scopes = scopes
	return f.issueRaw, f.issueID, f.issueErr
}

func (f *fakeAPIKeysService) GetScopes(_ context.Context, _ uuid.UUID) ([]domain.Scope, error) {
	return f.scopes, nil
}

func (f *fakeAPIKeysService) GetScopesBatch(_ context.Context, keyIDs []uuid.UUID) (map[uuid.UUID][]domain.Scope, error) {
	result := make(map[uuid.UUID][]domain.Scope, len(keyIDs))
	for _, id := range keyIDs {
		result[id] = f.scopes
	}
	return result, nil
}

func (f *fakeAPIKeysService) Revoke(_ context.Context, tenantID string, id uuid.UUID) error {
	f.revokeTenant = tenantID
	f.revokeID = id
	return f.revokeErr
}

func testRequestContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "tenant-1"},
	}
}

func TestListMapsRowsToProto(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 9, 1, 2, 3, 0, time.FixedZone("UTC+8", 8*60*60))
	last := created.Add(time.Hour)
	svc := &fakeAPIKeysService{
		listRows: []apikeyrepo.APIKeyListRow{{
			ID:         id,
			KeyPrefix:  "fbk_live_abc",
			Label:      "Primary",
			IsActive:   true,
			CreatedAt:  created,
			LastUsedAt: &last,
		}},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.List(testRequestContext(), &attunev1.ListApiKeysRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "tenant-1", svc.listTenant)
	require.Len(t, result.Body.GetItems(), 1)
	got := result.Body.GetItems()[0]
	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "fbk_live_abc", got.GetKeyPrefix())
	require.Equal(t, "Primary", got.GetLabel())
	require.True(t, got.GetIsActive())
	require.Equal(t, created.UTC().Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, last.UTC().Format(time.RFC3339), got.GetLastUsedAt())
}

func TestCreateTrimsLabelAndReturnsSecret(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	svc := &fakeAPIKeysService{
		issueRaw: "fbk_live_secret",
		issueID:  id,
		listRows: []apikeyrepo.APIKeyListRow{{
			ID:        id,
			KeyPrefix: "fbk_live_sec",
			Label:     "Primary",
			IsActive:  true,
			CreatedAt: created,
		}},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.Create(testRequestContext(), &attunev1.CreateApiKeyRequest{Label: "  Primary  "})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "tenant-1", svc.issueTenant)
	require.Equal(t, "Primary", svc.issueLabel)
	require.Equal(t, "fbk_live_secret", result.Body.GetSecret())
	require.Equal(t, id.String(), result.Body.GetKey().GetId())
}

func TestCreateRejectsMissingLabel(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}

	_, err := h.Create(testRequestContext(), &attunev1.CreateApiKeyRequest{Label: "  "})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

func TestCreateRejectsAPIKeyAdminScope(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}

	_, err := h.Create(testRequestContext(), &attunev1.CreateApiKeyRequest{
		Label:  "Admin Key",
		Scopes: []string{"apikey:admin", "ingest:write"},
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusForbidden, got.Status)
	require.Equal(t, attunev1.ErrorCode_FORBIDDEN, got.Code)
}

func TestRevokeMapsBadIDAndNotFound(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	_, err := h.Revoke(testRequestContext(), &attunev1.DeleteApiKeyRequest{Id: "not-a-uuid"})
	var badID *dispatcher.Error
	require.ErrorAs(t, err, &badID)
	require.Equal(t, http.StatusBadRequest, badID.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, badID.Code)

	svc := &fakeAPIKeysService{revokeErr: apikeyrepo.ErrAPIKeyNotFound}
	h = &APIKeysHandler{svc: svc}
	id := uuid.New()
	_, err = h.Revoke(testRequestContext(), &attunev1.DeleteApiKeyRequest{Id: id.String()})
	var notFound *dispatcher.Error
	require.ErrorAs(t, err, &notFound)
	require.Equal(t, http.StatusNotFound, notFound.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, notFound.Code)
	require.Equal(t, id, svc.revokeID)
	require.Equal(t, "tenant-1", svc.revokeTenant)
}

func TestRevokeReturnsNoContent(t *testing.T) {
	t.Parallel()

	svc := &fakeAPIKeysService{}
	h := &APIKeysHandler{svc: svc}
	id := uuid.New()

	result, err := h.Revoke(testRequestContext(), &attunev1.DeleteApiKeyRequest{Id: id.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, result.Status)
	require.Equal(t, id, svc.revokeID)
	require.Empty(t, result.Body)
}

func TestListServiceError(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{listErr: errors.New("db down")}}

	_, err := h.List(testRequestContext(), &attunev1.ListApiKeysRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}
