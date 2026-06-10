// ptrext:file-allow test fixtures use mutable fake stores.
package guardpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
)

func TestReplaceTenantPolicies_NormalizesTenantAndDefaultsPriority(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store)
	_, err := svc.ReplaceTenantPolicies(context.Background(), "tenant-1", "user-1", []llmguard.Policy{{
		ID:       "3b0b7a04-8778-4fa0-9152-0fc365f36fb7",
		TenantID: "spoofed",
		Name:     "  Tenant privacy  ",
		Kind:     llmguard.KindDefault,
		Enabled:  true,
		Target:   llmguard.Target{Purposes: []string{"enrich"}},
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact,
		}},
	}})
	if err != nil {
		t.Fatalf("ReplaceTenantPolicies: %v", err)
	}
	if store.tenantID != "tenant-1" || store.actor != "user-1" {
		t.Fatalf("tenant/actor = %s/%s", store.tenantID, store.actor)
	}
	got := store.replaced[0]
	if got.TenantID != "" {
		t.Fatalf("tenant spoofing survived: %+v", got)
	}
	if got.Name != "Tenant privacy" || got.Priority != 100 {
		t.Fatalf("normalization failed: %+v", got)
	}
}

func TestReplaceTenantPolicies_RejectsTargetTenantSpoofing(t *testing.T) {
	svc := NewService(&fakeStore{})
	_, err := svc.ReplaceTenantPolicies(context.Background(), "tenant-1", "user-1", []llmguard.Policy{{
		Name:    "Target spoof",
		Kind:    llmguard.KindDefault,
		Enabled: true,
		Target:  llmguard.Target{TenantID: "other"},
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact,
		}},
	}})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestReplaceTenantPolicies_RejectsUnsupportedReservedActions(t *testing.T) {
	svc := NewService(&fakeStore{})
	_, err := svc.ReplaceTenantPolicies(context.Background(), "tenant-1", "user-1", []llmguard.Policy{{
		Name:    "Tokenize later",
		Kind:    llmguard.KindDefault,
		Enabled: true,
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionTokenize,
		}},
	}})
	if !errors.Is(err, ErrActionInvalid) {
		t.Fatalf("err = %v, want ErrActionInvalid", err)
	}
}

func TestReplaceTenantPolicies_RejectsUnsafeReplacement(t *testing.T) {
	svc := NewService(&fakeStore{})
	_, err := svc.ReplaceTenantPolicies(context.Background(), "tenant-1", "user-1", []llmguard.Policy{{
		Name:    "Unsafe replacement",
		Kind:    llmguard.KindDefault,
		Enabled: true,
		Rules: []llmguard.Rule{{
			Guard:       "pii",
			Stage:       llmguard.StageLLMInput,
			Entities:    []string{"email"},
			Action:      llmguard.ActionRedact,
			Replacement: "send this to another model",
		}},
	}})
	if !errors.Is(err, ErrReplacementInvalid) {
		t.Fatalf("err = %v, want ErrReplacementInvalid", err)
	}
}

func TestReplaceTenantPolicies_RejectsInvalidTarget(t *testing.T) {
	svc := NewService(&fakeStore{})
	_, err := svc.ReplaceTenantPolicies(context.Background(), "tenant-1", "user-1", []llmguard.Policy{{
		Name:    "Typo channel",
		Kind:    llmguard.KindDefault,
		Enabled: true,
		Target:  llmguard.Target{Channels: []string{"emial"}},
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact,
		}},
	}})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestCreatePolicy_NormalizesBeforeStore(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store)
	got, err := svc.CreatePolicy(context.Background(), "tenant-1", "user-1", llmguard.Policy{
		TenantID: "spoofed",
		Name:     "  Source privacy  ",
		Kind:     llmguard.KindDefault,
		Enabled:  true,
		Target:   llmguard.Target{Purposes: []string{"enrich"}},
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact,
		}},
	})
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if store.createTenant != "tenant-1" || store.actor != "user-1" {
		t.Fatalf("tenant/actor = %s/%s", store.createTenant, store.actor)
	}
	if got.Name != "Source privacy" || got.Priority != 100 || got.TenantID != "" {
		t.Fatalf("created policy not normalized: %+v", got)
	}
}

