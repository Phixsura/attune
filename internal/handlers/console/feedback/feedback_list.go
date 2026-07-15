package feedback

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
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
	"source":               {},
	"type":                 {},
	"urgent":               {},
	"tag":                  {},
	"workflow_state":       {},
	"workflow_category":    {},
	"enrichment_status":    {},
	"terminal_failed_only": {},
	"ids":                  {},
	"confidence_lte":       {},
	"created_from":         {},
	"created_to":           {},
	"enriched_from":        {},
	"enriched_to":          {},
	"quality_signal":       {},
}

func BindListRequest(r *http.Request, req *attunev1.ListFeedbackRequest) error {
	q := r.URL.Query()
	req.Cursor = queryStr(q, "cursor")
	req.Limit = queryInt32(q, "limit")
	req.Q = queryStr(q, "q")
	req.Source = queryStr(q, "source")
	req.Type = queryStr(q, "type")
	req.Urgent = queryBool(q, "urgent")
	req.TagId = queryStr(q, "tag")
	req.WorkflowStateId = queryStr(q, "workflow_state")
	req.WorkflowCategory = queryStr(q, "workflow_category")
	req.EnrichmentStatus = queryStr(q, "enrichment_status")
	req.TerminalFailedOnly = queryBool(q, "terminal_failed_only")
	req.Ids = queryIDs(q, "ids")
	req.ConfidenceLte = queryFloat64(q, "confidence_lte")
	req.CreatedFrom = queryStr(q, "created_from")
	req.CreatedTo = queryStr(q, "created_to")
	req.EnrichedFrom = queryStr(q, "enriched_from")
	req.EnrichedTo = queryStr(q, "enriched_to")
	req.QualitySignal = queryStr(q, "quality_signal")
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

func queryIDs(q map[string][]string, key string) []int64 {
	var out []int64
	for _, raw := range q[key] {
		for _, token := range strings.Split(raw, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(token), 10, 64); err == nil && id > 0 {
				out = append(out, id)
			}
		}
	}
	if len(out) > 50 {
		return out[:50]
	}
	return out
}

func queryFloat64(q map[string][]string, key string) *float64 {
	if vs := q[key]; len(vs) > 0 {
		if n, err := strconv.ParseFloat(vs[0], 64); err == nil {
			return ptrext.Of(n)
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
	opts, err := consoleListOptsFromRequest(req, cfg.Dimensions)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListFeedbackResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
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

func consoleListOptsFromRequest(req *attunev1.ListFeedbackRequest, dims []domain.Dimension) (feedback.ConsoleListOpts, error) {
	opts := ptrext.Of(feedback.ConsoleListOpts{
		Q:     req.GetQ(),
		Attrs: attrFiltersFromProto(req.GetAttrs(), dims),
	})
	applyListOptionalOpts(req, opts)
	if err := applyListQualityOpts(req, opts); err != nil {
		return feedback.ConsoleListOpts{}, err
	}
	return ptrext.Indirect(opts), nil
}

func applyListOptionalOpts(req *attunev1.ListFeedbackRequest, opts *feedback.ConsoleListOpts) {
	if c := req.GetCursor(); c != "" {
		if v, err := strconv.ParseInt(c, 10, 64); err == nil {
			opts.Cursor = v
		}
	}
	if req.Limit != nil {
		opts.Limit = int(req.GetLimit())
	}
	if source := req.GetSource(); source != "" {
		opts.Source = ptrext.Of(source)
	}
	if typ := req.GetType(); typ != "" {
		opts.Type = ptrext.Of(typ)
	}
	opts.Urgent = req.Urgent
	opts.TagID = req.TagId
	opts.WorkflowStateID = req.WorkflowStateId
	opts.WorkflowCategory = req.WorkflowCategory
	opts.EnrichmentStatus = req.EnrichmentStatus
	opts.TerminalFailedOnly = req.TerminalFailedOnly
}

func applyListQualityOpts(req *attunev1.ListFeedbackRequest, opts *feedback.ConsoleListOpts) error {
	opts.IDs = req.GetIds()
	opts.ConfidenceLTE = req.ConfidenceLte
	opts.QualitySignal = req.QualitySignal
	var err error
	if opts.CreatedFrom, err = optionalListTime(req.CreatedFrom); err != nil {
		return err
	}
	if opts.CreatedTo, err = optionalListTime(req.CreatedTo); err != nil {
		return err
	}
	if opts.EnrichedFrom, err = optionalListTime(req.EnrichedFrom); err != nil {
		return err
	}
	if opts.EnrichedTo, err = optionalListTime(req.EnrichedTo); err != nil {
		return err
	}
	return nil
}

func optionalListTime(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	parsed, err := parseListTime(ptrext.Indirect(raw))
	if err != nil {
		return nil, err
	}
	return ptrext.Of(parsed), nil
}

func parseListTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
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
