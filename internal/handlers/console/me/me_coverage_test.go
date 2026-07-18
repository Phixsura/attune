// ptrext:file-allow test stubs use handler pointers and proto fixtures.
package me

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/oidcuser"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
)

// ---------------------------------------------------------------------------
// Stubs — named differently from fakeTenantReader / fakeTenantUserReader
// already defined in me_http_test.go.
// ---------------------------------------------------------------------------

type stubAdminReader struct {
	admin admin.Admin
	err   error
}

func (s *stubAdminReader) GetByID(_ context.Context, _ string) (admin.Admin, error) {
	return s.admin, s.err
}

type stubOIDCUserReader struct {
	user domain.OIDCUser
	err  error
}

func (s *stubOIDCUserReader) GetByID(_ context.Context, _ string) (domain.OIDCUser, error) {
	return s.user, s.err
}

type stubTenantGetter struct {
	tenant *tenantrepo.Tenant
	err    error
}

func (s *stubTenantGetter) GetByID(_ context.Context, _ string) (*tenantrepo.Tenant, error) {
	return s.tenant, s.err
}

type stubUserGetter struct {
	user     *tenantrepo.TenantUser
	err      error
	touchErr error
}

func (s *stubUserGetter) GetByID(_ context.Context, _ string) (*tenantrepo.TenantUser, error) {
	return s.user, s.err
}

func (s *stubUserGetter) TouchLastSeen(_ context.Context, _ string) error {
	return s.touchErr
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testSigner(t *testing.T) *session.Signer {
	t.Helper()
	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)
	return signer
}

func TestNewMeHandlerCreatesHandler(t *testing.T) {
	t.Parallel()

	h := NewMeHandler(nil, nil, nil, nil, nil)

	require.NotNil(t, h)
	require.Nil(t, h.signer)
	require.Nil(t, h.tenants)
	require.Nil(t, h.users)
	require.Nil(t, h.admins)
	require.Nil(t, h.oidcUsers)
}

// directCtx builds a RequestContext for handler methods that do NOT call
// ClearSessionCookie (no response writer needed).
func directCtx(tenantID, userID, userType string) *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: tenantID, UserID: userID, UserType: userType}),
	})
}

// httpDispatch exercises a path through the full HTTP transport (needed when
// the handler calls ClearSessionCookie, which writes to the response writer).
func httpDispatch(t *testing.T, h *MeHandler, tenantID, userID, userType string) *httptest.ResponseRecorder {
	t.Helper()
	handler := dispatcher.Bind(
		"test.Me",
		dispatcher.Empty(func() *attunev1.GetMeRequest { return ptrext.Of(attunev1.GetMeRequest{}) }),
		h.Me,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetMeRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/fb/v1/console/me", nil)
	r = r.WithContext(session.WithAuthCtx(r.Context(), ptrext.Of(session.AuthCtx{
		TenantID: tenantID,
		UserID:   userID,
		UserType: userType,
	})))
	handler(w, r)
	return w
}

// ---------------------------------------------------------------------------
// Admin session path
// ---------------------------------------------------------------------------

func TestMe_AdminFound_NoTenant(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{
			admin: admin.Admin{ID: "adm-1", Email: "root@co.dev", DisplayName: "Root", Role: "admin"},
		}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	ctx := directCtx("", "adm-1", "")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.Equal(t, "_admin", res.Body.Tenant.Slug)
	require.Equal(t, "System Admin", res.Body.Tenant.Name)
	require.Equal(t, "adm-1", res.Body.User.OpenId)
	require.Equal(t, "Root", res.Body.User.Name)
	require.Equal(t, "admin", res.Body.User.Role)
	require.Equal(t, signer.CSRFToken("adm-1"), res.Body.CsrfToken)
}

func TestMe_AdminFound_WithTenant(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{
			admin: admin.Admin{ID: "adm-1", Email: "root@co.dev", DisplayName: "Root"},
		}),
		tenants: ptrext.Of(stubTenantGetter{
			tenant: ptrext.Of(tenantrepo.Tenant{
				ID: "t-1", Slug: "acme", Name: "Acme Corp", Locale: "en-US", Timezone: "America/New_York",
			}),
		}),
	})

	ctx := directCtx("t-1", "adm-1", "")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.Equal(t, "t-1", res.Body.Tenant.Id)
	require.Equal(t, "acme", res.Body.Tenant.Slug)
	require.Equal(t, "Acme Corp", res.Body.Tenant.Name)
	require.Equal(t, "en-US", res.Body.Tenant.Locale)
	require.Equal(t, "America/New_York", res.Body.Tenant.Timezone)
}

