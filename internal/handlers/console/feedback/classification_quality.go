package feedback

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

const (
	qualityDefaultCurrentDays  = 7
	qualityDefaultBaselineDays = 28
	qualityMaxWindowDays       = 90
	qualityDefaultLimit        = 10
	qualityMaxLimit            = 50
	qualityMinDimensionVolume  = 30
	qualityWatchJSDistance     = 0.10
	qualityAlertJSDistance     = 0.20
	qualityWatchShareDeltaPP   = 10.0
	qualityAlertShareDeltaPP   = 20.0
	qualityWatchLowConfDeltaPP = 5.0
	qualityAlertLowConfDeltaPP = 10.0
)

type qualityWindow struct {
	currentFrom  time.Time
	currentTo    time.Time
	baselineFrom time.Time
	baselineTo   time.Time
	bucketWidth  string
	threshold    float64
	limit        int
}

type qualityBounds struct {
	currentFrom  time.Time
	currentTo    time.Time
	baselineFrom time.Time
	baselineTo   time.Time
}

type dimensionKindLookup map[string]domain.DimensionKind

type dimensionQuality struct {
	all         *feedback.ClassificationQualityValueAggregate
	values      map[string]feedback.ClassificationQualityValueAggregate
	total       int64
	appearances int64
	kind        domain.DimensionKind
}

func BindClassificationQualityRequest(r *http.Request, req *attunev1.GetClassificationQualityRequest) error {
	q := r.URL.Query()
	req.CurrentFrom = q.Get("current_from")
	req.CurrentTo = q.Get("current_to")
	req.BaselineFrom = q.Get("baseline_from")
	req.BaselineTo = q.Get("baseline_to")
	req.BucketWidth = q.Get("bucket_width")
	req.Source = q.Get("source")
	req.LogicalModel = q.Get("logical_model")
	req.ProviderModel = q.Get("provider_model")
	req.ChannelId = q.Get("channel_id")
	req.DimensionName = q.Get("dimension_name")
	req.Severity = q.Get("severity")
	if raw := q.Get("low_confidence_threshold"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "low_confidence_threshold must be a number")
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "low_confidence_threshold must be finite")
		}
		req.LowConfidenceThreshold = v
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be an integer")
		}
		req.Limit = int32(v)
	}
	return nil
}

func BindClassificationQualitySamplesRequest(r *http.Request, req *attunev1.GetClassificationQualitySamplesRequest) error {
	for _, raw := range r.URL.Query()["ids"] {
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			id, err := strconv.ParseInt(token, 10, 64)
			if err != nil {
				return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "ids must contain integers")
			}
			if id <= 0 {
				return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "ids must contain positive integers")
			}
			req.Ids = append(req.Ids, id)
		}
	}
	if len(req.Ids) > qualityMaxLimit {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "ids is limited to 50 values")
	}
	return nil
}

func (h *FeedbackHandler) GetClassificationQuality(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetClassificationQualityRequest,
) (dispatcher.Result[*attunev1.GetClassificationQualityResponse], error) {
	const where = "console.FeedbackHandler.GetClassificationQuality"
	auth := ctx.Auth
	window, err := resolveQualityWindow(req, time.Now().UTC())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetClassificationQualityResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	cfg, err := h.tenants.GetEnrichConfig(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] read tenant config failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetClassificationQualityResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read tenant config")
	}
	refreshFrom := minTime(window.currentFrom, window.baselineFrom)
	refreshTo := maxTime(window.currentTo, window.baselineTo)
	if err := h.repo.RefreshClassificationQuality(ctx, feedback.ClassificationQualityRefreshOpts{
		TenantID:               auth.TenantID,
		From:                   refreshFrom,
		To:                     refreshTo,
		BucketWidth:            window.bucketWidth,
		LowConfidenceThreshold: window.threshold,
	}); err != nil {
		logext.Errorf(ctx, "[%s] refresh failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetClassificationQualityResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to refresh classification quality")
	}
	current, baseline, series, err := h.readQualityAggregates(ctx, auth.TenantID, req, window)
	if err != nil {
		logext.Errorf(ctx, "[%s] aggregate read failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetClassificationQualityResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read classification quality")
	}
	dims := buildDimensionDrift(current.values, baseline.values, dimensionKinds(cfg.Dimensions), req.GetDimensionName(), req.GetSeverity(), window.limit)
	warnings := buildQualityWarnings(current.signal, dims)
	samples := h.qualitySamplesForResponse(ctx, auth.TenantID, sampleIDsFromQuality(dims, warnings))
	resp := qualityResponse(window, current.signal, series, dims, warnings, samples, refreshTo)
	recordClassificationQualityMetrics(auth.TenantID, cfg.Dimensions, resp.GetSummary(), dims, warnings)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,events:%d,dimensions:%d,warnings:%d",
		where, auth.TenantID, resp.Summary.GetClassificationEvents(), len(resp.Dimensions), len(resp.Warnings))
	return dispatcher.OK(resp)
}

