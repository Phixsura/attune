// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
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
		{name: "authenticated portal access is not public", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Policy.PortalAccessMode = repo.AccessModeAuthenticated
		}},
		{name: "invite-only portal access is not public", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Policy.PortalAccessMode = repo.AccessModeInviteOnly
		}},
		{name: "requests disabled", mutate: func(candidate *repo.PublicRequestCandidate) { candidate.Policy.RequestsEnabled = false }},
		{name: "profile excluded", mutate: func(candidate *repo.PublicRequestCandidate) { candidate.Profile.IncludedInPortal = false }},
		{name: "pending moderation", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Moderation.State = repo.ModerationStatePending
		}},
		{name: "rejected moderation", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Moderation.State = repo.ModerationStateRejected
		}},
		{name: "hidden moderation", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Moderation.State = repo.ModerationStateHidden
		}},
		{name: "spam moderation", mutate: func(candidate *repo.PublicRequestCandidate) {
			candidate.Moderation.State = repo.ModerationStateSpam
		}},
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

func TestPublicRequestListVisible(t *testing.T) {
	t.Parallel()

	policy := repo.Policy{
		PortalAccessMode: repo.AccessModePublic,
		RequestsEnabled:  true,
		RoadmapEnabled:   true,
	}
	if !publicRequestListVisible(policy, false) || !publicRequestListVisible(policy, true) {
		t.Fatalf("publicRequestListVisible(%#v) = false, want true for request and roadmap lists", policy)
	}
	policy.RequestsEnabled = false
	if publicRequestListVisible(policy, false) || !publicRequestListVisible(policy, true) {
		t.Fatalf("publicRequestListVisible(%#v) should hide requests and keep roadmap", policy)
	}
	policy.PortalAccessMode = repo.AccessModeDisabled
	if publicRequestListVisible(policy, true) {
		t.Fatalf("publicRequestListVisible(%#v) = true, want disabled portal hidden", policy)
	}
}

func TestPortalVisitorAndSubmitterHelpers(t *testing.T) {
	t.Parallel()

	if got := portalVisitorSubjectKey("  visitor-1  "); got != "portal:visitor-1" {
		t.Fatalf("portalVisitorSubjectKey() = %q, want trimmed portal key", got)
	}
	if got := portalVisitorSubjectKey("   "); got != "" {
		t.Fatalf("portalVisitorSubjectKey(blank) = %q, want empty", got)
	}
	if got := portalVisitorSubjectDisplay(); got != "Portal visitor" {
		t.Fatalf("portalVisitorSubjectDisplay() = %q, want public label", got)
	}

	displayPolicy := repo.Policy{
		ShowSubmitterDisplay:  true,
		SubmitterIdentityMode: repo.IdentityModeDisplayName,
		CommentsEnabled:       true,
		CommentWriteMode:      repo.WriteModeAnonymous,
	}
	if got := publicSubmitterDisplay(displayPolicy, "Ada"); got != "Ada" {
		t.Fatalf("publicSubmitterDisplay() = %q, want visible submitter", got)
	}
	if got := publicSubmitterDisplay(repo.Policy{ShowSubmitterDisplay: false, SubmitterIdentityMode: repo.IdentityModeDisplayName}, "Ada"); got != "" {
		t.Fatalf("publicSubmitterDisplay(hidden) = %q, want hidden display", got)
	}
	if got := publicSubmitterDisplay(repo.Policy{ShowSubmitterDisplay: true, SubmitterIdentityMode: repo.IdentityModeAnonymous}, "Ada"); got != "" {
		t.Fatalf("publicSubmitterDisplay(anonymous) = %q, want hidden display", got)
	}

	if !publicCommentWriteEnabled(displayPolicy) {
		t.Fatal("publicCommentWriteEnabled() = false, want true for enabled comments")
	}
	if publicCommentWriteEnabled(repo.Policy{CommentsEnabled: true, CommentWriteMode: repo.WriteModeDisabled}) {
		t.Fatal("publicCommentWriteEnabled() = true, want false for disabled write mode")
	}
	if !publicCommentsVisible(displayPolicy) || publicCommentsVisible(repo.Policy{}) {
		t.Fatal("publicCommentsVisible() did not reflect comment visibility policy")
	}
}

