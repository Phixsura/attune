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
	if _, err := service.GetPublicRequest(ctx, " ", "slug"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(empty tenant) error = %v, want %v", err, ErrNotFound)
	}
	if _, err := service.GetPublicRequest(ctx, "tenant", " "); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(empty slug) error = %v, want %v", err, ErrNotFound)
	}
}

func TestServiceReadMethodsUseRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.New()
	policy := defaultPolicy("tenant-a")
	policy.PortalAccessMode = repo.AccessModePublic
	policy.SearchIndexingEnabled = true
	policy.RequestsEnabled = true
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
			CustomerRequestLive: true,
		}),
	})
	service := ptrext.Of(Service{repo: fake})

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
	publicRequest, err := service.GetPublicRequest(ctx, " tenant-slug ", " pricing-api ")
	if err != nil || publicRequest.Votes != 7 || publicRequest.SubmitterDisplay != "Ada" {
		t.Fatalf("GetPublicRequest() = %#v, %v", publicRequest, err)
	}
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
	if _, err := service.GetPublicRequest(ctx, "tenant", "slug"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(not found) error = %v, want %v", err, ErrNotFound)
	}
	fake.candidateErr = nil
	fake.candidate = ptrext.Of(repo.PublicRequestCandidate{})
	if _, err := service.GetPublicRequest(ctx, "tenant", "slug"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublicRequest(hidden) error = %v, want %v", err, ErrNotFound)
	}
	fake.candidateErr = boom
	if _, err := service.GetPublicRequest(ctx, "tenant", "slug"); !errors.Is(err, boom) {
		t.Fatalf("GetPublicRequest(repo error) error = %v, want %v", err, boom)
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
}

func TestPublicRequestFromCandidate(t *testing.T) {
	t.Parallel()

	requestID := uuid.New()
	candidate := repo.PublicRequestCandidate{
		Policy: repo.Policy{
			TenantID:              "tenant-a",
			SearchIndexingEnabled: false,
			ShowVoteCount:         true,
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

	got := publicRequestFromCandidate(candidate)

	if got.Summary.RequestID != requestID || got.Summary.PublicSlug != "pricing-api" {
		t.Fatalf("publicRequestFromCandidate() = %#v, want profile summary", got)
	}
	if got.Policy.TenantID != "tenant-a" || got.Votes != 12 || got.Comments != 3 {
		t.Fatalf("publicRequestFromCandidate() = %#v, want policy and counts", got)
	}
	if got.SubmitterDisplay != "Ada" || !got.NoIndex {
		t.Fatalf("publicRequestFromCandidate() = %#v, want submitter display and noindex", got)
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
	policy         *repo.Policy
	policyErr      error
	policyTenant   string
	listResult     repo.ListResult
	listErr        error
	listFilter     repo.ListFilter
	publication    *repo.RequestPublication
	publicationErr error
	candidate      *repo.PublicRequestCandidate
	candidateErr   error
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

func (r *fakePublicRepo) GetPublicRequestCandidate(
	context.Context,
	string,
	string,
) (*repo.PublicRequestCandidate, error) {
	return r.candidate, r.candidateErr
}
