// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
	"github.com/Phixsura/attune/internal/service/auditlog"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
	"github.com/Phixsura/attune/internal/service/securityalert"
)

// adminIDByEmailResolver looks up admin ID by email.
type adminIDByEmailResolver interface {
	GetIDByEmail(ctx context.Context, email string) (string, error)
}

// BreakGlassHandler handles break-glass login endpoints.
type BreakGlassHandler struct {
	svc            *breakglasssvc.Service
	signer         *session.Signer
	tenantResolver tenantScopeResolver
	adminLookup    adminIDByEmailResolver
	auditSvc       *auditlog.Service
	alertSvc       *securityalert.Service
	lockout        *breakglasssvc.LockoutTracker
	baseURL        string
}

// NewBreakGlassHandler creates a new break-glass login handler.
func NewBreakGlassHandler(
	svc *breakglasssvc.Service,
	signer *session.Signer,
	tenantResolver tenantScopeResolver,
	adminLookup adminIDByEmailResolver,
	auditSvc *auditlog.Service,
	alertSvc *securityalert.Service,
	lockout *breakglasssvc.LockoutTracker,
	baseURL string,
) *BreakGlassHandler {
	if svc == nil || signer == nil {
		return nil
	}
	if lockout == nil {
		lockout = breakglasssvc.NewLockoutTracker(breakglasssvc.DefaultLockoutConfig())
	}
	return ptrext.Of(BreakGlassHandler{
		svc:            svc,
		signer:         signer,
		tenantResolver: tenantResolver,
		adminLookup:    adminLookup,
		auditSvc:       auditSvc,
		alertSvc:       alertSvc,
		lockout:        lockout,
		baseURL:        strings.TrimRight(baseURL, "/"),
	})
}

// Lockout returns the lockout tracker for admin inspection/management.
func (h *BreakGlassHandler) Lockout() *breakglasssvc.LockoutTracker {
	return h.lockout
}

// Login handles GET /console/breakglass?token=bg_...
// This endpoint validates the break-glass token and issues a session cookie.
func (h *BreakGlassHandler) Login(w http.ResponseWriter, r *http.Request) {
	const where = "auth.BreakGlassHandler.Login"
	ctx := r.Context()

	clientIP := nethardening.ClientIPDefault(r)

	// Check if IP is locked out.
	if status := h.lockout.Check(clientIP); status.Locked {
		logext.Warnf(ctx, "[%s] IP locked out,ip:%s,until:%s,remaining:%s",
			where, clientIP, status.LockedUntil.Format("15:04:05"), status.RemainingTime)
		h.errorPage(w, r, "ip_locked", fmt.Sprintf(
			"Too many failed attempts. Try again in %d minutes.",
			int(status.RemainingTime.Minutes())+1))
		return
	}

	rawToken := r.URL.Query().Get("token")
	if rawToken == "" {
		h.errorPage(w, r, "missing_token", "Token is required")
		return
	}

	if !strings.HasPrefix(rawToken, breakglasssvc.TokenPrefix) {
		h.errorPage(w, r, "invalid_token", "Invalid token format")
		return
	}

	tenantID := h.resolveTenantID(ctx)
	if tenantID == "" {
		logext.Warnf(ctx, "[%s] no tenant available for break-glass login", where)
		h.errorPage(w, r, "no_tenant", "No tenant configured")
		return
	}

	token, err := h.svc.ValidateByToken(ctx, tenantID, rawToken, clientIP)
	if err != nil {
		h.handleValidationError(ctx, w, r, clientIP, err)
		return
	}

	// Clear lockout state on successful login.
	h.lockout.RecordSuccess(clientIP)

	adminID, err := h.adminLookup.GetIDByEmail(ctx, token.AdminEmail)
	if err != nil {
		logext.Errorf(ctx, "[%s] admin lookup failed,email:%s,err:%s", where, token.AdminEmail, err.Error())
		h.errorPage(w, r, "internal_error", "Internal error")
		return
	}

	if err := h.signer.IssueSessionCookieWithType(responseWriterAdapter{w}, tenantID, adminID, "breakglass"); err != nil {
		logext.Errorf(ctx, "[%s] session issue failed,err:%s", where, err.Error())
		h.errorPage(w, r, "internal_error", "Internal error")
		return
	}

	// Record audit event for break-glass login with client IP for security tracking.
	if h.auditSvc != nil {
		_ = h.auditSvc.Record(ctx, auditlog.Event{
			TenantID:   tenantID,
			Actor:      auditlog.Actor{Type: "breakglass", ID: token.ID},
			Action:     "breakglass.use",
			TargetType: "admin",
			TargetID:   adminID,
			Summary:    fmt.Sprintf("Break-glass login: %s from %s", token.AdminEmail, clientIP),
		})
	}

	// Send real-time security alert for break-glass usage.
	if h.alertSvc != nil {
		h.alertSvc.Send(ctx, securityalert.BreakGlassUsedAlert(
			tenantID, token.AdminEmail, token.ID, clientIP, r.UserAgent(),
		))
	}

	logext.Infof(ctx, "[%s] break-glass login success,admin:%s,token_id:%s,ip:%s", where, token.AdminEmail, token.ID, clientIP)
	http.Redirect(w, r, "/console/", http.StatusFound)
}