func TestPolicyModeHelpers(t *testing.T) {
	t.Parallel()

	if !validAccessMode(repo.AccessModeDisabled) || !validAccessMode(repo.AccessModePublic) || validAccessMode(repo.AccessModeAuthenticated) {
		t.Fatal("validAccessMode() returned unexpected result")
	}
	if !validWriteMode(repo.WriteModeDisabled) || !validWriteMode(repo.WriteModeAnonymous) || !validWriteMode(repo.WriteModeIdentified) {
		t.Fatal("validWriteMode() should accept supported modes")
	}
	if validWriteMode(repo.WriteMode("invalid")) {
		t.Fatal("validWriteMode() accepted invalid mode")
	}
	if !validDefaultState(repo.ModerationStatePending) || !validDefaultState(repo.ModerationStateApproved) || validDefaultState(repo.ModerationStateRejected) {
		t.Fatal("validDefaultState() returned unexpected result")
	}
	if !validIdentityMode(repo.IdentityModeAnonymous) || !validIdentityMode(repo.IdentityModeDisplayName) || !validIdentityMode(repo.IdentityModeOrganization) || validIdentityMode(repo.IdentityMode("invalid")) {
		t.Fatal("validIdentityMode() returned unexpected result")
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
		PortalSubmissionForm: repo.PortalSubmissionForm{
			Headline:          " Share feedback ",
			Description:       " Tell us what is broken, missing, or worth improving. ",
			Acknowledgement:   " Thanks. We will review your submission. ",
			SubmitButtonLabel: " Submit feedback ",
			ShowPageURL:       true,
			Fields: []repo.PortalSubmissionField{
				{
					Key:         " severity ",
					Label:       " Severity ",
					Kind:        repo.PortalSubmissionFieldKindSelect,
					Required:    true,
					Options:     []string{" low ", " medium ", " high "},
					Placeholder: " Choose a severity ",
				},
			},
		},
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
	if policy.PortalSubmissionForm.Headline != "Share feedback" ||
		policy.PortalSubmissionForm.Fields[0].Key != "severity" ||
		policy.PortalSubmissionForm.Fields[0].Options[0] != "low" {
		t.Fatalf("normalizePolicyInput() portal form = %#v, want normalized portal form", policy.PortalSubmissionForm)
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

func TestNormalizePortalSubmissionForm(t *testing.T) {
	t.Parallel()

	form, err := normalizePortalSubmissionForm(repo.PortalSubmissionForm{
		Headline:          " Share feedback ",
		Description:       " Tell us what is broken, missing, or worth improving. ",
		Acknowledgement:   " Thanks. We will review your submission. ",
		SubmitButtonLabel: " Submit feedback ",
		ShowPageURL:       true,
		Fields: []repo.PortalSubmissionField{
			{
				Key:         " severity ",
				Label:       " Severity ",
				Kind:        repo.PortalSubmissionFieldKindSelect,
				Required:    true,
				Options:     []string{" low ", " medium ", " high "},
				Placeholder: " Choose a severity ",
			},
			{
				Key:         "notes",
				Label:       "Notes",
				Kind:        repo.PortalSubmissionFieldKindTextarea,
				Placeholder: "Optional notes",
			},
		},
	})
	if err != nil {
		t.Fatalf("normalizePortalSubmissionForm() unexpected error: %v", err)
	}
	if form.Headline != "Share feedback" || form.Fields[0].Key != "severity" ||
		form.Fields[0].Options[2] != "high" {
		t.Fatalf("normalizePortalSubmissionForm() = %#v, want normalized portal form", form)
	}

	tests := []struct {
		name string
		form repo.PortalSubmissionForm
	}{
		{
			name: "reserved key",
			form: repo.PortalSubmissionForm{Fields: []repo.PortalSubmissionField{{
				Key:   "title",
				Label: "Title",
				Kind:  repo.PortalSubmissionFieldKindText,
			}}},
		},
		{
			name: "duplicate key",
			form: repo.PortalSubmissionForm{Fields: []repo.PortalSubmissionField{
				{Key: "severity", Label: "Severity", Kind: repo.PortalSubmissionFieldKindText},
				{Key: "severity", Label: "Severity 2", Kind: repo.PortalSubmissionFieldKindText},
			}},
		},
		{
			name: "invalid options on text field",
			form: repo.PortalSubmissionForm{Fields: []repo.PortalSubmissionField{{
				Key:     "severity",
				Label:   "Severity",
				Kind:    repo.PortalSubmissionFieldKindText,
				Options: []string{"bad"},
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizePortalSubmissionForm(tt.form); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizePortalSubmissionForm() error = %v, want %v", err, ErrValidation)
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

func TestPublicMethodsValidateBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := New(nil, nil)
	ctx := context.Background()
	if _, err := service.GetPolicy(ctx, " "); !errors.Is(err, ErrValidation) {
		t.Fatalf("GetPolicy() error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.UpdatePolicy(ctx, UpdatePolicyInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdatePolicy() error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.ListModeration(ctx, ListModerationInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListModeration() error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.GetRequestPublication(ctx, " ", uuid.New()); !errors.Is(err, ErrValidation) {
		t.Fatalf("GetRequestPublication(empty tenant) error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.GetRequestPublication(ctx, "tenant-a", uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("GetRequestPublication(nil id) error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.UpsertRequestProfile(ctx, UpsertRequestProfileInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpsertRequestProfile() error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.Moderate(ctx, ModerateInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Moderate() error = %v, want %v", err, ErrValidation)
	}
	if _, err := service.GetPublicRequest(ctx, " ", "slug", "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(empty tenant) error = %v, want %v", err, ErrNotFound)
	}
	if _, err := service.GetPublicRequest(ctx, "tenant", " ", "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(empty slug) error = %v, want %v", err, ErrNotFound)
	}
	if _, err := service.ListPublicRequests(ctx, " ", 0, "", "", "", "", "", false, false, "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListPublicRequests(empty tenant) error = %v, want %v", err, ErrNotFound)
	}
	if _, err := service.ListPublicRoadmap(ctx, " ", 0, "", "", "", "", "", false, false, "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListPublicRoadmap(empty tenant) error = %v, want %v", err, ErrNotFound)
	}
}

func TestServicePolicyModerationAndPublicationReadsUseRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, fake, requestID := serviceReadFixture(t)

	gotPolicy, err := service.GetPolicy(ctx, " tenant-a ")
	if err != nil || gotPolicy.TenantID != "tenant-a" || fake.policyTenant != "tenant-a" {
		t.Fatalf("GetPolicy() = %#v, %v, tenant=%q", gotPolicy, err, fake.policyTenant)
	}
	list, err := service.ListModeration(ctx, ListModerationInput{TenantID: " tenant-a ", Cursor: " next "})
	if err != nil || len(list.Items) != 1 || fake.listFilter.Cursor != "next" {
		t.Fatalf("ListModeration() = %#v, %v, filter=%#v", list, err, fake.listFilter)
	}
	gotPublication, err := service.GetRequestPublication(ctx, "tenant-a", requestID)
	if err != nil || gotPublication.Profile.RequestID != requestID {
		t.Fatalf("GetRequestPublication() = %#v, %v", gotPublication, err)
	}
	if _, err := service.GetPublicRequest(ctx, "tenant-slug", "pricing-api", "visitor-1"); err != nil || fake.publicRequestViewerSubjectKey != "portal:visitor-1" {
		t.Fatalf("GetPublicRequest() viewer key = %q, err=%v", fake.publicRequestViewerSubjectKey, err)
	}
	if _, err := service.ListPublicRequests(ctx, "tenant-slug", 10, "", "", "", "", "", false, false, "visitor-1"); err != nil || fake.publicListFilter.ViewerSubjectKey != "portal:visitor-1" {
		t.Fatalf("ListPublicRequests() viewer key = %q, err=%v", fake.publicListFilter.ViewerSubjectKey, err)
	}
	if _, err := service.ListPublicRoadmap(ctx, "tenant-slug", 10, "", "", "", "", "", false, false, "visitor-1"); err != nil || fake.publicListFilter.ViewerSubjectKey != "portal:visitor-1" {
		t.Fatalf("ListPublicRoadmap() viewer key = %q, err=%v", fake.publicListFilter.ViewerSubjectKey, err)
	}
}

func TestServiceGetPublicRequestUsesRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, fake, _ := serviceReadFixture(t)

	publicRequest, err := service.GetPublicRequest(ctx, " tenant-slug ", " pricing-api ", "visitor-1")
	if err != nil || publicRequest.Votes != 7 || publicRequest.SubmitterDisplay != "Ada" {
		t.Fatalf("GetPublicRequest() = %#v, %v", publicRequest, err)
	}
	if !publicRequest.ViewerHasVoted {
		t.Fatalf("GetPublicRequest() viewer vote = %#v, want true", publicRequest)
	}
	if len(publicRequest.SimilarRequests) != 1 || publicRequest.SimilarRequests[0].Summary.PublicSlug != "pricing-dashboard" {
		t.Fatalf("GetPublicRequest() similar requests = %#v, want pricing-dashboard", publicRequest.SimilarRequests)
	}
	if fake.publicListFilter.TenantSlug != "tenant-slug" || fake.publicListFilter.Query != "" ||
		fake.publicListFilter.SimilarityText != "Pricing API" || fake.publicListFilter.ExcludePublicSlug != "pricing-api" ||
		fake.publicListFilter.Sort != "top" || fake.publicListFilter.Limit != 4 ||
		fake.publicListFilter.ViewerSubjectKey != "portal:visitor-1" {
		t.Fatalf("GetPublicRequest() similar filter = %#v", fake.publicListFilter)
	}
}

func TestServiceListPublicRequestsUsesRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, fake, _ := serviceReadFixture(t)

	publicRequests, err := service.ListPublicRequests(ctx, " tenant-slug ", 10, " 0 ", "pricing", "recent", "planned", "next", true, true, "visitor-1")
	if err != nil || len(publicRequests.Requests) != 1 || publicRequests.NextCursor != "50" ||
		fake.publicListFilter.TenantSlug != "tenant-slug" || fake.publicListFilter.Roadmap ||
		fake.publicListFilter.Query != "pricing" || fake.publicListFilter.Sort != "recent" ||
		fake.publicListFilter.State != "planned" || fake.publicListFilter.RoadmapColumn != "next" ||
		!fake.publicListFilter.OnlyVotedByViewer || !fake.publicListFilter.OnlyWithComments {
		t.Fatalf("ListPublicRequests() = %#v, %v, filter=%#v", publicRequests, err, fake.publicListFilter)
	}
	if !publicRequests.Requests[0].ViewerHasVoted {
		t.Fatalf("ListPublicRequests() viewer vote = %#v, want true", publicRequests.Requests[0])
	}
}

func TestServiceListPublicRoadmapUsesRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, fake, _ := serviceReadFixture(t)

	publicRoadmap, err := service.ListPublicRoadmap(ctx, " tenant-slug ", 10, " 0 ", "pricing", "recent", "planned", "next", true, true, "visitor-1")
	if err != nil || len(publicRoadmap.Requests) != 1 || !fake.publicListFilter.Roadmap {
		t.Fatalf("ListPublicRoadmap() = %#v, %v, filter=%#v", publicRoadmap, err, fake.publicListFilter)
	}
	if !publicRoadmap.Requests[0].ViewerHasVoted {
		t.Fatalf("ListPublicRoadmap() viewer vote = %#v, want true", publicRoadmap.Requests[0])
	}
}

func serviceReadFixture(t *testing.T) (*Service, *fakePublicRepo, uuid.UUID) {
	t.Helper()

	requestID := uuid.New()
	policy := defaultPolicy("tenant-a")
	policy.PortalAccessMode = repo.AccessModePublic
	policy.SearchIndexingEnabled = true
	policy.RequestsEnabled = true
	policy.RoadmapEnabled = true
	policy.SubmitterIdentityMode = repo.IdentityModeDisplayName
	policy.ShowSubmitterDisplay = true
	publication := servicePublication(requestID)
	fake := ptrext.Of(fakePublicRepo{
		policy: ptrext.Of(policy),
		listResult: repo.ListResult{
			Items: []repo.ModerationSubject{{ID: uuid.New(), TenantID: "tenant-a"}},
		},
		publication: ptrext.Of(publication),
		candidate: ptrext.Of(repo.PublicRequestCandidate{
			Policy:              policy,
			Profile:             publication.Profile,
			Moderation:          publication.Moderation,
			VoteCount:           7,
			CommentCount:        2,
			SubmitterDisplay:    "Ada",
			ViewerHasVoted:      true,
			CustomerRequestLive: true,
		}),
		publicListResult: repo.PublicRequestListResult{
			Policy: policy,
			Items: []repo.PublicRequestListCandidate{{
				Profile: repo.RequestProfile{
					ID:            uuid.New(),
					TenantID:      "tenant-a",
					RequestID:     uuid.New(),
					PublicSlug:    "pricing-dashboard",
					PublicTitle:   "Pricing Dashboard",
					PublicSummary: "Dashboard for pricing requests",
					PublicState:   "planned",
					RoadmapColumn: "next",
				},
				Moderation: repo.ModerationSubject{
					ID:        uuid.New(),
					TenantID:  "tenant-a",
					Surface:   repo.SurfaceRequest,
					SubjectID: uuid.NewString(),
					State:     repo.ModerationStateApproved,
				},
				VoteCount:        3,
				CommentCount:     1,
				SubmitterDisplay: "Ada",
				ViewerHasVoted:   true,
			}},
			NextCursor: "50",
		},
	})
	return ptrext.Of(Service{repo: fake}), fake, requestID
}

func TestServiceReadMethodErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.New()
	fake := ptrext.Of(fakePublicRepo{policyErr: repo.ErrNotFound})
	service := ptrext.Of(Service{repo: fake})
	policy, err := service.GetPolicy(ctx, "tenant-a")
	if err != nil || policy.PortalAccessMode != repo.AccessModeDisabled {
		t.Fatalf("GetPolicy(not found) = %#v, %v, want default policy", policy, err)
	}

	boom := errors.New("repo down")
	fake.policyErr = boom
	if _, err := service.GetPolicy(ctx, "tenant-a"); !errors.Is(err, boom) {
		t.Fatalf("GetPolicy(repo error) error = %v, want %v", err, boom)
	}
	fake.publicationErr = repo.ErrNotFound
	if _, err := service.GetRequestPublication(ctx, "tenant-a", requestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRequestPublication(not found) error = %v, want %v", err, ErrNotFound)
	}
	fake.publicationErr = boom
	if _, err := service.GetRequestPublication(ctx, "tenant-a", requestID); !errors.Is(err, boom) {
		t.Fatalf("GetRequestPublication(repo error) error = %v, want %v", err, boom)
	}
	fake.candidateErr = repo.ErrNotFound
	if _, err := service.GetPublicRequest(ctx, "tenant", "slug", "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(not found) error = %v, want %v", err, ErrNotFound)
	}
	fake.candidateErr = nil
	fake.candidate = ptrext.Of(repo.PublicRequestCandidate{})
	if _, err := service.GetPublicRequest(ctx, "tenant", "slug", "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(hidden) error = %v, want %v", err, ErrNotFound)
	}
	fake.candidateErr = boom
	if _, err := service.GetPublicRequest(ctx, "tenant", "slug", "visitor-1"); !errors.Is(err, boom) {
		t.Fatalf("GetPublicRequest(repo error) error = %v, want %v", err, boom)
	}
	fake.publicListErr = repo.ErrNotFound
	if _, err := service.ListPublicRequests(ctx, "tenant", 0, "", "", "", "", "", false, false, "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListPublicRequests(not found) error = %v, want %v", err, ErrNotFound)
	}
	fake.publicListErr = repo.ErrInvalidInput
	if _, err := service.ListPublicRoadmap(ctx, "tenant", 0, "bad", "", "", "", "", false, false, "visitor-1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListPublicRoadmap(invalid cursor) error = %v, want %v", err, ErrValidation)
	}
	fake.publicListErr = nil
	fake.publicListResult = repo.PublicRequestListResult{Policy: repo.Policy{PortalAccessMode: repo.AccessModeDisabled}}
	if _, err := service.ListPublicRequests(ctx, "tenant", 0, "", "", "", "", "", false, false, "visitor-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListPublicRequests(hidden policy) error = %v, want %v", err, ErrNotFound)
	}
	fake.publicListErr = boom
	if _, err := service.ListPublicRequests(ctx, "tenant", 0, "", "", "", "", "", false, false, "visitor-1"); !errors.Is(err, boom) {
		t.Fatalf("ListPublicRequests(repo error) error = %v, want %v", err, boom)
	}
	fake.candidateErr = nil
	fake.candidate = ptrext.Of(repo.PublicRequestCandidate{
		Policy: repo.Policy{
			TenantID:         "tenant-a",
			PortalAccessMode: repo.AccessModePublic,
			RequestsEnabled:  true,
		},
		Profile: repo.RequestProfile{
			PublicSlug:       "title-only",
			PublicTitle:      "Title Only",
			IncludedInPortal: true,
		},
		Moderation: repo.ModerationSubject{
			State: repo.ModerationStateApproved,
		},
		CustomerRequestLive: true,
	})
	fake.publicListErr = repo.ErrInvalidInput
	if _, err := service.GetPublicRequest(ctx, "tenant", "title-only", "visitor-1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("GetPublicRequest(similar invalid input) error = %v, want %v", err, ErrValidation)
	}
	fake.publicListErr = boom
	if _, err := service.GetPublicRequest(ctx, "tenant", "title-only", "visitor-1"); !errors.Is(err, boom) {
		t.Fatalf("GetPublicRequest(similar repo error) error = %v, want %v", err, boom)
	}
}

