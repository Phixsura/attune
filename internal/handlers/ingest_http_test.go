package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/service/ingest"
)

// fakeIngestor records the mapped input and returns a canned (id, err) so the
// handler can be exercised end-to-end without a database.
type fakeIngestor struct {
	id    int64
	err   error
	gotIn domain.IngestInput
}

func (f *fakeIngestor) IngestRow(_ context.Context, _ string, _ uuid.UUID, in domain.IngestInput) (int64, error) {
	f.gotIn = in
	return f.id, f.err
}

type okVerifier struct{}

func (okVerifier) Lookup(_ context.Context, _ string) (string, uuid.UUID, error) {
	return "tenant-123", uuid.Nil, nil
}

func (okVerifier) LookupWithScopes(_ context.Context, _ string) (string, uuid.UUID, []domain.Scope, error) {
	return "tenant-123", uuid.Nil, domain.AllScopes, nil
}

func (okVerifier) LookupWithScopesAndIP(_ context.Context, _, _ string) (string, uuid.UUID, []domain.Scope, error) {
	return "tenant-123", uuid.Nil, domain.AllScopes, nil
}

func ingestTestServer(ing *fakeIngestor) http.Handler {
	r := chi.NewRouter()
	r.Use(apikey.Middleware(okVerifier{}))
	r.Mount("/", NewIngestHandler(ing).Routes())
	return r
}

func doIngest(t *testing.T, srv http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(body))
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body: %v (%s)", err, rec.Body)
	}
	return m
}

// Happy path through the real middleware + handler: 200, id is a JSON string
// (Decision 1), camelCase status (Decision 2), and the proto body maps cleanly
// into the domain input.
func TestIngestHTTP_Success(t *testing.T) {
	fake := ptrext.Of(fakeIngestor{id: 4242})
	rec := doIngest(t, ingestTestServer(fake),
		`{"content":"hello","source":"web","sourceUser":"u","pageUrl":"/p"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	resp := decodeBody(t, rec)
	if resp["id"] != "4242" {
		t.Errorf("id = %v (%T), want JSON string \"4242\"", resp["id"], resp["id"])
	}
	if resp["enrichmentStatus"] != "pending" {
		t.Errorf("enrichmentStatus = %v, want pending (%s)", resp["enrichmentStatus"], rec.Body)
	}
	if fake.gotIn.Content != "hello" || fake.gotIn.Source != "web" ||
		fake.gotIn.SourceUser != "u" || fake.gotIn.PageURL != "/p" {
		t.Errorf("mapped input wrong: %+v", fake.gotIn)
	}
}

// An empty source defaults to "api" (behaviour preserved from the pre-proto handler).
func TestIngestHTTP_SourceDefault(t *testing.T) {
	fake := ptrext.Of(fakeIngestor{id: 1})
	if rec := doIngest(t, ingestTestServer(fake), `{"content":"x"}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	if fake.gotIn.Source != "api" {
		t.Errorf("empty source should default to api, got %q", fake.gotIn.Source)
	}
}

// Unknown fields are ignored end-to-end (Decision 6: DiscardUnknown).
func TestIngestHTTP_DiscardUnknown(t *testing.T) {
	rec := doIngest(t, ingestTestServer(ptrext.Of(fakeIngestor{id: 1})), `{"content":"x","legacyExtra":42}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown field should be ignored; status = %d (%s)", rec.Code, rec.Body)
	}
}

// Malformed JSON → 400 with the {"error": …} envelope.
func TestIngestHTTP_InvalidJSON(t *testing.T) {
	rec := doIngest(t, ingestTestServer(ptrext.Of(fakeIngestor{})), `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := decodeBody(t, rec); got["code"] == nil || got["message"] == nil {
		t.Errorf("want {code, message, …}, got %s", rec.Body)
	}
}

// A business (validation) error from the ingestor → 400 with the error message.
func TestIngestHTTP_IngestError(t *testing.T) {
	fake := ptrext.Of(fakeIngestor{err: errors.New("content is required")})
	rec := doIngest(t, ingestTestServer(fake), `{"content":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := decodeBody(t, rec)["message"]; got != "content is required" {
		t.Errorf("error message = %v (%s)", got, rec.Body)
	}
}

// The optional Idempotency-Key request header is mapped into the domain input.
func TestIngestHTTP_IdempotencyKeyHeaderMapped(t *testing.T) {
	fake := ptrext.Of(fakeIngestor{id: 5})
	req := httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "abcd1234efgh5678")
	rec := httptest.NewRecorder()
	ingestTestServer(fake).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if fake.gotIn.IdempotencyKey != "abcd1234efgh5678" {
		t.Errorf("IdempotencyKey = %q, want abcd1234efgh5678", fake.gotIn.IdempotencyKey)
	}
}

// An idempotency conflict from the service → 409 IDEMPOTENCY_CONFLICT.
func TestIngestHTTP_IdempotencyConflict(t *testing.T) {
	fake := ptrext.Of(fakeIngestor{err: ingest.ErrIdempotencyConflict})
	rec := doIngest(t, ingestTestServer(fake), `{"content":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decodeBody(t, rec)["code"]; got != "IDEMPOTENCY_CONFLICT" {
		t.Errorf("code = %v, want IDEMPOTENCY_CONFLICT (%s)", got, rec.Body)
	}
}

// A still-pending request with the same key → 409 REQUEST_IN_PROGRESS.
func TestIngestHTTP_RequestInProgress(t *testing.T) {
	fake := ptrext.Of(fakeIngestor{err: ingest.ErrRequestInProgress})
	rec := doIngest(t, ingestTestServer(fake), `{"content":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decodeBody(t, rec)["code"]; got != "REQUEST_IN_PROGRESS" {
		t.Errorf("code = %v, want REQUEST_IN_PROGRESS (%s)", got, rec.Body)
	}
}

// No API key → the middleware rejects before the handler (proves the wiring).
func TestIngestHTTP_NoAPIKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(`{"content":"x"}`))
	rec := httptest.NewRecorder()
	ingestTestServer(ptrext.Of(fakeIngestor{})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
