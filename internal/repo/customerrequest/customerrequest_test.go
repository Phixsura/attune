// ptrext:file-allow customer request repo tests use fake pgx rows and transaction fixtures.
// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestBuildListQueryFiltersAndOrdering(t *testing.T) {
	t.Parallel()

	ownerID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	query, args := buildListQuery(ListFilter{
		TenantID:      "tenant-a",
		Query:         " Export ",
		Statuses:      []Status{StatusOpen, StatusPlanned},
		Priorities:    []Priority{PriorityHigh},
		OwnerMemberID: &ownerID,
		Visibility:    VisibilityMerged,
		Sort:          SortPriority,
		Direction:     DirectionAsc,
		FeedbackID:    42,
	}, 101, 25)

	for _, want := range []string{
		"cr.tenant_id = $1",
		"cr.merged_into_request_id IS NOT NULL",
		"(LOWER(cr.title) LIKE $2 OR LOWER(cr.display_id) LIKE $2)",
		"cr.status = ANY($3)",
		"cr.priority = ANY($4)",
		"cr.owner_member_id = $5",
		"fl.feedback_id = $6",
		"ORDER BY CASE cr.priority WHEN 'urgent' THEN 4",
		"ASC NULLS LAST, cr.id ASC",
		"LIMIT $7 OFFSET $8",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("buildListQuery() SQL missing %q:\n%s", want, query)
		}
	}
	if len(args) != 8 {
		t.Fatalf("args len = %d, want 8: %#v", len(args), args)
	}
	if args[0] != "tenant-a" || args[1] != "%export%" || args[4] != ownerID || args[5] != int64(42) || args[6] != 101 || args[7] != 25 {
		t.Fatalf("args = %#v, want normalized query args", args)
	}
	if !reflect.DeepEqual(args[2], []string{"open", "planned"}) {
		t.Fatalf("status args = %#v", args[2])
	}
	if !reflect.DeepEqual(args[3], []string{"high"}) {
		t.Fatalf("priority args = %#v", args[3])
	}
}

func TestVisibilityAndSortClauses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		visibility Visibility
		want       string
	}{
		{visibility: VisibilityAll, want: "base"},
		{visibility: VisibilityMerged, want: "cr.merged_into_request_id IS NOT NULL"},
		{visibility: VisibilityArchived, want: "cr.archived_at IS NOT NULL AND cr.merged_into_request_id IS NULL"},
		{visibility: VisibilityActive, want: "cr.archived_at IS NULL AND cr.merged_into_request_id IS NULL"},
		{visibility: Visibility(""), want: "cr.archived_at IS NULL AND cr.merged_into_request_id IS NULL"},
	} {
		got := appendVisibilityClause([]string{"base"}, tc.visibility)
		if !containsString(got, tc.want) {
			t.Fatalf("appendVisibilityClause(%q) = %#v, want %q", tc.visibility, got, tc.want)
		}
	}

	for _, tc := range []struct {
		sort Sort
		want string
	}{
		{sort: SortUpdatedAt, want: "cr.updated_at DESC NULLS LAST, cr.id DESC"},
		{sort: SortCustomerCount, want: "fc.customer_count DESC NULLS LAST, cr.id DESC"},
		{sort: SortSupportingFeedbackCount, want: "fc.supporting_feedback_count DESC NULLS LAST, cr.id DESC"},
		{sort: SortLatestFeedbackAt, want: "fc.latest_feedback_at DESC NULLS LAST, cr.id DESC"},
		{sort: SortPriority, want: "CASE cr.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC NULLS LAST, cr.id DESC"},
		{sort: SortRevenueImpact, want: "revenue_impact_cents DESC NULLS LAST, cr.id DESC"},
		{sort: SortDecisionScore, want: "decision_score DESC NULLS LAST, cr.id DESC"},
		{sort: SortDeliveryHealth, want: "delivery_health_rank DESC NULLS LAST, cr.id DESC"},
	} {
		if got := orderByClause(tc.sort, DirectionDesc); got != tc.want {
			t.Fatalf("orderByClause(%q) = %q, want %q", tc.sort, got, tc.want)
		}
	}
	if got := orderByClause(SortDecisionScore, DirectionAsc); got != "decision_score ASC NULLS LAST, cr.id ASC" {
		t.Fatalf("ascending orderByClause = %q", got)
	}
}