func TestGetPublicRequestIncludesCommentsAndCanComment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.New()
	commentID := uuid.New()
	fake := ptrext.Of(fakePublicRepo{
		candidate: ptrext.Of(repo.PublicRequestCandidate{
			Policy: repo.Policy{
				TenantID:              "tenant-a",
				PortalAccessMode:      repo.AccessModePublic,
				RequestsEnabled:       true,
				CommentsEnabled:       true,
				CommentWriteMode:      repo.WriteModeIdentified,
				VoteWriteMode:         repo.WriteModeAnonymous,
				DefaultCommentState:   repo.ModerationStatePending,
				SubmitterIdentityMode: repo.IdentityModeDisplayName,
				ShowCommentCount:      true,
				ShowSubmitterDisplay:  true,
			},
			Profile: repo.RequestProfile{
				ID:               requestID,
				TenantID:         "tenant-a",
				RequestID:        requestID,
				PublicSlug:       "pricing-api",
				PublicTitle:      "Pricing API",
				PublicSummary:    "Public summary",
				PublicState:      "planned",
				RoadmapColumn:    "next",
				IncludedInPortal: true,
			},
			Moderation: repo.ModerationSubject{
				State: repo.ModerationStateApproved,
			},
			CustomerRequestLive: true,
		}),
		publicRequestCommentsResult: []repo.PublicRequestComment{{
			ID:                 commentID,
			Body:               "Great idea",
			SubmittedByDisplay: "Portal visitor",
			State:              repo.ModerationStatePending,
			CreatedAt:          time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC),
		}},
	})
	service := ptrext.Of(Service{repo: fake})

	result, err := service.GetPublicRequest(ctx, "tenant-a", "pricing-api", "visitor-1")
	if err != nil {
		t.Fatalf("GetPublicRequest() error = %v", err)
	}
	if fake.publicRequestCommentsViewerSubjectKey != "portal:visitor-1" {
		t.Fatalf("viewer subject key = %q, want portal visitor", fake.publicRequestCommentsViewerSubjectKey)
	}
	if fake.publicRequestCommentsTenantSlug != "tenant-a" || fake.publicRequestCommentsPublicSlug != "pricing-api" {
		t.Fatalf("comment lookup = %q/%q, want tenant-a/pricing-api", fake.publicRequestCommentsTenantSlug, fake.publicRequestCommentsPublicSlug)
	}
	if !result.CanComment || len(result.CommentItems) != 1 || result.CommentItems[0].ID != commentID {
		t.Fatalf("GetPublicRequest() = %#v, want comment thread and comment write enabled", result)
	}
	if result.CommentItems[0].State != repo.ModerationStatePending {
		t.Fatalf("GetPublicRequest() comment state = %q, want pending", result.CommentItems[0].State)
	}
}

func TestGetPublicRequestHidesCommentsWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fake := ptrext.Of(fakePublicRepo{
		candidate: ptrext.Of(repo.PublicRequestCandidate{
			Policy: repo.Policy{
				TenantID:              "tenant-a",
				PortalAccessMode:      repo.AccessModePublic,
				RequestsEnabled:       true,
				CommentsEnabled:       false,
				CommentWriteMode:      repo.WriteModeIdentified,
				SubmitterIdentityMode: repo.IdentityModeDisplayName,
				ShowCommentCount:      true,
			},
			Profile: repo.RequestProfile{
				PublicSlug:       "pricing-api",
				PublicTitle:      "Pricing API",
				PublicSummary:    "Public summary",
				PublicState:      "planned",
				RoadmapColumn:    "next",
				IncludedInPortal: true,
			},
			Moderation: repo.ModerationSubject{
				State: repo.ModerationStateApproved,
			},
			CustomerRequestLive: true,
		}),
		publicRequestCommentsResult: []repo.PublicRequestComment{{
			ID:                 uuid.New(),
			Body:               "Should not load",
			SubmittedByDisplay: "Portal visitor",
			State:              repo.ModerationStatePending,
		}},
	})
	service := ptrext.Of(Service{repo: fake})

	result, err := service.GetPublicRequest(ctx, "tenant-a", "pricing-api", "visitor-1")
	if err != nil {
		t.Fatalf("GetPublicRequest() error = %v", err)
	}
	if fake.publicRequestCommentsTenantSlug != "" || fake.publicRequestCommentsPublicSlug != "" {
		t.Fatalf("GetPublicRequest() unexpectedly loaded comments: %#v", fake)
	}
	if len(result.CommentItems) != 0 || result.CanComment {
		t.Fatalf("GetPublicRequest() = %#v, want comments hidden and write disabled", result)
	}
}

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()

	policy := defaultPolicy("tenant-a")
	if policy.TenantID != "tenant-a" || policy.PortalAccessMode != repo.AccessModeDisabled {
		t.Fatalf("defaultPolicy() = %#v, want disabled tenant policy", policy)
	}
	if policy.SubmissionWriteMode != repo.WriteModeDisabled || policy.DefaultRequestState != repo.ModerationStatePending {
		t.Fatalf("defaultPolicy() = %#v, want disabled writes and pending defaults", policy)
	}
	if !policy.ShowVoteCount || !policy.ShowCommentCount || policy.ShowSubmitterDisplay {
		t.Fatalf("defaultPolicy() = %#v, want conservative visibility defaults", policy)
	}
	if policy.PortalSubmissionForm.Headline != "Send feedback" || !policy.PortalSubmissionForm.ShowPageURL {
		t.Fatalf("defaultPolicy() = %#v, want default portal submission form", policy)
	}
}

