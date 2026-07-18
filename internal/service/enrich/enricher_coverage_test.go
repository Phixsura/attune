// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// ---------------------------------------------------------------------------
// classifyPurpose
// ---------------------------------------------------------------------------

func TestClassifyPurpose_DefaultFallback(t *testing.T) {
	t.Parallel()
	require.Equal(t, "enrich", classifyPurpose(ClassifyConfig{}))
}

func TestClassifyPurpose_ExplicitPurpose(t *testing.T) {
	t.Parallel()
	require.Equal(t, "eval", classifyPurpose(ClassifyConfig{Purpose: "eval"}))
}

func TestClassifyWithDiagnosticsUsesSchemaAndFiltersAttrs(t *testing.T) {
	t.Parallel()
	llm := &capturingEnrichLLM{
		text: `{"title":"Login fails","rationale":"Users cannot sign in","severity":"critical","unknown":"drop","classification_confidence":0.9}`,
	}
	enricher := NewEnricher(nil, llm, "logical-model")
	dims := domain.DimensionSet{{
		Name: "severity",
		Kind: domain.DimSingle,
		Taxonomy: []domain.Taxonomy{
			{Value: "critical", DisplayName: domain.I18nString{"en": "Critical"}},
			{Value: "minor", DisplayName: domain.I18nString{"en": "Minor"}},
		},
		UrgentSet: []string{"critical"},
	}}

	got, err := enricher.ClassifyWithDiagnostics(context.Background(), "Cannot login", ClassifyConfig{
		TenantID:   "tenant-1",
		FeedbackID: 42,
		Channel:    "web",
		SourceID:   "src-1",
		SourceTags: []string{"vip"},
		Dimensions: dims,
	})
	require.NoError(t, err)
	require.Equal(t, "Login fails", got.Enriched.Title)
	require.True(t, got.Enriched.IsUrgent)
	require.Equal(t, "critical", got.Enriched.Attrs["severity"])
	require.NotContains(t, got.Enriched.Attrs, "unknown")
	require.NotEmpty(t, got.DropDiagnostics)
	require.Equal(t, "logical-model", llm.lastReq.Model)
	require.NotNil(t, llm.lastReq.Schema)
	require.Equal(t, "tenant-1", llm.lastReq.Guard.TenantID)
	require.Equal(t, int64(42), llm.lastReq.Guard.FeedbackID)
	require.Equal(t, "enrich", llm.lastReq.Guard.Purpose)
	require.Equal(t, []string{"vip"}, llm.lastReq.Guard.SourceTags)
}

func TestClassifyDefaultsLanguageAndDisplayLocale(t *testing.T) {
	t.Parallel()
	llm := &capturingEnrichLLM{
		text: `{"title":"Billing issue","rationale":"Customer reports a billing problem"}`,
	}
	enricher := NewEnricher(nil, llm, "")

	got, err := enricher.Classify(context.Background(), "Billing problem", ClassifyConfig{})
	require.NoError(t, err)
	require.Equal(t, "Billing issue", got.Title)
	require.Equal(t, "Billing issue", got.DisplayTitle)
	require.Nil(t, llm.lastReq.Schema)
	require.Empty(t, llm.lastReq.Guard.TenantID)
	require.Equal(t, "enrich", llm.lastReq.Guard.Purpose)
}

func TestClassifyWrapsLLMAndParseErrors(t *testing.T) {
	t.Parallel()
	enricher := NewEnricher(nil, &capturingEnrichLLM{err: errors.New("provider down")}, "")
	if _, err := enricher.Classify(context.Background(), "content", ClassifyConfig{}); err == nil ||
		!strings.Contains(err.Error(), "llm:") {
		t.Fatalf("Classify(llm error) = %v, want llm wrapper", err)
	}

	enricher = NewEnricher(nil, &capturingEnrichLLM{text: "not-json"}, "")
	if _, err := enricher.Classify(context.Background(), "content", ClassifyConfig{}); err == nil ||
		!strings.Contains(err.Error(), "parse:") {
		t.Fatalf("Classify(parse error) = %v, want parse wrapper", err)
	}
}

func TestEnrichOneRejectsNilLLMBeforeRepoAccess(t *testing.T) {
	t.Parallel()
	enricher := NewEnricher(nil, nil, "")

	err := enricher.EnrichOne(context.Background(), 42)

	require.ErrorContains(t, err, "llm client not configured")
}

func TestRunBackgroundReturnsWithoutLLM(t *testing.T) {
	t.Parallel()
	enricher := NewEnricher(nil, nil, "")

	enricher.RunBackground(context.Background(), time.Hour, 10)
}

