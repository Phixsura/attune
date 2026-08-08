package feedback

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
)

func TestBuildDimensionDriftFlagsDistributionShift(t *testing.T) {
	t.Parallel()

	kinds := dimensionKindLookup{"severity": domain.DimSingle}
	current := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 100, 30, 1),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "bug-hash", "bug", 80, 20, 11),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "praise-hash", "praise", 20, 2, 12),
	}
	baseline := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 100, 1, 21),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "bug-hash", "bug", 50, 1, 22),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "praise-hash", "praise", 50, 1, 23),
	}

	dims := buildDimensionDrift(current, baseline, kinds, "", "", 10)

	require.Len(t, dims, 1)
	require.Equal(t, "severity", dims[0].GetDimensionName())
	require.Equal(t, "alert", dims[0].GetSeverity())
	require.Equal(t, "normal", dims[0].GetStatus())
	require.Greater(t, dims[0].GetJsDistance(), 0.0)
	require.InDelta(t, 0.30, dims[0].GetLowConfidenceRate(), 0.001)
	require.Contains(t, dims[0].GetSampleFeedbackIds(), int64(11))
	require.Len(t, dims[0].GetValues(), 2)
	require.InDelta(t, 30.0, dims[0].GetValues()[0].GetShareDeltaPp(), 0.001)
}

func TestBuildDimensionDriftScoresFullDistributionBeforeDisplayLimit(t *testing.T) {
	t.Parallel()

	kinds := dimensionKindLookup{"severity": domain.DimSingle}
	current := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 100, 0, 1),
	}
	baseline := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 100, 0, 11),
	}
	for i := 0; i < 10; i++ {
		hash := "v" + strconv.Itoa(i)
		display := "value-" + strconv.Itoa(i)
		current = append(current, qualityValue("severity", feedbackrepo.QualityValueConfigured, hash, display, 10, 0, int64(100+i)))
		if i < 5 {
			baseline = append(baseline, qualityValue("severity", feedbackrepo.QualityValueConfigured, hash, display, 20, 0, int64(200+i)))
		}
	}

	dims := buildDimensionDrift(current, baseline, kinds, "", "", 1)

	require.Len(t, dims, 1)
	require.Len(t, dims[0].GetValues(), 1)
	require.Greater(t, dims[0].GetJsDistance(), jsDistance(dims[0].GetValues())*2)
}

func TestBuildQualityWarningsUsesReasonSpecificSamples(t *testing.T) {
	t.Parallel()

	warnings := buildQualityWarnings(feedbackrepo.ClassificationQualitySignalAggregate{
		ClassificationEventCount:         100,
		FailedAttemptCount:               100,
		ConfidenceCount:                  100,
		LowConfidenceCount:               12,
		OffListCount:                     6,
		ParseFailureCount:                7,
		TerminalFailureCount:             4,
		SampleFeedbackIDs:                []int64{900},
		LowConfidenceSampleFeedbackIDs:   []int64{101},
		OffListSampleFeedbackIDs:         []int64{202},
		ParseFailureSampleFeedbackIDs:    []int64{303},
		TerminalFailureSampleFeedbackIDs: []int64{404},
	}, nil)

	got := map[string][]int64{}
	for _, warning := range warnings {
		got[warning.GetReason()] = warning.GetSampleFeedbackIds()
	}
	require.Equal(t, []int64{101}, got["low_confidence_rate_spike"])
	require.Equal(t, []int64{202}, got["off_list_rate_spike"])
	require.Equal(t, []int64{303}, got["parse_failure_rate_spike"])
	require.Equal(t, []int64{404}, got["terminal_failure_rate_spike"])
}

func TestBindClassificationQualityRequestParsesFiltersAndRejectsMalformedParams(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(
		http.MethodGet,
		"/console/classification-quality?current_from=2026-07-01T00:00:00Z&current_to=2026-07-02T00:00:00Z"+
			"&baseline_from=2026-06-01T00:00:00Z&baseline_to=2026-06-02T00:00:00Z&bucket_width=hour"+
			"&source=api&logical_model=classifier-v1&provider_model=gpt-4o-mini&channel_id=primary"+
			"&dimension_name=severity&severity=watch&low_confidence_threshold=0.42&limit=7",
		nil,
	)
	out := ptrext.Of(attunev1.GetClassificationQualityRequest{})

	err := BindClassificationQualityRequest(req, out)

	require.NoError(t, err)
	require.Equal(t, "2026-07-01T00:00:00Z", out.GetCurrentFrom())
	require.Equal(t, "2026-07-02T00:00:00Z", out.GetCurrentTo())
	require.Equal(t, "2026-06-01T00:00:00Z", out.GetBaselineFrom())
	require.Equal(t, "2026-06-02T00:00:00Z", out.GetBaselineTo())
	require.Equal(t, feedbackrepo.QualityBucketHour, out.GetBucketWidth())
	require.Equal(t, "api", out.GetSource())
	require.Equal(t, "classifier-v1", out.GetLogicalModel())
	require.Equal(t, "gpt-4o-mini", out.GetProviderModel())
	require.Equal(t, "primary", out.GetChannelId())
	require.Equal(t, "severity", out.GetDimensionName())
	require.Equal(t, "watch", out.GetSeverity())
	require.InDelta(t, 0.42, out.GetLowConfidenceThreshold(), 0.001)
	require.Equal(t, int32(7), out.GetLimit())

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "bad threshold",
			url:  "/console/classification-quality?low_confidence_threshold=nope",
			want: "must be a number",
		},
		{
			name: "bad limit",
			url:  "/console/classification-quality?limit=seven",
			want: "limit must be an integer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			out := ptrext.Of(attunev1.GetClassificationQualityRequest{})

			err := BindClassificationQualityRequest(req, out)

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestBindClassificationQualityRequestRejectsNonFiniteThreshold(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"NaN", "Inf", "-Inf"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/console/classification-quality?low_confidence_threshold="+raw, nil)
			out := ptrext.Of(attunev1.GetClassificationQualityRequest{})

			err := BindClassificationQualityRequest(req, out)

			require.ErrorContains(t, err, "finite")
		})
	}
}

