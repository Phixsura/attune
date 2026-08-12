// SPDX-License-Identifier: Apache-2.0

// Package anomaly holds the console handlers for anomaly & spike detection
// (#237): event listing, series replay with expected bands, evidence
// drilldown, and tenant detection configuration.
package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	anomalysvc "github.com/Phixsura/attune/internal/service/anomaly"
)

const (
	defaultSeriesDays = 90
	maxSeriesDays     = 180
	// seriesMinBaselinePoints mirrors the worker's gate so chart verdicts
	// replay identically.
	seriesMinBaselinePoints = 4
	defaultListLimit        = 50
	maxListLimit            = 200
	maxCustomSlices         = 20
	maxConditions           = 3
	maxConditionVals        = 10
)

// store is the repo slice these handlers consume.
type store interface {
	ListEventsBySliceType(ctx context.Context, tenantID, status, sliceType string, limit int) ([]anomalyrepo.Event, error)
	FilterLiveFeedbackIDs(ctx context.Context, tenantID string, ids []int64) ([]int64, error)
	GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*anomalyrepo.Event, error)
	BaselineCounts(ctx context.Context, tenantID, sliceType, sliceKey string, dates []time.Time) ([]int64, error)
	GetConfig(ctx context.Context, tenantID string) (anomalyrepo.Config, error)
	UpsertConfig(ctx context.Context, cfg anomalyrepo.Config, updatedBy string) error
	ListCustomSlices(ctx context.Context, tenantID string) ([]anomalyrepo.StoredCustomSlice, error)
	ReplaceCustomSlices(ctx context.Context, tenantID string, slices []anomalyrepo.StoredCustomSlice) error
}

// tenantReader resolves the tenant timezone for series date math.
type tenantReader interface {
	GetTimezone(ctx context.Context, tenantID string) (string, error)
}

// Handler serves the AnomalyService console routes.
type Handler struct {
	store   store
	tenants tenantReader
	audit   auditRecorder
}

// NewHandler wires the anomaly console handler.
func NewHandler(s store, tenants tenantReader) *Handler {
	return ptrext.Of(Handler{store: s, tenants: tenants})
}

// ── ListAnomalies ─────────────────────────────────────────────────────────

// BindListAnomaliesRequest parses query params for ListAnomalies.
func BindListAnomaliesRequest(r *http.Request, req *attunev1.ListAnomaliesRequest) error {
	q := r.URL.Query()
	req.Status = q.Get("status")
	req.SliceType = q.Get("slice_type")
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be an integer")
		}
		req.Limit = ptrext.Of(int32(v))
	}
	return nil
}

// ListAnomalies returns events filtered by status and slice type.
func (h *Handler) ListAnomalies(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListAnomaliesRequest,
) (dispatcher.Result[*attunev1.ListAnomaliesResponse], error) {
	const where = "console.AnomalyHandler.ListAnomalies"
	if err := validateStatusFilter(req.GetStatus()); err != nil {
		return dispatcher.Fail[*attunev1.ListAnomaliesResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	if err := validateSliceTypeFilter(req.GetSliceType()); err != nil {
		return dispatcher.Fail[*attunev1.ListAnomaliesResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	events, err := h.store.ListEventsBySliceType(ctx, ctx.Auth.TenantID, req.GetStatus(), req.GetSliceType(), limit)
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListAnomaliesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read anomalies")
	}
	out := make([]*attunev1.AnomalyEvent, 0, len(events))
	for i := range events {
		out = append(out, eventToProto(&events[i]))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListAnomaliesResponse{Events: out}))
}

// ── GetAnomalySeries ──────────────────────────────────────────────────────

// BindGetAnomalySeriesRequest parses query params for GetAnomalySeries.
func BindGetAnomalySeriesRequest(r *http.Request, req *attunev1.GetAnomalySeriesRequest) error {
	q := r.URL.Query()
	req.SliceType = q.Get("slice_type")
	req.SliceKey = q.Get("slice_key")
	if raw := q.Get("days"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "days must be an integer")
		}
		req.Days = ptrext.Of(int32(v))
	}
	return nil
}