func TestRunBackgroundReturnsWhenContextCancelled(t *testing.T) {
	t.Parallel()
	enricher := NewEnricher(nil, &capturingEnrichLLM{}, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	enricher.RunBackground(ctx, time.Hour, 10)
}

// ---------------------------------------------------------------------------
// shouldMarkEnrichFailed
// ---------------------------------------------------------------------------

func TestShouldMarkEnrichFailed_NilError(t *testing.T) {
	t.Parallel()
	require.True(t, shouldMarkEnrichFailed(errors.New("ordinary failure")))
}

func TestShouldMarkEnrichFailed_NonLLMError(t *testing.T) {
	t.Parallel()
	require.True(t, shouldMarkEnrichFailed(errors.New("db connection lost")))
}

// ---------------------------------------------------------------------------
// classifyConfigFromRow
// ---------------------------------------------------------------------------

func TestClassifyConfigFromRow(t *testing.T) {
	t.Parallel()
	tmpl := "custom {{content}}"
	row := &feedback.EnrichInput{
		TenantID:        "t1",
		Source:          "api",
		InboundSourceID: "src-123",
		SourceTags:      []string{"tag-a", "tag-b"},
		Language:        "zh",
		DisplayLocale:   "zh-CN",
		PromptTemplate:  ptrext.Of(tmpl),
		PromptPolicy:    domain.EnrichPromptPolicyConfig{Tone: "concise"},
		PromptVersionID: "ver-abc",
		Dimensions: domain.DimensionSet{{
			Name: "severity",
			Kind: domain.DimSingle,
		}},
	}
	cfg := classifyConfigFromRow(row)
	require.Equal(t, "t1", cfg.TenantID)
	require.Equal(t, "api", cfg.Channel)
	require.Equal(t, "src-123", cfg.SourceID)
	require.Equal(t, []string{"tag-a", "tag-b"}, cfg.SourceTags)
	require.Equal(t, "zh-CN", cfg.DisplayLocale)
	require.Equal(t, ptrext.Of(tmpl), cfg.PromptTemplate)
	require.Equal(t, "concise", cfg.PolicyConfig.Tone)
	require.Equal(t, "ver-abc", cfg.PromptVersionID)
	require.Len(t, cfg.Dimensions, 1)
}

func TestClassifyConfigFromRow_NormalizesDisplayLocale(t *testing.T) {
	t.Parallel()
	row := &feedback.EnrichInput{
		TenantID:      "t1",
		DisplayLocale: "",
	}
	cfg := classifyConfigFromRow(row)
	require.Equal(t, LanguageEnglish, cfg.DisplayLocale)
}

// ---------------------------------------------------------------------------
// droppedAttrsAudit
// ---------------------------------------------------------------------------

func TestDroppedAttrsAudit_Empty(t *testing.T) {
	t.Parallel()
	require.Nil(t, droppedAttrsAudit(nil))
	require.Nil(t, droppedAttrsAudit([]domain.AttrDropDiagnostic{}))
}

func TestDroppedAttrsAudit_NonEmpty(t *testing.T) {
	t.Parallel()
	diags := []domain.AttrDropDiagnostic{
		{Dim: "severity", Reason: domain.AttrDropOffListValue, Count: 1},
	}
	result := droppedAttrsAudit(diags)
	require.NotNil(t, result)
	require.Contains(t, result, "diagnostics")
}

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate_EmptyString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", truncate("", 5))
}

func TestTruncate_ShortString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "abc", truncate("abc", 10))
}

func TestTruncate_ExactBoundary(t *testing.T) {
	t.Parallel()
	require.Equal(t, "abcde", truncate("abcde", 5))
}

func TestTruncate_LongASCII(t *testing.T) {
	t.Parallel()
	require.Equal(t, "abc", truncate("abcdef", 3))
}

func TestTruncate_ZeroLimit(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", truncate("abc", 0))
}

// ---------------------------------------------------------------------------
// snapshotDisplayLocale and hasDisplayOutput
// ---------------------------------------------------------------------------

func TestSnapshotDisplayLocale_NoDisplayOutput(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{Title: "x", Rationale: "y"}
	require.Equal(t, "", snapshotDisplayLocale("zh-CN", e))
}

func TestSnapshotDisplayLocale_WithDisplayTitle(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{DisplayTitle: "dt"}
	require.Equal(t, "zh-CN", snapshotDisplayLocale("zh-CN", e))
}

func TestSnapshotDisplayLocale_WithDisplayRationale(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{DisplayRationale: "dr"}
	require.Equal(t, "zh-CN", snapshotDisplayLocale("zh-CN", e))
}

func TestSnapshotDisplayLocale_EmptyLocaleDefaultsToEn(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{DisplayTitle: "dt"}
	require.Equal(t, LanguageEnglish, snapshotDisplayLocale("", e))
}

func TestHasDisplayOutput_BothEmpty(t *testing.T) {
	t.Parallel()
	require.False(t, hasDisplayOutput(domain.Enriched{}))
}

func TestHasDisplayOutput_TitleSet(t *testing.T) {
	t.Parallel()
	require.True(t, hasDisplayOutput(domain.Enriched{DisplayTitle: "dt"}))
}

func TestHasDisplayOutput_RationaleSet(t *testing.T) {
	t.Parallel()
	require.True(t, hasDisplayOutput(domain.Enriched{DisplayRationale: "dr"}))
}

func TestHasDisplayOutput_BothSet(t *testing.T) {
	t.Parallel()
	require.True(t, hasDisplayOutput(domain.Enriched{DisplayTitle: "dt", DisplayRationale: "dr"}))
}

// ---------------------------------------------------------------------------
// normalizeEnrichedDisplay — additional edge cases
// ---------------------------------------------------------------------------

func TestNormalizeEnrichedDisplay_PreservesExistingDisplayFields(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{
		Title:            "title-en",
		Rationale:        "rationale-en",
		DisplayTitle:     "already-set-dt",
		DisplayRationale: "already-set-dr",
	}
	result := normalizeEnrichedDisplay(e, LanguageEnglish, "en-US")
	require.Equal(t, "already-set-dt", result.DisplayTitle)
	require.Equal(t, "already-set-dr", result.DisplayRationale)
}

func TestNormalizeEnrichedDisplay_UnknownSourceLeavesEmpty(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{Title: "t", Rationale: "r"}
	result := normalizeEnrichedDisplay(e, LanguageUnknown, "en")
	require.Equal(t, "", result.DisplayTitle)
	require.Equal(t, "", result.DisplayRationale)
}

// ---------------------------------------------------------------------------
// parseClassificationConfidence — NaN, Inf
// ---------------------------------------------------------------------------

func TestParseClassificationConfidence_NaN(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseClassificationConfidence(math.NaN()))
}

func TestParseClassificationConfidence_PosInf(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseClassificationConfidence(math.Inf(1)))
}

func TestParseClassificationConfidence_NegInf(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseClassificationConfidence(math.Inf(-1)))
}

func TestParseClassificationConfidence_BoolIgnored(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseClassificationConfidence(true))
}

func TestParseClassificationConfidence_EmptyStringIgnored(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseClassificationConfidence(""))
}

func TestParseClassificationConfidence_ValidString(t *testing.T) {
	t.Parallel()
	got := parseClassificationConfidence("  0.75  ")
	require.NotNil(t, got)
	require.Equal(t, 0.75, ptrext.Indirect(got))
}

func TestParseClassificationConfidence_ExactZero(t *testing.T) {
	t.Parallel()
	got := parseClassificationConfidence(0.0)
	require.NotNil(t, got)
	require.Equal(t, 0.0, ptrext.Indirect(got))
}

func TestParseClassificationConfidence_ExactOne(t *testing.T) {
	t.Parallel()
	got := parseClassificationConfidence(1.0)
	require.NotNil(t, got)
	require.Equal(t, 1.0, ptrext.Indirect(got))
}

