package me

import (
	"context"
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MeHandler serves GET /fb/v1/console/me and POST /fb/v1/console/logout.
//
// /me reads session (set by RequireSession middleware) and returns
// GetMeResponse. Two session shapes are supported:
//
//   - Admin session (TenantID == ""): UserID points at admins.id. The
//     response carries the admin Email/Name/Role + a synthetic Tenant
//     placeholder (slug="_admin") so the SPA can render the console
//     shell without a real tenant. This branch was added in review B1
//     fix (#66) — the previous code unconditionally looked up
//     tenant_users which 401'd every admin login.
//   - Tenant user session (TenantID != ""): existing tenant_users path.
type MeHandler struct {
	signer  *session.Signer
	tenants *tenant.TenantRepo
	users   *tenant.TenantUserRepo
	admins  *admin.Repo
}

// NewMeHandler wires the repos.
func NewMeHandler(
	signer *session.Signer,
	tenants *tenant.TenantRepo,
	users *tenant.TenantUserRepo,
	admins *admin.Repo,
) *MeHandler {
	return ptrext.Of(MeHandler{signer: signer, tenants: tenants, users: users, admins: admins})
}

// Me handles GET /fb/v1/console/me.
func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) {
	const where = "console.MeHandler.Me"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s,user_id:%s", where, auth.TenantID, auth.UserID)

	if auth.TenantID == "" {
		h.meAdmin(ctx, w, auth.UserID)
		return
	}
	h.meTenantUser(ctx, w, auth.TenantID, auth.UserID)
}

// adminTenantSlug is the synthetic Tenant.Slug returned in admin-session
// /me responses. SPA gates on it to suppress tenant-scoped CRUD if the
// caller is the global admin.
const adminTenantSlug = "_admin"

// meAdmin serves the admin-session /me branch.
func (h *MeHandler) meAdmin(ctx context.Context, w http.ResponseWriter, adminID string) {
	const where = "console.MeHandler.meAdmin"
	a, err := h.admins.GetByID(ctx, adminID)
	if err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			logext.Warnf(ctx, "[%s] admin row gone,admin_id:%s", where, adminID)
			h.signer.ClearSessionCookie(w)
			respond.Error(ctx, w, http.StatusUnauthorized, "user_gone", "admin row is gone")
			return
		}
		logext.Errorf(ctx, "[%s] admins.GetByID failed,admin_id:%s,err:%+v", where, adminID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load admin")
		return
	}
	// The synthetic tenant lets the SPA render its layout shell
	// (TopBar, etc.) without crashing on a missing Tenant.Slug. The
	// SPA distinguishes admin from tenant-scoped users via Role.
	me := ptrext.Of(attunev1.GetMeResponse{
		Tenant: ptrext.Of(attunev1.Tenant{
			Id:       adminTenantSlug,
			Slug:     adminTenantSlug,
			Name:     "System Admin",
			Locale:   "",
			Timezone: "",
		}),
		User: ptrext.Of(attunev1.SessionUser{
			OpenId: a.ID,
			Name:   adminDisplay(a),
			Role:   "admin",
		}),
		CsrfToken: h.signer.CSRFToken(a.ID),
	})
	respond.Proto(w, http.StatusOK, me)
	logext.Infof(ctx, "[%s] OK,admin_id:%s", where, a.ID)
}

// adminDisplay falls back to email when display_name is empty so the
// SPA always has a non-blank label to render in the TopBar.
func adminDisplay(a admin.Admin) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Email
}

// meTenantUser serves the tenant-user session /me branch (pre-#66
// behaviour).
func (h *MeHandler) meTenantUser(ctx context.Context, w http.ResponseWriter, tenantID, userID string) {
	const where = "console.MeHandler.meTenantUser"
	user, err := h.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantUserNotFound) {
			// Session points to a user that no longer exists — clear the
			// cookie and 401 so the SPA bounces to /login.
			logext.Warnf(ctx, "[%s] reject: user gone,user_id:%s", where, userID)
			h.signer.ClearSessionCookie(w)
			respond.Error(ctx, w, http.StatusUnauthorized, "user_gone", "user is disabled or deleted")
			return
		}
		logext.Errorf(ctx, "[%s] users.GetByID failed,user_id:%s,err:%+v",
			where, userID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load user")
		return
	}
	tenantRow, err := h.tenants.GetByID(ctx, tenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] tenants.GetByID failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load tenant")
		return
	}

	// Fire-and-forget: bump last_seen_at. Failure is just a metric loss,
	// but a sustained warn stream means the DB is unhappy — log it.
	if err := h.users.TouchLastSeen(ctx, user.ID); err != nil {
		logext.Warnf(ctx, "[%s] touch last_seen failed,err:%+v", where, err.Error())
	}

	me := ptrext.Of(attunev1.GetMeResponse{
		Tenant: ptrext.Of(attunev1.Tenant{
			Id:       tenantRow.ID,
			Slug:     tenantRow.Slug,
			Name:     tenantRow.Name,
			Locale:   tenantRow.Locale,
			Timezone: tenantRow.Timezone,
		}),
		User: ptrext.Of(attunev1.SessionUser{
			OpenId: user.OpenID,
			Name:   user.Name,
			Role:   user.Role,
		}),
		CsrfToken: h.signer.CSRFToken(user.ID),
	})
	if user.AvatarURL != "" {
		me.User.AvatarUrl = ptrext.Of(user.AvatarURL)
	}
	respond.Proto(w, http.StatusOK, me)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,user_id:%s,role:%s",
		where, tenantID, userID, user.Role)
}

// Logout handles POST /fb/v1/console/logout. Returns 204.
func (h *MeHandler) Logout(w http.ResponseWriter, r *http.Request) {
	const where = "console.MeHandler.Logout"
	h.signer.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	logext.Infof(r.Context(), "[%s] OK", where)
}
