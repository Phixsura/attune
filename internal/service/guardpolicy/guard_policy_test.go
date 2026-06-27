// ptrext:file-allow test fixtures use mutable fake stores.
package guardpolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
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

	// Error injection fields.
	listErr    error
	replaceErr error
	createErr  error
	updateErr  error
	deleteErr  error
	resolveErr error
}

func (f *fakeStore) ListForConsole(_ context.Context, tenantID string) ([]llmguard.Policy, error) {
	f.tenantID = tenantID
	return f.list, f.listErr
}

func (f *fakeStore) ReplaceTenantPolicies(_ context.Context, tenantID, actor string, policies []llmguard.Policy) error {
	f.tenantID = tenantID
	f.actor = actor
	f.replaced = policies
	return f.replaceErr
}

func (f *fakeStore) CreateTenantPolicy(_ context.Context, tenantID, actor string, policy llmguard.Policy) (llmguard.Policy, error) {
	f.createTenant = tenantID
	f.actor = actor
	f.created = policy
	return policy, f.createErr
}

func (f *fakeStore) UpdateTenantPolicy(_ context.Context, tenantID, actor, id string, policy llmguard.Policy) (llmguard.Policy, error) {
	f.updateTenant = tenantID
	f.actor = actor
	f.updateID = id
	f.updated = policy
	return policy, f.updateErr
}

func (f *fakeStore) DeleteTenantPolicy(_ context.Context, tenantID, id string) error {
	f.deleteTenant = tenantID
	f.deleteID = id
	return f.deleteErr
}

func (f *fakeStore) Resolve(_ context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error) {
	f.meta = meta
	return f.plan, f.resolveErr
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

// ---------------------------------------------------------------------------
// Service CRUD coverage: List, store errors, validation-failure short-circuits
// ---------------------------------------------------------------------------

func TestList_DelegatesToStore(t *testing.T) {
	t.Parallel()
	want := []llmguard.Policy{{ID: "p1", Name: "test"}}
	store := &fakeStore{list: want}
	svc := NewService(store, nil)
	got, err := svc.List(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("List returned %+v, want %+v", got, want)
	}
	if store.tenantID != "tenant-1" {
		t.Fatalf("store.tenantID = %q, want tenant-1", store.tenantID)
	}
}

func TestList_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("db down")
	store := &fakeStore{listErr: wantErr}
	svc := NewService(store, nil)
	_, err := svc.List(context.Background(), "tenant-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("List err = %v, want %v", err, wantErr)
	}
}

