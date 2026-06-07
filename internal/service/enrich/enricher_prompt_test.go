package enrich

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
)

func sevDim() domain.Dimension {
	return domain.Dimension{
		Name:        "severity",
		DisplayName: domain.I18nString{"default": "Severity", "zh": "严重程度"},
		Kind:        domain.DimSingle,
		Taxonomy: []domain.Taxonomy{
			{Value: "critical", DisplayName: domain.I18nString{"default": "Critical", "zh": "严重", "en": "Critical"}},
			{Value: "minor", DisplayName: domain.I18nString{"default": "Minor", "zh": "次要"}},
		},
		UrgentSet: []string{"critical"},
	}
}

func freeformLabelsDim() domain.Dimension {
	return domain.Dimension{
		Name:        "labels",
		DisplayName: domain.I18nString{"default": "Labels"},
		Kind:        domain.DimMulti,
		Taxonomy:    nil,
	}
}

func TestRenderPrompt_SubstitutesBothTokens(t *testing.T) {
	cfg := ClassifyConfig{Dimensions: domain.DimensionSet{sevDim()}}
	out := renderPrompt(cfg, "test content goes here")
	if !strings.Contains(out, "test content goes here") {
		t.Errorf("expected content interpolated, got: %s", out)
	}
	if !strings.Contains(out, `"severity"`) {
		t.Errorf("expected dim name in {{dimensions}} block, got: %s", out)
	}
	if strings.Contains(out, "{{content}}") || strings.Contains(out, "{{dimensions}}") {
		t.Error("raw tokens leaked into output")
	}
}

func TestRenderPrompt_CustomTemplateRespected(t *testing.T) {
	tmpl := "custom: {{content}} | dims:\n{{dimensions}}"
	cfg := ClassifyConfig{
		PromptTemplate: &tmpl,
		Dimensions:     domain.DimensionSet{sevDim()},
	}
	out := renderPrompt(cfg, "hi")
	if !strings.HasPrefix(out, "custom: hi |") {
		t.Errorf("custom template not used, got: %s", out)
	}
}

func TestRenderPrompt_EmptyDimensionsRendersToBlankSlot(t *testing.T) {
	out := renderPrompt(ClassifyConfig{}, "x")
	if !strings.Contains(out, "x") {
		t.Error("content missing")
	}
	// {{dimensions}} expanded to "" — the empty marker should be gone
	if strings.Contains(out, "{{dimensions}}") {
		t.Error("dimensions token leaked when no dims configured")
	}
}

func TestRenderPrompt_SSTISafe(t *testing.T) {
	// User content containing {{content}} must not re-trigger substitution.
	cfg := ClassifyConfig{}
	evil := "before {{content}} after"
	out := renderPrompt(cfg, evil)
	// The literal evil string appears, but only ONCE — substitution is single-pass.
	if strings.Count(out, "before {{content}} after") != 1 {
		t.Errorf("substitution should be single-pass, got: %s", out)
	}
}

func TestRenderDimensionsClause_SingleWithI18n(t *testing.T) {
	out := renderDimensionsClause(domain.DimensionSet{sevDim()})
	if !strings.Contains(out, `"severity"`) {
		t.Errorf("dim name missing: %s", out)
	}
	if !strings.Contains(out, `(single)`) {
		t.Errorf("kind hint missing: %s", out)
	}
	// pick-one line should mention both Values and at least one display hint
	if !strings.Contains(out, `"critical"`) || !strings.Contains(out, "Critical") {
		t.Errorf("missing critical Value / English hint: %s", out)
	}
	if !strings.Contains(out, "严重") {
		t.Errorf("missing Chinese hint: %s", out)
	}
}

func TestRenderDimensionsClause_FreeformMulti(t *testing.T) {
	out := renderDimensionsClause(domain.DimensionSet{freeformLabelsDim()})
	if !strings.Contains(out, "freeform") {
		t.Errorf("freeform multi should advertise freedom, got: %s", out)
	}
	if !strings.Contains(out, `"labels"`) {
		t.Error("dim name missing")
	}
}

func TestRenderDimensionsClause_NoEnglishHintFallsBackCleanly(t *testing.T) {
	// Taxonomy entry whose display name equals its Value (no extra hint to add).
	d := domain.Dimension{
		Name:        "x",
		DisplayName: domain.I18nString{"default": "X"},
		Kind:        domain.DimSingle,
		Taxonomy: []domain.Taxonomy{
			{Value: "foo", DisplayName: domain.I18nString{"default": "foo"}},
		},
	}
	out := renderDimensionsClause(domain.DimensionSet{d})
	// Should at least include the Value in quoted form
	if !strings.Contains(out, `"foo"`) {
		t.Errorf("Value missing: %s", out)
	}
}

