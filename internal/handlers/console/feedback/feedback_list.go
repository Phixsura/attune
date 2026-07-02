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
	"cursor":               {},
	"limit":                {},
	"q":                    {},
	"urgent":               {},
	"tag":                  {},
	"workflow_state":       {},
	"workflow_category":    {},
	"enrichment_status":    {},
	"terminal_failed_only": {},
}

func BindListRequest(r *http.Request, req *attunev1.ListFeedbackRequest) error {
	q := r.URL.Query()
	req.Cursor = queryStr(q, "cursor")
	req.Limit = queryInt32(q, "limit")
	req.Q = queryStr(q, "q")
	req.Urgent = queryBool(q, "urgent")
	req.TagId = queryStr(q, "tag")
	req.WorkflowStateId = queryStr(q, "workflow_state")
	req.WorkflowCategory = queryStr(q, "workflow_category")
	req.EnrichmentStatus = queryStr(q, "enrichment_status")
	req.TerminalFailedOnly = queryBool(q, "terminal_failed_only")
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

func queryStr(q map[string][]string, key string) *string {
	if vs := q[key]; len(vs) > 0 && vs[0] != "" {
		return ptrext.Of(vs[0])
	}
	return nil
}

func queryInt32(q map[string][]string, key string) *int32 {
	if vs := q[key]; len(vs) > 0 {
		if n, err := strconv.ParseInt(vs[0], 10, 32); err == nil {
			return ptrext.Of(int32(n))
		}
	}
	return nil
}

func queryBool(q map[string][]string, key string) *bool {
	if vs := q[key]; len(vs) > 0 && vs[0] != "" {
		return ptrext.Of(vs[0] == "true" || vs[0] == "1")
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
	if req.TagId != nil {
		opts.TagID = req.TagId
	}
	if req.WorkflowStateId != nil {
		opts.WorkflowStateID = req.WorkflowStateId
	}
	if req.WorkflowCategory != nil {
		opts.WorkflowCategory = req.WorkflowCategory
	}
	if req.EnrichmentStatus != nil {
		opts.EnrichmentStatus = req.EnrichmentStatus
	}
	if req.TerminalFailedOnly != nil {
		opts.TerminalFailedOnly = req.TerminalFailedOnly
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
	h.enrichItemsWithTags(ctx, where, auth.TenantID, rows, items)
	h.enrichItemsWithWorkflowState(ctx, where, auth.TenantID, rows, items)
	resp := ptrext.Of(attunev1.ListFeedbackResponse{Items: items})
	if opts.Limit > 0 && len(rows) == opts.Limit {
		resp.NextCursor = ptrext.Of(strconv.FormatInt(rows[len(rows)-1].ID, 10))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
	return dispatcher.OK(resp)
}

func (h *FeedbackHandler) enrichItemsWithTags(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where, tenantID string,
	rows []feedback.ConsoleListRow, items []*attunev1.Feedback,
) {
	enrichFeedbackItemsWithTags(ctx, where, tenantID, rows, items, h.tagAssignments)
}

func enrichFeedbackItemsWithTags(
	ctx *dispatcher.RequestContext[*session.AuthCtx], where, tenantID string,
	rows []feedback.ConsoleListRow, items []*attunev1.Feedback, reader tagAssignmentReader,
) {
	if reader == nil || len(rows) == 0 {
		return
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	tagMap, err := reader.ListByFeedbackBatch(ctx, tenantID, ids)
	if err != nil {
		logext.Warnf(ctx, "[%s] tag batch load failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return
	}
	for _, item := range items {
		for _, info := range tagMap[item.GetId()] {
			item.Tags = append(item.Tags, tagInfoToProto(info))
		}
	}
}
