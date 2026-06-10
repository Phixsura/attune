// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package me

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
)

type fakeTenantReader struct {
	row *tenantrepo.Tenant
	id  string
}

func (f *fakeTenantReader) GetByID(_ context.Context, id string) (*tenantrepo.Tenant, error) {
	f.id = id
	return f.row, nil
}

type fakeTenantUserReader struct {
	row       *tenantrepo.TenantUser
	id        string
	touchedID string
}

func (f *fakeTenantUserReader) GetByID(_ context.Context, id string) (*tenantrepo.TenantUser, error) {
	f.id = id
	return f.row, nil
}

func (f *fakeTenantUserReader) TouchLastSeen(_ context.Context, id string) error {
	f.touchedID = id
	return nil
}

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)

	t.Run("me", func(t *testing.T) {
		tenants := &fakeTenantReader{row: &tenantrepo.Tenant{
			ID:       dispatchtest.TenantID,
			Slug:     "acme",
			Name:     "Acme",
			Locale:   "zh-CN",
			Timezone: "Asia/Shanghai",
			IsActive: true,
		}}
		users := &fakeTenantUserReader{row: &tenantrepo.TenantUser{
			ID:        dispatchtest.UserID,
			TenantID:  dispatchtest.TenantID,
			OpenID:    "ou_123",
			Name:      "Alice",
			AvatarURL: "https://example.com/a.png",
			Role:      "admin",
			IsActive:  true,
		}}
		h := &MeHandler{signer: signer, tenants: tenants, users: users}
		handler := dispatcher.Bind(
			"console.MeHandler.Me",
			dispatcher.Empty(func() *attunev1.GetMeRequest { return &attunev1.GetMeRequest{} }),
			h.Me,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetMeRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/me", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, tenants.id)
		require.Equal(t, dispatchtest.UserID, users.id)
		require.Equal(t, dispatchtest.UserID, users.touchedID)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, signer.CSRFToken(dispatchtest.UserID), body["csrfToken"])
		require.Equal(t, "Acme", body["tenant"].(map[string]any)["name"])
		require.Equal(t, "Alice", body["user"].(map[string]any)["name"])
	})

	t.Run("logout", func(t *testing.T) {
		h := &MeHandler{signer: signer}
		handler := dispatcher.Bind(
			"console.MeHandler.Logout",
			dispatcher.Empty(func() *attunev1.LogoutRequest { return &attunev1.LogoutRequest{} }),
			h.Logout,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.LogoutRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/logout", ""))

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Empty(t, w.Body.String())
		setCookie := w.Header().Get("Set-Cookie")
		require.Contains(t, setCookie, session.SessionCookieName+"=")
		require.Contains(t, setCookie, "Max-Age=0")
	})
}