func TestBindClassificationQualitySamplesRequestParsesMultiValueIDs(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/console/classification-quality/samples?ids=101,+102&ids=103&ids=&ids=104", nil)
	out := ptrext.Of(attunev1.GetClassificationQualitySamplesRequest{})

	err := BindClassificationQualitySamplesRequest(req, out)

	require.NoError(t, err)
	require.Equal(t, []int64{101, 102, 103, 104}, out.GetIds())
}

func TestBindClassificationQualitySamplesRequestRejectsBadIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "not integer",
			url:  "/console/classification-quality/samples?ids=101,nope",
			want: "integers",
		},
		{
			name: "non positive",
			url:  "/console/classification-quality/samples?ids=0,-1",
			want: "positive",
		},
		{
			name: "too many",
			url:  "/console/classification-quality/samples?ids=" + strings.Join(numberStrings(1, qualityMaxLimit+1), ","),
			want: "limited",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			out := ptrext.Of(attunev1.GetClassificationQualitySamplesRequest{})

			err := BindClassificationQualitySamplesRequest(req, out)

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestResolveQualityWindowRejectsAdversarialBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		req  *attunev1.GetClassificationQualityRequest
		want string
	}{
		{
			name: "inverted current",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentFrom:  "2026-07-02T00:00:00Z",
				CurrentTo:    "2026-07-01T00:00:00Z",
				BaselineFrom: "2026-06-01T00:00:00Z",
				BaselineTo:   "2026-06-02T00:00:00Z",
			}),
			want: "after window start",
		},
		{
			name: "oversized current",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentFrom:  "2026-03-01T00:00:00Z",
				CurrentTo:    "2026-07-01T00:00:00Z",
				BaselineFrom: "2026-02-01T00:00:00Z",
				BaselineTo:   "2026-03-01T00:00:00Z",
			}),
			want: "90 days",
		},
		{
			name: "non finite threshold",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				LowConfidenceThreshold: math.NaN(),
			}),
			want: "finite",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := resolveQualityWindow(tt.req, now)

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestResolveQualityWindowDefaultsClampsAndRejectsBadInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 17, 15, 30, 0, 0, time.UTC)
	got, err := resolveQualityWindow(ptrext.Of(attunev1.GetClassificationQualityRequest{}), now)

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC), got.currentTo)
	require.Equal(t, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), got.currentFrom)
	require.Equal(t, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), got.baselineTo)
	require.Equal(t, time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC), got.baselineFrom)
	require.Equal(t, feedbackrepo.QualityBucketDay, got.bucketWidth)
	require.InDelta(t, 0.60, got.threshold, 0.001)
	require.Equal(t, qualityDefaultLimit, got.limit)

	got, err = resolveQualityWindow(ptrext.Of(attunev1.GetClassificationQualityRequest{
		CurrentFrom:            "2026-07-16T00:00:00Z",
		CurrentTo:              "2026-07-17T00:00:00Z",
		BaselineFrom:           "2026-07-15T00:00:00Z",
		BaselineTo:             "2026-07-16T00:00:00Z",
		BucketWidth:            " HOUR ",
		LowConfidenceThreshold: 1,
		Limit:                  99,
	}), now)

	require.NoError(t, err)
	require.Equal(t, feedbackrepo.QualityBucketHour, got.bucketWidth)
	require.InDelta(t, 1.0, got.threshold, 0.001)
	require.Equal(t, qualityMaxLimit, got.limit)

	tests := []struct {
		name string
		req  *attunev1.GetClassificationQualityRequest
		want string
	}{
		{
			name: "invalid time",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentFrom: "not-a-time",
			}),
			want: "RFC3339",
		},
		{
			name: "invalid bucket width",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				BucketWidth: "week",
			}),
			want: "bucket_width",
		},
		{
			name: "low threshold",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				LowConfidenceThreshold: -0.1,
			}),
			want: "between 0 and 1",
		},
		{
			name: "high threshold",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				LowConfidenceThreshold: 1.1,
			}),
			want: "between 0 and 1",
		},
		{
			name: "oversized baseline",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentFrom:  "2026-07-01T00:00:00Z",
				CurrentTo:    "2026-07-02T00:00:00Z",
				BaselineFrom: "2026-01-01T00:00:00Z",
				BaselineTo:   "2026-07-01T00:00:00Z",
			}),
			want: "90 days",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := resolveQualityWindow(tt.req, now)

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestQualityBoundsFromRequestReportsEachMalformedBound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 17, 15, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		req  *attunev1.GetClassificationQualityRequest
	}{
		{
			name: "current to",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentTo: "not-a-time",
			}),
		},
		{
			name: "current from",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentTo:   "2026-07-17T00:00:00Z",
				CurrentFrom: "not-a-time",
			}),
		},
		{
			name: "baseline to",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentTo:   "2026-07-17T00:00:00Z",
				CurrentFrom: "2026-07-10T00:00:00Z",
				BaselineTo:  "not-a-time",
			}),
		},
		{
			name: "baseline from",
			req: ptrext.Of(attunev1.GetClassificationQualityRequest{
				CurrentTo:    "2026-07-17T00:00:00Z",
				CurrentFrom:  "2026-07-10T00:00:00Z",
				BaselineTo:   "2026-07-10T00:00:00Z",
				BaselineFrom: "not-a-time",
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := qualityBoundsFromRequest(tt.req, now)

			require.ErrorContains(t, err, "RFC3339")
		})
	}
}

