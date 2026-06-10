package guardpolicy

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func (h *Handler) Resolve(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ResolveGuardPolicyRequest) (dispatcher.Result[*attunev1.ResolveGuardPolicyResponse], error) {
	const where = "console.GuardPolicyHandler.Resolve"
	auth := ctx.Auth
	if auth.TenantID == "" {
		return dispatcher.Fail[*attunev1.ResolveGuardPolicyResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_TENANT, "tenant session required")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,channel:%s,has_source_id:%t,purpose:%s",
		where, auth.TenantID, req.GetChannel(), req.GetSourceId() != "", req.GetPurpose())
	plan, err := h.svc.Resolve(ctx, llmclient.GuardMetadata{
		TenantID:    auth.TenantID,
		Channel:     req.GetChannel(),
		SourceID:    req.GetSourceId(),
		SourceTags:  req.GetSourceTags(),
		Purpose:     req.GetPurpose(),
		Environment: req.GetEnvironment(),
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] resolve failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ResolveGuardPolicyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to resolve guard policies")
	}
	return dispatcher.OK(ptrext.Of(attunev1.ResolveGuardPolicyResponse{Rules: rulesToProto(plan.Rules)}))
}
