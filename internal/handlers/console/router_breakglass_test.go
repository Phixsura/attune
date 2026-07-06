// SPDX-License-Identifier: Apache-2.0

package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
)

type routerBreakGlassRepoStub struct {
	revokeAllCount int64
}

func (s *routerBreakGlassRepoStub) Issue(context.Context, breakglass.NewToken) (domain.BreakGlassToken, error) {
	return domain.BreakGlassToken{}, nil
}

func (s *routerBreakGlassRepoStub) ListValidForAdmin(context.Context, string, string) ([]domain.BreakGlassToken, error) {
	return nil, nil
}

func (s *routerBreakGlassRepoStub) ListAll(context.Context, string, int) ([]domain.BreakGlassToken, error) {
	return []domain.BreakGlassToken{}, nil
}

func (s *routerBreakGlassRepoStub) MarkUsed(context.Context, string, string, string) error {
	return nil
}

func (s *routerBreakGlassRepoStub) Revoke(context.Context, string, string, string) error { return nil }

func (s *routerBreakGlassRepoStub) RevokeAll(_ context.Context, _ string, _ string) (int64, error) {
	return s.revokeAllCount, nil
}

func (s *routerBreakGlassRepoStub) CountValid(context.Context, string) (int, error) { return 0, nil }

func (s *routerBreakGlassRepoStub) Cleanup(context.Context, time.Time) (int64, error) { return 0, nil }

type routerBreakGlassTenantResolver struct{}

func (routerBreakGlassTenantResolver) FirstActiveID(context.Context) (string, error) {
	return "tenant-1", nil
}

type routerBreakGlassAdminLookup struct{}

func (routerBreakGlassAdminLookup) GetIDByEmail(context.Context, string) (string, error) {
	return "admin-1", nil
}

func TestSSOCutoverBreakGlassRoutes(t *testing.T) {
	t.Parallel()

	svc := breakglasssvc.NewService(ptrext.Of(routerBreakGlassRepoStub{revokeAllCount: 2}), breakglasssvc.DefaultConfig())
	lockout := breakglasssvc.NewLockoutTracker(breakglasssvc.LockoutConfig{
		MaxAttempts:         3,
		BaseLockoutDuration: time.Minute,
		MaxLockoutDuration:  time.Minute,
		AttemptWindow:       15 * time.Minute,
	})
	for i := 0; i < 3; i++ {
		lockout.RecordFailure("203.0.113.10")
	}

	router := ptrext.Of(Router{
		rbac:          rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin}),
		breakglass:    auth.NewBreakGlassHandler(svc, mustSigner(t), routerBreakGlassTenantResolver{}, routerBreakGlassAdminLookup{}, nil, nil, lockout, ""),
		breakglassAPI: auth.NewBreakGlassAPIHandler(svc, nil, nil),
	})
	mux := chi.NewRouter()
	router.mountSSOCutover(mux)

	req := authedReq()
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserType: "oidc_user",
		UserID:   "user-1",
	})))

	t.Run("revoke all route", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, cloneRequest(t, req, http.MethodPost, "/auth/breakglass/tokens/revoke-all"))
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), `"revoked":2`)
	})

	t.Run("list lockouts route", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, cloneRequest(t, req, http.MethodGet, "/auth/breakglass/lockouts"))
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), "203.0.113.10")
	})

	t.Run("unlock lockout route", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, cloneRequest(t, req, http.MethodPost, "/auth/breakglass/lockouts/203.0.113.10/unlock"))
		require.Equal(t, http.StatusNoContent, w.Code)
		require.False(t, lockout.IsLocked("203.0.113.10"))
	})
}

func mustSigner(t *testing.T) *session.Signer {
	t.Helper()
	signer, err := session.NewSigner("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	return signer
}

func cloneRequest(t *testing.T, src *http.Request, method, path string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(src.Context())
	return req
}