func (h *FeedbackHandler) GetClassificationQualitySamples(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetClassificationQualitySamplesRequest,
) (dispatcher.Result[*attunev1.GetClassificationQualitySamplesResponse], error) {
	auth := ctx.Auth
	samples, err := h.repo.ClassificationQualitySamples(ctx, auth.TenantID, req.GetIds())
	if err != nil {
		logext.Errorf(ctx, "[console.FeedbackHandler.GetClassificationQualitySamples] query failed,tenant_id:%s,err:%+v", auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetClassificationQualitySamplesResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to read classification quality samples")
	}
	return dispatcher.OK(ptrext.Of(attunev1.GetClassificationQualitySamplesResponse{
		Samples: qualitySamplesToProto(samples, ""),
	}))
}

type qualityAggregateRead struct {
	signal feedback.ClassificationQualitySignalAggregate
	values []feedback.ClassificationQualityValueAggregate
}

func (h *FeedbackHandler) readQualityAggregates(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	req *attunev1.GetClassificationQualityRequest,
	window qualityWindow,
) (current qualityAggregateRead, baseline qualityAggregateRead, series []feedback.ClassificationQualitySeriesBucket, err error) {
	currentOpts := qualityQueryOpts(tenantID, req, window.currentFrom, window.currentTo, window.bucketWidth)
	baselineOpts := qualityQueryOpts(tenantID, req, window.baselineFrom, window.baselineTo, window.bucketWidth)
	current.signal, current.values, err = h.repo.ClassificationQualityAggregates(ctx, currentOpts)
	if err != nil {
		return current, baseline, nil, err
	}
	baseline.signal, baseline.values, err = h.repo.ClassificationQualityAggregates(ctx, baselineOpts)
	if err != nil {
		return current, baseline, nil, err
	}
	series, err = h.repo.ClassificationQualitySeries(ctx, currentOpts)
	return current, baseline, series, err
}

func qualityQueryOpts(tenantID string, req *attunev1.GetClassificationQualityRequest, from, to time.Time, width string) feedback.ClassificationQualityQueryOpts {
	return feedback.ClassificationQualityQueryOpts{
		TenantID:      tenantID,
		From:          from,
		To:            to,
		BucketWidth:   width,
		Source:        strings.TrimSpace(req.GetSource()),
		LogicalModel:  strings.TrimSpace(req.GetLogicalModel()),
		ProviderModel: strings.TrimSpace(req.GetProviderModel()),
		ChannelID:     strings.TrimSpace(req.GetChannelId()),
	}
}

func resolveQualityWindow(req *attunev1.GetClassificationQualityRequest, now time.Time) (qualityWindow, error) {
	bounds, err := qualityBoundsFromRequest(req, now)
	if err != nil {
		return qualityWindow{}, err
	}
	width, err := qualityBucketWidth(req.GetBucketWidth(), bounds.currentFrom, bounds.currentTo, bounds.baselineFrom, bounds.baselineTo)
	if err != nil {
		return qualityWindow{}, err
	}
	threshold, err := qualityThreshold(req.GetLowConfidenceThreshold())
	if err != nil {
		return qualityWindow{}, err
	}
	if err := validateQualityWindows(bounds.currentFrom, bounds.currentTo, bounds.baselineFrom, bounds.baselineTo); err != nil {
		return qualityWindow{}, err
	}
	return qualityWindow{
		currentFrom:  bounds.currentFrom,
		currentTo:    bounds.currentTo,
		baselineFrom: bounds.baselineFrom,
		baselineTo:   bounds.baselineTo,
		bucketWidth:  width,
		threshold:    threshold,
		limit:        qualityLimit(req.GetLimit()),
	}, nil
}

func qualityBoundsFromRequest(req *attunev1.GetClassificationQualityRequest, now time.Time) (qualityBounds, error) {
	bounds := defaultQualityBounds(now)
	var err error
	if bounds.currentTo, err = overrideQualityTime(bounds.currentTo, req.GetCurrentTo()); err != nil {
		return qualityBounds{}, err
	}
	if bounds.currentFrom, err = overrideQualityTime(bounds.currentFrom, req.GetCurrentFrom()); err != nil {
		return qualityBounds{}, err
	}
	if bounds.baselineTo, err = overrideQualityTime(bounds.baselineTo, req.GetBaselineTo()); err != nil {
		return qualityBounds{}, err
	}
	if bounds.baselineFrom, err = overrideQualityTime(bounds.baselineFrom, req.GetBaselineFrom()); err != nil {
		return qualityBounds{}, err
	}
	return bounds, nil
}

func defaultQualityBounds(now time.Time) qualityBounds {
	currentTo := dayStart(now)
	currentFrom := currentTo.AddDate(0, 0, -qualityDefaultCurrentDays)
	baselineTo := currentFrom
	return qualityBounds{
		currentFrom:  currentFrom,
		currentTo:    currentTo,
		baselineFrom: baselineTo.AddDate(0, 0, -qualityDefaultBaselineDays),
		baselineTo:   baselineTo,
	}
}

func overrideQualityTime(fallback time.Time, raw string) (time.Time, error) {
	if raw == "" {
		return fallback, nil
	}
	return parseQualityTime(raw)
}

func qualityThreshold(threshold float64) (float64, error) {
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return 0, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "low_confidence_threshold must be finite")
	}
	if threshold == 0 {
		return 0.60, nil
	}
	if threshold < 0 || threshold > 1 {
		return 0, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "low_confidence_threshold must be between 0 and 1")
	}
	return threshold, nil
}