// GetAnomalySeries replays the detector over the requested window so chart
// bands and alert verdicts can never disagree.
func (h *Handler) GetAnomalySeries(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetAnomalySeriesRequest,
) (dispatcher.Result[*attunev1.GetAnomalySeriesResponse], error) {
	const where = "console.AnomalyHandler.GetAnomalySeries"
	if err := validateSliceRef(req.GetSliceType(), req.GetSliceKey()); err != nil {
		return dispatcher.Fail[*attunev1.GetAnomalySeriesResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	days := int(req.GetDays())
	if days <= 0 {
		days = defaultSeriesDays
	}
	if days > maxSeriesDays {
		return dispatcher.Fail[*attunev1.GetAnomalySeriesResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
			fmt.Sprintf("days must be at most %d", maxSeriesDays))
	}
	loc := h.tenantLocation(ctx, ctx.Auth.TenantID)
	cfg, err := h.store.GetConfig(ctx, ctx.Auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] config failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetAnomalySeriesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read config")
	}
	points, display, err := h.replaySeries(ctx, ctx.Auth.TenantID, req.GetSliceType(), req.GetSliceKey(), days, loc, cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] replay failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetAnomalySeriesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to build series")
	}
	return dispatcher.OK(ptrext.Of(attunev1.GetAnomalySeriesResponse{
		Points: points, SliceDisplay: display,
	}))
}

