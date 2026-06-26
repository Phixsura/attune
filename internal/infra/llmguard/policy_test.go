package llmguard

import (
	"testing"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestResolvePolicies_BaselineCannotBeRelaxed(t *testing.T) {
	meta := llmclient.GuardMetadata{TenantID: "t1", Channel: "email", Purpose: "enrich"}
	plan := ResolvePolicies([]Policy{
		policy(KindBaseline, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionBlock}),
		policy(KindOverride, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionOff}),
	}, meta)
	assertRuleAction(t, plan, "credit_card", ActionBlock)
}

func TestResolvePolicies_SourceOverrideCanRelaxDefault(t *testing.T) {
	meta := llmclient.GuardMetadata{TenantID: "t1", Channel: "email", SourceID: "src-1", Purpose: "enrich"}
	plan := ResolvePolicies([]Policy{
		policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact}),
		sourcePolicy("src-1", KindOverride, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionOff}),
	}, meta)
	assertRuleAction(t, plan, "email", ActionOff)
}

func TestResolvePolicies_TenantDefaultCanRelaxSystemDefault(t *testing.T) {
	meta := llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"}
	systemDefault := policy(KindDefault, Rule{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact,
	})
	tenantDefault := policy(KindDefault, Rule{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionOff,
	})
	tenantDefault.TenantID = "t1"
	plan := ResolvePolicies([]Policy{systemDefault, tenantDefault}, meta)
	assertRuleAction(t, plan, "email", ActionOff)
}

func TestResolvePolicies_ChannelDefaultCanRelaxTenantDefault(t *testing.T) {
	meta := llmclient.GuardMetadata{TenantID: "t1", Channel: "email", Purpose: "enrich"}
	tenantDefault := policy(KindDefault, Rule{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact,
	})
	tenantDefault.TenantID = "t1"
	channelDefault := policy(KindDefault, Rule{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionAudit,
	})
	channelDefault.TenantID = "t1"
	channelDefault.Target.Channels = []string{"email"}
	plan := ResolvePolicies([]Policy{tenantDefault, channelDefault}, meta)
	assertRuleAction(t, plan, "email", ActionAudit)
}

func TestResolvePolicies_TargetsChannelAndTags(t *testing.T) {
	meta := llmclient.GuardMetadata{
		TenantID:   "t1",
		Channel:    "webhook",
		SourceTags: []string{"public", "regulated"},
		Purpose:    "enrich",
	}
	p := policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"phone"}, Action: ActionAudit})
	p.Target = Target{Channels: []string{"webhook"}, SourceTags: []string{"regulated"}}
	plan := ResolvePolicies([]Policy{p}, meta)
	assertRuleAction(t, plan, "phone", ActionAudit)
}

func policy(kind PolicyKind, rule Rule) Policy {
	return Policy{Kind: kind, Enabled: true, Target: Target{Purposes: []string{"enrich"}}, Rules: []Rule{rule}}
}

func sourcePolicy(sourceID string, kind PolicyKind, rule Rule) Policy {
	p := policy(kind, rule)
	p.Target.SourceIDs = []string{sourceID}
	return p
}

func TestResolvePolicies_DisabledPolicySkipped(t *testing.T) {
	t.Parallel()
	meta := llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"}
	p := policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionBlock})
	p.Enabled = false
	plan := ResolvePolicies([]Policy{p}, meta)
	if len(plan.Rules) != 0 {
		t.Fatalf("disabled policy should produce no rules, got %+v", plan.Rules)
	}
}

func TestResolvePolicies_PurposeMismatchSkipped(t *testing.T) {
	t.Parallel()
	meta := llmclient.GuardMetadata{TenantID: "t1", Purpose: "reply_draft"}
	p := policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionBlock})
	plan := ResolvePolicies([]Policy{p}, meta)
	if len(plan.Rules) != 0 {
		t.Fatalf("purpose mismatch should produce no rules, got %+v", plan.Rules)
	}
}