func qualityLimit(raw int32) int {
	limit := int(raw)
	if limit <= 0 {
		return qualityDefaultLimit
	}
	if limit > qualityMaxLimit {
		return qualityMaxLimit
	}
	return limit
}

func parseQualityTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "time bounds must be RFC3339")
	}
	return t.UTC(), nil
}

func validateQualityWindows(currentFrom, currentTo, baselineFrom, baselineTo time.Time) error {
	if !currentTo.After(currentFrom) || !baselineTo.After(baselineFrom) {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "window end must be after window start")
	}
	maxWindow := time.Duration(qualityMaxWindowDays) * 24 * time.Hour
	if currentTo.Sub(currentFrom) > maxWindow || baselineTo.Sub(baselineFrom) > maxWindow {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "classification quality windows are limited to 90 days")
	}
	return nil
}

func qualityBucketWidth(raw string, windows ...time.Time) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		raw = feedback.QualityBucketDay
	}
	if raw != feedback.QualityBucketHour && raw != feedback.QualityBucketDay {
		return "", dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "bucket_width must be hour or day")
	}
	if raw == feedback.QualityBucketHour {
		for i := 0; i+1 < len(windows); i += 2 {
			if windows[i+1].Sub(windows[i]) > 14*24*time.Hour {
				return feedback.QualityBucketDay, nil
			}
		}
	}
	return raw, nil
}

func dayStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func dimensionKinds(dims []domain.Dimension) dimensionKindLookup {
	out := make(dimensionKindLookup, len(dims))
	for _, dim := range dims {
		out[dim.Name] = dim.Kind
	}
	return out
}

