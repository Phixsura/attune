// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
)

type fakeFeedbackRepo struct {
	listRows   []feedbackrepo.ConsoleListRow
	listOpts   feedbackrepo.ConsoleListOpts
	listTenant string

	getRow    *feedbackrepo.ConsoleDetailRow
	getID     int64
	getTenant string

	usageRows []feedbackrepo.UsageBucket
	urgent    int64
	topValues map[string][]feedbackrepo.ValueCount
}

func (f *fakeFeedbackRepo) ListForConsole(
	_ context.Context, tenantID string, opts feedbackrepo.ConsoleListOpts,
) ([]feedbackrepo.ConsoleListRow, error) {
	f.listTenant = tenantID
	f.listOpts = opts
	return f.listRows, nil
}

func (f *fakeFeedbackRepo) GetForConsole(
	_ context.Context, tenantID string, id int64,
) (*feedbackrepo.ConsoleDetailRow, error) {
	f.getTenant = tenantID
	f.getID = id
	return f.getRow, nil
}

func (f *fakeFeedbackRepo) UsageByDay(
	_ context.Context, _ string, _, _ time.Time,
) ([]feedbackrepo.UsageBucket, error) {
	return f.usageRows, nil
}

func (f *fakeFeedbackRepo) UrgentCount(_ context.Context, _ string, _, _ time.Time) (int64, error) {
	return f.urgent, nil
}

func (f *fakeFeedbackRepo) TopValuesByDim(
	_ context.Context, _ string, dim string, _ bool, _, _ time.Time, _ int,
) ([]feedbackrepo.ValueCount, error) {
	return f.topValues[dim], nil
}

type fakeTenantConfigRepo struct {
	cfg tenantrepo.EnrichConfig
}

func (f *fakeTenantConfigRepo) GetEnrichConfig(_ context.Context, _ string) (tenantrepo.EnrichConfig, error) {
	return f.cfg, nil
}

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	dims := domain.DimensionSet{
		{Name: "severity", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	}
	tenants := &fakeTenantConfigRepo{cfg: tenantrepo.EnrichConfig{Dimensions: dims}}

	t.Run("list", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			listRows: []feedbackrepo.ConsoleListRow{{
				ID:               123,
				Content:          "payment failed",
				Source:           "api",
				UserID:           "u-1",
				PageURL:          "https://example.com/pay",
				EnrichedTitle:    "Payment failed",
				EnrichedAttrs:    []byte(`{"severity":"critical","labels":["billing"]}`),
				IsUrgent:         true,
				EnrichmentStatus: "done",
				CreatedAt:        time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			}},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.List",
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return &attunev1.ListFeedbackRequest{} }, BindListRequest),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback?limit=1&q=pay&urgent=true&severity=critical&labels=billing",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.listTenant)
		require.Equal(t, 1, repo.listOpts.Limit)
		require.Equal(t, "pay", repo.listOpts.Q)
		require.NotNil(t, repo.listOpts.Urgent)
		require.True(t, *repo.listOpts.Urgent)
		require.Len(t, repo.listOpts.Attrs, 2)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "123", body["nextCursor"])
		items := body["items"].([]any)
		require.Equal(t, "123", items[0].(map[string]any)["id"])
	})

	t.Run("get", func(t *testing.T) {
		enrichedAt := time.Date(2026, 6, 9, 2, 3, 4, 0, time.UTC)
		repo := &fakeFeedbackRepo{
			getRow: &feedbackrepo.ConsoleDetailRow{
				ConsoleListRow: feedbackrepo.ConsoleListRow{
					ID:               123,
					Content:          "payment failed",
					Source:           "api",
					UserID:           "u-1",
					PageURL:          "https://example.com/pay",
					EnrichedTitle:    "Payment failed",
					EnrichedAttrs:    []byte(`{"severity":"critical"}`),
					IsUrgent:         true,
					EnrichmentStatus: "done",
					CreatedAt:        time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
				},
				SourceMeta:        []byte(`{"browser":"safari"}`),
				Attachments:       []byte(`[{"url":"https://example.com/a.png","mime":"image/png","size":42}]`),
				EnrichedAt:        &enrichedAt,
				EnrichedRationale: "critical checkout failure",
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.Get",
			dispatcher.Path(
				func() *attunev1.GetFeedbackRequest { return &attunev1.GetFeedbackRequest{} },
				dispatcher.ParamInt64("id", func(req *attunev1.GetFeedbackRequest, id int64) { req.Id = id }, "id must be an integer"),
			),
			h.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback/123",
			"",
			dispatchtest.Param{Name: "id", Value: "123"},
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, int64(123), repo.getID)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "123", body["id"])
		require.Equal(t, "critical checkout failure", body["enrichedRationale"])
		require.Equal(t, "safari", body["sourceMeta"].(map[string]any)["browser"])
	})

	t.Run("stats", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			usageRows: []feedbackrepo.UsageBucket{
				{Bucket: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Value: 3},
				{Bucket: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), Value: 4},
			},
			urgent: 2,
			topValues: map[string][]feedbackrepo.ValueCount{
				"severity": {{Value: "critical", Count: 5}},
				"labels":   {{Value: "billing", Count: 4}},
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.Stats",
			dispatcher.Empty(func() *attunev1.GetFeedbackStatsRequest { return &attunev1.GetFeedbackStatsRequest{} }),
			h.Stats,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackStatsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/stats", ""))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "7", body["total"])
		require.Equal(t, "2", body["urgentCount"])
		require.Len(t, body["dims"].([]any), 2)
	})
}
