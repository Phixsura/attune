// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type mockLister struct {
	items  []PublicFeedback
	cursor string
}

func (m *mockLister) ListPublished(string, int, string) ([]PublicFeedback, string, error) {
	return m.items, m.cursor, nil
}

type mockVoter struct {
	voted  bool
	counts map[string]int
}

func (m *mockVoter) CastVote(string, string, string) error {
	m.voted = true
	return nil
}

func (m *mockVoter) GetVoteCounts(string, []string) (map[string]int, error) {
	return m.counts, nil
}

func setupRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/portal/v1", h.Routes())
	return r
}

func TestListFeedback(t *testing.T) {
	lister := &mockLister{ // ptrext:allow test-fixture
		items: []PublicFeedback{
			{ID: "1", Title: "Login Bug", Content: "Can't log in", Status: "open"},
		},
		cursor: "next123",
	}
	voter := &mockVoter{counts: map[string]int{"1": 5}} // ptrext:allow test-fixture

	h := NewHandler(lister, voter)
	r := setupRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/portal/v1/acme/feedback", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Items      []PublicFeedback `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Items))
	}
	if resp.Items[0].VoteCount != 5 {
		t.Errorf("vote count = %d, want 5", resp.Items[0].VoteCount)
	}
	if resp.NextCursor != "next123" {
		t.Errorf("cursor = %q, want %q", resp.NextCursor, "next123")
	}
}

func TestCastVote(t *testing.T) {
	voter := &mockVoter{}                 // ptrext:allow test-fixture
	h := NewHandler(&mockLister{}, voter) // ptrext:allow test-fixture
	r := setupRouter(h)

	body, _ := json.Marshal(map[string]string{"fingerprint": "fp-123"})
	req := httptest.NewRequest(http.MethodPost, "/portal/v1/acme/feedback/42/vote", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !voter.voted {
		t.Error("CastVote was not called")
	}
}

func TestCastVote_MissingFingerprint(t *testing.T) {
	h := NewHandler(&mockLister{}, &mockVoter{}) // ptrext:allow test-fixture
	r := setupRouter(h)

	body, _ := json.Marshal(map[string]string{"fingerprint": ""})
	req := httptest.NewRequest(http.MethodPost, "/portal/v1/acme/feedback/42/vote", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
