package feedback

import (
	"net/http"
	"strconv"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

var listFeedbackReservedQuery = map[string]struct{}{
	"cursor": {},
	"limit":  {},
	"q":      {},
	"urgent": {},
}

func BindListRequest(r *http.Request, req *attunev1.ListFeedbackRequest) error {
	q := r.URL.Query()
	if v := q.Get("cursor"); v != "" {
		req.Cursor = ptrext.Of(v)
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			req.Limit = ptrext.Of(int32(n))
		}
	}
	if v := q.Get("q"); v != "" {
		req.Q = ptrext.Of(v)
	}
	if v := q.Get("urgent"); v != "" {
		req.Urgent = ptrext.Of(v == "true" || v == "1")
	}
	for k, vs := range q {
		if _, ok := listFeedbackReservedQuery[k]; ok {
			continue
		}
		for _, v := range vs {
			if v == "" {
				continue
			}
			req.Attrs = append(req.Attrs, ptrext.Of(attunev1.AttrFilter{Dim: k, Value: v}))
		}
	}
	return nil
}

// List handles GET /fb/v1/console/feedback.
//
// Query params:
// - cursor / limit / q / urgent: standard pagination + free-text + urgent toggle
// - any dim name (e.g. `?severity=critical&labels=payment`): per-dim
// containment filter. Repeated params build multiple filters
// AND-composed via JSONB `@>`.
func (h *FeedbackHandler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListFeedbackRequest) (dispatcher.Result[*attunev1.ListFeedbackResponse], error) {
	const where = "console.FeedbackHandler.List"
	auth := ctx.Auth
	cfg, err := h.tenants.GetEnrichConfig(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] read dim cfg failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListFeedbackResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read tenant config")
	}
	opts := feedback.ConsoleListOpts{
		Q:     req.GetQ(),
		Attrs: attrFiltersFromProto(req.GetAttrs(), cfg.Dimensions),
	}
	if c := req.GetCursor(); c != "" {
		if v, err := strconv.ParseInt(c, 10, 64); err == nil {
			opts.Cursor = v
		}
	}
	if req.Limit != nil {
		opts.Limit = int(req.GetLimit())
	}
	if req.Urgent != nil {
		opts.Urgent = req.Urgent
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,attrs_n:%d,limit:%d,cursor:%d",
		where, auth.TenantID, len(opts.Attrs), opts.Limit, opts.Cursor)

	rows, err := h.repo.ListForConsole(ctx, auth.TenantID, opts)
	if err != nil {
		logext.Errorf(ctx, "[%s] feedback.ListForConsole failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListFeedbackResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list feedback")
	}
	items := make([]*attunev1.Feedback, 0, len(rows))
	for _, row := range rows {
		items = append(items, toProtoFeedback(row))
	}
	resp := ptrext.Of(attunev1.ListFeedbackResponse{Items: items})
	if opts.Limit > 0 && len(rows) == opts.Limit {
		resp.NextCursor = ptrext.Of(strconv.FormatInt(rows[len(rows)-1].ID, 10))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
	return dispatcher.OK(resp)
}
