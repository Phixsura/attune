// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}

func (s *breakGlassRepoStub) Issue(context.Context, breakglass.NewToken) (domain.BreakGlassToken, error) {
	return domain.BreakGlassToken{}, nil
}

func (s *breakGlassRepoStub) ListValidForAdmin(context.Context, string, string) ([]domain.BreakGlassToken, error) {
	return nil, nil
}

func (s *breakGlassRepoStub) ListAll(context.Context, string, int) ([]domain.BreakGlassToken, error) {
	return s.listAllTokens, nil
}

func (s *breakGlassRepoStub) MarkUsed(context.Context, string, string, string) error { return nil }

func (s *breakGlassRepoStub) Revoke(context.Context, string, string, string) error { return nil }

func (s *breakGlassRepoStub) RevokeAll(_ context.Context, _ string, _ string) (int64, error) {
	s.revokeAllCalled = true
	return s.revokeAllCount, nil
}

func (s *breakGlassRepoStub) CountValid(context.Context, string) (int, error) { return 0, nil }

func (s *breakGlassRepoStub) Cleanup(context.Context, time.Time) (int64, error) { return 0, nil }

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