// ---------------------------------------------------------------------------
// semanticRouteGuard
// ---------------------------------------------------------------------------

func TestSemanticRouteGuard_AllFieldsSet(t *testing.T) {
	t.Parallel()
	route := llmclient.RouteMetadata{
		LogicalModel:  "gpt-4o",
		ProviderModel: "gpt-4o-2024-05-13",
		ChannelID:     "ch-1",
		ChannelName:   "Primary",
		Protocol:      "openai_compat",
	}
	out := semanticRouteGuard(route)
	require.Equal(t, "gpt-4o", out["logical_model"])
	require.Equal(t, "gpt-4o-2024-05-13", out["provider_model"])
	require.Equal(t, "ch-1", out["channel_id"])
	require.Equal(t, "Primary", out["channel_name"])
	require.Equal(t, "openai_compat", out["protocol"])
}

func TestSemanticRouteGuard_Empty(t *testing.T) {
	t.Parallel()
	out := semanticRouteGuard(llmclient.RouteMetadata{})
	require.Empty(t, out)
}

func TestSemanticRouteGuard_PartialFields(t *testing.T) {
	t.Parallel()
	route := llmclient.RouteMetadata{ProviderModel: "gpt-4o"}
	out := semanticRouteGuard(route)
	require.Len(t, out, 1)
	require.Equal(t, "gpt-4o", out["provider_model"])
}

// ---------------------------------------------------------------------------
// confidenceAudit
// ---------------------------------------------------------------------------

func TestConfidenceAudit_NilConfidence(t *testing.T) {
	t.Parallel()
	result := confidenceAudit(domain.Enriched{})
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestConfidenceAudit_WithConfidence(t *testing.T) {
	t.Parallel()
	result := confidenceAudit(domain.Enriched{
		ClassificationConfidence: ptrext.Of(0.87),
	})
	require.Equal(t, 0.87, result["overall"])
	require.Equal(t, "llm_self_report", result["source"])
}

// ---------------------------------------------------------------------------
// extractTraceID
// ---------------------------------------------------------------------------

func TestExtractTraceID_FromContext(t *testing.T) {
	t.Parallel()
	ctx := trace.WithID(context.Background(), "my-trace-id")
	require.Equal(t, "my-trace-id", extractTraceID(ctx))
}

func TestExtractTraceID_FallbackGenerated(t *testing.T) {
	t.Parallel()
	id := extractTraceID(context.Background())
	require.NotEmpty(t, id)
}

func TestExtractTraceID_TwoCallsWithoutCtxAreDifferent(t *testing.T) {
	t.Parallel()
	id1 := extractTraceID(context.Background())
	id2 := extractTraceID(context.Background())
	require.NotEqual(t, id1, id2)
}

// ---------------------------------------------------------------------------
// selectOutboxTargets — Discord coverage
// ---------------------------------------------------------------------------

func TestSelectOutboxTargets_DiscordRoutes(t *testing.T) {
	t.Parallel()
	targets := []notifytarget.NotifyTarget{
		{TenantID: "t", DestinationType: notifytarget.DestDiscord, Audience: notifytarget.AudiencePool, URL: "https://discord.com/hook/x"},
	}
	got := selectOutboxTargets(targets, sampleSnapshot(false))
	require.Len(t, got, 1)
}

// ---------------------------------------------------------------------------
// buildOutboxEnvelope — display fields
// ---------------------------------------------------------------------------

func TestBuildOutboxEnvelope_WithDisplayFields(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.DisplayTitle = "Display Title"
	s.DisplayRationale = "Display Rationale"
	s.DisplayLocale = "zh-CN"
	s.Language = "zh"
	payload, err := buildOutboxEnvelope(s, "trace-1", "API client")
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(payload, &got))
	fb := got["feedback"].(map[string]any)
	enriched := fb["enriched"].(map[string]any)
	require.Equal(t, "Display Title", enriched["display_title"])
	require.Equal(t, "Display Rationale", enriched["display_rationale"])
	require.Equal(t, "zh-CN", enriched["display_locale"])
	require.Equal(t, "zh", fb["language"])
}

func TestBuildOutboxEnvelope_LanguageOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = ""
	payload, err := buildOutboxEnvelope(s, "", "")
	require.NoError(t, err)

	var env map[string]any
	require.NoError(t, json.Unmarshal(payload, &env))
	fb := env["feedback"].(map[string]any)
	_, present := fb["language"]
	require.False(t, present, "empty language should be omitted via omitempty")
}

// ---------------------------------------------------------------------------
// semanticRun — edge cases
// ---------------------------------------------------------------------------

func TestSemanticRun_EmptyRouteUsesEnricherModel(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	cfg := ClassifyConfig{TenantID: s.TenantID}
	e := ptrext.Of(Enricher{model: "fallback-model"})
	run := e.semanticRun(s, cfg, classifyResult{
		Enriched: domain.Enriched{Title: "x", Rationale: "y"},
		Route:    llmclient.RouteMetadata{},
	})
	require.Equal(t, "fallback-model", run.Model)
}

func TestSemanticRun_RouteModelOverridesEnricherModel(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	cfg := ClassifyConfig{TenantID: s.TenantID}
	e := ptrext.Of(Enricher{model: "fallback-model"})
	run := e.semanticRun(s, cfg, classifyResult{
		Enriched: domain.Enriched{Title: "x", Rationale: "y"},
		Route:    llmclient.RouteMetadata{ProviderModel: "routed-model"},
	})
	require.Equal(t, "routed-model", run.Model)
}

func TestSemanticRun_NoPromptVersionID(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	cfg := ClassifyConfig{TenantID: s.TenantID}
	e := ptrext.Of(Enricher{model: "m"})
	run := e.semanticRun(s, cfg, classifyResult{
		Enriched: domain.Enriched{Title: "x", Rationale: "y"},
	})
	require.Equal(t, "", run.PromptVersionID)
	_, hasVersionKey := run.GuardSummary["prompt_version_id"]
	require.False(t, hasVersionKey)
}

