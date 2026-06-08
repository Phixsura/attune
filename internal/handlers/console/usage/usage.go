package usage

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// UsageHandler serves GET /fb/v1/console/usage. Today's handler ships only the
// current calendar month — no granularity/range params. The proto-defined
// `granularity` + `range` fields are silently ignored until billing surfaces
// real choices.
type UsageHandler struct {
	repo *feedback.FeedbackRepo
}

func NewUsageHandler(r *feedback.FeedbackRepo) *UsageHandler {
	return ptrext.Of(UsageHandler{repo: r})
}

// ServeHTTP handles GET /fb/v1/console/usage.
func (h *UsageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const where = "console.UsageHandler.ServeHTTP"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := now
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	buckets, err := h.repo.UsageByDay(ctx, auth.TenantID, periodStart, periodEnd)
	if err != nil {
		logext.Errorf(ctx, "[%s] feedback.UsageByDay failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to read usage")
		return
	}

	series := make([]*attunev1.UsageBucket, 0, len(buckets))
	var total int64
	for _, b := range buckets {
		series = append(series, ptrext.Of(attunev1.UsageBucket{
			Bucket: b.Bucket.UTC().Format(time.RFC3339),
			Value:  b.Value,
		}))
		total += b.Value
	}

	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.GetUsageResponse{
		PeriodStart: periodStart.Format(time.RFC3339),
		PeriodEnd:   periodEnd.Format(time.RFC3339),
		Total:       total,
		Series:      series,
		Quota:       nil, // null until billing lands
	}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d,buckets:%d",
		where, auth.TenantID, total, len(series))
}
