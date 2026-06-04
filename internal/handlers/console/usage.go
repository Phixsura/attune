package console

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/repo"
)

// UsageHandler serves GET /fb/v1/console/usage. Phase 1 ships only the
// current calendar month — no granularity/range params. The yaml-defined
// `granularity` + `range` parameters are silently ignored until Wave 3
// billing surfaces real choices.
type UsageHandler struct {
	repo *repo.FeedbackRepo
}

func NewUsageHandler(r *repo.FeedbackRepo) *UsageHandler {
	return &UsageHandler{repo: r}
}

type usageBucketDTO struct {
	Bucket string `json:"bucket"`
	Value  int64  `json:"value"`
}

type usageResponse struct {
	PeriodStart string           `json:"period_start"`
	PeriodEnd   string           `json:"period_end"`
	Total       int64            `json:"total"`
	Series      []usageBucketDTO `json:"series"`
	Quota       *int64           `json:"quota"` // null until billing lands
}

// ServeHTTP handles GET /fb/v1/console/usage.
func (h *UsageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const where = "console.UsageHandler.ServeHTTP"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := now
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	buckets, err := h.repo.UsageByDay(r.Context(), auth.TenantID, periodStart, periodEnd)
	if err != nil {
		slog.ErrorContext(ctx, "usage", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.UsageByDay failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "查询用量失败")
		return
	}
	series := make([]usageBucketDTO, 0, len(buckets))
	var total int64
	for _, b := range buckets {
		series = append(series, usageBucketDTO{
			Bucket: b.Bucket.UTC().Format(time.RFC3339),
			Value:  b.Value,
		})
		total += b.Value
	}

	resp := usageResponse{
		PeriodStart: periodStart.Format(time.RFC3339),
		PeriodEnd:   periodEnd.Format(time.RFC3339),
		Total:       total,
		Series:      series,
		Quota:       nil,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d,buckets:%d",
		where, auth.TenantID, total, len(series))
}
