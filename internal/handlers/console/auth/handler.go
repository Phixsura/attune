// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// Handler owns the local-admin login + logout endpoints (#66 Plan T11)
// that replace the deleted external-OAuth flow.
type Handler struct {
	signer  *session.Signer
	admins  *admin.Repo
	tenants *tenant.TenantRepo
	baseURL string
}

// NewHandler wires the dependencies.
func NewHandler(signer *session.Signer, admins *admin.Repo, tenants *tenant.TenantRepo, baseURL string) *Handler {
	return ptrext.Of(Handler{
		signer:  signer,
		admins:  admins,
		tenants: tenants,
		baseURL: strings.TrimRight(baseURL, "/"),
	})
}

// Login handles POST /install/login. The request/response shapes are
// attune.v1.LoginRequest / LoginResponse (proto, per CLAUDE.md §11).
// The response never distinguishes "unknown email" from "wrong password"
// (dummy bcrypt + generic 401); failure counts are tracked on the admin
// row so a real attacker hits the 5-strike / 15-minute lockout, but
// enumeration via timing is not possible.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	const where = "console.auth.Handler.Login"
	ctx := r.Context()

	// Login-CSRF defence (review H6, #66). Login is the one console
	// endpoint that runs without a prior session cookie + CSRF token,
	// so we need an out-of-band origin check. SameSite=Lax stops
	// the browser from *sending* a cookie cross-site but does NOT stop
	// a malicious origin from forcing a victim's browser to set a
	// fresh session cookie ("login fixation").
	if !originAllowed(r, h.baseURL) {
		logext.Warnf(ctx, "[%s] reject: bad origin,origin:%s",
			where, r.Header.Get("Origin"))
		respond.Error(ctx, w, http.StatusForbidden, "forbidden", "cross-site login not allowed")
		return
	}

	req, ok := decodeLoginRequest(ctx, w, r)
	if !ok {
		return
	}

	a, ok := h.authenticate(ctx, w, req) // *LoginRequest — proto messages carry sync.Mutex
	if !ok {
		return
	}

	scopeTenantID := h.resolveAdminScope(ctx, where)
	if err := h.signer.IssueSessionCookie(w, scopeTenantID, a.ID); err != nil {
		logext.Errorf(ctx, "[%s] IssueSessionCookie failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	redirect := "/console/"
	if req.GetRedirectUri() != "" && redirectIsSafe(h.baseURL, req.GetRedirectUri()) {
		redirect = req.GetRedirectUri()
	}

	logext.Infof(ctx, "[%s] OK,admin_id:%s,redirect:%s", where, a.ID, redirect)
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.LoginResponse{Redirect: redirect}))
}

// decodeLoginRequest reads + validates the proto body shape. Returns
// (req, true) on success; on failure writes a 400/413 and returns ok=false.
// Returns a pointer because proto messages carry a sync.Mutex; copying
// by value trips `vet copylocks`.
func decodeLoginRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) (*attunev1.LoginRequest, bool) {
	req := ptrext.Of(attunev1.LoginRequest{})
	if err := respond.Decode(r.Body, req); err != nil {
		if errors.Is(err, respond.ErrBodyTooLarge) {
			respond.Error(ctx, w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 1 MiB")
			return nil, false
		}
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
		return nil, false
	}
	if req.GetEmail() == "" || req.GetPassword() == "" {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "email and password required")
		return nil, false
	}
	return req, true
}

// authenticate resolves the admin row by email and verifies the password.
// On success returns (admin, true). On any failure writes the response
// and returns ok=false. Timing is equalised across "unknown email",
// "locked", and "wrong password" by always running a bcrypt op.
func (h *Handler) authenticate(ctx context.Context, w http.ResponseWriter, req *attunev1.LoginRequest) (admin.Admin, bool) {
	const where = "console.auth.Handler.authenticate"
	a, err := h.admins.GetByEmail(ctx, req.GetEmail())
	switch {
	case errors.Is(err, admin.ErrNotFound):
		// Equalise timing with a dummy bcrypt run; result discarded.
		_ = VerifyOrDummy("", req.GetPassword())
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return admin.Admin{}, false
	case err != nil:
		logext.Errorf(ctx, "[%s] GetByEmail failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "internal error")
		return admin.Admin{}, false
	}

	if a.LockedUntil != nil && a.LockedUntil.After(time.Now()) {
		// Run a dummy bcrypt even when locked so an attacker probing the
		// locked-account oracle can't distinguish "exists + locked" from
		// "wrong password" by response time (review M1, #66).
		_ = VerifyOrDummy("", req.GetPassword())
		respond.Error(ctx, w, http.StatusLocked, "locked", "account locked due to too many failed attempts")
		return admin.Admin{}, false
	}

	if !VerifyOrDummy(a.PasswordHash, req.GetPassword()) {
		if err := h.admins.IncrementFailedAttempts(ctx, a.ID); err != nil {
			logext.Warnf(ctx, "[%s] IncrementFailedAttempts failed,err:%+v", where, err.Error())
		}
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return admin.Admin{}, false
	}

	if err := h.admins.ResetFailedAttempts(ctx, a.ID); err != nil {
		logext.Warnf(ctx, "[%s] ResetFailedAttempts failed,err:%+v", where, err.Error())
	}
	return a, true
}

// resolveAdminScope returns the TEXT id of the lex-first active tenant
// (single-tenant dogfood; #38 brings a tenant switcher) or "" when no
// tenant exists. Tenant-scoped handlers degrade gracefully on "".
func (h *Handler) resolveAdminScope(ctx context.Context, where string) string {
	if h.tenants == nil {
		return ""
	}
	id, err := h.tenants.FirstActiveID(ctx)
	if err == nil {
		return id
	}
	if !errors.Is(err, tenant.ErrTenantNotFound) {
		logext.Warnf(ctx, "[%s] FirstActiveID failed,err:%+v", where, err.Error())
	}
	return ""
}

// originAllowed returns true when the request's Origin (or Referer
// fallback) matches the configured baseURL's origin, or when neither
// header is set (curl / native-app POSTs — these cannot be driven from
// a malicious page). Sec-Fetch-Site=same-origin is treated as a
// definitive same-site signal.
//
// baseURL is the canonical console origin from config
// (ConsoleBaseURL); both http:// and https:// schemes are matched
// because dev / loopback deploys may run plain HTTP behind a TLS
// reverse proxy.
func originAllowed(r *http.Request, baseURL string) bool {
	// Modern browsers send Sec-Fetch-Site since 2020; trust the
	// explicit "same-origin" / "same-site" / "none" labels.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site":
		return true
	case "cross-site":
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		// curl, server-to-server, native apps — these cannot be driven
		// by a malicious page DOM, so an empty Origin is acceptable.
		return true
	}
	if baseURL == "" {
		return true
	}
	return sameOrigin(origin, baseURL)
}

// sameOrigin compares the scheme + host portion of two URLs. A trailing
// path is allowed (Referer typically includes one); the comparison
// ignores everything past the host.
func sameOrigin(got, want string) bool {
	return originHost(got) == originHost(want)
}

func originHost(u string) string {
	// strip scheme
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	// strip path / query / fragment
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return strings.ToLower(u)
}