func TestLoadDetailScansAllCollections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ownerID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	mergedID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	issueID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	customerID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	voteID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	noteID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	duplicateID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	latest := now.Add(2 * time.Hour)
	syncedAt := now.Add(time.Hour)

	db := &fakeRepoDB{
		rows: []fakeRepoRow{summaryRow(requestID, "tenant-a", "CR-7", ownerID, &mergedID, now, &latest)},
		queries: []*fakeRepoRows{
			{rows: [][]any{{
				int64(42), "Please add exports", "slack", "request", "user-1", "Ada", "Export request",
				string(ImportanceCritical), "renewal blocker", "admin-1", now.Add(-time.Hour), now.Add(-2 * time.Hour),
			}}},
			{rows: [][]any{{
				issueID, "github", "Phixsura/attune#212", "https://github.com/Phixsura/attune/issues/212",
				"Customer request object", "open", "admin-1", now.Add(-time.Hour), now,
				&syncedAt, string(IssueSyncStateSynced), "done", "octo", &latest, "",
			}}},
			{rows: [][]any{customerLinkValues(customerID, "user:ada", "hash:ada", "Ada", "acct:acme", "Acme", "buyer", "admin-1", now, "acct:acme", "Acme", 1200000)}},
			{rows: [][]any{
				voteValues(voteID, "user:ada", "hash:ada", "Ada", "acct:acme", "Acme", 5, "same account", "admin-2", now, "acct:acme", "Acme", 1200000),
				voteValues(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), "", "", "", "acct:beta", "Beta", 3, "second account", "admin-3", now, "acct:beta", "Beta", 250000),
			}},
			{rows: [][]any{{noteID, "Coordinate rollout", "admin-1", now}}},
			{rows: [][]any{{duplicateID, "CR-2", "Old request", now.Add(-30 * time.Minute)}}},
		},
	}

	detail, err := loadDetail(ctx, db, "tenant-a", requestID, 500)
	require.NoError(t, err)
	require.Equal(t, requestID, detail.Summary.ID)
	require.NotNil(t, detail.Summary.Owner)
	require.Equal(t, ownerID, detail.Summary.Owner.ID)
	require.NotNil(t, detail.Summary.MergedIntoRequestID)
	require.Equal(t, mergedID, *detail.Summary.MergedIntoRequestID)
	require.Len(t, detail.Feedback, 1)
	require.Equal(t, ImportanceCritical, detail.Feedback[0].Importance)
	require.Len(t, detail.IssueLinks, 1)
	require.Equal(t, IssueSyncStateSynced, detail.IssueLinks[0].SyncState)
	require.Len(t, detail.CustomerLinks, 1)
	require.NotNil(t, detail.CustomerLinks[0].AccountProfile)
	require.Len(t, detail.Votes, 2)
	require.NotNil(t, detail.Votes[1].AccountProfile)
	require.Len(t, detail.Notes, 1)
	require.Equal(t, "Coordinate rollout", detail.Notes[0].Body)
	require.Len(t, detail.Duplicates, 1)
	require.Equal(t, "CR-2", detail.Duplicates[0].DisplayID)
	require.Len(t, detail.AccountProfiles, 2)
	require.Equal(t, 100, db.queryArgs[0][2])
}

