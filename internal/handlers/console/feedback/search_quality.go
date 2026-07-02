// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
)

const (
	searchQualityDefaultWindow = 7 * 24 * time.Hour
	searchQualityMaxWindow     = 90 * 24 * time.Hour
	searchQualityMaxHourWindow = 14 * 24 * time.Hour
	searchQualityDefaultLimit  = 10
	searchQualityMaxLimit      = 50
	searchQueryPreviewRunes    = 160
	searchEventActionOpen      = "open"
)

type searchQualityWindow struct {
	from        time.Time
	to          time.Time
	bucketWidth string
	limit       int
}

func BindSearchQualityRequest(r *http.Request, req *attunev1.GetSearchQualityRequest) error {
	q := r.URL.Query()
	req.CurrentFrom = q.Get("current_from")
	req.CurrentTo = q.Get("current_to")
	req.BucketWidth = q.Get("bucket_width")
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be an integer")
		}
		req.Limit = ptrext.Of(int32(v))
	}
	return nil
}

func (h *SearchHandler) GetSearchQuality(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetSearchQualityRequest,
) (dispatcher.Result[*attunev1.GetSearchQualityResponse], error) {
	const where = "console.SearchHandler.GetSearchQuality"
	if h.operations == nil {
		return dispatcher.Fail[*attunev1.GetSearchQualityResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"search quality operations are not configured",
		)
	}
	window, err := resolveSearchQualityWindow(req, time.Now().UTC())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetSearchQualityResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	auth := ctx.Auth
	dashboard, err := h.operations.SearchQualityDashboard(ctx, repofeedback.SearchQualityQueryOpts{
		TenantID:    auth.TenantID,
		From:        window.from,
		To:          window.to,
		BucketWidth: window.bucketWidth,
		Limit:       window.limit,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetSearchQualityResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to read search quality",
		)
	}
	return dispatcher.OK(searchQualityDashboardToProto(window, dashboard, time.Now().UTC()))
}

func (h *SearchHandler) RecordSearchEvent(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RecordSearchEventRequest,
) (dispatcher.Result[*attunev1.RecordSearchEventResponse], error) {
	if h.operations == nil {
		return dispatcher.Fail[*attunev1.RecordSearchEventResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"search quality operations are not configured",
		)
	}
	row, err := searchResultEventFromRequest(ctx.Auth, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.RecordSearchEventResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			err.Error(),
		)
	}
	err = h.operations.RecordSearchResultEvent(ctx, row)
	if errors.Is(err, repofeedback.ErrSearchRunNotFound) {
		return dispatcher.Fail[*attunev1.RecordSearchEventResponse](
			http.StatusNotFound,
			attunev1.ErrorCode_NOT_FOUND,
			"search run not found",
		)
	}
	if err != nil {
		logext.Errorf(ctx, "[console.SearchHandler.RecordSearchEvent] insert failed,tenant_id:%s,err:%+v", ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.RecordSearchEventResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to record search event",
		)
	}
	return dispatcher.OK(ptrext.Of(attunev1.RecordSearchEventResponse{}))
}

