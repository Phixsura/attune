package feedback

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// FeedbackHandler serves /fb/v1/console/feedback. All queries scope to the
// session's tenant via FromContext — never taking tenant_id from query params.
//
// The handler also reads the tenant's EnrichConfig to translate the
// stable wire query (`?type=bug&severity=critical&labels=payment`)
// into the per-dim AttrFilter shape the repo's JSONB containment
// queries expect.
type FeedbackHandler struct {
	repo    *feedback.FeedbackRepo
	tenants *tenant.TenantRepo
}

func NewFeedbackHandler(r *feedback.FeedbackRepo, t *tenant.TenantRepo) *FeedbackHandler {
	return ptrext.Of(FeedbackHandler{repo: r, tenants: t})
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return ptrext.Of(s)
}

// attrsToStruct decodes the raw JSONB attrs payload into a structpb
// for proto wire output. Empty / missing returns an empty Struct so
// the SPA can rely on the field always being present.
func attrsToStruct(raw []byte) *structpb.Struct {
	if len(raw) == 0 {
		s, _ := structpb.NewStruct(map[string]any{})
		return s
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		s, _ := structpb.NewStruct(map[string]any{})
		return s
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		s2, _ := structpb.NewStruct(map[string]any{})
		return s2
	}
	return s
}

func toProtoFeedback(row feedback.ConsoleListRow) *attunev1.Feedback {
	return ptrext.Of(attunev1.Feedback{
		Id:               row.ID,
		Content:          row.Content,
		Source:           row.Source,
		Type:             row.Type,
		UserId:           row.UserID,
		PageUrl:          row.PageURL,
		EnrichedTitle:    nullableString(row.EnrichedTitle),
		EnrichedAttrs:    attrsToStruct(row.EnrichedAttrs),
		IsUrgent:         row.IsUrgent,
		EnrichmentStatus: row.EnrichmentStatus,
		CreatedAt:        row.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// List handles GET /fb/v1/console/feedback.
//
// Query params:
// - cursor / limit / q / urgent: standard pagination + free-text + urgent toggle
// - any dim name (e.g. `?severity=critical&labels=payment`): per-dim
// containment filter. Repeated params build multiple filters
// AND-composed via JSONB `@>`.
func (h *FeedbackHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.List"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	q := r.URL.Query()
	cfg, err := h.tenants.GetEnrichConfig(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] read dim cfg failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read tenant config")
		return
	}
	opts := feedback.ConsoleListOpts{
		Q:     q.Get("q"),
		Attrs: extractAttrFilters(q, cfg.Dimensions),
	}
	if c := q.Get("cursor"); c != "" {
		if v, err := strconv.ParseInt(c, 10, 64); err == nil {
			opts.Cursor = v
		}
	}
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			opts.Limit = v
		}
	}
	if u := q.Get("urgent"); u != "" {
		opts.Urgent = ptrext.Of(u == "true" || u == "1")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,attrs_n:%d,limit:%d,cursor:%d",
		where, auth.TenantID, len(opts.Attrs), opts.Limit, opts.Cursor)

	rows, err := h.repo.ListForConsole(ctx, auth.TenantID, opts)
	if err != nil {
		logext.Errorf(ctx, "[%s] feedback.ListForConsole failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to list feedback")
		return
	}
	items := make([]*attunev1.Feedback, 0, len(rows))
	for _, row := range rows {
		items = append(items, toProtoFeedback(row))
	}
	resp := ptrext.Of(attunev1.ListFeedbackResponse{Items: items})
	if opts.Limit > 0 && len(rows) == opts.Limit {
		resp.NextCursor = ptrext.Of(strconv.FormatInt(rows[len(rows)-1].ID, 10))
	}
	respond.Proto(w, http.StatusOK, resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// extractAttrFilters walks the query string for keys matching a
// Dimension.Name in the tenant's set, building one AttrFilter per
// value. Each per-dim filter is AND-composed downstream; multiple
// values for the same dim become multiple filters (also AND-composed,
// effectively requiring all of them).
func extractAttrFilters(q map[string][]string, dims []domain.Dimension) []feedback.AttrFilter {
	if len(dims) == 0 || len(q) == 0 {
		return nil
	}
	dimIdx := make(map[string]domain.Dimension, len(dims))
	for _, d := range dims {
		dimIdx[d.Name] = d
	}
	var out []feedback.AttrFilter
	for k, vs := range q {
		d, ok := dimIdx[k]
		if !ok {
			continue
		}
		for _, v := range vs {
			if v == "" {
				continue
			}
			out = append(out, feedback.AttrFilter{
				Dim:   d.Name,
				Value: v,
				Multi: d.Kind == domain.DimMulti,
			})
		}
	}
	return out
}

// Get handles GET /fb/v1/console/feedback/{id}.
func (h *FeedbackHandler) Get(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.Get"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id_str:%s",
			where, auth.TenantID, idStr)
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id must be an integer")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%d", where, auth.TenantID, id)
	row, err := h.repo.GetForConsole(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, feedback.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d",
				where, auth.TenantID, id)
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "feedback not found or not owned by tenant")
			return
		}
		logext.Errorf(ctx, "[%s] feedback.GetForConsole failed,tenant_id:%s,id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load feedback detail")
		return
	}

	f := toProtoFeedback(row.ConsoleListRow)
	detail := ptrext.Of(attunev1.FeedbackDetail{
		Id:                f.GetId(),
		Content:           f.GetContent(),
		Source:            f.GetSource(),
		Type:              f.GetType(),
		UserId:            f.GetUserId(),
		PageUrl:           f.GetPageUrl(),
		EnrichedTitle:     f.EnrichedTitle,
		EnrichedAttrs:     f.GetEnrichedAttrs(),
		IsUrgent:          f.GetIsUrgent(),
		EnrichmentStatus:  f.GetEnrichmentStatus(),
		CreatedAt:         f.GetCreatedAt(),
		EnrichmentError:   nullableString(row.EnrichmentError),
		EnrichedRationale: nullableString(row.EnrichedRationale),
	})
	if len(row.SourceMeta) > 0 {
		var m map[string]any
		if json.Unmarshal(row.SourceMeta, &m) == nil {
			if s, err := structpb.NewStruct(m); err == nil {
				detail.SourceMeta = s
			}
		}
	}
	if len(row.Attachments) > 0 {
		var atts []struct {
			URL  string  `json:"url"`
			Mime *string `json:"mime"`
			Size *int64  `json:"size"`
		}
		if json.Unmarshal(row.Attachments, &atts) == nil {
			for _, a := range atts {
				detail.Attachments = append(detail.Attachments, ptrext.Of(attunev1.Attachment{
					Url:  a.URL,
					Mime: a.Mime,
					Size: a.Size,
				}))
			}
		}
	}
	if row.EnrichedAt != nil {
		detail.EnrichedAt = ptrext.Of(row.EnrichedAt.UTC().Format(time.RFC3339))
	}
	respond.Proto(w, http.StatusOK, detail)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
}

