// SPDX-License-Identifier: Apache-2.0

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
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
)

// BatchHandler handles batch feedback operations.
type BatchHandler struct {
	service feedbackbatch.Service
}

// NewBatchHandler creates a new batch handler with the given service.
func NewBatchHandler(service feedbackbatch.Service) *BatchHandler {
	return ptrext.Of(BatchHandler{service: service})
}

// Execute handles POST /fb/v1/console/feedback/batch.
func (h *BatchHandler) Execute(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.BatchFeedbackRequest,
) (dispatcher.Result[*attunev1.BatchFeedbackResponse], error) {
	const where = "console.BatchHandler.Execute"
	auth := ctx.Auth

	// Convert proto request to service request.
	svcReq, err := h.toServiceRequest(auth, req)
	if err != nil {
		logext.Warnf(ctx, "[%s] invalid request,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	// Execute the batch operation.
	resp, err := h.service.Execute(ctx, svcReq)
	if err != nil {
		return h.handleServiceError(ctx, auth.TenantID, err)
	}

	// Convert service response to proto response.
	protoResp := h.toProtoResponse(resp)

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,matched:%d,succeeded:%d,skipped:%d,failed:%d,job_id:%s",
		where, auth.TenantID, resp.TotalMatched, resp.Succeeded, resp.Skipped, len(resp.Failed), resp.JobID)

	if resp.JobID != "" {
		// Async job created - return 202 Accepted.
		return dispatcher.Success(http.StatusAccepted, protoResp)
	}
	return dispatcher.OK(protoResp)
}

// toServiceRequest converts the proto request to a service request.
func (h *BatchHandler) toServiceRequest(
	auth *session.AuthCtx,
	req *attunev1.BatchFeedbackRequest,
) (*feedbackbatch.BatchRequest, error) {
	svcReq := ptrext.Of(feedbackbatch.BatchRequest{
		TenantID:       auth.TenantID,
		CreatedBy:      auth.UserID,
		FeedbackIDs:    req.GetFeedbackIds(),
		Filter:         req.GetQuery(),
		DryRun:         req.GetDryRun(),
		Operation:      req.GetOperation(),
		IdempotencyKey: req.GetIdempotencyKey(),
	})

	// Handle max_affected optional field.
	if req.MaxAffected != nil {
		svcReq.MaxAffected = int(req.GetMaxAffected())
	}

	// Handle confirm_count optional field.
	if req.ConfirmCount != nil {
		svcReq.ConfirmCount = int(req.GetConfirmCount())
		svcReq.HasConfirmCount = true
	}

	// Parse if_unmodified_since as RFC3339 timestamp if provided.
	if req.IfUnmodifiedSince != nil {
		ts, err := time.Parse(time.RFC3339, req.GetIfUnmodifiedSince())
		if err != nil {
			return nil, errors.New("if_unmodified_since must be RFC3339 format")
		}
		svcReq.IfUnmodifiedSince = ptrext.Of(ts)
	}

	return svcReq, nil
}

// toProtoResponse converts the service response to a proto response.
func (h *BatchHandler) toProtoResponse(resp *feedbackbatch.BatchResponse) *attunev1.BatchFeedbackResponse {
	protoResp := ptrext.Of(attunev1.BatchFeedbackResponse{
		TotalMatched: int32(resp.TotalMatched),
		Succeeded:    int32(resp.Succeeded),
		Skipped:      int32(resp.Skipped),
		Failed:       resp.Failed,
	})

	if resp.JobID != "" {
		protoResp.JobId = ptrext.Of(resp.JobID)
	}
	if resp.JobStatusURL != "" {
		protoResp.JobStatusUrl = ptrext.Of(resp.JobStatusURL)
	}

	return protoResp
}

// handleServiceError maps service errors to HTTP responses.
func (h *BatchHandler) handleServiceError(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	err error,
) (dispatcher.Result[*attunev1.BatchFeedbackResponse], error) {
	const where = "console.BatchHandler.Execute"

	switch {
	case errors.Is(err, feedbackbatch.ErrRateLimited):
		logext.Warnf(ctx, "[%s] rate limited,tenant_id:%s", where, tenantID)
		// Set Retry-After header.
		ctx.SetCookie(ptrext.Of(http.Cookie{
			Name:   "X-Retry-After",
			Value:  "60",
			MaxAge: 60,
		}))
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusTooManyRequests, attunev1.ErrorCode_RATE_LIMITED, "rate limit exceeded, retry after 60 seconds")

	case errors.Is(err, feedbackbatch.ErrIdempotencyConflict):
		logext.Warnf(ctx, "[%s] idempotency conflict,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusConflict, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT, "idempotency key used with different request parameters")

	case errors.Is(err, feedbackbatch.ErrRequestInProgress):
		logext.Warnf(ctx, "[%s] request in progress,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusConflict, attunev1.ErrorCode_REQUEST_IN_PROGRESS, "request with this idempotency key is already in progress")

	case errors.Is(err, feedbackbatch.ErrCountMismatch):
		logext.Warnf(ctx, "[%s] count mismatch,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "actual count does not match confirm_count")

	case errors.Is(err, feedbackbatch.ErrExceedsMaxAffected):
		logext.Warnf(ctx, "[%s] exceeds max affected,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "query matches more items than max_affected allows")

	case errors.Is(err, feedbackbatch.ErrConcurrentJobLimit):
		logext.Warnf(ctx, "[%s] concurrent job limit,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusTooManyRequests, attunev1.ErrorCode_RATE_LIMITED, "concurrent batch job limit reached")

	case errors.Is(err, feedbackbatch.ErrNoOperation):
		logext.Warnf(ctx, "[%s] no operation,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "operation is required")

	case errors.Is(err, feedbackbatch.ErrNoSelection):
		logext.Warnf(ctx, "[%s] no selection,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "either feedback_ids or query is required")

	case errors.Is(err, feedbackbatch.ErrMixedSelection):
		logext.Warnf(ctx, "[%s] mixed selection,tenant_id:%s", where, tenantID)
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "cannot specify both feedback_ids and query")

	default:
		logext.Errorf(ctx, "[%s] internal error,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return dispatcher.Fail[*attunev1.BatchFeedbackResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal server error")
	}
}