func TestSemanticRun_WithDisplayLocale(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	s.DisplayLocale = "zh-CN"
	cfg := ClassifyConfig{TenantID: s.TenantID, DisplayLocale: "zh-CN"}
	e := ptrext.Of(Enricher{model: "m"})
	run := e.semanticRun(s, cfg, classifyResult{
		Enriched: domain.Enriched{
			Title:            "t",
			Rationale:        "r",
			DisplayRationale: "dr",
		},
	})
	require.Equal(t, "dr", run.Rationale["display_summary"])
	require.Equal(t, "zh-CN", run.Rationale["display_locale"])
}

func TestSemanticRun_WithDroppedAttrs(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	cfg := ClassifyConfig{TenantID: s.TenantID}
	e := ptrext.Of(Enricher{model: "m"})
	diags := []domain.AttrDropDiagnostic{
		{Dim: "severity", Reason: domain.AttrDropOffListValue, Count: 1},
	}
	run := e.semanticRun(s, cfg, classifyResult{
		Enriched:          domain.Enriched{Title: "x", Rationale: "y"},
		DroppedAttrsAudit: droppedAttrsAudit(diags),
	})
	require.NotNil(t, run.DroppedAttrs)
}

func TestSemanticRun_WithRouteGuard(t *testing.T) {
	t.Parallel()
	s := sampleSnapshot(false)
	s.Language = LanguageEnglish
	cfg := ClassifyConfig{TenantID: s.TenantID}
	e := ptrext.Of(Enricher{model: "m"})
	run := e.semanticRun(s, cfg, classifyResult{
		Enriched: domain.Enriched{Title: "x", Rationale: "y"},
		Route:    llmclient.RouteMetadata{LogicalModel: "gpt-4o", ChannelID: "ch-1"},
	})
	require.Contains(t, run.GuardSummary, "routing")
}

// ---------------------------------------------------------------------------
// promptPolicyMap
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// promptPolicyGuard
// ---------------------------------------------------------------------------

func TestPromptPolicyGuard_ContainsExpectedKeys(t *testing.T) {
	t.Parallel()
	policy := resolvePromptPolicy(ClassifyConfig{})
	guard := promptPolicyGuard(policy)
	expectedKeys := []string{
		"policy_id", "policy_version", "mode", "prompt_source",
		"template_language", "display_locale", "prompt_contract",
		"output_contract", "policy_config", "schema_fingerprint",
		"prompt_fingerprint", "warnings",
	}
	for _, k := range expectedKeys {
		require.Contains(t, guard, k)
	}
}

// ---------------------------------------------------------------------------
// warningCodes
// ---------------------------------------------------------------------------

func TestWarningCodes_Empty(t *testing.T) {
	t.Parallel()
	require.Nil(t, warningCodes(nil))
	require.Nil(t, warningCodes([]PromptWarning{}))
}

func TestWarningCodes_NonEmpty(t *testing.T) {
	t.Parallel()
	warnings := []PromptWarning{
		{Code: "w1"}, {Code: "w2"},
	}
	codes := warningCodes(warnings)
	require.Equal(t, []string{"w1", "w2"}, codes)
}

// ---------------------------------------------------------------------------
// sortedKeys
// ---------------------------------------------------------------------------

func TestSortedKeys_MultipleKeys(t *testing.T) {
	t.Parallel()
	m := map[string]bool{"z": true, "a": true, "m": true}
	require.Equal(t, []string{"a", "m", "z"}, sortedKeys(m))
}

func TestSortedKeys_Empty(t *testing.T) {
	t.Parallel()
	require.Empty(t, sortedKeys(map[string]bool{}))
}

// ---------------------------------------------------------------------------
// fingerprintJSON
// ---------------------------------------------------------------------------

func TestFingerprintJSON_ValidValue(t *testing.T) {
	t.Parallel()
	fp := fingerprintJSON(map[string]string{"a": "b"})
	require.True(t, strings.HasPrefix(fp, "sha256:"))
	require.Len(t, fp, len("sha256:")+16)
}

func TestFingerprintJSON_DeterministicForSameInput(t *testing.T) {
	t.Parallel()
	a := fingerprintJSON(42)
	b := fingerprintJSON(42)
	require.Equal(t, a, b)
}

func TestFingerprintJSON_DifferentForDifferentInput(t *testing.T) {
	t.Parallel()
	a := fingerprintJSON("one")
	b := fingerprintJSON("two")
	require.NotEqual(t, a, b)
}

// ---------------------------------------------------------------------------
// failureReasonClass / normalizeFailureReasonClass / failureSnapshot
// ---------------------------------------------------------------------------

func TestFailureReasonClass_BucketsLLMAndParseAndOther(t *testing.T) {
	t.Parallel()
	require.Equal(t, "llm_err", failureReasonClass(errors.New("llm: upstream timeout")))
	require.Equal(t, "parse_err", failureReasonClass(errors.New("parse: invalid json")))
	require.Equal(t, "other_err", failureReasonClass(errors.New("db: unavailable")))
	require.Equal(t, "other_err", failureReasonClass(nil))
}

func TestNormalizeFailureReasonClass_OnlyAllowsKnownValues(t *testing.T) {
	t.Parallel()
	require.Equal(t, "llm_err", normalizeFailureReasonClass("llm_err"))
	require.Equal(t, "parse_err", normalizeFailureReasonClass("parse_err"))
	require.Equal(t, "other_err", normalizeFailureReasonClass("other_err"))
	require.Equal(t, "other_err", normalizeFailureReasonClass("unexpected"))
}

func TestFailureSnapshot_CapturesRouteAndPolicyMetadata(t *testing.T) {
	t.Parallel()
	e := &Enricher{model: "fallback-model"}
	cfg := ClassifyConfig{
		TenantID:        "tenant-1",
		PolicyConfig:    domain.EnrichPromptPolicyConfig{Tone: "concise"},
		PromptVersionID: "prompt-ver-1",
	}
	snapshot := e.failureSnapshot(cfg, llmclient.RouteMetadata{
		ChannelID:     "chan-1",
		ChannelName:   "anthropic-prod",
		Protocol:      "anthropic",
		LogicalModel:  "logical-model",
		ProviderModel: "provider-model",
	}, errors.New("llm: timeout"))

	require.Equal(t, "llm_err", snapshot.ReasonClass)
	require.Equal(t, "provider-model", snapshot.Model)
	require.Equal(t, "chan-1", snapshot.ChannelID)
	require.Equal(t, "anthropic-prod", snapshot.ChannelName)
	require.Equal(t, "llm_err", normalizeFailureReasonClass(snapshot.ReasonClass))
	require.NotEmpty(t, snapshot.ConfigFingerprint)
	require.NotEmpty(t, snapshot.PromptVersion)
}

