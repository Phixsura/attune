package enrich

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
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
	if !strings.Contains(out, `"classification_confidence"`) ||
		!strings.Contains(out, "0.5 means ambiguous enough for human review") {
		t.Fatalf("confidence instruction missing from prompt:\n%s", out)
	}
}

func TestRenderPrompt_CustomTemplateRespected(t *testing.T) {
	cfg := ClassifyConfig{
		Language:       LanguageChinese,
		PromptTemplate: ptrext.Of("custom: {{content}} | dims:\n{{dimensions}}"),
		Dimensions:     domain.DimensionSet{sevDim()},
	}
	out := renderPrompt(cfg, "hi")
	if !strings.HasPrefix(out, "custom: hi |") {
		t.Errorf("custom template not used, got: %s", out)
	}
}

func TestRenderPrompt_SubstitutesWhitespaceWrappedTokens(t *testing.T) {
	cfg := ClassifyConfig{
		PromptTemplate: ptrext.Of("custom: {{ content }} | dims:\n{{ dimensions }}"),
		Dimensions:     domain.DimensionSet{sevDim()},
	}
	out := renderPrompt(cfg, "hi")
	if !strings.Contains(out, "custom: hi") {
		t.Fatalf("content token not substituted:\n%s", out)
	}
	if !strings.Contains(out, `"severity"`) {
		t.Fatalf("dimensions token not substituted:\n%s", out)
	}
	if strings.Contains(out, "{{ content }}") || strings.Contains(out, "{{ dimensions }}") {
		t.Fatalf("raw whitespace-wrapped token leaked:\n%s", out)
	}
}

func TestRenderPrompt_UsesLanguageVariant(t *testing.T) {
	zh := renderPrompt(ClassifyConfig{Language: LanguageEnglish, DisplayLocale: LanguageChinese}, "checkout failed")
	if !strings.Contains(zh, "你是产品反馈分类器") {
		t.Fatalf("Chinese language should select zh prompt, got:\n%s", zh)
	}
	if !strings.Contains(zh, `"display_title"`) || !strings.Contains(zh, "Simplified Chinese") {
		t.Fatalf("Chinese display prompt should request display fields, got:\n%s", zh)
	}
	traditional := renderPrompt(ClassifyConfig{DisplayLocale: "zh-TW"}, "checkout failed")
	if !strings.Contains(traditional, "Traditional Chinese") {
		t.Fatalf("zh-TW display prompt should request Traditional Chinese, got:\n%s", traditional)
	}
	en := renderPrompt(ClassifyConfig{Language: LanguageJapanese}, "ログインできません")
	if !strings.Contains(en, "You are a product-feedback classifier") {
		t.Fatalf("Japanese currently falls back to English prompt, got:\n%s", en)
	}
	if !strings.Contains(en, "same natural language") {
		t.Fatalf("English prompt should preserve source language, got:\n%s", en)
	}
}

func TestDefaultPromptTemplateForLocale(t *testing.T) {
	require.Contains(t, DefaultPromptTemplate(), "You are a product-feedback classifier")
	require.Contains(t, DefaultPromptTemplateForLocale("zh-CN"), "你是产品反馈分类器")
	require.Contains(t, DefaultPromptTemplateForLocale("zh-Hant"), "你是产品反馈分类器")
	require.Contains(t, DefaultPromptTemplateForLocale("ja-JP"), "You are a product-feedback classifier")
}

func TestRenderPolicyGuidance(t *testing.T) {
	displayOnly := renderOutputLanguagePolicy(domain.EnrichPromptPolicyConfig{
		OutputLanguagePolicy: domain.PromptOutputDisplayOnly,
	}, "Japanese")
	require.Contains(t, displayOnly, `Write "title" and "rationale" in Japanese`)

	sourceAndDisplay := renderOutputLanguagePolicy(domain.EnrichPromptPolicyConfig{}, "Simplified Chinese")
	require.Contains(t, sourceAndDisplay, "same natural language as the raw feedback")
	require.Contains(t, sourceAndDisplay, "Simplified Chinese")

	require.Equal(t, "", renderDomainGuidance("   "))
	require.Equal(t, "Additional operator guidance: Treat billing as high signal.", renderDomainGuidance("  Treat billing as high signal.  "))
}

