// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// ---------------------------------------------------------------------------
// NewHandler
// ---------------------------------------------------------------------------

func TestNewHandler_NonNil(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil, nil, nil, "https://example.com")
	require.NotNil(t, h)
}

func TestNewHandler_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantURL string
	}{
		{"no slash", "https://example.com", "https://example.com"},
		{"single slash", "https://example.com/", "https://example.com"},
		{"multiple slashes", "https://example.com///", "https://example.com"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := NewHandler(nil, nil, nil, nil, tt.input)
			require.Equal(t, tt.wantURL, h.baseURL)
		})
	}
}

// ---------------------------------------------------------------------------
// RequireLoginOrigin
// ---------------------------------------------------------------------------

func TestRequireLoginOrigin_NoOriginAllows(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{baseURL: "https://example.com"})
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	req := &attunev1.LoginRequest{} // ptrext:allow proto-mutex
	err := h.RequireLoginOrigin(r, req)
	require.NoError(t, err)
}

func TestRequireLoginOrigin_MatchingOriginAllows(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{baseURL: "https://example.com"})
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.Header.Set("Origin", "https://example.com")
	req := &attunev1.LoginRequest{} // ptrext:allow proto-mutex
	err := h.RequireLoginOrigin(r, req)
	require.NoError(t, err)
}

func TestRequireLoginOrigin_CrossSiteRejects(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{baseURL: "https://example.com"})
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.Header.Set("Origin", "https://evil.com")
	req := &attunev1.LoginRequest{} // ptrext:allow proto-mutex
	err := h.RequireLoginOrigin(r, req)
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusForbidden, de.Status)
}

// ---------------------------------------------------------------------------
// ValidateRequest (Login)
// ---------------------------------------------------------------------------

func TestValidateRequest_EmptyEmail(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	req := &attunev1.LoginRequest{Password: "secret"} // ptrext:allow proto-mutex
	err := h.ValidateRequest(httptest.NewRequest(http.MethodPost, "/", nil), req)
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestValidateRequest_EmptyPassword(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	req := &attunev1.LoginRequest{Email: "admin@example.com"} // ptrext:allow proto-mutex
	err := h.ValidateRequest(httptest.NewRequest(http.MethodPost, "/", nil), req)
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestValidateRequest_BothPresentOK(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	// ptrext:allow proto-mutex
	req := &attunev1.LoginRequest{Email: "admin@example.com", Password: "secret"}
	err := h.ValidateRequest(httptest.NewRequest(http.MethodPost, "/", nil), req)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// resolveAdminScope
// ---------------------------------------------------------------------------

func TestResolveAdminScope_NilTenants(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{tenants: nil})
	got := h.resolveAdminScope(context.Background(), "test")
	require.Equal(t, "", got)
}

func TestResolveAdminScope_ReturnsFirstActiveID(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		tenants: ptrext.Of(fakeTenantScopeResolver{id: "tenant-abc"}),
	})
	got := h.resolveAdminScope(context.Background(), "test")
	require.Equal(t, "tenant-abc", got)
}

func TestResolveAdminScope_TenantNotFound(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		tenants: ptrext.Of(fakeTenantScopeResolver{err: tenant.ErrTenantNotFound}),
	})
	got := h.resolveAdminScope(context.Background(), "test")
	require.Equal(t, "", got)
}

func TestResolveAdminScope_OtherError(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		tenants: ptrext.Of(fakeTenantScopeResolver{err: errors.New("db down")}),
	})
	got := h.resolveAdminScope(context.Background(), "test")
	require.Equal(t, "", got)
}

// ---------------------------------------------------------------------------
// ensureAdminMembership
// ---------------------------------------------------------------------------

func TestEnsureAdminMembership_NilHandler(t *testing.T) {
	t.Parallel()
	var h *Handler
	// must not panic
	h.ensureAdminMembership(context.Background(), "test", "t1", "u1")
}

func TestEnsureAdminMembership_NilMembers(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{members: nil})
	// must not panic
	h.ensureAdminMembership(context.Background(), "test", "t1", "u1")
}

func TestEnsureAdminMembership_EmptyTenantID(t *testing.T) {
	t.Parallel()
	ms := ptrext.Of(fakeAdminMembershipStore{})
	h := ptrext.Of(Handler{members: ms})
	h.ensureAdminMembership(context.Background(), "test", "", "u1")
	require.Equal(t, 0, ms.calls)
}

func TestEnsureAdminMembership_EmptyUserID(t *testing.T) {
	t.Parallel()
	ms := ptrext.Of(fakeAdminMembershipStore{})
	h := ptrext.Of(Handler{members: ms})
	h.ensureAdminMembership(context.Background(), "test", "t1", "")
	require.Equal(t, 0, ms.calls)
}

func TestEnsureAdminMembership_ValidCall(t *testing.T) {
	t.Parallel()
	ms := ptrext.Of(fakeAdminMembershipStore{})
	h := ptrext.Of(Handler{members: ms})
	h.ensureAdminMembership(context.Background(), "test", "tenant-1", "user-1")
	require.Equal(t, 1, ms.calls)
	require.Equal(t, "tenant-1", ms.tenantID)
	require.Equal(t, "user-1", ms.userID)
}

// ---------------------------------------------------------------------------
// HashPassword
// ---------------------------------------------------------------------------

func TestHashPassword_ProducesVerifiableHash(t *testing.T) {
	t.Parallel()
	plain := "my-secure-password-123"
	hash, err := HashPassword(plain)
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	require.True(t, VerifyOrDummy(hash, plain))
	require.False(t, VerifyOrDummy(hash, "wrong-password"))
}

func TestHashPassword_DifferentCallsProduceDifferentHashes(t *testing.T) {
	t.Parallel()
	h1, err := HashPassword("same-password-1234")
	require.NoError(t, err)
	h2, err := HashPassword("same-password-1234")
	require.NoError(t, err)
	// bcrypt salts should make them different
	require.NotEqual(t, h1, h2)
}

// ---------------------------------------------------------------------------
// redirectIsSafe edge cases
// ---------------------------------------------------------------------------

func TestRedirectIsSafe_BackslashSecondChar(t *testing.T) {
	t.Parallel()
	require.False(t, redirectIsSafe("https://attune.app", "/\\evil.com"))
}

func TestRedirectIsSafe_NonASCII(t *testing.T) {
	t.Parallel()
	require.False(t, redirectIsSafe("https://attune.app", "/p\x80th"))
}

func TestRedirectIsSafe_EmptyPostLogin(t *testing.T) {
	t.Parallel()
	require.False(t, redirectIsSafe("https://attune.app", ""))
}

func TestRedirectIsSafe_UnparseableBaseURL(t *testing.T) {
	t.Parallel()
	// A baseURL with no scheme that makes combined parse fail the host check
	require.False(t, redirectIsSafe("://", "/console"))
}

func TestRedirectIsSafe_TabCharacter(t *testing.T) {
	t.Parallel()
	require.False(t, redirectIsSafe("https://attune.app", "/con\tsole"))
}

// ---------------------------------------------------------------------------
// originAllowed — Sec-Fetch-Site "none" falls through to Origin check
// ---------------------------------------------------------------------------

func TestOriginAllowed_SecFetchNoneFallsThrough(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"none with matching origin", "https://example.com", true},
		{"none with cross origin", "https://evil.com", false},
		{"none with no origin", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/login", nil)
			r.Header.Set("Sec-Fetch-Site", "none")
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			got := originAllowed(r, "https://example.com")
			require.Equal(t, tt.want, got)
		})
	}
}