func TestResolvePolicies_RulesForStageFilters(t *testing.T) {
	t.Parallel()
	meta := llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"}
	plan := ResolvePolicies([]Policy{
		policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact}),
		policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMOutput, Entities: []string{"phone"}, Action: ActionRedact}),
	}, meta)
	inputRules := RulesForStage(plan, StageLLMInput)
	if len(inputRules) != 1 || inputRules[0].Entities[0] != "email" {
		t.Fatalf("expected only email rule for input stage, got %+v", inputRules)
	}
	outputRules := RulesForStage(plan, StageLLMOutput)
	if len(outputRules) != 1 || outputRules[0].Entities[0] != "phone" {
		t.Fatalf("expected only phone rule for output stage, got %+v", outputRules)
	}
}

func TestActionRank_AllValues(t *testing.T) {
	t.Parallel()
	if actionRank(ActionBlock) <= actionRank(ActionRedact) {
		t.Fatal("block should outrank redact")
	}
	if actionRank(ActionRedact) <= actionRank(ActionHash) {
		t.Fatal("redact should outrank hash")
	}
	if actionRank(ActionHash) <= actionRank(ActionAudit) {
		t.Fatal("hash should outrank audit")
	}
	if actionRank(ActionAudit) <= actionRank(ActionOff) {
		t.Fatal("audit should outrank off")
	}
	if actionRank(ActionHash) != actionRank(ActionTokenize) {
		t.Fatal("hash and tokenize should be equal rank")
	}
}

func TestRuleSortKey(t *testing.T) {
	t.Parallel()
	r := Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}}
	key := ruleSortKey(r)
	if key != "llm_input/pii/email" {
		t.Fatalf("expected 'llm_input/pii/email', got %q", key)
	}

	r2 := Rule{Guard: "pii", Stage: StageLLMOutput}
	key2 := ruleSortKey(r2)
	if key2 != "llm_output/pii/" {
		t.Fatalf("expected 'llm_output/pii/', got %q", key2)
	}
}

func TestStrongerFloor(t *testing.T) {
	t.Parallel()
	block := weightedRule{rule: Rule{Action: ActionBlock}, specificity: 1, priority: 1}
	redact := weightedRule{rule: Rule{Action: ActionRedact}, specificity: 2, priority: 1}

	result := strongerFloor(nil, redact)
	if result.rule.Action != ActionRedact {
		t.Fatal("nil current should accept any")
	}

	result = strongerFloor(result, block)
	if result.rule.Action != ActionBlock {
		t.Fatal("stronger action should win")
	}

	// Same action rank, more specific wins
	auditLow := weightedRule{rule: Rule{Action: ActionAudit}, specificity: 1, priority: 1}
	auditHigh := weightedRule{rule: Rule{Action: ActionAudit}, specificity: 2, priority: 1}
	result = strongerFloor(ptrext.Of(auditLow), auditHigh)
	if result.specificity != 2 {
		t.Fatal("higher specificity should win at same action rank")
	}
}

func TestNarrowerRule(t *testing.T) {
	t.Parallel()
	low := weightedRule{rule: Rule{Action: ActionAudit}, specificity: 1, priority: 1}
	high := weightedRule{rule: Rule{Action: ActionAudit}, specificity: 2, priority: 1}

	result := narrowerRule(nil, low)
	if result == nil {
		t.Fatal("nil current should accept any")
	}

	result = narrowerRule(result, high)
	if result.specificity != 2 {
		t.Fatal("more specific should win")
	}

	// Same specificity and priority but stronger action
	sameSpecBlock := weightedRule{rule: Rule{Action: ActionBlock}, specificity: 2, priority: 1}
	result = narrowerRule(result, sameSpecBlock)
	if result.rule.Action != ActionBlock {
		t.Fatal("same spec+priority but stronger action should win")
	}
}

func TestBeatsBySpecificity(t *testing.T) {
	t.Parallel()
	a := weightedRule{specificity: 1, priority: 1}
	b := weightedRule{specificity: 2, priority: 1}
	if !beatsBySpecificity(b, a) {
		t.Fatal("higher specificity should beat lower")
	}
	if beatsBySpecificity(a, b) {
		t.Fatal("lower specificity should not beat higher")
	}

	c := weightedRule{specificity: 1, priority: 2}
	d := weightedRule{specificity: 1, priority: 1}
	if !beatsBySpecificity(d, c) {
		t.Fatal("lower priority value should beat higher at same specificity")
	}
}

