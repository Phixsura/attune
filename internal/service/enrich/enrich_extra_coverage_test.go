// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package enrich

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// applyAttrsGate — pure function (metrics are package-level counters that
// tolerate concurrent increment; no registry needed)
// ---------------------------------------------------------------------------

func TestApplyAttrsGate_NoDimsDropsUnknownKeys(t *testing.T) {
	t.Parallel()
	produced := map[string]any{"category": "bug"}
	kept, diagnostics := applyAttrsGate(context.Background(), "t1", produced, nil)
	require.Empty(t, kept)
	require.NotNil(t, diagnostics)
}

func TestApplyAttrsGate_AllValuesKept(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "severity",
		Kind:     domain.DimSingle,
		Taxonomy: []domain.Taxonomy{{Value: "low"}, {Value: "high"}},
	}}
	produced := map[string]any{"severity": "low"}
	kept, diagnostics := applyAttrsGate(context.Background(), "t1", produced, dims)
	require.Equal(t, "low", kept["severity"])
	require.Nil(t, diagnostics)
}

func TestApplyAttrsGate_OffListValueDropped(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "severity",
		Kind:     domain.DimSingle,
		Taxonomy: []domain.Taxonomy{{Value: "low"}, {Value: "high"}},
	}}
	produced := map[string]any{"severity": "critical"}
	kept, diagnostics := applyAttrsGate(context.Background(), "t1", produced, dims)
	_, hasSeverity := kept["severity"]
	require.False(t, hasSeverity)
	require.NotEmpty(t, diagnostics)
	require.Equal(t, domain.AttrDropOffListValue, diagnostics[0].Reason)
}

func TestApplyAttrsGate_UnknownDimensionSkipsMetricIncrement(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "severity",
		Kind:     domain.DimSingle,
		Taxonomy: []domain.Taxonomy{{Value: "low"}},
	}}
	produced := map[string]any{"severity": "low", "invented": "nonsense"}
	kept, diagnostics := applyAttrsGate(context.Background(), "t1", produced, dims)
	require.Equal(t, "low", kept["severity"])
	require.NotEmpty(t, diagnostics)
	foundUnknown := false
	for _, d := range diagnostics {
		if d.Reason == domain.AttrDropUnknownDimension {
			foundUnknown = true
		}
	}
	require.True(t, foundUnknown)
}

func TestApplyAttrsGate_MultiDimOffListDropped(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "tags",
		Kind:     domain.DimMulti,
		Taxonomy: []domain.Taxonomy{{Value: "ui"}, {Value: "backend"}},
	}}
	produced := map[string]any{"tags": []any{"ui", "database", "infra"}}
	kept, diagnostics := applyAttrsGate(context.Background(), "t2", produced, dims)
	// filterMulti returns []string, not []any.
	keptTags, ok := kept["tags"].([]string)
	require.True(t, ok)
	require.Contains(t, keptTags, "ui")
	require.NotContains(t, keptTags, "database")
	require.NotEmpty(t, diagnostics)
}

func TestApplyAttrsGate_EmptyProduced(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "severity",
		Kind:     domain.DimSingle,
		Taxonomy: []domain.Taxonomy{{Value: "low"}},
	}}
	kept, diagnostics := applyAttrsGate(context.Background(), "t1", map[string]any{}, dims)
	require.Empty(t, kept)
	require.Nil(t, diagnostics)
}

func TestApplyAttrsGate_NilProduced(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "severity",
		Kind:     domain.DimSingle,
		Taxonomy: []domain.Taxonomy{{Value: "low"}},
	}}
	kept, diagnostics := applyAttrsGate(context.Background(), "t1", nil, dims)
	require.Empty(t, kept)
	require.Nil(t, diagnostics)
}

func TestApplyAttrsGate_SuggestedDeduplication(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:     "labels",
		Kind:     domain.DimMulti,
		Taxonomy: []domain.Taxonomy{{Value: "good"}},
	}}
	produced := map[string]any{"labels": []any{"bad1", "bad2"}}
	_, diagnostics := applyAttrsGate(context.Background(), "t3", produced, dims)
	require.NotEmpty(t, diagnostics)
}

