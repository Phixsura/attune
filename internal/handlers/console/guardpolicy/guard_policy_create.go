package guardpolicy

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func (h *Handler) Create(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateGuardPolicyRequest) (dispatcher.Result[*attunev1.GuardPolicy], error) {
	const where = "console.GuardPolicyHandler.Create"
	auth := ctx.Auth
	if auth.TenantID == "" {
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusBadRequest, attunev1.ErrorCode_MISSING_TENANT, "tenant session required")
	}
	policy := policyFromProto(req.GetPolicy())
	logext.Infof(ctx, "[%s] start,tenant_id:%s,name:%s", where, auth.TenantID, policy.Name)
	created, err := h.svc.CreatePolicy(ctx, auth.TenantID, auth.UserID, policy)
	if err != nil {
		return guardPolicyWriteError(ctx, where, auth.TenantID, err)
	}
	if err := h.recordAudit(ctx, "guard_policy.create", "guard_policy", created.ID,
		guardPolicySummary("Created guard policy", created), nil, auditPolicySnapshot(created)); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,policy_id:%s,err:%+v", where, auth.TenantID, created.ID, err.Error())
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(policyToProto(created))
}