func buildDimensionDrift(
	currentValues []feedback.ClassificationQualityValueAggregate,
	baselineValues []feedback.ClassificationQualityValueAggregate,
	kinds dimensionKindLookup,
	dimFilter string,
	severityFilter string,
	limit int,
) []*attunev1.ClassificationDimensionDrift {
	current := groupDimensionQuality(currentValues, kinds)
	baseline := groupDimensionQuality(baselineValues, kinds)
	names := unionDimensionNames(current, baseline, dimFilter)
	out := make([]*attunev1.ClassificationDimensionDrift, 0, len(names))
	for _, name := range names {
		dim := dimensionDrift(name, current[name], baseline[name], limit)
		if dim == nil || (severityFilter != "" && dim.GetSeverity() != severityFilter) {
			continue
		}
		out = append(out, dim)
	}
	sort.Slice(out, func(i, j int) bool {
		if severityRank(out[i].GetSeverity()) != severityRank(out[j].GetSeverity()) {
			return severityRank(out[i].GetSeverity()) > severityRank(out[j].GetSeverity())
		}
		return out[i].GetJsDistance() > out[j].GetJsDistance()
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func groupDimensionQuality(values []feedback.ClassificationQualityValueAggregate, kinds dimensionKindLookup) map[string]dimensionQuality {
	out := map[string]dimensionQuality{}
	for _, row := range values {
		dq := out[row.DimensionName]
		if dq.values == nil {
			dq.values = map[string]feedback.ClassificationQualityValueAggregate{}
			dq.kind = kinds[row.DimensionName]
		}
		if row.ValueStatus == feedback.QualityValueAll {
			dq.all = ptrext.Of(row)
			dq.total += row.EventCount
		} else {
			dq.values[row.DimensionValueHash+"|"+row.ValueStatus] = row
			dq.appearances += qualityValueCount(row, dq.kind)
		}
		out[row.DimensionName] = dq
	}
	return out
}

func unionDimensionNames(current, baseline map[string]dimensionQuality, filter string) []string {
	seen := map[string]bool{}
	for name := range current {
		seen[name] = true
	}
	for name := range baseline {
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		if filter == "" || filter == name {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func dimensionDrift(name string, current dimensionQuality, baseline dimensionQuality, limit int) *attunev1.ClassificationDimensionDrift {
	currentDenom := qualityDenominator(current)
	baselineDenom := qualityDenominator(baseline)
	status := "normal"
	if currentDenom < qualityMinDimensionVolume || baselineDenom < qualityMinDimensionVolume {
		status = "insufficient_data"
	}
	allValues := valueDrifts(current, baseline, currentDenom, baselineDenom)
	values := limitValueDrifts(allValues, limit)
	js := jsDistance(allValues)
	psi := psiScore(allValues)
	lowDelta := math.Abs(ratePP(lowConfidenceRate(current.all)) - ratePP(lowConfidenceRate(baseline.all)))
	severity := severityFor(status, js, maxShareDelta(allValues), lowDelta)
	return ptrext.Of(attunev1.ClassificationDimensionDrift{
		DimensionName:        name,
		Severity:             severity,
		Status:               status,
		CurrentCount:         currentDenom,
		BaselineCount:        baselineDenom,
		JsDistance:           js,
		Psi:                  psi,
		LowConfidenceRate:    lowConfidenceRate(current.all),
		OffListRate:          dimensionStatusRate(current, feedback.QualityValueOffList),
		UnknownDimensionRate: dimensionStatusRate(current, feedback.QualityValueUnknownDim),
		Values:               values,
		SampleFeedbackIds:    dimensionSamples(values, current.all),
	})
}

func valueDrifts(current, baseline dimensionQuality, currentDenom, baselineDenom int64) []*attunev1.ClassificationValueDrift {
	keys := map[string]bool{}
	for key := range current.values {
		keys[key] = true
	}
	for key := range baseline.values {
		keys[key] = true
	}
	out := make([]*attunev1.ClassificationValueDrift, 0, len(keys))
	for key := range keys {
		c := current.values[key]
		b := baseline.values[key]
		currentCount := qualityValueCount(c, current.kind)
		baselineCount := qualityValueCount(b, baseline.kind)
		currentShare := safeShare(currentCount, currentDenom)
		baselineShare := safeShare(baselineCount, baselineDenom)
		out = append(out, ptrext.Of(attunev1.ClassificationValueDrift{
			ValueHash:         firstNonEmpty(c.DimensionValueHash, b.DimensionValueHash),
			ValueDisplay:      firstNonEmpty(c.DimensionValueDisplay, b.DimensionValueDisplay),
			ValueStatus:       firstNonEmpty(c.ValueStatus, b.ValueStatus),
			CurrentCount:      currentCount,
			BaselineCount:     baselineCount,
			CurrentShare:      currentShare,
			BaselineShare:     baselineShare,
			ShareDeltaPp:      (currentShare - baselineShare) * 100,
			Contribution:      math.Abs(currentShare - baselineShare),
			SampleFeedbackIds: c.SampleFeedbackIDs,
		}))
	}
	sort.Slice(out, func(i, j int) bool {
		return math.Abs(out[i].GetShareDeltaPp()) > math.Abs(out[j].GetShareDeltaPp())
	})
	return out
}

func limitValueDrifts(values []*attunev1.ClassificationValueDrift, limit int) []*attunev1.ClassificationValueDrift {
	if limit <= 0 {
		return nil
	}
	out := values
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func qualityValueCount(row feedback.ClassificationQualityValueAggregate, kind domain.DimensionKind) int64 {
	if kind == domain.DimMulti {
		return row.AppearanceCount
	}
	return row.EventCount
}

func qualityDenominator(dq dimensionQuality) int64 {
	if dq.kind == domain.DimMulti {
		return dq.appearances
	}
	if dq.all != nil {
		return dq.all.EventCount
	}
	return dq.total
}

func jsDistance(values []*attunev1.ClassificationValueDrift) float64 {
	var divergence float64
	for _, value := range values {
		p := clampProb(value.GetCurrentShare())
		q := clampProb(value.GetBaselineShare())
		m := (p + q) / 2
		divergence += 0.5 * klTerm(p, m)
		divergence += 0.5 * klTerm(q, m)
	}
	return math.Sqrt(divergence)
}

func psiScore(values []*attunev1.ClassificationValueDrift) float64 {
	var out float64
	for _, value := range values {
		p := clampProb(value.GetCurrentShare())
		q := clampProb(value.GetBaselineShare())
		out += (p - q) * math.Log(p/q)
	}
	return out
}

func klTerm(p, q float64) float64 {
	if p <= 0 || q <= 0 {
		return 0
	}
	return p * math.Log(p/q)
}

func clampProb(v float64) float64 {
	const eps = 1e-9
	if v < eps {
		return eps
	}
	if v > 1 {
		return 1
	}
	return v
}

func severityFor(status string, js float64, shareDeltaPP float64, lowDeltaPP float64) string {
	if status == "insufficient_data" {
		return "insufficient_data"
	}
	if js >= qualityAlertJSDistance || shareDeltaPP >= qualityAlertShareDeltaPP || lowDeltaPP >= qualityAlertLowConfDeltaPP {
		return "alert"
	}
	if js >= qualityWatchJSDistance || shareDeltaPP >= qualityWatchShareDeltaPP || lowDeltaPP >= qualityWatchLowConfDeltaPP {
		return "watch"
	}
	return "normal"
}

func severityRank(severity string) int {
	switch severity {
	case "alert":
		return 3
	case "watch":
		return 2
	case "insufficient_data":
		return 1
	default:
		return 0
	}
}

func maxShareDelta(values []*attunev1.ClassificationValueDrift) float64 {
	var out float64
	for _, value := range values {
		out = math.Max(out, math.Abs(value.GetShareDeltaPp()))
	}
	return out
}

func lowConfidenceRate(row *feedback.ClassificationQualityValueAggregate) float64 {
	if row == nil || row.ConfidenceCount == 0 {
		return 0
	}
	return float64(row.LowConfidenceCount) / float64(row.ConfidenceCount)
}

func dimensionStatusRate(dq dimensionQuality, status string) float64 {
	denom := qualityDenominator(dq)
	if denom == 0 {
		return 0
	}
	var count int64
	for _, value := range dq.values {
		if value.ValueStatus == status {
			count += qualityValueCount(value, dq.kind)
		}
	}
	return float64(count) / float64(denom)
}

func ratePP(rate float64) float64 { return rate * 100 }

func safeShare(count, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func dimensionSamples(values []*attunev1.ClassificationValueDrift, all *feedback.ClassificationQualityValueAggregate) []int64 {
	var out []int64
	for _, value := range values {
		out = appendIDSet(out, value.GetSampleFeedbackIds(), 5)
	}
	if all != nil {
		out = appendIDSet(out, all.SampleFeedbackIDs, 5)
	}
	return out
}

func buildQualityWarnings(signal feedback.ClassificationQualitySignalAggregate, dims []*attunev1.ClassificationDimensionDrift) []*attunev1.ClassificationQualityWarning {
	var out []*attunev1.ClassificationQualityWarning
	for _, dim := range dims {
		switch dim.GetSeverity() {
		case "alert", "watch":
			out = append(out, ptrext.Of(attunev1.ClassificationQualityWarning{
				Reason:            "dimension_distribution_drift",
				Severity:          dim.GetSeverity(),
				DimensionName:     dim.GetDimensionName(),
				Value:             dim.GetJsDistance(),
				Threshold:         qualityWatchJSDistance,
				Message:           "Dimension distribution changed against baseline",
				SampleFeedbackIds: dim.GetSampleFeedbackIds(),
			}))
		}
	}
	out = appendRateWarnings(out, signal)
	return out
}

func appendRateWarnings(out []*attunev1.ClassificationQualityWarning, signal feedback.ClassificationQualitySignalAggregate) []*attunev1.ClassificationQualityWarning {
	attempts := signal.ClassificationEventCount + signal.FailedAttemptCount
	rules := []struct {
		reason  string
		rate    float64
		watch   float64
		alert   float64
		samples []int64
	}{
		{"low_confidence_rate_spike", safeShare(signal.LowConfidenceCount, signal.ConfidenceCount), 0.05, 0.10, signal.LowConfidenceSampleFeedbackIDs},
		{"off_list_rate_spike", safeShare(signal.OffListCount, signal.ClassificationEventCount), 0.02, 0.05, signal.OffListSampleFeedbackIDs},
		{"parse_failure_rate_spike", safeShare(signal.ParseFailureCount, attempts), 0.02, 0.05, signal.ParseFailureSampleFeedbackIDs},
		{"terminal_failure_rate_spike", safeShare(signal.TerminalFailureCount, attempts), 0.01, 0.03, signal.TerminalFailureSampleFeedbackIDs},
	}
	for _, rule := range rules {
		severity := ""
		threshold := rule.watch
		if rule.rate >= rule.alert {
			severity = "alert"
			threshold = rule.alert
		} else if rule.rate >= rule.watch {
			severity = "watch"
		}
		if severity != "" {
			out = append(out, ptrext.Of(attunev1.ClassificationQualityWarning{
				Reason:            rule.reason,
				Severity:          severity,
				Value:             rule.rate,
				Threshold:         threshold,
				Message:           "Quality signal crossed threshold",
				SampleFeedbackIds: rule.samples,
			}))
		}
	}
	return out
}

func qualityResponse(
	window qualityWindow,
	signal feedback.ClassificationQualitySignalAggregate,
	series []feedback.ClassificationQualitySeriesBucket,
	dims []*attunev1.ClassificationDimensionDrift,
	warnings []*attunev1.ClassificationQualityWarning,
	samples []*attunev1.ClassificationQualitySample,
	dataThrough time.Time,
) *attunev1.GetClassificationQualityResponse {
	now := time.Now().UTC()
	return ptrext.Of(attunev1.GetClassificationQualityResponse{
		GeneratedAt:      now.Format(time.RFC3339),
		DataThrough:      dataThrough.UTC().Format(time.RFC3339),
		RollupLagSeconds: int64(math.Max(0, now.Sub(dataThrough).Seconds())),
		CurrentFrom:      window.currentFrom.Format(time.RFC3339),
		CurrentTo:        window.currentTo.Format(time.RFC3339),
		BaselineFrom:     window.baselineFrom.Format(time.RFC3339),
		BaselineTo:       window.baselineTo.Format(time.RFC3339),
		BucketWidth:      window.bucketWidth,
		Summary:          qualitySummary(signal, worstSeverity(dims, warnings)),
		Series:           qualitySeriesToProto(series),
		Dimensions:       dims,
		Warnings:         warnings,
		Samples:          samples,
	})
}

func qualitySummary(signal feedback.ClassificationQualitySignalAggregate, worst string) *attunev1.ClassificationQualitySummary {
	attempts := signal.ClassificationEventCount + signal.FailedAttemptCount
	return ptrext.Of(attunev1.ClassificationQualitySummary{
		ClassificationEvents: signal.ClassificationEventCount,
		FailedAttempts:       signal.FailedAttemptCount,
		AverageConfidence:    safeFloat(signal.ConfidenceSum, signal.ConfidenceCount),
		LowConfidenceRate:    safeShare(signal.LowConfidenceCount, signal.ConfidenceCount),
		OffListRate:          safeShare(signal.OffListCount, signal.ClassificationEventCount),
		UnknownDimensionRate: safeShare(signal.UnknownDimensionCount, signal.ClassificationEventCount),
		ParseFailureRate:     safeShare(signal.ParseFailureCount, attempts),
		TerminalFailureRate:  safeShare(signal.TerminalFailureCount, attempts),
		WorstSeverity:        worst,
	})
}

func qualitySeriesToProto(series []feedback.ClassificationQualitySeriesBucket) []*attunev1.ClassificationQualityTimeBucket {
	out := make([]*attunev1.ClassificationQualityTimeBucket, 0, len(series))
	for _, row := range series {
		attempts := row.ClassificationEventCount + row.FailedAttemptCount
		out = append(out, ptrext.Of(attunev1.ClassificationQualityTimeBucket{
			Bucket:               row.Bucket.UTC().Format(time.RFC3339),
			ClassificationEvents: row.ClassificationEventCount,
			FailedAttempts:       row.FailedAttemptCount,
			AverageConfidence:    safeFloat(row.ConfidenceSum, row.ConfidenceCount),
			LowConfidenceRate:    safeShare(row.LowConfidenceCount, row.ConfidenceCount),
			OffListRate:          safeShare(row.OffListCount, row.ClassificationEventCount),
			UnknownDimensionRate: safeShare(row.UnknownDimensionCount, row.ClassificationEventCount),
			ParseFailureRate:     safeShare(row.ParseFailureCount, attempts),
			TerminalFailureRate:  safeShare(row.TerminalFailureCount, attempts),
		}))
	}
	return out
}

func safeFloat(sum float64, count int64) float64 {
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func worstSeverity(dims []*attunev1.ClassificationDimensionDrift, warnings []*attunev1.ClassificationQualityWarning) string {
	worst := "normal"
	for _, dim := range dims {
		if severityRank(dim.GetSeverity()) > severityRank(worst) {
			worst = dim.GetSeverity()
		}
	}
	for _, warning := range warnings {
		if severityRank(warning.GetSeverity()) > severityRank(worst) {
			worst = warning.GetSeverity()
		}
	}
	return worst
}

func sampleIDsFromQuality(dims []*attunev1.ClassificationDimensionDrift, warnings []*attunev1.ClassificationQualityWarning) []int64 {
	var out []int64
	for _, dim := range dims {
		out = appendIDSet(out, dim.GetSampleFeedbackIds(), qualityMaxLimit)
		for _, value := range dim.GetValues() {
			out = appendIDSet(out, value.GetSampleFeedbackIds(), qualityMaxLimit)
		}
	}
	for _, warning := range warnings {
		out = appendIDSet(out, warning.GetSampleFeedbackIds(), qualityMaxLimit)
	}
	return out
}

func appendIDSet(dst []int64, ids []int64, limit int) []int64 {
	for _, id := range ids {
		if len(dst) >= limit {
			return dst
		}
		seen := false
		for _, existing := range dst {
			if existing == id {
				seen = true
				break
			}
		}
		if !seen && id > 0 {
			dst = append(dst, id)
		}
	}
	return dst
}

func (h *FeedbackHandler) qualitySamplesForResponse(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tenantID string,
	ids []int64,
) []*attunev1.ClassificationQualitySample {
	samples, err := h.repo.ClassificationQualitySamples(ctx, tenantID, ids)
	if err != nil {
		logext.Warnf(ctx, "[console.FeedbackHandler.qualitySamplesForResponse] sample query failed,tenant_id:%s,err:%+v", tenantID, err.Error())
		return nil
	}
	return qualitySamplesToProto(samples, "quality")
}

func qualitySamplesToProto(samples []feedback.ClassificationQualitySample, reason string) []*attunev1.ClassificationQualitySample {
	out := make([]*attunev1.ClassificationQualitySample, 0, len(samples))
	for _, sample := range samples {
		row := ptrext.Of(attunev1.ClassificationQualitySample{
			Id:                       sample.ID,
			CreatedAt:                sample.CreatedAt.UTC().Format(time.RFC3339),
			Source:                   sample.Source,
			Title:                    firstNonEmpty(sample.DisplayTitle, sample.Title),
			DisplayTitle:             nullableString(sample.DisplayTitle),
			EnrichmentStatus:         sample.EnrichmentStatus,
			ClassificationConfidence: sample.ClassificationConfidence,
			SignalReason:             reason,
		})
		if sample.EnrichedAt != nil {
			row.EnrichedAt = ptrext.Of(sample.EnrichedAt.UTC().Format(time.RFC3339))
		}
		out = append(out, row)
	}
	return out
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func recordClassificationQualityMetrics(
	tenantID string,
	configuredDims []domain.Dimension,
	summary *attunev1.ClassificationQualitySummary,
	dims []*attunev1.ClassificationDimensionDrift,
	warnings []*attunev1.ClassificationQualityWarning,
) {
	if summary == nil {
		return
	}
	metrics.ClassificationQualityLowConfidenceRatio.WithLabelValues(tenantID).Set(summary.GetLowConfidenceRate())
	metrics.ClassificationQualityOffListRatio.WithLabelValues(tenantID).Set(summary.GetOffListRate())
	metrics.ClassificationQualityParseFailureRatio.WithLabelValues(tenantID).Set(summary.GetParseFailureRate())
	metrics.ClassificationQualityTerminalFailureRatio.WithLabelValues(tenantID).Set(summary.GetTerminalFailureRate())
	for _, dim := range configuredDims {
		metrics.ClassificationQualityDriftScore.WithLabelValues(tenantID, dim.Name).Set(0)
	}
	for _, dim := range dims {
		metrics.ClassificationQualityDriftScore.WithLabelValues(tenantID, dim.GetDimensionName()).Set(dim.GetJsDistance())
	}
	for _, reason := range knownQualityWarningReasons() {
		metrics.ClassificationQualityWarningActive.WithLabelValues(tenantID, reason, "watch").Set(0)
		metrics.ClassificationQualityWarningActive.WithLabelValues(tenantID, reason, "alert").Set(0)
	}
	for _, warning := range warnings {
		if warning.GetReason() != "" && warning.GetSeverity() != "" {
			metrics.ClassificationQualityWarningActive.WithLabelValues(tenantID, warning.GetReason(), warning.GetSeverity()).Set(1)
		}
	}
}

func knownQualityWarningReasons() []string {
	return []string{
		"dimension_distribution_drift",
		"low_confidence_rate_spike",
		"off_list_rate_spike",
		"parse_failure_rate_spike",
		"terminal_failure_rate_spike",
	}
}