func TestMe_AdminFound_TenantLookupFails(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{
			admin: admin.Admin{ID: "adm-1", Email: "root@co.dev"},
		}),
		tenants: ptrext.Of(stubTenantGetter{
			err: errors.New("db down"),
		}),
	})

	ctx := directCtx("t-1", "adm-1", "")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	// Falls back to synthetic admin tenant.
	require.Equal(t, "_admin", res.Body.Tenant.Slug)
	require.Equal(t, "System Admin", res.Body.Tenant.Name)
}

func TestMe_AdminLookupInternalError(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{
			err: errors.New("connection refused"),
		}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	ctx := directCtx("t-1", "adm-1", "")
	_, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
}

func TestMe_AdminLookupContextCanceled_MapsToClientCanceled(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{
			err: context.Canceled,
		}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	w := httpDispatch(t, h, "t-1", "adm-1", "")
	require.Equal(t, dispatcher.StatusClientClosedRequest, w.Code)

	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "CLIENT_CANCELED", body["code"])
}

func TestMe_AdminNotFound_NoTenant_ClearsSession(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
	})

	// Uses HTTP dispatch because ClearSessionCookie writes to the response.
	w := httpDispatch(t, h, "", "gone-user", "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "USER_GONE", body["code"])

	setCookie := w.Header().Get("Set-Cookie")
	require.Contains(t, setCookie, session.SessionCookieName)
	require.Contains(t, setCookie, "Max-Age=0")
}

// ---------------------------------------------------------------------------
// OIDC user session path
// ---------------------------------------------------------------------------

func TestMe_OIDC_NotConfigured(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer:    signer,
		oidcUsers: nil,
	})

	ctx := directCtx("", "ou-1", "oidc")
	_, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Contains(t, de.Message, "OIDC not configured")
}

func TestMe_OIDC_UserFound(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			user: domain.OIDCUser{
				ID:          "ou-1",
				ExternalID:  "ext-sub-123",
				Email:       "alice@idp.dev",
				DisplayName: "Alice SSO",
				Role:        "member",
			},
		}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	ctx := directCtx("", "ou-1", "oidc")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.Equal(t, "ext-sub-123", res.Body.User.OpenId)
	require.Equal(t, "Alice SSO", res.Body.User.Name)
	require.Equal(t, "member", res.Body.User.Role)
	require.Equal(t, signer.CSRFToken("ou-1"), res.Body.CsrfToken)
}

func TestMe_OIDC_UserNotFound_ClearsSession(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			err: oidcuser.ErrNotFound,
		}),
	})

	// Uses HTTP dispatch because ClearSessionCookie writes to the response.
	w := httpDispatch(t, h, "", "ou-gone", "oidc")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "USER_GONE", body["code"])

	setCookie := w.Header().Get("Set-Cookie")
	require.Contains(t, setCookie, session.SessionCookieName)
	require.Contains(t, setCookie, "Max-Age=0")
}

func TestMe_OIDC_InternalErrorReturns500WithoutClearingSession(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			err: errors.New("database unavailable"),
		}),
	})

	ctx := directCtx("", "ou-broken", "oidc")
	_, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.Error(t, err)

	var de *dispatcher.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
	require.Contains(t, de.Message, "failed to load OIDC user")
}

func TestMe_OIDC_UserNotFound_ClearsSessionWhenRepoReturnsSentinel(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			err: oidcuser.ErrNotFound,
		}),
	})

	w := httpDispatch(t, h, "", "ou-gone", "oidc")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "USER_GONE", body["code"])
}

func TestMe_OIDC_EmptyDisplayName_FallsBackToEmail(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			user: domain.OIDCUser{
				ID:          "ou-2",
				ExternalID:  "ext-sub-456",
				Email:       "bob@idp.dev",
				DisplayName: "",
				Role:        "admin",
			},
		}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	ctx := directCtx("", "ou-2", "oidc")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, "bob@idp.dev", res.Body.User.Name)
}

func TestMe_OIDC_WithTenant(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			user: domain.OIDCUser{
				ID:         "ou-3",
				ExternalID: "ext-sub-789",
				Email:      "carol@idp.dev",
				Role:       "member",
			},
		}),
		tenants: ptrext.Of(stubTenantGetter{
			tenant: ptrext.Of(tenantrepo.Tenant{
				ID: "t-2", Slug: "globex", Name: "Globex", Locale: "ja-JP", Timezone: "Asia/Tokyo",
			}),
		}),
	})

	ctx := directCtx("t-2", "ou-3", "oidc")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, "t-2", res.Body.Tenant.Id)
	require.Equal(t, "globex", res.Body.Tenant.Slug)
	require.Equal(t, "Globex", res.Body.Tenant.Name)
}

