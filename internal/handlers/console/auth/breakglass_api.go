// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/auditlog"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
	"github.com/Phixsura/attune/internal/service/securityalert"
)

// BreakGlassAPIHandler handles break-glass token management via Console API.
type BreakGlassAPIHandler struct {
	svc      *breakglasssvc.Service
	auditSvc *auditlog.Service
	alertSvc *securityalert.Service
}

// NewBreakGlassAPIHandler creates a handler for break-glass token management.
func NewBreakGlassAPIHandler(svc *breakglasssvc.Service, auditSvc *auditlog.Service, alertSvc *securityalert.Service) *BreakGlassAPIHandler {
	return ptrext.Of(BreakGlassAPIHandler{svc: svc, auditSvc: auditSvc, alertSvc: alertSvc})
}

// ListTokensResponse is the response for listing break-glass tokens.
type ListTokensResponse struct {
	Tokens []TokenInfo `json:"tokens"`
}

// TokenInfo is the public representation of a break-glass token.
type TokenInfo struct {
	ID         string   `json:"id"`
	AdminEmail string   `json:"admin_email"`
	ExpiresAt  string   `json:"expires_at"`
	UsedAt     *string  `json:"used_at,omitempty"`
	UsedFromIP *string  `json:"used_from_ip,omitempty"`
	IssuedBy   string   `json:"issued_by"`
	IssuedAt   string   `json:"issued_at"`
	RevokedAt  *string  `json:"revoked_at,omitempty"`
	RevokedBy  *string  `json:"revoked_by,omitempty"`
	AllowedIPs []string `json:"allowed_ips,omitempty"`
	Status     string   `json:"status"`
}

