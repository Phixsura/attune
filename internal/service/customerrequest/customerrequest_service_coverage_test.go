// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestCustomerRequestServiceValidationGuards(t *testing.T) {
	ctx := context.Background()
	s := New(nil, nil, nil)
	s.SetNotificationSink(nil)
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000101")
	targetID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000102")
	linkID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000103")
	actor := testCustomerRequestActor()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "List empty tenant", call: func() error {
			_, err := s.List(ctx, ListInput{})
			return err
		}},
		{name: "GetScoringSettings blank tenant", call: func() error {
			_, err := s.GetScoringSettings(ctx, "  ")
			return err
		}},
		{name: "UpdateScoringSettings blank tenant", call: func() error {
			_, err := s.UpdateScoringSettings(ctx, ScoringSettingsInput{TenantID: " "})
			return err
		}},
		{name: "Create invalid", call: func() error {
			_, err := s.Create(ctx, CreateInput{TenantID: "", Title: "", IdempotencyKey: "bad"})
			return err
		}},
		{name: "Update invalid", call: func() error {
			_, err := s.Update(ctx, UpdateInput{TenantID: "", ID: uuid.Nil})
			return err
		}},
		{name: "PromoteFeedback invalid", call: func() error {
			_, err := s.PromoteFeedback(ctx, PromoteInput{TenantID: "tenant-1", Title: "Request", IdempotencyKey: "promote_key_1"})
			return err
		}},
		{name: "LinkFeedback invalid", call: func() error {
			_, err := s.LinkFeedback(ctx, LinkFeedbackInput{TenantID: "", RequestID: requestID, FeedbackID: 1})
			return err
		}},
		{name: "UnlinkFeedback invalid", call: func() error {
			_, err := s.UnlinkFeedback(ctx, "tenant-1", requestID, 0, actor)
			return err
		}},
		{name: "LinkCustomer invalid", call: func() error {
			_, err := s.LinkCustomer(ctx, LinkCustomerInput{TenantID: "tenant-1", RequestID: requestID})
			return err
		}},
		{name: "UnlinkCustomer invalid", call: func() error {
			_, err := s.UnlinkCustomer(ctx, "tenant-1", requestID, uuid.Nil, actor)
			return err
		}},
		{name: "AddVote invalid", call: func() error {
			_, err := s.AddVote(ctx, VoteInput{TenantID: "tenant-1", RequestID: requestID, Weight: 101})
			return err
		}},
		{name: "RemoveVote invalid", call: func() error {
			_, err := s.RemoveVote(ctx, "tenant-1", requestID, uuid.Nil, actor)
			return err
		}},
		{name: "AddNote invalid", call: func() error {
			_, err := s.AddNote(ctx, NoteInput{TenantID: "tenant-1", RequestID: requestID, Body: ""})
			return err
		}},
		{name: "DeleteNote invalid", call: func() error {
			_, err := s.DeleteNote(ctx, "tenant-1", requestID, uuid.Nil, actor)
			return err
		}},
		{name: "Merge invalid same IDs", call: func() error {
			_, err := s.Merge(ctx, MergeInput{TenantID: "tenant-1", SourceID: requestID, TargetID: requestID, IdempotencyKey: "merge_key_1"})
			return err
		}},
		{name: "Merge invalid idempotency key", call: func() error {
			_, err := s.Merge(ctx, MergeInput{TenantID: "tenant-1", SourceID: requestID, TargetID: targetID, IdempotencyKey: "bad"})
			return err
		}},
		{name: "LinkIssue invalid provider", call: func() error {
			_, err := s.LinkIssue(ctx, LinkIssueInput{TenantID: "tenant-1", RequestID: requestID, Provider: "jira"})
			return err
		}},
		{name: "CreateGitHubIssue invalid", call: func() error {
			_, err := s.CreateGitHubIssue(ctx, CreateGitHubIssueInput{TenantID: "tenant-1", RequestID: requestID, Actor: actor})
			return err
		}},
		{name: "UnlinkIssue invalid", call: func() error {
			_, err := s.UnlinkIssue(ctx, "tenant-1", requestID, uuid.Nil, actor)
			return err
		}},
		{name: "RecordIssueSync invalid", call: func() error {
			_, err := s.RecordIssueSync(ctx, IssueSyncInput{TenantID: "tenant-1", RequestID: requestID, IssueLinkID: linkID, SyncState: repo.IssueSyncState("bad")})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrValidation) &&
				!errors.Is(err, ErrUnsupportedProvider) &&
				!errors.Is(err, ErrInvalidIssueURL) {
				t.Fatalf("%s error = %v, want validation error", tc.name, err)
			}
		})
	}
}

func TestCustomerRequestServiceReadMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableCustomerRequestService(t)
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000001")

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "List", call: func() error {
			_, err := s.List(ctx, ListInput{TenantID: "tenant-1"})
			return err
		}},
		{name: "GetScoringSettings", call: func() error {
			_, err := s.GetScoringSettings(ctx, "tenant-1")
			return err
		}},
		{name: "UpdateScoringSettings", call: func() error {
			_, err := s.UpdateScoringSettings(ctx, ScoringSettingsInput{TenantID: "tenant-1", Actor: testCustomerRequestActor()})
			return err
		}},
		{name: "Get", call: func() error {
			_, err := s.Get(ctx, "tenant-1", requestID, 25)
			return err
		}},
	} {
		expectCustomerRequestServiceError(t, tc.name, tc.call)
	}
}

func TestCustomerRequestServiceCreateUpdateAndMergeMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableCustomerRequestService(t)
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000002")
	targetID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000003")
	actor := testCustomerRequestActor()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "Create", call: func() error {
			_, err := s.Create(ctx, CreateInput{
				TenantID: "tenant-1", Title: "Export bundles", IdempotencyKey: "create_key_1", Actor: actor,
			})
			return err
		}},
		{name: "Update", call: func() error {
			_, err := s.Update(ctx, UpdateInput{TenantID: "tenant-1", ID: requestID, Title: ptrext.Of("Renamed request"), Actor: actor})
			return err
		}},
		{name: "PromoteFeedback", call: func() error {
			_, err := s.PromoteFeedback(ctx, PromoteInput{
				TenantID: "tenant-1", FeedbackIDs: []int64{42}, Title: "Promoted request",
				IdempotencyKey: "promote_key_1", Actor: actor,
			})
			return err
		}},
		{name: "Merge", call: func() error {
			_, err := s.Merge(ctx, MergeInput{
				TenantID: "tenant-1", SourceID: requestID, TargetID: targetID, IdempotencyKey: "merge_key_1", Actor: actor,
			})
			return err
		}},
	} {
		expectCustomerRequestServiceError(t, tc.name, tc.call)
	}
}

func TestCustomerRequestServiceFeedbackCustomerVoteAndNoteMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableCustomerRequestService(t)
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000004")
	linkID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000005")
	actor := testCustomerRequestActor()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "LinkFeedback", call: func() error {
			_, err := s.LinkFeedback(ctx, LinkFeedbackInput{
				TenantID: "tenant-1", RequestID: requestID, FeedbackID: 42, Importance: repo.ImportanceImportant, Actor: actor,
			})
			return err
		}},
		{name: "UnlinkFeedback", call: func() error {
			_, err := s.UnlinkFeedback(ctx, "tenant-1", requestID, 42, actor)
			return err
		}},
		{name: "LinkCustomer", call: func() error {
			_, err := s.LinkCustomer(ctx, LinkCustomerInput{
				TenantID: "tenant-1", RequestID: requestID, SubjectKey: "user:42", SubjectDisplay: "Ada", Actor: actor,
			})
			return err
		}},
		{name: "UnlinkCustomer", call: func() error {
			_, err := s.UnlinkCustomer(ctx, "tenant-1", requestID, linkID, actor)
			return err
		}},
		{name: "AddVote", call: func() error {
			_, err := s.AddVote(ctx, VoteInput{
				TenantID: "tenant-1", RequestID: requestID, AccountKey: "account:acme", Weight: 3, Actor: actor,
			})
			return err
		}},
		{name: "RemoveVote", call: func() error {
			_, err := s.RemoveVote(ctx, "tenant-1", requestID, linkID, actor)
			return err
		}},
		{name: "AddNote", call: func() error {
			_, err := s.AddNote(ctx, NoteInput{TenantID: "tenant-1", RequestID: requestID, Body: "Coordinate rollout", Actor: actor})
			return err
		}},
		{name: "DeleteNote", call: func() error {
			_, err := s.DeleteNote(ctx, "tenant-1", requestID, linkID, actor)
			return err
		}},
	} {
		expectCustomerRequestServiceError(t, tc.name, tc.call)
	}
}

func TestCustomerRequestServiceIssueMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableCustomerRequestService(t)
	s.SetIssueCreateRunStore(fakeIssueCreateRunStore{})
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000006")
	linkID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000007")
	actor := testCustomerRequestActor()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "LinkIssue", call: func() error {
			_, err := s.LinkIssue(ctx, LinkIssueInput{
				TenantID: "tenant-1", RequestID: requestID, Provider: "github",
				ExternalURL: "https://github.com/Phixsura/attune/issues/224", Title: "Request notifications", Actor: actor,
			})
			return err
		}},
		{name: "CreateGitHubIssue", call: func() error {
			_, err := s.CreateGitHubIssue(ctx, CreateGitHubIssueInput{TenantID: "tenant-1", RequestID: requestID, Actor: actor})
			return err
		}},
		{name: "UnlinkIssue", call: func() error {
			_, err := s.UnlinkIssue(ctx, "tenant-1", requestID, linkID, actor)
			return err
		}},
		{name: "RecordIssueSync", call: func() error {
			_, err := s.RecordIssueSync(ctx, IssueSyncInput{
				TenantID: "tenant-1", RequestID: requestID, IssueLinkID: linkID,
				SyncState: repo.IssueSyncStateSynced, Status: "open", Actor: actor,
			})
			return err
		}},
	} {
		expectCustomerRequestServiceError(t, tc.name, tc.call)
	}
}

func TestCustomerRequestServiceRecordAuditTxRecordsDefaults(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000201")
	ownerID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000203")
	writer := ptrext.Of(fakeCustomerRequestAuditWriter{})
	s := New(nil, nil, auditlogsvc.New(writer))
	summary := repo.Summary{
		ID:            requestID,
		TenantID:      "tenant-1",
		DisplayID:     "CR-42",
		Title:         "Export bundles",
		Status:        repo.StatusOpen,
		Priority:      repo.PriorityHigh,
		OwnerMemberID: ptrext.Of(ownerID),
	}

	if err := s.recordAuditTx(ctx, nil, auditlogsvc.Actor{}, "customer_request.create", summary, "Created", map[string]any{"ok": true}); err != nil {
		t.Fatalf("recordAuditTx returned error: %v", err)
	}
	if len(writer.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(writer.entries))
	}
	entry := writer.entries[0]
	if entry.ActorType != "admin" || entry.ActorID != "system" || entry.TargetID != requestID.String() {
		t.Fatalf("create audit entry = %+v", entry)
	}
}

func TestCustomerRequestServiceRecordAuditRecordsEntry(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000205")
	writer := ptrext.Of(fakeCustomerRequestAuditWriter{})
	s := New(nil, nil, auditlogsvc.New(writer))
	summary := repo.Summary{
		ID:       requestID,
		TenantID: "tenant-1",
	}

	if err := s.recordAudit(ctx, auditlogsvc.Actor{Type: "user", ID: "user-1"}, "customer_request.create_github_issue", summary, "Queued issue", map[string]any{"provider": "github"}); err != nil {
		t.Fatalf("recordAudit returned error: %v", err)
	}
	if len(writer.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(writer.entries))
	}
	entry := writer.entries[0]
	if entry.Action != "customer_request.create_github_issue" || entry.ActorID != "user-1" || entry.TargetID != requestID.String() {
		t.Fatalf("audit entry = %+v", entry)
	}
}

