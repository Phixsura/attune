// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-mock-fixtures

package cohortsyncwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/cohortsync"
	// Adapters are registered via blank-import in TestMain to comply with
	// the cohortsync-boundary depguard rule (only cmd/attune may import adapters).
	// We use an indirect import via a test helper file.
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	svc "github.com/Phixsura/attune/internal/service/cohortsync"
)

func TestMain(m *testing.M) {
	cohortsync.ResetForTest()
	// Register minimal stub providers for full-flow tests. These stubs
	// delegate to the real ParseWebhook/Check/PullCohort implementations
	// would be in the adapter packages, but since depguard forbids importing
	// them here we register stubs that handle just enough for handler tests.
	cohortsync.Register("amplitude", "Amplitude", func() cohortsync.Provider {
		return stubAmplitudeProvider{}
	})
	cohortsync.Register("mixpanel", "Mixpanel", func() cohortsync.Provider {
		return stubMixpanelProvider{}
	})
	os.Exit(m.Run())
}

// stubAmplitudeProvider is a minimal ParseWebhook implementation for handler tests.
type stubAmplitudeProvider struct{}

func (stubAmplitudeProvider) Provider() string { return "amplitude" }
func (stubAmplitudeProvider) Check(_ context.Context, _ cohortsync.Connection) (cohortsync.CheckResult, error) {
	return cohortsync.CheckResult{OK: true}, nil
}

func (stubAmplitudeProvider) PullCohort(_ context.Context, _ cohortsync.Connection, _ string) (cohortsync.SyncPayload, error) {
	return cohortsync.SyncPayload{}, nil
}

func (stubAmplitudeProvider) ParseWebhook(body []byte, headers map[string]string, _ []byte) (cohortsync.SyncPayload, error) {
	// Minimal amplitude-compatible parser for handler tests.
	type p struct {
		CohortID string   `json:"cohort_id"`
		UserIDs  []string `json:"user_ids"`
	}
	var payload p
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		return cohortsync.SyncPayload{}, fmt.Errorf("amplitude stub: %w", err)
	}
	if payload.CohortID == "" {
		return cohortsync.SyncPayload{}, fmt.Errorf("amplitude stub: cohort_id required")
	}
	deltas := make([]cohortsync.MemberDelta, 0, len(payload.UserIDs))
	for _, uid := range payload.UserIDs {
		deltas = append(deltas, cohortsync.MemberDelta{ExternalUserID: uid, Action: "add"})
	}
	return cohortsync.SyncPayload{Provider: "amplitude", ExternalCohortID: payload.CohortID, Deltas: deltas}, nil
}

// stubMixpanelProvider is a minimal ParseWebhook implementation for handler tests.
type stubMixpanelProvider struct{}

func (stubMixpanelProvider) Provider() string { return "mixpanel" }
func (stubMixpanelProvider) Check(_ context.Context, _ cohortsync.Connection) (cohortsync.CheckResult, error) {
	return cohortsync.CheckResult{OK: true}, nil
}

func (stubMixpanelProvider) PullCohort(_ context.Context, _ cohortsync.Connection, _ string) (cohortsync.SyncPayload, error) {
	return cohortsync.SyncPayload{}, nil
}

func (stubMixpanelProvider) ParseWebhook(body []byte, _ map[string]string, _ []byte) (cohortsync.SyncPayload, error) {
	type member struct {
		DistinctID string `json:"mixpanel_distinct_id"`
		Email      string `json:"email"`
	}
	type p struct {
		Action   string   `json:"action"`
		CohortID string   `json:"cohort_id"`
		Members  []member `json:"members"`
	}
	var payload p
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		return cohortsync.SyncPayload{}, fmt.Errorf("mixpanel stub: %w", err)
	}
	if payload.CohortID == "" {
		return cohortsync.SyncPayload{}, fmt.Errorf("mixpanel stub: cohort_id required")
	}
	isSnapshot := payload.Action == "members"
	deltas := make([]cohortsync.MemberDelta, 0, len(payload.Members))
	for _, m := range payload.Members {
		deltas = append(deltas, cohortsync.MemberDelta{ExternalUserID: m.DistinctID, Email: m.Email, Action: "add"})
	}
	return cohortsync.SyncPayload{Provider: "mixpanel", ExternalCohortID: payload.CohortID, IsFullSnapshot: isSnapshot, Deltas: deltas}, nil
}