func TestPublicRequestFromCandidate(t *testing.T) {
	t.Parallel()

	requestID := uuid.New()
	candidate := repo.PublicRequestCandidate{
		Policy: repo.Policy{
			TenantID:              "tenant-a",
			SearchIndexingEnabled: false,
			SubmitterIdentityMode: repo.IdentityModeDisplayName,
			CommentsEnabled:       true,
			CommentWriteMode:      repo.WriteModeIdentified,
			ShowVoteCount:         true,
			ShowSubmitterDisplay:  true,
		},
		Profile: repo.RequestProfile{
			RequestID:     requestID,
			PublicSlug:    "pricing-api",
			PublicTitle:   "Pricing API",
			PublicState:   "planned",
			RoadmapColumn: "next",
		},
		VoteCount:        12,
		CommentCount:     3,
		SubmitterDisplay: "Ada",
	}

	assertPublicRequestFromCandidateBase(t, publicRequestFromCandidate(candidate), requestID)

	candidate.Policy.SubmitterIdentityMode = repo.IdentityModeAnonymous
	assertPublicRequestFromCandidateHiddenSubmitter(t, publicRequestFromCandidate(candidate), "anonymous identity")
	candidate.Policy.SubmitterIdentityMode = repo.IdentityModeDisplayName
	candidate.Policy.ShowSubmitterDisplay = false
	assertPublicRequestFromCandidateHiddenSubmitter(t, publicRequestFromCandidate(candidate), "hidden display")

	listCandidate := repo.PublicRequestListCandidate{
		Profile:          candidate.Profile,
		VoteCount:        5,
		CommentCount:     1,
		SubmitterDisplay: "Grace",
		ViewerHasVoted:   true,
	}
	candidate.Policy.ShowSubmitterDisplay = true
	assertPublicRequestFromListCandidate(t, publicRequestFromListCandidate(candidate.Policy, listCandidate))
}