func TestCustomerRequestServiceRecordMergeAuditTxRecordsCounts(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000201")
	sourceID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000202")
	writer := ptrext.Of(fakeCustomerRequestAuditWriter{})
	s := New(nil, nil, auditlogsvc.New(writer))

	merge := repo.MergeResult{
		SourceID:                      sourceID,
		TargetID:                      requestID,
		SourceDisplayID:               "CR-41",
		TargetDisplayID:               "CR-42",
		MovedFeedbackCount:            2,
		MovedCustomerCount:            1,
		SkippedDuplicateFeedbackCount: 3,
	}
	if err := s.recordMergeAuditTx(ctx, nil, "tenant-1", auditlogsvc.Actor{}, merge); err != nil {
		t.Fatalf("recordMergeAuditTx returned error: %v", err)
	}
	entry := writer.entries[0]
	if entry.Action != "customer_request.merge" || entry.Summary != "Merged CR-41 into CR-42" {
		t.Fatalf("merge audit entry = %+v", entry)
	}
	var mergeAfter map[string]any
	if err := json.Unmarshal(entry.AfterJSON, &mergeAfter); err != nil {
		t.Fatalf("merge after JSON = %s: %v", string(entry.AfterJSON), err)
	}
	if mergeAfter["moved_feedback_count"] != float64(2) || mergeAfter["skipped_duplicate_feedback_count"] != float64(3) {
		t.Fatalf("merge after = %#v", mergeAfter)
	}
}

func TestCustomerRequestServiceRecordScoringSettingsAuditTxRecordsBeforeAfter(t *testing.T) {
	ctx := context.Background()
	writer := ptrext.Of(fakeCustomerRequestAuditWriter{})
	s := New(nil, nil, auditlogsvc.New(writer))
	before := repo.ScoringSettings{PriorityHighWeight: 20, RevenueCentsPerPoint: 1000}
	after := repo.ScoringSettings{PriorityHighWeight: 30, RevenueCentsPerPoint: 2000}
	if err := s.recordScoringSettingsAuditTx(ctx, nil, "tenant-1", auditlogsvc.Actor{Type: "user", ID: "admin-1"}, before, after); err != nil {
		t.Fatalf("recordScoringSettingsAuditTx returned error: %v", err)
	}
	entry := writer.entries[0]
	if entry.Action != "customer_request.update_scoring_settings" || entry.TargetID != "tenant-1" {
		t.Fatalf("settings audit entry = %+v", entry)
	}
	var beforeJSON map[string]any
	var afterJSON map[string]any
	if err := json.Unmarshal(entry.BeforeJSON, &beforeJSON); err != nil {
		t.Fatalf("settings before JSON = %s: %v", string(entry.BeforeJSON), err)
	}
	if err := json.Unmarshal(entry.AfterJSON, &afterJSON); err != nil {
		t.Fatalf("settings after JSON = %s: %v", string(entry.AfterJSON), err)
	}
	if beforeJSON["priority_high_weight"] != float64(20) || afterJSON["priority_high_weight"] != float64(30) {
		t.Fatalf("settings before/after = %#v / %#v", beforeJSON, afterJSON)
	}
}

