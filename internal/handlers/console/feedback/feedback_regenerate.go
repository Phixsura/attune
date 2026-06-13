package feedback

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Regenerate handles POST /fb/v1/console/feedback/{id}/reply-draft/regenerate.
// It re-runs the shared ReplyDrafter.Generate core synchronously and returns
// the fresh draft. All guards are tenant-scoped via Precheck: the row must be
// owned by the tenant (404), reply-draft must be enabled for the tenant (403),
// and the row must be enriched (409) — only then is an LLM call spent. A manual
// regenerate is an explicit operator action and so bypasses only the
// *confidence* gate (which governs automatic pre-generation at enrich time),
// never the opt-in flag. Token/cost are audited via the wrapped client. The
// draft is operator-facing only — never auto-sent.
func (h *FeedbackHandler) Regenerate(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RegenerateReplyDraftRequest,
) (dispatcher.Result[*attunev1.RegenerateReplyDraftResponse], error) {
	const where = "console.FeedbackHandler.Regenerate"
	auth := ctx.Auth
	id := req.GetId()
	if h.drafter == nil {
		logext.Warnf(ctx, "[%s] reject: reply-draft not configured,tenant_id:%s,id:%d", where, auth.TenantID, id)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusServiceUnavailable, attunev1.ErrorCode_INTERNAL, "reply-draft generation is not configured")
	}
	status, enabled, found, err := h.drafter.Precheck(ctx, id, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] precheck failed,tenant_id:%s,id:%d,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load feedback")
	}
	if !found {
		logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d", where, auth.TenantID, id)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "feedback not found or not owned by tenant")
	}
	if !enabled {
		logext.Warnf(ctx, "[%s] reject: reply-draft disabled for tenant,tenant_id:%s,id:%d", where, auth.TenantID, id)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "reply-draft is not enabled for this tenant")
	}
	if status != "done" {
		logext.Warnf(ctx, "[%s] reject: not enriched,tenant_id:%s,id:%d,status:%s", where, auth.TenantID, id, status)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "feedback is not enriched yet")
	}
	draft, generatedAt, err := h.drafter.Generate(ctx, id, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,tenant_id:%s,id:%d,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "failed to regenerate reply draft")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
	return dispatcher.OK(ptrext.Of(attunev1.RegenerateReplyDraftResponse{
		ReplyDraft:            draft,
		ReplyDraftGeneratedAt: generatedAt.UTC().Format(time.RFC3339),
	}))
}
