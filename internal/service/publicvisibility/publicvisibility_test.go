// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestNextState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current repo.ModerationState
		action  ModerationAction
		want    repo.ModerationState
		wantErr error
	}{
		{name: "approve pending", current: repo.ModerationStatePending, action: ActionApprove, want: repo.ModerationStateApproved},
		{name: "approve rejected", current: repo.ModerationStateRejected, action: ActionApprove, want: repo.ModerationStateApproved},
		{name: "reject pending", current: repo.ModerationStatePending, action: ActionReject, want: repo.ModerationStateRejected},
		{name: "hide approved", current: repo.ModerationStateApproved, action: ActionHide, want: repo.ModerationStateHidden},
		{name: "mark approved as spam", current: repo.ModerationStateApproved, action: ActionMarkSpam, want: repo.ModerationStateSpam},
		{name: "restore hidden", current: repo.ModerationStateHidden, action: ActionRestore, want: repo.ModerationStateApproved},
		{name: "restore spam", current: repo.ModerationStateSpam, action: ActionRestore, want: repo.ModerationStatePending},
		{name: "cannot hide pending", current: repo.ModerationStatePending, action: ActionHide, wantErr: ErrInvalidTransition},
		{name: "cannot reject approved", current: repo.ModerationStateApproved, action: ActionReject, wantErr: ErrInvalidTransition},
		{name: "cannot mark spam twice", current: repo.ModerationStateSpam, action: ActionMarkSpam, wantErr: ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nextState(tt.current, tt.action)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("nextState() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("nextState() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("nextState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicRequestVisible(t *testing.T) {
	t.Parallel()

	base := repo.PublicRequestCandidate{
		Policy: repo.Policy{
			PortalAccessMode: repo.AccessModePublic,
			RequestsEnabled:  true,
		},
		Profile: repo.RequestProfile{
			IncludedInPortal: true,
		},
		Moderation: repo.ModerationSubject{
			State: repo.ModerationStateApproved,
		},
		CustomerRequestLive: true,
	}
	if !publicRequestVisible(base) {
		t.Fatal("expected approved public request to be visible")
	}

	tests := []struct {
		name   string
		mutate func(*repo.PublicRequestCandidate)
	}{
		{name: "portal disabled", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Policy.PortalAccessMode = repo.AccessModeDisabled
		}},
		{name: "requests disabled", mutate: func(candidate *repo.PublicRequestCandidate) { candidate.Policy.RequestsEnabled = false }},
		{name: "profile excluded", mutate: func(candidate *repo.PublicRequestCandidate) { candidate.Profile.IncludedInPortal = false }},
		{name: "not approved", mutate: func(candidate *repo.PublicRequestCandidate) { candidate.Moderation.State = repo.ModerationStatePending }},
		{name: "request not live", mutate: func(candidate *repo.PublicRequestCandidate) { candidate.CustomerRequestLive = false }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := ptrext.Of(base)
			tt.mutate(candidate)
			if publicRequestVisible(ptrext.Indirect(candidate)) {
				t.Fatal("expected request to be hidden")
			}
		})
	}
}

func TestNormalizePolicyInput(t *testing.T) {
	t.Parallel()

	valid := UpdatePolicyInput{
		TenantID:              "tenant-a",
		PortalAccessMode:      repo.AccessModePublic,
		SubmissionWriteMode:   repo.WriteModeIdentified,
		CommentWriteMode:      repo.WriteModeDisabled,
		VoteWriteMode:         repo.WriteModeAnonymous,
		DefaultRequestState:   repo.ModerationStateApproved,
		DefaultCommentState:   repo.ModerationStatePending,
		SubmitterIdentityMode: repo.IdentityModeDisplayName,
		Actor: auditlogsvc.Actor{
			ID:   uuid.NewString(),
			Type: "admin",
		},
	}
	policy, err := normalizePolicyInput(valid)
	if err != nil {
		t.Fatalf("normalizePolicyInput() unexpected error: %v", err)
	}
	if policy.TenantID != valid.TenantID || policy.UpdatedBy != valid.Actor.ID {
		t.Fatalf("normalizePolicyInput() = %#v, want tenant and actor normalized", policy)
	}

	invalid := valid
	invalid.PortalAccessMode = repo.AccessModeInviteOnly
	if _, err := normalizePolicyInput(invalid); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizePolicyInput() error = %v, want %v", err, ErrValidation)
	}
}