func TestReplaceTenantPolicies_StoreReplaceError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("replace failed")
	store := &fakeStore{replaceErr: wantErr}
	svc := NewService(store, nil)
	_, err := svc.ReplaceTenantPolicies(context.Background(), "t", "a", []llmguard.Policy{{
		Name: "ok", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestReplaceTenantPolicies_ListErrorAfterReplace(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("list failed after replace")
	store := &fakeStore{listErr: wantErr}
	svc := NewService(store, nil)
	_, err := svc.ReplaceTenantPolicies(context.Background(), "t", "a", []llmguard.Policy{{
		Name: "ok", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestCreatePolicy_ValidationFailure(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "", Kind: llmguard.KindDefault,
	})
	if !errors.Is(err, ErrPolicyNameRequired) {
		t.Fatalf("err = %v, want ErrPolicyNameRequired", err)
	}
}

func TestCreatePolicy_StoreError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("create failed")
	store := &fakeStore{createErr: wantErr}
	svc := NewService(store, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "ok", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestUpdatePolicy_ValidationFailure(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.UpdatePolicy(context.Background(), "t", "a", "id-1", llmguard.Policy{
		Name: "", Kind: llmguard.KindDefault,
	})
	if !errors.Is(err, ErrPolicyNameRequired) {
		t.Fatalf("err = %v, want ErrPolicyNameRequired", err)
	}
}

func TestUpdatePolicy_StoreError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("update failed")
	store := &fakeStore{updateErr: wantErr}
	svc := NewService(store, nil)
	_, err := svc.UpdatePolicy(context.Background(), "t", "a", "id-1", llmguard.Policy{
		Name: "ok", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestDeletePolicy_StoreError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("delete failed")
	store := &fakeStore{deleteErr: wantErr}
	svc := NewService(store, nil)
	err := svc.DeletePolicy(context.Background(), "t", "id-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestResolve_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("resolve failed")
	store := &fakeStore{resolveErr: wantErr}
	svc := NewService(store, nil)
	_, err := svc.Resolve(context.Background(), llmclient.GuardMetadata{TenantID: "t"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestResolve_PreservesPurposeWhenSet(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	svc := NewService(store, nil)
	_, err := svc.Resolve(context.Background(), llmclient.GuardMetadata{
		TenantID: "t", Purpose: "eval",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if store.meta.Purpose != "eval" {
		t.Fatalf("purpose = %q, want eval", store.meta.Purpose)
	}
}

// ---------------------------------------------------------------------------
// validatePolicies: count limit
// ---------------------------------------------------------------------------

func TestValidatePolicies_TooMany(t *testing.T) {
	t.Parallel()
	policies := make([]llmguard.Policy, MaxPolicies+1)
	for i := range policies {
		policies[i] = llmguard.Policy{
			Name: fmt.Sprintf("p%d", i), Kind: llmguard.KindDefault,
			Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
		}
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.ReplaceTenantPolicies(context.Background(), "t", "a", policies)
	if !errors.Is(err, ErrPolicyLimit) {
		t.Fatalf("err = %v, want ErrPolicyLimit", err)
	}
}

func TestValidatePolicies_ExactlyMaxAllowed(t *testing.T) {
	t.Parallel()
	policies := make([]llmguard.Policy, MaxPolicies)
	for i := range policies {
		policies[i] = llmguard.Policy{
			Name: fmt.Sprintf("p%d", i), Kind: llmguard.KindDefault,
			Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
		}
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.ReplaceTenantPolicies(context.Background(), "t", "a", policies)
	if err != nil {
		t.Fatalf("MaxPolicies should be accepted: %v", err)
	}
}

func TestValidatePolicies_EmptyIsValid(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.ReplaceTenantPolicies(context.Background(), "t", "a", nil)
	if err != nil {
		t.Fatalf("nil policies should be valid: %v", err)
	}
}

// ---------------------------------------------------------------------------
// validatePolicy: name, kind, priority, rule limit
// ---------------------------------------------------------------------------

func TestValidatePolicy_EmptyName(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "   ", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrPolicyNameRequired) {
		t.Fatalf("whitespace-only name: err = %v, want ErrPolicyNameRequired", err)
	}
}

func TestValidatePolicy_NameTooLong(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: strings.Repeat("x", MaxPolicyNameBytes+1), Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrPolicyNameTooLong) {
		t.Fatalf("err = %v, want ErrPolicyNameTooLong", err)
	}
}

func TestValidatePolicy_InvalidKind(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: "invalid-kind",
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrKindInvalid) {
		t.Fatalf("err = %v, want ErrKindInvalid", err)
	}
}

func TestValidatePolicy_NegativePriority(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault, Priority: -1,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrPriorityInvalid) {
		t.Fatalf("err = %v, want ErrPriorityInvalid", err)
	}
}

func TestValidatePolicy_PriorityAboveMax(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault, Priority: MaxPriority + 1,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrPriorityInvalid) {
		t.Fatalf("err = %v, want ErrPriorityInvalid", err)
	}
}

func TestValidatePolicy_PriorityBoundsAccepted(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	for _, p := range []int{0, 1, MaxPriority} {
		_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
			Name: "test", Kind: llmguard.KindDefault, Priority: p,
			Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
		})
		if err != nil {
			t.Fatalf("priority %d should be valid: %v", p, err)
		}
	}
}

func TestValidatePolicy_TooManyRules(t *testing.T) {
	t.Parallel()
	rules := make([]llmguard.Rule, MaxRulesPerPolicy+1)
	for i := range rules {
		rules[i] = llmguard.Rule{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault, Rules: rules,
	})
	if !errors.Is(err, ErrRuleLimit) {
		t.Fatalf("err = %v, want ErrRuleLimit", err)
	}
}

// ---------------------------------------------------------------------------
// validateRule: guard, stage, action, entities, replacement
// ---------------------------------------------------------------------------

func TestValidateRule_InvalidGuard(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "not_pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrGuardInvalid) {
		t.Fatalf("err = %v, want ErrGuardInvalid", err)
	}
}

func TestValidateRule_InvalidStage(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: "invalid_stage", Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrStageInvalid) {
		t.Fatalf("err = %v, want ErrStageInvalid", err)
	}
}

func TestValidateRule_InvalidEntity(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"ssn"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrEntityInvalid) {
		t.Fatalf("err = %v, want ErrEntityInvalid", err)
	}
}

func TestValidateRule_EmptyReplacementAccepted(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact, Replacement: ""}},
	})
	if err != nil {
		t.Fatalf("empty replacement should be valid: %v", err)
	}
}

func TestValidateRule_ValidReplacement(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact, Replacement: "<{type}>"}},
	})
	if err != nil {
		t.Fatalf("valid replacement should be accepted: %v", err)
	}
}

