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

// GetStatus handles GET /fb/v1/console/jobs/{job_id}.
func (h *Handler) GetStatus(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetJobStatusRequest,
) (dispatcher.Result[*attunev1.JobStatusResponse], error) {
	const where = "console.feedbackjob.Handler.GetStatus"
	auth := ctx.Auth
	jobID := req.GetJobId()

	logext.Infof(ctx, "[%s] start,tenant_id:%s,job_id:%s", where, auth.TenantID, jobID)

	resp, err := h.svc.GetJobStatus(ctx, auth.TenantID, jobID)
	if errors.Is(err, feedbackbatch.ErrJobNotFound) {
		logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,job_id:%s", where, auth.TenantID, jobID)
		return dispatcher.Fail[*attunev1.JobStatusResponse](
			http.StatusNotFound,
			attunev1.ErrorCode_JOB_NOT_FOUND,
			"job not found",
		)
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] svc.GetJobStatus failed,tenant_id:%s,job_id:%s,err:%+v",
			where, auth.TenantID, jobID, err.Error())
		return dispatcher.Fail[*attunev1.JobStatusResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to get job status",
		)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,job_id:%s,status:%s",
		where, auth.TenantID, jobID, resp.GetStatus())
	return dispatcher.OK(resp)
}
