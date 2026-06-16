package guardpolicy

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func (h *Handler) Delete(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteGuardPolicyRequest) (dispatcher.Result[*attunev1.DeleteGuardPolicyResponse], error) {
	const where = "console.GuardPolicyHandler.Delete"
	auth := ctx.Auth
	if auth.TenantID == "" {
		return dispatcher.Fail[*attunev1.DeleteGuardPolicyResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_TENANT, "tenant session required")
	}
	id, err := parsePolicyID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.DeleteGuardPolicyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	before := h.policyByID(ctx, auth.TenantID, id)
	if err := h.svc.DeletePolicy(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, llmguard.ErrPolicyNotFound) {
			return dispatcher.Fail[*attunev1.DeleteGuardPolicyResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "guard policy not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.DeleteGuardPolicyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete guard policy")
	}
	var beforeSnapshot any
	summary := "Deleted guard policy"
	if before != nil {
		beforeValue := ptrext.Indirect(before)
		beforeSnapshot = auditPolicySnapshot(beforeValue)
		summary = guardPolicySummary(summary, beforeValue)
	}
	if err := h.recordAudit(ctx, "guard_policy.delete", "guard_policy", id, summary, beforeSnapshot, nil); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,policy_id:%s,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteGuardPolicyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.NoContent[*attunev1.DeleteGuardPolicyResponse]()
}
