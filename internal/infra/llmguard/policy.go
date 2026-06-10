// Package llmguard applies source-aware guard policies around LLM calls.
//
// It is intentionally provider-neutral: callers wrap any llmclient.LLMClient,
// pass explicit GuardMetadata on CompletionRequest, and this package mutates or
// blocks request/response text according to resolved policies.
package llmguard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrPolicyNotFound = errors.New("guard policy not found")

type Stage string

const (
	StageLLMInput  Stage = "llm_input"
	StageLLMOutput Stage = "llm_output"
	StageOutbound  Stage = "outbound"
	StageToolCall  Stage = "tool_call"
)

type Action string

const (
	ActionOff      Action = "off"
	ActionAudit    Action = "audit"
	ActionRedact   Action = "redact"
	ActionHash     Action = "hash"
	ActionTokenize Action = "tokenize"
	ActionBlock    Action = "block"
)

type PolicyKind string

const (
	KindBaseline PolicyKind = "baseline"
	KindDefault  PolicyKind = "default"
	KindOverride PolicyKind = "override"
)

type Target struct {
	TenantID     string   `json:"tenantId,omitempty"`
	Channels     []string `json:"channels,omitempty"`
	SourceIDs    []string `json:"sourceIds,omitempty"`
	SourceTags   []string `json:"sourceTags,omitempty"`
	Purposes     []string `json:"purposes,omitempty"`
	Stages       []Stage  `json:"stages,omitempty"`
	Environments []string `json:"environments,omitempty"`
}

type Rule struct {
	Guard       string   `json:"guard"`
	Stage       Stage    `json:"stage"`
	Entities    []string `json:"entities,omitempty"`
	Action      Action   `json:"action"`
	Replacement string   `json:"replacement,omitempty"`
}

type Policy struct {
	ID       string
	TenantID string
	Name     string
	Kind     PolicyKind
	Enabled  bool
	Priority int
	Target   Target
	Rules    []Rule
}

type Plan struct {
	Rules []Rule
}

type ruleBucket struct {
	baseline *weightedRule
	def      *weightedRule
	override *weightedRule
}

type weightedRule struct {
	rule        Rule
	specificity int
	priority    int
}

// PolicyResolver returns the effective guard plan for one LLM request.
// Production uses a DB-backed implementation; tests may use StaticResolver.
type PolicyResolver interface {
	Resolve(ctx context.Context, meta llmclient.GuardMetadata) (Plan, error)
}

type StaticResolver struct {
	Plan Plan
	Err  error
}

func (r StaticResolver) Resolve(context.Context, llmclient.GuardMetadata) (Plan, error) {
	return r.Plan, r.Err
}

func ResolvePolicies(policies []Policy, meta llmclient.GuardMetadata) Plan {
	type key struct {
		guard  string
		stage  Stage
		entity string
	}
	buckets := map[key]ruleBucket{}
	for _, p := range policies {
		if !p.Enabled || !targetMatches(p.Target, meta) {
			continue
		}
		for _, r := range p.Rules {
			if !ruleStageMatches(p.Target, r.Stage) {
				continue
			}
			for _, entity := range ruleEntities(r) {
				k := key{guard: r.Guard, stage: r.Stage, entity: entity}
				b := buckets[k]
				candidate := r
				candidate.Entities = []string{entity}
				b = putRule(b, p, candidate)
				buckets[k] = b
			}
		}
	}
	out := make([]Rule, 0, len(buckets))
	for _, b := range buckets {
		if r, ok := effectiveRule(b); ok {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Rule) int {
		return strings.Compare(ruleSortKey(a), ruleSortKey(b))
	})
	return Plan{Rules: out}
}