func assertPublicRequestFromCandidateBase(t *testing.T, got PublicRequest, requestID uuid.UUID) {
	t.Helper()

	if got.Summary.RequestID != requestID || got.Summary.PublicSlug != "pricing-api" {
		t.Fatalf("publicRequestFromCandidate() = %#v, want profile summary", got)
	}
	if got.Policy.TenantID != "tenant-a" || got.Votes != 12 || got.Comments != 3 {
		t.Fatalf("publicRequestFromCandidate() = %#v, want policy and counts", got)
	}
	if got.SubmitterDisplay != "Ada" || !got.NoIndex || !got.CanComment {
		t.Fatalf("publicRequestFromCandidate() = %#v, want submitter display and noindex", got)
	}
}

func assertPublicRequestFromCandidateHiddenSubmitter(t *testing.T, got PublicRequest, scenario string) {
	t.Helper()

	if got.SubmitterDisplay != "" {
		t.Fatalf("publicRequestFromCandidate(%s) = %#v, want hidden submitter", scenario, got)
	}
}

func assertPublicRequestFromListCandidate(t *testing.T, got PublicRequest) {
	t.Helper()

	if got.Votes != 5 || got.Comments != 1 || got.SubmitterDisplay != "Grace" || !got.CanComment {
		t.Fatalf("publicRequestFromListCandidate() = %#v, want counts and submitter display", got)
	}
	if !got.ViewerHasVoted {
		t.Fatalf("publicRequestFromListCandidate() = %#v, want viewer vote", got)
	}
}

func TestActorForAuditDefaultsMissingFields(t *testing.T) {
	t.Parallel()

	actor := actorForAudit(auditlogsvc.Actor{})
	if actor.Type != "admin" || actor.ID != "system" {
		t.Fatalf("actorForAudit() = %#v, want fallback actor", actor)
	}

	actor = actorForAudit(auditlogsvc.Actor{Type: "oidc", ID: "user-1"})
	if actor.Type != "oidc" || actor.ID != "user-1" {
		t.Fatalf("actorForAudit() = %#v, want provided actor", actor)
	}
}

