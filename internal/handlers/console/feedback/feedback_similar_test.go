// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
)

type stubSimilarFinder struct {
	hits []repofeedback.SemanticSearchHit
	err  error
}

func (s stubSimilarFinder) FindSimilarFeedback(_ context.Context, _ string, _ int64, _ int, _ float64) ([]repofeedback.SemanticSearchHit, error) {
	return s.hits, s.err
}

func serveSimilar(h *FeedbackHandler, id string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req := httptest.NewRequest(http.MethodGet, "/feedback/"+id+"/similar", nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = session.WithAuthCtx(ctx, ptrext.Of(session.AuthCtx{TenantID: "t1"}))
	h.SimilarFeedback(rr, req.WithContext(ctx))
	return rr
}

func TestSimilarFeedback_ReturnsNeighbors(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	h := ptrext.Of(FeedbackHandler{})
	h.SetSimilarFinder(stubSimilarFinder{hits: []repofeedback.SemanticSearchHit{
		{
			Feedback: ptrext.Of(repofeedback.SearchFeedback{
				ID: 42, EnrichedTitle: "PDF export blocked", Source: "intercom", CreatedAt: created,
			}),
			Similarity: 0.91,
		},
		{
			Feedback: ptrext.Of(repofeedback.SearchFeedback{
				ID: 43, Content: "export fails\nsecond line", Source: "zendesk", CreatedAt: created,
			}),
			Similarity: 0.82,
		},
	}})

	rr := serveSimilar(h, "11")
	require.Equal(t, http.StatusOK, rr.Code)
	var out struct {
		Items []similarFeedbackItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out.Items, 2)
	require.Equal(t, int64(42), out.Items[0].ID)
	require.Equal(t, "PDF export blocked", out.Items[0].Title)
	require.Equal(t, "intercom", out.Items[0].Source)
	require.InDelta(t, 0.91, out.Items[0].Similarity, 0.001)
	// Content fallback trims to the first line.
	require.Equal(t, "export fails", out.Items[1].Title)
}

func TestSimilarFeedback_EmptyOnNoEmbeddingOrNilFinder(t *testing.T) {
	t.Parallel()
	// No-embedding error → empty list, 200.
	h := ptrext.Of(FeedbackHandler{})
	h.SetSimilarFinder(stubSimilarFinder{err: errors.New("repo: feedback 11 has no embedding")})
	rr := serveSimilar(h, "11")
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"items":[]}`, rr.Body.String())

	// Nil finder (embeddings disabled) → empty list, 200.
	bare := ptrext.Of(FeedbackHandler{})
	rr2 := serveSimilar(bare, "11")
	require.Equal(t, http.StatusOK, rr2.Code)
	require.JSONEq(t, `{"items":[]}`, rr2.Body.String())
}

func TestSimilarFeedback_BadID(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(FeedbackHandler{})
	require.Equal(t, http.StatusBadRequest, serveSimilar(h, "abc").Code)
	require.Equal(t, http.StatusBadRequest, serveSimilar(h, "-1").Code)
}

func TestFirstLine(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hello", firstLine("hello\nworld"))
	require.Equal(t, "hello", firstLine("  hello  "))
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'x'
	}
	require.Len(t, firstLine(string(long)), 120)
}
