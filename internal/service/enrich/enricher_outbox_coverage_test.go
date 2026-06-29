// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package enrich

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// ---------------------------------------------------------------------------
// confidenceAudit
// ---------------------------------------------------------------------------

func TestCoverageConfidenceAudit_NilConfidence(t *testing.T) {
	t.Parallel()
	enriched := domain.Enriched{
		Title:                    "test",
		ClassificationConfidence: nil,
	}
	got := confidenceAudit(enriched)
	if len(got) != 0 {
		t.Errorf("nil confidence should produce empty map, got %v", got)
	}
}

func TestCoverageConfidenceAudit_NonNilConfidence(t *testing.T) {
	t.Parallel()
	enriched := domain.Enriched{
		Title:                    "test",
		ClassificationConfidence: ptrext.Of(0.85),
	}
	got := confidenceAudit(enriched)
	overall, ok := got["overall"]
	if !ok {
		t.Fatal("expected 'overall' key in confidence audit")
	}
	if overall != 0.85 {
		t.Errorf("overall = %v; want 0.85", overall)
	}
	source, ok := got["source"]
	if !ok {
		t.Fatal("expected 'source' key in confidence audit")
	}
	if source != "llm_self_report" {
		t.Errorf("source = %v; want llm_self_report", source)
	}
}

func TestCoverageConfidenceAudit_ZeroValuePointer(t *testing.T) {
	t.Parallel()
	enriched := domain.Enriched{
		Title:                    "test",
		ClassificationConfidence: ptrext.Of(0.0),
	}
	got := confidenceAudit(enriched)
	overall, ok := got["overall"]
	if !ok {
		t.Fatal("non-nil zero confidence must still produce 'overall' key")
	}
	if overall != 0.0 {
		t.Errorf("overall = %v; want 0.0", overall)
	}
	if got["source"] != "llm_self_report" {
		t.Errorf("source = %v; want llm_self_report", got["source"])
	}
}

// ---------------------------------------------------------------------------
// semanticRouteGuard
// ---------------------------------------------------------------------------

func TestCoverageSemanticRouteGuard_Empty(t *testing.T) {
	t.Parallel()
	got := semanticRouteGuard(llmclient.RouteMetadata{})
	if len(got) != 0 {
		t.Errorf("empty route should produce empty map, got %v", got)
	}
}

func TestCoverageSemanticRouteGuard_OnlyLogicalModel(t *testing.T) {
	t.Parallel()
	got := semanticRouteGuard(llmclient.RouteMetadata{
		LogicalModel: "gpt-4o",
	})
	if len(got) != 1 {
		t.Errorf("expected 1 key, got %d: %v", len(got), got)
	}
	if got["logical_model"] != "gpt-4o" {
		t.Errorf("logical_model = %v", got["logical_model"])
	}
}

func TestCoverageSemanticRouteGuard_AllFields(t *testing.T) {
	t.Parallel()
	got := semanticRouteGuard(llmclient.RouteMetadata{
		LogicalModel:  "gpt-4o",
		ProviderModel: "gpt-4o-2024-05-13",
		ChannelID:     "ch-1",
		ChannelName:   "primary",
		Protocol:      "openai",
	})
	if len(got) != 5 {
		t.Errorf("expected 5 keys, got %d: %v", len(got), got)
	}
	checks := map[string]string{
		"logical_model":  "gpt-4o",
		"provider_model": "gpt-4o-2024-05-13",
		"channel_id":     "ch-1",
		"channel_name":   "primary",
		"protocol":       "openai",
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("%s = %v; want %v", k, got[k], want)
		}
	}
}

func TestCoverageSemanticRouteGuard_MixedFields(t *testing.T) {
	t.Parallel()
	got := semanticRouteGuard(llmclient.RouteMetadata{
		ProviderModel: "claude-sonnet-4-20250514",
		Protocol:      "anthropic",
	})
	if len(got) != 2 {
		t.Errorf("expected 2 keys, got %d: %v", len(got), got)
	}
	if got["provider_model"] != "claude-sonnet-4-20250514" {
		t.Errorf("provider_model = %v", got["provider_model"])
	}
	if got["protocol"] != "anthropic" {
		t.Errorf("protocol = %v", got["protocol"])
	}
	if _, present := got["logical_model"]; present {
		t.Error("empty logical_model should be absent")
	}
}

