// ptrext:file-allow test fixtures use mutable fake stores.
package guardpolicy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestReplaceTenantPolicies_NormalizesTenantAndDefaultsPriority(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, nil)
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
	svc := NewService(&fakeStore{}, nil)
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
	svc := NewService(&fakeStore{}, nil)
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
	svc := NewService(&fakeStore{}, nil)
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
	svc := NewService(&fakeStore{}, nil)
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

// TestValidateTarget_UnknownChannelSurfacesValidList verifies the channel
// branch surfaces the offending value + the valid set (registry-driven), so an
// operator who typos a channel sees what to fix — without masking the target's
// other failure causes, which keep the bare sentinel.
func TestValidateTarget_UnknownChannelSurfacesValidList(t *testing.T) {
	svc := NewService(&fakeStore{}, nil)
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
	msg := ErrToMessage(err)
	if !strings.Contains(msg, `"emial"`) || !strings.Contains(msg, "valid:") {
		t.Errorf("ErrToMessage = %q; want it to name the offending value and the valid set", msg)
	}
	// A non-channel target failure must keep the opaque sentinel message.
	_, err = svc.ReplaceTenantPolicies(context.Background(), "tenant-1", "user-1", []llmguard.Policy{{
		Name: "Tenant spoof", Kind: llmguard.KindDefault, Enabled: true,
		Target: llmguard.Target{TenantID: "other"},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	}})
	if got := ErrToMessage(err); got != "guard policy target is invalid" {
		t.Errorf("non-channel target failure message = %q; want the bare sentinel (no masking)", got)
	}
}

func TestCreatePolicy_NormalizesBeforeStore(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, nil)
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
	svc := NewService(store, nil)
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
	svc := NewService(store, nil)
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
	svc := NewService(store, nil)
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

func TestValidKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind llmguard.PolicyKind
		want bool
	}{
		{llmguard.KindBaseline, true},
		{llmguard.KindDefault, true},
		{llmguard.KindOverride, true},
		{"", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := validKind(tt.kind); got != tt.want {
				t.Errorf("validKind(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestValidStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stage llmguard.Stage
		want  bool
	}{
		{llmguard.StageLLMInput, true},
		{llmguard.StageLLMOutput, true},
		{llmguard.StageOutbound, true},
		{llmguard.StageToolCall, true},
		{"", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			if got := validStage(tt.stage); got != tt.want {
				t.Errorf("validStage(%q) = %v, want %v", tt.stage, got, tt.want)
			}
		})
	}
}

func TestValidAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action llmguard.Action
		want   bool
	}{
		{llmguard.ActionOff, true},
		{llmguard.ActionAudit, true},
		{llmguard.ActionRedact, true},
		{llmguard.ActionBlock, true},
		{llmguard.ActionTokenize, false},
		{"", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			if got := validAction(tt.action); got != tt.want {
				t.Errorf("validAction(%q) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}

func TestValidPIIEntity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entity string
		want   bool
	}{
		{"email", true},
		{"phone", true},
		{"cn_mobile", true},
		{"cn_id", true},
		{"credit_card", true},
		{"", false},
		{"invalid", false},
		{"ssn", false},
	}

	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			if got := validPIIEntity(tt.entity); got != tt.want {
				t.Errorf("validPIIEntity(%q) = %v, want %v", tt.entity, got, tt.want)
			}
		})
	}
}

func TestValidReplacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		replacement string
		want        bool
	}{
		{"valid template", "{type}:{entity}", true},
		{"valid simple", "REDACTED", true},
		{"with colon", "PII:email", true},
		{"too long", string(make([]byte, MaxReplacementBytes+1)), false},
		{"invalid chars", "hello world", false},
		{"newline", "hello\nworld", false},
		{"empty fails regex", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validReplacement(tt.replacement); got != tt.want {
				t.Errorf("validReplacement(%q) = %v, want %v", tt.replacement, got, tt.want)
			}
		})
	}
}

func TestValidPurposeList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		purposes []string
		want     bool
	}{
		{"valid single", []string{"enrich"}, true},
		{"valid multiple", []string{"enrich", "digest", "reply_draft"}, true},
		{"all valid", []string{"enrich", "digest", "reply_draft", "eval", "outbound"}, true},
		{"empty list", []string{}, true},
		{"invalid purpose", []string{"invalid"}, false},
		{"mixed valid invalid", []string{"enrich", "invalid"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validPurposeList(tt.purposes); got != tt.want {
				t.Errorf("validPurposeList(%v) = %v, want %v", tt.purposes, got, tt.want)
			}
		})
	}
}

func TestTrimStringSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"no trim needed", []string{"a", "b"}, []string{"a", "b"}},
		{"trim needed", []string{"  a  ", "  b  "}, []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimStringSlice(tt.in)
			if len(got) != len(tt.want) {
				t.Errorf("trimStringSlice(%v) = %v, want %v", tt.in, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("trimStringSlice(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestErrToCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want attunev1.ErrorCode
	}{
		{ErrPolicyNameRequired, attunev1.ErrorCode_VALIDATION},
		{ErrPolicyNameTooLong, attunev1.ErrorCode_VALIDATION},
		{ErrPolicyLimit, attunev1.ErrorCode_VALIDATION},
		{ErrRuleLimit, attunev1.ErrorCode_VALIDATION},
		{ErrKindInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrGuardInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrStageInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrActionInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrEntityInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrTargetInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrReplacementInvalid, attunev1.ErrorCode_VALIDATION},
		{ErrPriorityInvalid, attunev1.ErrorCode_VALIDATION},
		{errors.New("other"), attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			if got := ErrToCode(tt.err); got != tt.want {
				t.Errorf("ErrToCode(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestErrToMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err          error
		wantNonEmpty bool
	}{
		{ErrPolicyNameRequired, true},
		{ErrPolicyNameTooLong, true},
		{ErrPolicyLimit, true},
		{ErrRuleLimit, true},
		{ErrKindInvalid, true},
		{ErrGuardInvalid, true},
		{ErrStageInvalid, true},
		{ErrActionInvalid, true},
		{ErrEntityInvalid, true},
		{ErrTargetInvalid, true},
		{ErrReplacementInvalid, true},
		{ErrPriorityInvalid, true},
		{errors.New("other"), false},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			got := ErrToMessage(tt.err)
			if tt.wantNonEmpty && got == "" {
				t.Errorf("ErrToMessage(%v) = empty, want non-empty", tt.err)
			}
			if !tt.wantNonEmpty && got != "" {
				t.Errorf("ErrToMessage(%v) = %q, want empty", tt.err, got)
			}
		})
	}
}
