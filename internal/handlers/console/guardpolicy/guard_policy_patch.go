package guardpolicy

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func (h *Handler) Patch(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.PatchGuardPolicyRequest) (dispatcher.Result[*attunev1.GuardPolicy], error) {
	const where = "console.GuardPolicyHandler.Patch"
	auth := ctx.Auth
	if auth.TenantID == "" {
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusBadRequest, attunev1.ErrorCode_MISSING_TENANT, "tenant session required")
	}
	id, err := parsePolicyID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	policy := policyFromProto(req.GetPolicy())
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s,name:%s", where, auth.TenantID, id, policy.Name)
	before := h.policyByID(ctx, auth.TenantID, id)
	updated, err := h.svc.UpdatePolicy(ctx, auth.TenantID, auth.UserID, id, policy)
	if err != nil {
		return guardPolicyWriteError(ctx, where, auth.TenantID, err)
	}
	var beforeSnapshot any
	if before != nil {
		beforeSnapshot = auditPolicySnapshot(ptrext.Indirect(before))
	}
	if err := h.recordAudit(ctx, "guard_policy.update", "guard_policy", updated.ID,
		guardPolicySummary("Updated guard policy", updated), beforeSnapshot, auditPolicySnapshot(updated)); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,policy_id:%s,err:%+v", where, auth.TenantID, updated.ID, err.Error())
		return dispatcher.Fail[*attunev1.GuardPolicy](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(policyToProto(updated))
}
