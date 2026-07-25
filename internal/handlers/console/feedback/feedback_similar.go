// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
)

const (
	// similarFeedbackLimit bounds the recurrence signal — operators need
	// "this came up N more times", not an exhaustive listing.
	similarFeedbackLimit = 5
	// similarFeedbackMinSimilarity filters to genuinely-the-same-issue
	// neighbors; matches the embedding worker's clustering threshold band.
	similarFeedbackMinSimilarity = 0.78
)

// similarFeedbackFinder is the narrow repo surface for recurrence lookups.
type similarFeedbackFinder interface {
	FindSimilarFeedback(ctx context.Context, tenantID string, feedbackID int64, limit int, minSimilarity float64) ([]repofeedback.SemanticSearchHit, error)
}

// requestLinkReader resolves which customer requests already reference a
// set of feedback rows — the dedup signal that turns "promote" into
// "link to the existing request" when the recurring cluster is already
// being tracked.
type requestLinkReader interface {
	RequestsLinkedToFeedback(ctx context.Context, tenantID string, feedbackIDs []int64) (map[int64][]repofeedback.LinkedRequestRef, error)
}

// SetSimilarFinder wires the semantic-similarity reader. Nil (no
// embeddings configured) keeps the endpoint returning empty lists.
func (h *FeedbackHandler) SetSimilarFinder(f similarFeedbackFinder) { h.similarFinder = f }

// SetRequestLinkReader wires the linked-request resolver for the dedup
// suggestion on the candidate card.
func (h *FeedbackHandler) SetRequestLinkReader(r requestLinkReader) { h.requestLinks = r }

// linkedRequestRef is the wire shape for an existing customer request
// that already tracks one of the neighbors.
type linkedRequestRef struct {
	ID     string `json:"id"`
	CrNo   int64  `json:"cr_no"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// similarFeedbackItem is the wire shape for one recurrence neighbor.
type similarFeedbackItem struct {
	ID             int64              `json:"id"`
	Title          string             `json:"title"`
	Source         string             `json:"source"`
	Similarity     float64            `json:"similarity"`
	CreatedAt      string             `json:"created_at"`
	LinkedRequests []linkedRequestRef `json:"linked_requests,omitempty"`
}

// SimilarFeedback handles GET /fb/v1/console/feedback/{id}/similar.
// Returns semantically-similar feedback — the "recurring signal" behind
// a request candidate. Uses a raw http.HandlerFunc because the response
// is a lightweight operational shape, not a proto message (same pattern
// as inbound's RecentFeedback).
func (h *FeedbackHandler) SimilarFeedback(w http.ResponseWriter, r *http.Request) {
	const where = "console.FeedbackHandler.SimilarFeedback"
	auth := session.FromContext(r.Context())
	feedbackID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || feedbackID <= 0 {
		http.Error(w, "id must be a positive integer", http.StatusBadRequest)
		return
	}

	items := []similarFeedbackItem{}
	if h.similarFinder != nil {
		hits, ferr := h.similarFinder.FindSimilarFeedback(r.Context(), auth.TenantID, feedbackID, similarFeedbackLimit, similarFeedbackMinSimilarity)
		if ferr != nil {
			// No embedding yet (fresh row, embeddings disabled) is the
			// common case — an empty recurrence signal, not an error.
			if !isNoEmbeddingError(ferr) {
				logext.Warnf(r.Context(), "[%s] find similar failed,tenant_id:%s,feedback_id:%d,err:%+v",
					where, auth.TenantID, feedbackID, ferr.Error())
			}
		}
		for _, hit := range hits {
			title := hit.Feedback.EnrichedTitle
			if title == "" {
				title = firstLine(hit.Feedback.Content)
			}
			items = append(items, similarFeedbackItem{
				ID:         hit.Feedback.ID,
				Title:      title,
				Source:     hit.Feedback.Source,
				Similarity: hit.Similarity,
				CreatedAt:  hit.Feedback.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		h.attachLinkedRequests(r.Context(), auth.TenantID, items, where)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items}) //nolint:errcheck
}

// attachLinkedRequests hydrates each neighbor with the customer requests
// already tracking it — the dedup signal. Best-effort: a resolver
// failure leaves the field empty rather than failing the endpoint.
func (h *FeedbackHandler) attachLinkedRequests(ctx context.Context, tenantID string, items []similarFeedbackItem, where string) {
	if h.requestLinks == nil || len(items) == 0 {
		return
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	linked, err := h.requestLinks.RequestsLinkedToFeedback(ctx, tenantID, ids)
	if err != nil {
		logext.Warnf(ctx, "[%s] resolve linked requests failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return
	}
	for i := range items {
		for _, ref := range linked[items[i].ID] {
			items[i].LinkedRequests = append(items[i].LinkedRequests, linkedRequestRef{
				ID:     ref.ID,
				CrNo:   ref.CrNo,
				Title:  ref.Title,
				Status: ref.Status,
			})
		}
	}
}

// isNoEmbeddingError matches the repo's has-no-embedding failure, which
// simply means the recurrence signal isn't available yet.
func isNoEmbeddingError(err error) bool {
	return strings.Contains(err.Error(), "has no embedding")
}

// firstLine trims content to its first line, capped for display.
func firstLine(content string) string {
	if idx := strings.IndexByte(content, '\n'); idx > 0 {
		content = content[:idx]
	}
	content = strings.TrimSpace(content)
	if len(content) > 120 {
		content = content[:120]
	}
	return content
}
