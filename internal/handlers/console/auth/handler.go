// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

// Handler owns the local-admin login + logout endpoints (#66 Plan T11)
// that replace the deleted external-OAuth flow.
type Handler struct {
	signer  *session.Signer
	admins  *admin.Repo
	baseURL string
}

// NewHandler wires the dependencies.
func NewHandler(signer *session.Signer, admins *admin.Repo, baseURL string) *Handler {
	return ptrext.Of(Handler{
		signer:  signer,
		admins:  admins,
		baseURL: strings.TrimRight(baseURL, "/"),
	})
}

// Routes mounts /install/login + /install/logout.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	return r
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

	var req attunev1.LoginRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		if errors.Is(err, respond.ErrBodyTooLarge) {
			respond.Error(ctx, w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 1 MiB")
			return
		}
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}
	if req.GetEmail() == "" || req.GetPassword() == "" {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "email and password required")
		return
	}

	a, err := h.admins.GetByEmail(ctx, req.GetEmail())
	switch {
	case errors.Is(err, admin.ErrNotFound):
		// Equalise timing with a dummy bcrypt run; result discarded.
		_ = VerifyOrDummy("", req.GetPassword())
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	case err != nil:
		logext.Errorf(ctx, "[%s] GetByEmail failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	if a.LockedUntil != nil && a.LockedUntil.After(time.Now()) {
		respond.Error(ctx, w, http.StatusLocked, "locked", "account locked due to too many failed attempts")
		return
	}

	if !VerifyOrDummy(a.PasswordHash, req.GetPassword()) {
		if err := h.admins.IncrementFailedAttempts(ctx, a.ID); err != nil {
			logext.Warnf(ctx, "[%s] IncrementFailedAttempts failed,err:%+v", where, err.Error())
		}
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	if err := h.admins.ResetFailedAttempts(ctx, a.ID); err != nil {
		logext.Warnf(ctx, "[%s] ResetFailedAttempts failed,err:%+v", where, err.Error())
	}

	// Admins are global, not tenant-scoped (RBAC for tenants comes in
	// #38). TenantID stays empty; UserID is the admin row id.
	if err := h.signer.IssueSessionCookie(w, "", a.ID); err != nil {
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

// Logout handles POST /install/logout. Clears the session cookie. Idempotent.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	const where = "console.auth.Handler.Logout"
	ctx := r.Context()

	// Match IssueSessionCookie's Secure flag (Insecure mode allows
	// plain-HTTP dev deploys).
	secure := !h.signer.Insecure()
	http.SetCookie(w, ptrext.Of(http.Cookie{
		Name:     session.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	}))
	logext.Infof(ctx, "[%s] OK", where)
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.LogoutResponse{}))
}
