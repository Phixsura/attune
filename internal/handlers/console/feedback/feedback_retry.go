package feedback

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// RetryEnrichment handles POST /fb/v1/console/feedback/{id}/retry-enrichment.
// It resets a terminal-failed row so it can be re-enriched.
func (h *FeedbackHandler) RetryEnrichment(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RetryEnrichmentRequest,
) (dispatcher.Result[*attunev1.RetryEnrichmentResponse], error) {
	const where = "console.FeedbackHandler.RetryEnrichment"
	auth := ctx.Auth
	id := req.GetId()
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%d", where, auth.TenantID, id)

	result, err := h.repo.RetryEnrichment(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, feedback.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.RetryEnrichmentResponse](
				http.StatusNotFound,
				attunev1.ErrorCode_NOT_FOUND,
				"feedback not found or not owned by tenant",
			)
		}
		if errors.Is(err, feedback.ErrInvalidStateForRetry) {
			logext.Warnf(ctx, "[%s] reject: invalid state,tenant_id:%s,id:%d",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.RetryEnrichmentResponse](
				http.StatusConflict,
				attunev1.ErrorCode_CONFLICT,
				"row must be in failed status to retry",
			)
		}
		logext.Errorf(ctx, "[%s] feedback.RetryEnrichment failed,tenant_id:%s,id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RetryEnrichmentResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to retry enrichment",
		)
	}

	// Audit log the action
	if h.audit != nil {
		actorType := auth.UserType
		if actorType == "" {
			actorType = "admin"
		}
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(actorType, auth.UserID, ctx.Request()),
			Action:     "retry_enrichment",
			TargetType: "feedback",
			TargetID:   fmt.Sprintf("%d", id),
			Summary:    "Retried enrichment for terminal-failed feedback",
			Before:     map[string]any{"enrichment_status": "failed"},
			After: map[string]any{
				"enrichment_status":   result.Status,
				"enrichment_attempts": result.Attempts,
			},
		})
	}

	resp := ptrext.Of(attunev1.RetryEnrichmentResponse{
		Id:                    result.ID,
		EnrichmentStatus:      result.Status,
		EnrichmentAttempts:    int32(result.Attempts),
		EnrichmentNextRetryAt: result.NextRetryAt.UTC().Format(time.RFC3339),
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
	return dispatcher.OK(resp)
}
