// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
)

func TestResolveSearchQualityWindowValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		req  *attunev1.GetSearchQualityRequest
		want searchQualityWindow
		err  string
	}{
		{
			name: "defaults to seven day day buckets",
			req:  ptrext.Of(attunev1.GetSearchQualityRequest{}),
			want: searchQualityWindow{
				from:        now.Add(-searchQualityDefaultWindow),
				to:          now,
				bucketWidth: repofeedback.SearchQualityBucketDay,
				limit:       searchQualityDefaultLimit,
			},
		},
		{
			name: "accepts bounded hour window",
			req: ptrext.Of(attunev1.GetSearchQualityRequest{
				CurrentFrom: "2026-07-01T12:00:00Z",
				CurrentTo:   "2026-07-02T12:00:00Z",
				BucketWidth: repofeedback.SearchQualityBucketHour,
				Limit:       ptrext.Of[int32](25),
			}),
			want: searchQualityWindow{
				from:        time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
				to:          now,
				bucketWidth: repofeedback.SearchQualityBucketHour,
				limit:       25,
			},
		},
		{
			name: "rejects malformed from",
			req:  ptrext.Of(attunev1.GetSearchQualityRequest{CurrentFrom: "nope"}),
			err:  "current_from must be RFC3339",
		},
		{
			name: "rejects malformed to",
			req:  ptrext.Of(attunev1.GetSearchQualityRequest{CurrentTo: "nope"}),
			err:  "current_to must be RFC3339",
		},
		{
			name: "rejects inverted range",
			req: ptrext.Of(attunev1.GetSearchQualityRequest{
				CurrentFrom: "2026-07-02T12:00:00Z",
				CurrentTo:   "2026-07-02T12:00:00Z",
			}),
			err: "current_from must be before current_to",
		},
		{
			name: "rejects bad bucket",
			req:  ptrext.Of(attunev1.GetSearchQualityRequest{BucketWidth: "week"}),
			err:  "bucket_width must be hour or day",
		},
		{
			name: "rejects long day range",
			req: ptrext.Of(attunev1.GetSearchQualityRequest{
				CurrentFrom: "2026-01-01T00:00:00Z",
				CurrentTo:   "2026-07-02T00:00:00Z",
			}),
			err: "search quality window must be at most 90 days",
		},
		{
			name: "rejects long hour range",
			req: ptrext.Of(attunev1.GetSearchQualityRequest{
				CurrentFrom: "2026-06-01T00:00:00Z",
				CurrentTo:   "2026-07-02T00:00:00Z",
				BucketWidth: repofeedback.SearchQualityBucketHour,
			}),
			err: "hour bucket search quality window must be at most 14 days",
		},
		{
			name: "rejects limit underflow",
			req:  ptrext.Of(attunev1.GetSearchQualityRequest{Limit: ptrext.Of[int32](0)}),
			err:  "limit must be between 1 and 50",
		},
		{
			name: "rejects limit overflow",
			req:  ptrext.Of(attunev1.GetSearchQualityRequest{Limit: ptrext.Of[int32](51)}),
			err:  "limit must be between 1 and 50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveSearchQualityWindow(tt.req, now)
			if tt.err != "" {
				require.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSearchResultEventFromRequestValidation(t *testing.T) {
	t.Parallel()
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	validRunID := "11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name string
		req  *attunev1.RecordSearchEventRequest
		want repofeedback.SearchResultEventInsert
		err  string
	}{
		{
			name: "normalizes action and truncates match type",
			req: ptrext.Of(attunev1.RecordSearchEventRequest{
				RunId:      validRunID,
				FeedbackId: 42,
				Action:     " Open ",
				Rank:       3,
				MatchType:  strings.Repeat("x", 40),
			}),
			want: repofeedback.SearchResultEventInsert{
				TenantID:    "tenant-1",
				RunID:       validRunID,
				FeedbackID:  42,
				Action:      "open",
				Rank:        3,
				MatchType:   strings.Repeat("x", 29) + "...",
				ActorUserID: "user-1",
			},
		},
		{
			name: "requires run id",
			req:  ptrext.Of(attunev1.RecordSearchEventRequest{FeedbackId: 42, Action: "open"}),
			err:  "run_id is required",
		},
		{
			name: "requires uuid run id",
			req:  ptrext.Of(attunev1.RecordSearchEventRequest{RunId: "bad", FeedbackId: 42, Action: "open"}),
			err:  "run_id must be a UUID",
		},
		{
			name: "requires positive feedback id",
			req:  ptrext.Of(attunev1.RecordSearchEventRequest{RunId: validRunID, Action: "open"}),
			err:  "feedback_id must be positive",
		},
		{
			name: "rejects unknown action",
			req:  ptrext.Of(attunev1.RecordSearchEventRequest{RunId: validRunID, FeedbackId: 42, Action: "delete"}),
			err:  "action must be impression, open, copy, transition, or retry",
		},
		{
			name: "rejects negative rank",
			req:  ptrext.Of(attunev1.RecordSearchEventRequest{RunId: validRunID, FeedbackId: 42, Action: "retry", Rank: -1}),
			err:  "rank must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := searchResultEventFromRequest(auth, tt.req)
			if tt.err != "" {
				require.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSearchQualityDashboardToProtoComputesDerivedFields(t *testing.T) {
	t.Parallel()
	missingSince := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	window := searchQualityWindow{
		from:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		to:          time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		bucketWidth: repofeedback.SearchQualityBucketDay,
		limit:       10,
	}
	dashboard := ptrext.Of(repofeedback.SearchQualityDashboard{
		Summary: repofeedback.SearchQualitySummary{
			QueryCount:         10,
			ZeroResultCount:    2,
			FallbackCount:      3,
			ClickCount:         4,
			ClickedRunCount:    5,
			AverageResultCount: 2.5,
			P95LatencyMS:       3200,
		},
		Series: []repofeedback.SearchQualitySeriesBucket{{
			Bucket:          time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			QueryCount:      10,
			ZeroResultCount: 1,
			FallbackCount:   2,
			ClickCount:      3,
			ClickedRunCount: 4,
			P95LatencyMS:    1200,
		}},
		Queries: []repofeedback.SearchQualityQueryAggregate{{
			QueryHash:          "hash",
			QueryPreview:       "refund problem",
			QueryCount:         5,
			ZeroResultCount:    1,
			FallbackCount:      2,
			ClickCount:         3,
			ClickedRunCount:    4,
			AverageResultCount: 2.2,
			P95LatencyMS:       900,
			LastSeenAt:         updatedAt,
		}},
		FallbackBreakdown: []repofeedback.SearchFallbackAggregate{{
			Reason: "embedding_unavailable",
			Count:  3,
			Share:  0.75,
		}},
		IndexHealth: repofeedback.SearchIndexHealth{
			TotalLiveFeedback:       10,
			TotalWithEmbeddings:     7,
			EmbeddingModel:          "text-embedding-3-small",
			OldestMissingFeedbackAt: ptrext.Of(missingSince),
			MissingFeedbackCount:    3,
		},
		RankingVersions: []repofeedback.SearchRankingVersion{{
			RankingVersion: "rrf.next",
			Status:         "canary",
			TrafficPercent: 10,
			Notes:          "trial",
			UpdatedAt:      updatedAt,
		}},
	})

	resp := searchQualityDashboardToProto(window, dashboard, updatedAt)
	require.Equal(t, "2026-07-02T10:00:00Z", resp.GetGeneratedAt())
	require.Equal(t, "2026-07-01T00:00:00Z", resp.GetCurrentFrom())
	require.Equal(t, "2026-07-02T00:00:00Z", resp.GetCurrentTo())
	require.Equal(t, "alert", resp.GetSummary().GetWorstSeverity())
	require.InDelta(t, 0.2, resp.GetSummary().GetZeroResultRate(), 0.001)
	require.InDelta(t, 0.3, resp.GetSummary().GetFallbackRate(), 0.001)
	require.InDelta(t, 0.5, resp.GetSummary().GetClickThroughRate(), 0.001)
	require.InDelta(t, 0.4, resp.GetSeries()[0].GetClickThroughRate(), 0.001)
	require.Equal(t, "2026-07-02T10:00:00Z", resp.GetQueries()[0].GetLastSeenAt())
	require.Equal(t, "embedding_unavailable", resp.GetFallbackBreakdown()[0].GetReason())
	require.InDelta(t, 0.7, resp.GetIndexHealth().GetCoverageRatio(), 0.001)
	require.Equal(t, "2026-07-01T09:30:00Z", resp.GetIndexHealth().GetOldestMissingFeedbackAt())
	require.Equal(t, "rrf.next", resp.GetRankingVersions()[0].GetRankingVersion())
}

func TestSearchQualityFallbackHelpers(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"impression", "open", "copy", "transition", "retry"}, []string{
		normalizeSearchEventAction(" impression "),
		normalizeSearchEventAction("OPEN"),
		normalizeSearchEventAction("copy"),
		normalizeSearchEventAction("transition"),
		normalizeSearchEventAction("retry"),
	})
	require.Empty(t, normalizeSearchEventAction("archive"))

	require.Len(t, searchStableHash(" invoice "), 64)
	require.Equal(t, searchStableHash("invoice"), searchStableHash(" invoice "))
	require.Empty(t, searchFilterHash(nil))
	require.NotEmpty(t, searchFilterHash(ptrext.Of(attunev1.FeedbackFilter{Urgent: ptrext.Of(true)})))

	require.Equal(t, "界界...", truncateSearchLabel("  "+strings.Repeat("界", 6)+"  ", 5))
	require.Equal(t, "ab", truncateSearchLabel("abcdef", 2))

	require.InDelta(t, 1.0, searchCoverageRatio(0, 0), 0.001)
	require.InDelta(t, 1.0, searchCoverageRatio(12, 10), 0.001)
	require.InDelta(t, 0.0, rate(-1, 10), 0.001)
	require.InDelta(t, 1.0, rate(12, 10), 0.001)
	require.InDelta(t, 0.0, clampUnit(math.NaN()), 0.001)
	require.InDelta(t, 1.0, clampUnit(2), 0.001)

	require.Equal(t, "insufficient_data", searchWorstSeverity(repofeedback.SearchQualitySummary{}, repofeedback.SearchIndexHealth{}))
	require.Equal(t, "watch", searchWorstSeverity(
		repofeedback.SearchQualitySummary{QueryCount: 20, ZeroResultCount: 2},
		repofeedback.SearchIndexHealth{TotalLiveFeedback: 100, TotalWithEmbeddings: 94},
	))
	require.Equal(t, "normal", searchWorstSeverity(
		repofeedback.SearchQualitySummary{QueryCount: 20, ZeroResultCount: 1, FallbackCount: 1, P95LatencyMS: 500},
		repofeedback.SearchIndexHealth{TotalLiveFeedback: 100, TotalWithEmbeddings: 99},
	))

	ranking := searchRankingVersionsToProto(nil)
	require.Len(t, ranking, 1)
	require.Equal(t, semanticsearch.RankingVersion, ranking[0].GetRankingVersion())
}