func TestPromptVersion(t *testing.T) {
	if got := promptVersion(ClassifyConfig{Language: LanguageJapanese}); got != "enrich.default@1" {
		t.Fatalf("Japanese source should report default policy, got %q", got)
	}
	if got := promptVersion(ClassifyConfig{
		Language:      LanguageEnglish,
		DisplayLocale: LanguageChinese,
	}); got != "enrich.default@1" {
		t.Fatalf("Chinese display locale should report default policy, got %q", got)
	}
	if got := promptVersion(ClassifyConfig{
		Language:       LanguageChinese,
		PromptTemplate: ptrext.Of("custom {{content}}"),
	}); !strings.HasPrefix(got, "enrich.legacy_custom_template@sha256:") {
		t.Fatalf("custom prompt version=%q", got)
	}
}

func TestResolvePromptPolicy_CustomWarningsAndIdentity(t *testing.T) {
	policy := resolvePromptPolicy(ClassifyConfig{
		PromptTemplate: ptrext.Of("custom {{content}} {{modules}} {{unknown}}"),
		Dimensions:     domain.DimensionSet{sevDim()},
	})
	if policy.PolicyID != "enrich.legacy_custom_template" {
		t.Fatalf("policy id=%q", policy.PolicyID)
	}
	if policy.Mode != "legacy_custom_override" {
		t.Fatalf("mode=%q", policy.Mode)
	}
	if !strings.HasPrefix(policy.PolicyVersion, "sha256:") {
		t.Fatalf("policy version=%q", policy.PolicyVersion)
	}
	codes := map[string]bool{}
	for _, w := range policy.Warnings {
		codes[w.Code] = true
	}
	for _, want := range []string{"legacy_variable_modules", "unknown_variable", "missing_dimensions"} {
		if !codes[want] {
			t.Fatalf("warning %q missing from %#v", want, policy.Warnings)
		}
	}
}

func TestResolvePromptPolicy_DefaultMetadataAndGuard(t *testing.T) {
	policy := resolvePromptPolicy(ClassifyConfig{
		DisplayLocale: "ja-JP",
		PolicyConfig: domain.EnrichPromptPolicyConfig{
			Tone:           "detailed",
			DomainGuidance: "Treat checkout failures as high signal.",
		},
		Dimensions: domain.DimensionSet{sevDim()},
	})
	require.Equal(t, "enrich.default", policy.PolicyID)
	require.Equal(t, "1", policy.PolicyVersion)
	require.Equal(t, "enrich.default@1", policy.PromptVersion)
	require.Equal(t, "default", policy.Mode)
	require.Equal(t, "built_in", policy.PromptSource)
	require.Equal(t, "en", policy.TemplateLanguage)
	require.Equal(t, "ja-JP", policy.DisplayLocale)
	require.Equal(t, "Japanese", policy.DisplayLanguageName)
	require.True(t, strings.HasPrefix(policy.PromptFingerprint, "sha256:"), policy.PromptPolicyMetadata)
	require.True(t, strings.HasPrefix(policy.SchemaFingerprint, "sha256:"), policy.PromptPolicyMetadata)
	require.GreaterOrEqual(t, len(policy.Variables), 5)
	require.GreaterOrEqual(t, len(policy.Outputs), 5)

	guard := promptPolicyGuard(policy)
	require.Contains(t, guard, "policy_id")
	require.Contains(t, guard, "policy_version")
	require.Contains(t, guard, "prompt_contract")
	require.Contains(t, guard, "output_contract")
	require.Contains(t, guard, "policy_config")
	require.Contains(t, guard, "schema_fingerprint")
	require.Contains(t, guard, "prompt_fingerprint")
	require.Nil(t, warningCodes(nil))
}

