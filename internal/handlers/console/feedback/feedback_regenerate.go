package feedback

import (
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// Regenerate handles POST /fb/v1/console/feedback/{id}/reply-draft/regenerate.
// It re-runs the shared Generate core synchronously and returns the fresh
// draft. A manual regenerate is an explicit operator action, so it bypasses
// the per-tenant opt-in/confidence gate (that gate only governs automatic
// pre-generation at enrich time). Token/cost are still recorded via the
// audit-wrapping client. The tenant-scoped load first prevents cross-tenant id
// probing.
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
	if _, err := h.repo.GetForConsole(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, feedback.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d", where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "feedback not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] load failed,tenant_id:%s,id:%d,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load feedback")
	}
	draft, err := h.drafter.Generate(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,tenant_id:%s,id:%d,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RegenerateReplyDraftResponse](http.StatusBadGateway, attunev1.ErrorCode_INTERNAL, "failed to regenerate reply draft")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
	return dispatcher.OK(ptrext.Of(attunev1.RegenerateReplyDraftResponse{
		ReplyDraft:            draft,
		ReplyDraftGeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}))
}