func TestAuditFieldHelpers(t *testing.T) {
	t.Parallel()

	policyFields := policyAuditFields(repo.Policy{
		PortalAccessMode:      repo.AccessModePublic,
		SearchIndexingEnabled: true,
		RequestsEnabled:       true,
		SubmissionWriteMode:   repo.WriteModeIdentified,
		DefaultRequestState:   repo.ModerationStateApproved,
		SubmitterIdentityMode: repo.IdentityModeDisplayName,
		ShowVoteCount:         true,
	})
	if policyFields["portal_access_mode"] != repo.AccessModePublic || policyFields["show_vote_count"] != true {
		t.Fatalf("policyAuditFields() = %#v, want public policy fields", policyFields)
	}

	subjectID := uuid.NewString()
	moderationFields := moderationAuditFields(repo.ModerationSubject{
		Surface:    repo.SurfaceRequest,
		SubjectID:  subjectID,
		State:      repo.ModerationStateRejected,
		ReasonCode: "policy",
	})
	if moderationFields["subject_id"] != subjectID || moderationFields["state"] != repo.ModerationStateRejected {
		t.Fatalf("moderationAuditFields() = %#v, want moderation fields", moderationFields)
	}

	requestID := uuid.New()
	profileFields := requestProfileAuditFields(repo.RequestProfile{
		RequestID:         requestID,
		PublicSlug:        "pricing-api",
		PublicTitle:       "Pricing API",
		PublicSummary:     "Summary",
		PublicState:       "planned",
		RoadmapColumn:     "next",
		IncludedInPortal:  true,
		IncludedInRoadmap: false,
	})
	if profileFields["request_id"] != requestID.String() || profileFields["public_title_length"] != 11 {
		t.Fatalf("requestProfileAuditFields() = %#v, want profile fields", profileFields)
	}
}

func TestRecordAuditNoServiceConfigured(t *testing.T) {
	t.Parallel()

	service := New(nil, nil)
	ctx := context.Background()
	policy := defaultPolicy("tenant-a")
	if err := service.recordPolicyAuditTx(ctx, nil, auditlogsvc.Actor{}, repo.Policy{}, policy); err != nil {
		t.Fatalf("recordPolicyAuditTx() error = %v, want nil", err)
	}

	publication := repo.RequestPublication{
		Profile: repo.RequestProfile{
			ID:        uuid.New(),
			TenantID:  "tenant-a",
			RequestID: uuid.New(),
		},
	}
	if err := service.recordRequestProfileAuditTx(ctx, nil, auditlogsvc.Actor{}, nil, publication); err != nil {
		t.Fatalf("recordRequestProfileAuditTx() error = %v, want nil", err)
	}

	before := repo.ModerationSubject{ID: uuid.New(), TenantID: "tenant-a", State: repo.ModerationStatePending}
	after := before
	after.State = repo.ModerationStateApproved
	if err := service.recordModerationAuditTx(ctx, nil, auditlogsvc.Actor{}, ActionApprove, before, after); err != nil {
		t.Fatalf("recordModerationAuditTx() error = %v, want nil", err)
	}
}

func TestRecordAuditWritesEvents(t *testing.T) {
	t.Parallel()

	auditRepo := ptrext.Of(fakeAuditRepo{})
	service := New(nil, auditlogsvc.New(auditRepo))
	ctx := context.Background()
	actor := auditlogsvc.Actor{Type: "admin", ID: "user-1"}

	beforePolicy := defaultPolicy("tenant-a")
	afterPolicy := beforePolicy
	afterPolicy.PortalAccessMode = repo.AccessModePublic
	if err := service.recordPolicyAuditTx(ctx, nil, actor, beforePolicy, afterPolicy); err != nil {
		t.Fatalf("recordPolicyAuditTx() error = %v, want nil", err)
	}

	beforePublication := ptrext.Of(repo.RequestPublication{Profile: repo.RequestProfile{
		ID:        uuid.New(),
		TenantID:  "tenant-a",
		RequestID: uuid.New(),
	}})
	afterPublication := ptrext.Indirect(beforePublication)
	afterPublication.Profile.PublicSlug = "pricing-api"
	if err := service.recordRequestProfileAuditTx(ctx, nil, actor, beforePublication, afterPublication); err != nil {
		t.Fatalf("recordRequestProfileAuditTx() error = %v, want nil", err)
	}

	beforeSubject := repo.ModerationSubject{
		ID:        uuid.New(),
		TenantID:  "tenant-a",
		Surface:   repo.SurfaceRequest,
		SubjectID: "request-1",
		State:     repo.ModerationStatePending,
	}
	afterSubject := beforeSubject
	afterSubject.State = repo.ModerationStateApproved
	if err := service.recordModerationAuditTx(ctx, nil, actor, ActionApprove, beforeSubject, afterSubject); err != nil {
		t.Fatalf("recordModerationAuditTx() error = %v, want nil", err)
	}
	if len(auditRepo.entries) != 3 {
		t.Fatalf("record audit entries = %d, want 3", len(auditRepo.entries))
	}
	if auditRepo.entries[0].Action != "public_policy.update" ||
		auditRepo.entries[1].Action != "public_request_profile.upsert" ||
		auditRepo.entries[2].Action != "moderation.approve" {
		t.Fatalf("record audit actions = %#v, want public visibility audit actions", auditRepo.entries)
	}
}

type fakeAuditRepo struct {
	entries []auditlogrepo.Entry
	err     error
}

func (r *fakeAuditRepo) Insert(context.Context, auditlogrepo.Entry) error {
	return r.err
}

func (r *fakeAuditRepo) InsertTx(_ context.Context, _ pgx.Tx, entry auditlogrepo.Entry) error {
	r.entries = append(r.entries, entry)
	return r.err
}

