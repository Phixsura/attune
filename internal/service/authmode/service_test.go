// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package authmode

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
)

// --- mock implementations ---

type mockPreflightRunner struct {
	runChecksFn func(ctx context.Context, env *preflight.Environment, names []string) preflight.Report
}

func (m *mockPreflightRunner) RunChecks(ctx context.Context, env *preflight.Environment, names []string) preflight.Report {
	if m.runChecksFn != nil {
		return m.runChecksFn(ctx, env, names)
	}
	return preflight.Report{Status: preflight.StatusPass}
}

// newTestService creates a Service with mock-able fields.
// Since Service.settings is *systemsettings.Repo (concrete), we create
// a test wrapper that mirrors the Service logic but uses interfaces.
func newTestService(
	getMode func(ctx context.Context, tenantID string) (domain.AuthMode, error),
	setMode func(ctx context.Context, tenantID string, mode domain.AuthMode, updatedBy string) error,
	runner PreflightRunner,
	counter func(ctx context.Context, tenantID string) (int, error),
) *testService {
	return ptrext.Of(testService{
		getModeFn:         getMode,
		setModeFn:         setMode,
		preflightRunner:   runner,
		preflightEnv:      ptrext.Of(preflight.Environment{}),
		breakglassCounter: counter,
	})
}

// testService mirrors Service for unit testing with mocked dependencies.
type testService struct {
	getModeFn         func(ctx context.Context, tenantID string) (domain.AuthMode, error)
	setModeFn         func(ctx context.Context, tenantID string, mode domain.AuthMode, updatedBy string) error
	preflightRunner   PreflightRunner
	preflightEnv      *preflight.Environment
	breakglassCounter func(ctx context.Context, tenantID string) (int, error)
}

func (s *testService) GetMode(ctx context.Context, tenantID string) (domain.AuthMode, error) {
	return s.getModeFn(ctx, tenantID)
}

func (s *testService) Cutover(ctx context.Context, input CutoverInput) (CutoverResult, error) {
	current, err := s.getModeFn(ctx, input.TenantID)
	if err != nil {
		return CutoverResult{}, err
	}
	if current == domain.AuthModeSSOnly {
		return CutoverResult{
			Success: true,
			Message: "Already in sso_only mode",
		}, nil
	}

	report := s.preflightRunner.RunChecks(ctx, s.preflightEnv, ssoCutoverChecks)

	if !input.SkipBreakglassCheck {
		bgCheck := s.checkBreakglassReady(ctx, input.TenantID)
		report.Checks = append(report.Checks, bgCheck)
		if bgCheck.Status == preflight.StatusFail {
			report.Status = preflight.StatusFail
		}
	}

	if report.Status == preflight.StatusFail {
		return CutoverResult{
			Success:   false,
			Preflight: report,
			Message:   "SSO cutover blocked by preflight checks",
		}, nil
	}

	if err := s.setModeFn(ctx, input.TenantID, domain.AuthModeSSOnly, input.UpdatedBy); err != nil {
		return CutoverResult{}, fmt.Errorf("failed to set auth mode: %w", err)
	}

	return CutoverResult{
		Success:   true,
		Preflight: report,
		Message:   "SSO cutover complete",
	}, nil
}

func (s *testService) Fallback(ctx context.Context, tenantID, updatedBy string) error {
	return s.setModeFn(ctx, tenantID, domain.AuthModeHybrid, updatedBy)
}

func (s *testService) IsLocalLoginAllowed(ctx context.Context, tenantID string) (bool, error) {
	mode, err := s.getModeFn(ctx, tenantID)
	if err != nil {
		return true, err
	}
	return mode == domain.AuthModeHybrid, nil
}

func (s *testService) checkBreakglassReady(ctx context.Context, tenantID string) preflight.Result {
	r := preflight.Result{
		Name:     "sso:breakglass_ready",
		Category: preflight.CategoryAuth,
	}

	if s.breakglassCounter == nil {
		r.Status = preflight.StatusSkipped
		r.Message = "Break-glass counter not configured"
		return r
	}

	count, err := s.breakglassCounter(ctx, tenantID)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("Failed to count break-glass tokens: %v", err)
		return r
	}

	if count == 0 {
		r.Status = preflight.StatusFail
		r.Message = "No valid break-glass tokens exist"
		r.Remediation = "Issue a break-glass token before SSO cutover, or use skip_breakglass_check to bypass."
		return r
	}

	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("%d valid break-glass token(s) available", count)
	return r
}