func TestResolveQualityWindowDowngradesWideHourlyWindow(t *testing.T) {
	t.Parallel()

	got, err := resolveQualityWindow(ptrext.Of(attunev1.GetClassificationQualityRequest{
		CurrentFrom:  "2026-06-01T00:00:00Z",
		CurrentTo:    "2026-06-20T00:00:00Z",
		BaselineFrom: "2026-05-01T00:00:00Z",
		BaselineTo:   "2026-05-10T00:00:00Z",
		BucketWidth:  feedbackrepo.QualityBucketHour,
	}), time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC))

	require.NoError(t, err)
	require.Equal(t, feedbackrepo.QualityBucketDay, got.bucketWidth)
}

func TestBuildDimensionDriftAppliesDimensionLimit(t *testing.T) {
	t.Parallel()

	kinds := dimensionKindLookup{
		"severity": domain.DimSingle,
		"topic":    domain.DimSingle,
	}
	current := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 100, 0, 1),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "bug-hash", "bug", 90, 0, 2),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "praise-hash", "praise", 10, 0, 3),
		qualityValue("topic", feedbackrepo.QualityValueAll, "", "", 100, 0, 4),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "billing-hash", "billing", 80, 0, 5),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "pricing-hash", "pricing", 20, 0, 6),
	}
	baseline := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 100, 0, 11),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "bug-hash", "bug", 10, 0, 12),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "praise-hash", "praise", 90, 0, 13),
		qualityValue("topic", feedbackrepo.QualityValueAll, "", "", 100, 0, 14),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "billing-hash", "billing", 20, 0, 15),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "pricing-hash", "pricing", 80, 0, 16),
	}

	dims := buildDimensionDrift(current, baseline, kinds, "", "", 1)

	require.Len(t, dims, 1)
	require.Equal(t, "alert", dims[0].GetSeverity())
}

func TestBuildDimensionDriftFiltersInsufficientDataAndOrdering(t *testing.T) {
	t.Parallel()

	kinds := dimensionKindLookup{
		"severity": domain.DimSingle,
		"topic":    domain.DimSingle,
	}
	current := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 10, 1, 1),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "bug-hash", "bug", 8, 1, 2),
		qualityValue("topic", feedbackrepo.QualityValueAll, "", "", 100, 1, 11),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "billing-hash", "billing", 90, 1, 12),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "pricing-hash", "pricing", 10, 1, 13),
	}
	baseline := []feedbackrepo.ClassificationQualityValueAggregate{
		qualityValue("severity", feedbackrepo.QualityValueAll, "", "", 10, 0, 21),
		qualityValue("severity", feedbackrepo.QualityValueConfigured, "bug-hash", "bug", 10, 0, 22),
		qualityValue("topic", feedbackrepo.QualityValueAll, "", "", 100, 0, 31),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "billing-hash", "billing", 20, 0, 32),
		qualityValue("topic", feedbackrepo.QualityValueConfigured, "pricing-hash", "pricing", 80, 0, 33),
	}

	dims := buildDimensionDrift(current, baseline, kinds, "", "", 10)

	require.Len(t, dims, 2)
	require.Equal(t, "topic", dims[0].GetDimensionName())
	require.Equal(t, "alert", dims[0].GetSeverity())
	require.Equal(t, "severity", dims[1].GetDimensionName())
	require.Equal(t, "insufficient_data", dims[1].GetStatus())
	require.Equal(t, "insufficient_data", dims[1].GetSeverity())
	require.Equal(t, []int64{2, 1}, dims[1].GetSampleFeedbackIds())

	filtered := buildDimensionDrift(current, baseline, kinds, "topic", "alert", 10)
	require.Len(t, filtered, 1)
	require.Equal(t, "topic", filtered[0].GetDimensionName())
	require.Empty(t, buildDimensionDrift(current, baseline, kinds, "missing", "", 10))
	require.Empty(t, buildDimensionDrift(current, baseline, kinds, "topic", "watch", 10))
	require.Empty(t, limitValueDrifts(filtered[0].GetValues(), 0))
}

func TestDimensionQualityCountsMultiSelectAndStatusRates(t *testing.T) {
	t.Parallel()

	rows := []feedbackrepo.ClassificationQualityValueAggregate{
		{
			DimensionName:   "labels",
			ValueStatus:     feedbackrepo.QualityValueAll,
			EventCount:      50,
			AppearanceCount: 50,
		},
		{
			DimensionName:      "labels",
			DimensionValueHash: "bug-hash",
			ValueStatus:        feedbackrepo.QualityValueConfigured,
			EventCount:         10,
			AppearanceCount:    25,
		},
		{
			DimensionName:      "labels",
			DimensionValueHash: "unknown-hash",
			ValueStatus:        feedbackrepo.QualityValueUnknownDim,
			EventCount:         4,
			AppearanceCount:    15,
		},
		{
			DimensionName:      "labels",
			DimensionValueHash: "off-list-hash",
			ValueStatus:        feedbackrepo.QualityValueOffList,
			EventCount:         2,
			AppearanceCount:    10,
		},
	}
	grouped := groupDimensionQuality(rows, dimensionKindLookup{"labels": domain.DimMulti})
	labels := grouped["labels"]

	require.Equal(t, int64(25), qualityValueCount(rows[1], domain.DimMulti))
	require.Equal(t, int64(10), qualityValueCount(rows[1], domain.DimSingle))
	require.Equal(t, int64(50), qualityDenominator(labels))
	require.InDelta(t, 0.20, dimensionStatusRate(labels, feedbackrepo.QualityValueOffList), 0.001)
	require.InDelta(t, 0.30, dimensionStatusRate(labels, feedbackrepo.QualityValueUnknownDim), 0.001)
	require.Zero(t, dimensionStatusRate(dimensionQuality{}, feedbackrepo.QualityValueOffList))

	singleAll := feedbackrepo.ClassificationQualityValueAggregate{EventCount: 99}
	require.Equal(t, int64(99), qualityDenominator(dimensionQuality{
		all:  ptrext.Of(singleAll),
		kind: domain.DimSingle,
	}))
	require.Equal(t, int64(7), qualityDenominator(dimensionQuality{
		total: 7,
		kind:  domain.DimSingle,
	}))
}

