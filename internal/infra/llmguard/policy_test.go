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
