// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
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
	admins *admin.Repo
	signer *session.Signer
}

// NewChangePasswordHandler wires the dependencies. The signer is kept
// so a future iteration can revoke peer sessions on rotate; the current
// release does not (single-session per admin is the day-1 expectation).
func NewChangePasswordHandler(admins *admin.Repo, signer *session.Signer) *ChangePasswordHandler {
	return ptrext.Of(ChangePasswordHandler{admins: admins, signer: signer})
}

// ChangePassword handles POST /fb/v1/console/me/change-password.
//
// Flow:
//  1. Decode + validate the request body (length, current != new).
//  2. Load admin row by UserID; if not found OR not an admin, reject
//     403 (tenant-user sessions can't change passwords here).
//  3. Verify current_password via VerifyOrDummy (timing-equalised). On
//     mismatch, return 401 with a generic message — the SPA shows
//     "current password is wrong" without leaking enumerate-ability.
//  4. Hash new_password (bcrypt cost 12) and persist.
//  5. Return 200 ChangePasswordResponse{}.
func (h *ChangePasswordHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	const where = "console.auth.ChangePasswordHandler.ChangePassword"
	ctx := r.Context()
	auth := session.FromContext(ctx)

	var req attunev1.ChangePasswordRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		if errors.Is(err, respond.ErrBodyTooLarge) {
			respond.Error(ctx, w, http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE,
				"request body exceeds 1 MiB")
			return
		}
		respond.Error(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
		return
	}
	if req.GetCurrentPassword() == "" || req.GetNewPassword() == "" {
		respond.Error(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST,
			"current_password and new_password are required")
		return
	}
	if len(req.GetNewPassword()) < minNewPasswordLen {
		respond.Error(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_WEAK_PASSWORD,
			"new_password must be at least 12 characters")
		return
	}
	if req.GetCurrentPassword() == req.GetNewPassword() {
		respond.Error(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_SAME_PASSWORD,
			"new_password must differ from current_password")
		return
	}

	a, err := h.admins.GetByID(ctx, auth.UserID)
	switch {
	case errors.Is(err, admin.ErrNotFound):
		// Session UserID isn't an admin (tenant-user, or admin row
		// deleted). 403 keeps the SPA from bouncing to /login.
		logext.Warnf(ctx, "[%s] not an admin,user_id:%s,tenant_id:%s",
			where, auth.UserID, auth.TenantID)
		respond.Error(ctx, w, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN,
			"only console admins can change their password here")
		return
	case err != nil:
		logext.Errorf(ctx, "[%s] GetByID failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal error")
		return
	}

	if !VerifyOrDummy(a.PasswordHash, req.GetCurrentPassword()) {
		// Generic 401 — same response shape as login so timing /
		// content-based enumeration is not possible.
		respond.Error(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED,
			"current password is wrong")
		return
	}

	newHash, err := HashPassword(req.GetNewPassword())
	if err != nil {
		logext.Errorf(ctx, "[%s] HashPassword failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to hash password")
		return
	}
	if err := h.admins.UpdatePasswordHash(ctx, a.ID, newHash); err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			// Lost a TOCTOU race against an admin delete; behave the
			// same as the row-gone branch above.
			h.signer.ClearSessionCookie(w)
			respond.Error(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_USER_GONE, "admin row is gone")
			return
		}
		logext.Errorf(ctx, "[%s] UpdatePasswordHash failed,user_id:%s,err:%+v",
			where, auth.UserID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update password")
		return
	}

	logext.Infof(ctx, "[%s] OK,user_id:%s", where, a.ID)
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.ChangePasswordResponse{}))
}
