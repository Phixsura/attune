package respond

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	httpbody "google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestProtoWritesJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Proto(rec, http.StatusAccepted, ptrext.Of(attunev1.ListNotifyTargetsResponse{}))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
}

func TestProtoWritesHTTPBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Proto(rec, http.StatusOK, ptrext.Of(httpbody.HttpBody{
		ContentType: "text/plain",
		Data:        []byte("hello"),
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Body.String(); got != "hello" {
		t.Fatalf("body = %q", got)
	}
}

func TestErrorWritesUnifiedErrorResponse(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-123")
	middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		Error(r.Context(), rec, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "nope")
	})).ServeHTTP(httptest.NewRecorder(), req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	var body attunev1.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Code != attunev1.ErrorCode_FORBIDDEN.String() || body.Message != "nope" {
		t.Fatalf("unexpected error body fields: code=%q message=%q requestId=%q", body.Code, body.Message, body.RequestId)
	}
}

func TestProtoWritesHTTPBodyWithEmptyContentType(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Proto(rec, http.StatusOK, ptrext.Of(httpbody.HttpBody{
		ContentType: "",
		Data:        []byte("binary data"),
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := rec.Body.String(); got != "binary data" {
		t.Fatalf("body = %q", got)
	}
}

func TestProtoWithPresetContentType(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "image/png")
	Proto(rec, http.StatusOK, ptrext.Of(httpbody.HttpBody{
		ContentType: "text/plain",
		Data:        []byte("data"),
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png (preset not overwritten)", got)
	}
}
