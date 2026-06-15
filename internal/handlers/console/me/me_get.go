package me

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// Me handles GET /fb/v1/console/me.
func (h *MeHandler) Me(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetMeRequest) (dispatcher.Result[*attunev1.GetMeResponse], error) {
	const where = "console.MeHandler.Me"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s,user_id:%s,user_type:%s", where, auth.TenantID, auth.UserID, auth.UserType)

	// OIDC user sessions are identified by UserType="oidc".
	if auth.UserType == "oidc" {
		return h.meOIDCUser(ctx, auth.TenantID, auth.UserID)
	}

	// Admin sessions are identified by UserID hitting the admins table. They
	// may carry a non-empty TenantID resolved at login so tenant-scoped
	// handlers can operate, but /me itself renders the admin profile.
	if h.admins != nil {
		if a, err := h.admins.GetByID(ctx, auth.UserID); err == nil {
			return h.meAdmin(ctx, a, auth.TenantID)
		} else if !errors.Is(err, admin.ErrNotFound) {
			logext.Errorf(ctx, "[%s] admins.GetByID failed,user_id:%s,err:%+v",
				where, auth.UserID, err.Error())
			return dispatcher.Fail[*attunev1.GetMeResponse](
				http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to look up admin")
		}
	}
	if auth.TenantID == "" {
		h.signer.ClearSessionCookie(ctx)
		return dispatcher.Fail[*attunev1.GetMeResponse](
			http.StatusUnauthorized, attunev1.ErrorCode_USER_GONE, "session subject not found")
	}
	return h.meTenantUser(ctx, auth.TenantID, auth.UserID)
}

// adminTenantSlug is the synthetic Tenant.Slug returned in admin-session /me
// responses when no real tenant has been resolved at login.
const adminTenantSlug = "_admin"

func (h *MeHandler) meAdmin(ctx *dispatcher.RequestContext[*session.AuthCtx], a admin.Admin, tenantID string) (dispatcher.Result[*attunev1.GetMeResponse], error) {
	const where = "console.MeHandler.meAdmin"
	tenantPB := ptrext.Of(attunev1.Tenant{
		Id:       adminTenantSlug,
		Slug:     adminTenantSlug,
		Name:     "System Admin",
		Locale:   "",
		Timezone: "",
	})
	if tenantID != "" && h.tenants != nil {
		if row, err := h.tenants.GetByID(ctx, tenantID); err == nil {
			tenantPB = ptrext.Of(attunev1.Tenant{
				Id:       row.ID,
				Slug:     row.Slug,
				Name:     row.Name,
				Locale:   row.Locale,
				Timezone: row.Timezone,
			})
		} else {
			logext.Warnf(ctx, "[%s] tenant load failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		}
	}
	me := ptrext.Of(attunev1.GetMeResponse{
		Tenant: tenantPB,
		User: ptrext.Of(attunev1.SessionUser{
			OpenId: a.ID,
			Name:   adminDisplay(a),
			Role:   "admin",
		}),
		CsrfToken: h.signer.CSRFToken(a.ID),
	})
	logext.Infof(ctx, "[%s] OK,admin_id:%s,tenant_id:%s", where, a.ID, tenantID)
	return dispatcher.OK(me)
}

func adminDisplay(a admin.Admin) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Email
}

func (h *MeHandler) meOIDCUser(ctx *dispatcher.RequestContext[*session.AuthCtx], tenantID, userID string) (dispatcher.Result[*attunev1.GetMeResponse], error) {
	const where = "console.MeHandler.meOIDCUser"

	if h.oidcUsers == nil {
		logext.Errorf(ctx, "[%s] oidcUsers repo not configured", where)
		return dispatcher.Fail[*attunev1.GetMeResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "OIDC not configured")
	}

	user, err := h.oidcUsers.GetByID(ctx, userID)
	if err != nil {
		logext.Warnf(ctx, "[%s] oidc user not found,user_id:%s,err:%s", where, userID, err.Error())
		h.signer.ClearSessionCookie(ctx)
		return dispatcher.Fail[*attunev1.GetMeResponse](
			http.StatusUnauthorized, attunev1.ErrorCode_USER_GONE, "OIDC user not found")
	}

	// Build tenant block (synthetic for OIDC users without tenant association)
	tenantPB := ptrext.Of(attunev1.Tenant{
		Id:       "_oidc",
		Slug:     "_oidc",
		Name:     "SSO User",
		Locale:   "",
		Timezone: "",
	})
	if tenantID != "" && h.tenants != nil {
		if row, err := h.tenants.GetByID(ctx, tenantID); err == nil {
			tenantPB = ptrext.Of(attunev1.Tenant{
				Id:       row.ID,
				Slug:     row.Slug,
				Name:     row.Name,
				Locale:   row.Locale,
				Timezone: row.Timezone,
			})
		}
	}

	displayName := user.DisplayName
	if displayName == "" {
		displayName = user.Email
	}

	me := ptrext.Of(attunev1.GetMeResponse{
		Tenant: tenantPB,
		User: ptrext.Of(attunev1.SessionUser{
			OpenId: user.ExternalID,
			Name:   displayName,
			Role:   user.Role,
		}),
		CsrfToken: h.signer.CSRFToken(user.ID),
	})
	logext.Infof(ctx, "[%s] OK,oidc_user_id:%s,role:%s", where, user.ID, user.Role)
	return dispatcher.OK(me)
}

func (h *MeHandler) meTenantUser(ctx *dispatcher.RequestContext[*session.AuthCtx], tenantID, userID string) (dispatcher.Result[*attunev1.GetMeResponse], error) {
	const where = "console.MeHandler.meTenantUser"
	user, err := h.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantUserNotFound) {
			logext.Warnf(ctx, "[%s] reject: user gone,user_id:%s", where, userID)
			h.signer.ClearSessionCookie(ctx)
			return dispatcher.Fail[*attunev1.GetMeResponse](
				http.StatusUnauthorized, attunev1.ErrorCode_USER_GONE, "user is disabled or deleted")
		}
		logext.Errorf(ctx, "[%s] users.GetByID failed,user_id:%s,err:%+v",
			where, userID, err.Error())
		return dispatcher.Fail[*attunev1.GetMeResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load user")
	}
	tenantRow, err := h.tenants.GetByID(ctx, tenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] tenants.GetByID failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetMeResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load tenant")
	}

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
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,user_id:%s,role:%s",
		where, tenantID, userID, user.Role)
	return dispatcher.OK(me)
}