func resolveSearchQualityWindow(req *attunev1.GetSearchQualityRequest, now time.Time) (searchQualityWindow, error) {
	to := now.UTC()
	var from time.Time
	var err error
	if req.GetCurrentTo() != "" {
		to, err = time.Parse(time.RFC3339, req.GetCurrentTo())
		if err != nil {
			return searchQualityWindow{}, errors.New("current_to must be RFC3339")
		}
		to = to.UTC()
	}
	if req.GetCurrentFrom() != "" {
		from, err = time.Parse(time.RFC3339, req.GetCurrentFrom())
		if err != nil {
			return searchQualityWindow{}, errors.New("current_from must be RFC3339")
		}
		from = from.UTC()
	} else {
		from = to.Add(-searchQualityDefaultWindow)
	}
	if !from.Before(to) {
		return searchQualityWindow{}, errors.New("current_from must be before current_to")
	}
	width := req.GetBucketWidth()
	if width == "" {
		width = repofeedback.SearchQualityBucketDay
	}
	if width != repofeedback.SearchQualityBucketDay && width != repofeedback.SearchQualityBucketHour {
		return searchQualityWindow{}, errors.New("bucket_width must be hour or day")
	}
	span := to.Sub(from)
	if span > searchQualityMaxWindow {
		return searchQualityWindow{}, errors.New("search quality window must be at most 90 days")
	}
	if width == repofeedback.SearchQualityBucketHour && span > searchQualityMaxHourWindow {
		return searchQualityWindow{}, errors.New("hour bucket search quality window must be at most 14 days")
	}
	limit := searchQualityDefaultLimit
	if req.Limit != nil {
		limit = int(req.GetLimit())
	}
	if limit <= 0 || limit > searchQualityMaxLimit {
		return searchQualityWindow{}, errors.New("limit must be between 1 and 50")
	}
	return searchQualityWindow{from: from, to: to, bucketWidth: width, limit: limit}, nil
}

func searchResultEventFromRequest(auth *session.AuthCtx, req *attunev1.RecordSearchEventRequest) (repofeedback.SearchResultEventInsert, error) {
	if req.GetRunId() == "" {
		return repofeedback.SearchResultEventInsert{}, errors.New("run_id is required")
	}
	if _, err := uuid.Parse(req.GetRunId()); err != nil {
		return repofeedback.SearchResultEventInsert{}, errors.New("run_id must be a UUID")
	}
	if req.GetFeedbackId() <= 0 {
		return repofeedback.SearchResultEventInsert{}, errors.New("feedback_id must be positive")
	}
	action := normalizeSearchEventAction(req.GetAction())
	if action == "" {
		return repofeedback.SearchResultEventInsert{}, errors.New("action must be impression, open, copy, transition, or retry")
	}
	if req.GetRank() < 0 {
		return repofeedback.SearchResultEventInsert{}, errors.New("rank must be non-negative")
	}
	return repofeedback.SearchResultEventInsert{
		TenantID:    auth.TenantID,
		RunID:       req.GetRunId(),
		FeedbackID:  req.GetFeedbackId(),
		Action:      action,
		Rank:        int(req.GetRank()),
		MatchType:   truncateSearchLabel(req.GetMatchType(), 32),
		ActorUserID: auth.UserID,
	}, nil
}

func normalizeSearchEventAction(action string) string {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "impression", searchEventActionOpen, "copy", "transition", "retry":
		return strings.TrimSpace(strings.ToLower(action))
	default:
		return ""
	}
}

func (h *SearchHandler) recordSearchRun(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	auth *session.AuthCtx,
	req *attunev1.SemanticSearchRequest,
	query string,
	runID string,
	latency time.Duration,
	resp *semanticsearch.SearchResponse,
) {
	if h.operations == nil || resp == nil {
		return
	}
	coverage := resp.Coverage
	totalLive := 0
	totalWithEmbeddings := resp.TotalWithEmbeddings
	embeddingModel := resp.EmbeddingModel
	if coverage != nil {
		totalLive = coverage.TotalLiveFeedback
		totalWithEmbeddings = coverage.TotalWithEmbeddings
		if embeddingModel == "" {
			embeddingModel = coverage.EmbeddingModel
		}
	}
	row := repofeedback.SearchRunInsert{
		TenantID:            auth.TenantID,
		RunID:               runID,
		QueryHash:           searchStableHash(strings.ToLower(query)),
		QueryPreview:        truncateSearchLabel(query, searchQueryPreviewRunes),
		FilterHash:          searchFilterHash(req.GetFilter()),
		RankingVersion:      resp.RankingVersion,
		EmbeddingModel:      embeddingModel,
		ResultCount:         len(resp.Hits),
		UsedKeywordFallback: resp.UsedKeywordFallback,
		FallbackReason:      resp.FallbackReason,
		LatencyMS:           int(latency.Milliseconds()),
		TotalLiveFeedback:   totalLive,
		TotalWithEmbeddings: totalWithEmbeddings,
		CoverageRatio:       searchCoverageRatio(totalWithEmbeddings, totalLive),
		ActorUserID:         auth.UserID,
	}
	if err := h.operations.RecordSearchRun(ctx, row); err != nil {
		logext.Warnf(ctx, "[console.SearchHandler.Search] search telemetry write failed,tenant_id:%s,err:%+v", auth.TenantID, err.Error())
	}
}