func TestQualityMathHelpersCoverEdges(t *testing.T) {
	t.Parallel()

	require.Equal(t, "insufficient_data", severityFor("insufficient_data", 1, 100, 100))
	require.Equal(t, "alert", severityFor("normal", qualityAlertJSDistance, 0, 0))
	require.Equal(t, "alert", severityFor("normal", 0, qualityAlertShareDeltaPP, 0))
	require.Equal(t, "alert", severityFor("normal", 0, 0, qualityAlertLowConfDeltaPP))
	require.Equal(t, "watch", severityFor("normal", qualityWatchJSDistance, 0, 0))
	require.Equal(t, "normal", severityFor("normal", 0, 0, 0))
	require.Equal(t, 3, severityRank("alert"))
	require.Equal(t, 2, severityRank("watch"))
	require.Equal(t, 1, severityRank("insufficient_data"))
	require.Zero(t, severityRank("normal"))

	require.Zero(t, klTerm(0, 0.5))
	require.Zero(t, klTerm(0.5, 0))
	require.Greater(t, klTerm(0.5, 0.25), 0.0)
	require.InDelta(t, 1e-9, clampProb(0), 1e-12)
	require.Equal(t, 1.0, clampProb(1.5))
	require.InDelta(t, 0.25, clampProb(0.25), 0.001)
	require.Zero(t, lowConfidenceRate(nil))
	require.Zero(t, lowConfidenceRate(ptrext.Of(feedbackrepo.ClassificationQualityValueAggregate{})))
	require.InDelta(t, 0.40, lowConfidenceRate(ptrext.Of(feedbackrepo.ClassificationQualityValueAggregate{
		ConfidenceCount:    10,
		LowConfidenceCount: 4,
	})), 0.001)
	require.InDelta(t, 0.5, safeShare(2, 4), 0.001)
	require.Zero(t, safeShare(2, 0))
	require.InDelta(t, 2.5, safeFloat(10, 4), 0.001)
	require.Zero(t, safeFloat(10, 0))
	require.Equal(t, "fallback", firstNonEmpty("", "fallback", "ignored"))
	require.Empty(t, firstNonEmpty("", ""))

	values := []*attunev1.ClassificationValueDrift{
		{CurrentShare: 0.60, BaselineShare: 0.40, ShareDeltaPp: -11},
		{CurrentShare: 0.40, BaselineShare: 0.60, ShareDeltaPp: 7},
	}
	require.InDelta(t, 11, maxShareDelta(values), 0.001)
	require.Greater(t, jsDistance(values), 0.0)
	require.Greater(t, psiScore(values), 0.0)

	early := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	require.Equal(t, early, minTime(late, early))
	require.Equal(t, early, minTime(early, late))
	require.Equal(t, late, maxTime(late, early))
	require.Equal(t, late, maxTime(early, late))
}

func TestBuildQualityWarningsAndSampleIDsDeduplicateEdges(t *testing.T) {
	t.Parallel()

	dims := []*attunev1.ClassificationDimensionDrift{
		{
			DimensionName:     "severity",
			Severity:          "watch",
			JsDistance:        0.12,
			SampleFeedbackIds: []int64{1, 2},
			Values: []*attunev1.ClassificationValueDrift{{
				SampleFeedbackIds: []int64{2, 3, 0, -1},
			}},
		},
		{
			DimensionName:     "topic",
			Severity:          "normal",
			JsDistance:        0.01,
			SampleFeedbackIds: []int64{9},
		},
	}
	warnings := buildQualityWarnings(feedbackrepo.ClassificationQualitySignalAggregate{
		ClassificationEventCount:         100,
		FailedAttemptCount:               100,
		ConfidenceCount:                  100,
		LowConfidenceCount:               5,
		OffListCount:                     5,
		ParseFailureCount:                4,
		TerminalFailureCount:             6,
		LowConfidenceSampleFeedbackIDs:   []int64{4},
		OffListSampleFeedbackIDs:         []int64{5},
		ParseFailureSampleFeedbackIDs:    []int64{6},
		TerminalFailureSampleFeedbackIDs: []int64{7},
	}, dims)

	got := map[string]*attunev1.ClassificationQualityWarning{}
	for _, warning := range warnings {
		got[warning.GetReason()] = warning
	}
	require.Equal(t, "watch", got["dimension_distribution_drift"].GetSeverity())
	require.Equal(t, "watch", got["low_confidence_rate_spike"].GetSeverity())
	require.Equal(t, "alert", got["off_list_rate_spike"].GetSeverity())
	require.Equal(t, "watch", got["parse_failure_rate_spike"].GetSeverity())
	require.Equal(t, "alert", got["terminal_failure_rate_spike"].GetSeverity())
	require.InDelta(t, 0.05, got["low_confidence_rate_spike"].GetThreshold(), 0.001)
	require.InDelta(t, 0.05, got["off_list_rate_spike"].GetThreshold(), 0.001)

	ids := sampleIDsFromQuality(dims, []*attunev1.ClassificationQualityWarning{{
		SampleFeedbackIds: []int64{3, 4, 2},
	}})
	require.Equal(t, []int64{1, 2, 3, 9, 4}, ids)
	require.Equal(t, []int64{1, 2}, appendIDSet([]int64{1}, []int64{1, 2, 3}, 2))
}