func TestFailureSnapshot_FallsBackToLogicalModelThenEnricherModel(t *testing.T) {
	t.Parallel()
	e := &Enricher{model: "fallback-model"}
	cfg := ClassifyConfig{}

	withLogical := e.failureSnapshot(cfg, llmclient.RouteMetadata{
		LogicalModel: "logical-model",
	}, errors.New("parse: bad json"))
	require.Equal(t, "logical-model", withLogical.Model)

	withFallback := e.failureSnapshot(cfg, llmclient.RouteMetadata{}, errors.New("db: down"))
	require.Equal(t, "fallback-model", withFallback.Model)
}

// ---------------------------------------------------------------------------
// canonicalTemplate
// ---------------------------------------------------------------------------

func TestCanonicalTemplate_NormalizesWhitespace(t *testing.T) {
	t.Parallel()
	require.Equal(t, "{{content}} {{dimensions}}", canonicalTemplate("{{ content }} {{ dimensions }}"))
}

func TestCanonicalTemplate_LeavesNonTokensAlone(t *testing.T) {
	t.Parallel()
	input := "plain text with no tokens"
	require.Equal(t, input, canonicalTemplate(input))
}

// ---------------------------------------------------------------------------
// hashString
// ---------------------------------------------------------------------------

func TestHashString_Prefix(t *testing.T) {
	t.Parallel()
	require.True(t, strings.HasPrefix(hashString("test"), "sha256:"))
}

// ---------------------------------------------------------------------------
// shortHash
// ---------------------------------------------------------------------------

func TestShortHash_Deterministic(t *testing.T) {
	t.Parallel()
	a := shortHash("hello", "world")
	b := shortHash("hello", "world")
	require.Equal(t, a, b)
}

func TestShortHash_Length16(t *testing.T) {
	t.Parallel()
	require.Len(t, shortHash("x"), 16)
}

func TestShortHash_DifferentInputsDiffer(t *testing.T) {
	t.Parallel()
	require.NotEqual(t, shortHash("a"), shortHash("b"))
}

// ---------------------------------------------------------------------------
// displayLanguageName
// ---------------------------------------------------------------------------

func TestDisplayLanguageName_SimplifiedChinese(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Simplified Chinese", displayLanguageName("zh"))
	require.Equal(t, "Simplified Chinese", displayLanguageName("zh-CN"))
}

func TestDisplayLanguageName_TraditionalChinese(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Traditional Chinese", displayLanguageName("zh-TW"))
	require.Equal(t, "Traditional Chinese", displayLanguageName("zh-HK"))
	require.Equal(t, "Traditional Chinese", displayLanguageName("zh-MO"))
	require.Equal(t, "Traditional Chinese", displayLanguageName("zh-Hant"))
}

func TestDisplayLanguageName_Japanese(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Japanese", displayLanguageName("ja"))
	require.Equal(t, "Japanese", displayLanguageName("ja-JP"))
}

func TestDisplayLanguageName_English(t *testing.T) {
	t.Parallel()
	require.Equal(t, "English", displayLanguageName("en"))
	require.Equal(t, "English", displayLanguageName("en-US"))
}

func TestDisplayLanguageName_FallbackToEnglish(t *testing.T) {
	t.Parallel()
	require.Equal(t, "English", displayLanguageName("fr"))
	require.Equal(t, "English", displayLanguageName(""))
}

// ---------------------------------------------------------------------------
// classifyDisplayLocale
// ---------------------------------------------------------------------------

func TestClassifyDisplayLocale_ExplicitLocale(t *testing.T) {
	t.Parallel()
	require.Equal(t, "zh-CN", classifyDisplayLocale(ClassifyConfig{DisplayLocale: "zh-CN"}))
}

func TestClassifyDisplayLocale_FallbackToLanguage(t *testing.T) {
	t.Parallel()
	require.Equal(t, "zh", classifyDisplayLocale(ClassifyConfig{Language: "zh"}))
}

func TestClassifyDisplayLocale_FallbackToEnglish(t *testing.T) {
	t.Parallel()
	require.Equal(t, LanguageEnglish, classifyDisplayLocale(ClassifyConfig{}))
}

func TestClassifyDisplayLocale_EmptyDisplayLocaleUsesLanguage(t *testing.T) {
	t.Parallel()
	require.Equal(t, "ja", classifyDisplayLocale(ClassifyConfig{Language: "ja"}))
}

// ---------------------------------------------------------------------------
// MemoryQueue — Close idempotency, Configure, Status, Len
// ---------------------------------------------------------------------------

func TestMemoryQueue_CloseIdempotent(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(5)
	require.NoError(t, q.Close())
	require.NoError(t, q.Close())
}

func TestMemoryQueue_SubmitAfterClose(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(5)
	_ = q.Close()
	err := q.Submit(context.Background(), Job{ID: 1})
	require.ErrorIs(t, err, ErrQueueClosed)
}

func TestMemoryQueue_Len(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(5)
	defer func() { _ = q.Close() }()
	require.Equal(t, 0, q.Len())
	_ = q.Submit(context.Background(), Job{ID: 1})
	require.Equal(t, 1, q.Len())
}

func TestMemoryQueue_Status(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(10)
	defer func() { _ = q.Close() }()
	status := q.Status()
	require.Equal(t, 0, status.Depth)
	require.Equal(t, 10, status.CapacityTarget)
	require.Equal(t, 10, status.CapacityEffective)
	require.False(t, status.ResizePending)
}

func TestMemoryQueue_ConfigureResize(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(5)
	defer func() { _ = q.Close() }()
	status := q.Configure(20)
	require.Equal(t, 20, status.CapacityTarget)
}

func TestMemoryQueue_ConfigureMinCapacity(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(5)
	defer func() { _ = q.Close() }()
	status := q.Configure(0)
	require.Equal(t, 1, status.CapacityTarget)
}