// GetAuthMode returns the current auth mode for the tenant.
func (h *BreakGlassHandler) GetAuthMode(ctx context.Context, tenantID string, authModeGetter func(context.Context, string) (domain.AuthMode, error)) (domain.AuthMode, error) {
	if authModeGetter == nil {
		return domain.AuthModeHybrid, nil
	}
	return authModeGetter(ctx, tenantID)
}

func (h *BreakGlassHandler) resolveTenantID(ctx context.Context) string {
	if h.tenantResolver == nil {
		return ""
	}
	id, err := h.tenantResolver.FirstActiveID(ctx)
	if err != nil {
		return ""
	}
	return id
}

func (h *BreakGlassHandler) handleValidationError(ctx context.Context, w http.ResponseWriter, r *http.Request, clientIP string, err error) {
	const where = "auth.BreakGlassHandler.handleValidationError"

	// Record failure for lockout tracking (except for already-used tokens).
	if errors.Is(err, breakglass.ErrNotFound) || errors.Is(err, breakglass.ErrIPNotAllowed) {
		if locked := h.lockout.RecordFailure(clientIP); locked {
			logext.Warnf(ctx, "[%s] IP locked after failed attempt,ip:%s", where, clientIP)
		}
	}

	switch {
	case errors.Is(err, breakglass.ErrNotFound):
		logext.Warnf(ctx, "[%s] invalid or expired token,ip:%s", where, clientIP)
		h.errorPage(w, r, "invalid_token", "Invalid or expired token")
	case errors.Is(err, breakglass.ErrAlreadyUsed):
		logext.Warnf(ctx, "[%s] token already used", where)
		h.errorPage(w, r, "token_used", "This token has already been used")
	case errors.Is(err, breakglass.ErrIPNotAllowed):
		logext.Warnf(ctx, "[%s] IP not allowed,ip:%s", where, clientIP)
		h.errorPage(w, r, "ip_not_allowed", "Access denied from this IP address")
	default:
		logext.Errorf(ctx, "[%s] token validation failed,err:%s", where, err.Error())
		h.errorPage(w, r, "internal_error", "Internal error")
	}
}

func (h *BreakGlassHandler) errorPage(w http.ResponseWriter, r *http.Request, code, message string) {
	http.Redirect(w, r, "/console/login/error?code="+code, http.StatusFound)
}

// responseWriterAdapter adapts http.ResponseWriter to session.cookieSink.
type responseWriterAdapter struct {
	w http.ResponseWriter
}

func (a responseWriterAdapter) SetCookie(c *http.Cookie) {
	http.SetCookie(a.w, c)
}
