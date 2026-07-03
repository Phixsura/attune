package feedback

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

// regenerateCooldown is the minimum gap between manual reply-draft regenerations
// of the same row. It's the per-row cost backstop on the synchronous operator
// endpoint (the auto path is gated by opt-in + confidence at enrich time); a
// human re-reading a 2-3 sentence draft before retrying rarely beats it, while a
// scripted loop of POSTs is capped instead of running unbounded LLM spend.
const regenerateCooldown = 10 * time.Second

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
	// Per-tenant rate limit first (in-memory, no DB): caps a tenant's total
	// regenerations so a script can't spread unbounded LLM spend across many
	// owned rows, which the per-row cooldown below alone wouldn't stop.
	if h.regenLimiter != nil && !h.regenLimiter.Allow(auth.TenantID) {
		logext.Warnf(ctx, "[%s] reject: rate limited,tenant_id:%s,id:%d", where, auth.TenantID, id)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusTooManyRequests, attunev1.ErrorCode_RATE_LIMITED, "too many reply-draft regenerations; please slow down")
	}
	status, enabled, found, lastGeneratedAt, err := h.drafter.Precheck(ctx, id, auth.TenantID)
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
	sendPending, err := h.replyDraftSendPending(ctx, where, auth.TenantID, id)
	if err != nil {
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "reply-draft workflow failed")
	}
	if sendPending {
		logext.Warnf(ctx, "[%s] reject: send pending,tenant_id:%s,id:%d", where, auth.TenantID, id)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusConflict, attunev1.ErrorCode_REQUEST_IN_PROGRESS, "reply send is already in progress")
	}
	// Per-row cooldown: the async/auto path is cost-gated at enrich time (opt-in
	// + confidence); this is the matching backstop on the synchronous operator
	// endpoint so a loop of POSTs against one row can't drive unbounded LLM
	// spend. The client also disables the button while pending, but that is
	// trivially bypassed by calling the endpoint directly.
	if lastGeneratedAt != nil && time.Since(ptrext.Indirect(lastGeneratedAt)) < regenerateCooldown {
		logext.Warnf(ctx, "[%s] reject: cooldown,tenant_id:%s,id:%d", where, auth.TenantID, id)
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusTooManyRequests, attunev1.ErrorCode_RATE_LIMITED, "reply draft was just regenerated; please wait a few seconds before retrying")
	}
	draft, generatedAt, err := h.drafter.Generate(ctx, id, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,tenant_id:%s,id:%d,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "failed to regenerate reply draft")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
	return h.replyDraftRegenerateResponse(ctx, where, auth.TenantID, id, draft, generatedAt)
}

func (h *FeedbackHandler) replyDraftRegenerateResponse(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where string, tenantID string, id int64, draft string, generatedAt time.Time,
) (dispatcher.Result[*attunev1.RegenerateReplyDraftResponse], error) {
	resp := ptrext.Of(attunev1.RegenerateReplyDraftResponse{
		ReplyDraft:            draft,
		ReplyDraftGeneratedAt: generatedAt.UTC().Format(time.RFC3339),
	})
	if h.replyWorkflow != nil {
		if snap, err := h.replyWorkflow.Snapshot(ctx, tenantID, id); err == nil {
			resp.Workflow = replyDraftWorkflowToProto(snap)
			if err := h.recordReplyDraftAudit(ctx, "reply_draft.generate", id, "Generated reply draft", nil, auditWorkflowSnapshot(snap)); err != nil {
				return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to record audit event")
			}
		} else {
			logext.Errorf(ctx, "[%s] workflow snapshot failed,tenant_id:%s,id:%d,err:%+v", where, tenantID, id, err.Error())
			return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "reply-draft workflow failed")
		}
	}
	return dispatcher.OK(resp)
}

func (h *FeedbackHandler) replyDraftSendPending(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where string, tenantID string, id int64,
) (bool, error) {
	if h.replyWorkflow == nil {
		return false, nil
	}
	snap, err := h.replyWorkflow.Snapshot(ctx, tenantID, id)
	if err != nil {
		logext.Warnf(ctx, "[%s] workflow snapshot failed,tenant_id:%s,id:%d,err:%+v", where, tenantID, id, err.Error())
		return false, err
	}
	return snap.Draft != nil && snap.Draft.Status == replydraftrepo.StatusSendPending, nil
}