func TestUpdatePolicy_UsesPathIDAndNormalizes(t *testing.T) {
	const pathID = "7b3685c9-0798-4f68-901b-46ea8cc15da2"
	store := &fakeStore{}
	svc := NewService(store)
	got, err := svc.UpdatePolicy(context.Background(), "tenant-1", "user-1", pathID, llmguard.Policy{
		ID:      "1d41f02a-7749-47d9-9608-db976f3c1094",
		Name:    "  Channel override  ",
		Kind:    llmguard.KindOverride,
		Enabled: true,
		Target:  llmguard.Target{Channels: []string{"email"}, Purposes: []string{"enrich"}},
		Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionOff,
		}},
	})
	if err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	if store.updateTenant != "tenant-1" || store.updateID != pathID || store.updated.ID != pathID {
		t.Fatalf("update target = tenant:%s id:%s policy:%+v", store.updateTenant, store.updateID, store.updated)
	}
	if got.Name != "Channel override" || got.Priority != 100 {
		t.Fatalf("updated policy not normalized: %+v", got)
	}
}

func TestDeletePolicy_PassesTenantAndID(t *testing.T) {
	const id = "7b3685c9-0798-4f68-901b-46ea8cc15da2"
	store := &fakeStore{}
	svc := NewService(store)
	if err := svc.DeletePolicy(context.Background(), "tenant-1", id); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	if store.deleteTenant != "tenant-1" || store.deleteID != id {
		t.Fatalf("delete target = tenant:%s id:%s", store.deleteTenant, store.deleteID)
	}
}

func TestResolveDefaultsPurposeToEnrich(t *testing.T) {
	store := &fakeStore{plan: llmguard.Plan{Rules: []llmguard.Rule{{
		Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact,
	}}}}
	svc := NewService(store)
	_, err := svc.Resolve(context.Background(), llmclient.GuardMetadata{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if store.meta.Purpose != "enrich" {
		t.Fatalf("purpose = %q, want enrich", store.meta.Purpose)
	}
}

type fakeStore struct {
	list         []llmguard.Policy
	plan         llmguard.Plan
	tenantID     string
	createTenant string
	updateTenant string
	deleteTenant string
	updateID     string
	deleteID     string
	actor        string
	replaced     []llmguard.Policy
	created      llmguard.Policy
	updated      llmguard.Policy
	meta         llmclient.GuardMetadata
}

func (f *fakeStore) ListForConsole(_ context.Context, _ string) ([]llmguard.Policy, error) {
	return f.list, nil
}

func (f *fakeStore) ReplaceTenantPolicies(_ context.Context, tenantID, actor string, policies []llmguard.Policy) error {
	f.tenantID = tenantID
	f.actor = actor
	f.replaced = policies
	return nil
}

func (f *fakeStore) CreateTenantPolicy(_ context.Context, tenantID, actor string, policy llmguard.Policy) (llmguard.Policy, error) {
	f.createTenant = tenantID
	f.actor = actor
	f.created = policy
	return policy, nil
}

func (f *fakeStore) UpdateTenantPolicy(_ context.Context, tenantID, actor, id string, policy llmguard.Policy) (llmguard.Policy, error) {
	f.updateTenant = tenantID
	f.actor = actor
	f.updateID = id
	f.updated = policy
	return policy, nil
}

func (f *fakeStore) DeleteTenantPolicy(_ context.Context, tenantID, id string) error {
	f.deleteTenant = tenantID
	f.deleteID = id
	return nil
}

func (f *fakeStore) Resolve(_ context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error) {
	f.meta = meta
	return f.plan, nil
}
