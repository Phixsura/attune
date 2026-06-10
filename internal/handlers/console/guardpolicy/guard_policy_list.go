package guardpolicy

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func (h *Handler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListGuardPoliciesRequest) (dispatcher.Result[*attunev1.ListGuardPoliciesResponse], error) {
	const where = "console.GuardPolicyHandler.List"
	auth := ctx.Auth
	if auth.TenantID == "" {
		return dispatcher.Fail[*attunev1.ListGuardPoliciesResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_TENANT, "tenant session required")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	policies, err := h.svc.List(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListGuardPoliciesResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list guard policies")
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListGuardPoliciesResponse{Items: policiesToProto(policies)}))
}