func TestNormalizeRequestProfileInput(t *testing.T) {
	t.Parallel()

	actor := auditlogsvc.Actor{ID: uuid.NewString(), Type: "admin"}
	valid := UpsertRequestProfileInput{
		TenantID:          " tenant-a ",
		RequestID:         uuid.New(),
		PublicSlug:        " Pricing-API ",
		PublicTitle:       " Public pricing API ",
		PublicSummary:     " Safe public summary ",
		PublicState:       "Planned",
		RoadmapColumn:     "Next",
		IncludedInPortal:  true,
		IncludedInRoadmap: true,
		Actor:             actor,
	}
	profile, err := normalizeRequestProfileInput(valid)
	if err != nil {
		t.Fatalf("normalizeRequestProfileInput() unexpected error: %v", err)
	}
	if profile.TenantID != "tenant-a" || profile.PublicSlug != "pricing-api" || profile.PublicTitle != "Public pricing API" {
		t.Fatalf("normalizeRequestProfileInput() = %#v, want trimmed public profile", profile)
	}

	tests := []struct {
		name   string
		mutate func(*UpsertRequestProfileInput)
	}{
		{name: "missing actor", mutate: func(input *UpsertRequestProfileInput) { input.Actor.ID = "" }},
		{name: "invalid slug", mutate: func(input *UpsertRequestProfileInput) { input.PublicSlug = "bad slug" }},
		{name: "empty title", mutate: func(input *UpsertRequestProfileInput) { input.PublicTitle = " " }},
		{name: "summary too long", mutate: func(input *UpsertRequestProfileInput) { input.PublicSummary = strings.Repeat("a", 2001) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ptrext.Of(valid)
			tt.mutate(input)
			if _, err := normalizeRequestProfileInput(ptrext.Indirect(input)); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeRequestProfileInput() error = %v, want %v", err, ErrValidation)
			}
		})
	}
}

func TestNormalizeModerateInput(t *testing.T) {
	t.Parallel()

	actor := auditlogsvc.Actor{ID: uuid.NewString(), Type: "admin"}
	valid := ModerateInput{
		TenantID:   " tenant-a ",
		ID:         uuid.New(),
		Action:     ActionReject,
		ReasonCode: "Policy.Sensitive",
		ReasonNote: strings.Repeat("x", 1002),
		Actor:      actor,
	}
	normalized, err := normalizeModerateInput(valid)
	if err != nil {
		t.Fatalf("normalizeModerateInput() unexpected error: %v", err)
	}
	if normalized.TenantID != "tenant-a" || normalized.ReasonCode != "policy.sensitive" ||
		len([]rune(normalized.ReasonNote)) != 1000 {
		t.Fatalf("normalizeModerateInput() = %#v, want trimmed and bounded input", normalized)
	}

	approveWithoutReason := valid
	approveWithoutReason.Action = ActionApprove
	approveWithoutReason.ReasonCode = ""
	if _, err := normalizeModerateInput(approveWithoutReason); err != nil {
		t.Fatalf("normalizeModerateInput() approve without reason error = %v, want nil", err)
	}

	tests := []struct {
		name   string
		mutate func(*ModerateInput)
	}{
		{name: "reject requires reason", mutate: func(input *ModerateInput) { input.ReasonCode = " " }},
		{name: "hide requires reason", mutate: func(input *ModerateInput) {
			input.Action = ActionHide
			input.ReasonCode = ""
		}},
		{name: "spam requires reason", mutate: func(input *ModerateInput) {
			input.Action = ActionMarkSpam
			input.ReasonCode = ""
		}},
		{name: "invalid reason code", mutate: func(input *ModerateInput) { input.ReasonCode = "unsafe content!" }},
		{name: "missing actor", mutate: func(input *ModerateInput) { input.Actor.ID = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ptrext.Of(valid)
			tt.mutate(input)
			if _, err := normalizeModerateInput(ptrext.Indirect(input)); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeModerateInput() error = %v, want %v", err, ErrValidation)
			}
		})
	}
}

func TestBoundedUsesRunes(t *testing.T) {
	t.Parallel()

	got := bounded("你好世界", 2)
	if got != "你好" {
		t.Fatalf("bounded() = %q, want %q", got, "你好")
	}
}
