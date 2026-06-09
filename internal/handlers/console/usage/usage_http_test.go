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
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

type fakeUsageRepo struct {
	rows   []feedbackrepo.UsageBucket
	tenant string
}

func (f *fakeUsageRepo) UsageByDay(
	_ context.Context, tenantID string, _, _ time.Time,
) ([]feedbackrepo.UsageBucket, error) {
	f.tenant = tenantID
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
		dispatchtest.Auth,
		dispatcher.Empty(func() *attunev1.GetUsageRequest { return &attunev1.GetUsageRequest{} }),
		h.Get,
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
