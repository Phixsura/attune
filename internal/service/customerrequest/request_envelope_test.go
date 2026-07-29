package customerrequest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
)

func envelopeSummary() repo.Summary {
	return repo.Summary{
		ID:            uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		TenantID:      "t1",
		DisplayNumber: 42,
		DisplayID:     "REQ-42",
		Title:         "Dark mode",
		Description:   "Please add dark mode",
		Status:        repo.StatusInProgress,
		Priority:      repo.Priority("high"),
		CreatedAt:     time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC),
	}
}

func TestBuildRequestEnvelope_StatusChanged(t *testing.T) {
	payload, err := BuildRequestEnvelope(envelopeSummary(), domain.EventRequestStatusChanged, "planned", "trace-1")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["version"] != "2" {
		t.Errorf("version: %v", got["version"])
	}
	if got["event_type"] != "request.status_changed" {
		t.Errorf("event_type: %v", got["event_type"])
	}
	if got["trace_id"] != "trace-1" {
		t.Errorf("trace_id: %v", got["trace_id"])
	}
	req := got["request"].(map[string]any)
	for k, want := range map[string]any{
		"id":              "11111111-2222-3333-4444-555555555555",
		"display_id":      "REQ-42",
		"title":           "Dark mode",
		"description":     "Please add dark mode",
		"status":          "in_progress",
		"previous_status": "planned",
		"priority":        "high",
		"created_at":      "2026-07-01T10:00:00Z",
		"updated_at":      "2026-07-29T11:00:00Z",
	} {
		if req[k] != want {
			t.Errorf("request.%s: got %v want %v", k, req[k], want)
		}
	}
	if _, ok := got["feedback"]; ok {
		t.Error("request envelope must not carry a feedback object")
	}
}

func TestBuildRequestEnvelope_CreatedOmitsPreviousStatus(t *testing.T) {
	payload, err := BuildRequestEnvelope(envelopeSummary(), domain.EventRequestCreated, "", "trace-2")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["event_type"] != "request.created" {
		t.Errorf("event_type: %v", got["event_type"])
	}
	req := got["request"].(map[string]any)
	if _, ok := req["previous_status"]; ok {
		t.Error("request.created must omit previous_status")
	}
}