func TestResolvePolicies_ChannelMismatchSkipped(t *testing.T) {
	t.Parallel()
	meta := llmclient.GuardMetadata{TenantID: "t1", Channel: "api", Purpose: "enrich"}
	p := policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionBlock})
	p.Target.Channels = []string{"webhook"}
	plan := ResolvePolicies([]Policy{p}, meta)
	if len(plan.Rules) != 0 {
		t.Fatalf("channel mismatch should produce no rules, got %+v", plan.Rules)
	}
}

func TestResolvePolicies_TagMismatchSkipped(t *testing.T) {
	t.Parallel()
	meta := llmclient.GuardMetadata{TenantID: "t1", SourceTags: []string{"internal"}, Purpose: "enrich"}
	p := policy(KindDefault, Rule{Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionBlock})
	p.Target.SourceTags = []string{"external"}
	plan := ResolvePolicies([]Policy{p}, meta)
	if len(plan.Rules) != 0 {
		t.Fatalf("tag mismatch should produce no rules, got %+v", plan.Rules)
	}
}

func TestRuleStageMatches(t *testing.T) {
	t.Parallel()
	if !ruleStageMatches(Target{}, StageLLMInput) {
		t.Fatal("empty stages should match any stage")
	}
	if !ruleStageMatches(Target{Stages: []Stage{StageLLMInput}}, StageLLMInput) {
		t.Fatal("matching stage should pass")
	}
	if ruleStageMatches(Target{Stages: []Stage{StageLLMInput}}, StageLLMOutput) {
		t.Fatal("non-matching stage should fail")
	}
}

func TestRuleEntities_Empty(t *testing.T) {
	t.Parallel()
	got := ruleEntities(Rule{})
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("empty entities should return [\"\"], got %v", got)
	}
}

func TestRuleEntities_NonEmpty(t *testing.T) {
	t.Parallel()
	got := ruleEntities(Rule{Entities: []string{"email", "phone"}})
	if len(got) != 2 {
		t.Fatalf("should preserve, got %v", got)
	}
}

func TestEffectiveRule_NoRules(t *testing.T) {
	t.Parallel()
	_, ok := effectiveRule(ruleBucket{})
	if ok {
		t.Fatal("empty bucket should return false")
	}
}

func TestEffectiveRule_OverrideTakesPrecedence(t *testing.T) {
	t.Parallel()
	def := ptrext.Of(weightedRule{rule: Rule{Action: ActionRedact}})
	override := ptrext.Of(weightedRule{rule: Rule{Action: ActionOff}})
	r, ok := effectiveRule(ruleBucket{def: def, override: override})
	if !ok {
		t.Fatal("should find rule")
	}
	if r.Action != ActionOff {
		t.Fatalf("override should win, got %s", r.Action)
	}
}

func TestStrongerFloor_Nil(t *testing.T) {
	t.Parallel()
	next := weightedRule{rule: Rule{Action: ActionBlock}}
	got := strongerFloor(nil, next)
	if got.rule.Action != ActionBlock {
		t.Fatal("nil current should accept next")
	}
}

func TestNarrowerRule_Nil(t *testing.T) {
	t.Parallel()
	next := weightedRule{rule: Rule{Action: ActionRedact}}
	got := narrowerRule(nil, next)
	if got.rule.Action != ActionRedact {
		t.Fatal("nil current should accept next")
	}
}

func TestPolicySpecificity(t *testing.T) {
	t.Parallel()
	p := Policy{TenantID: "t1", Target: Target{Channels: []string{"email"}, SourceIDs: []string{"s1"}, SourceTags: []string{"x"}}}
	score := policySpecificity(p)
	if score != 100 {
		t.Fatalf("expected 10+20+30+40=100, got %d", score)
	}
	if policySpecificity(Policy{}) != 0 {
		t.Fatal("empty policy should have 0 specificity")
	}
}

