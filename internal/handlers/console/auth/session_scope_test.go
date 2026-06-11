package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

type fakeAdminStore struct {
	row admin.Admin
	err error
}

func (f *fakeAdminStore) GetByEmail(context.Context, string) (admin.Admin, error) {
	return admin.Admin{}, admin.ErrNotFound
}

func (f *fakeAdminStore) GetByID(context.Context, string) (admin.Admin, error) {
	if f.err != nil {
		return admin.Admin{}, f.err
	}
	return f.row, nil
}

func (f *fakeAdminStore) IncrementFailedAttempts(context.Context, string) error {
	return nil
}

func (f *fakeAdminStore) ResetFailedAttempts(context.Context, string) error {
	return nil
}

type fakeTenantScopeResolver struct {
	id  string
	err error
}

func (f *fakeTenantScopeResolver) FirstActiveID(context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.id, nil
}

func TestScopeAdminSessionScopesEmptyAdminTenant(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{
		admins:  ptrext.Of(fakeAdminStore{row: admin.Admin{ID: "admin-1"}}),
		tenants: ptrext.Of(fakeTenantScopeResolver{id: "tenant-1"}),
	})
	req := scopedRequest("", "admin-1")
	rec := httptest.NewRecorder()

	var got *session.AuthCtx
	h.ScopeAdminSession(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = session.FromContext(r.Context())
	})).ServeHTTP(rec, req)

	require.NotNil(t, got)
	require.Equal(t, "tenant-1", got.TenantID)
	require.Equal(t, "admin-1", got.UserID)
	require.Empty(t, rec.Result().Cookies())
}

func TestScopeAdminSessionLeavesTenantUsersUnchanged(t *testing.T) {
	t.Parallel()

	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)

	h := ptrext.Of(Handler{
		signer:  signer,
		admins:  ptrext.Of(fakeAdminStore{err: admin.ErrNotFound}),
		tenants: ptrext.Of(fakeTenantScopeResolver{id: "tenant-1"}),
	})
	req := scopedRequest("", "tenant-user-1")
	rec := httptest.NewRecorder()

	var got *session.AuthCtx
	h.ScopeAdminSession(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = session.FromContext(r.Context())
	})).ServeHTTP(rec, req)

	require.NotNil(t, got)
	require.Empty(t, got.TenantID)
	require.Equal(t, "tenant-user-1", got.UserID)
	require.Empty(t, rec.Result().Cookies())
}

func TestScopeAdminSessionLeavesAdminUnscopedWhenNoTenantExists(t *testing.T) {
	t.Parallel()

	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)

	h := ptrext.Of(Handler{
		signer:  signer,
		admins:  ptrext.Of(fakeAdminStore{row: admin.Admin{ID: "admin-1"}}),
		tenants: ptrext.Of(fakeTenantScopeResolver{err: tenant.ErrTenantNotFound}),
	})
	req := scopedRequest("", "admin-1")
	rec := httptest.NewRecorder()

	var got *session.AuthCtx
	h.ScopeAdminSession(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = session.FromContext(r.Context())
	})).ServeHTTP(rec, req)

	require.NotNil(t, got)
	require.Empty(t, got.TenantID)
	require.Equal(t, "admin-1", got.UserID)
	require.Empty(t, rec.Result().Cookies())
}

func scopedRequest(tenantID, userID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/feedback", nil)
	ctx := session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: tenantID,
		UserID:   userID,
	}))
	return req.WithContext(ctx)
}