func TestBuildEnrichSchema_SingleAddsEnum(t *testing.T) {
	schema := buildEnrichSchema(domain.DimensionSet{sevDim()})
	if schema == nil {
		t.Fatal("nil schema")
	}
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	props := got["properties"].(map[string]any)
	sev := props["severity"].(map[string]any)
	if sev["type"] != "string" {
		t.Errorf("expected severity.type=string, got %v", sev["type"])
	}
	enum, ok := sev["enum"].([]any)
	if !ok || len(enum) != 2 {
		t.Errorf("expected 2-value enum, got %v", sev["enum"])
	}
}

func TestBuildEnrichSchema_MultiArrayItemsEnum(t *testing.T) {
	d := domain.Dimension{
		Name:        "labels",
		DisplayName: domain.I18nString{"default": "Labels"},
		Kind:        domain.DimMulti,
		Taxonomy: []domain.Taxonomy{
			{Value: "a", DisplayName: domain.I18nString{"default": "A"}},
			{Value: "b", DisplayName: domain.I18nString{"default": "B"}},
		},
	}
	schema := buildEnrichSchema(domain.DimensionSet{d})
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	props := got["properties"].(map[string]any)
	lbl := props["labels"].(map[string]any)
	if lbl["type"] != "array" {
		t.Errorf("expected array type, got %v", lbl["type"])
	}
	items := lbl["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("items.type should be string, got %v", items["type"])
	}
	if _, ok := items["enum"].([]any); !ok {
		t.Error("items.enum missing")
	}
}

func TestBuildEnrichSchema_FreeformOmitsEnum(t *testing.T) {
	schema := buildEnrichSchema(domain.DimensionSet{freeformLabelsDim()})
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	props := got["properties"].(map[string]any)
	lbl := props["labels"].(map[string]any)
	items := lbl["items"].(map[string]any)
	if _, ok := items["enum"]; ok {
		t.Error("freeform multi should NOT carry enum on items")
	}
}

func TestBuildEnrichSchema_RequiredDimAddedToRequired(t *testing.T) {
	d := sevDim()
	d.Required = true
	schema := buildEnrichSchema(domain.DimensionSet{d})
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	required, _ := got["required"].([]any)
	found := false
	for _, x := range required {
		if x == "severity" {
			found = true
		}
	}
	if !found {
		t.Errorf("required dim not in 'required' list: %v", required)
	}
}

func TestBuildEnrichSchema_AlwaysRequiresTitleAndRationale(t *testing.T) {
	schema := buildEnrichSchema(nil)
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	required := got["required"].([]any)
	wantSet := map[string]bool{"title": false, "rationale": false}
	for _, x := range required {
		if s, ok := x.(string); ok {
			wantSet[s] = true
		}
	}
	if !wantSet["title"] || !wantSet["rationale"] {
		t.Errorf("title/rationale must always be required, got: %v", required)
	}
}

func TestBuildEnrichSchema_AdditionalPropertiesFalse(t *testing.T) {
	schema := buildEnrichSchema(domain.DimensionSet{sevDim()})
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["additionalProperties"] != false {
		t.Errorf("additionalProperties must be false (strict mode), got %v", got["additionalProperties"])
	}
}

func TestDimsMode(t *testing.T) {
	freeform := ClassifyConfig{Dimensions: domain.DimensionSet{freeformLabelsDim()}}
	if dimsMode(freeform) != "freeform" {
		t.Errorf("expected freeform, got %s", dimsMode(freeform))
	}
	constrained := ClassifyConfig{Dimensions: domain.DimensionSet{sevDim()}}
	if dimsMode(constrained) != "constrained" {
		t.Errorf("expected constrained, got %s", dimsMode(constrained))
	}
}

func TestHasConstrained_MixedSet(t *testing.T) {
	cfg := ClassifyConfig{Dimensions: domain.DimensionSet{freeformLabelsDim(), sevDim()}}
	if !cfg.HasConstrained() {
		t.Error("at least one constrained dim should flip HasConstrained")
	}
}

func TestClassifyErrResult(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"", "ok"},
		{"llm: foo", "llm_err"},
		{"parse failed", "parse_err"},
		{"random other", "other_err"},
	}
	for _, c := range cases {
		got := classifyErrResult(errFromMsg(c.msg))
		if got != c.want {
			t.Errorf("classifyErrResult(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

type strErr string

func (s strErr) Error() string { return string(s) }

func errFromMsg(m string) error {
	if m == "" {
		return nil
	}
	return strErr(m)
}

func TestTruncate_HandlesUnicodeBoundary(t *testing.T) {
	in := "你好世界abc"
	got := truncate(in, 3)
	if len([]rune(got)) != 3 {
		t.Errorf("expected 3 runes, got %d (%q)", len([]rune(got)), got)
	}
}