// ---------------------------------------------------------------------------
// attrsSizeBytes — marshal error branch (line 33)
// ---------------------------------------------------------------------------

func TestAttrsSizeBytes_MarshalErrorReturnsZero(t *testing.T) {
	t.Parallel()
	attrs := map[string]any{"ch": make(chan int)}
	require.Equal(t, 0, attrsSizeBytes(attrs))
}

func TestAttrsSizeBytes_NonEmptyValue(t *testing.T) {
	t.Parallel()
	attrs := map[string]any{"key": "value"}
	size := attrsSizeBytes(attrs)
	require.Greater(t, size, 0)
	require.Equal(t, len(`{"key":"value"}`), size)
}

// ---------------------------------------------------------------------------
// truncate — multi-byte runes branch (line 173: len(runes) <= n)
// ---------------------------------------------------------------------------

func TestTruncate_MultiByteRunesFitInRuneLimit(t *testing.T) {
	t.Parallel()
	// lint-artifacts:allow test-fixture
	input := "你好吗" // lint-artifacts:allow test-fixture
	// 9 bytes, 3 runes. Limit 5: byte length > 5 but rune count 3 <= 5.
	require.Equal(t, input, truncate(input, 5))
}

func TestTruncate_MultiByteRunesExactRuneCount(t *testing.T) {
	t.Parallel()
	// lint-artifacts:allow test-fixture
	input := "你好吗" // lint-artifacts:allow test-fixture
	// 9 bytes, 3 runes. Limit 3: byte len > 3 but rune count == 3.
	require.Equal(t, input, truncate(input, 3))
}

func TestTruncate_MultiByteRunesTruncated(t *testing.T) {
	t.Parallel()
	// lint-artifacts:allow test-fixture
	input := "你好吗啊" // lint-artifacts:allow test-fixture
	got := truncate(input, 2)
	require.Len(t, []rune(got), 2)
}

// ---------------------------------------------------------------------------
// shouldMarkEnrichFailed — the false branch (ErrRateLimitWaitCanceled)
// ---------------------------------------------------------------------------

func TestShouldMarkEnrichFailed_RateLimitWrapped(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("context canceled: %w", llmclient.ErrRateLimitWaitCanceled)
	require.False(t, shouldMarkEnrichFailed(err))
}

func TestShouldMarkEnrichFailed_RateLimitDirect(t *testing.T) {
	t.Parallel()
	require.False(t, shouldMarkEnrichFailed(llmclient.ErrRateLimitWaitCanceled))
}

// ---------------------------------------------------------------------------
// countLanguageScripts — non-Latin/non-CJK letter fallback (line 50)
// ---------------------------------------------------------------------------

func TestCountLanguageScripts_CyrillicLetters(t *testing.T) {
	t.Parallel()
	// lint-artifacts:allow test-fixture
	counts := countLanguageScripts("Привет") // lint-artifacts:allow test-fixture
	require.Equal(t, 0, counts.latin)
	require.Equal(t, 0, counts.han)
	require.Equal(t, 6, counts.letters)
}

// ---------------------------------------------------------------------------
// classifyLanguageCounts — uncovered Latin threshold/ratio branches
// ---------------------------------------------------------------------------

func TestClassifyLanguageCounts_FewLatinLetters(t *testing.T) {
	t.Parallel()
	counts := languageCounts{latin: 2, letters: 10}
	require.Equal(t, LanguageUnknown, classifyLanguageCounts(counts))
}

func TestClassifyLanguageCounts_LatinBelowHalfRatio(t *testing.T) {
	t.Parallel()
	counts := languageCounts{latin: 3, letters: 10}
	require.Equal(t, LanguageUnknown, classifyLanguageCounts(counts))
}

func TestClassifyLanguageCounts_LatinMeetsThresholdAndRatio(t *testing.T) {
	t.Parallel()
	counts := languageCounts{latin: 5, letters: 10}
	require.Equal(t, LanguageEnglish, classifyLanguageCounts(counts))
}