func TestMemoryQueue_ConfigureShrinkPending(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(10)
	for i := 0; i < 5; i++ {
		require.NoError(t, q.Submit(context.Background(), Job{ID: int64(i + 1)}))
	}
	status := q.Configure(2)
	require.Equal(t, 2, status.CapacityTarget)
	require.True(t, status.ResizePending, "effective > target while buffer is larger")
	require.NoError(t, q.Close())
}

func TestMemoryQueue_ConsumeChannel(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(2)
	require.NoError(t, q.Submit(context.Background(), Job{ID: 42}))
	require.NoError(t, q.Close())

	ch := q.Consume()
	job := <-ch
	require.Equal(t, int64(42), job.ID)

	_, open := <-ch
	require.False(t, open, "channel should be closed after queue close + drain")
}

func TestMemoryQueue_NewMinCapacity(t *testing.T) {
	t.Parallel()
	q := NewMemoryQueue(0)
	defer func() { _ = q.Close() }()
	require.Equal(t, 1, q.Status().CapacityTarget)
}

// ---------------------------------------------------------------------------
// normalizeRunnerConfig
// ---------------------------------------------------------------------------

func TestNormalizeRunnerConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := normalizeRunnerConfig(RunnerConfig{})
	require.Equal(t, 1, cfg.QueueLen)
	require.Equal(t, 1, cfg.Workers)
	require.Equal(t, 1, cfg.BatchSize)
	require.Equal(t, time.Millisecond, cfg.BatchWindow)
	require.Equal(t, time.Second, cfg.SweepInterval)
}

func TestNormalizeRunnerConfig_PreservesValidValues(t *testing.T) {
	t.Parallel()
	cfg := normalizeRunnerConfig(RunnerConfig{
		QueueLen:      100,
		Workers:       8,
		BatchSize:     50,
		BatchWindow:   time.Second,
		SweepInterval: time.Minute,
	})
	require.Equal(t, 100, cfg.QueueLen)
	require.Equal(t, 8, cfg.Workers)
	require.Equal(t, 50, cfg.BatchSize)
	require.Equal(t, time.Second, cfg.BatchWindow)
	require.Equal(t, time.Minute, cfg.SweepInterval)
}

func TestNormalizeRunnerConfig_NegativeValues(t *testing.T) {
	t.Parallel()
	cfg := normalizeRunnerConfig(RunnerConfig{
		QueueLen: -1, Workers: -1, BatchSize: -1,
		BatchWindow: -time.Second, SweepInterval: -time.Second,
	})
	require.Equal(t, 1, cfg.QueueLen)
	require.Equal(t, 1, cfg.Workers)
	require.Equal(t, 1, cfg.BatchSize)
	require.Equal(t, time.Millisecond, cfg.BatchWindow)
	require.Equal(t, time.Second, cfg.SweepInterval)
}

// ---------------------------------------------------------------------------
// Runner.Configure + Runner.Snapshot
// ---------------------------------------------------------------------------

func TestRunnerConfigureAndSnapshot(t *testing.T) {
	t.Parallel()
	r := NewRunner(&stubLister{}, &stubExecutor{}, RunnerConfig{
		QueueLen: 5, Workers: 2, BatchSize: 3,
		BatchWindow: time.Second, SweepInterval: time.Minute,
	})
	snap := r.Snapshot()
	require.Equal(t, 2, snap.Workers)
	require.Equal(t, 3, snap.BatchSize)

	_ = r.Configure(RunnerConfig{
		QueueLen: 10, Workers: 4, BatchSize: 6,
		BatchWindow: 2 * time.Second, SweepInterval: 2 * time.Minute,
	})
	snap = r.Snapshot()
	require.Equal(t, 4, snap.Workers)
	require.Equal(t, 6, snap.BatchSize)
	require.Equal(t, 2*time.Second, snap.BatchWindow)
}

// ---------------------------------------------------------------------------
// Runner.Wait
// ---------------------------------------------------------------------------

func TestRunnerWait_NilRunner(t *testing.T) {
	t.Parallel()
	var r *Runner
	require.NoError(t, r.Wait(context.Background()))
}