func TestStrongerFloor_SameRankHigherSpecificity(t *testing.T) {
	t.Parallel()
	current := ptrext.Of(weightedRule{rule: Rule{Action: ActionBlock}, specificity: 10, priority: 1})
	next := weightedRule{rule: Rule{Action: ActionBlock}, specificity: 20, priority: 1}
	got := strongerFloor(current, next)
	if got.specificity != 20 {
		t.Fatalf("same action rank but higher specificity should win: got %d", got.specificity)
	}
}

func TestStrongerFloor_WeakerRank(t *testing.T) {
	t.Parallel()
	current := ptrext.Of(weightedRule{rule: Rule{Action: ActionBlock}, specificity: 10})
	next := weightedRule{rule: Rule{Action: ActionRedact}, specificity: 20}
	got := strongerFloor(current, next)
	if got.rule.Action != ActionBlock {
		t.Fatal("weaker action rank should not replace stronger")
	}
}

func TestNarrowerRule_SameSpecificityStrongerAction(t *testing.T) {
	t.Parallel()
	current := ptrext.Of(weightedRule{rule: Rule{Action: ActionRedact}, specificity: 10, priority: 1})
	next := weightedRule{rule: Rule{Action: ActionBlock}, specificity: 10, priority: 1}
	got := narrowerRule(current, next)
	if got.rule.Action != ActionBlock {
		t.Fatal("same specificity/priority but stronger action should win")
	}
}

func TestNarrowerRule_WeakerSpecificity(t *testing.T) {
	t.Parallel()
	current := ptrext.Of(weightedRule{rule: Rule{Action: ActionBlock}, specificity: 20, priority: 1})
	next := weightedRule{rule: Rule{Action: ActionRedact}, specificity: 10, priority: 1}
	got := narrowerRule(current, next)
	if got.specificity != 20 {
		t.Fatal("lower specificity should not replace higher")
	}
}

func TestEffectiveRule_NoBaseline_NoOverride_NoDefault(t *testing.T) {
	t.Parallel()
	_, ok := effectiveRule(ruleBucket{})
	if ok {
		t.Fatal("empty bucket should produce no rule")
	}
}

func TestEffectiveRule_BaselineStrongerThanOverride(t *testing.T) {
	t.Parallel()
	r, ok := effectiveRule(ruleBucket{
		baseline: ptrext.Of(weightedRule{rule: Rule{Action: ActionBlock}}),
		override: ptrext.Of(weightedRule{rule: Rule{Action: ActionRedact}}),
	})
	if !ok {
		t.Fatal("expected rule")
	}
	if r.Action != ActionBlock {
		t.Fatalf("baseline should win when stronger: got %s", r.Action)
	}
}

func TestEffectiveRule_OverrideWhenNoBaseline(t *testing.T) {
	t.Parallel()
	r, ok := effectiveRule(ruleBucket{
		override: ptrext.Of(weightedRule{rule: Rule{Action: ActionRedact}}),
	})
	if !ok {
		t.Fatal("expected rule")
	}
	if r.Action != ActionRedact {
		t.Fatalf("override should be used when no baseline: got %s", r.Action)
	}
}

func TestEffectiveRule_DefaultWhenNoBaselineNoOverride(t *testing.T) {
	t.Parallel()
	r, ok := effectiveRule(ruleBucket{
		def: ptrext.Of(weightedRule{rule: Rule{Action: ActionRedact}}),
	})
	if !ok {
		t.Fatal("expected rule")
	}
	if r.Action != ActionRedact {
		t.Fatalf("default should be used: got %s", r.Action)
	}
}

func assertRuleAction(t *testing.T, plan Plan, entity string, want Action) {
	t.Helper()
	for _, r := range plan.Rules {
		if len(r.Entities) > 0 && r.Entities[0] == entity {
			if r.Action != want {
				t.Fatalf("%s action: got %s want %s", entity, r.Action, want)
			}
			return
		}
	}
	t.Fatalf("missing rule for %s in %+v", entity, plan.Rules)
}
