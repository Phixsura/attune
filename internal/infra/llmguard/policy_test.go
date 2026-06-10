package llmguard

import (
	"testing"

	"github.com/Phixsura/attune/internal/infra/llmclient"
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