// --- GetMode tests ---

func TestGetMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     domain.AuthMode
		err      error
		wantMode domain.AuthMode
		wantErr  bool
	}{
		{"hybrid mode", domain.AuthModeHybrid, nil, domain.AuthModeHybrid, false},
		{"sso_only mode", domain.AuthModeSSOnly, nil, domain.AuthModeSSOnly, false},
		{"repo error", "", errors.New("db error"), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestService(
				func(_ context.Context, _ string) (domain.AuthMode, error) {
					return tt.mode, tt.err
				},
				nil, nil, nil,
			)

			mode, err := svc.GetMode(t.Context(), "tenant-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMode, mode)
		})
	}
}

// --- Cutover tests ---

func TestCutover_AlreadyInSSOOnlyMode(t *testing.T) {
	t.Parallel()

	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeSSOnly, nil
		},
		nil, nil, nil,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "Already in sso_only mode", result.Message)
}

func TestCutover_GetModeError(t *testing.T) {
	t.Parallel()

	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return "", errors.New("db connection failed")
		},
		nil, nil, nil,
	)

	_, err := svc.Cutover(t.Context(), CutoverInput{TenantID: "tenant-1"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "db connection failed")
}

func TestCutover_PreflightFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		failedName string
		message    string
	}{
		{"oidc not enabled", "sso:oidc_enabled", "OIDC not configured"},
		{"issuer unreachable", "sso:issuer_reachable", "Cannot reach issuer"},
		{"redirect uri mismatch", "sso:redirect_uri_match", "Redirect URI mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := ptrext.Of(mockPreflightRunner{
				runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
					return preflight.Report{
						Status: preflight.StatusFail,
						Checks: []preflight.Result{{
							Name:    tt.failedName,
							Status:  preflight.StatusFail,
							Message: tt.message,
						}},
					}
				},
			})
			svc := newTestService(
				func(_ context.Context, _ string) (domain.AuthMode, error) {
					return domain.AuthModeHybrid, nil
				},
				nil, runner, nil,
			)

			result, err := svc.Cutover(t.Context(), CutoverInput{
				TenantID:  "tenant-1",
				UpdatedBy: "admin",
			})

			require.NoError(t, err)
			require.False(t, result.Success)
			require.Equal(t, "SSO cutover blocked by preflight checks", result.Message)
			require.Equal(t, preflight.StatusFail, result.Preflight.Status)
		})
	}
}

func TestCutover_BreakglassCheckFails(t *testing.T) {
	t.Parallel()

	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	counter := func(_ context.Context, _ string) (int, error) {
		return 0, nil // zero break-glass tokens
	}
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		nil, runner, counter,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin",
	})

	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Message, "blocked by preflight")

	// Verify breakglass check is appended
	var foundBGCheck bool
	for _, c := range result.Preflight.Checks {
		if c.Name == "sso:breakglass_ready" {
			foundBGCheck = true
			require.Equal(t, preflight.StatusFail, c.Status)
			require.Contains(t, c.Message, "No valid break-glass tokens")
		}
	}
	require.True(t, foundBGCheck, "breakglass check should be in results")
}

func TestCutover_BreakglassCounterError(t *testing.T) {
	t.Parallel()

	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	counter := func(_ context.Context, _ string) (int, error) {
		return 0, errors.New("token store unavailable")
	}
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		nil, runner, counter,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin",
	})

	require.NoError(t, err)
	require.False(t, result.Success)

	var foundBGCheck bool
	for _, c := range result.Preflight.Checks {
		if c.Name == "sso:breakglass_ready" {
			foundBGCheck = true
			require.Equal(t, preflight.StatusFail, c.Status)
			require.Contains(t, c.Message, "Failed to count break-glass tokens")
		}
	}
	require.True(t, foundBGCheck, "breakglass check should be in results")
}