func TestRunnerWait_ContextCancel(t *testing.T) {
	t.Parallel()
	r := NewRunner(&stubLister{}, &stubExecutor{}, RunnerConfig{
		QueueLen: 1, Workers: 1, BatchSize: 1,
		BatchWindow: time.Millisecond, SweepInterval: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.Wait(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Runner.Submit with nil receiver
// ---------------------------------------------------------------------------

func TestRunnerSubmit_NilRunner(t *testing.T) {
	t.Parallel()
	var r *Runner
	err := r.Submit(context.Background(), Job{ID: 1})
	require.ErrorIs(t, err, ErrQueueClosed)
}

// ---------------------------------------------------------------------------
// maxInt
// ---------------------------------------------------------------------------

func TestMaxInt_Positive(t *testing.T) {
	t.Parallel()
	require.Equal(t, 5, maxInt(5, 1))
}

func TestMaxInt_Zero(t *testing.T) {
	t.Parallel()
	require.Equal(t, 10, maxInt(0, 10))
}

func TestMaxInt_Negative(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1, maxInt(-3, 1))
}

// ---------------------------------------------------------------------------
// buildSnapshot — additional coverage
// ---------------------------------------------------------------------------

func TestBuildSnapshot_NoDisplayOutput(t *testing.T) {
	t.Parallel()
	row := ptrext.Of(feedback.EnrichInput{
		TenantID:      "t1",
		Content:       "content",
		DisplayLocale: "zh-CN",
		CreatedAt:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	enriched := domain.Enriched{
		Title:     "title",
		Rationale: "rationale",
	}
	snap := buildSnapshot(1, row, enriched, time.Now())
	require.Equal(t, "", snap.DisplayLocale, "no display output means empty locale")
}

func TestBuildSnapshot_WithDisplayOutput(t *testing.T) {
	t.Parallel()
	row := ptrext.Of(feedback.EnrichInput{
		TenantID:      "t1",
		Content:       "content",
		DisplayLocale: "zh-CN",
		CreatedAt:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	enriched := domain.Enriched{
		Title:        "title",
		DisplayTitle: "display-t",
		Rationale:    "rationale",
	}
	snap := buildSnapshot(1, row, enriched, time.Now())
	require.Equal(t, "zh-CN", snap.DisplayLocale)
}

// ---------------------------------------------------------------------------
// Enricher.sourceDisplay — with injected SourceSet
// ---------------------------------------------------------------------------

func TestSourceDisplay_WithInjectedSet(t *testing.T) {
	t.Parallel()
	e := ptrext.Of(Enricher{})
	custom := domain.NewSourceSet(map[string]string{
		"custom": "Custom Source",
	})
	e.SetSourceSet(custom)
	require.Equal(t, "Custom Source", e.sourceDisplay("custom"))
}

// ---------------------------------------------------------------------------
// classifyErrResult — comprehensive
// ---------------------------------------------------------------------------

func TestClassifyErrResult_Nil(t *testing.T) {
	t.Parallel()
	require.Equal(t, "ok", classifyErrResult(nil))
}

func TestClassifyErrResult_LLMPrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "llm_err", classifyErrResult(errors.New("llm: connection refused")))
}

func TestClassifyErrResult_ParsePrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "parse_err", classifyErrResult(errors.New("parse: invalid JSON")))
}

func TestClassifyErrResult_Other(t *testing.T) {
	t.Parallel()
	require.Equal(t, "other_err", classifyErrResult(errors.New("timeout")))
}

func TestClassifyErrResult_ShortMessage(t *testing.T) {
	t.Parallel()
	require.Equal(t, "other_err", classifyErrResult(errors.New("x")))
}

// ---------------------------------------------------------------------------
// renderDomainGuidance
// ---------------------------------------------------------------------------

func TestRenderDomainGuidance_Empty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", renderDomainGuidance(""))
	require.Equal(t, "", renderDomainGuidance("   "))
}

func TestRenderDomainGuidance_NonEmpty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Additional operator guidance: focus on UX", renderDomainGuidance("  focus on UX  "))
}

// ---------------------------------------------------------------------------
// defaultPromptTemplateFor
// ---------------------------------------------------------------------------

func TestDefaultPromptTemplateFor_Chinese(t *testing.T) {
	t.Parallel()
	tmpl := defaultPromptTemplateFor("zh")
	require.Contains(t, tmpl, "{{content}}")
}

func TestDefaultPromptTemplateFor_English(t *testing.T) {
	t.Parallel()
	tmpl := defaultPromptTemplateFor("en")
	require.Contains(t, tmpl, "{{content}}")
}

func TestDefaultPromptTemplateFor_FallbackToEnglish(t *testing.T) {
	t.Parallel()
	// Unknown language should use English template.
	en := defaultPromptTemplateFor("en")
	fr := defaultPromptTemplateFor("fr")
	require.Equal(t, en, fr)
}

// ---------------------------------------------------------------------------
// renderOutputLanguagePolicy
// ---------------------------------------------------------------------------

func TestRenderOutputLanguagePolicy_DisplayOnly(t *testing.T) {
	t.Parallel()
	result := renderOutputLanguagePolicy(
		domain.EnrichPromptPolicyConfig{
			OutputLanguagePolicy: domain.PromptOutputDisplayOnly,
		},
		"Simplified Chinese",
	)
	require.Contains(t, result, "Simplified Chinese")
	require.Contains(t, result, "display fields must match")
}

func TestRenderOutputLanguagePolicy_SourcePreserve(t *testing.T) {
	t.Parallel()
	result := renderOutputLanguagePolicy(
		domain.EnrichPromptPolicyConfig{
			OutputLanguagePolicy: domain.PromptOutputSourceAndDisplay,
		},
		"English",
	)
	require.Contains(t, result, "same natural language as the raw feedback")
}

// ---------------------------------------------------------------------------
// missingOutputMentions
// ---------------------------------------------------------------------------

func TestMissingOutputMentions_AllPresent(t *testing.T) {
	t.Parallel()
	// Build a template that mentions all built-in outputs.
	allOutputs := builtInPromptOutputs()
	var parts []string
	for _, out := range allOutputs {
		parts = append(parts, out.Name)
	}
	tmpl := strings.Join(parts, " ")
	require.Empty(t, missingOutputMentions(tmpl))
}

func TestMissingOutputMentions_EmptyTemplate(t *testing.T) {
	t.Parallel()
	// An empty template is missing everything except dimension_fields.
	missing := missingOutputMentions("")
	require.NotEmpty(t, missing)
	for _, name := range missing {
		require.NotEqual(t, "dimension_fields", name, "dimension_fields should be skipped")
	}
}

// ---------------------------------------------------------------------------
// i18nHint
// ---------------------------------------------------------------------------

func TestI18nHint_BothLanguages(t *testing.T) {
	t.Parallel()
	s := domain.I18nString{"en": "bug", "zh": "缺陷"} // lint-artifacts:allow test-fixture
	hint := i18nHint(s)
	require.Equal(t, "bug | 缺陷", hint) // lint-artifacts:allow test-fixture
}

func TestI18nHint_EnglishOnly(t *testing.T) {
	t.Parallel()
	s := domain.I18nString{"en": "bug"}
	require.Equal(t, "bug", i18nHint(s))
}

func TestI18nHint_ChineseOnly(t *testing.T) {
	t.Parallel()
	s := domain.I18nString{"zh": "缺陷"}  // lint-artifacts:allow test-fixture
	require.Equal(t, "缺陷", i18nHint(s)) // lint-artifacts:allow test-fixture
}

func TestI18nHint_Empty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", i18nHint(domain.I18nString{}))
}

func TestI18nHint_SameEnAndZh(t *testing.T) {
	t.Parallel()
	s := domain.I18nString{"en": "same", "zh": "same"}
	require.Equal(t, "same", i18nHint(s))
}

// ---------------------------------------------------------------------------
// promptVersion
// ---------------------------------------------------------------------------

func TestPromptVersion_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()
	v := promptVersion(ClassifyConfig{})
	require.NotEmpty(t, v)
}

// ---------------------------------------------------------------------------
// renderDimensionsClause — additional edge
// ---------------------------------------------------------------------------

func TestRenderDimensionsClause_NoDimensions(t *testing.T) {
	t.Parallel()
	result := renderDimensionsClause(nil)
	require.Contains(t, result, "No custom Dimensions configured")
}