func TestPromptTemplateWarnings_MissingOutputMentions(t *testing.T) {
	warnings := promptTemplateWarnings("custom {{content}} {{dimensions}} title rationale")
	codes := map[string]int{}
	for _, w := range warnings {
		codes[w.Code]++
		if w.Code == "missing_output_mention" && w.Severity != "info" {
			t.Fatalf("missing output warning should be info: %#v", w)
		}
	}
	if codes["missing_dimensions"] != 0 || codes["unknown_variable"] != 0 {
		t.Fatalf("unexpected warning codes: %#v", codes)
	}
	if codes["missing_output_mention"] == 0 {
		t.Fatalf("expected missing output mention warnings: %#v", warnings)
	}
	missing := missingOutputMentions("title rationale display_title display_rationale classification_confidence")
	if len(missing) != 0 {
		t.Fatalf("all required mentions present, got %#v", missing)
	}
}

func TestPromptVariableHelpers(t *testing.T) {
	if got := promptVariableName("{{ display_locale }}"); got != "display_locale" {
		t.Fatalf("variable name=%q", got)
	}
	if got := promptVariableName("not a token"); got != "" {
		t.Fatalf("invalid token should not parse, got %q", got)
	}
	vars := promptVariables("{{content}} {{ content }} {{dimensions}}")
	if !vars["content"] || !vars["dimensions"] || len(vars) != 2 {
		t.Fatalf("variables not normalized/deduplicated: %#v", vars)
	}
	unknown := unknownPromptVariables("{{zzz}} {{aaa}} {{content}} {{zzz}}")
	if strings.Join(unknown, ",") != "aaa,zzz" {
		t.Fatalf("unknown variables should be sorted/deduped, got %#v", unknown)
	}
	if canonicalTemplate("{{ content }} {{dimensions }}") != "{{content}} {{dimensions}}" {
		t.Fatalf("canonical template mismatch")
	}
}

func TestResolvePromptPolicy_DimensionsTokenAllowsWhitespace(t *testing.T) {
	policy := resolvePromptPolicy(ClassifyConfig{
		PromptTemplate: ptrext.Of("custom {{ content }} {{ dimensions }}"),
	})
	for _, w := range policy.Warnings {
		if w.Code == "missing_dimensions" {
			t.Fatalf("{{ dimensions }} should satisfy dimensions warning check: %#v", policy.Warnings)
		}
	}
}

func TestResolvePromptPolicy_FingerprintDoesNotIncludeContent(t *testing.T) {
	cfg := ClassifyConfig{PromptTemplate: ptrext.Of("custom {{content}} {{dimensions}}")}
	a := resolvePromptPolicy(cfg)
	b := resolvePromptPolicy(cfg)
	if a.promptFingerprint != b.promptFingerprint {
		t.Fatalf("same prompt shape should have stable fingerprint: %q vs %q", a.promptFingerprint, b.promptFingerprint)
	}
	if strings.Contains(a.promptFingerprint, "custom") || strings.Contains(a.promptFingerprint, "content") {
		t.Fatalf("fingerprint should be a hash only, got %q", a.promptFingerprint)
	}
}