func TestCutover_SkipBreakglassCheck(t *testing.T) {
	t.Parallel()

	var setCalled bool
	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	counter := func(_ context.Context, _ string) (int, error) {
		return 0, nil // zero tokens, would normally fail
	}
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		func(_ context.Context, _ string, mode domain.AuthMode, _ string) error {
			setCalled = true
			require.Equal(t, domain.AuthModeSSOnly, mode)
			return nil
		},
		runner, counter,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:            "tenant-1",
		SkipBreakglassCheck: true,
		UpdatedBy:           "admin",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, setCalled)
	require.Equal(t, "SSO cutover complete", result.Message)

	// Breakglass check should not be in results
	for _, c := range result.Preflight.Checks {
		require.NotEqual(t, "sso:breakglass_ready", c.Name)
	}
}

func TestCutover_Success(t *testing.T) {
	t.Parallel()

	var captured struct {
		tenantID  string
		mode      domain.AuthMode
		updatedBy string
	}
	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, names []string) preflight.Report {
			require.Contains(t, names, "sso:oidc_enabled")
			require.Contains(t, names, "sso:issuer_reachable")
			require.Contains(t, names, "sso:redirect_uri_match")
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	counter := func(_ context.Context, tenantID string) (int, error) {
		require.Equal(t, "tenant-1", tenantID)
		return 2, nil // two valid tokens
	}
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		func(_ context.Context, tenantID string, mode domain.AuthMode, updatedBy string) error {
			captured.tenantID = tenantID
			captured.mode = mode
			captured.updatedBy = updatedBy
			return nil
		},
		runner, counter,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin@example.com",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "SSO cutover complete", result.Message)

	require.Equal(t, "tenant-1", captured.tenantID)
	require.Equal(t, domain.AuthModeSSOnly, captured.mode)
	require.Equal(t, "admin@example.com", captured.updatedBy)

	// Verify breakglass check passed
	var foundBGCheck bool
	for _, c := range result.Preflight.Checks {
		if c.Name == "sso:breakglass_ready" {
			foundBGCheck = true
			require.Equal(t, preflight.StatusPass, c.Status)
			require.Contains(t, c.Message, "2 valid break-glass token(s)")
		}
	}
	require.True(t, foundBGCheck, "breakglass check should be in results")
}

func TestCutover_SetAuthModeError(t *testing.T) {
	t.Parallel()

	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	counter := func(_ context.Context, _ string) (int, error) {
		return 1, nil
	}
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		func(_ context.Context, _ string, _ domain.AuthMode, _ string) error {
			return errors.New("write failed")
		},
		runner, counter,
	)

	_, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to set auth mode")
	require.Contains(t, err.Error(), "write failed")
}

func TestCutover_NilBreakglassCounter(t *testing.T) {
	t.Parallel()

	var setCalled bool
	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	// nil counter
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		func(_ context.Context, _ string, _ domain.AuthMode, _ string) error {
			setCalled = true
			return nil
		},
		runner, nil,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, setCalled)

	// Breakglass check should be skipped
	var foundBGCheck bool
	for _, c := range result.Preflight.Checks {
		if c.Name == "sso:breakglass_ready" {
			foundBGCheck = true
			require.Equal(t, preflight.StatusSkipped, c.Status)
			require.Contains(t, c.Message, "not configured")
		}
	}
	require.True(t, foundBGCheck, "breakglass check should be in results as skipped")
}

// --- Fallback tests ---

func TestFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setErr   error
		wantErr  bool
		wantMode domain.AuthMode
		wantBy   string
	}{
		{"success", nil, false, domain.AuthModeHybrid, "admin@example.com"},
		{"repo error", errors.New("db error"), true, domain.AuthModeHybrid, "admin@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var captured struct {
				tenantID  string
				mode      domain.AuthMode
				updatedBy string
			}
			svc := newTestService(
				nil,
				func(_ context.Context, tenantID string, mode domain.AuthMode, updatedBy string) error {
					captured.tenantID = tenantID
					captured.mode = mode
					captured.updatedBy = updatedBy
					return tt.setErr
				},
				nil, nil,
			)

			err := svc.Fallback(t.Context(), "tenant-1", "admin@example.com")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "tenant-1", captured.tenantID)
			require.Equal(t, tt.wantMode, captured.mode)
			require.Equal(t, tt.wantBy, captured.updatedBy)
		})
	}
}

