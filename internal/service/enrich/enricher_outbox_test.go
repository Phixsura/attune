package enrich

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
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
	payload, err := buildOutboxEnvelope(sampleSnapshot(true), "trace-abc", "API client")
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
	if fb["source"] != "api" {
		t.Errorf("source: %v", fb["source"])
	}
	if fb["source_display"] != "API client" {
		t.Errorf("source_display: %v; want %q", fb["source_display"], "API client")
	}
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
	payload, err := buildOutboxEnvelope(s, "", domain.SourceDisplayName(s.Source))
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

// TestBuildOutboxEnvelope_EmptySourceDisplayOmitted pins the omitempty contract:
// when no display is resolved (an old in-flight row, or an enricher built without
// a SourceSet), source_display must be ABSENT from the envelope so the
// github-issue renderers fall back to domain.SourceDisplayName. A regression from
// `omitempty` to a plain tag would emit "source_display":"" and defeat that
// fallback's `if sourceDisplay == ""` check.
func TestBuildOutboxEnvelope_EmptySourceDisplayOmitted(t *testing.T) {
	t.Parallel()
	payload, err := buildOutboxEnvelope(sampleSnapshot(false), "trace", "")
	if err != nil {
		t.Fatalf("buildOutboxEnvelope: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fb := got["feedback"].(map[string]any)
	if _, present := fb["source_display"]; present {
		t.Errorf("empty source_display should be omitted (omitempty), but key is present: %v", fb["source_display"])
	}
}

func TestBuildOutboxEnvelope_StableFieldOrder(t *testing.T) {
	// Customer verifiers may rely on canonical JSON. encoding/json preserves
	// struct field order; this snapshots the top-level key sequence.
	payload, _ := buildOutboxEnvelope(sampleSnapshot(true), "t", "API client")
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

func TestSemanticRun_RecordsPromptPolicyProvenance(t *testing.T) {
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	s.DisplayLocale = "zh-CN"
	tmpl := "custom {{content}} {{modules}}"
	cfg := ClassifyConfig{
		TenantID:        s.TenantID,
		DisplayLocale:   s.DisplayLocale,
		PromptTemplate:  ptrext.Of(tmpl),
		PromptVersionID: "11111111-2222-3333-4444-555555555555",
		Dimensions:      domain.DimensionSet{sevDim()},
	}
	run := ptrext.Of(Enricher{model: "fallback-model"}).semanticRun(s, cfg, classifyResult{
		Enriched: domain.Enriched{
			Title:                    "Login fails",
			Rationale:                "Unicode bug",
			DisplayTitle:             "登录失败",
			DisplayRationale:         "Unicode 问题",
			Attrs:                    map[string]any{"severity": "critical"},
			ClassificationConfidence: ptrext.Of(0.82),
		},
	})

	if !strings.HasPrefix(run.PromptVersion, "enrich.legacy_custom_template@sha256:") {
		t.Fatalf("prompt version should use canonical legacy identity, got %q", run.PromptVersion)
	}
	policy, ok := run.GuardSummary["prompt_policy"].(map[string]any)
	if !ok {
		t.Fatalf("prompt_policy guard missing: %#v", run.GuardSummary)
	}
	if policy["policy_id"] != "enrich.legacy_custom_template" {
		t.Fatalf("policy_id=%v", policy["policy_id"])
	}
	if policy["mode"] != "legacy_custom_override" {
		t.Fatalf("mode=%v", policy["mode"])
	}
	if got, ok := policy["prompt_fingerprint"].(string); !ok || !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("prompt fingerprint missing: %#v", policy)
	}
	if got, ok := policy["schema_fingerprint"].(string); !ok || !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("schema fingerprint missing: %#v", policy)
	}
	if run.PromptVersionID != cfg.PromptVersionID {
		t.Fatalf("prompt version id=%q", run.PromptVersionID)
	}
	if run.GuardSummary["prompt_version_id"] != cfg.PromptVersionID {
		t.Fatalf("guard prompt_version_id=%v", run.GuardSummary["prompt_version_id"])
	}
	warnings, ok := policy["warnings"].([]string)
	if !ok {
		t.Fatalf("warnings should be []string, got %T", policy["warnings"])
	}
	seenModules := false
	for _, code := range warnings {
		if code == "legacy_variable_modules" {
			seenModules = true
		}
	}
	if !seenModules {
		t.Fatalf("legacy modules warning missing: %#v", warnings)
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
		// Unknown dest types must be filtered — selectOutboxTargets
		// guards against schema drift where the migration table accepts
		// a value the worker doesn't know how to send.
		{TenantID: "t", DestinationType: "slack-bot", Audience: notifytarget.AudiencePool, URL: "https://x"},
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

func TestSelectOutboxTargets_SlackRoutes(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestSlack, Audience: notifytarget.AudiencePool, URL: "https://hooks.slack.com/services/x"},
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(false)); len(got) != 1 {
		t.Errorf("slack must route through outbox: %d", len(got))
	}
}

func TestSelectOutboxTargets_LarkRoutes(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestLark, Audience: notifytarget.AudiencePool, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/x"},
	}
	if got := selectOutboxTargets(targets, sampleSnapshot(false)); len(got) != 1 {
		t.Errorf("lark must route through outbox: %d", len(got))
	}
}

func TestSelectOutboxTargets_MultiChannelFanout(t *testing.T) {
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestRawWebhook, Audience: notifytarget.AudiencePool, URL: "https://a"},
		{TenantID: "t", DestinationType: notifytarget.DestSlack, Audience: notifytarget.AudiencePool, URL: "https://b"},
		{TenantID: "t", DestinationType: notifytarget.DestLark, Audience: notifytarget.AudiencePool, URL: "https://c"},
		{TenantID: "t", DestinationType: notifytarget.DestGitHubIssue, Audience: notifytarget.AudienceAll, URL: "https://d"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(false))
	if len(got) != 4 {
		t.Errorf("all four outbox dest types should route, got %d", len(got))
	}
}

func TestBuildOutboxEnvelope_EventTypeParameter(t *testing.T) {
	base, err := buildOutboxEnvelopeTyped(sampleSnapshot(true), "trace-abc", "API client", domain.EventFeedbackEnriched)
	if err != nil {
		t.Fatalf("build enriched: %v", err)
	}
	legacy, err := buildOutboxEnvelope(sampleSnapshot(true), "trace-abc", "API client")
	if err != nil {
		t.Fatalf("build legacy: %v", err)
	}
	if string(base) != string(legacy) {
		t.Fatalf("legacy path must be byte-identical:\n%s\n%s", legacy, base)
	}

	urgent, err := buildOutboxEnvelopeTyped(sampleSnapshot(true), "trace-abc", "API client", domain.EventFeedbackUrgent)
	if err != nil {
		t.Fatalf("build urgent: %v", err)
	}
	var a, b map[string]any
	if err := json.Unmarshal(base, &a); err != nil {
		t.Fatalf("decode base: %v", err)
	}
	if err := json.Unmarshal(urgent, &b); err != nil {
		t.Fatalf("decode urgent: %v", err)
	}
	if b["event_type"] != "feedback.urgent" {
		t.Fatalf("event_type: %v", b["event_type"])
	}
	// only event_type differs
	a["event_type"] = b["event_type"]
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatalf("envelopes must differ only in event_type:\n%s\n%s", aj, bj)
	}
}