func TestRenderPrompt_EmptyDimensionsExplainsBuiltInsOnly(t *testing.T) {
	out := renderPrompt(ClassifyConfig{}, "x")
	if !strings.Contains(out, "x") {
		t.Error("content missing")
	}
	if strings.Contains(out, "{{dimensions}}") {
		t.Error("dimensions token leaked when no dims configured")
	}
	if !strings.Contains(out, "No custom Dimensions configured") {
		t.Fatalf("empty dimension guidance missing:\n%s", out)
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

func TestRenderDimensionsClause_SemanticMetadata(t *testing.T) {
	d := domain.Dimension{
		Name:           "sentiment",
		DisplayName:    domain.I18nString{"default": "Customer tone"},
		Description:    domain.I18nString{"default": "The emotional tone of the user."},
		Kind:           domain.DimSingle,
		Examples:       []string{"frustrated: I paid and this still fails"},
		ExtractionHint: "Use frustrated only when patience is running out.",
		Taxonomy: []domain.Taxonomy{
			{
				Value:       "frustrated",
				DisplayName: domain.I18nString{"default": "Frustrated"},
				Description: domain.I18nString{"default": "Repeated failure or escalation language."},
				Examples:    []string{"refund request", "cancel my account"},
			},
		},
	}
	out := renderDimensionsClause(domain.DimensionSet{d})
	for _, want := range []string{
		"The emotional tone",
		"Use frustrated only",
		"I paid and this still fails",
		"Repeated failure",
		"refund request",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("semantic prompt metadata %q missing from:\n%s", want, out)
		}
	}
}

func TestBuildEnrichSchema_OptionalSingleAllowsNull(t *testing.T) {
	// sevDim() is Required=false → must accept null both as type and as
	// an enum value (OpenAI strict mode contract).
	schema := buildEnrichSchema(domain.DimensionSet{sevDim()})
	if schema == nil {
		t.Fatal("nil schema")
	}
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	props := got["properties"].(map[string]any)
	if _, ok := props["display_title"]; !ok {
		t.Fatalf("display_title missing from schema properties: %v", props)
	}
	if _, ok := props["classification_confidence"]; !ok {
		t.Fatalf("classification_confidence missing from schema properties: %v", props)
	}
	sev := props["severity"].(map[string]any)
	types, _ := sev["type"].([]any)
	if len(types) != 2 || types[0] != "string" || types[1] != "null" {
		t.Errorf(`expected type=["string","null"], got %v`, sev["type"])
	}
	enum, _ := sev["enum"].([]any)
	if len(enum) != 3 {
		t.Errorf("expected 3-entry enum (2 values + null), got %v", enum)
	}
	if enum[len(enum)-1] != nil {
		t.Errorf("trailing enum entry should be null, got %v", enum[len(enum)-1])
	}
}

func TestBuildEnrichSchema_RequiredSingleStaysScalar(t *testing.T) {
	d := sevDim()
	d.Required = true
	schema := buildEnrichSchema(domain.DimensionSet{d})
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	sev := got["properties"].(map[string]any)["severity"].(map[string]any)
	if sev["type"] != "string" {
		t.Errorf("required single must keep type=string, got %v", sev["type"])
	}
	enum := sev["enum"].([]any)
	if len(enum) != 2 {
		t.Errorf("required single must NOT add null to enum, got %v", enum)
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

func TestBuildEnrichSchema_AllDimsInRequiredForStrictMode(t *testing.T) {
	// OpenAI strict structured-output rejects schemas where
	// `additionalProperties: false` coexists with any property that
	// isn't in `required`. Every dim (Required=true OR false) must
	// land in `required`; optionality lives in the type union.
	d := sevDim() // Required = false
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
		t.Errorf("every dim must be in 'required' for strict mode, got %v", required)
	}
}

func TestBuildEnrichSchema_AlwaysRequiresTitleAndRationale(t *testing.T) {
	schema := buildEnrichSchema(nil)
	raw, _ := json.Marshal(schema.Schema)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	required := got["required"].([]any)
	wantSet := map[string]bool{
		"title":                     false,
		"display_title":             false,
		"rationale":                 false,
		"display_rationale":         false,
		"classification_confidence": false,
	}
	for _, x := range required {
		if s, ok := x.(string); ok {
			wantSet[s] = true
		}
	}
	for field, found := range wantSet {
		if !found {
			t.Errorf("%s must always be required, got: %v", field, required)
		}
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