// --- IsLocalLoginAllowed tests ---

func TestIsLocalLoginAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    domain.AuthMode
		err     error
		want    bool
		wantErr bool
	}{
		{"hybrid allows local login", domain.AuthModeHybrid, nil, true, false},
		{"sso_only denies local login", domain.AuthModeSSOnly, nil, false, false},
		{"repo error returns true (fail-open)", "", errors.New("db error"), true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestService(
				func(_ context.Context, _ string) (domain.AuthMode, error) {
					return tt.mode, tt.err
				},
				nil, nil, nil,
			)

			allowed, err := svc.IsLocalLoginAllowed(t.Context(), "tenant-1")

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want, allowed)
		})
	}
}

// --- checkBreakglassReady tests ---

func TestCheckBreakglassReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		counter     func(ctx context.Context, tenantID string) (int, error)
		wantStatus  preflight.Status
		wantMsgPart string
		wantRemed   bool
	}{
		{
			name:        "nil counter",
			counter:     nil,
			wantStatus:  preflight.StatusSkipped,
			wantMsgPart: "not configured",
		},
		{
			name: "counter error",
			counter: func(_ context.Context, _ string) (int, error) {
				return 0, errors.New("connection refused")
			},
			wantStatus:  preflight.StatusFail,
			wantMsgPart: "Failed to count",
		},
		{
			name: "zero tokens",
			counter: func(_ context.Context, _ string) (int, error) {
				return 0, nil
			},
			wantStatus:  preflight.StatusFail,
			wantMsgPart: "No valid break-glass",
			wantRemed:   true,
		},
		{
			name: "one token",
			counter: func(_ context.Context, _ string) (int, error) {
				return 1, nil
			},
			wantStatus:  preflight.StatusPass,
			wantMsgPart: "1 valid break-glass token(s)",
		},
		{
			name: "multiple tokens",
			counter: func(_ context.Context, _ string) (int, error) {
				return 5, nil
			},
			wantStatus:  preflight.StatusPass,
			wantMsgPart: "5 valid break-glass token(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestService(nil, nil, nil, tt.counter)
			result := svc.checkBreakglassReady(t.Context(), "tenant-1")

			require.Equal(t, "sso:breakglass_ready", result.Name)
			require.Equal(t, preflight.CategoryAuth, result.Category)
			require.Equal(t, tt.wantStatus, result.Status)
			require.Contains(t, result.Message, tt.wantMsgPart)
			if tt.wantRemed {
				require.NotEmpty(t, result.Remediation)
			}
		})
	}
}

// --- NewService test ---

func TestNewService(t *testing.T) {
	t.Parallel()

	counter := func(_ context.Context, _ string) (int, error) { return 1, nil }
	runner := ptrext.Of(mockPreflightRunner{})
	env := ptrext.Of(preflight.Environment{})

	// Verify NewService returns a non-nil service with fields wired correctly.
	svc := NewService(nil, runner, env, counter)
	require.NotNil(t, svc)
	require.Equal(t, runner, svc.preflightRunner)
	require.Equal(t, env, svc.preflightEnv)
	require.NotNil(t, svc.breakglassCounter)
}

// --- ssoCutoverChecks test ---

func TestSSOCutoverChecks(t *testing.T) {
	t.Parallel()

	expected := []string{
		"sso:oidc_enabled",
		"sso:issuer_reachable",
		"sso:redirect_uri_match",
	}
	require.Equal(t, expected, ssoCutoverChecks)
}

// --- CutoverInput/CutoverResult tests ---

func TestCutoverInputFields(t *testing.T) {
	t.Parallel()

	input := CutoverInput{
		TenantID:            "tenant-abc",
		SkipBreakglassCheck: true,
		UpdatedBy:           "user@example.com",
	}

	require.Equal(t, "tenant-abc", input.TenantID)
	require.True(t, input.SkipBreakglassCheck)
	require.Equal(t, "user@example.com", input.UpdatedBy)
}

