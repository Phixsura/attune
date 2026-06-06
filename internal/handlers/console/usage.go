package console

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo"
)

// UsageHandler serves GET /fb/v1/console/usage. Phase 1 ships only the
// current calendar month — no granularity/range params. The proto-defined
// `granularity` + `range` fields are silently ignored until billing surfaces
// real choices.
type UsageHandler struct {
	repo *repo.FeedbackRepo
}

func NewUsageHandler(r *repo.FeedbackRepo) *UsageHandler {
	return &UsageHandler{repo: r}
}

// ServeHTTP handles GET /fb/v1/console/usage.
func (h *UsageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const where = "console.UsageHandler.ServeHTTP"
	ctx := r.Context()
	auth := FromContext(ctx)
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := now
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	buckets, err := h.repo.UsageByDay(ctx, auth.TenantID, periodStart, periodEnd)
	if err != nil {
		slog.ErrorContext(ctx, "usage", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.UsageByDay failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respondError(ctx, w, http.StatusInternalServerError, "internal", "查询用量失败")
		return
	}

	series := make([]*attunev1.UsageBucket, 0, len(buckets))
	var total int64
	for _, b := range buckets {
		series = append(series, &attunev1.UsageBucket{
			Bucket: b.Bucket.UTC().Format(time.RFC3339),
			Value:  b.Value,
		})
		total += b.Value
	}

	respondProto(w, http.StatusOK, &attunev1.GetUsageResponse{
		PeriodStart: periodStart.Format(time.RFC3339),
		PeriodEnd:   periodEnd.Format(time.RFC3339),
		Total:       total,
		Series:      series,
		Quota:       nil, // null until billing lands
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d,buckets:%d",
		where, auth.TenantID, total, len(series))
}