func TestQualitySummarySeriesWorstAndSampleProtoFallbacks(t *testing.T) {
	t.Parallel()

	signal := feedbackrepo.ClassificationQualitySignalAggregate{
		ClassificationEventCount: 80,
		FailedAttemptCount:       20,
		ConfidenceCount:          40,
		ConfidenceSum:            30,
		LowConfidenceCount:       8,
		OffListCount:             4,
		UnknownDimensionCount:    2,
		ParseFailureCount:        6,
		TerminalFailureCount:     3,
	}
	summary := qualitySummary(signal, "alert")
	require.Equal(t, int64(80), summary.GetClassificationEvents())
	require.InDelta(t, 0.75, summary.GetAverageConfidence(), 0.001)
	require.InDelta(t, 0.20, summary.GetLowConfidenceRate(), 0.001)
	require.InDelta(t, 0.05, summary.GetOffListRate(), 0.001)
	require.InDelta(t, 0.025, summary.GetUnknownDimensionRate(), 0.001)
	require.InDelta(t, 0.06, summary.GetParseFailureRate(), 0.001)
	require.InDelta(t, 0.03, summary.GetTerminalFailureRate(), 0.001)
	require.Equal(t, "alert", summary.GetWorstSeverity())

	series := qualitySeriesToProto([]feedbackrepo.ClassificationQualitySeriesBucket{{
		Bucket:                   time.Date(2026, 7, 1, 12, 0, 0, 0, time.FixedZone("SGT", 8*3600)),
		ClassificationEventCount: 10,
		FailedAttemptCount:       5,
		ConfidenceCount:          4,
		ConfidenceSum:            3,
		LowConfidenceCount:       1,
		OffListCount:             2,
		UnknownDimensionCount:    3,
		ParseFailureCount:        1,
		TerminalFailureCount:     2,
	}})
	require.Len(t, series, 1)
	require.Equal(t, "2026-07-01T04:00:00Z", series[0].GetBucket())
	require.InDelta(t, 0.75, series[0].GetAverageConfidence(), 0.001)
	require.InDelta(t, 0.25, series[0].GetLowConfidenceRate(), 0.001)
	require.InDelta(t, 0.20, series[0].GetOffListRate(), 0.001)
	require.InDelta(t, 2.0/15.0, series[0].GetTerminalFailureRate(), 0.001)

	require.Equal(t, "alert", worstSeverity(
		[]*attunev1.ClassificationDimensionDrift{{Severity: "watch"}},
		[]*attunev1.ClassificationQualityWarning{{Severity: "alert"}},
	))
	require.Equal(t, "normal", worstSeverity(nil, nil))

	enrichedAt := time.Date(2026, 7, 2, 3, 4, 5, 0, time.FixedZone("SGT", 8*3600))
	confidence := 0.91
	samples := qualitySamplesToProto([]feedbackrepo.ClassificationQualitySample{
		{
			ID:               101,
			CreatedAt:        time.Date(2026, 7, 1, 1, 2, 3, 0, time.UTC),
			Source:           "api",
			Title:            "raw title",
			EnrichmentStatus: "pending",
		},
		{
			ID:                       102,
			CreatedAt:                time.Date(2026, 7, 1, 1, 2, 3, 0, time.UTC),
			EnrichedAt:               ptrext.Of(enrichedAt),
			Source:                   "api",
			Title:                    "raw title",
			DisplayTitle:             "display title",
			EnrichmentStatus:         "done",
			ClassificationConfidence: ptrext.Of(confidence),
		},
	}, "quality")
	require.Len(t, samples, 2)
	require.Equal(t, "raw title", samples[0].GetTitle())
	require.Empty(t, samples[0].GetDisplayTitle())
	require.Nil(t, samples[0].DisplayTitle)
	require.Empty(t, samples[0].GetEnrichedAt())
	require.Equal(t, "quality", samples[0].GetSignalReason())
	require.Equal(t, "display title", samples[1].GetTitle())
	require.Equal(t, "display title", samples[1].GetDisplayTitle())
	require.Equal(t, "2026-07-01T19:04:05Z", samples[1].GetEnrichedAt())
	require.InDelta(t, confidence, samples[1].GetClassificationConfidence(), 0.001)
}