func targetMatches(t Target, meta llmclient.GuardMetadata) bool {
	return matchesScalar(t.TenantID, meta.TenantID) &&
		matchesAny(t.Channels, meta.Channel) &&
		matchesAny(t.SourceIDs, meta.SourceID) &&
		matchesAllTags(t.SourceTags, meta.SourceTags) &&
		matchesAny(t.Purposes, meta.Purpose) &&
		matchesAny(t.Environments, meta.Environment)
}

func ruleStageMatches(t Target, stage Stage) bool {
	if len(t.Stages) > 0 && !slices.Contains(t.Stages, stage) {
		return false
	}
	return true
}

func matchesScalar(want, got string) bool {
	return want == "" || want == got
}

func matchesAny(wants []string, got string) bool {
	return len(wants) == 0 || slices.Contains(wants, got)
}

func matchesAllTags(wants, got []string) bool {
	for _, want := range wants {
		if !slices.Contains(got, want) {
			return false
		}
	}
	return true
}

func ruleEntities(r Rule) []string {
	if len(r.Entities) == 0 {
		return []string{""}
	}
	return r.Entities
}

func putRule(b ruleBucket, p Policy, r Rule) ruleBucket {
	w := weightedRule{rule: r, specificity: policySpecificity(p), priority: p.Priority}
	switch p.Kind {
	case KindBaseline:
		b.baseline = strongerFloor(b.baseline, w)
	case KindOverride:
		b.override = narrowerRule(b.override, w)
	default:
		b.def = narrowerRule(b.def, w)
	}
	return b
}

func policySpecificity(p Policy) int {
	score := 0
	if p.TenantID != "" {
		score += 10
	}
	if len(p.Target.Channels) > 0 {
		score += 20
	}
	if len(p.Target.SourceTags) > 0 {
		score += 30
	}
	if len(p.Target.SourceIDs) > 0 {
		score += 40
	}
	return score
}

func effectiveRule(b ruleBucket) (Rule, bool) {
	chosen := b.def
	if b.override != nil {
		chosen = b.override
	}
	if b.baseline == nil {
		if chosen == nil {
			return Rule{}, false
		}
		return chosen.rule, true
	}
	if chosen == nil || actionRank(b.baseline.rule.Action) > actionRank(chosen.rule.Action) {
		return b.baseline.rule, true
	}
	return chosen.rule, true
}

func strongerFloor(current *weightedRule, next weightedRule) *weightedRule {
	if current == nil || actionRank(next.rule.Action) > actionRank(current.rule.Action) {
		return ptrext.Of(next)
	}
	if actionRank(next.rule.Action) == actionRank(current.rule.Action) && beatsBySpecificity(next, ptrext.Indirect(current)) {
		return ptrext.Of(next)
	}
	return current
}

func narrowerRule(current *weightedRule, next weightedRule) *weightedRule {
	if current == nil || beatsBySpecificity(next, ptrext.Indirect(current)) {
		return ptrext.Of(next)
	}
	if next.specificity == current.specificity &&
		next.priority == current.priority &&
		actionRank(next.rule.Action) > actionRank(current.rule.Action) {
		return ptrext.Of(next)
	}
	return current
}

func beatsBySpecificity(next, current weightedRule) bool {
	if next.specificity != current.specificity {
		return next.specificity > current.specificity
	}
	return next.priority < current.priority
}

func actionRank(a Action) int {
	switch a {
	case ActionBlock:
		return 5
	case ActionRedact:
		return 4
	case ActionHash, ActionTokenize:
		return 3
	case ActionAudit:
		return 2
	default:
		return 1
	}
}

func ruleSortKey(r Rule) string {
	entity := ""
	if len(r.Entities) > 0 {
		entity = r.Entities[0]
	}
	return fmt.Sprintf("%s/%s/%s", r.Stage, r.Guard, entity)
}

func RulesForStage(plan Plan, stage Stage) []Rule {
	out := make([]Rule, 0, len(plan.Rules))
	for _, r := range plan.Rules {
		if r.Stage == stage {
			out = append(out, r)
		}
	}
	return out
}
