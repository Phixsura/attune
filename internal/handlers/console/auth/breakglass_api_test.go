// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
)

type breakGlassRepoStub struct {
	revokeAllCalled bool
	revokeAllCount  int64
	listAllTokens   []domain.BreakGlassToken
	issueInput      breakglass.NewToken
	issueToken      domain.BreakGlassToken
	issueErr        error
	revokeCalled    bool
	revokeTenantID  string
	revokeTokenID   string
	revokeBy        string
	revokeErr       error
	listAllErr      error
}

func (s *breakGlassRepoStub) Issue(_ context.Context, n breakglass.NewToken) (domain.BreakGlassToken, error) {
	s.issueInput = n
	if s.issueErr != nil {
		return domain.BreakGlassToken{}, s.issueErr
	}
	if s.issueToken.ID != "" {
		return s.issueToken, nil
	}
	return domain.BreakGlassToken{
		ID:         "token-issued",
		TenantID:   n.TenantID,
		AdminEmail: n.AdminEmail,
		TokenHash:  n.TokenHash,
		ExpiresAt:  n.ExpiresAt,
		IssuedBy:   n.IssuedBy,
		IssuedAt:   time.Now(),
		AllowedIPs: n.AllowedIPs,
	}, nil
}

func (s *breakGlassRepoStub) ListValidForAdmin(context.Context, string, string) ([]domain.BreakGlassToken, error) {
	return nil, nil
}

func (s *breakGlassRepoStub) ListAll(context.Context, string, int) ([]domain.BreakGlassToken, error) {
	if s.listAllErr != nil {
		return nil, s.listAllErr
	}
	return s.listAllTokens, nil
}

func (s *breakGlassRepoStub) MarkUsed(context.Context, string, string, string) error { return nil }

func (s *breakGlassRepoStub) Revoke(_ context.Context, tenantID, id, revokedBy string) error {
	s.revokeCalled = true
	s.revokeTenantID = tenantID
	s.revokeTokenID = id
	s.revokeBy = revokedBy
	return s.revokeErr
}

func (s *breakGlassRepoStub) RevokeAll(_ context.Context, _ string, _ string) (int64, error) {
	s.revokeAllCalled = true
	return s.revokeAllCount, nil
}

func (s *breakGlassRepoStub) CountValid(context.Context, string) (int, error) { return 0, nil }

func (s *breakGlassRepoStub) Cleanup(context.Context, time.Time) (int64, error) { return 0, nil }

func authedBreakGlassAPIRequest(method, target, body string) *http.Request {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	return req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "admin",
	})))
}

func TestBreakGlassAPIListTokens(t *testing.T) {
	t.Parallel()

	now := time.Now()
	usedAt := now.Add(-10 * time.Minute)
	revokedAt := now.Add(-9 * time.Minute)
	revokedBy := "admin-2"
	usedIP := "203.0.113.10"
	repo := ptrext.Of(breakGlassRepoStub{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:         "valid",
				TenantID:   "tenant-1",
				AdminEmail: "valid@example.com",
				ExpiresAt:  now.Add(30 * time.Minute),
				IssuedBy:   "user-1",
				IssuedAt:   now.Add(-time.Minute),
				AllowedIPs: []string{"203.0.113.0/24"},
			},
			{
				ID:         "used",
				TenantID:   "tenant-1",
				AdminEmail: "used@example.com",
				ExpiresAt:  now.Add(30 * time.Minute),
				UsedAt:     ptrext.Of(usedAt),
				UsedFromIP: ptrext.Of(usedIP),
				IssuedBy:   "user-1",
				IssuedAt:   now.Add(-time.Hour),
			},
			{
				ID:         "revoked",
				TenantID:   "tenant-1",
				AdminEmail: "revoked@example.com",
				ExpiresAt:  now.Add(30 * time.Minute),
				IssuedBy:   "user-1",
				IssuedAt:   now.Add(-time.Hour),
				RevokedAt:  ptrext.Of(revokedAt),
				RevokedBy:  ptrext.Of(revokedBy),
			},
			{
				ID:         "expired",
				TenantID:   "tenant-1",
				AdminEmail: "expired@example.com",
				ExpiresAt:  now.Add(-time.Minute),
				IssuedBy:   "user-1",
				IssuedAt:   now.Add(-time.Hour),
			},
		},
	})
	h := NewBreakGlassAPIHandler(breakglasssvc.NewService(repo, breakglasssvc.DefaultConfig()), nil, nil)
	w := httptest.NewRecorder()

	h.List(w, authedBreakGlassAPIRequest(http.MethodGet, "/fb/v1/console/auth/breakglass/tokens", ""))

	require.Equal(t, http.StatusOK, w.Code)
	var resp ListTokensResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Tokens, 4)
	require.Equal(t, "valid", resp.Tokens[0].Status)
	require.Equal(t, []string{"203.0.113.0/24"}, resp.Tokens[0].AllowedIPs)
	require.Equal(t, "used", resp.Tokens[1].Status)
	require.Equal(t, ptrext.Of(usedIP), resp.Tokens[1].UsedFromIP)
	require.NotNil(t, resp.Tokens[1].UsedAt)
	require.Equal(t, "revoked", resp.Tokens[2].Status)
	require.Equal(t, ptrext.Of(revokedBy), resp.Tokens[2].RevokedBy)
	require.NotNil(t, resp.Tokens[2].RevokedAt)
	require.Equal(t, "expired", resp.Tokens[3].Status)
}