// ---------------------------------------------------------------------------
// selectOutboxTargets — additional edge cases
// ---------------------------------------------------------------------------

func TestCoverageSelectOutboxTargets_EmptyTargets(t *testing.T) {
	t.Parallel()
	got := selectOutboxTargets(nil, sampleSnapshot(false))
	if len(got) != 0 {
		t.Errorf("nil targets should return empty slice, got %d", len(got))
	}
	got = selectOutboxTargets([]notifytarget.NotifyTarget{}, sampleSnapshot(true))
	if len(got) != 0 {
		t.Errorf("empty targets should return empty slice, got %d", len(got))
	}
}

func TestCoverageSelectOutboxTargets_DiscordRoutes(t *testing.T) {
	t.Parallel()
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestDiscord, Audience: notifytarget.AudiencePool, URL: "https://discord.com/api/webhooks/x"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(false))
	if len(got) != 1 {
		t.Errorf("discord must route through outbox: %d", len(got))
	}
}

func TestCoverageSelectOutboxTargets_UnknownAudience(t *testing.T) {
	t.Parallel()
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: "unknown-audience", URL: "https://x"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(true))
	if len(got) != 0 {
		t.Errorf("unknown audience should not route, got %d", len(got))
	}
}

func TestCoverageSelectOutboxTargets_MixedAudiencesAndDests(t *testing.T) {
	t.Parallel()
	targets := []notifytarget.NotifyTarget{
		// Outbox dest, pool audience -> routes
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: notifytarget.AudiencePool, URL: "https://a"},
		// Outbox dest, radar audience, non-urgent -> skipped
		{TenantID: "t", DestinationType: notifytarget.DestSlack, Audience: notifytarget.AudienceRadar, URL: "https://b"},
		// Non-outbox dest, pool audience -> skipped
		{TenantID: "t", DestinationType: "email", Audience: notifytarget.AudiencePool, URL: "https://c"},
		// Outbox dest, all audience -> routes
		{TenantID: "t", DestinationType: notifytarget.DestDiscord, Audience: notifytarget.AudienceAll, URL: "https://d"},
		// Unknown dest, unknown audience -> skipped
		{TenantID: "t", DestinationType: "carrier-pigeon", Audience: "vip", URL: "https://e"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(false))
	if len(got) != 2 {
		t.Errorf("expected 2 routed targets (pool webhook + all discord), got %d", len(got))
	}
}