func TestGetClassificationQualityReturnsSummaryWarningsAndSamples(t *testing.T) {
	t.Parallel()

	confidence := 0.42
	repo := ptrext.Of(fakeFeedbackRepo{
		qualityAggregate: feedbackrepo.ClassificationQualitySignalAggregate{
			ClassificationEventCount:         100,
			FailedAttemptCount:               10,
			ParseFailureCount:                3,
			TerminalFailureCount:             4,
			OffListCount:                     6,
			ConfidenceCount:                  100,
			ConfidenceSum:                    72,
			LowConfidenceCount:               12,
			SampleFeedbackIDs:                []int64{101, 102},
			LowConfidenceSampleFeedbackIDs:   []int64{101, 102},
			OffListSampleFeedbackIDs:         []int64{101, 102},
			ParseFailureSampleFeedbackIDs:    []int64{101, 102},
			TerminalFailureSampleFeedbackIDs: []int64{101, 102},
		},
		qualitySeries: []feedbackrepo.ClassificationQualitySeriesBucket{{
			Bucket:                   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			ClassificationEventCount: 100,
			ConfidenceCount:          100,
			ConfidenceSum:            72,
			LowConfidenceCount:       12,
		}},
		qualitySamples: []feedbackrepo.ClassificationQualitySample{{
			ID:                       101,
			CreatedAt:                time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC),
			Source:                   "api",
			Title:                    "raw title",
			DisplayTitle:             "classified sample",
			EnrichmentStatus:         "done",
			ClassificationConfidence: ptrext.Of(confidence),
		}},
	})
	tenants := ptrext.Of(fakeTenantConfigRepo{cfg: tenantrepo.EnrichConfig{
		Dimensions: domain.DimensionSet{{Name: "severity", Kind: domain.DimSingle}},
	}})
	h := ptrext.Of(FeedbackHandler{repo: repo, tenants: tenants})

	result, err := h.GetClassificationQuality(qualityTestCtx(), ptrext.Of(attunev1.GetClassificationQualityRequest{
		CurrentFrom:            "2026-07-01T00:00:00Z",
		CurrentTo:              "2026-07-02T00:00:00Z",
		BaselineFrom:           "2026-06-24T00:00:00Z",
		BaselineTo:             "2026-07-01T00:00:00Z",
		Source:                 "api",
		LogicalModel:           "classifier-v1",
		ProviderModel:          "gpt-4o-mini",
		ChannelId:              "primary",
		DimensionName:          "severity",
		Severity:               "alert",
		LowConfidenceThreshold: 0.55,
		Limit:                  5,
	}))

	require.NoError(t, err)
	require.Equal(t, 200, result.Status)
	require.Equal(t, dispatchtest.TenantID, repo.qualityRefreshOpts.TenantID)
	require.Equal(t, feedbackrepo.QualityBucketDay, repo.qualityRefreshOpts.BucketWidth)
	require.InDelta(t, 0.55, repo.qualityRefreshOpts.LowConfidenceThreshold, 0.001)
	require.Len(t, repo.qualityAggOpts, 2)
	requireQualityQueryOpts(t, repo.qualityAggOpts[0])
	requireQualityQueryOpts(t, repo.qualityAggOpts[1])
	require.NotNil(t, repo.qualitySeriesOpts)
	requireQualityQueryOpts(t, ptrext.Indirect(repo.qualitySeriesOpts))
	require.Equal(t, []int64{101, 102}, repo.qualitySampleIDs)
	body := result.Body
	require.Equal(t, int64(100), body.GetSummary().GetClassificationEvents())
	require.InDelta(t, 0.72, body.GetSummary().GetAverageConfidence(), 0.001)
	require.InDelta(t, 0.12, body.GetSummary().GetLowConfidenceRate(), 0.001)
	require.Equal(t, "alert", body.GetSummary().GetWorstSeverity())
	require.Len(t, body.GetWarnings(), 4)
	require.Equal(t, "low_confidence_rate_spike", body.GetWarnings()[0].GetReason())
	require.Len(t, body.GetSeries(), 1)
	require.Len(t, body.GetDimensions(), 0)
	require.Len(t, body.GetSamples(), 1)
	require.Equal(t, "classified sample", body.GetSamples()[0].GetTitle())
	require.InDelta(t, confidence, body.GetSamples()[0].GetClassificationConfidence(), 0.001)
}

func TestGetClassificationQualityMapsDependencyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		repo     *fakeFeedbackRepo
		tenants  *fakeTenantConfigRepo
		wantAggs int
	}{
		{
			name:    "tenant config",
			repo:    ptrext.Of(fakeFeedbackRepo{}),
			tenants: ptrext.Of(fakeTenantConfigRepo{err: errors.New("config failed")}),
		},
		{
			name:    "refresh",
			repo:    ptrext.Of(fakeFeedbackRepo{qualityRefreshErr: errors.New("refresh failed")}),
			tenants: ptrext.Of(fakeTenantConfigRepo{}),
		},
		{
			name:     "current aggregate",
			repo:     ptrext.Of(fakeFeedbackRepo{qualityAggErrs: []error{errors.New("current failed")}}),
			tenants:  ptrext.Of(fakeTenantConfigRepo{}),
			wantAggs: 1,
		},
		{
			name:     "baseline aggregate",
			repo:     ptrext.Of(fakeFeedbackRepo{qualityAggErrs: []error{nil, errors.New("baseline failed")}}),
			tenants:  ptrext.Of(fakeTenantConfigRepo{}),
			wantAggs: 2,
		},
		{
			name:     "series",
			repo:     ptrext.Of(fakeFeedbackRepo{qualitySeriesErr: errors.New("series failed")}),
			tenants:  ptrext.Of(fakeTenantConfigRepo{}),
			wantAggs: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := ptrext.Of(FeedbackHandler{repo: tt.repo, tenants: tt.tenants})

			result, err := h.GetClassificationQuality(qualityTestCtx(), ptrext.Of(attunev1.GetClassificationQualityRequest{}))

			require.Zero(t, result.Status)
			typed := ptrext.Of((*dispatcher.Error)(nil))
			require.ErrorAs(t, err, typed)
			require.Equal(t, http.StatusInternalServerError, ptrext.Indirect(typed).Status)
			require.Equal(t, attunev1.ErrorCode_INTERNAL, ptrext.Indirect(typed).Code)
			require.Len(t, tt.repo.qualityAggOpts, tt.wantAggs)
		})
	}
}