func TestBreakGlassAPIListTokensHandlesServiceError(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(breakGlassRepoStub{listAllErr: errors.New("db down")})
	h := NewBreakGlassAPIHandler(breakglasssvc.NewService(repo, breakglasssvc.DefaultConfig()), nil, nil)
	w := httptest.NewRecorder()

	h.List(w, authedBreakGlassAPIRequest(http.MethodGet, "/fb/v1/console/auth/breakglass/tokens", ""))

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestBreakGlassAPIIssue(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(breakGlassRepoStub{})
	h := NewBreakGlassAPIHandler(breakglasssvc.NewService(repo, breakglasssvc.DefaultConfig()), nil, nil)
	body := `{"admin_email":"admin@example.com","ttl_minutes":5,"allowed_ips":["203.0.113.0/24"]}`
	w := httptest.NewRecorder()

	h.Issue(w, authedBreakGlassAPIRequest(http.MethodPost, "/fb/v1/console/auth/breakglass/tokens", body))

	require.Equal(t, http.StatusCreated, w.Code)
	require.Equal(t, "tenant-1", repo.issueInput.TenantID)
	require.Equal(t, "admin@example.com", repo.issueInput.AdminEmail)
	require.Equal(t, "user-1", repo.issueInput.IssuedBy)
	require.Equal(t, []string{"203.0.113.0/24"}, repo.issueInput.AllowedIPs)
	require.GreaterOrEqual(t, time.Until(repo.issueInput.ExpiresAt), 4*time.Minute)

	var resp IssueResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "token-issued", resp.Token.ID)
	require.Equal(t, "valid", resp.Token.Status)
	require.NotEmpty(t, resp.RawToken)
	require.NotEmpty(t, resp.ExpiresAt)
}

func TestBreakGlassAPIIssueRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	h := NewBreakGlassAPIHandler(breakglasssvc.NewService(ptrext.Of(breakGlassRepoStub{}), breakglasssvc.DefaultConfig()), nil, nil)
	cases := []struct {
		name string
		body string
	}{
		{name: "bad json", body: `{"admin_email":`},
		{name: "missing email", body: `{"ttl_minutes":5}`},
		{name: "ttl too low", body: `{"admin_email":"admin@example.com","ttl_minutes":4}`},
		{name: "ttl too high", body: `{"admin_email":"admin@example.com","ttl_minutes":1441}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()

			h.Issue(w, authedBreakGlassAPIRequest(http.MethodPost, "/fb/v1/console/auth/breakglass/tokens", tc.body))

			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestBreakGlassAPIRevoke(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(breakGlassRepoStub{})
	h := NewBreakGlassAPIHandler(breakglasssvc.NewService(repo, breakglasssvc.DefaultConfig()), nil, nil)
	req := authedBreakGlassAPIRequest(http.MethodDelete, "/fb/v1/console/auth/breakglass/tokens/token-1", "")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "token-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()

	h.Revoke(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, repo.revokeCalled)
	require.Equal(t, "tenant-1", repo.revokeTenantID)
	require.Equal(t, "token-1", repo.revokeTokenID)
	require.Equal(t, "user-1", repo.revokeBy)
}

func TestBreakGlassAPIRevokeRejectsMissingTokenID(t *testing.T) {
	t.Parallel()

	h := NewBreakGlassAPIHandler(nil, nil, nil)
	w := httptest.NewRecorder()

	h.Revoke(w, authedBreakGlassAPIRequest(http.MethodDelete, "/fb/v1/console/auth/breakglass/tokens/", ""))

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBreakGlassAPIRevokeAll(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(breakGlassRepoStub{revokeAllCount: 3})
	svc := breakglasssvc.NewService(repo, breakglasssvc.DefaultConfig())
	h := NewBreakGlassAPIHandler(svc, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/auth/breakglass/tokens/revoke-all", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
	})))
	w := httptest.NewRecorder()

	h.RevokeAll(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, repo.revokeAllCalled)

	var resp RevokeAllResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, int64(3), resp.Revoked)
}

func TestBreakGlassAPIListLockouts(t *testing.T) {
	t.Parallel()

	tracker := breakglasssvc.NewLockoutTracker(breakglasssvc.LockoutConfig{
		MaxAttempts:         3,
		BaseLockoutDuration: 1 * time.Minute,
		MaxLockoutDuration:  1 * time.Minute,
		AttemptWindow:       15 * time.Minute,
	})
	for i := 0; i < 3; i++ {
		tracker.RecordFailure("203.0.113.10")
	}

	h := NewBreakGlassAPIHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/auth/breakglass/lockouts", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
	})))
	w := httptest.NewRecorder()

	h.ListLockouts(w, req, tracker)

	require.Equal(t, http.StatusOK, w.Code)

	var resp ListLockoutsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Lockouts, 1)
	require.Equal(t, "203.0.113.10", resp.Lockouts[0].IP)
	require.Equal(t, 3, resp.Lockouts[0].Attempts)
}

func TestBreakGlassAPIUnlockIP(t *testing.T) {
	t.Parallel()

	tracker := breakglasssvc.NewLockoutTracker(breakglasssvc.DefaultLockoutConfig())
	for i := 0; i < 5; i++ {
		tracker.RecordFailure("203.0.113.11")
	}
	require.True(t, tracker.IsLocked("203.0.113.11"))

	h := NewBreakGlassAPIHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/auth/breakglass/lockouts/203.0.113.11/unlock", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
	})))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("ip", "203.0.113.11")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()

	h.UnlockIP(w, req, tracker)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.False(t, tracker.IsLocked("203.0.113.11"))
}
