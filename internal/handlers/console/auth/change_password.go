// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

// minNewPasswordLen is the lower bound the change-password endpoint
// enforces on new_password. 12 chars matches the OWASP 2024 baseline
// and the bootstrap-admin code path.
const minNewPasswordLen = 12

// ChangePasswordHandler owns POST /fb/v1/console/me/change-password.
// It sits behind RequireSession so CSRF is already enforced by the
// middleware; this handler only deals with the cred rotation itself.
//
// Admin-only by design: tenant_users password storage is out of scope
// for #66 (their rotation belongs to the IdP that owns their identity);
// a session whose TenantID is non-empty is rejected with 403.
type ChangePasswordHandler struct {
	admins passwordAdminStore
	signer *session.Signer
}

type passwordAdminStore interface {
	GetByID(ctx context.Context, id string) (admin.Admin, error)
	UpdatePasswordHash(ctx context.Context, id, newHash string) error
}

// NewChangePasswordHandler wires the dependencies. The signer is kept
// so a future iteration can revoke peer sessions on rotate; the current
// release does not (single-session per admin is the day-1 expectation).
func NewChangePasswordHandler(admins *admin.Repo, signer *session.Signer) *ChangePasswordHandler {
	var store passwordAdminStore
	if admins != nil {
		store = admins
	}
	return ptrext.Of(ChangePasswordHandler{admins: store, signer: signer})
}

// ValidateRequest enforces endpoint-specific body rules after
// dispatcher.JSONBody has decoded the proto request.
func (h *ChangePasswordHandler) ValidateRequest(_ *http.Request, req *attunev1.ChangePasswordRequest) error {
	if req.GetCurrentPassword() == "" || req.GetNewPassword() == "" {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST,
			"current_password and new_password are required")
	}
	if len(req.GetNewPassword()) < minNewPasswordLen {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_WEAK_PASSWORD,
			"new_password must be at least 12 characters")
	}
	if req.GetCurrentPassword() == req.GetNewPassword() {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_SAME_PASSWORD,
			"new_password must differ from current_password")
	}
	return nil
}

func (h *ChangePasswordHandler) ChangePassword(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ChangePasswordRequest) (dispatcher.Result[*attunev1.ChangePasswordResponse], error) {
	const where = "console.auth.ChangePasswordHandler.ChangePassword"
	auth := ctx.Auth

	a, err := h.admins.GetByID(ctx, auth.UserID)
	switch {
	case errors.Is(err, admin.ErrNotFound):
		// Session UserID isn't an admin (tenant-user, or admin row
		// deleted). 403 keeps the SPA from bouncing to /login.
		logext.Warnf(ctx, "[%s] not an admin,user_id:%s,tenant_id:%s",
			where, auth.UserID, auth.TenantID)
		return dispatcher.Fail[*attunev1.ChangePasswordResponse](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN,
			"only console admins can change their password here")
	case err != nil:
		logext.Errorf(ctx, "[%s] GetByID failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		return dispatcher.Fail[*attunev1.ChangePasswordResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal error")
	}

	if !VerifyOrDummy(a.PasswordHash, req.GetCurrentPassword()) {
		// Generic 401 — same response shape as login so timing /
		// content-based enumeration is not possible.
		return dispatcher.Fail[*attunev1.ChangePasswordResponse](http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED,
			"current password is wrong")
	}

	newHash, err := HashPassword(req.GetNewPassword())
	if err != nil {
		logext.Errorf(ctx, "[%s] HashPassword failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		return dispatcher.Fail[*attunev1.ChangePasswordResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to hash password")
	}
	if err := h.admins.UpdatePasswordHash(ctx, a.ID, newHash); err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			// Lost a TOCTOU race against an admin delete; behave the
			// same as the row-gone branch above.
			h.signer.ClearSessionCookie(ctx)
			return dispatcher.Fail[*attunev1.ChangePasswordResponse](http.StatusUnauthorized, attunev1.ErrorCode_USER_GONE, "admin row is gone")
		}
		logext.Errorf(ctx, "[%s] UpdatePasswordHash failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		return dispatcher.Fail[*attunev1.ChangePasswordResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update password")
	}

	logext.Infof(ctx, "[%s] OK,user_id:%s", where, a.ID)
	return dispatcher.OK(ptrext.Of(attunev1.ChangePasswordResponse{}))
}
