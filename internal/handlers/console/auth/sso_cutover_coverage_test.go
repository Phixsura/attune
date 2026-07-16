// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
	"github.com/Phixsura/attune/internal/service/authmode"
)

type ssoCutoverPreflightRunner struct{}

func (ssoCutoverPreflightRunner) RunChecks(context.Context, *preflight.Environment, []string) preflight.Report {
	return preflight.Report{Status: preflight.StatusPass}
}

func TestConvertPreflightReport(t *testing.T) {
	t.Parallel()

	report := preflight.Report{
		Status: preflight.StatusWarn,
		Checks: []preflight.Result{
			{
				Name:        "sso:oidc_enabled",
				Status:      preflight.StatusPass,
				Message:     "OIDC configured",
				Remediation: "",
			},
			{
				Name:        "sso:redirect_uri_match",
				Status:      preflight.StatusFail,
				Message:     "redirect mismatch",
				Remediation: "Update the redirect URI",
			},
		},
	}

	got := convertPreflightReport(report)
	require.NotNil(t, got)
	require.Equal(t, "warn", got.Status)
	require.Len(t, got.Checks, 2)
	require.Equal(t, "sso:oidc_enabled", got.Checks[0].Name)
	require.Equal(t, "pass", got.Checks[0].Status)
	require.Equal(t, "OIDC configured", got.Checks[0].Message)
	require.Equal(t, "sso:redirect_uri_match", got.Checks[1].Name)
	require.Equal(t, "fail", got.Checks[1].Status)
	require.Equal(t, "redirect mismatch", got.Checks[1].Message)
	require.Equal(t, "Update the redirect URI", got.Checks[1].Remediation)
}

func TestSSOCutoverHandlerDefaultsWithoutService(t *testing.T) {
	t.Parallel()

	var nilHandler *SSOCutoverHandler
	require.True(t, nilHandler.IsLocalLoginAllowedCtx(context.Background(), "tenant-1"))
	require.Equal(t, domain.AuthModeHybrid, nilHandler.GetAuthModeForTenantCtx(context.Background(), "tenant-1"))

	handler := ptrext.Of(SSOCutoverHandler{})
	require.True(t, handler.IsLocalLoginAllowedCtx(context.Background(), "tenant-1"))
	require.Equal(t, domain.AuthModeHybrid, handler.GetAuthModeForTenantCtx(context.Background(), "tenant-1"))
}

func TestNewSSOCutoverHandler_NilService(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewSSOCutoverHandler(nil))
}

func TestNewSSOCutoverHandler_Service(t *testing.T) {
	t.Parallel()

	require.NotNil(t, NewSSOCutoverHandler(newUnreachableAuthModeService(t)))
}

func TestSSOCutoverHandlerHTTPErrorPaths(t *testing.T) {
	t.Parallel()

	handler := NewSSOCutoverHandler(newUnreachableAuthModeService(t))

	t.Run("get auth mode database error", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()

		handler.GetAuthMode(w, authedSSOCutoverRequest(http.MethodGet, "/fb/v1/console/auth/mode", ""))

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("cutover rejects bad json", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()

		handler.Cutover(w, authedSSOCutoverRequest(http.MethodPost, "/fb/v1/console/auth/cutover", `{"skip_breakglass_check":`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("cutover database error", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()

		handler.Cutover(w, authedSSOCutoverRequest(http.MethodPost, "/fb/v1/console/auth/cutover", `{}`))

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("fallback database error", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()

		handler.Fallback(w, authedSSOCutoverRequest(http.MethodPost, "/fb/v1/console/auth/fallback", ""))

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestSSOCutoverHandlerHelpersFailOpenOnServiceError(t *testing.T) {
	t.Parallel()

	handler := NewSSOCutoverHandler(newUnreachableAuthModeService(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.True(t, handler.IsLocalLoginAllowedCtx(ctx, "tenant-1"))
	require.Equal(t, domain.AuthModeHybrid, handler.GetAuthModeForTenantCtx(ctx, "tenant-1"))
}

func authedSSOCutoverRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	return req.WithContext(session.WithAuthCtx(ctx, ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "admin",
	})))
}

func newUnreachableAuthModeService(t *testing.T) *authmode.Service {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	require.NoError(t, err)
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return authmode.NewService(
		systemsettings.NewRepo(pool),
		ssoCutoverPreflightRunner{},
		ptrext.Of(preflight.Environment{}),
		func(context.Context, string) (int, error) { return 1, nil },
	)
}