func TestGetClassificationQualityMapsValidationErrors(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(FeedbackHandler{
		repo:    ptrext.Of(fakeFeedbackRepo{}),
		tenants: ptrext.Of(fakeTenantConfigRepo{}),
	})

	_, err := h.GetClassificationQuality(qualityTestCtx(), ptrext.Of(attunev1.GetClassificationQualityRequest{
		BucketWidth: "week",
	}))

	requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
}

func TestQualitySamplesForResponseDropsSampleLookupErrors(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackRepo{qualitySamplesErr: errors.New("samples failed")})
	h := ptrext.Of(FeedbackHandler{repo: repo})

	got := h.qualitySamplesForResponse(qualityTestCtx(), dispatchtest.TenantID, []int64{101, 102})

	require.Nil(t, got)
	require.Equal(t, []int64{101, 102}, repo.qualitySampleIDs)
}

func TestGetClassificationQualitySamplesReturnsRequestedRows(t *testing.T) {
	t.Parallel()

	confidence := 0.74
	repo := ptrext.Of(fakeFeedbackRepo{
		qualitySamples: []feedbackrepo.ClassificationQualitySample{{
			ID:                       101,
			CreatedAt:                time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC),
			Source:                   "api",
			Title:                    "raw title",
			DisplayTitle:             "classified sample",
			EnrichmentStatus:         "done",
			ClassificationConfidence: ptrext.Of(confidence),
		}},
	})
	h := ptrext.Of(FeedbackHandler{repo: repo})

	result, err := h.GetClassificationQualitySamples(qualityTestCtx(), ptrext.Of(attunev1.GetClassificationQualitySamplesRequest{
		Ids: []int64{101, 102},
	}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, dispatchtest.TenantID, repo.qualityTenant)
	require.Equal(t, []int64{101, 102}, repo.qualitySampleIDs)
	require.Len(t, result.Body.GetSamples(), 1)
	require.Equal(t, "classified sample", result.Body.GetSamples()[0].GetTitle())
	require.InDelta(t, confidence, result.Body.GetSamples()[0].GetClassificationConfidence(), 0.001)
}

func TestGetClassificationQualitySamplesMapsRepoErrors(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackRepo{qualitySamplesErr: errors.New("query failed")})
	h := ptrext.Of(FeedbackHandler{repo: repo})

	result, err := h.GetClassificationQualitySamples(qualityTestCtx(), ptrext.Of(attunev1.GetClassificationQualitySamplesRequest{
		Ids: []int64{101},
	}))

	require.Zero(t, result.Status)
	typed := ptrext.Of((*dispatcher.Error)(nil))
	require.ErrorAs(t, err, typed)
	require.Equal(t, http.StatusInternalServerError, ptrext.Indirect(typed).Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, ptrext.Indirect(typed).Code)
}