func newSearchRunID() string {
	return uuid.NewString()
}

func searchStableHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func searchFilterHash(filter *attunev1.FeedbackFilter) string {
	if filter == nil {
		return ""
	}
	raw, err := protojson.MarshalOptions{EmitUnpopulated: false}.Marshal(filter)
	if err != nil {
		return ""
	}
	return searchStableHash(string(raw))
}

func truncateSearchLabel(value string, maxRunes int) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, "\uFFFD"))
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func searchQualityDashboardToProto(
	window searchQualityWindow,
	dashboard *repofeedback.SearchQualityDashboard,
	generatedAt time.Time,
) *attunev1.GetSearchQualityResponse {
	if dashboard == nil {
		dashboard = ptrext.Of(repofeedback.SearchQualityDashboard{})
	}
	summary := searchQualitySummaryToProto(dashboard.Summary, dashboard.IndexHealth)
	return ptrext.Of(attunev1.GetSearchQualityResponse{
		GeneratedAt:       generatedAt.UTC().Format(time.RFC3339),
		CurrentFrom:       window.from.UTC().Format(time.RFC3339),
		CurrentTo:         window.to.UTC().Format(time.RFC3339),
		BucketWidth:       window.bucketWidth,
		Summary:           summary,
		Series:            searchQualitySeriesToProto(dashboard.Series),
		Queries:           searchQualityQueriesToProto(dashboard.Queries),
		ZeroResultQueries: searchQualityQueriesToProto(dashboard.ZeroResultQueries),
		FallbackBreakdown: searchFallbackBreakdownToProto(dashboard.FallbackBreakdown),
		IndexHealth:       searchIndexHealthToProto(dashboard.IndexHealth),
		RankingVersions:   searchRankingVersionsToProto(dashboard.RankingVersions),
	})
}

func searchQualitySummaryToProto(
	row repofeedback.SearchQualitySummary,
	health repofeedback.SearchIndexHealth,
) *attunev1.SearchQualitySummary {
	return ptrext.Of(attunev1.SearchQualitySummary{
		QueryCount:         row.QueryCount,
		ZeroResultCount:    row.ZeroResultCount,
		ZeroResultRate:     rate(row.ZeroResultCount, row.QueryCount),
		FallbackCount:      row.FallbackCount,
		FallbackRate:       rate(row.FallbackCount, row.QueryCount),
		ClickCount:         row.ClickCount,
		ClickThroughRate:   rate(row.ClickedRunCount, row.QueryCount),
		AverageResultCount: row.AverageResultCount,
		P95LatencyMs:       row.P95LatencyMS,
		WorstSeverity:      searchWorstSeverity(row, health),
	})
}

func searchQualitySeriesToProto(rows []repofeedback.SearchQualitySeriesBucket) []*attunev1.SearchQualityTimeBucket {
	out := make([]*attunev1.SearchQualityTimeBucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, ptrext.Of(attunev1.SearchQualityTimeBucket{
			Bucket:           row.Bucket.UTC().Format(time.RFC3339),
			QueryCount:       row.QueryCount,
			ZeroResultCount:  row.ZeroResultCount,
			ZeroResultRate:   rate(row.ZeroResultCount, row.QueryCount),
			FallbackCount:    row.FallbackCount,
			FallbackRate:     rate(row.FallbackCount, row.QueryCount),
			ClickCount:       row.ClickCount,
			ClickThroughRate: rate(row.ClickedRunCount, row.QueryCount),
			P95LatencyMs:     row.P95LatencyMS,
		}))
	}
	return out
}

