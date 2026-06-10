package me

import (
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Logout handles POST /fb/v1/console/logout. Returns 204.
func (h *MeHandler) Logout(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.LogoutRequest) (dispatcher.Result[*attunev1.LogoutResponse], error) {
	const where = "console.MeHandler.Logout"
	h.signer.ClearSessionCookie(ctx)
	logext.Infof(ctx, "[%s] OK", where)
	return dispatcher.NoContent[*attunev1.LogoutResponse]()
}