func TestValidateRule_NoEntitiesAccepted(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("no entities should be valid: %v", err)
	}
}

// ---------------------------------------------------------------------------
// validateTarget: channels, source IDs, tags, purposes, stages, environments
// ---------------------------------------------------------------------------

func TestValidateTarget_ValidSourceIDs(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceIDs: []string{"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("valid source IDs should pass: %v", err)
	}
}

func TestValidateTarget_InvalidSourceID(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceIDs: []string{"not-a-uuid"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_EmptySourceIDInList(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceIDs: []string{""}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_SourceIDTooLong(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceIDs: []string{strings.Repeat("a", MaxTargetValueBytes+1)}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_TooManySourceIDs(t *testing.T) {
	t.Parallel()
	ids := make([]string, MaxTargetValues+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("a0eebc99-9c0b-4ef8-bb6d-%012d", i)
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceIDs: ids},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_ValidSourceTags(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceTags: []string{"tag-1", "tag.2", "TAG:3"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("valid source tags should pass: %v", err)
	}
}

func TestValidateTarget_InvalidSourceTag(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceTags: []string{"has spaces"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_EmptySourceTag(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceTags: []string{""}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_TooManySourceTags(t *testing.T) {
	t.Parallel()
	tags := make([]string, MaxTargetValues+1)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag%d", i)
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceTags: tags},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_SourceTagTooLong(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{SourceTags: []string{strings.Repeat("t", MaxTargetValueBytes+1)}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_InvalidEnvironment(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Environments: []string{"has spaces"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_TooManyEnvironments(t *testing.T) {
	t.Parallel()
	envs := make([]string, MaxTargetValues+1)
	for i := range envs {
		envs[i] = fmt.Sprintf("env%d", i)
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Environments: envs},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_InvalidPurpose(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Purposes: []string{"unknown"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_TooManyPurposes(t *testing.T) {
	t.Parallel()
	purposes := make([]string, MaxTargetValues+1)
	for i := range purposes {
		purposes[i] = "enrich"
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Purposes: purposes},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_InvalidStageInTarget(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Stages: []llmguard.Stage{"invalid"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_TooManyStages(t *testing.T) {
	t.Parallel()
	stages := make([]llmguard.Stage, MaxTargetValues+1)
	for i := range stages {
		stages[i] = llmguard.StageLLMInput
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Stages: stages},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTarget_ValidStagesAccepted(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Stages: []llmguard.Stage{llmguard.StageLLMInput, llmguard.StageLLMOutput}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("valid stages should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// validateTargetChannels: empty, too long, too many, unknown channel
// ---------------------------------------------------------------------------

func TestValidateTargetChannels_EmptyValueInList(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{""}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTargetChannels_WhitespaceOnlyValue(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{"   "}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTargetChannels_ValueTooLong(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{strings.Repeat("x", MaxTargetValueBytes+1)}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTargetChannels_TooManyValues(t *testing.T) {
	t.Parallel()
	channels := make([]string, MaxTargetValues+1)
	for i := range channels {
		channels[i] = "api"
	}
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: channels},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid", err)
	}
}

func TestValidateTargetChannels_ValidKnownChannel(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{"api", "email"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("known channels should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewService: nil sources fallback
// ---------------------------------------------------------------------------

func TestNewService_NilSourcesFallsBackToDefault(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeStore{}, nil)
	// The DefaultSourceSet includes "api" -- verify a valid channel is accepted.
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{"api"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("nil sources should use DefaultSourceSet: %v", err)
	}
}

func TestNewService_CustomSourceSet(t *testing.T) {
	t.Parallel()
	custom := domain.NewSourceSet(map[string]string{"custom_channel": "Custom"})
	svc := NewService(&fakeStore{}, custom)
	// "api" is NOT in the custom set.
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{"api"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("err = %v, want ErrTargetInvalid (api not in custom set)", err)
	}
	// "custom_channel" IS in the custom set.
	_, err = svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{"custom_channel"}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("custom_channel should be valid: %v", err)
	}
}

// ---------------------------------------------------------------------------
// validUUIDList: thorough unit-level coverage
// ---------------------------------------------------------------------------

func TestValidUUIDList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		want   bool
	}{
		{"nil", nil, true},
		{"empty", []string{}, true},
		{"valid single", []string{"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"}, true},
		{"valid multiple", []string{
			"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
			"b1ffcd00-0d1c-5f09-cc7e-7ccace491b22",
		}, true},
		{"empty string element", []string{""}, false},
		{"not a uuid", []string{"not-a-uuid"}, false},
		{"mixed valid invalid", []string{"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", "invalid"}, false},
		{"too long value", []string{strings.Repeat("a", MaxTargetValueBytes+1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validUUIDList(tt.values); got != tt.want {
				t.Errorf("validUUIDList(%v) = %v, want %v", tt.values, got, tt.want)
			}
		})
	}

	// Too many values.
	t.Run("too many", func(t *testing.T) {
		ids := make([]string, MaxTargetValues+1)
		for i := range ids {
			ids[i] = fmt.Sprintf("a0eebc99-9c0b-4ef8-bb6d-%012d", i)
		}
		if validUUIDList(ids) {
			t.Error("too many UUIDs should return false")
		}
	})
}

// ---------------------------------------------------------------------------
// validTokenList: thorough unit-level coverage
// ---------------------------------------------------------------------------

func TestValidTokenList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		want   bool
	}{
		{"nil", nil, true},
		{"empty", []string{}, true},
		{"valid simple", []string{"abc"}, true},
		{"valid with special chars", []string{"a.b-c_d:e"}, true},
		{"uppercase", []string{"ABC"}, true},
		{"digits", []string{"123"}, true},
		{"empty element", []string{""}, false},
		{"spaces", []string{"a b"}, false},
		{"special chars", []string{"a@b"}, false},
		{"newline", []string{"a\nb"}, false},
		{"too long", []string{strings.Repeat("x", MaxTargetValueBytes+1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validTokenList(tt.values); got != tt.want {
				t.Errorf("validTokenList(%v) = %v, want %v", tt.values, got, tt.want)
			}
		})
	}

	t.Run("too many", func(t *testing.T) {
		tokens := make([]string, MaxTargetValues+1)
		for i := range tokens {
			tokens[i] = fmt.Sprintf("t%d", i)
		}
		if validTokenList(tokens) {
			t.Error("too many tokens should return false")
		}
	})
}

// ---------------------------------------------------------------------------
// validStageList: thorough unit-level coverage
// ---------------------------------------------------------------------------

func TestValidStageList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		stages []llmguard.Stage
		want   bool
	}{
		{"nil", nil, true},
		{"empty", []llmguard.Stage{}, true},
		{"valid single", []llmguard.Stage{llmguard.StageLLMInput}, true},
		{"all valid", []llmguard.Stage{llmguard.StageLLMInput, llmguard.StageLLMOutput, llmguard.StageOutbound, llmguard.StageToolCall}, true},
		{"invalid", []llmguard.Stage{"invalid"}, false},
		{"mixed", []llmguard.Stage{llmguard.StageLLMInput, "invalid"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validStageList(tt.stages); got != tt.want {
				t.Errorf("validStageList(%v) = %v, want %v", tt.stages, got, tt.want)
			}
		})
	}

	t.Run("too many", func(t *testing.T) {
		stages := make([]llmguard.Stage, MaxTargetValues+1)
		for i := range stages {
			stages[i] = llmguard.StageLLMInput
		}
		if validStageList(stages) {
			t.Error("too many stages should return false")
		}
	})
}

// ---------------------------------------------------------------------------
// normalizeTenantPolicy: priority default, name trim, tenant wipe
// ---------------------------------------------------------------------------

func TestNormalizeTenantPolicy_NonZeroPriorityPreserved(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	svc := NewService(store, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault, Priority: 500,
		Rules: []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if store.created.Priority != 500 {
		t.Fatalf("priority = %d, want 500", store.created.Priority)
	}
}

func TestNormalizeTenantPolicy_TargetChannelsTrimmed(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	svc := NewService(store, nil)
	_, err := svc.CreatePolicy(context.Background(), "t", "a", llmguard.Policy{
		Name: "test", Kind: llmguard.KindDefault,
		Target: llmguard.Target{Channels: []string{"  api  "}, Purposes: []string{"  enrich  "}},
		Rules:  []llmguard.Rule{{Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact}},
	})
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if len(store.created.Target.Channels) != 1 || store.created.Target.Channels[0] != "api" {
		t.Fatalf("channels = %v, want [api]", store.created.Target.Channels)
	}
	if len(store.created.Target.Purposes) != 1 || store.created.Target.Purposes[0] != "enrich" {
		t.Fatalf("purposes = %v, want [enrich]", store.created.Target.Purposes)
	}
}

// ---------------------------------------------------------------------------
// ErrToMessage: enriched channel error message
// ---------------------------------------------------------------------------

func TestErrToMessage_WrappedTargetInvalid(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("%w: source %q is not a known source (valid: api, web)", ErrTargetInvalid, "typo")
	msg := ErrToMessage(wrapped)
	if !strings.Contains(msg, "typo") || !strings.Contains(msg, "valid:") {
		t.Errorf("ErrToMessage = %q, want enriched message with offending value", msg)
	}
}

func TestErrToMessage_BareTargetInvalid(t *testing.T) {
	t.Parallel()
	msg := ErrToMessage(ErrTargetInvalid)
	if msg != "guard policy target is invalid" {
		t.Errorf("ErrToMessage = %q, want bare sentinel text", msg)
	}
}

// ---------------------------------------------------------------------------
// ErrToCode: wrapped errors
// ---------------------------------------------------------------------------

func TestErrToCode_WrappedErrors(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("wrap: %w", ErrPolicyNameRequired)
	if got := ErrToCode(wrapped); got != attunev1.ErrorCode_VALIDATION {
		t.Errorf("ErrToCode(wrapped) = %v, want VALIDATION", got)
	}
}

func TestErrToCode_NilError(t *testing.T) {
	t.Parallel()
	// A non-sentinel error should return unspecified.
	if got := ErrToCode(errors.New("random")); got != attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED {
		t.Errorf("ErrToCode(random) = %v, want UNSPECIFIED", got)
	}
}