func searchQualityQueriesToProto(rows []repofeedback.SearchQualityQueryAggregate) []*attunev1.SearchQualityQuery {
	out := make([]*attunev1.SearchQualityQuery, 0, len(rows))
	for _, row := range rows {
		out = append(out, ptrext.Of(attunev1.SearchQualityQuery{
			QueryHash:          row.QueryHash,
			QueryPreview:       row.QueryPreview,
			QueryCount:         row.QueryCount,
			ZeroResultCount:    row.ZeroResultCount,
			ZeroResultRate:     rate(row.ZeroResultCount, row.QueryCount),
			FallbackCount:      row.FallbackCount,
			ClickCount:         row.ClickCount,
			ClickThroughRate:   rate(row.ClickedRunCount, row.QueryCount),
			AverageResultCount: row.AverageResultCount,
			P95LatencyMs:       row.P95LatencyMS,
			LastSeenAt:         row.LastSeenAt.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func searchFallbackBreakdownToProto(rows []repofeedback.SearchFallbackAggregate) []*attunev1.SearchFallbackBreakdown {
	out := make([]*attunev1.SearchFallbackBreakdown, 0, len(rows))
	for _, row := range rows {
		out = append(out, ptrext.Of(attunev1.SearchFallbackBreakdown{
			Reason: row.Reason,
			Count:  row.Count,
			Share:  row.Share,
		}))
	}
	return out
}

func searchIndexHealthToProto(row repofeedback.SearchIndexHealth) *attunev1.SearchIndexHealth {
	out := ptrext.Of(attunev1.SearchIndexHealth{
		TotalLiveFeedback:    int32(row.TotalLiveFeedback),
		TotalWithEmbeddings:  int32(row.TotalWithEmbeddings),
		CoverageRatio:        searchCoverageRatio(int(row.TotalWithEmbeddings), int(row.TotalLiveFeedback)),
		EmbeddingModel:       row.EmbeddingModel,
		MissingFeedbackCount: row.MissingFeedbackCount,
	})
	if row.OldestMissingFeedbackAt != nil {
		out.OldestMissingFeedbackAt = ptrext.Of(row.OldestMissingFeedbackAt.UTC().Format(time.RFC3339))
	}
	return out
}

func searchRankingVersionsToProto(rows []repofeedback.SearchRankingVersion) []*attunev1.SearchRankingVersion {
	if len(rows) == 0 {
		return []*attunev1.SearchRankingVersion{{
			RankingVersion: semanticsearch.RankingVersion,
			Status:         "active",
			TrafficPercent: 100,
			Notes:          "Current production ranker",
			UpdatedAt:      "",
		}}
	}
	out := make([]*attunev1.SearchRankingVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, ptrext.Of(attunev1.SearchRankingVersion{
			RankingVersion: row.RankingVersion,
			Status:         row.Status,
			TrafficPercent: int32(row.TrafficPercent),
			Notes:          row.Notes,
			UpdatedAt:      row.UpdatedAt.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func searchWorstSeverity(
	row repofeedback.SearchQualitySummary,
	health repofeedback.SearchIndexHealth,
) string {
	if row.QueryCount == 0 {
		return "insufficient_data"
	}
	zeroRate := rate(row.ZeroResultCount, row.QueryCount)
	fallbackRate := rate(row.FallbackCount, row.QueryCount)
	coverage := searchCoverageRatio(int(health.TotalWithEmbeddings), int(health.TotalLiveFeedback))
	switch {
	case zeroRate >= 0.20 || fallbackRate >= 0.25 || row.P95LatencyMS >= 3000 || coverage < 0.80:
		return "alert"
	case zeroRate >= 0.10 || fallbackRate >= 0.10 || row.P95LatencyMS >= 1200 || coverage < 0.95:
		return "watch"
	default:
		return "normal"
	}
}

func searchCoverageRatio(withEmbeddings, total int) float64 {
	if total <= 0 {
		return 1
	}
	return clampUnit(float64(withEmbeddings) / float64(total))
}

func rate(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return clampUnit(float64(part) / float64(total))
}

func clampUnit(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
