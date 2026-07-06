// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
)

// Handler owns the local-admin login + logout endpoints that replace the
// deleted external-OAuth flow.
type Handler struct {
	signer  *session.Signer
	admins  adminStore
	tenants tenantScopeResolver
	members adminMembershipStore
	baseURL string
}

type adminStore interface {
	GetByEmail(ctx context.Context, email string) (admin.Admin, error)
	GetByID(ctx context.Context, id string) (admin.Admin, error)
	IncrementFailedAttempts(ctx context.Context, id string) error
	ResetFailedAttempts(ctx context.Context, id string) error
}

type tenantScopeResolver interface {
	FirstActiveID(ctx context.Context) (string, error)
}

type adminMembershipStore interface {
	EnsureAdminMember(ctx context.Context, tenantID, userID string) (tenantmember.Member, error)
}

// NewHandler wires the dependencies.
func NewHandler(signer *session.Signer, admins *admin.Repo, tenants *tenant.TenantRepo, members adminMembershipStore, baseURL string) *Handler {
	return ptrext.Of(Handler{
		signer:  signer,
		admins:  admins,
		tenants: tenants,
		members: members,
		baseURL: strings.TrimRight(baseURL, "/"),
	})
}

// ScopeAdminSession scopes stale admin sessions that were minted before the
// first tenant existed. That keeps tenant-scoped console pages usable after
// operators create the first tenant without forcing a logout/login cycle.
func (h *Handler) ScopeAdminSession(next http.Handler) http.Handler {
	const where = "console.auth.Handler.ScopeAdminSession"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCtx := session.FromContext(r.Context())
		if h == nil || authCtx.TenantID != "" || h.admins == nil || h.tenants == nil {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := h.admins.GetByID(r.Context(), authCtx.UserID); err != nil {
			if !errors.Is(err, admin.ErrNotFound) {
				logext.Warnf(r.Context(), "[%s] admin lookup failed,user_id:%s,err:%+v",
					where, authCtx.UserID, err.Error())
			}
			next.ServeHTTP(w, r)
			return
		}
		tenantID, err := h.tenants.FirstActiveID(r.Context())
		if err != nil {
			if !errors.Is(err, tenant.ErrTenantNotFound) {
				logext.Warnf(r.Context(), "[%s] FirstActiveID failed,user_id:%s,err:%+v",
					where, authCtx.UserID, err.Error())
			}
			next.ServeHTTP(w, r)
			return
		}
		scoped := ptrext.Indirect(authCtx)
		scoped.TenantID = tenantID
		h.ensureAdminMembership(r.Context(), where, tenantID, authCtx.UserID)
		next.ServeHTTP(w, r.WithContext(session.WithAuthCtx(r.Context(), ptrext.Of(scoped))))
	})
}

// RequireLoginOrigin handles the login-only origin check before dispatcher
// decodes the proto request body.
func (h *Handler) RequireLoginOrigin(r *http.Request, _ *attunev1.LoginRequest) error {
	const where = "console.auth.Handler.Login"
	ctx := r.Context()
	if !originAllowed(r, h.baseURL) {
		logext.Warnf(ctx, "[%s] reject: bad origin,origin:%s",
			where, r.Header.Get("Origin"))
		return dispatcher.NewError(http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "cross-site login not allowed")
	}
	return nil
}

// ValidateRequest enforces endpoint-specific body rules after
// dispatcher.JSONBody has decoded the proto request.
func (h *Handler) ValidateRequest(_ *http.Request, req *attunev1.LoginRequest) error {
	if req.GetEmail() == "" || req.GetPassword() == "" {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "email and password required")
	}
	return nil
}

// Login handles POST /install/login. The request/response shapes are
// attune.v1.LoginRequest / LoginResponse (proto, per CLAUDE.md §11).
// The response never distinguishes "unknown email" from "wrong password"
// (dummy bcrypt + generic 401); failure counts are tracked on the admin
// row so a real attacker hits the 5-strike / 15-minute lockout, but
// enumeration via timing is not possible.
func (h *Handler) Login(ctx *dispatcher.RequestContext[struct{}], req *attunev1.LoginRequest) (dispatcher.Result[*attunev1.LoginResponse], error) {
	const where = "console.auth.Handler.Login"
	a, err := h.authenticate(ctx, req) // *LoginRequest — proto messages carry sync.Mutex
	if err != nil {
		return dispatcher.Result[*attunev1.LoginResponse]{}, err
	}

	scopeTenantID := h.resolveAdminScope(ctx, where)
	if scopeTenantID != "" {
		h.ensureAdminMembership(ctx, where, scopeTenantID, a.ID)
	}
	if err := h.signer.IssueSessionCookie(ctx, scopeTenantID, a.ID); err != nil {
		logext.Errorf(ctx, "[%s] IssueSessionCookie failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.LoginResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal error")
	}

	redirect := "/console/"
	if req.GetRedirectUri() != "" && redirectIsSafe(h.baseURL, req.GetRedirectUri()) {
		redirect = req.GetRedirectUri()
	}

	logext.Infof(ctx, "[%s] OK,admin_id:%s,redirect:%s", where, a.ID, redirect)
	return dispatcher.OK(ptrext.Of(attunev1.LoginResponse{Redirect: redirect}))
}

