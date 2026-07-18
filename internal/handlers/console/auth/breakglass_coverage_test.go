// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	breakglassrepo "github.com/Phixsura/attune/internal/repo/breakglass"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
)

func TestNewBreakGlassHandlerValidationAndDefaults(t *testing.T) {
	t.Parallel()

	signer := mustBreakGlassSigner(t)
	if got := NewBreakGlassHandler(nil, signer, nil, nil, nil, nil, nil, ""); got != nil {
		t.Fatal("NewBreakGlassHandler(nil service) returned non-nil")
	}
	if got := NewBreakGlassHandler(ptrext.Of(breakglasssvc.Service{}), nil, nil, nil, nil, nil, nil, ""); got != nil {
		t.Fatal("NewBreakGlassHandler(nil signer) returned non-nil")
	}

	handler := NewBreakGlassHandler(
		ptrext.Of(breakglasssvc.Service{}),
		signer,
		ptrext.Of(fakeTenantScopeResolver{id: "tenant-1"}),
		nil,
		nil,
		nil,
		nil,
		"https://console.example///",
	)
	require.NotNil(t, handler)
	require.Equal(t, "https://console.example", handler.baseURL)
	require.NotNil(t, handler.Lockout())
}

func TestBreakGlassHandlerGetAuthMode(t *testing.T) {
	t.Parallel()

	handler := ptrext.Of(BreakGlassHandler{})
	got, err := handler.GetAuthMode(context.Background(), "tenant-1", nil)
	require.NoError(t, err)
	require.Equal(t, domain.AuthModeHybrid, got)

	wantErr := errors.New("auth mode unavailable")
	got, err = handler.GetAuthMode(context.Background(), "tenant-1", func(context.Context, string) (domain.AuthMode, error) {
		return domain.AuthModeSSOnly, wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, domain.AuthModeSSOnly, got)
}

func TestBreakGlassHandlerResolveTenantID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", ptrext.Of(BreakGlassHandler{}).resolveTenantID(context.Background()))
	require.Equal(t, "", ptrext.Of(BreakGlassHandler{
		tenantResolver: ptrext.Of(fakeTenantScopeResolver{err: errors.New("tenant lookup")}),
	}).resolveTenantID(context.Background()))
	require.Equal(t, "tenant-1", ptrext.Of(BreakGlassHandler{
		tenantResolver: ptrext.Of(fakeTenantScopeResolver{id: "tenant-1"}),
	}).resolveTenantID(context.Background()))
}

func TestBreakGlassLoginEarlyErrorRedirects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		handler   *BreakGlassHandler
		query     string
		remoteIP  string
		wantCode  string
		lockFirst bool
	}{
		{name: "missing token", handler: newBreakGlassCoverageHandler("tenant-1"), wantCode: "missing_token"},
		{name: "invalid prefix", handler: newBreakGlassCoverageHandler("tenant-1"), query: "?token=not-bg", wantCode: "invalid_token"},
		{name: "no tenant", handler: newBreakGlassCoverageHandler(""), query: "?token=bg_token", wantCode: "no_tenant"},
		{name: "locked ip", handler: newBreakGlassCoverageHandler("tenant-1"), query: "?token=bg_token", remoteIP: "203.0.113.9", wantCode: "ip_locked", lockFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.lockFirst {
				require.True(t, tc.handler.lockout.RecordFailure(tc.remoteIP))
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/console/breakglass"+tc.query, nil)
			if tc.remoteIP != "" {
				req.RemoteAddr = tc.remoteIP + ":1234"
			}
			tc.handler.Login(rec, req)
			require.Equal(t, http.StatusFound, rec.Code)
			require.Contains(t, rec.Header().Get("Location"), "code="+tc.wantCode)
		})
	}
}

func TestBreakGlassHandleValidationErrorRedirects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "not found", err: breakglassrepo.ErrNotFound, wantCode: "invalid_token"},
		{name: "already used", err: breakglassrepo.ErrAlreadyUsed, wantCode: "token_used"},
		{name: "ip not allowed", err: breakglassrepo.ErrIPNotAllowed, wantCode: "ip_not_allowed"},
		{name: "generic", err: errors.New("validator failed"), wantCode: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := newBreakGlassCoverageHandler("tenant-1")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/console/breakglass", nil)
			handler.handleValidationError(context.Background(), rec, req, "203.0.113.10", tc.err)
			require.Equal(t, http.StatusFound, rec.Code)
			require.Contains(t, rec.Header().Get("Location"), "code="+tc.wantCode)
		})
	}
}

func TestBreakGlassResponseWriterAdapterSetsCookie(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	responseWriterAdapter{w: rec}.SetCookie(ptrext.Of(http.Cookie{Name: "test", Value: "value"}))
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "test", cookies[0].Name)
	require.Equal(t, "value", cookies[0].Value)
}

func newBreakGlassCoverageHandler(tenantID string) *BreakGlassHandler {
	return ptrext.Of(BreakGlassHandler{
		tenantResolver: ptrext.Of(fakeTenantScopeResolver{id: tenantID}),
		lockout: breakglasssvc.NewLockoutTracker(breakglasssvc.LockoutConfig{
			MaxAttempts:         1,
			BaseLockoutDuration: time.Hour,
			MaxLockoutDuration:  time.Hour,
			AttemptWindow:       time.Hour,
		}),
	})
}

func mustBreakGlassSigner(t *testing.T) *session.Signer {
	t.Helper()
	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)
	return signer
}
