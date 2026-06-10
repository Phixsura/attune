//go:build integration

// ptrext:file-allow integration scans use pointer arguments.
package guardpolicy_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/repo/guardpolicy"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_GuardPolicyReplaceListAndResolve(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool)
	repo := guardpolicy.New(pool)

	initial, err := repo.ListForConsole(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListForConsole initial: %v", err)
	}
	if !hasSystemPrivacyDefault(initial) {
		t.Fatalf("system privacy default missing: %+v", initial)
	}

	if err := repo.ReplaceTenantPolicies(ctx, tenantID, "user-1", []llmguard.Policy{
		{
			Name:     "Tenant regulated baseline",
			Kind:     llmguard.KindBaseline,
			Enabled:  true,
			Priority: 10,
			Target:   llmguard.Target{Purposes: []string{"enrich"}},
			Rules: []llmguard.Rule{{
				Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"credit_card"}, Action: llmguard.ActionBlock,
			}},
		},
		{
			Name:     "Email trusted override",
			Kind:     llmguard.KindOverride,
			Enabled:  true,
			Priority: 20,
			Target:   llmguard.Target{Channels: []string{"email"}, Purposes: []string{"enrich"}},
			Rules: []llmguard.Rule{{
				Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionOff,
			}},
		},
		{
			Name:     "Disabled phone block",
			Kind:     llmguard.KindBaseline,
			Enabled:  false,
			Priority: 30,
			Target:   llmguard.Target{Purposes: []string{"enrich"}},
			Rules: []llmguard.Rule{{
				Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"phone"}, Action: llmguard.ActionBlock,
			}},
		},
	}); err != nil {
		t.Fatalf("ReplaceTenantPolicies: %v", err)
	}

	policies, err := repo.ListForConsole(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListForConsole after replace: %v", err)
	}
	if countTenantPolicies(policies, tenantID) != 3 {
		t.Fatalf("tenant policy count mismatch: %+v", policies)
	}
	assertPolicyVisible(t, policies, "Disabled phone block", false)

	plan, err := repo.Resolve(ctx, llmclient.GuardMetadata{
		TenantID: tenantID,
		Channel:  "email",
		Purpose:  "enrich",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertEntityAction(t, plan, "email", llmguard.ActionOff)
	assertEntityAction(t, plan, "credit_card", llmguard.ActionBlock)
	assertEntityAction(t, plan, "cn_id", llmguard.ActionRedact)
	assertEntityAction(t, plan, "phone", llmguard.ActionRedact)
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES ('guard-policy', 'Guard Policy')
		RETURNING id`,
	).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func hasSystemPrivacyDefault(policies []llmguard.Policy) bool {
	for _, p := range policies {
		if p.TenantID == "" && p.Name == "System privacy default" && p.Kind == llmguard.KindDefault {
			return true
		}
	}
	return false
}

func countTenantPolicies(policies []llmguard.Policy, tenantID string) int {
	n := 0
	for _, p := range policies {
		if p.TenantID == tenantID {
			n++
		}
	}
	return n
}

func assertPolicyVisible(t *testing.T, policies []llmguard.Policy, name string, enabled bool) {
	t.Helper()
	for _, p := range policies {
		if p.Name == name {
			if p.Enabled != enabled {
				t.Fatalf("policy %q enabled = %t, want %t", name, p.Enabled, enabled)
			}
			return
		}
	}
	t.Fatalf("policy %q missing from %+v", name, policies)
}

func assertEntityAction(t *testing.T, plan llmguard.Plan, entity string, want llmguard.Action) {
	t.Helper()
	for _, r := range plan.Rules {
		if len(r.Entities) > 0 && r.Entities[0] == entity {
			if r.Action != want {
				t.Fatalf("entity %s action = %s, want %s", entity, r.Action, want)
			}
			return
		}
	}
	t.Fatalf("entity %s missing in %+v", entity, plan.Rules)
}
