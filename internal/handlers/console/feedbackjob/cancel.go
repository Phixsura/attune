// SPDX-License-Identifier: Apache-2.0

package feedbackjob

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
)

// Cancel handles POST /fb/v1/console/jobs/{job_id}/cancel.
func (h *Handler) Cancel(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CancelJobRequest,
) (dispatcher.Result[*attunev1.CancelJobResponse], error) {
	const where = "console.feedbackjob.Handler.Cancel"
	auth := ctx.Auth
	jobID := req.GetJobId()

	logext.Infof(ctx, "[%s] start,tenant_id:%s,job_id:%s", where, auth.TenantID, jobID)

	resp, err := h.svc.CancelJob(ctx, auth.TenantID, jobID)
	if errors.Is(err, feedbackbatch.ErrJobNotFound) {
		logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,job_id:%s", where, auth.TenantID, jobID)
		return dispatcher.Fail[*attunev1.CancelJobResponse](
			http.StatusNotFound,
			attunev1.ErrorCode_JOB_NOT_FOUND,
			"job not found",
		)
	}
	if errors.Is(err, feedbackbatch.ErrJobNotCancellable) {
		logext.Warnf(ctx, "[%s] reject: not cancellable,tenant_id:%s,job_id:%s", where, auth.TenantID, jobID)
		return dispatcher.Fail[*attunev1.CancelJobResponse](
			http.StatusConflict,
			attunev1.ErrorCode_JOB_ALREADY_CANCELLED,
			"job cannot be cancelled in its current state",
		)
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] svc.CancelJob failed,tenant_id:%s,job_id:%s,err:%+v",
			where, auth.TenantID, jobID, err.Error())
		return dispatcher.Fail[*attunev1.CancelJobResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to cancel job",
		)
	}
	if err := h.recordAudit(ctx, "feedback_job.cancel", jobID, "Cancelled feedback batch job", map[string]any{
		"job_id": jobID,
	}, map[string]any{
		"job_id":  jobID,
		"status":  resp.GetStatus().String(),
		"message": resp.GetMessage(),
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,job_id:%s,err:%+v",
			where, auth.TenantID, jobID, err.Error())
		return dispatcher.Fail[*attunev1.CancelJobResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to write audit log",
		)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,job_id:%s", where, auth.TenantID, jobID)
	return dispatcher.OK(resp)
}
