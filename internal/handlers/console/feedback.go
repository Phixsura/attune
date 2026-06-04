package console

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/repo"
)

// FeedbackHandler serves /fb/v1/console/feedback.
// All queries scope to the session's tenant via FromContext — never
// taking tenant_id from query params.
type FeedbackHandler struct {
	repo *repo.FeedbackRepo
}

func NewFeedbackHandler(r *repo.FeedbackRepo) *FeedbackHandler {
	return &FeedbackHandler{repo: r}
}

// feedbackListDTO matches openapi.yaml `Feedback`.
type feedbackListDTO struct {
	ID               int64    `json:"id"`
	Content          string   `json:"content"`
	Source           string   `json:"source"`
	Type             string   `json:"type"`
	UserID           string   `json:"user_id"`
	PageURL          string   `json:"page_url"`
	EnrichedTitle    *string  `json:"enriched_title"`
	EnrichedKind     *string  `json:"enriched_kind"`
	EnrichedSeverity *string  `json:"enriched_severity"`
	EnrichedModules  []string `json:"enriched_modules"`
	EnrichedPriority *float64 `json:"enriched_priority"`
	EnrichmentStatus string   `json:"enrichment_status"`
	CreatedAt        string   `json:"created_at"`
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toListDTO(row repo.ConsoleListRow) feedbackListDTO {
	var modules []string
	if len(row.EnrichedModules) > 0 {
		_ = json.Unmarshal(row.EnrichedModules, &modules)
	}
	return feedbackListDTO{
		ID:               row.ID,
		Content:          row.Content,
		Source:           row.Source,
		Type:             row.Type,
		UserID:           row.UserID,
		PageURL:          row.PageURL,
		EnrichedTitle:    nullableString(row.EnrichedTitle),
		EnrichedKind:     nullableString(row.EnrichedKind),
		EnrichedSeverity: nullableString(row.EnrichedSeverity),
		EnrichedModules:  modules,
		EnrichedPriority: row.EnrichedPriority,
		EnrichmentStatus: row.EnrichmentStatus,
		CreatedAt:        row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// feedbackDetailDTO matches openapi.yaml `FeedbackDetail` (Feedback + extras).
type feedbackDetailDTO struct {
	feedbackListDTO
	SourceMeta      map[string]any `json:"source_meta"`
	Attachments     []any          `json:"attachments"`
	EnrichmentError *string        `json:"enrichment_error"`
	EnrichedAt      *string        `json:"enriched_at"`
}

// List handles GET /fb/v1/console/feedback.
func (h *FeedbackHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.List"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
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

	rows, err := h.repo.ListForConsole(r.Context(), auth.TenantID, opts)
	if err != nil {
		slog.ErrorContext(ctx, "feedback list", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.ListForConsole failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "查询反馈失败")
		return
	}
	items := make([]feedbackListDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toListDTO(row))
	}
	// next_cursor = last row's id (so client passes ?cursor=<id> next)
	// or null when this page wasn't full (no more rows to fetch).
	var nextCursor *string
	if len(rows) > 0 && (opts.Limit > 0 && len(rows) == opts.Limit) {
		s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		nextCursor = &s
	}
	resp := map[string]any{
		"items":       items,
		"next_cursor": nextCursor,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// Get handles GET /fb/v1/console/feedback/{id}.
func (h *FeedbackHandler) Get(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.Get"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id_str:%s",
			where, auth.TenantID, idStr)
		writeError(w, http.StatusBadRequest, "bad_id", "id 必须是整数")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%d", where, auth.TenantID, id)
	row, err := h.repo.GetForConsole(r.Context(), auth.TenantID, id)
	if err != nil {
		if errors.Is(err, repo.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d",
				where, auth.TenantID, id)
			writeError(w, http.StatusNotFound, "not_found", "反馈不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "feedback detail", "err", err, "id", id)
		logext.Errorf(ctx, "[%s] repo.GetForConsole failed,tenant_id:%s,id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "加载反馈详情失败")
		return
	}
	dto := feedbackDetailDTO{feedbackListDTO: toListDTO(row.ConsoleListRow)}
	if len(row.SourceMeta) > 0 {
		_ = json.Unmarshal(row.SourceMeta, &dto.SourceMeta)
	}
	if len(row.Attachments) > 0 {
		_ = json.Unmarshal(row.Attachments, &dto.Attachments)
	}
	if row.EnrichmentError != "" {
		dto.EnrichmentError = &row.EnrichmentError
	}
	if row.EnrichedAt != nil {
		s := row.EnrichedAt.UTC().Format(time.RFC3339)
		dto.EnrichedAt = &s
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(dto)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
}

// statsResponse mirrors openapi /feedback/stats. by_kind is a flat
// map so SPA donut can sort + iterate without knowing the schema enum.
type statsResponse struct {
	PeriodStart string           `json:"period_start"`
	PeriodEnd   string           `json:"period_end"`
	Total       int64            `json:"total"`
	ByKind      map[string]int64 `json:"by_kind"`
}

// Stats handles GET /fb/v1/console/feedback/stats. Phase 1.5 Layer 2:
// powers the donut on the feedback page. Window fixed to current
// calendar month (UTC) — matches /usage's semantics.
func (h *FeedbackHandler) Stats(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.Stats"
	ctx := r.Context()
	auth := FromContext(ctx)
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now
	counts, err := h.repo.KindCounts(ctx, auth.TenantID, from, to)
	if err != nil {
		slog.ErrorContext(ctx, "feedback stats", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.KindCounts failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "查询反馈分布失败")
		return
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	if counts == nil {
		counts = map[string]int64{}
	}
	resp := statsResponse{
		PeriodStart: from.Format(time.RFC3339),
		PeriodEnd:   to.Format(time.RFC3339),
		Total:       total,
		ByKind:      counts,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d", where, auth.TenantID, total)
}