func TestClassifyLanguageCounts_SingleLetter(t *testing.T) {
	t.Parallel()
	counts := languageCounts{latin: 1, letters: 1}
	require.Equal(t, LanguageUnknown, classifyLanguageCounts(counts))
}

// ---------------------------------------------------------------------------
// renderPrompt — unknown placeholder preserved (the else branch)
// ---------------------------------------------------------------------------

func TestRenderPrompt_UnknownPlaceholderPreserved(t *testing.T) {
	t.Parallel()
	tmpl := "Hello {{content}} and {{unknown_var}}"
	cfg := ClassifyConfig{PromptTemplate: ptrext.Of(tmpl)}
	out := renderPrompt(cfg, "test feedback")
	require.Contains(t, out, "{{unknown_var}}")
	require.Contains(t, out, "test feedback")
}

// ---------------------------------------------------------------------------
// renderDimensionsClause — all annotation branches combined
// ---------------------------------------------------------------------------

func TestRenderDimensionsClause_AllAnnotations(t *testing.T) {
	t.Parallel()
	dims := domain.DimensionSet{{
		Name:           "mood",
		Kind:           domain.DimSingle,
		Description:    domain.I18nString{"en": "User mood"},
		ExtractionHint: "Infer from tone",
		Examples:       []string{"frustrated", "happy"},
		Taxonomy: []domain.Taxonomy{
			{Value: "positive"},
			{Value: "negative"},
		},
	}}
	out := renderDimensionsClause(dims)
	require.Contains(t, out, "meaning: User mood")
	require.Contains(t, out, "guidance: Infer from tone")
	require.Contains(t, out, "examples: frustrated | happy")
	require.Contains(t, out, "pick one")
}

// ---------------------------------------------------------------------------
// sourceDisplay — default fallback and unknown source
// ---------------------------------------------------------------------------

func TestSourceDisplay_FallbackToDefaultSet(t *testing.T) {
	t.Parallel()
	e := ptrext.Of(Enricher{})
	display := e.sourceDisplay("api")
	require.NotEmpty(t, display)
}

func TestSourceDisplay_UnknownSourceStillNonEmpty(t *testing.T) {
	t.Parallel()
	e := ptrext.Of(Enricher{})
	display := e.sourceDisplay("completely-unknown-source")
	require.NotEmpty(t, display)
}

// ---------------------------------------------------------------------------
// buildOutboxEnvelope — source_display field
// ---------------------------------------------------------------------------

func TestBuildOutboxEnvelope_SourceDisplayPresent(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	payload, err := buildOutboxEnvelope(s, "trace-1", "API Client")
	require.NoError(t, err)
	require.Contains(t, string(payload), `"source_display":"API Client"`)
}

func TestBuildOutboxEnvelope_SourceDisplayOmittedIfEmpty(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	payload, err := buildOutboxEnvelope(s, "trace-1", "")
	require.NoError(t, err)
	require.NotContains(t, string(payload), "source_display")
}

// ---------------------------------------------------------------------------
// buildOutboxEnvelope — nil attrs normalized to {}
// ---------------------------------------------------------------------------

func TestBuildOutboxEnvelope_NilAttrsNormalized(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Attrs = nil
	payload, err := buildOutboxEnvelope(s, "trace-1", "")
	require.NoError(t, err)
	require.Contains(t, string(payload), `"attrs":{}`)
}

// ---------------------------------------------------------------------------
// normalizeEnrichedDisplay — different language does not fill
// ---------------------------------------------------------------------------

func TestNormalizeEnrichedDisplay_DiffLangNoAutoFill(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{Title: "English Title", Rationale: "English Rationale"}
	result := normalizeEnrichedDisplay(e, LanguageChinese, "en")
	require.Equal(t, "", result.DisplayTitle)
	require.Equal(t, "", result.DisplayRationale)
}

// ---------------------------------------------------------------------------
// sameLanguage — edge cases
// ---------------------------------------------------------------------------