func TestScanHelpersAndErrorMapping(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	ownerID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	owner, err := scanOwner(fakeRepoRow{values: []any{ownerID.String(), "user", "user-1", "ada@example.com", "admin"}})
	if err != nil {
		t.Fatalf("scanOwner() error = %v", err)
	}
	if owner.ID != ownerID || owner.Email != "ada@example.com" {
		t.Fatalf("owner = %+v", owner)
	}
	if _, err := scanOwner(fakeRepoRow{values: []any{"not-a-uuid", "user", "user-1", "", "viewer"}}); err == nil {
		t.Fatal("scanOwner(invalid uuid) error = nil")
	}

	var customer CustomerLink
	if err := scanCustomerLink(fakeRepoRow{values: customerLinkValues(
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		"user:1", "hash:1", "Ada", "", "", "", "admin-1", now,
		"", "", 0,
	)}, &customer); err != nil {
		t.Fatalf("scanCustomerLink() error = %v", err)
	}
	if customer.AccountProfile != nil {
		t.Fatalf("AccountProfile = %+v, want nil when account row is null", customer.AccountProfile)
	}

	profiles := collectAccountProfiles(
		[]CustomerLink{{AccountProfile: (&accountProfileScan{
			AccountKey:      sql.NullString{String: "acct:acme", Valid: true},
			AccountDisplay:  sql.NullString{String: "Acme", Valid: true},
			RevenueCents:    sql.NullInt64{Int64: 100, Valid: true},
			RevenueCurrency: sql.NullString{String: "USD", Valid: true},
			UpdatedAt:       sql.NullTime{Time: now, Valid: true},
		}).toProfile()}},
		[]Vote{
			{AccountProfile: (&accountProfileScan{AccountKey: sql.NullString{String: "acct:acme", Valid: true}}).toProfile()},
			{AccountProfile: (&accountProfileScan{AccountKey: sql.NullString{String: "acct:beta", Valid: true}}).toProfile()},
		},
	)
	if got, want := len(profiles), 2; got != want {
		t.Fatalf("collectAccountProfiles len = %d, want %d", got, want)
	}
	if decisionScoreExplanation(nil) != "" {
		t.Fatal("decisionScoreExplanation(nil) should be empty")
	}
	explanation := decisionScoreExplanation(&Summary{Priority: PriorityHigh, SupportingFeedbackCount: 3, CustomerCount: 2, AccountCount: 1, VoteCount: 4, RevenueImpactCents: 500, DeliveryHealth: DeliveryHealthPending})
	if !strings.Contains(explanation, "priority=high") || !strings.Contains(explanation, "delivery_health=pending") {
		t.Fatalf("decisionScoreExplanation() = %q", explanation)
	}

	for _, tc := range []struct {
		code string
		want error
	}{
		{code: "23514", want: ErrInvalidInput},
		{code: "23505", want: ErrConflict},
		{code: "23503", want: ErrNotFound},
	} {
		if err := mapWriteError(&pgconn.PgError{Code: tc.code}, "write"); !errors.Is(err, tc.want) {
			t.Fatalf("mapWriteError(%s) = %v, want %v", tc.code, err, tc.want)
		}
	}
	boom := errors.New("boom")
	if err := mapWriteError(boom, "write"); !errors.Is(err, boom) || !strings.Contains(err.Error(), "write") {
		t.Fatalf("mapWriteError(boom) = %v, want wrapped op", err)
	}
}

func TestTransactionHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 7, 13, 0, 0, 0, time.UTC)
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := Repo{}

	createTx := &fakeRepoTx{
		rows: []fakeRepoRow{
			{values: []any{int64(7)}},
			{values: []any{requestID}},
			summaryRow(requestID, "tenant-a", "CR-7", uuid.Nil, nil, now, nil),
		},
	}
	created, err := repo.CreateTx(ctx, createTx, CreateInput{
		TenantID:    "tenant-a",
		Title:       "Export bundles",
		Description: "CSV export",
		Status:      StatusOpen,
		Priority:    PriorityHigh,
		ActorID:     "admin-1",
	})
	require.NoError(t, err)
	require.Equal(t, "CR-7", created.DisplayID)
	require.Equal(t, 2, createTx.execIdx)

	linkTx := &fakeRepoTx{rows: []fakeRepoRow{{values: []any{int64(42)}}}}
	err = repo.LinkFeedbackTx(ctx, linkTx, LinkFeedbackInput{TenantID: "tenant-a", RequestID: requestID, FeedbackID: 42, Importance: ImportanceNormal, ActorID: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, 1, linkTx.execIdx)
	err = repo.LinkFeedbackTx(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, LinkFeedbackInput{TenantID: "tenant-a", RequestID: requestID, FeedbackID: 42})
	require.ErrorIs(t, err, ErrFeedbackNotFound)

	unlinkTx := &fakeRepoTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("UPDATE 1")}}
	require.NoError(t, repo.UnlinkFeedbackTx(ctx, unlinkTx, "tenant-a", requestID, 42, "admin-1"))
	err = repo.UnlinkFeedbackTx(ctx, &fakeRepoTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 0")}}, "tenant-a", requestID, 42, "admin-1")
	require.ErrorIs(t, err, ErrLinkNotFound)
	err = touchRequestTx(ctx, &fakeRepoTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")}}, "tenant-a", requestID, "admin-1")
	require.ErrorIs(t, err, ErrNotFound)

	noteID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	noteTx := &fakeRepoTx{
		rows:  []fakeRepoRow{{values: []any{noteID}}, {values: []any{noteID, "Ship it", "admin-1", now}}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
	}
	note, err := repo.AddNoteTx(ctx, noteTx, NoteInput{TenantID: "tenant-a", RequestID: requestID, Body: "Ship it", ActorID: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, noteID, note.ID)
	require.Equal(t, "Ship it", note.Body)
	_, err = repo.DeleteNoteTx(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID, noteID, "admin-1")
	require.ErrorIs(t, err, ErrLinkNotFound)

	issueID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	issueTx := &fakeRepoTx{
		rows:  []fakeRepoRow{issueLinkRow(issueID, now, nil)},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	issue, err := repo.RecordIssueSyncTx(ctx, issueTx, IssueSyncInput{TenantID: "tenant-a", RequestID: requestID, IssueLinkID: issueID, SyncState: IssueSyncStateSynced, ActorID: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, issueID, issue.ID)
	require.Equal(t, IssueSyncStateSynced, issue.SyncState)
	_, err = repo.RecordIssueSyncTx(ctx, &fakeRepoTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")}}, IssueSyncInput{TenantID: "tenant-a", RequestID: requestID, IssueLinkID: issueID})
	require.ErrorIs(t, err, ErrLinkNotFound)

	_, err = repo.MergeTx(ctx, &fakeRepoTx{}, "tenant-a", requestID, requestID, "admin-1")
	require.ErrorIs(t, err, ErrConflict)
}

func TestUpdateAndCustomerTransactions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 7, 14, 0, 0, 0, time.UTC)
	repo := Repo{}
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	linkID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	title := "Renamed export"
	status := StatusInProgress

	updateTx := &fakeRepoTx{
		rows:  []fakeRepoRow{summaryRow(requestID, "tenant-a", "CR-2", uuid.Nil, nil, now, nil), summaryRow(requestID, "tenant-a", "CR-2", uuid.Nil, nil, now.Add(time.Minute), nil)},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
	}
	before, after, err := repo.UpdateTx(ctx, updateTx, UpdateInput{TenantID: "tenant-a", ID: requestID, Title: &title, Status: &status, ActorID: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, requestID, before.ID)
	require.Equal(t, requestID, after.ID)
	require.Equal(t, 1, updateTx.execIdx)

	customerTx := &fakeRepoTx{
		rows:  []fakeRepoRow{{values: []any{linkID}}, {values: customerLinkValues(linkID, "user:ada", "hash:ada", "Ada", "acct:acme", "Acme", "buyer", "admin-1", now, "acct:acme", "Acme", 1200000)}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	customer, err := repo.LinkCustomerTx(ctx, customerTx, CustomerLinkInput{
		TenantID: "tenant-a", RequestID: requestID, SubjectKey: "user:ada", SubjectHash: "hash:ada",
		SubjectDisplay: "Ada", AccountKey: "acct:acme", AccountDisplay: "Acme", ActorID: "admin-1",
		AccountProfile: AccountProfileInput{AccountKey: "acct:acme", AccountDisplay: "Acme", ActorID: "admin-1"},
	})
	require.NoError(t, err)
	require.Equal(t, linkID, customer.ID)
	require.NotNil(t, customer.AccountProfile)

	unlinkCustomerTx := &fakeRepoTx{
		rows:  []fakeRepoRow{{values: customerLinkValues(linkID, "user:ada", "hash:ada", "Ada", "acct:acme", "Acme", "buyer", "admin-1", now, "acct:acme", "Acme", 1200000)}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	removed, err := repo.UnlinkCustomerTx(ctx, unlinkCustomerTx, "tenant-a", requestID, linkID, "admin-1")
	require.NoError(t, err)
	require.Equal(t, linkID, removed.ID)
}

func TestVoteAndIssueTransactions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 7, 14, 0, 0, 0, time.UTC)
	repo := Repo{}
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	linkID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	voteTx := &fakeRepoTx{
		rows:  []fakeRepoRow{{values: []any{linkID}}, {values: voteValues(linkID, "user:ada", "hash:ada", "Ada", "acct:acme", "Acme", 9, "weighted", "admin-1", now, "acct:acme", "Acme", 1200000)}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	vote, err := repo.AddVoteTx(ctx, voteTx, VoteInput{TenantID: "tenant-a", RequestID: requestID, SubjectKey: "user:ada", SubjectHash: "hash:ada", AccountKey: "acct:acme", Weight: 9, ActorID: "admin-1", AccountProfile: AccountProfileInput{AccountKey: "acct:acme", ActorID: "admin-1"}})
	require.NoError(t, err)
	require.Equal(t, linkID, vote.ID)
	require.Equal(t, 9, vote.Weight)

	removeVoteTx := &fakeRepoTx{
		rows:  []fakeRepoRow{{values: voteValues(linkID, "user:ada", "hash:ada", "Ada", "acct:acme", "Acme", 9, "weighted", "admin-1", now, "acct:acme", "Acme", 1200000)}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	removedVote, err := repo.RemoveVoteTx(ctx, removeVoteTx, "tenant-a", requestID, linkID, "admin-1")
	require.NoError(t, err)
	require.Equal(t, linkID, removedVote.ID)

	issueTx := &fakeRepoTx{rows: []fakeRepoRow{{values: []any{linkID}}, issueLinkRow(linkID, now, nil)}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}}
	issue, err := repo.LinkIssueTx(ctx, issueTx, IssueLinkInput{TenantID: "tenant-a", RequestID: requestID, Provider: "github", ExternalKey: "Phixsura/attune#212", ExternalURL: "https://github.com/Phixsura/attune/issues/212", Title: "Customer request object", Status: "open", ActorID: "admin-1"})
	require.NoError(t, err)
	require.Equal(t, linkID, issue.ID)
	require.Equal(t, "github", issue.Provider)

	unlinkIssueTx := &fakeRepoTx{rows: []fakeRepoRow{issueLinkRow(linkID, now, nil)}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("UPDATE 1")}}
	removedIssue, err := repo.UnlinkIssueTx(ctx, unlinkIssueTx, "tenant-a", requestID, linkID, "admin-1")
	require.NoError(t, err)
	require.Equal(t, linkID, removedIssue.ID)
	require.NoError(t, upsertAccountProfileTx(ctx, &fakeRepoTx{}, "tenant-a", AccountProfileInput{}))
}

func TestMergeTransactionMovesBacklinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := Repo{}
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	targetID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mergeTx := &fakeRepoTx{
		rows: []fakeRepoRow{
			{values: lockRowValues("CR-1", nil, nil)},
			{values: lockRowValues("CR-2", nil, nil)},
			{values: []any{10}},
			{values: []any{8}},
			{values: []any{6}},
			{values: []any{5}},
			{values: []any{4}},
			{values: []any{3}},
			{values: []any{2}},
			{values: []any{2}},
			{values: []any{7}},
			{values: []any{6}},
		},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	result, err := repo.MergeTx(ctx, mergeTx, "tenant-a", requestID, targetID, "admin-1")
	require.NoError(t, err)
	require.Equal(t, "CR-2", result.SourceDisplayID)
	require.Equal(t, "CR-1", result.TargetDisplayID)
	require.Equal(t, 8, result.MovedFeedbackCount)
	require.Equal(t, 2, result.SkippedDuplicateFeedbackCount)
	require.Equal(t, 6, result.MovedIssueCount)
	require.Equal(t, 1, result.SkippedDuplicateIssueCount)
}

func TestLoadDetailAndListErrorBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 7, 15, 0, 0, 0, time.UTC)
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	boom := errors.New("boom")

	if _, err := loadDetail(ctx, &fakeRepoDB{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("loadDetail(missing summary) error = %v, want ErrNotFound", err)
	}

	if _, err := loadDetail(ctx, &fakeRepoDB{
		rows:      []fakeRepoRow{summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
		queryErrs: []error{boom},
	}, "tenant-a", requestID, 50); !errors.Is(err, boom) {
		t.Fatalf("loadDetail(feedback query error) = %v, want boom", err)
	}

	if _, err := loadDetail(ctx, &fakeRepoDB{
		rows: []fakeRepoRow{summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
		queries: []*fakeRepoRows{
			{},
			{scanErr: boom, rows: [][]any{{uuid.MustParse("22222222-2222-2222-2222-222222222222")}}},
		},
	}, "tenant-a", requestID, 50); !errors.Is(err, boom) {
		t.Fatalf("loadDetail(issue scan error) = %v, want boom", err)
	}

	if _, err := listEvidence(ctx, &fakeRepoDB{queryErrs: []error{boom}}, "tenant-a", requestID, 10); !errors.Is(err, boom) {
		t.Fatalf("listEvidence(query error) = %v, want boom", err)
	}
	if _, err := listNotes(ctx, &fakeRepoDB{queries: []*fakeRepoRows{{err: boom}}}, "tenant-a", requestID); !errors.Is(err, boom) {
		t.Fatalf("listNotes(rows error) = %v, want boom", err)
	}
	if _, err := listDuplicates(ctx, &fakeRepoDB{queries: []*fakeRepoRows{{scanErr: boom, rows: [][]any{{requestID, "CR-1", "Title", now}}}}}, "tenant-a", requestID); !errors.Is(err, boom) {
		t.Fatalf("listDuplicates(scan error) = %v, want boom", err)
	}
}

func TestTransactionConflictAndMissingBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 7, 16, 0, 0, 0, time.UTC)
	repo := Repo{}
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	linkID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	for name, run := range map[string]func() error{
		"link customer conflict": func() error {
			_, err := repo.LinkCustomerTx(ctx, &fakeRepoTx{
				rows: []fakeRepoRow{{err: pgx.ErrNoRows}, summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
			}, CustomerLinkInput{TenantID: "tenant-a", RequestID: requestID, SubjectKey: "user:1", ActorID: "admin-1"})
			return err
		},
		"add vote conflict": func() error {
			_, err := repo.AddVoteTx(ctx, &fakeRepoTx{
				rows: []fakeRepoRow{{err: pgx.ErrNoRows}, summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
			}, VoteInput{TenantID: "tenant-a", RequestID: requestID, SubjectKey: "user:1", Weight: 1, ActorID: "admin-1"})
			return err
		},
		"add note conflict": func() error {
			_, err := repo.AddNoteTx(ctx, &fakeRepoTx{
				rows: []fakeRepoRow{{err: pgx.ErrNoRows}, summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
			}, NoteInput{TenantID: "tenant-a", RequestID: requestID, Body: "Note", ActorID: "admin-1"})
			return err
		},
		"link issue conflict": func() error {
			_, err := repo.LinkIssueTx(ctx, &fakeRepoTx{
				rows: []fakeRepoRow{{err: pgx.ErrNoRows}, summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
			}, IssueLinkInput{TenantID: "tenant-a", RequestID: requestID, Provider: "github", ExternalKey: "o/r#1", ExternalURL: "https://github.com/o/r/issues/1", ActorID: "admin-1"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := run(); !errors.Is(err, ErrConflict) {
				t.Fatalf("%s error = %v, want ErrConflict", name, err)
			}
		})
	}

	if _, err := getCustomerLink(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID, linkID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("getCustomerLink(missing) error = %v, want ErrLinkNotFound", err)
	}
	if _, err := getVote(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID, linkID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("getVote(missing) error = %v, want ErrLinkNotFound", err)
	}
	if _, err := getNote(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID, linkID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("getNote(missing) error = %v, want ErrLinkNotFound", err)
	}
	if _, err := getIssueLink(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID, linkID); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("getIssueLink(missing) error = %v, want ErrLinkNotFound", err)
	}

	mergedID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	row, err := lockRequest(ctx, &fakeRepoTx{rows: []fakeRepoRow{{values: lockRowValues("CR-1", &mergedID, nil)}}}, "tenant-a", requestID)
	if err != nil {
		t.Fatalf("lockRequest(merged) error = %v", err)
	}
	if row.MergedIntoRequestID == nil || *row.MergedIntoRequestID != mergedID {
		t.Fatalf("lockRequest merged id = %+v, want %s", row.MergedIntoRequestID, mergedID)
	}
	if _, err := lockRequest(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: pgx.ErrNoRows}}}, "tenant-a", requestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lockRequest(missing) error = %v, want ErrNotFound", err)
	}
}

func TestAllocateAndWriteErrorBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 7, 17, 0, 0, 0, time.UTC)
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := Repo{}
	boom := errors.New("boom")

	if _, _, err := allocateDisplayID(ctx, &fakeRepoTx{execErrs: []error{boom}}, "tenant-a"); !errors.Is(err, boom) {
		t.Fatalf("allocateDisplayID(ensure error) = %v, want boom", err)
	}
	if _, _, err := allocateDisplayID(ctx, &fakeRepoTx{rows: []fakeRepoRow{{err: boom}}}, "tenant-a"); !errors.Is(err, boom) {
		t.Fatalf("allocateDisplayID(lock error) = %v, want boom", err)
	}
	if _, _, err := allocateDisplayID(ctx, &fakeRepoTx{rows: []fakeRepoRow{{values: []any{int64(7)}}}, execErrs: []error{nil, boom}}, "tenant-a"); !errors.Is(err, boom) {
		t.Fatalf("allocateDisplayID(advance error) = %v, want boom", err)
	}

	if _, err := repo.CreateTx(ctx, &fakeRepoTx{
		rows: []fakeRepoRow{
			{values: []any{int64(7)}},
			{err: &pgconn.PgError{Code: "23505"}},
		},
	}, CreateInput{TenantID: "tenant-a", Title: "Export", Status: StatusOpen, Priority: PriorityNone, ActorID: "admin-1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateTx(unique violation) error = %v, want ErrConflict", err)
	}

	archivedAt := now
	archived := summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)
	archived.values[14] = &archivedAt
	if _, _, err := repo.UpdateTx(ctx, &fakeRepoTx{rows: []fakeRepoRow{archived}}, UpdateInput{TenantID: "tenant-a", ID: requestID, ActorID: "admin-1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("UpdateTx(archived) error = %v, want ErrConflict", err)
	}

	if _, _, err := repo.UpdateTx(ctx, &fakeRepoTx{
		rows:     []fakeRepoRow{summaryRow(requestID, "tenant-a", "CR-1", uuid.Nil, nil, now, nil)},
		execErrs: []error{&pgconn.PgError{Code: "23514"}},
	}, UpdateInput{TenantID: "tenant-a", ID: requestID, ActorID: "admin-1"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateTx(check violation) error = %v, want ErrInvalidInput", err)
	}

	if err := repo.LinkFeedbackTx(ctx, &fakeRepoTx{
		rows:  []fakeRepoRow{{values: []any{int64(42)}}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")},
	}, LinkFeedbackInput{TenantID: "tenant-a", RequestID: requestID, FeedbackID: 42, Importance: ImportanceNormal, ActorID: "admin-1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LinkFeedbackTx(touch missing) error = %v, want ErrNotFound", err)
	}

	if err := repo.UnlinkFeedbackTx(ctx, &fakeRepoTx{execErrs: []error{boom}}, "tenant-a", requestID, 42, "admin-1"); !errors.Is(err, boom) {
		t.Fatalf("UnlinkFeedbackTx(delete error) = %v, want boom", err)
	}
}

func summaryRow(id uuid.UUID, tenantID, displayID string, ownerID uuid.UUID, mergedID *uuid.UUID, now time.Time, latestFeedbackAt *time.Time) fakeRepoRow {
	ownerMemberID := sql.NullString{}
	ownerScanID := sql.NullString{}
	ownerType := sql.NullString{}
	ownerUserID := sql.NullString{}
	ownerEmail := sql.NullString{}
	ownerRole := sql.NullString{}
	if ownerID != uuid.Nil {
		ownerMemberID = sql.NullString{String: ownerID.String(), Valid: true}
		ownerScanID = sql.NullString{String: ownerID.String(), Valid: true}
		ownerType = sql.NullString{String: "user", Valid: true}
		ownerUserID = sql.NullString{String: "user-1", Valid: true}
		ownerEmail = sql.NullString{String: "ada@example.com", Valid: true}
		ownerRole = sql.NullString{String: "admin", Valid: true}
	}
	merged := sql.NullString{}
	if mergedID != nil {
		merged = sql.NullString{String: mergedID.String(), Valid: true}
	}
	firstFeedbackAt := now.Add(-2 * time.Hour)
	return fakeRepoRow{values: []any{
		id,
		tenantID,
		int64(7),
		displayID,
		"Export bundles",
		"CSV export",
		string(StatusOpen),
		string(PriorityHigh),
		ownerMemberID,
		"admin-1",
		"admin-2",
		merged,
		now.Add(-24 * time.Hour),
		now,
		(*time.Time)(nil),
		ownerScanID,
		ownerType,
		ownerUserID,
		ownerEmail,
		ownerRole,
		3,
		2,
		1,
		1,
		4,
		1,
		0,
		int64(1200000),
		"USD",
		95,
		string(DeliveryHealthSynced),
		1,
		1,
		0,
		0,
		0,
		0,
		&firstFeedbackAt,
		latestFeedbackAt,
	}}
}

func customerLinkValues(id uuid.UUID, subjectKey, subjectHash, subjectDisplay, accountKey, accountDisplay, note, createdBy string, createdAt time.Time, profileKey, profileDisplay string, revenueCents int64) []any {
	return append([]any{
		id,
		subjectKey,
		subjectHash,
		subjectDisplay,
		accountKey,
		accountDisplay,
		note,
		createdBy,
		createdAt,
	}, profileValues(profileKey, profileDisplay, revenueCents)...)
}

func voteValues(id uuid.UUID, subjectKey, subjectHash, subjectDisplay, accountKey, accountDisplay string, weight int, note, createdBy string, createdAt time.Time, profileKey, profileDisplay string, revenueCents int64) []any {
	return append([]any{
		id,
		subjectKey,
		subjectHash,
		subjectDisplay,
		accountKey,
		accountDisplay,
		weight,
		note,
		createdBy,
		createdAt,
	}, profileValues(profileKey, profileDisplay, revenueCents)...)
}

func profileValues(accountKey, accountDisplay string, revenueCents int64) []any {
	if accountKey == "" {
		return []any{
			sql.NullString{},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullTime{},
		}
	}
	now := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
	return []any{
		sql.NullString{String: accountKey, Valid: true},
		sql.NullString{String: accountDisplay, Valid: true},
		sql.NullInt64{Int64: revenueCents, Valid: true},
		sql.NullString{String: "USD", Valid: true},
		sql.NullString{String: "enterprise", Valid: true},
		sql.NullString{String: "mid_market", Valid: true},
		sql.NullString{String: "active", Valid: true},
		sql.NullString{String: "salesforce", Valid: true},
		sql.NullString{String: "001", Valid: true},
		sql.NullString{String: "manual", Valid: true},
		sql.NullTime{Time: now, Valid: true},
	}
}

func issueLinkRow(id uuid.UUID, now time.Time, syncedAt *time.Time) fakeRepoRow {
	return fakeRepoRow{values: []any{
		id,
		"github",
		"Phixsura/attune#212",
		"https://github.com/Phixsura/attune/issues/212",
		"Customer request object",
		"open",
		"admin-1",
		now.Add(-time.Hour),
		now,
		syncedAt,
		string(IssueSyncStateSynced),
		"done",
		"octo",
		syncedAt,
		"",
	}}
}

func lockRowValues(displayID string, mergedID *uuid.UUID, archivedAt *time.Time) []any {
	merged := sql.NullString{}
	if mergedID != nil {
		merged = sql.NullString{String: mergedID.String(), Valid: true}
	}
	return []any{displayID, merged, archivedAt}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type fakeRepoDB struct {
	rows      []fakeRepoRow
	rowIdx    int
	queries   []*fakeRepoRows
	queryErrs []error
	queryIdx  int
	queryArgs [][]any
}

func (db *fakeRepoDB) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	db.queryArgs = append(db.queryArgs, args)
	if db.queryIdx < len(db.queryErrs) && db.queryErrs[db.queryIdx] != nil {
		err := db.queryErrs[db.queryIdx]
		db.queryIdx++
		return nil, err
	}
	if db.queryIdx >= len(db.queries) {
		db.queryIdx++
		return &fakeRepoRows{}, nil
	}
	rows := db.queries[db.queryIdx]
	db.queryIdx++
	return rows, nil
}

func (db *fakeRepoDB) QueryRow(context.Context, string, ...any) pgx.Row {
	if db.rowIdx >= len(db.rows) {
		return fakeRepoRow{err: errors.New("unexpected query row")}
	}
	row := db.rows[db.rowIdx]
	db.rowIdx++
	return row
}

type fakeRepoTx struct {
	rows      []fakeRepoRow
	rowIdx    int
	queries   []*fakeRepoRows
	queryErrs []error
	queryIdx  int
	execs     []pgconn.CommandTag
	execErrs  []error
	execIdx   int
}

func (tx *fakeRepoTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeRepoTx) Commit(context.Context) error          { return nil }
func (tx *fakeRepoTx) Rollback(context.Context) error        { return nil }
func (tx *fakeRepoTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *fakeRepoTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeRepoTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (tx *fakeRepoTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *fakeRepoTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	idx := tx.execIdx
	tx.execIdx++
	if idx < len(tx.execErrs) && tx.execErrs[idx] != nil {
		return pgconn.CommandTag{}, tx.execErrs[idx]
	}
	if idx < len(tx.execs) {
		return tx.execs[idx], nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeRepoTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.queryIdx < len(tx.queryErrs) && tx.queryErrs[tx.queryIdx] != nil {
		err := tx.queryErrs[tx.queryIdx]
		tx.queryIdx++
		return nil, err
	}
	if tx.queryIdx >= len(tx.queries) {
		tx.queryIdx++
		return &fakeRepoRows{}, nil
	}
	rows := tx.queries[tx.queryIdx]
	tx.queryIdx++
	return rows, nil
}

func (tx *fakeRepoTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return fakeRepoRow{err: errors.New("unexpected query row")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}

func (tx *fakeRepoTx) Conn() *pgx.Conn { return nil }

type fakeRepoRow struct {
	values []any
	err    error
}

func (r fakeRepoRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignRepoScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

type fakeRepoRows struct {
	rows    [][]any
	idx     int
	err     error
	scanErr error
}

func (r *fakeRepoRows) Close() {}

func (r *fakeRepoRows) Err() error {
	return r.err
}

func (r *fakeRepoRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeRepoRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeRepoRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRepoRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan called without current row")
	}
	if len(dest) != len(r.rows[r.idx-1]) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignRepoScanValue(dest[i], r.rows[r.idx-1][i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeRepoRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values called without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeRepoRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRepoRows) Conn() *pgx.Conn {
	return nil
}

func assignRepoScanValue(dest any, src any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		return errors.New("scan destination must be a non-nil pointer")
	}
	target := destValue.Elem()
	if src == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	source := reflect.ValueOf(src)
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return nil
	}
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return nil
	}
	return errors.New("scan source type mismatch")
}
