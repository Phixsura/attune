package enrich

import (
	"fmt"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// TestBuildSnapshot_CopiesRationale verifies the fix for the silent
// data-loss bug: buildSnapshot previously omitted the Rationale field,
// so outbox envelopes always had "rationale":"".
func TestBuildSnapshot_CopiesRationale(t *testing.T) {
	row := &feedback.EnrichInput{
		TenantID: "acme",
		Content:  "结账按钮没反应",
		Source:   "web",
		UserID:   "u-1",
	}
	enriched := domain.Enriched{
		Title:     "结账按钮失效",
		Kind:      "bug",
		Modules:   []string{"checkout"},
		Severity:  "P1",
		Rationale: "结账按钮点击无响应，阻塞支付流程",
		Priority:  0.9,
	}
	at := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

	snap := buildSnapshot(42, row, enriched, at)

	if snap.Rationale != enriched.Rationale {
		t.Errorf("Rationale: want %q, got %q", enriched.Rationale, snap.Rationale)
	}
	// Also verify all other fields are correctly copied.
	if snap.ID != 42 {
		t.Errorf("ID: want 42, got %d", snap.ID)
	}
	if snap.TenantID != "acme" {
		t.Errorf("TenantID: want acme, got %q", snap.TenantID)
	}
	if snap.Content != row.Content {
		t.Errorf("Content: want %q, got %q", row.Content, snap.Content)
	}
	if snap.Title != enriched.Title {
		t.Errorf("Title: want %q, got %q", enriched.Title, snap.Title)
	}
	if snap.Kind != enriched.Kind {
		t.Errorf("Kind: want %q, got %q", enriched.Kind, snap.Kind)
	}
	if !slicesEqualStr(snap.Modules, enriched.Modules) {
		t.Errorf("Modules: want %v, got %v", enriched.Modules, snap.Modules)
	}
	if snap.Severity != enriched.Severity {
		t.Errorf("Severity: want %q, got %q", enriched.Severity, snap.Severity)
	}
	if snap.Priority != enriched.Priority {
		t.Errorf("Priority: want %v, got %v", enriched.Priority, snap.Priority)
	}
	if !snap.EnrichedAt.Equal(at) {
		t.Errorf("EnrichedAt: want %v, got %v", at, snap.EnrichedAt)
	}
}

// TestTruncate_UTF8Safe verifies truncate doesn't split multi-byte
// characters. CJK characters are 3 bytes each in UTF-8.
func TestTruncate_UTF8Safe(t *testing.T) {
	// "购物车" = 9 bytes (3 chars × 3 bytes each).
	// Truncating at byte 7 would split the 3rd character.
	input := "购物车添加商品"
	got := truncate(input, 4)
	// With rune-aware truncation, we should get 4 runes = "购物车添".
	if got != "购物车添" {
		t.Errorf("truncate UTF-8: want %q, got %q", "购物车添", got)
	}
}

func TestTruncate_ShortString(t *testing.T) {
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("short string should pass through, got %q", got)
	}
}

func TestTruncate_Empty(t *testing.T) {
	if got := truncate("", 5); got != "" {
		t.Errorf("empty string: want empty, got %q", got)
	}
}

// TestParseEnrichJSON_ValidJSON exercises the happy path.
func TestParseEnrichJSON_ValidJSON(t *testing.T) {
	raw := `{"title":"结账崩溃","kind":"bug","modules":["cart","checkout"],"severity":"P1","rationale":"阻塞支付"}`
	e, err := parseEnrichJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Title != "结账崩溃" {
		t.Errorf("Title: want 结账崩溃, got %q", e.Title)
	}
	if e.Kind != "bug" {
		t.Errorf("Kind: want bug, got %q", e.Kind)
	}
	if e.Severity != "P1" {
		t.Errorf("Severity: want P1, got %q", e.Severity)
	}
	if e.Rationale != "阻塞支付" {
		t.Errorf("Rationale: want 阻塞支付, got %q", e.Rationale)
	}
}

