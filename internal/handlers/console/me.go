package console

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// MeHandler serves GET /fb/v1/console/me and POST /fb/v1/console/logout.
//
// /me reads session (set by RequireSession middleware), loads tenant +
// user from DB, and returns GetMeResponse. /logout clears the cookie.
type MeHandler struct {
	signer  *Signer
	tenants *tenant.TenantRepo
	users   *tenant.TenantUserRepo
}

func NewMeHandler(signer *Signer, tenants *tenant.TenantRepo, users *tenant.TenantUserRepo) *MeHandler {
	return &MeHandler{signer: signer, tenants: tenants, users: users}
}

// Me handles GET /fb/v1/console/me.
func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) {
	const where = "console.MeHandler.Me"
	ctx := r.Context()
	auth := FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s,user_id:%s", where, auth.TenantID, auth.UserID)

	user, err := h.users.GetByID(ctx, auth.UserID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantUserNotFound) {
			// Session points to a user that no longer exists — clear the
			// cookie and 401 so the SPA bounces to /login.
			logext.Warnf(ctx, "[%s] reject: user gone,user_id:%s", where, auth.UserID)
			h.signer.ClearSessionCookie(w)
			respondError(ctx, w, http.StatusUnauthorized, "user_gone", "用户已停用或被删除")
			return
		}
		slog.ErrorContext(ctx, "/me: load user", "err", err, "user_id", auth.UserID)
		logext.Errorf(ctx, "[%s] users.GetByID failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "加载用户失败")
		return
	}
	tenant, err := h.tenants.GetByID(ctx, auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "/me: load tenant", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] tenants.GetByID failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "加载 tenant 失败")
		return
	}

	// Fire-and-forget: bump last_seen_at. Failure is just a metric loss.
	if err := h.users.TouchLastSeen(ctx, user.ID); err != nil {
		slog.DebugContext(ctx, "/me: touch last_seen", "err", err)
	}

	me := &attunev1.GetMeResponse{
		Tenant: &attunev1.Tenant{
			Id:            tenant.ID,
			Slug:          tenant.Slug,
			Name:          tenant.Name,
			LarkTenantKey: tenant.LarkTenantKey,
			Locale:        tenant.Locale,
			Timezone:      tenant.Timezone,
		},
		User: &attunev1.SessionUser{
			OpenId: user.OpenID,
			Name:   user.Name,
			Role:   user.Role,
		},
		CsrfToken: h.signer.CSRFToken(user.ID),
	}
	if user.AvatarURL != "" {
		me.User.AvatarUrl = &user.AvatarURL
	}
	respondProto(w, http.StatusOK, me)
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
