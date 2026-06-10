// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
)

type fakeUsageRepo struct {
	rows   []feedbackrepo.UsageBucket
	tenant string
}

type fakeLLMUsageRepo struct {
	rows        []llmauditrepo.UsageBucket
	tenant      string
	granularity llmauditrepo.UsageGranularity
}

func (f *fakeUsageRepo) UsageByDay(
	_ context.Context, tenantID string, _, _ time.Time,
) ([]feedbackrepo.UsageBucket, error) {
	f.tenant = tenantID
	return f.rows, nil
}

func (f *fakeLLMUsageRepo) UsageByTenant(
	_ context.Context,
	tenantID string,
	granularity llmauditrepo.UsageGranularity,
	_, _ time.Time,
) ([]llmauditrepo.UsageBucket, error) {
	f.tenant = tenantID
	f.granularity = granularity
	return f.rows, nil
}

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	repo := &fakeUsageRepo{
		rows: []feedbackrepo.UsageBucket{
			{Bucket: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Value: 3},
			{Bucket: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), Value: 4},
		},
	}
	h := &UsageHandler{repo: repo}
	handler := dispatcher.Bind(
		"console.UsageHandler.Get",
		dispatcher.Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		h.Get,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetUsageRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/usage", ""))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, dispatchtest.TenantID, repo.tenant)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "7", body["total"])
	require.Len(t, body["series"].([]any), 2)
	require.Nil(t, body["quota"])
}

func TestLLMUsageHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	repo := &fakeLLMUsageRepo{
		rows: []llmauditrepo.UsageBucket{{
			Bucket:           time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			TenantID:         dispatchtest.TenantID,
			ModelID:          "gpt-4o-mini",
			PromptTokens:     100,
			CompletionTokens: 50,
			CostUSD:          0.0015,
			Calls:            2,
			Errors:           1,
		}},
	}
	h := &UsageHandler{llm: repo}
	handler := dispatcher.Bind(
		"console.UsageHandler.GetLLMUsage",
		dispatcher.Query(
			func() *attunev1.GetLLMUsageRequest { return &attunev1.GetLLMUsageRequest{} },
			BindLLMUsageRequest,
		),
		h.GetLLMUsage,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetLLMUsageRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/llm-usage?granularity=month&range=now-90d", ""))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, dispatchtest.TenantID, repo.tenant)
	require.Equal(t, llmauditrepo.GranularityMonth, repo.granularity)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "100", body["promptTokens"])
	require.Equal(t, "50", body["completionTokens"])
	require.Equal(t, "2", body["calls"])
	require.Equal(t, "1", body["errors"])
	require.Equal(t, 0.0015, body["costUsd"])
	require.Len(t, body["series"].([]any), 1)
	series := body["series"].([]any)[0].(map[string]any)
	require.Equal(t, "gpt-4o-mini", series["modelId"])
	require.Equal(t, 0.0015, series["costUsd"])
}

func TestBindLLMUsageRequestAcceptsEnumSymbols(t *testing.T) {
	t.Parallel()

	req := &attunev1.GetLLMUsageRequest{}
	httpReq := httptest.NewRequest(http.MethodGet, "/?granularity=USAGE_GRANULARITY_DAY", nil)

	require.NoError(t, BindLLMUsageRequest(httpReq, req))
	require.Equal(t, attunev1.UsageGranularity_USAGE_GRANULARITY_DAY, req.GetGranularity())
}

func TestBindLLMUsageRequestNormalizesRangeExpressions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "default", raw: "", want: "now-30d"},
		{name: "legacy days", raw: "90d", want: "now-90d"},
		{name: "legacy month", raw: "month", want: "now/M"},
		{name: "grafana relative", raw: "now-7d", want: "now-7d"},
		{name: "grafana month", raw: "now/M", want: "now/M"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := &attunev1.GetLLMUsageRequest{}
			httpReq := httptest.NewRequest(http.MethodGet, "/?range="+tc.raw, nil)

			require.NoError(t, BindLLMUsageRequest(httpReq, req))
			require.Equal(t, tc.want, req.GetRange())
		})
	}
}

func TestLLMUsageRejectsInvalidFilters(t *testing.T) {
	t.Parallel()

	repo := &fakeLLMUsageRepo{}
	h := &UsageHandler{llm: repo}
	handler := dispatcher.Bind(
		"console.UsageHandler.GetLLMUsage",
		dispatcher.Query(
			func() *attunev1.GetLLMUsageRequest { return &attunev1.GetLLMUsageRequest{} },
			BindLLMUsageRequest,
		),
		h.GetLLMUsage,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetLLMUsageRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	for _, path := range []string{
		"/fb/v1/console/llm-usage?granularity=hour",
		"/fb/v1/console/llm-usage?range=365d",
	} {
		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, path, ""))

		require.Equal(t, http.StatusBadRequest, w.Code, path)
	}
}
