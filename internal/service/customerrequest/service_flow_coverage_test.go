// ptrext:file-allow customer request service flow tests use pointer-valued fixtures.
// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestCustomerRequestServiceUpdateSuccessTriggersSinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000001")
	shipped := repo.StatusShipped
	fakeRepo := newFlowRequestRepo(requestID)
	fakeRepo.updateBefore = ptrext.Of(customerRequestFlowSummary(requestID, repo.StatusOpen))
	fakeRepo.updateAfter = ptrext.Of(customerRequestFlowSummary(requestID, repo.StatusShipped))
	notifications := &recordingNotificationSink{}
	surveys := &recordingSurveySink{}
	service := &Service{repo: fakeRepo}
	service.SetNotificationSink(notifications)
	service.SetSurveySink(surveys)

	detail, err := service.Update(ctx, UpdateInput{
		TenantID: "tenant-1", ID: requestID, Status: ptrext.Of(shipped), Actor: testCustomerRequestActor(),
	})
	if err != nil || detail.Request.Summary.Status != repo.StatusShipped {
		t.Fatalf("Update() = %+v, %v", detail, err)
	}
	if fakeRepo.tx.commitCalls != 1 {
		t.Fatalf("commit calls = %d, want 1", fakeRepo.tx.commitCalls)
	}
	if len(notifications.changes) != 1 || notifications.changes[0].newStatus != string(repo.StatusShipped) {
		t.Fatalf("notification changes = %+v", notifications.changes)
	}
	if len(surveys.events) != 1 || surveys.events[0].RequestID != requestID {
		t.Fatalf("survey events = %+v", surveys.events)
	}
}

func TestCustomerRequestServiceMutationSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000002")
	linkID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000003")
	fakeRepo := newFlowRequestRepo(requestID)
	service := &Service{repo: fakeRepo}
	actor := testCustomerRequestActor()

	cases := []struct {
		name string
		call func() (*Detail, error)
	}{
		{name: "LinkFeedback", call: func() (*Detail, error) {
			return service.LinkFeedback(ctx, LinkFeedbackInput{TenantID: "tenant-1", RequestID: requestID, FeedbackID: 42, Importance: repo.ImportanceImportant, Actor: actor})
		}},
		{name: "UnlinkFeedback", call: func() (*Detail, error) {
			return service.UnlinkFeedback(ctx, "tenant-1", requestID, 42, actor)
		}},
		{name: "LinkCustomer", call: func() (*Detail, error) {
			return service.LinkCustomer(ctx, LinkCustomerInput{TenantID: "tenant-1", RequestID: requestID, SubjectKey: "user:42", AccountKey: "acct:acme", Actor: actor})
		}},
		{name: "UnlinkCustomer", call: func() (*Detail, error) {
			return service.UnlinkCustomer(ctx, "tenant-1", requestID, linkID, actor)
		}},
		{name: "AddVote", call: func() (*Detail, error) {
			return service.AddVote(ctx, VoteInput{TenantID: "tenant-1", RequestID: requestID, AccountKey: "acct:acme", Weight: 3, Actor: actor})
		}},
		{name: "RemoveVote", call: func() (*Detail, error) {
			return service.RemoveVote(ctx, "tenant-1", requestID, linkID, actor)
		}},
		{name: "AddNote", call: func() (*Detail, error) {
			return service.AddNote(ctx, NoteInput{TenantID: "tenant-1", RequestID: requestID, Body: "Coordinate rollout", Actor: actor})
		}},
		{name: "DeleteNote", call: func() (*Detail, error) {
			return service.DeleteNote(ctx, "tenant-1", requestID, linkID, actor)
		}},
		{name: "LinkIssue", call: func() (*Detail, error) {
			return service.LinkIssue(ctx, LinkIssueInput{TenantID: "tenant-1", RequestID: requestID, Provider: "github", ExternalURL: "https://github.com/Phixsura/attune/issues/235", Actor: actor})
		}},
		{name: "UnlinkIssue", call: func() (*Detail, error) {
			return service.UnlinkIssue(ctx, "tenant-1", requestID, linkID, actor)
		}},
		{name: "RecordIssueSync", call: func() (*Detail, error) {
			return service.RecordIssueSync(ctx, IssueSyncInput{TenantID: "tenant-1", RequestID: requestID, IssueLinkID: linkID, Status: "closed", Actor: actor})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail, err := tc.call()
			if err != nil || detail.Request.Summary.ID != requestID {
				t.Fatalf("%s() = %+v, %v", tc.name, detail, err)
			}
		})
	}
}