// Stats handles GET /fb/v1/console/feedback/stats. Window fixed to the current
// calendar month (UTC) — matches /usage's semantics.
func (h *FeedbackHandler) Stats(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.Stats"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	cfg, err := h.tenants.GetEnrichConfig(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] read dim cfg failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read tenant config")
		return
	}

	// Total ingest count for the window — sum the daily buckets.
	var totalIngest int64
	if rows, err := h.repo.UsageByDay(ctx, auth.TenantID, from, to); err == nil {
		for _, b := range rows {
			totalIngest += b.Value
		}
	}

	urgent, err := h.repo.UrgentCount(ctx, auth.TenantID, from, to)
	if err != nil {
		logext.Errorf(ctx, "[%s] urgent count failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read urgent stats")
		return
	}

	dims := make([]*attunev1.DimStats, 0, len(cfg.Dimensions))
	for _, d := range cfg.Dimensions {
		top, err := h.repo.TopValuesByDim(ctx, auth.TenantID, d.Name, d.Kind == domain.DimMulti, from, to, 5)
		if err != nil {
			logext.Warnf(ctx, "[%s] top values failed,tenant_id:%s,dim:%s,err:%+v",
				where, auth.TenantID, d.Name, err.Error())
			continue
		}
		bucket := ptrext.Of(attunev1.DimStats{Dim: d.Name})
		for _, v := range top {
			bucket.Top = append(bucket.Top, ptrext.Of(attunev1.ValueCount{Value: v.Value, Count: v.Count}))
		}
		dims = append(dims, bucket)
	}

	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.GetFeedbackStatsResponse{
		PeriodStart: from.Format(time.RFC3339),
		PeriodEnd:   to.Format(time.RFC3339),
		Total:       totalIngest,
		Dims:        dims,
		UrgentCount: urgent,
	}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d,urgent:%d,dims:%d",
		where, auth.TenantID, totalIngest, urgent, len(dims))
}