func TestMe_OIDC_NoTenant_SyntheticOIDCTenant(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		oidcUsers: ptrext.Of(stubOIDCUserReader{
			user: domain.OIDCUser{
				ID:          "ou-4",
				ExternalID:  "ext-sub-000",
				Email:       "dan@idp.dev",
				DisplayName: "Dan",
				Role:        "member",
			},
		}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	ctx := directCtx("", "ou-4", "oidc")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, "_oidc", res.Body.Tenant.Id)
	require.Equal(t, "_oidc", res.Body.Tenant.Slug)
	require.Equal(t, "SSO User", res.Body.Tenant.Name)
}

// ---------------------------------------------------------------------------
// Tenant user session path
// ---------------------------------------------------------------------------

func TestMe_TenantUser_NotFound_ClearsSession(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer:  signer,
		admins:  ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
		users:   ptrext.Of(stubUserGetter{err: tenantrepo.ErrTenantUserNotFound}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	// Uses HTTP dispatch because ClearSessionCookie writes to the response.
	w := httpDispatch(t, h, "t-1", "gone-user", "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "USER_GONE", body["code"])

	setCookie := w.Header().Get("Set-Cookie")
	require.Contains(t, setCookie, session.SessionCookieName)
	require.Contains(t, setCookie, "Max-Age=0")
}

func TestMe_TenantUser_TenantLookupFails(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
		users: ptrext.Of(stubUserGetter{
			user: ptrext.Of(tenantrepo.TenantUser{
				ID: "u-1", TenantID: "t-1", OpenID: "ou_1", Name: "Eve", Role: "member",
			}),
		}),
		tenants: ptrext.Of(stubTenantGetter{err: errors.New("db timeout")}),
	})

	ctx := directCtx("t-1", "u-1", "")
	_, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestMe_TenantUser_WithAvatarURL(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
		users: ptrext.Of(stubUserGetter{
			user: ptrext.Of(tenantrepo.TenantUser{
				ID: "u-2", TenantID: "t-1", OpenID: "ou_2", Name: "Frank",
				AvatarURL: "https://cdn.example.com/avatar.png", Role: "admin",
			}),
		}),
		tenants: ptrext.Of(stubTenantGetter{
			tenant: ptrext.Of(tenantrepo.Tenant{
				ID: "t-1", Slug: "acme", Name: "Acme", Locale: "en-US", Timezone: "UTC",
			}),
		}),
	})

	ctx := directCtx("t-1", "u-2", "")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.NotNil(t, res.Body.User.AvatarUrl)
	require.Equal(t, "https://cdn.example.com/avatar.png", ptrext.Indirect(res.Body.User.AvatarUrl))
	require.Equal(t, signer.CSRFToken("u-2"), res.Body.CsrfToken)
}

func TestMe_TenantUser_WithoutAvatarURL(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
		users: ptrext.Of(stubUserGetter{
			user: ptrext.Of(tenantrepo.TenantUser{
				ID: "u-3", TenantID: "t-1", OpenID: "ou_3", Name: "Grace",
				AvatarURL: "", Role: "member",
			}),
		}),
		tenants: ptrext.Of(stubTenantGetter{
			tenant: ptrext.Of(tenantrepo.Tenant{
				ID: "t-1", Slug: "acme", Name: "Acme",
			}),
		}),
	})

	ctx := directCtx("t-1", "u-3", "")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Nil(t, res.Body.User.AvatarUrl)
}

func TestMe_TenantUser_TouchLastSeenFails_StillSucceeds(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer: signer,
		admins: ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
		users: ptrext.Of(stubUserGetter{
			user: ptrext.Of(tenantrepo.TenantUser{
				ID: "u-4", TenantID: "t-1", OpenID: "ou_4", Name: "Heidi", Role: "member",
			}),
			touchErr: errors.New("redis timeout"),
		}),
		tenants: ptrext.Of(stubTenantGetter{
			tenant: ptrext.Of(tenantrepo.Tenant{
				ID: "t-1", Slug: "acme", Name: "Acme",
			}),
		}),
	})

	ctx := directCtx("t-1", "u-4", "")
	res, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.Equal(t, "Heidi", res.Body.User.Name)
}

func TestMe_TenantUser_GetByIDInternalError(t *testing.T) {
	t.Parallel()

	signer := testSigner(t)
	h := ptrext.Of(MeHandler{
		signer:  signer,
		admins:  ptrext.Of(stubAdminReader{err: admin.ErrNotFound}),
		users:   ptrext.Of(stubUserGetter{err: errors.New("unexpected pg error")}),
		tenants: ptrext.Of(stubTenantGetter{}),
	})

	ctx := directCtx("t-1", "u-5", "")
	_, err := h.Me(ctx, ptrext.Of(attunev1.GetMeRequest{}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Contains(t, de.Message, "failed to load user")
}