func TestCustomerRequestServiceCreateMergeScoringAndReadSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000004")
	sourceID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000005")
	fakeRepo := newFlowRequestRepo(requestID)
	service := &Service{repo: fakeRepo, idempotency: &fakeIdempotencyStore{acquired: true}}

	if list, err := service.List(ctx, ListInput{TenantID: "tenant-1", Limit: 5}); err != nil || len(list.Items) != 1 {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	if summary, err := service.GetAccountSummary(ctx, AccountSummaryInput{TenantID: "tenant-1", AccountKey: " acct:acme "}); err != nil || summary.AccountKey != "acct:acme" {
		t.Fatalf("GetAccountSummary() = %+v, %v", summary, err)
	}
	if settings, err := service.GetScoringSettings(ctx, " tenant-1 "); err != nil || settings.TenantID != "tenant-1" {
		t.Fatalf("GetScoringSettings() = %+v, %v", settings, err)
	}
	if settings, err := service.UpdateScoringSettings(ctx, ScoringSettingsInput{TenantID: "tenant-1", FeedbackWeight: ptrext.Of(7), Actor: testCustomerRequestActor()}); err != nil || settings.FeedbackWeight != 7 {
		t.Fatalf("UpdateScoringSettings() = %+v, %v", settings, err)
	}
	if detail, err := service.Create(ctx, CreateInput{TenantID: "tenant-1", Title: "Export bundles", IdempotencyKey: "create_key_1", Actor: testCustomerRequestActor()}); err != nil || detail.Request.Summary.ID != requestID {
		t.Fatalf("Create() = %+v, %v", detail, err)
	}
	if detail, err := service.Merge(ctx, MergeInput{TenantID: "tenant-1", SourceID: sourceID, TargetID: requestID, IdempotencyKey: "merge_key_1", Actor: testCustomerRequestActor()}); err != nil || detail.Request.Summary.ID != requestID {
		t.Fatalf("Merge() = %+v, %v", detail, err)
	}
}

func TestCustomerRequestServiceGitHubIssueSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000006")
	connectionID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000007")
	mappingID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000008")
	runID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000009")
	objectLinkID := uuid.MustParse("aaaaaaaa-2000-4000-8000-000000000010")
	fakeRepo := newFlowRequestRepo(requestID)
	store := &recordingIssueCreateRunStore{
		resolveTarget: &externalsyncrepo.GitHubIssueLinkTarget{
			MappingID: mappingID, ExternalSyncKey: "Phixsura/attune#235", ExternalKey: "Phixsura/attune#235",
			ExternalURL: "https://github.com/Phixsura/attune/issues/235", Title: "World class console", Status: "open",
		},
		bindResult: &externalsyncrepo.ManagedGitHubIssueLinkBinding{
			ConnectionID: connectionID, MappingID: mappingID, ExternalKey: "Phixsura/attune#235", ExternalObjectLinkID: objectLinkID,
		},
		createResult: &externalsyncrepo.CustomerRequestIssueCreateRunResult{
			Mapping: externalsyncrepo.Mapping{ID: mappingID, ConnectionID: connectionID},
			Run:     externalsyncrepo.SyncRun{ID: runID},
		},
	}
	service := &Service{repo: fakeRepo, issueCreates: store}

	detail, err := service.LinkIssue(ctx, LinkIssueInput{
		TenantID: "tenant-1", RequestID: requestID, Provider: "github",
		ConnectionID: ptrext.Of(connectionID), IssueNumber: "235", Actor: testCustomerRequestActor(),
	})
	if err != nil || detail.Request.Summary.ID != requestID || len(store.pullInputs) != 1 {
		t.Fatalf("managed LinkIssue() = %+v, pullInputs=%+v, err=%v", detail, store.pullInputs, err)
	}
	result, err := service.CreateGitHubIssue(ctx, CreateGitHubIssueInput{
		TenantID: "tenant-1", RequestID: requestID, Actor: testCustomerRequestActor(),
	})
	if err != nil || result.RunID != runID || len(store.createInputs) != 1 {
		t.Fatalf("CreateGitHubIssue() = %+v, createInputs=%+v, err=%v", result, store.createInputs, err)
	}
}

