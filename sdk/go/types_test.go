package attune

import (
	"encoding/json"
	"testing"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestResultFromProto verifies the response mapping, in particular that the
// proto int64 id (sent as a JSON string) becomes a Go string.
func TestResultFromProto(t *testing.T) {
	var resp attunev1.IngestResponse
	if err := protojson.Unmarshal([]byte(`{"id":"98765432","enrichmentStatus":"pending"}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r := resultFromProto(&resp)
	if r.ID != "98765432" {
		t.Errorf("ID = %q, want 98765432", r.ID)
	}
	if r.EnrichmentStatus != "pending" {
		t.Errorf("EnrichmentStatus = %q, want pending", r.EnrichmentStatus)
	}
}

// TestIngestInputWireShape verifies a fully-populated input maps to the
// generated proto message and marshals to the server's lowerCamelCase wire
// names. protojson output is intentionally not byte-stable, so we assert the
// decoded shape rather than exact bytes.
func TestIngestInputWireShape(t *testing.T) {
	in := IngestInput{
		Content:    "the dashboard is slow",
		Source:     "web",
		SourceUser: "user-456",
		SourceMeta: map[string]any{"browser": "Chrome", "os": "macOS"},
		PageURL:    "https://example.com/dashboard",
	}
	req, err := in.toProto()
	if err != nil {
		t.Fatalf("toProto: %v", err)
	}
	raw, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]any{
		"content":    "the dashboard is slow",
		"source":     "web",
		"sourceUser": "user-456",
		"sourceMeta": map[string]any{"browser": "Chrome", "os": "macOS"},
		"pageUrl":    "https://example.com/dashboard",
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("wire output missing key %q", k)
			continue
		}
		if k == "sourceMeta" {
			continue // nested map compared loosely below
		}
		if gv != wv {
			t.Errorf("wire[%q] = %v, want %v", k, gv, wv)
		}
	}
	sm, _ := got["sourceMeta"].(map[string]any)
	if sm["browser"] != "Chrome" || sm["os"] != "macOS" {
		t.Errorf("sourceMeta = %v", got["sourceMeta"])
	}
}

// TestIngestInputOmitsEmptyOptionalFields ensures only content is on the wire
// when the optional fields are zero, so the server applies its defaults
// (source="api"). protojson omits proto3 zero values by default.
func TestIngestInputOmitsEmptyOptionalFields(t *testing.T) {
	req, err := IngestInput{Content: "hi"}.toProto()
	if err != nil {
		t.Fatalf("toProto: %v", err)
	}
	raw, _ := protojson.Marshal(req)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got["content"] != "hi" {
		t.Errorf("content-only wire = %v, want only {content: hi}", got)
	}
}

func TestIngestInputRejectsInvalidSourceMeta(t *testing.T) {
	// A channel value structpb cannot represent must surface as an error, not panic.
	_, err := IngestInput{Content: "x", SourceMeta: map[string]any{"bad": make(chan int)}}.toProto()
	if err == nil {
		t.Error("expected error for non-JSON sourceMeta value")
	}
}
