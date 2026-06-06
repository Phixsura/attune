package console

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo"
)

// FeedbackHandler serves /fb/v1/console/feedback. All queries scope to the
// session's tenant via FromContext — never taking tenant_id from query params.
type FeedbackHandler struct {
	repo *repo.FeedbackRepo
}

func NewFeedbackHandler(r *repo.FeedbackRepo) *FeedbackHandler {
	return &FeedbackHandler{repo: r}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func modulesOf(raw json.RawMessage) []string {
	var modules []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &modules)
	}
	return modules
}

func toProtoFeedback(row repo.ConsoleListRow) *attunev1.Feedback {
	return &attunev1.Feedback{
		Id:               row.ID,
		Content:          row.Content,
		Source:           row.Source,
		Type:             row.Type,
		UserId:           row.UserID,
		PageUrl:          row.PageURL,
		EnrichedTitle:    nullableString(row.EnrichedTitle),
		EnrichedKind:     nullableString(row.EnrichedKind),
		EnrichedSeverity: nullableString(row.EnrichedSeverity),
		EnrichedModules:  modulesOf(row.EnrichedModules),
		EnrichedPriority: row.EnrichedPriority,
		EnrichmentStatus: row.EnrichmentStatus,
		CreatedAt:        row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// List handles GET /fb/v1/console/feedback.
func (h *FeedbackHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.List"
	ctx := r.Context()
	auth := FromContext(ctx)
	q := r.URL.Query()
	opts := repo.ConsoleListOpts{
		Kind:     q.Get("kind"),
		Severity: q.Get("severity"),
		Q:        q.Get("q"),
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
	logext.Infof(ctx, "[%s] start,tenant_id:%s,kind:%s,severity:%s,limit:%d,cursor:%d",
		where, auth.TenantID, opts.Kind, opts.Severity, opts.Limit, opts.Cursor)

	rows, err := h.repo.ListForConsole(ctx, auth.TenantID, opts)
	if err != nil {
		slog.ErrorContext(ctx, "feedback list", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.ListForConsole failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "查询反馈失败")
		return
	}
	items := make([]*attunev1.Feedback, 0, len(rows))
	for _, row := range rows {
		items = append(items, toProtoFeedback(row))
	}
	resp := &attunev1.ListFeedbackResponse{Items: items}
	// next_cursor = last row's id when this page was full (more may remain).
	if opts.Limit > 0 && len(rows) == opts.Limit {
		s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		resp.NextCursor = &s
	}
	respondProto(w, http.StatusOK, resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// Get handles GET /fb/v1/console/feedback/{id}.
func (h *FeedbackHandler) Get(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.Get"
	ctx := r.Context()
	auth := FromContext(ctx)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id_str:%s",
			where, auth.TenantID, idStr)
		respondError(ctx, w, http.StatusBadRequest, "bad_id", "id 必须是整数")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%d", where, auth.TenantID, id)
	row, err := h.repo.GetForConsole(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, repo.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d",
				where, auth.TenantID, id)
			respondError(ctx, w, http.StatusNotFound, "not_found", "反馈不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "feedback detail", "err", err, "id", id)
		logext.Errorf(ctx, "[%s] repo.GetForConsole failed,tenant_id:%s,id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "加载反馈详情失败")
		return
	}

	f := toProtoFeedback(row.ConsoleListRow)
	detail := &attunev1.FeedbackDetail{
		Id:               f.GetId(),
		Content:          f.GetContent(),
		Source:           f.GetSource(),
		Type:             f.GetType(),
		UserId:           f.GetUserId(),
		PageUrl:          f.GetPageUrl(),
		EnrichedTitle:    f.EnrichedTitle,
		EnrichedKind:     f.EnrichedKind,
		EnrichedSeverity: f.EnrichedSeverity,
		EnrichedModules:  f.GetEnrichedModules(),
		EnrichedPriority: f.EnrichedPriority,
		EnrichmentStatus: f.GetEnrichmentStatus(),
		CreatedAt:        f.GetCreatedAt(),
		EnrichmentError:  nullableString(row.EnrichmentError),
	}
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
				detail.Attachments = append(detail.Attachments, &attunev1.Attachment{
					Url:  a.URL,
					Mime: a.Mime,
					Size: a.Size,
				})
			}
		}
	}
	if row.EnrichedAt != nil {
		s := row.EnrichedAt.UTC().Format(time.RFC3339)
		detail.EnrichedAt = &s
	}
	respondProto(w, http.StatusOK, detail)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
}

// Stats handles GET /fb/v1/console/feedback/stats. Window fixed to the current
// calendar month (UTC) — matches /usage's semantics.
func (h *FeedbackHandler) Stats(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.Stats"
	ctx := r.Context()
	auth := FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now
	counts, err := h.repo.KindCounts(ctx, auth.TenantID, from, to)
	if err != nil {
		slog.ErrorContext(ctx, "feedback stats", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.KindCounts failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "查询反馈分布失败")
		return
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	if counts == nil {
		counts = map[string]int64{}
	}
	respondProto(w, http.StatusOK, &attunev1.GetFeedbackStatsResponse{
		PeriodStart: from.Format(time.RFC3339),
		PeriodEnd:   to.Format(time.RFC3339),
		Total:       total,
		ByKind:      counts,
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d", where, auth.TenantID, total)
}