type flowRequestRepo struct {
	requestRepo

	tx           *attrTx
	detail       repo.Detail
	updateBefore *repo.Summary
	updateAfter  *repo.Summary
}

func newFlowRequestRepo(requestID uuid.UUID) *flowRequestRepo {
	summary := customerRequestFlowSummary(requestID, repo.StatusShipped)
	return &flowRequestRepo{
		tx:     &attrTx{},
		detail: repo.Detail{Summary: summary},
	}
}

func (f *flowRequestRepo) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func (f *flowRequestRepo) List(context.Context, repo.ListFilter) (repo.ListResult, error) {
	return repo.ListResult{Items: []repo.Summary{f.detail.Summary}}, nil
}

func (f *flowRequestRepo) GetAccountSummary(_ context.Context, filter repo.ListFilter) (repo.AccountSummary, error) {
	return repo.AccountSummary{AccountKey: filter.AccountKey, RequestCount: 1}, nil
}

func (f *flowRequestRepo) GetDetail(context.Context, string, uuid.UUID, int) (*repo.Detail, error) {
	return ptrext.Of(f.detail), nil
}

func (f *flowRequestRepo) GetDetailTx(context.Context, pgx.Tx, string, uuid.UUID, int) (*repo.Detail, error) {
	return ptrext.Of(f.detail), nil
}

func (f *flowRequestRepo) GetScoringSettings(context.Context, string) (repo.ScoringSettings, error) {
	return repo.DefaultScoringSettings("tenant-1"), nil
}

func (f *flowRequestRepo) UpsertScoringSettingsTx(_ context.Context, _ pgx.Tx, in repo.ScoringSettingsInput) (repo.ScoringSettings, error) {
	settings := repo.DefaultScoringSettings(in.TenantID)
	settings.FeedbackWeight = in.FeedbackWeight
	settings.UpdatedBy = in.ActorID
	return settings, nil
}

func (f *flowRequestRepo) CreateTx(_ context.Context, _ pgx.Tx, in repo.CreateInput) (*repo.Summary, error) {
	f.detail.Summary.Title = in.Title
	f.detail.Summary.Status = in.Status
	f.detail.Summary.Priority = in.Priority
	return ptrext.Of(f.detail.Summary), nil
}

func (f *flowRequestRepo) UpdateTx(context.Context, pgx.Tx, repo.UpdateInput) (*repo.Summary, *repo.Summary, error) {
	return f.updateBefore, f.updateAfter, nil
}

func (f *flowRequestRepo) MergeTx(_ context.Context, _ pgx.Tx, _ string, sourceID, targetID uuid.UUID, _ string) (repo.MergeResult, error) {
	return repo.MergeResult{SourceID: sourceID, TargetID: targetID, SourceDisplayID: "CR-1", TargetDisplayID: "CR-2", MovedFeedbackCount: 1}, nil
}

func (f *flowRequestRepo) LinkFeedbackTx(context.Context, pgx.Tx, repo.LinkFeedbackInput) error {
	return nil
}

func (f *flowRequestRepo) UnlinkFeedbackTx(context.Context, pgx.Tx, string, uuid.UUID, int64, string) error {
	return nil
}

func (f *flowRequestRepo) LinkCustomerTx(_ context.Context, _ pgx.Tx, in repo.CustomerLinkInput) (*repo.CustomerLink, error) {
	return ptrext.Of(repo.CustomerLink{ID: uuid.New(), SubjectKey: in.SubjectKey, AccountKey: in.AccountKey, CreatedBy: in.ActorID}), nil
}