// List returns all break-glass tokens for the current tenant.
func (h *BreakGlassAPIHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	auth := session.FromContext(ctx)
	if auth == nil {
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
		return
	}

	tokens, err := h.svc.List(ctx, auth.TenantID)
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list tokens")
		return
	}

	now := time.Now()
	resp := ListTokensResponse{Tokens: make([]TokenInfo, 0, len(tokens))}
	for _, t := range tokens {
		info := TokenInfo{
			ID:         t.ID,
			AdminEmail: t.AdminEmail,
			ExpiresAt:  t.ExpiresAt.Format(time.RFC3339),
			IssuedBy:   t.IssuedBy,
			IssuedAt:   t.IssuedAt.Format(time.RFC3339),
			AllowedIPs: t.AllowedIPs,
			UsedFromIP: t.UsedFromIP,
		}
		if t.UsedAt != nil {
			s := t.UsedAt.Format(time.RFC3339)
			info.UsedAt = ptrext.Of(s)
			info.Status = "used"
		} else if t.RevokedAt != nil {
			s := t.RevokedAt.Format(time.RFC3339)
			info.RevokedAt = ptrext.Of(s)
			info.RevokedBy = t.RevokedBy
			info.Status = "revoked"
		} else if t.IsExpired(now) {
			info.Status = "expired"
		} else {
			info.Status = "valid"
		}
		resp.Tokens = append(resp.Tokens, info)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// IssueRequest is the request to issue a new break-glass token.
type IssueRequest struct {
	AdminEmail string   `json:"admin_email"`
	TTLMinutes int      `json:"ttl_minutes"`
	AllowedIPs []string `json:"allowed_ips,omitempty"` // CIDR ranges; empty = any IP
}

// IssueResponse is the response after issuing a break-glass token.
type IssueResponse struct {
	Token     TokenInfo `json:"token"`
	RawToken  string    `json:"raw_token"`
	ExpiresAt string    `json:"expires_at"`
}

// Issue creates a new break-glass token.
func (h *BreakGlassAPIHandler) Issue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	auth := session.FromContext(ctx)
	if auth == nil {
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
		return
	}

	var req IssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid request body")
		return
	}

	if req.AdminEmail == "" {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "admin_email is required")
		return
	}
	if req.TTLMinutes <= 0 {
		req.TTLMinutes = 30
	}

	// Validate TTL bounds (5 min to 24 hours)
	const minTTL, maxTTL = 5, 1440
	if req.TTLMinutes < minTTL {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "ttl_minutes must be at least 5")
		return
	}
	if req.TTLMinutes > maxTTL {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "ttl_minutes must not exceed 1440 (24 hours)")
		return
	}

	ttl := time.Duration(req.TTLMinutes) * time.Minute
	result, err := h.svc.Issue(ctx, breakglasssvc.IssueInput{
		TenantID:   auth.TenantID,
		AdminEmail: req.AdminEmail,
		TTL:        ttl,
		IssuedBy:   auth.UserID,
		AllowedIPs: req.AllowedIPs,
	})
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to issue token")
		return
	}

	resp := IssueResponse{
		Token: TokenInfo{
			ID:         result.Token.ID,
			AdminEmail: result.Token.AdminEmail,
			ExpiresAt:  result.ExpiresAt.Format(time.RFC3339),
			IssuedBy:   result.Token.IssuedBy,
			IssuedAt:   result.Token.IssuedAt.Format(time.RFC3339),
			AllowedIPs: result.Token.AllowedIPs,
			Status:     "valid",
		},
		RawToken:  result.RawToken,
		ExpiresAt: result.ExpiresAt.Format(time.RFC3339),
	}

	auditAfter := map[string]any{
		"admin_email": req.AdminEmail,
		"expires_at":  result.ExpiresAt.Format(time.RFC3339),
	}
	if len(req.AllowedIPs) > 0 {
		auditAfter["allowed_ips"] = req.AllowedIPs
	}
	if h.auditSvc != nil {
		_ = h.auditSvc.Record(ctx, auditlog.Event{
			TenantID:   auth.TenantID,
			Actor:      auditlog.Actor{Type: "admin", ID: auth.UserID, Email: req.AdminEmail},
			Action:     "breakglass.issue",
			TargetType: "breakglass_token",
			TargetID:   result.Token.ID,
			Summary:    "Break-glass token issued for " + req.AdminEmail,
			After:      auditAfter,
		})
	}

	// Send security alert for token issuance.
	if h.alertSvc != nil {
		h.alertSvc.Send(ctx, securityalert.BreakGlassIssuedAlert(
			auth.TenantID, auth.UserID, req.AdminEmail, result.Token.ID, ttl,
		))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// Revoke revokes a break-glass token.
func (h *BreakGlassAPIHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	auth := session.FromContext(ctx)
	if auth == nil {
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
		return
	}

	tokenID := chi.URLParam(r, "id")
	if tokenID == "" {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "token id is required")
		return
	}

	if err := h.svc.Revoke(ctx, auth.TenantID, tokenID, auth.UserID); err != nil {
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to revoke token")
		return
	}

	if h.auditSvc != nil {
		_ = h.auditSvc.Record(ctx, auditlog.Event{
			TenantID:   auth.TenantID,
			Actor:      auditlog.Actor{Type: "admin", ID: auth.UserID},
			Action:     "breakglass.revoke",
			TargetType: "breakglass_token",
			TargetID:   tokenID,
			Summary:    "Break-glass token revoked",
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// RevokeAllResponse is the response for revoking all tokens.
type RevokeAllResponse struct {
	Revoked int64 `json:"revoked"`
}

// RevokeAll revokes all valid break-glass tokens for the tenant.
func (h *BreakGlassAPIHandler) RevokeAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	auth := session.FromContext(ctx)
	if auth == nil {
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
		return
	}

	n, err := h.svc.RevokeAll(ctx, auth.TenantID, auth.UserID)
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to revoke tokens")
		return
	}

	if h.auditSvc != nil && n > 0 {
		_ = h.auditSvc.Record(ctx, auditlog.Event{
			TenantID:   auth.TenantID,
			Actor:      auditlog.Actor{Type: "admin", ID: auth.UserID},
			Action:     "breakglass.revoke",
			TargetType: "breakglass_token",
			TargetID:   "*",
			Summary:    fmt.Sprintf("All break-glass tokens revoked (count: %d)", n),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RevokeAllResponse{Revoked: n})
}

// LockoutInfo represents a locked IP's status.
type LockoutInfo struct {
	IP            string `json:"ip"`
	LockedUntil   string `json:"locked_until"`
	RemainingMins int    `json:"remaining_mins"`
	Attempts      int    `json:"attempts"`
}

// ListLockoutsResponse is the response for listing locked IPs.
type ListLockoutsResponse struct {
	Lockouts []LockoutInfo `json:"lockouts"`
}

// ListLockouts returns all currently locked IPs.
func (h *BreakGlassAPIHandler) ListLockouts(w http.ResponseWriter, r *http.Request, lockout *breakglasssvc.LockoutTracker) {
	ctx := r.Context()
	auth := session.FromContext(ctx)
	if auth == nil {
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
		return
	}

	if lockout == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ListLockoutsResponse{Lockouts: []LockoutInfo{}})
		return
	}

	locked := lockout.ListLocked()
	resp := ListLockoutsResponse{Lockouts: make([]LockoutInfo, 0, len(locked))}

	for ip, status := range locked {
		resp.Lockouts = append(resp.Lockouts, LockoutInfo{
			IP:            ip,
			LockedUntil:   status.LockedUntil.Format(time.RFC3339),
			RemainingMins: int(status.RemainingTime.Minutes()) + 1,
			Attempts:      status.Attempts,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// UnlockIP manually unlocks a locked IP.
func (h *BreakGlassAPIHandler) UnlockIP(w http.ResponseWriter, r *http.Request, lockout *breakglasssvc.LockoutTracker) {
	ctx := r.Context()
	auth := session.FromContext(ctx)
	if auth == nil {
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
		return
	}

	ip := chi.URLParam(r, "ip")
	if ip == "" {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "ip is required")
		return
	}

	if lockout == nil {
		dispatcher.Reject(ctx, w, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "lockout not configured")
		return
	}

	lockout.Unlock(ip)

	if h.auditSvc != nil {
		_ = h.auditSvc.Record(ctx, auditlog.Event{
			TenantID:   auth.TenantID,
			Actor:      auditlog.Actor{Type: "admin", ID: auth.UserID},
			Action:     "breakglass.unlock_ip",
			TargetType: "ip",
			TargetID:   ip,
			Summary:    fmt.Sprintf("IP %s unlocked for break-glass access", ip),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
