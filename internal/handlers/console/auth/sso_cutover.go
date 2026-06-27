// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/authmode"
)

// SSOCutoverHandler handles SSO cutover and fallback operations.
type SSOCutoverHandler struct {
	svc *authmode.Service
}

// NewSSOCutoverHandler creates a new SSO cutover handler.
func NewSSOCutoverHandler(svc *authmode.Service) *SSOCutoverHandler {
	if svc == nil {
		return nil
	}
	return ptrext.Of(SSOCutoverHandler{svc: svc})
}

// GetAuthModeResponse is the response for getting auth mode.
type GetAuthModeResponse struct {
	Mode string `json:"mode"`
}

// GetAuthMode returns the current auth mode.
func (h *SSOCutoverHandler) GetAuthMode(w http.ResponseWriter, r *http.Request) {
	const where = "auth.SSOCutoverHandler.GetAuthMode"
	ctx := r.Context()
	auth := session.FromContext(ctx)

	mode, err := h.svc.GetMode(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] failed to get auth mode,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get auth mode")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(GetAuthModeResponse{Mode: string(mode)})
}

// CutoverRequest is the request for SSO cutover.
type CutoverRequest struct {
	SkipBreakglassCheck bool `json:"skip_breakglass_check"`
}

// CutoverResponse is the response for SSO cutover.
type CutoverResponse struct {
	Success   bool                     `json:"success"`
	Message   string                   `json:"message"`
	Preflight *PreflightReportResponse `json:"preflight,omitempty"`
}

// PreflightReportResponse is the preflight report in the response.
type PreflightReportResponse struct {
	Status string                   `json:"status"`
	Checks []PreflightCheckResponse `json:"checks"`
}

// PreflightCheckResponse is a single preflight check in the response.
type PreflightCheckResponse struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// Cutover switches to SSO-only mode.
func (h *SSOCutoverHandler) Cutover(w http.ResponseWriter, r *http.Request) {
	const where = "auth.SSOCutoverHandler.Cutover"
	ctx := r.Context()
	auth := session.FromContext(ctx)

	var req CutoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid request body")
		return
	}

	result, err := h.svc.Cutover(ctx, authmode.CutoverInput{
		TenantID:            auth.TenantID,
		SkipBreakglassCheck: req.SkipBreakglassCheck,
		UpdatedBy:           auth.UserID,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] cutover failed,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "cutover failed")
		return
	}

	resp := CutoverResponse{
		Success: result.Success,
		Message: result.Message,
	}

	if len(result.Preflight.Checks) > 0 {
		resp.Preflight = convertPreflightReport(result.Preflight)
	}

	w.Header().Set("Content-Type", "application/json")
	if !result.Success {
		w.WriteHeader(http.StatusPreconditionFailed)
	}
	_ = json.NewEncoder(w).Encode(resp)

	if result.Success {
		logext.Infof(ctx, "[%s] cutover success,tenant:%s", where, auth.TenantID)
	}
}

// FallbackResponse is the response for SSO fallback.
type FallbackResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Fallback switches back to hybrid mode.
func (h *SSOCutoverHandler) Fallback(w http.ResponseWriter, r *http.Request) {
	const where = "auth.SSOCutoverHandler.Fallback"
	ctx := r.Context()
	auth := session.FromContext(ctx)

	if err := h.svc.Fallback(ctx, auth.TenantID, auth.UserID); err != nil {
		logext.Errorf(ctx, "[%s] fallback failed,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "fallback failed")
		return
	}

	logext.Infof(ctx, "[%s] fallback success,tenant:%s", where, auth.TenantID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(FallbackResponse{
		Success: true,
		Message: "Fallback to hybrid mode complete",
	})
}

// IsLocalLoginAllowedCtx returns whether local login is allowed for the tenant.
func (h *SSOCutoverHandler) IsLocalLoginAllowedCtx(ctx context.Context, tenantID string) bool {
	if h == nil || h.svc == nil {
		return true
	}
	allowed, err := h.svc.IsLocalLoginAllowed(ctx, tenantID)
	if err != nil {
		return true
	}
	return allowed
}

// GetAuthModeForTenantCtx returns the auth mode for a specific tenant (used by auth providers).
func (h *SSOCutoverHandler) GetAuthModeForTenantCtx(ctx context.Context, tenantID string) domain.AuthMode {
	if h == nil || h.svc == nil {
		return domain.AuthModeHybrid
	}
	mode, err := h.svc.GetMode(ctx, tenantID)
	if err != nil {
		return domain.AuthModeHybrid
	}
	return mode
}

func convertPreflightReport(report preflight.Report) *PreflightReportResponse {
	checks := make([]PreflightCheckResponse, 0, len(report.Checks))
	for _, c := range report.Checks {
		checks = append(checks, PreflightCheckResponse{
			Name:        c.Name,
			Status:      string(c.Status),
			Message:     c.Message,
			Remediation: c.Remediation,
		})
	}
	return ptrext.Of(PreflightReportResponse{
		Status: string(report.Status),
		Checks: checks,
	})
}