func (f *flowRequestRepo) UnlinkCustomerTx(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID, string) (*repo.CustomerLink, error) {
	return ptrext.Of(repo.CustomerLink{ID: uuid.New(), SubjectKey: "user:42", AccountKey: "acct:acme"}), nil
}

func (f *flowRequestRepo) AddVoteTx(_ context.Context, _ pgx.Tx, in repo.VoteInput) (*repo.Vote, error) {
	return ptrext.Of(repo.Vote{ID: uuid.New(), AccountKey: in.AccountKey, Weight: in.Weight, CreatedBy: in.ActorID}), nil
}

func (f *flowRequestRepo) RemoveVoteTx(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID, string) (*repo.Vote, error) {
	return ptrext.Of(repo.Vote{ID: uuid.New(), AccountKey: "acct:acme", Weight: 3}), nil
}

func (f *flowRequestRepo) AddNoteTx(_ context.Context, _ pgx.Tx, in repo.NoteInput) (*repo.Note, error) {
	return ptrext.Of(repo.Note{ID: uuid.New(), Body: in.Body, CreatedBy: in.ActorID}), nil
}

func (f *flowRequestRepo) DeleteNoteTx(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID, string) (*repo.Note, error) {
	return ptrext.Of(repo.Note{ID: uuid.New(), Body: "Coordinate rollout"}), nil
}

func (f *flowRequestRepo) LinkIssueTx(_ context.Context, _ pgx.Tx, in repo.IssueLinkInput) (*repo.IssueLink, error) {
	return ptrext.Of(repo.IssueLink{ID: uuid.New(), Provider: in.Provider, ExternalKey: in.ExternalKey, ExternalURL: in.ExternalURL, Title: in.Title, Status: in.Status}), nil
}

func (f *flowRequestRepo) UnlinkIssueTx(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID, string) (*repo.IssueLink, error) {
	return ptrext.Of(repo.IssueLink{ID: uuid.New(), Provider: "github", ExternalKey: "Phixsura/attune#235", ExternalURL: "https://github.com/Phixsura/attune/issues/235"}), nil
}

func (f *flowRequestRepo) RecordIssueSyncTx(_ context.Context, _ pgx.Tx, in repo.IssueSyncInput) (*repo.IssueLink, error) {
	return ptrext.Of(repo.IssueLink{ID: in.IssueLinkID, Provider: "github", Status: in.Status, SyncState: in.SyncState}), nil
}

func (f *flowRequestRepo) BindIssueExternalObjectLinkTx(_ context.Context, _ pgx.Tx, _ string, _ uuid.UUID, issueLinkID, externalObjectLinkID uuid.UUID) (*repo.IssueLink, error) {
	return ptrext.Of(repo.IssueLink{ID: issueLinkID, Provider: "github", ExternalKey: "Phixsura/attune#235", ExternalObjectLinkID: ptrext.Of(externalObjectLinkID)}), nil
}

func customerRequestFlowSummary(requestID uuid.UUID, status repo.Status) repo.Summary {
	return repo.Summary{
		ID: requestID, TenantID: "tenant-1", DisplayID: "CR-235", Title: "World class console",
		Status: status, Priority: repo.PriorityHigh, RevenueCurrency: "USD",
		CreatedAt: time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
}

type recordingNotificationSink struct {
	changes []struct {
		oldStatus string
		newStatus string
	}
}

func (s *recordingNotificationSink) RecordStatusChangeTx(
	context.Context,
	pgx.Tx,
	string,
	uuid.UUID,
	string,
	string,
	auditlogsvc.Actor,
) error {
	s.changes = append(s.changes, struct {
		oldStatus string
		newStatus string
	}{oldStatus: string(repo.StatusOpen), newStatus: string(repo.StatusShipped)})
	return nil
}

type recordingSurveySink struct {
	events []SurveyRequestResolvedEvent
}

func (s *recordingSurveySink) RecordRequestResolved(_ context.Context, event SurveyRequestResolvedEvent) (int, error) {
	s.events = append(s.events, event)
	return 1, nil
}
