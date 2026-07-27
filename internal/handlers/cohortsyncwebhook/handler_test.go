// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-mock-fixtures

package cohortsyncwebhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/cohortsync"
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	svc "github.com/Phixsura/attune/internal/service/cohortsync"
)

func init() {
	cohortsync.ResetForTest()
}

type stubService struct {
	source     *repo.Source
	credential []byte
	applied    *cohortsync.SyncPayload
	applyErr   error
}

func (s *stubService) GetSource(_ context.Context, _ string, _ uuid.UUID) (*repo.Source, error) {
	if s.source == nil {
		return nil, repo.ErrSourceNotFound
	}
	return s.source, nil
}

func (s *stubService) DecryptCredential(_ repo.Source) ([]byte, error) {
	return s.credential, nil
}

func (s *stubService) ApplyDelta(_ context.Context, _ string, _ uuid.UUID, payload cohortsync.SyncPayload) (*svc.SyncRunResult, error) {
	s.applied = &payload
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	return &svc.SyncRunResult{}, nil
}

func (s *stubService) ApplyFullSnapshot(_ context.Context, _ string, _ uuid.UUID, payload cohortsync.SyncPayload) (*svc.SyncRunResult, error) {
	s.applied = &payload
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	return &svc.SyncRunResult{}, nil
}

func setupRouter(svc service) *chi.Mux {
	r := chi.NewMux()
	h := NewHandler(svc)
	r.Mount("/cohort-sync", h.Routes())
	return r
}

func TestAmplitude_SourceNotFound_Returns404(t *testing.T) {
	s := &stubService{source: nil}
	r := setupRouter(s)

	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+uuid.New().String()+"/add", strings.NewReader(`{}`))
	req.SetBasicAuth("key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAmplitude_BadAuth_Returns401(t *testing.T) {
	sourceID := uuid.New()
	s := &stubService{
		source:     &repo.Source{ID: sourceID, TenantID: "t1", Provider: "amplitude"},
		credential: []byte("correct-key"),
	}
	r := setupRouter(s)

	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(`{"cohort_id":"c1","operation":"add","user_ids":["u1"]}`))
	req.SetBasicAuth("wrong-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAmplitude_InvalidSourceID_Returns400(t *testing.T) {
	r := setupRouter(&stubService{})
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/not-a-uuid/add", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestMixpanel_ProviderMismatch_Returns400(t *testing.T) {
	sourceID := uuid.New()
	s := &stubService{
		source:     &repo.Source{ID: sourceID, TenantID: "t1", Provider: "amplitude"}, // wrong provider
		credential: []byte("key"),
	}
	r := setupRouter(s)

	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/mixpanel/t1/"+sourceID.String(),
		strings.NewReader(`{"action":"members","cohort_id":"c1","members":[]}`))
	req.SetBasicAuth("key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestLastPathSegment(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/a/b/create", "create"},
		{"/a/b/add", "add"},
		{"/a/b/remove", "remove"},
		{"remove", "remove"},
	}
	for _, tc := range cases {
		got := lastPathSegment(tc.path)
		if got != tc.want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
