package feedback

import (
	"context"
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