func (r *fakeAuditRepo) List(context.Context, auditlogrepo.ListFilter) (auditlogrepo.ListResult, error) {
	return auditlogrepo.ListResult{}, r.err
}

func (r *fakeAuditRepo) PruneBefore(context.Context, time.Time) (int64, error) {
	return 0, r.err
}

func servicePublication(requestID uuid.UUID) repo.RequestPublication {
	profileID := uuid.New()
	return repo.RequestPublication{
		Profile: repo.RequestProfile{
			ID:               profileID,
			TenantID:         "tenant-a",
			RequestID:        requestID,
			PublicSlug:       "pricing-api",
			PublicTitle:      "Pricing API",
			IncludedInPortal: true,
		},
		Moderation: repo.ModerationSubject{
			ID:        uuid.New(),
			TenantID:  "tenant-a",
			Surface:   repo.SurfaceRequest,
			SubjectID: profileID.String(),
			State:     repo.ModerationStateApproved,
		},
	}
}

type fakePublicRepo struct {
	policy                                *repo.Policy
	policyErr                             error
	policyTenant                          string
	listResult                            repo.ListResult
	listErr                               error
	listFilter                            repo.ListFilter
	publication                           *repo.RequestPublication
	publicationErr                        error
	candidate                             *repo.PublicRequestCandidate
	candidateErr                          error
	publicRequestViewerSubjectKey         string
	publicListResult                      repo.PublicRequestListResult
	publicListErr                         error
	publicListFilter                      repo.PublicRequestListFilter
	publicRequestCommentsResult           []repo.PublicRequestComment
	publicRequestCommentsErr              error
	publicRequestCommentsTenantSlug       string
	publicRequestCommentsPublicSlug       string
	publicRequestCommentsViewerSubjectKey string
}

func (r *fakePublicRepo) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

func (r *fakePublicRepo) GetPolicy(_ context.Context, tenantID string) (*repo.Policy, error) {
	r.policyTenant = tenantID
	return r.policy, r.policyErr
}

func (r *fakePublicRepo) ListSubjects(_ context.Context, filter repo.ListFilter) (repo.ListResult, error) {
	r.listFilter = filter
	return r.listResult, r.listErr
}

func (r *fakePublicRepo) GetRequestPublication(
	context.Context,
	string,
	uuid.UUID,
) (*repo.RequestPublication, error) {
	return r.publication, r.publicationErr
}

func (r *fakePublicRepo) UpsertPolicyTx(context.Context, pgx.Tx, repo.Policy) (*repo.Policy, error) {
	return nil, errors.New("not implemented")
}

func (r *fakePublicRepo) UpsertRequestPublicationTx(
	context.Context,
	pgx.Tx,
	repo.RequestProfile,
	repo.ModerationState,
	string,
	string,
) (*repo.RequestPublication, error) {
	return nil, errors.New("not implemented")
}

func (r *fakePublicRepo) GetSubjectForUpdateTx(
	context.Context,
	pgx.Tx,
	string,
	uuid.UUID,
) (*repo.ModerationSubject, error) {
	return nil, errors.New("not implemented")
}

func (r *fakePublicRepo) UpdateSubjectStateTx(
	context.Context,
	pgx.Tx,
	string,
	uuid.UUID,
	repo.ModerationState,
	string,
	string,
	string,
	time.Time,
) (*repo.ModerationSubject, error) {
	return nil, errors.New("not implemented")
}

func (r *fakePublicRepo) CreateModerationSubjectTx(context.Context, pgx.Tx, repo.ModerationSubject) (*repo.ModerationSubject, error) {
	return nil, errors.New("not implemented")
}

func (r *fakePublicRepo) GetPublicRequestCandidate(
	_ context.Context,
	_ string,
	_ string,
	viewerSubjectKey string,
) (*repo.PublicRequestCandidate, error) {
	r.publicRequestViewerSubjectKey = viewerSubjectKey
	return r.candidate, r.candidateErr
}

func (r *fakePublicRepo) ListPublicRequestCandidates(
	_ context.Context,
	filter repo.PublicRequestListFilter,
) (repo.PublicRequestListResult, error) {
	r.publicListFilter = filter
	return r.publicListResult, r.publicListErr
}

func (r *fakePublicRepo) ListPublicRequestComments(
	_ context.Context,
	tenantSlug string,
	publicSlug string,
	viewerSubjectKey string,
) ([]repo.PublicRequestComment, error) {
	r.publicRequestCommentsTenantSlug = tenantSlug
	r.publicRequestCommentsPublicSlug = publicSlug
	r.publicRequestCommentsViewerSubjectKey = viewerSubjectKey
	return r.publicRequestCommentsResult, r.publicRequestCommentsErr
}

func (r *fakePublicRepo) AddPublicRequestVoteTx(context.Context, pgx.Tx, string, uuid.UUID, string, string, string, string) error {
	return errors.New("not implemented")
}

func (r *fakePublicRepo) RemovePublicRequestVoteTx(context.Context, pgx.Tx, string, uuid.UUID, string) error {
	return errors.New("not implemented")
}

func (r *fakePublicRepo) AddPublicRequestCommentTx(context.Context, pgx.Tx, string, uuid.UUID, string, string, string, string, string) (*repo.PublicRequestComment, error) {
	return nil, errors.New("not implemented")
}