func TestCustomerRequestServiceAuditWritersSkipWhenAuditDisabled(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000204")
	s := New(nil, nil, nil)

	if err := s.recordAuditTx(ctx, nil, auditlogsvc.Actor{}, "customer_request.create", repo.Summary{ID: requestID, TenantID: "tenant-1"}, "Created", nil); err != nil {
		t.Fatalf("recordAuditTx without audit returned error: %v", err)
	}
	if err := s.recordAudit(ctx, auditlogsvc.Actor{}, "customer_request.create", repo.Summary{ID: requestID, TenantID: "tenant-1"}, "Created", nil); err != nil {
		t.Fatalf("recordAudit without audit returned error: %v", err)
	}
	if err := s.recordMergeAuditTx(ctx, nil, "tenant-1", auditlogsvc.Actor{}, repo.MergeResult{SourceID: requestID, TargetID: requestID}); err != nil {
		t.Fatalf("recordMergeAuditTx without audit returned error: %v", err)
	}
	if err := s.recordScoringSettingsAuditTx(ctx, nil, "tenant-1", auditlogsvc.Actor{}, repo.ScoringSettings{}, repo.ScoringSettings{}); err != nil {
		t.Fatalf("recordScoringSettingsAuditTx without audit returned error: %v", err)
	}
}

func newUnreachableCustomerRequestService(t *testing.T) *Service {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return New(repo.New(pool), ptrext.Of(fakeIdempotencyStore{acquired: true}), nil)
}

func testCustomerRequestActor() auditlogsvc.Actor {
	return auditlogsvc.Actor{Type: "user", ID: "user-1", Email: "ops@example.test"}
}

func expectCustomerRequestServiceError(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want repo error", name)
	}
}

type fakeIssueCreateRunStore struct{}

func (fakeIssueCreateRunStore) ResolveGitHubIssueLinkTarget(
	context.Context,
	externalsyncrepo.GitHubIssueLinkTargetInput,
) (*externalsyncrepo.GitHubIssueLinkTarget, error) {
	return nil, errors.New("unexpected issue link target store call")
}

func (fakeIssueCreateRunStore) BindManagedGitHubIssueLinkTx(
	context.Context,
	pgx.Tx,
	externalsyncrepo.ManagedGitHubIssueLinkInput,
) (*externalsyncrepo.ManagedGitHubIssueLinkBinding, error) {
	return nil, errors.New("unexpected issue link bind store call")
}

func (fakeIssueCreateRunStore) TombstoneLocalIssueExternalLinkTx(
	context.Context,
	pgx.Tx,
	string,
	uuid.UUID,
	uuid.UUID,
) error {
	return errors.New("unexpected issue link tombstone store call")
}

func (fakeIssueCreateRunStore) CreateCustomerRequestIssueRun(
	context.Context,
	externalsyncrepo.CustomerRequestIssueCreateRunInput,
) (*externalsyncrepo.CustomerRequestIssueCreateRunResult, error) {
	return nil, errors.New("unexpected issue create run store call")
}

func (fakeIssueCreateRunStore) CreateCustomerRequestIssuePullRun(
	context.Context,
	externalsyncrepo.CustomerRequestIssuePullRunInput,
) (*externalsyncrepo.CustomerRequestIssuePullRunResult, error) {
	return nil, errors.New("unexpected issue pull run store call")
}

type fakeCustomerRequestAuditWriter struct {
	entries []auditlogrepo.Entry
}

func (w *fakeCustomerRequestAuditWriter) Insert(_ context.Context, entry auditlogrepo.Entry) error {
	w.entries = append(w.entries, entry)
	return nil
}

func (w *fakeCustomerRequestAuditWriter) InsertTx(_ context.Context, _ pgx.Tx, entry auditlogrepo.Entry) error {
	w.entries = append(w.entries, entry)
	return nil
}

func (w *fakeCustomerRequestAuditWriter) List(context.Context, auditlogrepo.ListFilter) (auditlogrepo.ListResult, error) {
	return auditlogrepo.ListResult{}, errors.New("unexpected List call")
}

func (w *fakeCustomerRequestAuditWriter) PruneBefore(context.Context, time.Time) (int64, error) {
	return 0, errors.New("unexpected PruneBefore call")
}