func TestSameLanguage_BothChineseMatch(t *testing.T) {
	t.Parallel()
	require.True(t, sameLanguage("zh", "zh-CN"))
}

func TestSameLanguage_BothJapaneseMatch(t *testing.T) {
	t.Parallel()
	require.True(t, sameLanguage("ja", "ja-JP"))
}

// ---------------------------------------------------------------------------
// promptLanguageFor — edge cases
// ---------------------------------------------------------------------------

func TestPromptLanguageFor_ChineseVariant(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageChinese, promptLanguageFor("zh-CN"))
}

func TestPromptLanguageFor_JapaneseReturnsEn(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageEnglish, promptLanguageFor("ja"))
}

// ---------------------------------------------------------------------------
// displayLanguageForLocale — edge case
// ---------------------------------------------------------------------------

func TestDisplayLanguageForLocale_UnknownDefaultsToEn(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageEnglish, displayLanguageForLocale("unknown"))
}

func TestDisplayLanguageForLocale_JapaneseReturnsJa(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageJapanese, displayLanguageForLocale("ja"))
}

// ---------------------------------------------------------------------------
// classifyErrResult — exact boundary messages
// ---------------------------------------------------------------------------

func TestClassifyErrResult_ExactFourCharLLMPrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "llm_err", classifyErrResult(fmt.Errorf("llm:x")))
}

func TestClassifyErrResult_ExactFiveCharParsePrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "parse_err", classifyErrResult(fmt.Errorf("parse error detail")))
}

// ---------------------------------------------------------------------------
// taxonomyHint — combined annotation paths
// ---------------------------------------------------------------------------

func TestTaxonomyHint_DisplayDescExamples(t *testing.T) {
	t.Parallel()
	tx := domain.Taxonomy{
		Value:       "p0",
		DisplayName: domain.I18nString{"en": "Critical"},
		Description: domain.I18nString{"en": "Service down"},
		Examples:    []string{"outage", "data loss"},
	}
	hint := taxonomyHint(tx)
	require.Contains(t, hint, "Critical")
	require.Contains(t, hint, "Service down")
	require.Contains(t, hint, "examples: outage / data loss")
}

func TestTaxonomyHint_DisplayEqualsValue(t *testing.T) {
	t.Parallel()
	tx := domain.Taxonomy{
		Value:       "low",
		DisplayName: domain.I18nString{"en": "low"},
	}
	require.Equal(t, "", taxonomyHint(tx))
}

// ---------------------------------------------------------------------------
// promptPolicyMap — normal path
// ---------------------------------------------------------------------------

func TestPromptPolicyMap_ContainsExpectedFields(t *testing.T) {
	t.Parallel()
	policy := resolvePromptPolicy(ClassifyConfig{TenantID: "t1"})
	m := promptPolicyMap(policy.PromptPolicyMetadata)
	require.NotNil(t, m)
	require.Equal(t, defaultPromptPolicyID, m["policy_id"])
	require.Equal(t, defaultPromptPolicyVersion, m["policy_version"])
}

// ---------------------------------------------------------------------------
// normalizeLanguage — edge cases
// ---------------------------------------------------------------------------

func TestNormalizeLanguage_UnderscoreVariant(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageChinese, normalizeLanguage("zh_CN"))
}

func TestNormalizeLanguage_UpperCaseInput(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageEnglish, normalizeLanguage("EN-US"))
}

func TestNormalizeLanguage_UnknownCode(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageUnknown, normalizeLanguage("fr"))
	require.Equal(t, LanguageUnknown, normalizeLanguage(""))
}

// ---------------------------------------------------------------------------
// parseClassificationConfidence — boundary clamping
// ---------------------------------------------------------------------------

func TestParseClassificationConfidence_NegClamped(t *testing.T) {
	t.Parallel()
	got := parseClassificationConfidence(-0.5)
	require.NotNil(t, got)
	require.Equal(t, 0.0, ptrext.Indirect(got))
}

