package feedback

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Stats handles GET /fb/v1/console/feedback/stats. Window fixed to the current
// calendar month (UTC) — matches /usage's semantics.
func (h *FeedbackHandler) Stats(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetFeedbackStatsRequest) (dispatcher.Result[*attunev1.GetFeedbackStatsResponse], error) {
	const where = "console.FeedbackHandler.Stats"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	cfg, err := h.tenants.GetEnrichConfig(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] read dim cfg failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Result[*attunev1.GetFeedbackStatsResponse]{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read tenant config")
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
		return dispatcher.Result[*attunev1.GetFeedbackStatsResponse]{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read urgent stats")
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

	resp := ptrext.Of(attunev1.GetFeedbackStatsResponse{
		PeriodStart: from.Format(time.RFC3339),
		PeriodEnd:   to.Format(time.RFC3339),
		Total:       totalIngest,
		Dims:        dims,
		UrgentCount: urgent,
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d,urgent:%d,dims:%d",
		where, auth.TenantID, totalIngest, urgent, len(dims))
	return dispatcher.OK(http.StatusOK, resp), nil
}
