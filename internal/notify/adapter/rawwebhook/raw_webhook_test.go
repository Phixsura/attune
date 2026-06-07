package rawwebhook

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
)

func TestBuildRawEnvelope_AttrsShape(t *testing.T) {
	s := domain.Snapshot{
		ID:          1,
		TenantID:    "t",
		Content:     "c",
		Source:      "api",
		UserID:      "u",
		Title:       "title",
		Attrs:       map[string]any{"type": "bug", "labels": []string{"a"}},
		IsUrgent:    true,
		Rationale:   "r",
		SubmittedAt: time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC),
		EnrichedAt:  time.Date(2026, 6, 7, 0, 1, 0, 0, time.UTC),
	}
	payload, err := buildRawEnvelope(s)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	fb := got["feedback"].(map[string]any)
	if fb["submitted_at"] != "2026-06-07T00:00:00Z" {
		t.Errorf("submitted_at: %v", fb["submitted_at"])
	}
	enriched := fb["enriched"].(map[string]any)
	if enriched["is_urgent"] != true {
		t.Errorf("is_urgent")
	}
	attrs := enriched["attrs"].(map[string]any)
	if attrs["type"] != "bug" {
		t.Errorf("type attr: %v", attrs["type"])
	}
	labels := attrs["labels"].([]any)
	if len(labels) != 1 || labels[0] != "a" {
		t.Errorf("labels attr: %v", labels)
	}
	if got["event_type"] != "feedback.enriched" {
		t.Errorf("event_type")
	}
}

func TestNilSafeAttrs_NilBecomesEmpty(t *testing.T) {
	got := nilSafeAttrs(nil)
	if got == nil {
		t.Error("nil should become non-nil")
	}
	if len(got) != 0 {
		t.Errorf("len: %d", len(got))
	}
	// Pass-through for non-nil.
	in := map[string]any{"x": 1}
	out := nilSafeAttrs(in)
	if out["x"] != 1 {
		t.Errorf("pass-through broken")
	}
}

func TestBuildRawEnvelope_EmptyAttrsSerialisesAsEmptyObject(t *testing.T) {
	s := domain.Snapshot{ID: 1, TenantID: "t", Content: "c", Title: "t"}
	payload, _ := buildRawEnvelope(s)
	var got map[string]any
	_ = json.Unmarshal(payload, &got)
	enriched := got["feedback"].(map[string]any)["enriched"].(map[string]any)
	attrs, ok := enriched["attrs"].(map[string]any)
	if !ok || attrs == nil {
		t.Fatalf("attrs should be {}, got %T %v", enriched["attrs"], enriched["attrs"])
	}
	if len(attrs) != 0 {
		t.Errorf("expected empty object, got %v", attrs)
	}
}