// ---------------------------------------------------------------------------
// promptFingerprint — determinism
// ---------------------------------------------------------------------------

func TestPromptFingerprint_Deterministic(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{TenantID: "t1"}
	a := promptFingerprint("{{content}}", cfg, "en", "sha256:abc")
	b := promptFingerprint("{{content}}", cfg, "en", "sha256:abc")
	require.Equal(t, a, b)
}

func TestPromptFingerprint_DifferentContent(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{TenantID: "t1"}
	a := promptFingerprint("{{content}} v1", cfg, "en", "sha256:abc")
	b := promptFingerprint("{{content}} v2", cfg, "en", "sha256:abc")
	require.NotEqual(t, a, b)
}

// ---------------------------------------------------------------------------
// fingerprintJSON — error path
// ---------------------------------------------------------------------------

func TestFingerprintJSON_UnmarshalableValue(t *testing.T) {
	t.Parallel()
	// channels are not JSON-marshalable.
	ch := make(chan int)
	result := fingerprintJSON(ch)
	require.True(t, strings.HasPrefix(result, "sha256:"))
}

// ---------------------------------------------------------------------------
// dimPropertySchema
// ---------------------------------------------------------------------------

func TestDimPropertySchema_Single_WithTaxonomy(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{
		Name:     "severity",
		Kind:     domain.DimSingle,
		Required: true,
		Taxonomy: []domain.Taxonomy{
			{Value: "low"}, {Value: "medium"}, {Value: "high"},
		},
	}
	schema := dimPropertySchema(d)
	require.Equal(t, "string", schema["type"])
	enum := schema["enum"].([]any)
	require.Contains(t, enum, "low")
	require.Contains(t, enum, "medium")
	require.Contains(t, enum, "high")
}

func TestDimPropertySchema_Single_Optional(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{
		Name:     "priority",
		Kind:     domain.DimSingle,
		Required: false,
		Taxonomy: []domain.Taxonomy{{Value: "p0"}, {Value: "p1"}},
	}
	schema := dimPropertySchema(d)
	typeField := schema["type"].([]string)
	require.Contains(t, typeField, "string")
	require.Contains(t, typeField, "null")
	enum := schema["enum"].([]any)
	require.Contains(t, enum, "p0")
	require.Contains(t, enum, nil) // null included for optional
}

func TestDimPropertySchema_Multi_WithTaxonomy(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{
		Name:     "tags",
		Kind:     domain.DimMulti,
		Taxonomy: []domain.Taxonomy{{Value: "a"}, {Value: "b"}},
	}
	schema := dimPropertySchema(d)
	require.Equal(t, "array", schema["type"])
	items := schema["items"].(map[string]any)
	require.Equal(t, "string", items["type"])
	enum := items["enum"].([]any)
	require.Contains(t, enum, "a")
}

func TestDimPropertySchema_Multi_Freeform(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{
		Name: "notes",
		Kind: domain.DimMulti,
	}
	schema := dimPropertySchema(d)
	require.Equal(t, "array", schema["type"])
	items := schema["items"].(map[string]any)
	require.Equal(t, "string", items["type"])
	_, hasEnum := items["enum"]
	require.False(t, hasEnum, "freeform multi should not have enum")
}

func TestDimPropertySchema_Single_Freeform(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{
		Name:     "comment",
		Kind:     domain.DimSingle,
		Required: true,
	}
	schema := dimPropertySchema(d)
	require.Equal(t, "string", schema["type"])
	_, hasEnum := schema["enum"]
	require.False(t, hasEnum, "freeform single should not have enum")
}

func TestDimPropertySchema_UnknownKind(t *testing.T) {
	t.Parallel()
	d := domain.Dimension{
		Name: "x",
		Kind: "bogus",
	}
	schema := dimPropertySchema(d)
	require.Empty(t, schema)
}

// ---------------------------------------------------------------------------
// promptPolicyMap — error path with non-marshalable struct
// ---------------------------------------------------------------------------

func TestPromptPolicyMap_Empty(t *testing.T) {
	t.Parallel()
	m := promptPolicyMap(PromptPolicyMetadata{})
	require.NotNil(t, m)
	require.Equal(t, "", m["policy_id"])
}

// ---------------------------------------------------------------------------
// normalizeEnrichedDisplay — same language fills display from primary
// ---------------------------------------------------------------------------

func TestNormalizeEnrichedDisplay_SameLanguageFillsDisplay(t *testing.T) {
	t.Parallel()
	e := domain.Enriched{
		Title:     "English Title",
		Rationale: "English Rationale",
	}
	result := normalizeEnrichedDisplay(e, LanguageEnglish, "en")
	require.Equal(t, "English Title", result.DisplayTitle)
	require.Equal(t, "English Rationale", result.DisplayRationale)
}

// ---------------------------------------------------------------------------
// detectRowLanguage
// ---------------------------------------------------------------------------

func TestDetectRowLanguage_DelegatesToDetectLanguage(t *testing.T) {
	t.Parallel()
	lang := detectRowLanguage("Hello world")
	require.Equal(t, LanguageEnglish, lang)
}

// ---------------------------------------------------------------------------
// attrsSizeBytes — error and nil
// ---------------------------------------------------------------------------

func TestAttrsSizeBytes_Nil(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, attrsSizeBytes(nil))
}

func TestAttrsSizeBytes_Empty(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2, attrsSizeBytes(map[string]any{})) // "{}"
}

// ---------------------------------------------------------------------------
// Stubs for Runner tests
// ---------------------------------------------------------------------------

type stubLister struct{}

func (s *stubLister) ListPending(_ context.Context, _ int) ([]int64, error) {
	return nil, nil
}

type stubExecutor struct{}

func (s *stubExecutor) EnrichOne(_ context.Context, _ int64) error {
	return nil
}

type capturingEnrichLLM struct {
	text    string
	err     error
	lastReq llmclient.CompletionRequest
}

func (c *capturingEnrichLLM) Complete(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	c.lastReq = req
	if c.err != nil {
		return llmclient.CompletionResponse{
			Route: llmclient.RouteMetadata{LogicalModel: "fallback"},
		}, c.err
	}
	return llmclient.CompletionResponse{Text: c.text}, nil
}

func (c *capturingEnrichLLM) Close() error {
	return nil
}
