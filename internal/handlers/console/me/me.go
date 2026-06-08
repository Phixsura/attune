package me

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MeHandler serves GET /fb/v1/console/me and POST /fb/v1/console/logout.
//
// /me reads session (set by RequireSession middleware), loads tenant +
// user from DB, and returns GetMeResponse. /logout clears the cookie.
type MeHandler struct {
	signer  *session.Signer
	tenants *tenant.TenantRepo
	users   *tenant.TenantUserRepo
}

func NewMeHandler(signer *session.Signer, tenants *tenant.TenantRepo, users *tenant.TenantUserRepo) *MeHandler {
	return ptrext.Of(MeHandler{signer: signer, tenants: tenants, users: users})
}

// Me handles GET /fb/v1/console/me.
func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) {
	const where = "console.MeHandler.Me"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s,user_id:%s", where, auth.TenantID, auth.UserID)

	user, err := h.users.GetByID(ctx, auth.UserID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantUserNotFound) {
			// Session points to a user that no longer exists — clear the
			// cookie and 401 so the SPA bounces to /login.
			logext.Warnf(ctx, "[%s] reject: user gone,user_id:%s", where, auth.UserID)
			h.signer.ClearSessionCookie(w)
			respond.Error(ctx, w, http.StatusUnauthorized, "user_gone", "user is disabled or deleted")
			return
		}
		logext.Errorf(ctx, "[%s] users.GetByID failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load user")
		return
	}
	// Local name avoids shadowing the imported `tenant` package — code added
	// below this point can still reference tenant.ErrX / tenant.Y.
	tenantRow, err := h.tenants.GetByID(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] tenants.GetByID failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
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
			Id:            tenantRow.ID,
			Slug:          tenantRow.Slug,
			Name:          tenantRow.Name,
			LarkTenantKey: tenantRow.LarkTenantKey,
			Locale:        tenantRow.Locale,
			Timezone:      tenantRow.Timezone,
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
		where, auth.TenantID, auth.UserID, user.Role)
}

// Logout handles POST /fb/v1/console/logout. Returns 204.
func (h *MeHandler) Logout(w http.ResponseWriter, r *http.Request) {
	const where = "console.MeHandler.Logout"
	h.signer.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	logext.Infof(r.Context(), "[%s] OK", where)
}