type stubService struct {
	source         *repo.Source
	credential     []byte
	applied        *cohortsync.SyncPayload
	applyErr       error
	recordEventErr error
	duplicateEvent bool
	updatedStatus  string
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

func (s *stubService) ApplyFullSnapshot(_ context.Context, _ string, _ uuid.UUID, payload cohortsync.SyncPayload, _ string) (*svc.SyncRunResult, error) {
	s.applied = &payload
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	return &svc.SyncRunResult{}, nil
}

func (s *stubService) RecordEvent(_ context.Context, in repo.SyncEvent) (*repo.SyncEvent, error) {
	if s.duplicateEvent {
		return nil, repo.ErrDuplicateEvent
	}
	if s.recordEventErr != nil {
		return nil, s.recordEventErr
	}
	row := in
	row.ID = uuid.New()
	return &row, nil
}

func (s *stubService) UpdateEventStatus(_ context.Context, _ uuid.UUID, status string, _ *uuid.UUID, _ string) error {
	s.updatedStatus = status
	return nil
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
		source:     &repo.Source{ID: sourceID, TenantID: "t1", Provider: "amplitude", Enabled: true},
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
		source:     &repo.Source{ID: sourceID, TenantID: "t1", Provider: "amplitude", Enabled: true}, // wrong provider
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

// --- Full flow tests -------------------------------------------------

func newFullFlowStub(provider string) (*stubService, uuid.UUID) {
	sourceID := uuid.New()
	return &stubService{
		source:     &repo.Source{ID: sourceID, TenantID: "t1", Provider: provider, Enabled: true},
		credential: []byte("test-api-key"),
	}, sourceID
}

func TestAmplitude_FullFlow_Success(t *testing.T) {
	s, sourceID := newFullFlowStub("amplitude")
	r := setupRouter(s)

	body := `{"cohort_id":"c1","cohort_name":"Test Cohort","user_ids":["u1","u2"]}`
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(body))
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if s.applied == nil {
		t.Fatal("expected payload to be applied")
	}
	if s.applied.Provider != "amplitude" {
		t.Errorf("provider = %q, want amplitude", s.applied.Provider)
	}
	if s.applied.ExternalCohortID != "c1" {
		t.Errorf("cohort_id = %q, want c1", s.applied.ExternalCohortID)
	}
	if len(s.applied.Deltas) != 2 {
		t.Errorf("deltas = %d, want 2", len(s.applied.Deltas))
	}
	if s.updatedStatus != "processed" {
		t.Errorf("event status = %q, want processed", s.updatedStatus)
	}
}

func TestMixpanel_FullFlow_Success(t *testing.T) {
	s, sourceID := newFullFlowStub("mixpanel")
	r := setupRouter(s)

	body := `{"action":"members","cohort_id":"mx1","cohort_name":"Mixpanel Cohort","members":[{"mixpanel_distinct_id":"d1","email":"a@b.com"}]}`
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/mixpanel/t1/"+sourceID.String(),
		strings.NewReader(body))
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if s.applied == nil {
		t.Fatal("expected payload to be applied")
	}
	if s.applied.Provider != "mixpanel" {
		t.Errorf("provider = %q, want mixpanel", s.applied.Provider)
	}
	if !s.applied.IsFullSnapshot {
		t.Error("expected IsFullSnapshot=true for members action")
	}
	if len(s.applied.Deltas) != 1 {
		t.Errorf("deltas = %d, want 1", len(s.applied.Deltas))
	}
}

func TestAmplitude_FullFlow_Duplicate(t *testing.T) {
	s, sourceID := newFullFlowStub("amplitude")
	s.duplicateEvent = true
	r := setupRouter(s)

	body := `{"cohort_id":"c1","user_ids":["u1"]}`
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(body))
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (idempotent duplicate)", w.Code)
	}
	if s.applied != nil {
		t.Error("expected no apply on duplicate event")
	}
}

func TestAmplitude_FullFlow_ApplyError(t *testing.T) {
	s, sourceID := newFullFlowStub("amplitude")
	s.applyErr = fmt.Errorf("something broke")
	r := setupRouter(s)

	body := `{"cohort_id":"c1","user_ids":["u1"]}`
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(body))
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if s.updatedStatus != "failed" {
		t.Errorf("event status = %q, want failed", s.updatedStatus)
	}
}

func TestAmplitude_DisabledSource(t *testing.T) {
	sourceID := uuid.New()
	s := &stubService{
		source:     &repo.Source{ID: sourceID, TenantID: "t1", Provider: "amplitude", Enabled: false},
		credential: []byte("key"),
	}
	r := setupRouter(s)

	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(`{"cohort_id":"c1","user_ids":["u1"]}`))
	req.SetBasicAuth("key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (disabled source returns OK)", w.Code)
	}
	if s.applied != nil {
		t.Error("expected no apply for disabled source")
	}
}

