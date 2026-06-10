package enrich

import (
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

func TestParseEnrichJSON_HappyPath(t *testing.T) {
	in := `{"title":"Payment fails","display_title":"支付失败","type":"bug","severity":"critical","labels":["pay","ux"],"rationale":"core flow","display_rationale":"核心流程受阻"}`
	e, err := parseEnrichJSON(in)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if e.Title != "Payment fails" {
		t.Errorf("title: %q", e.Title)
	}
	if e.DisplayTitle != "支付失败" {
		t.Errorf("display title: %q", e.DisplayTitle)
	}
	if e.Rationale != "core flow" {
		t.Errorf("rationale: %q", e.Rationale)
	}
	if e.DisplayRationale != "核心流程受阻" {
		t.Errorf("display rationale: %q", e.DisplayRationale)
	}
	if e.Attrs["type"] != "bug" {
		t.Errorf("type attr: %v", e.Attrs["type"])
	}
	if e.Attrs["severity"] != "critical" {
		t.Errorf("severity attr: %v", e.Attrs["severity"])
	}
	labels, _ := e.Attrs["labels"].([]any)
	if len(labels) != 2 {
		t.Errorf("labels attr: %v", e.Attrs["labels"])
	}
}

func TestParseEnrichJSON_MarkdownFenceStripped(t *testing.T) {
	in := "```json\n{\"title\":\"x\",\"rationale\":\"y\"}\n```"
	e, err := parseEnrichJSON(in)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if e.Title != "x" {
		t.Errorf("expected stripped fence, got title=%q", e.Title)
	}
}

func TestParseEnrichJSON_PlainFenceStripped(t *testing.T) {
	in := "```\n{\"title\":\"x\",\"rationale\":\"y\"}\n```"
	if _, err := parseEnrichJSON(in); err != nil {
		t.Errorf("plain fence should be stripped: %v", err)
	}
}

func TestParseEnrichJSON_PreambleProseStripped(t *testing.T) {
	in := `Sure, here's the classification: {"title":"x","rationale":"y","type":"bug"}`
	e, err := parseEnrichJSON(in)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if e.Title != "x" || e.Attrs["type"] != "bug" {
		t.Errorf("preamble strip failed: %+v", e)
	}
}

func TestParseEnrichJSON_NoJSONObjectErrors(t *testing.T) {
	if _, err := parseEnrichJSON("no json here"); err == nil {
		t.Error("expected error on non-JSON input")
	}
}

func TestParseEnrichJSON_MissingTitleErrors(t *testing.T) {
	if _, err := parseEnrichJSON(`{"rationale":"y","type":"bug"}`); err == nil {
		t.Error("missing title should error")
	}
}

func TestParseEnrichJSON_EmptyTitleErrors(t *testing.T) {
	if _, err := parseEnrichJSON(`{"title":"","rationale":"y"}`); err == nil {
		t.Error("empty title should error")
	}
}

func TestParseEnrichJSON_MalformedJSONErrors(t *testing.T) {
	if _, err := parseEnrichJSON(`{"title":}`); err == nil {
		t.Error("malformed JSON should error")
	}
}

func TestParseEnrichJSON_OptionalRationale(t *testing.T) {
	// rationale missing — allowed; defaults to "".
	e, err := parseEnrichJSON(`{"title":"x","type":"bug"}`)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if e.Rationale != "" {
		t.Errorf("expected empty rationale, got %q", e.Rationale)
	}
}

func TestParseEnrichJSON_AttrsExcludesReservedKeys(t *testing.T) {
	// title + rationale should NOT leak into Attrs.
	e, err := parseEnrichJSON(`{"title":"x","display_title":"dx","rationale":"y","display_rationale":"dy","type":"bug"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Attrs["title"]; ok {
		t.Error("title leaked into Attrs")
	}
	if _, ok := e.Attrs["rationale"]; ok {
		t.Error("rationale leaked into Attrs")
	}
	if _, ok := e.Attrs["display_title"]; ok {
		t.Error("display_title leaked into Attrs")
	}
	if _, ok := e.Attrs["display_rationale"]; ok {
		t.Error("display_rationale leaked into Attrs")
	}
}

func TestParseEnrichJSON_DisplayFieldsRemainEmptyWhenMissing(t *testing.T) {
	e, err := parseEnrichJSON(`{"title":"x","rationale":"y","type":"bug"}`)
	if err != nil {
		t.Fatal(err)
	}
	if e.DisplayTitle != "" {
		t.Errorf("display title should stay empty when omitted: %q", e.DisplayTitle)
	}
	if e.DisplayRationale != "" {
		t.Errorf("display rationale should stay empty when omitted: %q", e.DisplayRationale)
	}
}

func TestNormalizeEnrichedDisplay_FillsOnlySameLanguageRows(t *testing.T) {
	same := domain.Enriched{Title: "支付失败", Rationale: "支付流程受阻"}
	same = normalizeEnrichedDisplay(same, LanguageChinese, "zh-CN")
	if same.DisplayTitle != same.Title || same.DisplayRationale != same.Rationale {
		t.Fatalf("same-language display should fall back to native: %+v", same)
	}
	cross := domain.Enriched{Title: "Payment failed", Rationale: "Checkout blocked"}
	cross = normalizeEnrichedDisplay(cross, LanguageEnglish, "zh-CN")
	if cross.DisplayTitle != "" || cross.DisplayRationale != "" {
		t.Fatalf("cross-language display should not be invented: %+v", cross)
	}
}

func TestParseEnrichJSON_ExtraDimsPassThrough(t *testing.T) {
	// Unknown dim keys pass through; gate (2) FilterAttrs drops them later.
	e, err := parseEnrichJSON(`{"title":"x","rationale":"y","custom_dim":"value","another":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if e.Attrs["custom_dim"] != "value" {
		t.Errorf("custom dim missing")
	}
	if e.Attrs["another"] != "x" {
		t.Errorf("another missing")
	}
}

func TestBuildSnapshot_CopiesAttrs(t *testing.T) {
	row := ptrext.Of(feedback.EnrichInput{
		Content:       "c",
		Source:        "s",
		UserID:        "u",
		Language:      "en",
		DisplayLocale: "zh",
		TenantID:      "t",
		CreatedAt:     time.Now(),
	})
	enriched := domain.Enriched{
		Title:            "t",
		DisplayTitle:     "dt",
		Attrs:            map[string]any{"type": "bug"},
		IsUrgent:         true,
		Rationale:        "r",
		DisplayRationale: "dr",
	}
	snap := buildSnapshot(42, row, enriched, time.Now())
	if snap.ID != 42 {
		t.Error("id mismatch")
	}
	if snap.Attrs["type"] != "bug" {
		t.Error("attrs not copied")
	}
	if snap.Language != "en" {
		t.Errorf("language: %q", snap.Language)
	}
	if snap.DisplayLocale != "zh" {
		t.Errorf("display locale: %q", snap.DisplayLocale)
	}
	if snap.DisplayTitle != "dt" {
		t.Errorf("display title: %q", snap.DisplayTitle)
	}
	if !snap.IsUrgent {
		t.Error("urgent not copied")
	}
	if snap.SubmittedAt.IsZero() {
		t.Error("submitted_at should mirror row.CreatedAt")
	}
}

func TestParseEnrichJSON_LeadingTrailingWhitespace(t *testing.T) {
	in := "\n\n  {\"title\":\"x\",\"rationale\":\"y\"}  \n"
	if _, err := parseEnrichJSON(in); err != nil {
		t.Errorf("whitespace should be trimmed: %v", err)
	}
}

func TestParseEnrichJSON_FenceWithLanguageTag(t *testing.T) {
	in := "```JSON\n{\"title\":\"x\",\"rationale\":\"y\"}\n```"
	if _, err := parseEnrichJSON(in); err != nil {
		t.Errorf("uppercase fence tag should be stripped: %v", err)
	}
}

func TestParseEnrichJSON_HandlesNullAttrValue(t *testing.T) {
	// LLM emits null for an unconfigured dim — should not crash; passes
	// through as nil and FilterAttrs drops it.
	e, err := parseEnrichJSON(`{"title":"x","rationale":"y","severity":null}`)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := e.Attrs["severity"]; !ok || v != nil {
		t.Errorf("severity should be nil pass-through, got %v ok=%v", v, ok)
	}
}

// Mini integration: parse + gate-2 filter for a realistic LLM payload.
func TestParseAndFilter_DropsUnknownDim(t *testing.T) {
	raw := `{"title":"x","rationale":"y","type":"bug","invented":"foo","severity":"bogus"}`
	parsed, err := parseEnrichJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	dims := domain.DimensionSet{
		{
			Name: "type", DisplayName: domain.I18nString{"default": "Type"}, Kind: domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{Value: "bug", DisplayName: domain.I18nString{"default": "Bug"}}},
		},
		{
			Name: "severity", DisplayName: domain.I18nString{"default": "Severity"}, Kind: domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{Value: "critical", DisplayName: domain.I18nString{"default": "C"}}},
		},
	}
	kept, dropped, suggested := domain.FilterAttrs(parsed.Attrs, dims)
	if kept["type"] != "bug" {
		t.Errorf("type should pass: %v", kept["type"])
	}
	if _, ok := kept["severity"]; ok {
		t.Errorf("off-list severity should be dropped, kept=%v", kept["severity"])
	}
	if _, ok := kept["invented"]; ok {
		t.Errorf("unknown dim should be dropped, got %v", kept["invented"])
	}
	if dropped["severity"] != 1 {
		t.Errorf("dropped should count severity, got %v", dropped)
	}
	if len(suggested) != 1 || suggested[0] != "severity" {
		t.Errorf("suggested mismatch: %v", suggested)
	}
	if !strings.HasPrefix(parsed.Title, "x") {
		t.Errorf("title preserved")
	}
}
