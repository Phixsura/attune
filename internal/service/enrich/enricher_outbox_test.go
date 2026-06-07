package enrich

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

func sampleSnapshot(urgent bool) domain.Snapshot {
	return domain.Snapshot{
		ID:          7,
		TenantID:    "tenant-x",
		Content:     "raw user text",
		Source:      "api",
		UserID:      "u1",
		Title:       "title",
		Attrs:       map[string]any{"type": "bug", "labels": []string{"a", "b"}},
		IsUrgent:    urgent,
		Rationale:   "why",
		SubmittedAt: time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC),
		EnrichedAt:  time.Date(2026, 6, 7, 8, 9, 15, 0, time.UTC),
	}
}

func TestBuildOutboxEnvelope_Version2WithAttrs(t *testing.T) {
	payload, err := buildOutboxEnvelope(sampleSnapshot(true), "trace-abc")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["version"] != "2" {
		t.Errorf("envelope version: %v", got["version"])
	}
	if got["event_type"] != "feedback.enriched" {
		t.Errorf("event_type: %v", got["event_type"])
	}
	if got["trace_id"] != "trace-abc" {
		t.Errorf("trace_id: %v", got["trace_id"])
	}
	fb := got["feedback"].(map[string]any)
	enriched := fb["enriched"].(map[string]any)
	if enriched["title"] != "title" {
		t.Errorf("title")
	}
	if enriched["is_urgent"] != true {
		t.Errorf("is_urgent")
	}
	attrs := enriched["attrs"].(map[string]any)
	if attrs["type"] != "bug" {
		t.Errorf("attrs.type: %v", attrs["type"])
	}
	// SubmittedAt must reflect Snapshot.SubmittedAt (UTC RFC3339).
	if fb["submitted_at"] != "2026-06-07T08:09:10Z" {
		t.Errorf("submitted_at: %v", fb["submitted_at"])
	}
}

func TestBuildOutboxEnvelope_NilAttrsBecomesEmptyObject(t *testing.T) {
	s := sampleSnapshot(false)
	s.Attrs = nil
	payload, err := buildOutboxEnvelope(s, "")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(payload, &got)
	fb := got["feedback"].(map[string]any)
	enriched := fb["enriched"].(map[string]any)
	attrs, ok := enriched["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("attrs should be object, got %T", enriched["attrs"])
	}
	if len(attrs) != 0 {
		t.Errorf("nil Attrs should serialize as {}, got %v", attrs)
	}
	// trace_id absent (empty string + omitempty)
	if _, present := got["trace_id"]; present {
		t.Error("empty trace_id should be omitted")
	}
}

func TestBuildOutboxEnvelope_StableFieldOrder(t *testing.T) {
	// Customer verifiers may rely on canonical JSON. encoding/json preserves
	// struct field order; this snapshots the top-level key sequence.
	payload, _ := buildOutboxEnvelope(sampleSnapshot(true), "t")
	s := string(payload)
	// version, event_type, delivered_at, trace_id, feedback
	idx := func(needle string) int {
		for i := range s {
			if i+len(needle) > len(s) {
				return -1
			}
			if s[i:i+len(needle)] == needle {
				return i
			}
		}
		return -1
	}
	if idx(`"version"`) > idx(`"event_type"`) ||
		idx(`"event_type"`) > idx(`"delivered_at"`) ||
		idx(`"delivered_at"`) > idx(`"trace_id"`) ||
		idx(`"trace_id"`) > idx(`"feedback"`) {
		t.Errorf("top-level key order violated: %s", s)
	}
}

func TestSelectOutboxTargets_AudiencePoolAlwaysRoutes(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: notifytarget.AudiencePool, URL: "https://x"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(false))
	if len(got) != 1 {
		t.Errorf("pool target should route regardless of urgency, got %d", len(got))
	}
}

func TestSelectOutboxTargets_AudienceRadarOnlyWhenUrgent(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: notifytarget.AudienceRadar, URL: "https://x"},
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(false)); len(got) != 0 {
		t.Errorf("radar must skip non-urgent, got %d", len(got))
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(true)); len(got) != 1 {
		t.Errorf("radar must fire on urgent, got %d", len(got))
	}
}

func TestSelectOutboxTargets_AudienceAllRoutesBothPaths(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: notifytarget.AudienceAll, URL: "https://x"},
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(false)); len(got) != 1 {
		t.Errorf("audience=all should always route non-urgent: %d", len(got))
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(true)); len(got) != 1 {
		t.Errorf("audience=all should always route urgent: %d", len(got))
	}
}

func TestSelectOutboxTargets_DropsNonOutboxDests(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		// lark-bot uses the inline notifier path, not outbox.
		{TenantID: "t", DestinationType: "lark-bot", Audience: notifytarget.AudiencePool, URL: "https://x"},
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(true)); len(got) != 0 {
		t.Errorf("non-outbox dest should be filtered, got %d", len(got))
	}
}

func TestSelectOutboxTargets_GitHubIssueRoutes(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestGitHubIssue, Audience: notifytarget.AudiencePool, URL: "https://x"},
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(false)); len(got) != 1 {
		t.Errorf("github-issue must route through outbox: %d", len(got))
	}
}