func TestCutoverResultFields(t *testing.T) {
	t.Parallel()

	result := CutoverResult{
		Success: true,
		Preflight: preflight.Report{
			Status: preflight.StatusPass,
			Checks: []preflight.Result{
				{Name: "test", Status: preflight.StatusPass},
			},
		},
		Message: "test message",
	}

	require.True(t, result.Success)
	require.Equal(t, preflight.StatusPass, result.Preflight.Status)
	require.Len(t, result.Preflight.Checks, 1)
	require.Equal(t, "test message", result.Message)
}

// --- edge cases ---

func TestCutover_PreflightWarnStillPasses(t *testing.T) {
	t.Parallel()

	var setCalled bool
	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{
				Status: preflight.StatusWarn, // warn, not fail
				Checks: []preflight.Result{{
					Name:    "sso:oidc_enabled",
					Status:  preflight.StatusWarn,
					Message: "OIDC configured but certificate expires soon",
				}},
			}
		},
	})
	counter := func(_ context.Context, _ string) (int, error) {
		return 1, nil
	}
	svc := newTestService(
		func(_ context.Context, _ string) (domain.AuthMode, error) {
			return domain.AuthModeHybrid, nil
		},
		func(_ context.Context, _ string, _ domain.AuthMode, _ string) error {
			setCalled = true
			return nil
		},
		runner, counter,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "tenant-1",
		UpdatedBy: "admin",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, setCalled)
}

func TestCutover_EmptyTenantID(t *testing.T) {
	t.Parallel()

	var capturedTenantID string
	runner := ptrext.Of(mockPreflightRunner{
		runChecksFn: func(_ context.Context, _ *preflight.Environment, _ []string) preflight.Report {
			return preflight.Report{Status: preflight.StatusPass}
		},
	})
	counter := func(_ context.Context, tenantID string) (int, error) {
		capturedTenantID = tenantID
		return 1, nil
	}
	svc := newTestService(
		func(_ context.Context, tenantID string) (domain.AuthMode, error) {
			capturedTenantID = tenantID
			return domain.AuthModeHybrid, nil
		},
		func(_ context.Context, _ string, _ domain.AuthMode, _ string) error {
			return nil
		},
		runner, counter,
	)

	result, err := svc.Cutover(t.Context(), CutoverInput{
		TenantID:  "",
		UpdatedBy: "admin",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "", capturedTenantID)
}

func TestFallback_EmptyUpdatedBy(t *testing.T) {
	t.Parallel()

	var capturedBy string
	svc := newTestService(
		nil,
		func(_ context.Context, _ string, _ domain.AuthMode, updatedBy string) error {
			capturedBy = updatedBy
			return nil
		},
		nil, nil,
	)

	err := svc.Fallback(t.Context(), "tenant-1", "")
	require.NoError(t, err)
	require.Equal(t, "", capturedBy)
}

func TestGetMode_TenantIDPassedThrough(t *testing.T) {
	t.Parallel()

	var capturedID string
	svc := newTestService(
		func(_ context.Context, tenantID string) (domain.AuthMode, error) {
			capturedID = tenantID
			return domain.AuthModeHybrid, nil
		},
		nil, nil, nil,
	)

	_, err := svc.GetMode(t.Context(), "my-tenant-123")
	require.NoError(t, err)
	require.Equal(t, "my-tenant-123", capturedID)
}

func TestIsLocalLoginAllowed_TenantIDPassedThrough(t *testing.T) {
	t.Parallel()

	var capturedID string
	svc := newTestService(
		func(_ context.Context, tenantID string) (domain.AuthMode, error) {
			capturedID = tenantID
			return domain.AuthModeHybrid, nil
		},
		nil, nil, nil,
	)

	_, err := svc.IsLocalLoginAllowed(t.Context(), "tenant-xyz")
	require.NoError(t, err)
	require.Equal(t, "tenant-xyz", capturedID)
}
