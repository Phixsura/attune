package handlers

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// TestIngestResponseWire pins the ingest response wire shape:
//   - Decision 1: int64 id serializes as a JSON *string* (JS-safe).
//   - Decision 2: fields are lowerCamelCase (enrichmentStatus).
func TestIngestResponseWire(t *testing.T) {
	b, err := protojson.Marshal(&attunev1.IngestResponse{Id: 12345, EnrichmentStatus: "pending"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Decode as generic JSON so the assertions are whitespace-insensitive
	// (protojson injects non-deterministic spacing on purpose).
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("re-decode: %v (%s)", err, b)
	}
	if _, ok := m["id"].(string); !ok {
		t.Errorf("id must be a JSON string, got %T in %s", m["id"], b)
	}
	if m["id"] != "12345" {
		t.Errorf("id = %v, want \"12345\" (%s)", m["id"], b)
	}
	if m["enrichmentStatus"] != "pending" {
		t.Errorf("want camelCase enrichmentStatus=pending, got %s", b)
	}
}

// TestIngestRequestDiscardUnknown proves Decision 6: the handler's lenient
// decoder ignores unknown fields (matching the old encoding/json behaviour),
// while strict protojson would reject them.
func TestIngestRequestDiscardUnknown(t *testing.T) {
	body := []byte(`{"content":"hi","source":"web","sourceUser":"u","pageUrl":"/p","legacyExtra":42}`)

	var req attunev1.IngestRequest
	if err := ingestUnmarshal.Unmarshal(body, &req); err != nil {
		t.Fatalf("DiscardUnknown must ignore unknown fields: %v", err)
	}
	if req.GetContent() != "hi" || req.GetSource() != "web" ||
		req.GetSourceUser() != "u" || req.GetPageUrl() != "/p" {
		t.Errorf("field mapping wrong: %+v", &req)
	}

	// Without DiscardUnknown, the same body must fail — proving the option is
	// load-bearing, not incidental.
	if err := protojson.Unmarshal(body, &attunev1.IngestRequest{}); err == nil {
		t.Error("strict protojson should reject the unknown field")
	}
}

// TestErrorResponseWire pins the unified error envelope: {code, message, requestId}.
// The previous {"error":"..."} shape was removed when #19 unified errors;
// the apikey middleware (the last leak, caught by E2E) now also emits this.
func TestErrorResponseWire(t *testing.T) {
	b, err := protojson.Marshal(&attunev1.ErrorResponse{Code: "validation", Message: "content is required"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("re-decode: %v (%s)", err, b)
	}
	if m["code"] != "validation" || m["message"] != "content is required" {
		t.Errorf("unified error envelope wrong: %s", b)
	}
}
