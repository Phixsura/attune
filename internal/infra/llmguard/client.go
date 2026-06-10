package llmguard

import (
	"context"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrBlocked = errors.New("llm guard blocked request")

type Guard interface {
	Name() string
	Apply(ctx context.Context, input GuardInput) (GuardResult, error)
}

type GuardInput struct {
	Stage Stage
	Text  string
	Rules []Rule
	Meta  llmclient.GuardMetadata
}

type GuardResult struct {
	Text     string
	Blocked  bool
	Findings []Finding
}

type Finding struct {
	Guard  string
	Entity string
	Action Action
	Count  int
}

type Client struct {
	next     llmclient.LLMClient
	resolver PolicyResolver
	guards   map[string]Guard
}

func NewClient(next llmclient.LLMClient, resolver PolicyResolver, guards ...Guard) *Client {
	c := ptrext.Of(Client{next: next, resolver: resolver, guards: map[string]Guard{}})
	for _, g := range guards {
		c.guards[g.Name()] = g
	}
	if _, ok := c.guards["pii"]; !ok {
		pii := NewPIIGuard()
		c.guards[pii.Name()] = pii
	}
	return c
}

func (c *Client) Complete(ctx context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	if c == nil || c.next == nil {
		return llmclient.CompletionResponse{}, fmt.Errorf("llmguard: nil client")
	}
	plan, err := c.resolve(ctx, req.Guard)
	if err != nil {
		return llmclient.CompletionResponse{}, err
	}
	prompt, err := c.applyStage(ctx, StageLLMInput, req.Prompt, plan, req.Guard)
	if err != nil {
		return llmclient.CompletionResponse{}, err
	}
	req.Prompt = prompt
	resp, err := c.next.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	text, err := c.applyStage(ctx, StageLLMOutput, resp.Text, plan, req.Guard)
	if err != nil {
		return resp, err
	}
	resp.Text = text
	return resp, nil
}

func (c *Client) Close() error {
	if c == nil || c.next == nil {
		return nil
	}
	return c.next.Close()
}

func (c *Client) resolve(ctx context.Context, meta llmclient.GuardMetadata) (Plan, error) {
	if c.resolver == nil {
		return Plan{}, nil
	}
	return c.resolver.Resolve(ctx, meta)
}

func (c *Client) applyStage(
	ctx context.Context, stage Stage, text string, plan Plan, meta llmclient.GuardMetadata,
) (string, error) {
	rules := RulesForStage(plan, stage)
	if len(rules) == 0 {
		return text, nil
	}
	out := text
	for _, group := range rulesByGuard(rules) {
		g := c.guards[group.guard]
		if g == nil {
			continue
		}
		result, err := g.Apply(ctx, GuardInput{Stage: stage, Text: out, Rules: group.rules, Meta: meta})
		recordFindings(meta, stage, result.Findings)
		if err != nil {
			return "", err
		}
		if result.Blocked {
			recordBlocked(meta, stage, group.guard)
			return "", fmt.Errorf("%w: guard=%s stage=%s", ErrBlocked, group.guard, stage)
		}
		out = result.Text
	}
	return out, nil
}

type guardRuleGroup struct {
	guard string
	rules []Rule
}

func rulesByGuard(rules []Rule) []guardRuleGroup {
	byGuard := map[string][]Rule{}
	order := make([]string, 0, len(rules))
	for _, r := range rules {
		if _, ok := byGuard[r.Guard]; !ok {
			order = append(order, r.Guard)
		}
		byGuard[r.Guard] = append(byGuard[r.Guard], r)
	}
	out := make([]guardRuleGroup, 0, len(order))
	for _, name := range order {
		out = append(out, guardRuleGroup{guard: name, rules: byGuard[name]})
	}
	return out
}

func recordFindings(meta llmclient.GuardMetadata, stage Stage, findings []Finding) {
	tenant := labelOrUnknown(meta.TenantID)
	for _, f := range findings {
		metrics.GuardActionsTotal.WithLabelValues(
			tenant, string(stage), f.Guard, f.Entity, string(f.Action),
		).Add(float64(f.Count))
	}
}

func recordBlocked(meta llmclient.GuardMetadata, stage Stage, guard string) {
	metrics.GuardBlockedTotal.WithLabelValues(
		labelOrUnknown(meta.TenantID), string(stage), guard, "guard_block",
	).Inc()
}

func labelOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