func TestCoverageSelectOutboxTargets_DigestAudienceSkipped(t *testing.T) {
	t.Parallel()
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: notifytarget.AudienceDigest, URL: "https://x"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(true))
	if len(got) != 0 {
		t.Errorf("digest audience should not route via selectOutboxTargets, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// buildOutboxEnvelope — additional edge cases
// ---------------------------------------------------------------------------

func TestCoverageBuildOutboxEnvelope_DisplayTitleAndLocale(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.DisplayTitle = "display-title"
	s.DisplayLocale = "zh-CN"
	payload, err := buildOutboxEnvelope(s, "trace-1", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fb := got["feedback"].(map[string]any)
	enriched := fb["enriched"].(map[string]any)
	if enriched["display_title"] != "display-title" {
		t.Errorf("display_title = %v; want display-title", enriched["display_title"])
	}
	if enriched["display_locale"] != "zh-CN" {
		t.Errorf("display_locale = %v; want zh-CN", enriched["display_locale"])
	}
}

func TestCoverageBuildOutboxEnvelope_EmptyContent(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Content = ""
	payload, err := buildOutboxEnvelope(s, "t", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fb := got["feedback"].(map[string]any)
	if fb["content"] != "" {
		t.Errorf("content should be empty, got %v", fb["content"])
	}
}

func TestCoverageBuildOutboxEnvelope_LanguageSet(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = "ja"
	payload, err := buildOutboxEnvelope(s, "t", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fb := got["feedback"].(map[string]any)
	if fb["language"] != "ja" {
		t.Errorf("language = %v; want ja", fb["language"])
	}
}

func TestCoverageBuildOutboxEnvelope_DisplayRationale(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.DisplayRationale = "localized reason"
	payload, err := buildOutboxEnvelope(s, "t", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fb := got["feedback"].(map[string]any)
	enriched := fb["enriched"].(map[string]any)
	if enriched["display_rationale"] != "localized reason" {
		t.Errorf("display_rationale = %v; want 'localized reason'", enriched["display_rationale"])
	}
}

func TestCoverageBuildOutboxEnvelope_TimestampFormat(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	// Use a non-UTC time to verify UTC normalization.
	loc := time.FixedZone("UTC+8", 8*60*60)
	s.EnrichedAt = time.Date(2026, 1, 2, 11, 0, 0, 0, loc)  // 03:00 UTC
	s.SubmittedAt = time.Date(2026, 1, 2, 10, 0, 0, 0, loc) // 02:00 UTC
	payload, err := buildOutboxEnvelope(s, "t", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["delivered_at"] != "2026-01-02T03:00:00Z" {
		t.Errorf("delivered_at = %v; want 2026-01-02T03:00:00Z", got["delivered_at"])
	}
	fb := got["feedback"].(map[string]any)
	if fb["submitted_at"] != "2026-01-02T02:00:00Z" {
		t.Errorf("submitted_at = %v; want 2026-01-02T02:00:00Z", fb["submitted_at"])
	}
}

// ---------------------------------------------------------------------------
// outboxDestTypes map verification
// ---------------------------------------------------------------------------

func TestCoverageOutboxDestTypes_KnownTypes(t *testing.T) {
	t.Parallel()
	known := []string{
		notifytarget.DestRawWebhook,
		notifytarget.DestGitHubIssue,
		notifytarget.DestSlack,
		notifytarget.DestLark,
		notifytarget.DestDiscord,
		notifytarget.DestTeams,
		notifytarget.DestJira,
		notifytarget.DestLinear,
	}
	for _, dt := range known {
		if !outboxDestTypes[dt] {
			t.Errorf("dest type %q should be in outboxDestTypes", dt)
		}
	}
	if len(outboxDestTypes) != 8 {
		t.Errorf("outboxDestTypes has %d entries; want 8", len(outboxDestTypes))
	}
}

func TestCoverageOutboxDestTypes_UnknownReturnsFalse(t *testing.T) {
	t.Parallel()
	unknowns := []string{"email", "slack-bot", "carrier-pigeon", ""}
	for _, dt := range unknowns {
		if outboxDestTypes[dt] {
			t.Errorf("dest type %q should not be in outboxDestTypes", dt)
		}
	}
}

// ---------------------------------------------------------------------------
// extractTraceID
// ---------------------------------------------------------------------------

func TestCoverageExtractTraceID_BackgroundContext(t *testing.T) {
	t.Parallel()
	// A bare background context has no trace; extractTraceID should
	// generate a fresh one via trace.New().
	id := extractTraceID(context.Background())
	if id == "" {
		t.Error("extractTraceID should return non-empty string on background ctx")
	}
	if len(id) != 32 {
		t.Errorf("generated trace id should be 32 hex chars, got len=%d: %q", len(id), id)
	}
}

func TestCoverageExtractTraceID_WithExplicitTrace(t *testing.T) {
	t.Parallel()
	ctx := trace.WithID(context.Background(), "explicit-trace-id")
	id := extractTraceID(ctx)
	if id != "explicit-trace-id" {
		t.Errorf("extractTraceID = %q; want explicit-trace-id", id)
	}
}

func TestCoverageExtractTraceID_Uniqueness(t *testing.T) {
	t.Parallel()
	// Two calls on background ctx should produce different IDs.
	id1 := extractTraceID(context.Background())
	id2 := extractTraceID(context.Background())
	if id1 == id2 {
		t.Errorf("consecutive extractTraceID calls should produce unique IDs, both = %q", id1)
	}
}
