// SPDX-License-Identifier: Apache-2.0

// Package authmode manages the authentication mode (hybrid vs sso_only)
// for a tenant (#158).
package authmode

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
)

// PreflightRunner runs preflight checks.
type PreflightRunner interface {
	RunChecks(ctx context.Context, env *preflight.Environment, names []string) preflight.Report
}

// Service handles auth mode operations.
type Service struct {
	settings          *systemsettings.Repo
	preflightRunner   PreflightRunner
	preflightEnv      *preflight.Environment
	breakglassCounter func(ctx context.Context, tenantID string) (int, error)
}

// NewService creates a new auth mode service.
func NewService(
	settings *systemsettings.Repo,
	preflightRunner PreflightRunner,
	preflightEnv *preflight.Environment,
	breakglassCounter func(ctx context.Context, tenantID string) (int, error),
) *Service {
	return ptrext.Of(Service{
		settings:          settings,
		preflightRunner:   preflightRunner,
		preflightEnv:      preflightEnv,
		breakglassCounter: breakglassCounter,
	})
}

// GetMode returns the current auth mode for a tenant.
func (s *Service) GetMode(ctx context.Context, tenantID string) (domain.AuthMode, error) {
	return s.settings.GetAuthMode(ctx, tenantID)
}

// CutoverInput is the input for SSO cutover.
type CutoverInput struct {
	TenantID            string
	SkipBreakglassCheck bool
	UpdatedBy           string
}

// CutoverResult is the result of SSO cutover.
type CutoverResult struct {
	Success   bool
	Preflight preflight.Report
	Message   string
}

// ssoCutoverChecks are the preflight checks required for SSO cutover.
var ssoCutoverChecks = []string{
	"sso:oidc_enabled",
	"sso:issuer_reachable",
	"sso:redirect_uri_match",
}

// Cutover switches from hybrid to sso_only mode after preflight checks pass.
func (s *Service) Cutover(ctx context.Context, input CutoverInput) (CutoverResult, error) {
	const where = "authmode.Service.Cutover"

	current, err := s.settings.GetAuthMode(ctx, input.TenantID)
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
		logext.Warnf(ctx, "[%s] cutover blocked by preflight,tenant:%s", where, input.TenantID)
		return CutoverResult{
			Success:   false,
			Preflight: report,
			Message:   "SSO cutover blocked by preflight checks",
		}, nil
	}

	if err := s.settings.SetAuthMode(ctx, input.TenantID, domain.AuthModeSSOnly, input.UpdatedBy); err != nil {
		return CutoverResult{}, fmt.Errorf("failed to set auth mode: %w", err)
	}

	logext.Infof(ctx, "[%s] cutover complete,tenant:%s,updated_by:%s", where, input.TenantID, input.UpdatedBy)
	return CutoverResult{
		Success:   true,
		Preflight: report,
		Message:   "SSO cutover complete",
	}, nil
}

// Fallback switches from sso_only back to hybrid mode.
func (s *Service) Fallback(ctx context.Context, tenantID, updatedBy string) error {
	const where = "authmode.Service.Fallback"

	if err := s.settings.SetAuthMode(ctx, tenantID, domain.AuthModeHybrid, updatedBy); err != nil {
		return err
	}

	logext.Infof(ctx, "[%s] fallback to hybrid,tenant:%s,updated_by:%s", where, tenantID, updatedBy)
	return nil
}

// checkBreakglassReady checks if at least one valid break-glass token exists.
func (s *Service) checkBreakglassReady(ctx context.Context, tenantID string) preflight.Result {
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

// IsLocalLoginAllowed returns true if local admin login is allowed.
func (s *Service) IsLocalLoginAllowed(ctx context.Context, tenantID string) (bool, error) {
	mode, err := s.settings.GetAuthMode(ctx, tenantID)
	if err != nil {
		return true, err
	}
	return mode == domain.AuthModeHybrid, nil
}
