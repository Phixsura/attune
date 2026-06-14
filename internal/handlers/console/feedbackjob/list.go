// SPDX-License-Identifier: Apache-2.0

package feedbackjob

import (
	"net/http"
	"strconv"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// DefaultLimit is the default number of jobs to return.
const DefaultLimit = 20

// List handles GET /fb/v1/console/jobs.
func (h *Handler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListJobsRequest,
) (dispatcher.Result[*attunev1.ListJobsResponse], error) {
	const where = "console.feedbackjob.Handler.List"
	auth := ctx.Auth

	// Get status filter.
	var status *string
	if req.Status != nil && req.GetStatus() != "" {
		s := req.GetStatus()
		status = ptrext.Of(s)
	}

	// Get limit with default.
	limit := DefaultLimit
	if req.Limit != nil && req.GetLimit() > 0 {
		limit = int(req.GetLimit())
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,status:%v,limit:%d", where, auth.TenantID, status, limit)

	jobs, err := h.svc.ListJobs(ctx, auth.TenantID, status, limit)
	if err != nil {
		logext.Errorf(ctx, "[%s] svc.ListJobs failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListJobsResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to list jobs",
		)
	}

	resp := ptrext.Of(attunev1.ListJobsResponse{Jobs: jobs})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(jobs))
	return dispatcher.OK(resp)
}

// BindListRequest binds query parameters to the ListJobsRequest.
func BindListRequest(r *http.Request, req *attunev1.ListJobsRequest) error {
	q := r.URL.Query()

	if status := q.Get("status"); status != "" {
		req.Status = ptrext.Of(status)
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		limit, err := strconv.ParseInt(limitStr, 10, 32)
		if err != nil {
			return dispatcher.NewError(
				http.StatusBadRequest,
				attunev1.ErrorCode_BAD_REQUEST,
				"limit must be an integer",
			)
		}
		req.Limit = ptrext.Of(int32(limit))
	}

	return nil
}