func TestParseClassificationConfidence_Over1Clamped(t *testing.T) {
	t.Parallel()
	got := parseClassificationConfidence(1.5)
	require.NotNil(t, got)
	require.Equal(t, 1.0, ptrext.Indirect(got))
}

func TestParseClassificationConfidence_BadStringNil(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseClassificationConfidence("not-a-number"))
}

// ---------------------------------------------------------------------------
// HasConstrained — edge cases
// ---------------------------------------------------------------------------

func TestHasConstrained_NilDims(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{}
	require.False(t, cfg.HasConstrained())
}

func TestHasConstrained_AllFreeformDims(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{
		Dimensions: domain.DimensionSet{
			{Name: "comment", Kind: domain.DimSingle},
			{Name: "tags", Kind: domain.DimMulti},
		},
	}
	require.False(t, cfg.HasConstrained())
}

func TestHasConstrained_OneDimWithTaxonomy(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{
		Dimensions: domain.DimensionSet{
			{Name: "severity", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{{Value: "low"}}},
		},
	}
	require.True(t, cfg.HasConstrained())
}

// ---------------------------------------------------------------------------
// promptVariableName — edge cases
// ---------------------------------------------------------------------------

func TestPromptVariableName_NonToken(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", promptVariableName("not a token"))
}

func TestPromptVariableName_ValidToken(t *testing.T) {
	t.Parallel()
	require.Equal(t, "content", promptVariableName("{{content}}"))
}

func TestPromptVariableName_WithInternalSpaces(t *testing.T) {
	t.Parallel()
	require.Equal(t, "content", promptVariableName("{{ content }}"))
}

// ---------------------------------------------------------------------------
// unknownPromptVariables
// ---------------------------------------------------------------------------

func TestUnknownPromptVariables_AllKnown(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{dimensions}} {{display_locale}}"
	require.Empty(t, unknownPromptVariables(tmpl))
}

func TestUnknownPromptVariables_HasUnknown(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{custom_field}} {{another}}"
	unknown := unknownPromptVariables(tmpl)
	require.Contains(t, unknown, "another")
	require.Contains(t, unknown, "custom_field")
}

// ---------------------------------------------------------------------------
// promptTemplateWarnings — modules and missing dimensions
// ---------------------------------------------------------------------------

func TestPromptTemplateWarnings_LegacyModules(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} {{modules}} title rationale classification_confidence display_title display_rationale"
	warnings := promptTemplateWarnings(tmpl)
	found := false
	for _, w := range warnings {
		if w.Code == "legacy_variable_modules" {
			found = true
		}
	}
	require.True(t, found)
}

func TestPromptTemplateWarnings_MissingDimensions(t *testing.T) {
	t.Parallel()
	tmpl := "{{content}} title rationale classification_confidence display_title display_rationale"
	warnings := promptTemplateWarnings(tmpl)
	found := false
	for _, w := range warnings {
		if w.Code == "missing_dimensions" {
			found = true
		}
	}
	require.True(t, found)
}

// ---------------------------------------------------------------------------
// stringFromPolicy
// ---------------------------------------------------------------------------

func TestStringFromPolicy_Exists(t *testing.T) {
	t.Parallel()
	require.Equal(t, "val", stringFromPolicy(map[string]any{"key": "val"}, "key"))
}

func TestStringFromPolicy_Missing(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", stringFromPolicy(map[string]any{"key": "val"}, "missing"))
}

func TestStringFromPolicy_NonStringType(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", stringFromPolicy(map[string]any{"key": 42}, "key"))
}

func TestStringFromPolicy_NilMapSafe(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", stringFromPolicy(nil, "key"))
}

// ---------------------------------------------------------------------------
// displayLocaleForTenantLocale — whitespace
// ---------------------------------------------------------------------------

func TestDisplayLocaleForTenantLocale_WhitespaceDefaultsToEn(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageEnglish, displayLocaleForTenantLocale("  "))
}

func TestDisplayLocaleForTenantLocale_ValidPassthrough(t *testing.T) {
	t.Parallel()
	require.Equal(t, "zh-CN", displayLocaleForTenantLocale("zh-CN"))
}
