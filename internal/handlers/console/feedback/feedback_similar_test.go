// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	require.JSONEq(t, `{"items":[],"anchor_linked_requests":[]}`, rr.Body.String())

	// Nil finder (embeddings disabled) → empty list, 200.
	bare := ptrext.Of(FeedbackHandler{})
	rr2 := serveSimilar(bare, "11")
	require.Equal(t, http.StatusOK, rr2.Code)
	require.JSONEq(t, `{"items":[],"anchor_linked_requests":[]}`, rr2.Body.String())
}

type stubRequestLinks struct {
	links map[int64][]repofeedback.LinkedRequestRef
	err   error
}

func (s stubRequestLinks) RequestsLinkedToFeedback(_ context.Context, _ string, _ []int64) (map[int64][]repofeedback.LinkedRequestRef, error) {
	return s.links, s.err
}

func TestSimilarFeedback_AttachesLinkedRequests(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(FeedbackHandler{})
	h.SetSimilarFinder(stubSimilarFinder{hits: []repofeedback.SemanticSearchHit{
		{Feedback: ptrext.Of(repofeedback.SearchFeedback{ID: 42, EnrichedTitle: "t", Source: "intercom"}), Similarity: 0.9},
	}})
	h.SetRequestLinkReader(stubRequestLinks{links: map[int64][]repofeedback.LinkedRequestRef{
		42: {{ID: "uuid-1", CrNo: 7, Title: "Existing request", Status: "open"}},
		// The anchor (id 11) is itself already tracked — must surface
		// as anchor_linked_requests so the card never offers a
		// duplicate promote.
		11: {{ID: "uuid-2", CrNo: 9, Title: "Anchor request", Status: "open"}},
	}})

	rr := serveSimilar(h, "11")
	require.Equal(t, http.StatusOK, rr.Code)
	var out struct {
		Items       []similarFeedbackItem `json:"items"`
		AnchorLinks []linkedRequestRef    `json:"anchor_linked_requests"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out.Items, 1)
	require.Len(t, out.Items[0].LinkedRequests, 1)
	require.Equal(t, int64(7), out.Items[0].LinkedRequests[0].CrNo)
	require.Equal(t, "Existing request", out.Items[0].LinkedRequests[0].Title)
	require.Len(t, out.AnchorLinks, 1)
	require.Equal(t, int64(9), out.AnchorLinks[0].CrNo)

	// Resolver failure degrades to no linked_requests, not an error.
	h2 := ptrext.Of(FeedbackHandler{})
	h2.SetSimilarFinder(stubSimilarFinder{hits: []repofeedback.SemanticSearchHit{
		{Feedback: ptrext.Of(repofeedback.SearchFeedback{ID: 42, EnrichedTitle: "t", Source: "intercom"}), Similarity: 0.9},
	}})
	h2.SetRequestLinkReader(stubRequestLinks{err: errors.New("db down")})
	rr2 := serveSimilar(h2, "11")
	require.Equal(t, http.StatusOK, rr2.Code)
	var out2 struct {
		Items []similarFeedbackItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &out2))
	require.Empty(t, out2.Items[0].LinkedRequests)
}

func TestSimilarFeedback_GenericFinderErrorLogsAndDegrades(t *testing.T) {
	t.Parallel()
	// A non-embedding failure (DB down) is logged but still degrades to
	// an empty recurrence signal — the endpoint never 500s over it.
	h := ptrext.Of(FeedbackHandler{})
	h.SetSimilarFinder(stubSimilarFinder{err: errors.New("connection refused")})
	rr := serveSimilar(h, "11")
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"items":[],"anchor_linked_requests":[]}`, rr.Body.String())
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
	// Rune-safe: CJK content truncates at a character boundary.
	cjk := strings.Repeat("中", 200)
	got := firstLine(cjk)
	require.Equal(t, strings.Repeat("中", 120), got)
	require.True(t, utf8.ValidString(got))
}
