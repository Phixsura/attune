// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
)

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
