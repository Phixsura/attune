package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// DevLoginHandler implements the backdoor /install/dev-login endpoint
// for HTTP-only end-to-end smoke testing — bypasses the Lark OAuth flow so
// we can verify the SPA + session middleware + tenant_users + 4 stub
// pages end-to-end before real OAuth is wired (which needs HTTPS +
// registered domain).
//
// HARD GATE: only mounted if `ConsoleDevLogin == true` AND
// `ConsoleInsecureCookies == true` in config. Logs WARN on every use
// with the resolved tenant + synthetic open_id so audit trails make
// it obvious which sessions were minted via this backdoor.
//
// Must be removed (or at minimum the env flags zeroed) before any real
// user-facing deployment.
type DevLoginHandler struct {
	signer  *session.Signer
	tenants *tenant.TenantRepo
	users   *tenant.TenantUserRepo
	baseURL string
}

func NewDevLoginHandler(
	signer *session.Signer,
	tenants *tenant.TenantRepo,
	users *tenant.TenantUserRepo,
	baseURL string,
) *DevLoginHandler {
	return &DevLoginHandler{
		signer:  signer,
		tenants: tenants,
		users:   users,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// ServeHTTP handles GET /fb/v1/console/install/dev-login.
//
// Query params:
//
//	tenant — tenant slug (required; must already exist)
//	name — display name for the synthetic user (default "dev-tester")
//	role — admin|member (default admin)
//
// Effect: upserts tenant_users with open_id=`dev:<sha256(tenant+name)[:16]>`,
// signs a session cookie, 302s to /console/.
func (h *DevLoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const where = "console.DevLoginHandler.ServeHTTP"
	ctx := r.Context()
	slug := r.URL.Query().Get("tenant")
	name := r.URL.Query().Get("name")
	role := r.URL.Query().Get("role")
	if slug == "" {
		logext.Warnf(ctx, "[%s] reject: missing tenant slug", where)
		respond.Error(ctx, w, http.StatusBadRequest, "missing_tenant", "missing ?tenant=<slug>")
		return
	}
	if name == "" {
		name = "dev-tester"
	}
	if role != "admin" && role != "member" {
		role = "admin"
	}
	logext.Infof(ctx, "[%s] start,slug:%s,name:%s,role:%s,remote_ip:%s",
		where, slug, name, role, r.RemoteAddr)

	tenantID, err := h.tenants.ResolveSlug(ctx, slug)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			logext.Warnf(ctx, "[%s] reject: tenant not found,slug:%s", where, slug)
			respond.Error(ctx, w, http.StatusNotFound, "tenant_not_found",
				"tenant '"+slug+"' does not exist; create it first via `attune tenant create`")
			return
		}
		slog.ErrorContext(ctx, "dev-login: resolve tenant", "err", err)
		logext.Errorf(ctx, "[%s] tenants.ResolveSlug failed,slug:%s,err:%+v",
			where, slug, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to resolve tenant")
		return
	}

	// Synthetic open_id — deterministic per (tenant, name) so repeated
	// dev-login with the same name idempotently lands on the same row.
	sum := sha256.Sum256([]byte(slug + ":" + name))
	openID := "dev:" + base64.RawURLEncoding.EncodeToString(sum[:8])

	userID, err := h.users.Upsert(ctx, tenantID, openID, name, "", role)
	if err != nil {
		slog.ErrorContext(ctx, "dev-login: upsert user", "err", err)
		logext.Errorf(ctx, "[%s] users.Upsert failed,tenant_id:%s,open_id:%s,err:%+v",
			where, tenantID, openID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to upsert user")
		return
	}

	if err := h.signer.IssueSessionCookie(w, tenantID, userID); err != nil {
		slog.ErrorContext(ctx, "dev-login: sign session", "err", err)
		logext.Errorf(ctx, "[%s] signer.IssueSessionCookie failed,user_id:%s,err:%+v",
			where, userID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to sign session")
		return
	}

	slog.WarnContext(ctx, "dev-login: backdoor session minted",
		"tenant_slug", slug, "tenant_id", tenantID,
		"user_id", userID, "open_id", openID, "role", role,
		"remote_ip", r.RemoteAddr)
	logext.Warnf(ctx, "[%s] backdoor session minted,slug:%s,tenant_id:%s,user_id:%s,open_id:%s,role:%s,remote_ip:%s",
		where, slug, tenantID, userID, openID, role, r.RemoteAddr)

	http.Redirect(w, r, h.baseURL+"/console/", http.StatusFound)
}
