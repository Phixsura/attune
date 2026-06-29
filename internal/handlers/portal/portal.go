// SPDX-License-Identifier: Apache-2.0

// Package portal serves the public voting portal API. End-users can
// browse published feedback and cast votes without authentication.
//
// All endpoints are scoped to a tenant slug in the URL path. The portal
// is opt-in — tenants must enable it via their settings.
package portal

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

// FeedbackLister loads published feedback for the portal.
type FeedbackLister interface {
	ListPublished(tenantSlug string, limit int, cursor string) ([]PublicFeedback, string, error)
}

// VoteCaster records a vote on a feedback item.
type VoteCaster interface {
	CastVote(tenantSlug string, feedbackID string, voterFingerprint string) error
	GetVoteCounts(tenantSlug string, feedbackIDs []string) (map[string]int, error)
}

// PublicFeedback is the read-only projection shown in the portal.
type PublicFeedback struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	VoteCount int    `json:"voteCount"`
	CreatedAt string `json:"createdAt"`
}

// Handler serves the portal REST API.
type Handler struct {
	lister FeedbackLister
	voter  VoteCaster
}

// NewHandler creates a portal handler.
func NewHandler(lister FeedbackLister, voter VoteCaster) *Handler {
	return &Handler{lister: lister, voter: voter} // ptrext:allow struct-with-interfaces
}

// Routes mounts the portal under /portal/v1/{tenantSlug}.
func (h *Handler) Routes() func(chi.Router) {
	return func(r chi.Router) {
		r.Route("/{tenantSlug}", func(r chi.Router) {
			r.Get("/feedback", h.listFeedback)
			r.Post("/feedback/{feedbackID}/vote", h.castVote)
		})
	}
}

func (h *Handler) listFeedback(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "tenantSlug")
	cursor := r.URL.Query().Get("cursor")
	limit := 50

	items, nextCursor, err := h.lister.ListPublished(slug, limit, cursor)
	if err != nil {
		logext.Warnf(r.Context(), "[portal] list feedback failed,tenant:%s,err:%+v", slug, err)
		http.Error(w, `{"code":"INTERNAL","message":"failed to list feedback"}`, http.StatusInternalServerError)
		return
	}

	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	if h.voter != nil && len(ids) > 0 {
		counts, err := h.voter.GetVoteCounts(slug, ids)
		if err == nil {
			for i := range items {
				items[i].VoteCount = counts[items[i].ID]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{ // ptrext:allow encoder
		"items":      items,
		"nextCursor": nextCursor,
	})
}

func (h *Handler) castVote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "tenantSlug")
	feedbackID := chi.URLParam(r, "feedbackID")

	var body struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil { // ptrext:allow decoder
		http.Error(w, `{"code":"BAD_REQUEST","message":"invalid body"}`, http.StatusBadRequest)
		return
	}

	if body.Fingerprint == "" {
		http.Error(w, `{"code":"BAD_REQUEST","message":"fingerprint required"}`, http.StatusBadRequest)
		return
	}

	if err := h.voter.CastVote(slug, feedbackID, body.Fingerprint); err != nil {
		logext.Warnf(r.Context(), "[portal] cast vote failed,tenant:%s,feedback:%s,err:%+v", slug, feedbackID, err)
		http.Error(w, `{"code":"INTERNAL","message":"failed to cast vote"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) // ptrext:allow encoder
}