// --- Error path tests ------------------------------------------------

func TestAmplitude_BodyTooLarge(t *testing.T) {
	s, sourceID := newFullFlowStub("amplitude")
	r := setupRouter(s)

	// Build a body that exceeds 32MB.
	bigBody := strings.NewReader(strings.Repeat("x", 33<<20))
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add", bigBody)
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}

func TestAmplitude_InvalidJSON(t *testing.T) {
	s, sourceID := newFullFlowStub("amplitude")
	r := setupRouter(s)

	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(`{not json at all`))
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAmplitude_ParseError_MissingCohortID(t *testing.T) {
	s, sourceID := newFullFlowStub("amplitude")
	r := setupRouter(s)

	body := `{"user_ids":["u1"]}`
	req := httptest.NewRequest(http.MethodPost, "/cohort-sync/amplitude/t1/"+sourceID.String()+"/add",
		strings.NewReader(body))
	req.SetBasicAuth("test-api-key", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing cohort_id", w.Code)
	}
}

// --- Helper function tests -------------------------------------------

func TestEventType_Snapshot(t *testing.T) {
	got := eventType(cohortsync.SyncPayload{IsFullSnapshot: true})
	if got != "full_snapshot" {
		t.Errorf("eventType(snapshot) = %q, want full_snapshot", got)
	}
}

func TestEventType_Incremental(t *testing.T) {
	got := eventType(cohortsync.SyncPayload{IsFullSnapshot: false})
	if got != "incremental" {
		t.Errorf("eventType(incremental) = %q, want incremental", got)
	}
}

// --- reject() error classification tests -----------------------------

func TestReject_Validation(t *testing.T) {
	h := NewHandler(&stubService{})
	w := httptest.NewRecorder()
	ctx := context.Background()

	err := fmt.Errorf("bad input: %w", svc.ErrValidation)
	h.reject(ctx, w, "test", err)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for ErrValidation", w.Code)
	}
}

func TestReject_Conflict(t *testing.T) {
	h := NewHandler(&stubService{})
	w := httptest.NewRecorder()
	ctx := context.Background()

	err := fmt.Errorf("version conflict: %w", repo.ErrConflict)
	h.reject(ctx, w, "test", err)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for ErrConflict", w.Code)
	}
}

func TestReject_ForeignKeyViolation(t *testing.T) {
	h := NewHandler(&stubService{})
	w := httptest.NewRecorder()
	ctx := context.Background()

	// pgconn.PgError with code 23503 = FK violation.
	pgErr := &pgconn.PgError{Code: "23503", ConstraintName: "fk_source_id"}
	err := fmt.Errorf("insert membership: %w", pgErr)
	h.reject(ctx, w, "test", err)

	if w.Code != http.StatusGone {
		t.Errorf("status = %d, want 410 for FK violation", w.Code)
	}
}

func TestReject_InternalDefault(t *testing.T) {
	h := NewHandler(&stubService{})
	w := httptest.NewRecorder()
	ctx := context.Background()

	h.reject(ctx, w, "test", errors.New("unexpected boom"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for unknown error", w.Code)
	}
}

func TestReject_SourceNotFound(t *testing.T) {
	h := NewHandler(&stubService{})
	w := httptest.NewRecorder()
	ctx := context.Background()

	err := fmt.Errorf("lookup: %w", repo.ErrSourceNotFound)
	h.reject(ctx, w, "test", err)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for ErrSourceNotFound", w.Code)
	}
}

func TestReject_CohortNotFound(t *testing.T) {
	h := NewHandler(&stubService{})
	w := httptest.NewRecorder()
	ctx := context.Background()

	err := fmt.Errorf("lookup: %w", repo.ErrCohortNotFound)
	h.reject(ctx, w, "test", err)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for ErrCohortNotFound", w.Code)
	}
}

// --- readBody edge case tests ----------------------------------------

func TestReadBody_ReadError(t *testing.T) {
	w := httptest.NewRecorder()
	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPost, "/", &failReader{})

	body, ok := readBody(ctx, w, req, "test")

	if ok {
		t.Error("expected readBody to return false on read error")
	}
	if body != nil {
		t.Error("expected nil body on error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for read error", w.Code)
	}
}

// failReader is an io.ReadCloser that always returns an error.
type failReader struct{}

func (failReader) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failReader) Close() error               { return nil }