// replaySeries builds per-day points by running the same Detect the worker
// uses over each day's same-weekday baseline. The full window plus its
// deepest baseline (days + 56) is fetched in ONE BaselineCounts call and
// replayed from memory — the previous per-day reads issued 2 queries per
// point (~180 for the default 90-day window).
func (h *Handler) replaySeries(
	ctx context.Context, tenantID, sliceType, sliceKey string,
	days int, loc *time.Location, cfg anomalyrepo.Config,
) ([]*attunev1.SeriesPoint, string, error) {
	detCfg := anomalysvc.DetectorConfig{
		ZThreshold:        anomalysvc.ZThresholdFor(cfg.Sensitivity),
		MinCount:          int64(cfg.MinCount),
		MinBaselinePoints: seriesMinBaselinePoints,
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// One contiguous span: [today − (days−1) − 56, today].
	spanDays := days + 7*8
	dates := make([]time.Time, 0, spanDays)
	for i := spanDays - 1; i >= 0; i-- {
		dates = append(dates, today.AddDate(0, 0, -i))
	}
	counts, err := h.store.BaselineCounts(ctx, tenantID, sliceType, sliceKey, dates)
	if err != nil {
		return nil, "", err
	}
	countAt := make(map[string]int64, len(dates))
	for i, d := range dates {
		countAt[d.Format("2006-01-02")] = counts[i]
	}

	points := make([]*attunev1.SeriesPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		baseline := make([]int64, 0, 8)
		for week := 8; week >= 1; week-- {
			baseline = append(baseline, countAt[day.AddDate(0, 0, -7*week).Format("2006-01-02")])
		}
		observed := countAt[day.Format("2006-01-02")]
		verdict := anomalysvc.Detect(baseline, observed, detCfg)
		points = append(points, ptrext.Of(attunev1.SeriesPoint{
			Date:         day.Format("2006-01-02"),
			Count:        observed,
			ExpectedMed:  verdict.ExpectedMed,
			ExpectedLow:  verdict.ExpectedLow,
			ExpectedHigh: verdict.ExpectedHigh,
			IsAnomalous:  verdict.Direction != "",
			Insufficient: verdict.Insufficient,
		}))
	}
	return points, sliceKey, nil
}

// ── GetAnomalyEvidence ────────────────────────────────────────────────────

// BindGetAnomalyEvidenceRequest parses the path param.
func BindGetAnomalyEvidenceRequest(r *http.Request, req *attunev1.GetAnomalyEvidenceRequest) error {
	req.EventId = r.PathValue("event_id")
	return nil
}

// GetAnomalyEvidence returns the stored contribution breakdown and live
// sample feedback ids for one event.
func (h *Handler) GetAnomalyEvidence(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetAnomalyEvidenceRequest,
) (dispatcher.Result[*attunev1.GetAnomalyEvidenceResponse], error) {
	const where = "console.AnomalyHandler.GetAnomalyEvidence"
	id, err := uuid.Parse(req.GetEventId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetAnomalyEvidenceResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "event_id must be a UUID")
	}
	event, err := h.store.GetEvent(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return dispatcher.Fail[*attunev1.GetAnomalyEvidenceResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "anomaly event not found")
	}
	var doc struct {
		SampleIDs    []int64 `json:"sample_ids"`
		Contribution []struct {
			Dim   string  `json:"dim"`
			Value string  `json:"value"`
			Share float64 `json:"share"`
		} `json:"contribution"`
		Spread bool `json:"spread"`
	}
	if err := json.Unmarshal([]byte(event.EvidenceJSON), &doc); err != nil {
		logext.Warnf(ctx, "[%s] evidence decode failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
	}
	contributions := make([]*attunev1.ContributionEntry, 0, len(doc.Contribution))
	for _, c := range doc.Contribution {
		contributions = append(contributions, ptrext.Of(attunev1.ContributionEntry{
			Dim: c.Dim, Value: c.Value, Share: c.Share,
		}))
	}
	// Evidence samples must reflect live rows only: GDPR-deleted feedback
	// ids stored at detection time are filtered out at read time.
	liveIDs, err := h.store.FilterLiveFeedbackIDs(ctx, ctx.Auth.TenantID, doc.SampleIDs)
	if err != nil {
		logext.Warnf(ctx, "[%s] live-id filter failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		liveIDs = nil // fail closed: no ids rather than possibly-dead ids
	}
	return dispatcher.OK(ptrext.Of(attunev1.GetAnomalyEvidenceResponse{
		Contributions: contributions,
		Spread:        doc.Spread,
		FeedbackIds:   liveIDs,
	}))
}

// ── helpers ───────────────────────────────────────────────────────────────

func (h *Handler) tenantLocation(ctx context.Context, tenantID string) *time.Location {
	tz, err := h.tenants.GetTimezone(ctx, tenantID)
	if err != nil {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func eventToProto(e *anomalyrepo.Event) *attunev1.AnomalyEvent {
	resolvedAt := ""
	if e.ResolvedAt != nil {
		resolvedAt = e.ResolvedAt.UTC().Format(time.RFC3339)
	}
	return ptrext.Of(attunev1.AnomalyEvent{
		EventId:         e.ID.String(),
		SliceType:       e.SliceType,
		SliceKey:        e.SliceKey,
		SliceDisplay:    e.SliceDisplay,
		Direction:       e.Direction,
		FirstBucketDate: e.FirstBucketDate.Format("2006-01-02"),
		LastBucketDate:  e.LastBucketDate.Format("2006-01-02"),
		Observed:        e.Observed,
		ExpectedMed:     e.ExpectedMed,
		ExpectedLow:     e.ExpectedLow,
		ExpectedHigh:    e.ExpectedHigh,
		ZScore:          e.ZScore,
		Status:          e.Status,
		CreatedAt:       e.CreatedAt.UTC().Format(time.RFC3339),
		ResolvedAt:      resolvedAt,
	})
}

func validateStatusFilter(status string) error {
	switch status {
	case "", "open", "resolved", "retracted":
		return nil
	}
	return fmt.Errorf("status must be open, resolved, or retracted")
}

func validateSliceTypeFilter(t string) error {
	if t == "" {
		return nil
	}
	for _, v := range anomalyrepo.AllSliceTypes() {
		if v == t {
			return nil
		}
	}
	return fmt.Errorf("unknown slice_type %q", t)
}

func validateSliceRef(sliceType, sliceKey string) error {
	if sliceType == "" || sliceKey == "" {
		return fmt.Errorf("slice_type and slice_key are required")
	}
	if err := validateSliceTypeFilter(sliceType); err != nil {
		return err
	}
	if len(sliceKey) > 120 {
		return fmt.Errorf("slice_key too long")
	}
	return nil
}