func TestGetClassificationReviewLearningReturnsSummary(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	repo := ptrext.Of(fakeFeedbackRepo{
		classificationReviewLearning: feedbackrepo.ClassificationReviewLearning{
			From:                    from,
			To:                      to,
			TotalReviews:            9,
			Accepted:                5,
			Edited:                  3,
			Dismissed:               1,
			TrainingCandidateCount:  4,
			ReviewedFeedbackCount:   8,
			ClassifiedFeedbackCount: 40,
			ReviewCoverageRate:      0.2,
			ReasonBuckets: []feedbackrepo.ClassificationReviewReasonBucket{{
				SignalReason:           "low_confidence_rate_spike",
				TotalReviews:           4,
				Accepted:               1,
				Edited:                 2,
				Dismissed:              1,
				TrainingCandidateCount: 3,
				LastReviewedAt:         to.Add(-time.Hour),
			}},
			RecentEvents: []feedbackrepo.ClassificationReviewEvent{classificationReviewEventFixture(from.Add(time.Hour))},
		},
	})
	h := ptrext.Of(FeedbackHandler{repo: repo})

	result, err := h.GetClassificationReviewLearning(qualityTestCtx(), ptrext.Of(attunev1.GetClassificationReviewLearningRequest{
		CurrentFrom:  from.Format(time.RFC3339),
		CurrentTo:    to.Format(time.RFC3339),
		SignalReason: "low_confidence_rate_spike",
		Limit:        5,
	}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, dispatchtest.TenantID, repo.classificationReviewLearningOpts.TenantID)
	require.Equal(t, from, repo.classificationReviewLearningOpts.From)
	require.Equal(t, to, repo.classificationReviewLearningOpts.To)
	require.Equal(t, "low_confidence_rate_spike", repo.classificationReviewLearningOpts.SignalReason)
	require.Equal(t, 5, repo.classificationReviewLearningOpts.Limit)
	require.Equal(t, int64(9), result.Body.GetTotalReviews())
	require.Equal(t, int64(4), result.Body.GetTrainingCandidateCount())
	require.InDelta(t, 0.2, result.Body.GetReviewCoverageRate(), 0.001)
	require.Equal(t, "low_confidence_rate_spike", result.Body.GetReasonBuckets()[0].GetSignalReason())
	require.Equal(t, int64(101), result.Body.GetRecentEvents()[0].GetFeedbackId())
}

func TestRecordClassificationReviewPersistsEventAndAudit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	repo := ptrext.Of(fakeFeedbackRepo{
		classificationReviewRecord: classificationReviewEventFixture(now),
		classificationReviewLearning: feedbackrepo.ClassificationReviewLearning{
			From:                   now.AddDate(0, 0, -7),
			To:                     now,
			TotalReviews:           1,
			Edited:                 1,
			TrainingCandidateCount: 1,
		},
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(FeedbackHandler{repo: repo, audit: audit})

	result, err := h.RecordClassificationReview(qualityTestCtx(), ptrext.Of(attunev1.RecordClassificationReviewRequest{
		FeedbackId:     101,
		Outcome:        " EDITED ",
		SignalReason:   "low_confidence_rate_spike",
		CorrectionJson: `{"severity":"bug"}`,
		Note:           "corrected label",
	}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, dispatchtest.TenantID, repo.classificationReviewRecordInput.TenantID)
	require.Equal(t, int64(101), repo.classificationReviewRecordInput.FeedbackID)
	require.Equal(t, feedbackrepo.ClassificationReviewOutcomeEdited, repo.classificationReviewRecordInput.Outcome)
	require.Equal(t, `{"severity":"bug"}`, repo.classificationReviewRecordInput.CorrectionJSON)
	require.Equal(t, "corrected label", repo.classificationReviewRecordInput.Note)
	require.Equal(t, dispatchtest.UserID, repo.classificationReviewRecordInput.ReviewedBy)
	require.Equal(t, "77", result.Body.GetEvent().GetEventId())
	require.InDelta(t, 0.42, result.Body.GetEvent().GetClassificationConfidence(), 0.001)
	require.Equal(t, int64(1), result.Body.GetLearning().GetTrainingCandidateCount())
	require.Len(t, audit.events, 1)
	require.Equal(t, "classification_review.record", audit.events[0].Action)
	require.Equal(t, "101", audit.events[0].TargetID)
}

func TestRecordClassificationReviewMapsValidationAndNotFound(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(FeedbackHandler{repo: ptrext.Of(fakeFeedbackRepo{})})
	_, err := h.RecordClassificationReview(qualityTestCtx(), ptrext.Of(attunev1.RecordClassificationReviewRequest{
		FeedbackId:     101,
		Outcome:        "edited",
		CorrectionJson: "[]",
	}))
	requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)

	missing := ptrext.Of(FeedbackHandler{repo: ptrext.Of(fakeFeedbackRepo{
		classificationReviewRecordErr: feedbackrepo.ErrClassificationReviewFeedbackNotFound,
	})})
	_, err = missing.RecordClassificationReview(qualityTestCtx(), ptrext.Of(attunev1.RecordClassificationReviewRequest{
		FeedbackId: 101,
		Outcome:    "accepted",
	}))
	requireDispatcherError(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

func TestRecordClassificationQualityMetricsEdges(t *testing.T) {
	t.Parallel()

	recordClassificationQualityMetrics("tenant-1", nil, nil, nil, nil)
	recordClassificationQualityMetrics(
		"tenant-1",
		[]domain.Dimension{{Name: "severity"}},
		ptrext.Of(attunev1.ClassificationQualitySummary{
			LowConfidenceRate:    0.1,
			OffListRate:          0.2,
			ParseFailureRate:     0.3,
			TerminalFailureRate:  0.4,
			ClassificationEvents: 10,
			FailedAttempts:       4,
		}),
		[]*attunev1.ClassificationDimensionDrift{{
			DimensionName: "severity",
			JsDistance:    0.42,
		}},
		[]*attunev1.ClassificationQualityWarning{{
			Reason: "low_confidence_rate_spike",
		}},
	)
}

func classificationReviewEventFixture(reviewedAt time.Time) feedbackrepo.ClassificationReviewEvent {
	confidence := 0.42
	runID := int64(17)
	return feedbackrepo.ClassificationReviewEvent{
		ID:                       77,
		FeedbackID:               101,
		SemanticRunID:            ptrext.Of(runID),
		Outcome:                  feedbackrepo.ClassificationReviewOutcomeEdited,
		SignalReason:             "low_confidence_rate_spike",
		CorrectionJSON:           `{"severity":"bug"}`,
		Note:                     "corrected label",
		Source:                   "api",
		LogicalModel:             "classifier-v1",
		ProviderModel:            "gpt-4o-mini",
		ChannelID:                "primary",
		PromptVersion:            "default",
		PromptVersionID:          "prompt-version-1",
		ClassificationConfidence: ptrext.Of(confidence),
		ReviewedBy:               dispatchtest.UserID,
		ReviewedAt:               reviewedAt,
	}
}

func requireQualityQueryOpts(t *testing.T, opts feedbackrepo.ClassificationQualityQueryOpts) {
	t.Helper()
	require.Equal(t, dispatchtest.TenantID, opts.TenantID)
	require.Equal(t, feedbackrepo.QualityBucketDay, opts.BucketWidth)
	require.Equal(t, "api", opts.Source)
	require.Equal(t, "classifier-v1", opts.LogicalModel)
	require.Equal(t, "gpt-4o-mini", opts.ProviderModel)
	require.Equal(t, "primary", opts.ChannelID)
}

func qualityValue(
	dim string,
	status string,
	hash string,
	display string,
	count int64,
	lowConfidence int64,
	sampleID int64,
) feedbackrepo.ClassificationQualityValueAggregate {
	return feedbackrepo.ClassificationQualityValueAggregate{
		DimensionName:         dim,
		DimensionValueHash:    hash,
		DimensionValueDisplay: display,
		ValueStatus:           status,
		AppearanceCount:       count,
		EventCount:            count,
		ConfidenceCount:       count,
		ConfidenceSum:         float64(count),
		LowConfidenceCount:    lowConfidence,
		SampleFeedbackIDs:     []int64{sampleID},
	}
}

func numberStrings(from int, count int) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, strconv.Itoa(from+i))
	}
	return out
}

func qualityTestCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: dispatchtest.TenantID,
			UserID:   dispatchtest.UserID,
		}),
	})
}