// authenticate resolves the admin row by email and verifies the password.
// Timing is equalised across "unknown email", "locked", and "wrong password"
// by always running a bcrypt op.
func (h *Handler) authenticate(ctx context.Context, req *attunev1.LoginRequest) (admin.Admin, error) {
	const where = "console.auth.Handler.authenticate"
	a, err := h.admins.GetByEmail(ctx, req.GetEmail())
	switch {
	case errors.Is(err, admin.ErrNotFound):
		// Equalise timing with a dummy bcrypt run; result discarded.
		_ = VerifyOrDummy("", req.GetPassword())
		return admin.Admin{}, dispatcher.NewError(http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "invalid credentials")
	case err != nil:
		logext.Errorf(ctx, "[%s] GetByEmail failed,err:%+v", where, err.Error())
		return admin.Admin{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal error")
	}

	if a.LockedUntil != nil && a.LockedUntil.After(time.Now()) {
		// Run a dummy bcrypt even when locked so an attacker probing the
		// locked-account oracle can't distinguish "exists + locked" from
		// "wrong password" by response time (review M1, #66).
		_ = VerifyOrDummy("", req.GetPassword())
		return admin.Admin{}, dispatcher.NewError(http.StatusLocked, attunev1.ErrorCode_LOCKED, "account locked due to too many failed attempts")
	}

	// Lock expired but counter still hot — fresh attempt window starts
	// now. Without this reset the attacker would re-lock the admin on
	// the very next failed try (failed_attempts already == threshold),
	// turning the 15-min lockout into an indefinite DoS (#66 review
	// M-1, paired with the `=` threshold tweak in the repo).
	if a.LockedUntil != nil && a.FailedAttempts > 0 {
		if err := h.admins.ResetFailedAttempts(ctx, a.ID); err != nil {
			logext.Warnf(ctx, "[%s] reset expired lock counter failed,err:%+v", where, err.Error())
		}
	}

	if !VerifyOrDummy(a.PasswordHash, req.GetPassword()) {
		if err := h.admins.IncrementFailedAttempts(ctx, a.ID); err != nil {
			logext.Warnf(ctx, "[%s] IncrementFailedAttempts failed,err:%+v", where, err.Error())
		}
		return admin.Admin{}, dispatcher.NewError(http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "invalid credentials")
	}

	if err := h.admins.ResetFailedAttempts(ctx, a.ID); err != nil {
		logext.Warnf(ctx, "[%s] ResetFailedAttempts failed,err:%+v", where, err.Error())
	}
	return a, nil
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

func (h *Handler) ensureAdminMembership(ctx context.Context, where, tenantID, userID string) {
	if h == nil || h.members == nil || tenantID == "" || userID == "" {
		return
	}
	if _, err := h.members.EnsureAdminMember(ctx, tenantID, userID); err != nil {
		logext.Warnf(ctx, "[%s] ensure admin member failed,tenant_id:%s,user_id:%s,err:%+v",
			where, tenantID, userID, err.Error())
	}
}

// originAllowed returns true when the request's Origin (or Referer
// fallback) matches the configured baseURL's origin, or when neither
// header is set (curl / native-app POSTs — these cannot be driven from
// a malicious page). Sec-Fetch-Site=same-origin is treated as a
// definitive same-site signal.
//
// baseURL is the canonical console origin from config
// (ConsoleBaseURL). The configured scheme is respected, so dev may use
// http:// while production uses https://.
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

// sameOrigin compares the scheme + host:port portion of two URLs.
// Trailing path is allowed (Referer typically includes one); we
// normalise default ports (`:443` for https, `:80` for http) so an
// explicit `https://x:443` matches an implicit `https://x` (#66
// review NIT). Scheme is part of the comparison — an http origin
// cannot impersonate https.
func sameOrigin(got, want string) bool {
	a, ok1 := originKey(got)
	b, ok2 := originKey(want)
	return ok1 && ok2 && a == b
}

// originKey returns "<scheme>://<host>:<port>" with default ports
// stripped, suitable for direct string equality. Returns (_, false)
// on parse failure.
func originKey(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port, true
	}
	return scheme + "://" + host, true
}