// TestParseEnrichJSON_MarkdownWrapped verifies markdown fence stripping.
func TestParseEnrichJSON_MarkdownWrapped(t *testing.T) {
	raw := "```json\n{\"title\":\"test\",\"kind\":\"bug\",\"modules\":[],\"severity\":\"P2\",\"rationale\":\"ok\"}\n```"
	e, err := parseEnrichJSON(raw)
	if err != nil {
		t.Fatalf("markdown-wrapped JSON should parse: %v", err)
	}
	if e.Title != "test" {
		t.Errorf("Title: want test, got %q", e.Title)
	}
}

// TestParseEnrichJSON_MarkdownCaseInsensitive verifies that uppercase
// language tags like ```JSON or ```Javascript are also stripped.
func TestParseEnrichJSON_MarkdownCaseInsensitive(t *testing.T) {
	cases := []string{
		"```JSON\n{\"title\":\"a\",\"kind\":\"bug\",\"modules\":[],\"severity\":\"P1\",\"rationale\":\"x\"}\n```",
		"```Json\n{\"title\":\"b\",\"kind\":\"feature\",\"modules\":[],\"severity\":\"P2\",\"rationale\":\"y\"}\n```",
		"```\n{\"title\":\"c\",\"kind\":\"ops\",\"modules\":[],\"severity\":\"P3\",\"rationale\":\"z\"}\n```",
	}
	for i, raw := range cases {
		e, err := parseEnrichJSON(raw)
		if err != nil {
			t.Errorf("case %d: markdown-wrapped JSON should parse: %v", i, err)
			continue
		}
		if e.Title == "" {
			t.Errorf("case %d: Title should not be empty", i)
		}
	}
}

// TestParseEnrichJSON_MissingRequired verifies required field validation.
func TestParseEnrichJSON_MissingRequired(t *testing.T) {
	raw := `{"kind":"bug","severity":"P1"}`
	_, err := parseEnrichJSON(raw)
	if err == nil {
		t.Fatal("want error for missing title, got nil")
	}
}

// TestParseEnrichJSON_InvalidKind verifies kind validation.
func TestParseEnrichJSON_InvalidKind(t *testing.T) {
	raw := `{"title":"t","kind":"invalid","modules":[],"severity":"P1"}`
	_, err := parseEnrichJSON(raw)
	if err == nil {
		t.Fatal("want error for invalid kind, got nil")
	}
}

// TestParseEnrichJSON_InvalidSeverity verifies severity validation.
func TestParseEnrichJSON_InvalidSeverity(t *testing.T) {
	raw := `{"title":"t","kind":"bug","modules":[],"severity":"P99"}`
	_, err := parseEnrichJSON(raw)
	if err == nil {
		t.Fatal("want error for invalid severity, got nil")
	}
}

// TestParseEnrichJSON_NoJSON verifies the no-JSON-object error.
func TestParseEnrichJSON_NoJSON(t *testing.T) {
	_, err := parseEnrichJSON("just some prose with no JSON")
	if err == nil {
		t.Fatal("want error for no JSON, got nil")
	}
}

// TestClassifyErrResult_Buckets verifies error classification for metrics.
func TestClassifyErrResult_Buckets(t *testing.T) {
	if r := classifyErrResult(nil); r != "ok" {
		t.Errorf("nil err: want ok, got %q", r)
	}
	if r := classifyErrResult(fmt.Errorf("llm: timeout")); r != "llm_err" {
		t.Errorf("llm err: want llm_err, got %q", r)
	}
	if r := classifyErrResult(fmt.Errorf("parse: bad json")); r != "parse_err" {
		t.Errorf("parse err: want parse_err, got %q", r)
	}
	if r := classifyErrResult(fmt.Errorf("something else")); r != "other_err" {
		t.Errorf("other err: want other_err, got %q", r)
	}
}
