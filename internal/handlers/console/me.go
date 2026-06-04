package console

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wanmuchengchuan/listen/internal/logext"
	"github.com/wanmuchengchuan/listen/internal/repo"
)

// MeHandler serves GET /fb/v1/console/me and POST /fb/v1/console/logout.
//
// /me reads session (set by RequireSession middleware), loads tenant +
// user from DB, and returns the SessionMe schema (openapi.yaml).
// /logout simply clears the cookie.
type MeHandler struct {
	signer  *Signer
	tenants *repo.TenantRepo
	users   *repo.TenantUserRepo
}

func NewMeHandler(signer *Signer, tenants *repo.TenantRepo, users *repo.TenantUserRepo) *MeHandler {
	return &MeHandler{signer: signer, tenants: tenants, users: users}
}

type meResponse struct {
	Tenant    meTenant `json:"tenant"`
	User      meUser   `json:"user"`
	CSRFToken string   `json:"csrf_token"`
}

type meTenant struct {
	ID            string  `json:"id"`
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	LarkTenantKey *string `json:"lark_tenant_key,omitempty"`
	Locale        string  `json:"locale"`
	Timezone      string  `json:"timezone"`
}

type meUser struct {
	OpenID    string  `json:"open_id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url,omitempty"`
	Role      string  `json:"role"`
}

// Me handles GET /fb/v1/console/me.
func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) {
	const where = "console.MeHandler.Me"
	auth := FromContext(r.Context())
	ctx := r.Context()
	logext.Infof(ctx, "[%s] start,tenant_id:%s,user_id:%s", where, auth.TenantID, auth.UserID)

	user, err := h.users.GetByID(ctx, auth.UserID)
	if err != nil {
		if errors.Is(err, repo.ErrTenantUserNotFound) {
			// Session points to a user that no longer exists — clear
			// the cookie and 401 so the SPA bounces to /login.
			logext.Warnf(ctx, "[%s] reject: user gone,user_id:%s", where, auth.UserID)
			h.signer.ClearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "user_gone", "用户已停用或被删除")
			return
		}
		slog.ErrorContext(ctx, "/me: load user", "err", err, "user_id", auth.UserID)
		logext.Errorf(ctx, "[%s] users.GetByID failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "加载用户失败")
		return
	}
	tenant, err := h.tenants.GetByID(ctx, auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "/me: load tenant", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] tenants.GetByID failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "加载 tenant 失败")
		return
	}

	// Fire-and-forget: bump last_seen_at. Failure is just a metric loss.
	if err := h.users.TouchLastSeen(ctx, user.ID); err != nil {
		slog.DebugContext(ctx, "/me: touch last_seen", "err", err)
	}

	var avatar *string
	if user.AvatarURL != "" {
		avatar = &user.AvatarURL
	}

	resp := meResponse{
		Tenant: meTenant{
			ID:            tenant.ID,
			Slug:          tenant.Slug,
			Name:          tenant.Name,
			LarkTenantKey: tenant.LarkTenantKey,
			Locale:        tenant.Locale,
			Timezone:      tenant.Timezone,
		},
		User: meUser{
			OpenID:    user.OpenID,
			Name:      user.Name,
			AvatarURL: avatar,
			Role:      user.Role,
		},
		CSRFToken: h.signer.CSRFToken(user.ID),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
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
