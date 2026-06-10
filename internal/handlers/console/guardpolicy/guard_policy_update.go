package guardpolicy

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	serviceguard "github.com/Phixsura/attune/internal/service/guardpolicy"
)

func (h *Handler) Update(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateGuardPoliciesRequest) (dispatcher.Result[*attunev1.UpdateGuardPoliciesResponse], error) {
	const where = "console.GuardPolicyHandler.Update"
	auth := ctx.Auth
	if auth.TenantID == "" {
		return dispatcher.Fail[*attunev1.UpdateGuardPoliciesResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_TENANT, "tenant session required")
	}
	policies := policiesFromProto(req.GetPolicies())
	logext.Infof(ctx, "[%s] start,tenant_id:%s,policies_n:%d", where, auth.TenantID, len(policies))
	items, err := h.svc.ReplaceTenantPolicies(ctx, auth.TenantID, auth.UserID, policies)
	if err != nil {
		if code := serviceguard.ErrToCode(err); code != attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED {
			return dispatcher.Fail[*attunev1.UpdateGuardPoliciesResponse](http.StatusBadRequest, code, serviceguard.ErrToMessage(err))
		}
		logext.Errorf(ctx, "[%s] replace failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.UpdateGuardPoliciesResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update guard policies")
	}
	return dispatcher.OK(ptrext.Of(attunev1.UpdateGuardPoliciesResponse{Items: policiesToProto(items)}))
}
