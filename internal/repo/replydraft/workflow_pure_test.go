// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestWorkflowStatusPredicates(t *testing.T) {
	t.Parallel()

	for _, status := range []string{StatusSuggested, StatusEdited, StatusApproved, StatusSendFailed, StatusStale} {
		if !canEditDraft(status) {
			t.Fatalf("canEditDraft(%q) = false, want true", status)
		}
	}
	for _, status := range []string{StatusSent, StatusRejected, StatusSendPending, "unknown"} {
		if canEditDraft(status) {
			t.Fatalf("canEditDraft(%q) = true, want false", status)
		}
	}
	for _, status := range []string{StatusSuggested, StatusEdited} {
		if !canApproveDraft(status) {
			t.Fatalf("canApproveDraft(%q) = false, want true", status)
		}
	}
	for _, status := range []string{StatusApproved, StatusSendFailed, StatusStale, StatusSent} {
		if canApproveDraft(status) {
			t.Fatalf("canApproveDraft(%q) = true, want false", status)
		}
	}
	for _, status := range []string{StatusApproved, StatusSendFailed} {
		if !canPrepareDelivery(status) {
			t.Fatalf("canPrepareDelivery(%q) = false, want true", status)
		}
	}
	for _, status := range []string{StatusSuggested, StatusEdited, StatusSendPending, StatusSent} {
		if canPrepareDelivery(status) {
			t.Fatalf("canPrepareDelivery(%q) = true, want false", status)
		}
	}
}

func TestWorkflowErrorAndRevisionRules(t *testing.T) {
	t.Parallel()

	if err := checkExpectedRevision(Draft{Revision: 7}, 7); err != nil {
		t.Fatalf("checkExpectedRevision exact match returned %v", err)
	}
	for _, expected := range []int64{0, 6, 8} {
		if err := checkExpectedRevision(Draft{Revision: 7}, expected); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("checkExpectedRevision(%d) = %v, want ErrRevisionConflict", expected, err)
		}
	}
	for _, err := range []error{ErrStaleDraft, ErrInvalidDraftState, ErrRevisionConflict, ErrHookNotFound} {
		if !isTerminalDeliveryClaimError(err) {
			t.Fatalf("isTerminalDeliveryClaimError(%v) = false, want true", err)
		}
	}
	if isTerminalDeliveryClaimError(ErrRequestInProgress) {
		t.Fatalf("ErrRequestInProgress should remain retryable by the claimant")
	}
}

func TestWorkflowDeliveryCompletionRules(t *testing.T) {
	t.Parallel()

	attempt := deliveryAttemptRow{HookID: "hook-1", RevisionID: "rev-1"}
	if !canCompleteDeliveryAttempt(Draft{
		Status:             StatusSendPending,
		ApprovedRevisionID: "rev-1",
		ApprovedHookID:     "hook-1",
	}, attempt) {
		t.Fatalf("canCompleteDeliveryAttempt returned false for matching pending draft")
	}
	for _, draft := range []Draft{
		{Status: StatusApproved, ApprovedRevisionID: "rev-1", ApprovedHookID: "hook-1"},
		{Status: StatusSendPending, ApprovedRevisionID: "other", ApprovedHookID: "hook-1"},
		{Status: StatusSendPending, ApprovedRevisionID: "rev-1", ApprovedHookID: "other"},
	} {
		if canCompleteDeliveryAttempt(draft, attempt) {
			t.Fatalf("canCompleteDeliveryAttempt(%+v) = true, want false", draft)
		}
	}
}

func TestWorkflowRetryDelayAndTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 0, want: 30 * time.Second},
		{attempts: 1, want: 30 * time.Second},
		{attempts: 2, want: time.Minute},
		{attempts: 6, want: 16 * time.Minute},
		{attempts: 7, want: 30 * time.Minute},
		{attempts: 12, want: 30 * time.Minute},
	}
	for _, tc := range tests {
		if got := deliveryRetryDelay(tc.attempts); got != tc.want {
			t.Fatalf("deliveryRetryDelay(%d) = %s, want %s", tc.attempts, got, tc.want)
		}
	}
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("truncate short = %q, want unchanged", got)
	}
	if got := truncate("åßçdé", 3); got != "åßç" {
		t.Fatalf("truncate rune-safe = %q, want first three runes", got)
	}
}

func TestWorkflowFingerprintsAndSQLHelpers(t *testing.T) {
	t.Parallel()

	fp := fingerprint("tenant", "feedback")
	if len(fp) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(fp))
	}
	if fp != fingerprint("tenant", "feedback") {
		t.Fatalf("fingerprint should be deterministic")
	}
	if fp == fingerprint("tenant", "other") {
		t.Fatalf("fingerprint should change when inputs change")
	}
	if deliveryRequestFingerprint("draft", "hook") != fingerprint("draft", "hook") {
		t.Fatalf("deliveryRequestFingerprint should delegate to fingerprint")
	}
	if len(sha256Bytes("reply")) != 32 {
		t.Fatalf("sha256Bytes length = %d, want 32", len(sha256Bytes("reply")))
	}
	if got := string(emptyBytesIfNil(nil)); got != "" {
		t.Fatalf("emptyBytesIfNil(nil) = %q, want empty bytes", got)
	}
	if got := emptyBytesIfNil([]byte("secret")); string(got) != "secret" {
		t.Fatalf("emptyBytesIfNil(non-empty) = %q, want original", got)
	}
	for _, sqlText := range []string{draftByIDSQL(), hookSelectSQL(), deliveryAttemptsSelectSQL(), activeDraftSQL()} {
		if strings.TrimSpace(sqlText) == "" || !strings.Contains(sqlText, "SELECT") {
			t.Fatalf("SQL helper returned unexpected text: %q", sqlText)
		}
	}
}

func TestRollbackIgnoresRollbackErrors(t *testing.T) {
	t.Parallel()

	rollback(context.Background(), ptrext.Of(fakeTx{}))
}

func TestCreateTaskTxUsesTransactionAndWrapsErrors(t *testing.T) {
	t.Parallel()

	repo := DraftTaskRepo{}
	ctx := context.Background()

	tx := ptrext.Of(fakeTx{})
	if err := repo.CreateTaskTx(ctx, tx, 42, "tenant-1", ptrext.Of(0.8), "trace-1"); err != nil {
		t.Fatalf("CreateTaskTx returned error: %v", err)
	}
	if tx.execIdx != 1 {
		t.Fatalf("CreateTaskTx execs = %d, want one enqueue statement", tx.execIdx)
	}

	boom := errors.New("insert failed")
	if err := repo.CreateTaskTx(ctx, ptrext.Of(fakeTx{execErrs: []error{boom}}), 42, "tenant-1", nil, "trace-1"); err == nil {
		t.Fatalf("CreateTaskTx error = nil, want wrapped exec error")
	}
}

func TestScanDraftMapsNullableColumns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	draft, err := scanDraft(fakeRow{scan: scanValues(
		"draft-1", "tenant-1", int64(42), 2, StatusApproved,
		sql.NullString{String: "rev-active", Valid: true},
		sql.NullString{String: "rev-approved", Valid: true},
		sql.NullString{},
		sql.NullString{String: "hook-1", Valid: true},
		"hook-fp",
		"Thanks for the report.", "source-fp", "", "accepted", "msg-1",
		sql.NullTime{Time: now.Add(-4 * time.Hour), Valid: true}, "system",
		sql.NullTime{}, "",
		sql.NullTime{Time: now.Add(-3 * time.Hour), Valid: true}, "admin-1",
		sql.NullTime{}, "",
		sql.NullTime{}, "",
		int64(9), now.Add(-5*time.Hour), now,
	)})
	if err != nil {
		t.Fatalf("scanDraft returned error: %v", err)
	}
	if draft.ID != "draft-1" || draft.ActiveRevisionID != "rev-active" || draft.ApprovedRevisionID != "rev-approved" {
		t.Fatalf("draft identifiers = %+v", draft)
	}
	if draft.SentRevisionID != "" || draft.SentAt != nil || draft.RejectedAt != nil {
		t.Fatalf("nullable fields were not cleared: %+v", draft)
	}
	if draft.GeneratedAt == nil || !draft.GeneratedAt.Equal(now.Add(-4*time.Hour)) {
		t.Fatalf("GeneratedAt = %v, want populated timestamp", draft.GeneratedAt)
	}
	if _, err := scanDraft(fakeRow{err: pgx.ErrNoRows}); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("scanDraft no rows = %v, want ErrDraftNotFound", err)
	}
}

func TestScanDeliveryAttemptMapsNullableColumns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	attempt, err := scanDeliveryAttempt(fakeRow{scan: scanValues(
		"attempt-1", "tenant-1", "", int64(0), "hook-1", "hooks.example.test", "hook-fp",
		"", DeliveryEventReplyTest, "reply_test_abc", DeliveryStatusFailed,
		502, 2, maxReplyDeliveryAttempts,
		sql.NullTime{Time: now.Add(30 * time.Second), Valid: true},
		"", "receiver returned 502", "admin", "admin-1", now.Add(-time.Minute),
		sql.NullTime{}, now.Add(-time.Minute), now,
	)})
	if err != nil {
		t.Fatalf("scanDeliveryAttempt returned error: %v", err)
	}
	if attempt.DraftID != "" || attempt.RevisionID != "" || attempt.FeedbackID != 0 {
		t.Fatalf("nullable delivery fields = %+v, want empty draft metadata", attempt)
	}
	if attempt.NextRetryAt == nil || !attempt.NextRetryAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("NextRetryAt = %v, want retry timestamp", attempt.NextRetryAt)
	}
	if attempt.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil", attempt.CompletedAt)
	}
}

func TestScanHookDestFillsHook(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	hook := ptrext.Of(Hook{})
	if err := scanValues(
		"hook-1", "tenant-1", "Reply hook", []byte("url"), "url-key", "url-fp",
		"hooks.example.test", []byte("secret"), "secret-key", true,
		"admin-1", "admin-2", sql.NullTime{}, now.Add(-time.Hour), now,
	)(scanHookDest(hook)...); err != nil {
		t.Fatalf("scanHookDest scan returned error: %v", err)
	}
	if hook.ID != "hook-1" || hook.URLHost != "hooks.example.test" || string(hook.SecretCiphertext) != "secret" {
		t.Fatalf("hook = %+v", hook)
	}
	if hook.DisabledAt.Valid {
		t.Fatalf("DisabledAt = %+v, want invalid", hook.DisabledAt)
	}
}

func TestScanRevisionAndEventRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	revisions, err := scanRevisions(ptrext.Of(fakeRows{rows: [][]any{
		{"rev-2", "draft-1", "tenant-1", int64(42), 1, 2, "human", "Edited", "source-fp", []byte(`{"reason":"edit"}`), "admin-1", now},
		{"rev-1", "draft-1", "tenant-1", int64(42), 1, 1, "ai", "Generated", "source-fp", []byte(`{}`), "system", now.Add(-time.Minute)},
	}}))
	if err != nil {
		t.Fatalf("scanRevisions returned error: %v", err)
	}
	if len(revisions) != 2 || revisions[0].RevisionNo != 2 || revisions[1].Origin != "ai" {
		t.Fatalf("revisions = %+v", revisions)
	}

	events, err := scanEvents(ptrext.Of(fakeRows{rows: [][]any{
		{
			"event-1", "draft-1", "tenant-1", int64(42),
			sql.NullString{String: "rev-2", Valid: true},
			sql.NullString{},
			"edit", "admin", "admin-1", "", []byte(`{}`), now,
		},
	}}))
	if err != nil {
		t.Fatalf("scanEvents returned error: %v", err)
	}
	if len(events) != 1 || events[0].RevisionID != "rev-2" || events[0].HookID != "" {
		t.Fatalf("events = %+v", events)
	}
}

func TestScanRowsPropagatesErrors(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("broken row")
	if _, err := scanDeliveryAttempt(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanDeliveryAttempt error = %v, want wrapped scan error", err)
	}
	if _, err := scanRevisions(ptrext.Of(fakeRows{err: scanErr})); !errors.Is(err, scanErr) {
		t.Fatalf("scanRevisions error = %v, want rows error", err)
	}
	if _, err := scanEvents(ptrext.Of(fakeRows{rows: [][]any{{"bad"}}, scanErr: scanErr})); !errors.Is(err, scanErr) {
		t.Fatalf("scanEvents scan error = %v, want wrapped scan error", err)
	}
}

func TestEditDraftTxCreatesHumanRevisionAndReloadsDraft(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	draft := testRepoDraft(StatusApproved, now)
	edited := draft
	edited.Status = StatusEdited
	edited.ActiveRevisionID = "22222222-2222-2222-2222-222222222222"
	edited.ActiveContent = "Human edit"
	edited.Revision = 8
	rev := testRepoRevision(draft, edited.ActiveRevisionID, 2, "human", "Human edit", now)
	tx := ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		revisionRow(rev),
		draftRow(edited),
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	repo := DraftTaskRepo{}

	got, err := repo.editDraftTx(context.Background(), tx, draft.TenantID, draft.FeedbackID, " Human edit ", draft.Revision, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("editDraftTx returned error: %v", err)
	}
	if got.Status != StatusEdited || got.ActiveRevisionID != edited.ActiveRevisionID || got.ActiveContent != "Human edit" {
		t.Fatalf("edited draft = %+v", got)
	}
	if tx.execIdx != 3 {
		t.Fatalf("exec calls = %d, want 3", tx.execIdx)
	}
}

func TestApproveDraftTxApprovesFreshDraftAndMarksStaleSource(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{"path":"/login"}`, "Login failure", "Cannot login", `{"sentiment":"negative"}`, "en", "completed")
	draft := testRepoDraft(StatusEdited, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	hook := testRepoHook(now)
	approved := draft
	approved.Status = StatusApproved
	approved.ApprovedRevisionID = draft.ActiveRevisionID
	approved.ApprovedHookID = hook.ID
	approved.ApprovedHookFingerprint = hook.URLFingerprint
	tx := ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		feedbackSnapshotRow("Login fails", "web", "user-1", `{"path":"/login"}`, "Login failure", "Cannot login", `{"sentiment":"negative"}`, "en", "completed"),
		hookRow(hook),
		draftRow(approved),
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	repo := DraftTaskRepo{}

	got, err := repo.approveDraftTx(context.Background(), tx, draft.TenantID, draft.FeedbackID, draft.Revision, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("approveDraftTx returned error: %v", err)
	}
	if got.Status != StatusApproved || got.ApprovedHookID != hook.ID {
		t.Fatalf("approved draft = %+v", got)
	}

	staleTx := ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		feedbackSnapshotRow("Changed content", "web", "user-1", `{"path":"/login"}`, "Login failure", "Cannot login", `{"sentiment":"negative"}`, "en", "completed"),
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	_, err = repo.approveDraftTx(context.Background(), staleTx, draft.TenantID, draft.FeedbackID, draft.Revision, Actor{Type: "admin", ID: "admin-1"})
	if !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("stale approve error = %v, want ErrStaleDraft", err)
	}
	if staleTx.execIdx != 2 {
		t.Fatalf("stale approve exec calls = %d, want stale update and event", staleTx.execIdx)
	}
}

func TestRejectDraftTxRejectsEditableDraft(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	draft := testRepoDraft(StatusSuggested, now)
	rejected := draft
	rejected.Status = StatusRejected
	tx := ptrext.Of(fakeTx{rows: []fakeRow{draftRow(draft), draftRow(rejected)}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	repo := DraftTaskRepo{}

	got, err := repo.rejectDraftTx(context.Background(), tx, draft.TenantID, draft.FeedbackID, draft.Revision, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("rejectDraftTx returned error: %v", err)
	}
	if got.Status != StatusRejected {
		t.Fatalf("rejected draft status = %q, want rejected", got.Status)
	}
}

func TestPrepareDeliveryTxCreatesAttemptAndCachesAcceptedSend(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	draft := testRepoDraft(StatusApproved, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	draft.ApprovedRevisionID = draft.ActiveRevisionID
	hook := testRepoHook(now)
	draft.ApprovedHookID = hook.ID
	draft.ApprovedHookFingerprint = hook.URLFingerprint
	rev := testRepoRevision(draft, draft.ActiveRevisionID, 1, "human", "Thanks.", now)
	tx := ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
		hookRow(hook),
		revisionRow(rev),
		{scan: scanValues("55555555-5555-5555-5555-555555555555")},
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	repo := DraftTaskRepo{}

	prep, err := repo.prepareDeliveryTx(context.Background(), tx, draft.TenantID, draft.FeedbackID, "reply_send_123456", draft.Revision, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("prepareDeliveryTx returned error: %v", err)
	}
	if prep.AttemptID != "55555555-5555-5555-5555-555555555555" || prep.FromCache {
		t.Fatalf("prep = %+v, want new attempt", prep)
	}

	sent := draft
	sent.Status = StatusSent
	cacheTx := ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(sent),
		{scan: scanValues("55555555-5555-5555-5555-555555555555", hook.ID, rev.ID)},
		hookRow(hook),
		revisionRow(rev),
	}})

	prep, err = repo.prepareDeliveryTx(context.Background(), cacheTx, draft.TenantID, draft.FeedbackID, "reply_send_123456", draft.Revision, Actor{})
	if err != nil {
		t.Fatalf("cached prepareDeliveryTx returned error: %v", err)
	}
	if !prep.FromCache || prep.AttemptID == "" || prep.Revision.ID != rev.ID {
		t.Fatalf("cached prep = %+v", prep)
	}
}

func TestResolveExistingAttemptsEnforcesIdempotency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	draft := testRepoDraft(StatusApproved, now)
	requestFingerprint := "same-request"
	repo := DraftTaskRepo{}

	acceptedTx := ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("attempt-1", DeliveryStatusAccepted, requestFingerprint)}}})
	attemptID, fromCache, err := repo.resolveExistingAttemptTx(context.Background(), acceptedTx, draft, "reply_send_123456", requestFingerprint)
	if err != nil {
		t.Fatalf("resolveExistingAttemptTx accepted returned error: %v", err)
	}
	if attemptID != "attempt-1" || !fromCache {
		t.Fatalf("accepted attempt = %q cache:%v, want cache", attemptID, fromCache)
	}

	conflictTx := ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("attempt-1", DeliveryStatusAccepted, "different")}}})
	_, _, err = repo.resolveExistingAttemptTx(context.Background(), conflictTx, draft, "reply_send_123456", requestFingerprint)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v, want ErrIdempotencyConflict", err)
	}

	pendingTx := ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("attempt-1", DeliveryStatusPending, requestFingerprint)}}})
	_, _, err = repo.resolveExistingAttemptTx(context.Background(), pendingTx, draft, "reply_send_123456", requestFingerprint)
	if !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("pending error = %v, want ErrRequestInProgress", err)
	}

	failedTx := ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("attempt-1", DeliveryStatusFailed, requestFingerprint)}}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}})

	attemptID, fromCache, err = repo.resolveExistingAttemptTx(context.Background(), failedTx, draft, "reply_send_123456", requestFingerprint)
	if err != nil {
		t.Fatalf("failed retry resolve returned error: %v", err)
	}
	if attemptID != "attempt-1" || fromCache {
		t.Fatalf("failed retry attempt = %q cache:%v, want retry", attemptID, fromCache)
	}
}

func TestResolveExistingHookTestAttemptEnforcesIdempotency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	hook := testRepoHook(now)
	repo := DraftTaskRepo{}
	requestFingerprint := "same-request"

	deadTx := ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("attempt-1", DeliveryStatusDead, requestFingerprint)}}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}})

	attemptID, fromCache, err := repo.resolveExistingHookTestAttemptTx(context.Background(), deadTx, hook, "reply_test_123456", requestFingerprint, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("dead hook test resolve returned error: %v", err)
	}
	if attemptID != "attempt-1" || fromCache {
		t.Fatalf("dead hook test attempt = %q cache:%v, want retry", attemptID, fromCache)
	}

	unknownTx := ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("attempt-1", "unknown", requestFingerprint)}}})
	_, _, err = repo.resolveExistingHookTestAttemptTx(context.Background(), unknownTx, hook, "reply_test_123456", requestFingerprint, Actor{})
	if err == nil || !strings.Contains(err.Error(), "unknown reply hook test attempt status") {
		t.Fatalf("unknown status error = %v, want status error", err)
	}
}

func TestMarkDeliveryAcceptedAndFailedTx(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	draft := testRepoDraft(StatusSendPending, now)
	draft.ApprovedRevisionID = draft.ActiveRevisionID
	hook := testRepoHook(now)
	draft.ApprovedHookID = hook.ID
	attempt := deliveryAttemptRow{
		DraftID: draft.ID, HookID: hook.ID, RevisionID: draft.ActiveRevisionID,
		EventType: DeliveryEventReplySend, Status: DeliveryStatusPending, Attempts: 1,
		MaxAttempts: maxReplyDeliveryAttempts, RequestedByType: "admin", RequestedBy: "admin-1",
	}
	sent := draft
	sent.Status = StatusSent
	sent.ExternalDeliveryStatus = DeliveryStatusAccepted
	acceptedTx := ptrext.Of(fakeTx{rows: []fakeRow{
		attemptRow(attempt),
		draftRow(draft),
		draftRow(sent),
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	repo := DraftTaskRepo{}

	got, err := repo.markDeliveryAcceptedTx(context.Background(), acceptedTx, "attempt-1", 202, "external-1")
	if err != nil {
		t.Fatalf("markDeliveryAcceptedTx returned error: %v", err)
	}
	if got.Status != StatusSent {
		t.Fatalf("accepted draft status = %q, want sent", got.Status)
	}

	failed := draft
	failed.Status = StatusSendFailed
	failed.ExternalDeliveryStatus = DeliveryStatusFailed
	failedTx := ptrext.Of(fakeTx{rows: []fakeRow{
		attemptRow(attempt),
		draftRow(draft),
		draftRow(failed),
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	err = repo.markDeliveryFailedTx(context.Background(), failedTx, "attempt-1", 500, errors.New(strings.Repeat("x", 600)))
	if err != nil {
		t.Fatalf("markDeliveryFailedTx returned error: %v", err)
	}
	if failedTx.execIdx != 3 {
		t.Fatalf("failed exec calls = %d, want attempt, draft, event", failedTx.execIdx)
	}
}

func TestRedeliveryHelpersValidateStatusAndPrepareSend(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	hook := testRepoHook(now)
	attempt := testDeliveryAttemptForScan(now, DeliveryStatusFailed)

	pending := attempt
	pending.Status = DeliveryStatusPending
	pendingTx := ptrext.Of(fakeTx{rows: []fakeRow{deliveryAttemptScanRow(pending)}})
	if _, _, err := repo.loadRedeliveryAttemptTx(context.Background(), pendingTx, "tenant-1", pending.ID); !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("pending redelivery error = %v, want ErrRequestInProgress", err)
	}

	accepted := attempt
	accepted.Status = DeliveryStatusAccepted
	acceptedTx := ptrext.Of(fakeTx{rows: []fakeRow{deliveryAttemptScanRow(accepted)}})
	if _, _, err := repo.loadRedeliveryAttemptTx(context.Background(), acceptedTx, "tenant-1", accepted.ID); !errors.Is(err, ErrInvalidDraftState) {
		t.Fatalf("accepted redelivery error = %v, want ErrInvalidDraftState", err)
	}

	validTx := ptrext.Of(fakeTx{rows: []fakeRow{deliveryAttemptScanRow(attempt), hookRow(hook)}})
	gotAttempt, gotHook, err := repo.loadRedeliveryAttemptTx(context.Background(), validTx, "tenant-1", attempt.ID)
	if err != nil {
		t.Fatalf("loadRedeliveryAttemptTx returned error: %v", err)
	}
	if gotAttempt.ID != attempt.ID || gotHook.ID != hook.ID {
		t.Fatalf("redelivery attempt/hook = %+v/%+v", gotAttempt, gotHook)
	}

	disabledHook := hook
	disabledHook.Enabled = false
	disabledHook.DisabledAt = sql.NullTime{Time: now, Valid: true}
	disabledTx := ptrext.Of(fakeTx{rows: []fakeRow{deliveryAttemptScanRow(attempt), hookRow(disabledHook)}})
	if _, _, err := repo.loadRedeliveryAttemptTx(context.Background(), disabledTx, "tenant-1", attempt.ID); !errors.Is(err, ErrHookNotFound) {
		t.Fatalf("disabled hook error = %v, want ErrHookNotFound", err)
	}

	testAttempt := attempt
	testAttempt.EventType = DeliveryEventReplyTest
	testAttempt.DraftID = ""
	testAttempt.RevisionID = ""
	prep, err := repo.prepareRedeliveryFromAttemptTx(context.Background(), ptrext.Of(fakeTx{}), testAttempt, hook, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("test redelivery prep returned error: %v", err)
	}
	if prep.EventType != DeliveryEventReplyTest || prep.Draft.ID != "" {
		t.Fatalf("test redelivery prep = %+v", prep)
	}

	draft := testRepoDraft(StatusSendFailed, now)
	draft.ID = attempt.DraftID
	draft.ApprovedRevisionID = attempt.RevisionID
	rev := testRepoRevision(draft, attempt.RevisionID, 2, "human", "Retry this.", now)
	sendTx := ptrext.Of(fakeTx{rows: []fakeRow{draftRow(draft), revisionRow(rev)}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	prep, err = repo.prepareRedeliveryFromAttemptTx(context.Background(), sendTx, attempt, hook, Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("send redelivery prep returned error: %v", err)
	}
	if prep.EventType != DeliveryEventReplySend || prep.Revision.ID != attempt.RevisionID || sendTx.execIdx != 2 {
		t.Fatalf("send redelivery prep = %+v execs=%d", prep, sendTx.execIdx)
	}
}

func TestClaimDueDeliveryTxHandlesTerminalAndRetryableAttempts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	attempt := testDeliveryAttemptForScan(now, DeliveryStatusFailed)
	hook := testRepoHook(now)

	missingHookTx := ptrext.Of(fakeTx{rows: []fakeRow{{err: pgx.ErrNoRows}}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}})

	prep, claimed, err := repo.claimDueDeliveryTx(context.Background(), missingHookTx, attempt, Actor{})
	if err != nil {
		t.Fatalf("missing hook claim returned error: %v", err)
	}
	if claimed || prep.AttemptID != "" || missingHookTx.execIdx != 1 {
		t.Fatalf("missing hook claim = prep:%+v claimed:%v execs:%d", prep, claimed, missingHookTx.execIdx)
	}

	testAttempt := attempt
	testAttempt.EventType = DeliveryEventReplyTest
	testAttempt.DraftID = ""
	testAttempt.RevisionID = ""
	claimTx := ptrext.Of(fakeTx{rows: []fakeRow{hookRow(hook)}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}})

	prep, claimed, err = repo.claimDueDeliveryTx(context.Background(), claimTx, testAttempt, Actor{Type: "system", ID: "reply-delivery-worker"})
	if err != nil {
		t.Fatalf("retryable hook test claim returned error: %v", err)
	}
	if !claimed || prep.AttemptID != testAttempt.ID || prep.EventType != DeliveryEventReplyTest {
		t.Fatalf("retryable hook test claim = prep:%+v claimed:%v", prep, claimed)
	}
}

func TestFreshHookChecksMarkStaleWhenHookChanges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	draft := testRepoDraft(StatusApproved, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	hook := testRepoHook(now)
	draft.ApprovedHookID = hook.ID
	draft.ApprovedHookFingerprint = hook.URLFingerprint

	if err := repo.ensureFreshApprovedHookTx(context.Background(), ptrext.Of(fakeTx{}), draft.TenantID, draft.FeedbackID, draft, hook, Actor{}); err != nil {
		t.Fatalf("matching hook returned error: %v", err)
	}
	changedHook := hook
	changedHook.URLFingerprint = "changed"
	staleTx := ptrext.Of(fakeTx{rows: []fakeRow{feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	err := repo.ensureFreshApprovedHookTx(context.Background(), staleTx, draft.TenantID, draft.FeedbackID, draft, changedHook, Actor{Type: "admin", ID: "admin-1"})
	if !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("changed hook error = %v, want ErrStaleDraft", err)
	}
	if staleTx.execIdx != 2 {
		t.Fatalf("changed hook execs = %d, want stale update and event", staleTx.execIdx)
	}
}

func TestDeliveryCompletionNoops(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	acceptedTestAttempt := deliveryAttemptRow{
		HookID: "hook-1", EventType: DeliveryEventReplyTest, Status: DeliveryStatusAccepted,
		Attempts: 1, MaxAttempts: maxReplyDeliveryAttempts, RequestedByType: "admin", RequestedBy: "admin-1",
	}
	got, err := repo.markDeliveryAcceptedTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{attemptRow(acceptedTestAttempt)}}), "attempt-1", 202, "external")
	if err != nil {
		t.Fatalf("accepted hook test no-op returned error: %v", err)
	}
	if got.ID != "" {
		t.Fatalf("accepted hook test draft = %+v, want empty", got)
	}

	draft := testRepoDraft(StatusApproved, now)
	acceptedReplyAttempt := acceptedTestAttempt
	acceptedReplyAttempt.DraftID = draft.ID
	acceptedReplyAttempt.RevisionID = draft.ActiveRevisionID
	acceptedReplyAttempt.Status = DeliveryStatusAccepted
	got, err = repo.markDeliveryAcceptedTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{attemptRow(acceptedReplyAttempt), draftRow(draft)}}), "attempt-1", 202, "external")
	if err != nil {
		t.Fatalf("accepted reply no-op returned error: %v", err)
	}
	if got.ID != draft.ID {
		t.Fatalf("accepted reply draft = %+v, want loaded draft", got)
	}

	err = repo.markDeliveryFailedTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{attemptRow(acceptedReplyAttempt)}}), "attempt-1", 500, errors.New("already done"))
	if err != nil {
		t.Fatalf("failed no-op returned error: %v", err)
	}
}

func TestDeliveryAttemptQueryHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	attempt := testDeliveryAttemptForScan(now, DeliveryStatusFailed)
	rows := ptrext.Of(fakeRows{rows: [][]any{deliveryAttemptValues(attempt)}})
	attempts, err := repo.claimableDeliveryAttemptsTx(context.Background(), ptrext.Of(fakeTx{queries: []*fakeRows{rows}}), 10)
	if err != nil {
		t.Fatalf("claimableDeliveryAttemptsTx returned error: %v", err)
	}
	if len(attempts) != 1 || attempts[0].ID != attempt.ID {
		t.Fatalf("claimable attempts = %+v", attempts)
	}

	got, err := repo.loadDeliveryAttemptByIDForUpdateTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{deliveryAttemptScanRow(attempt)}}), "tenant-1", attempt.ID)
	if err != nil {
		t.Fatalf("loadDeliveryAttemptByIDForUpdateTx returned error: %v", err)
	}
	if got.ID != attempt.ID || got.NextRetryAt == nil {
		t.Fatalf("loaded attempt = %+v", got)
	}
	_, err = repo.loadDeliveryAttemptByIDForUpdateTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{{err: pgx.ErrNoRows}}}), "tenant-1", attempt.ID)
	if !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("load missing attempt error = %v, want ErrDeliveryNotFound", err)
	}

	if err := repo.markDeliveryDeadTx(context.Background(), ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}}), attempt.ID, ""); err != nil {
		t.Fatalf("markDeliveryDeadTx blank reason returned error: %v", err)
	}
	if err := repo.resetDeliveryAttemptTx(context.Background(), ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}}), attempt.ID, Actor{}); err != nil {
		t.Fatalf("resetDeliveryAttemptTx without actor returned error: %v", err)
	}
	if err := repo.resetDeliveryAttemptTx(context.Background(), ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}}), attempt.ID, Actor{Type: "admin", ID: "admin-1"}); err != nil {
		t.Fatalf("resetDeliveryAttemptTx with actor returned error: %v", err)
	}
}

func TestInsertAttemptHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	draft := testRepoDraft(StatusApproved, now)
	hook := testRepoHook(now)
	rev := testRepoRevision(draft, draft.ActiveRevisionID, 1, "human", "Thanks.", now)

	attemptID, inserted, err := repo.insertDeliveryAttemptTx(
		context.Background(),
		ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("55555555-5555-5555-5555-555555555555")}}}),
		draft,
		hook,
		rev,
		"reply_send_123456",
		"request-fp",
		Actor{Type: "admin", ID: "admin-1"},
	)
	if err != nil {
		t.Fatalf("insertDeliveryAttemptTx returned error: %v", err)
	}
	if attemptID == "" || !inserted {
		t.Fatalf("inserted delivery attempt = %q inserted:%v", attemptID, inserted)
	}

	attemptID, inserted, err = repo.insertDeliveryAttemptTx(
		context.Background(),
		ptrext.Of(fakeTx{rows: []fakeRow{{err: pgx.ErrNoRows}}}),
		draft,
		hook,
		rev,
		"reply_send_123456",
		"request-fp",
		Actor{},
	)
	if err != nil {
		t.Fatalf("conflicting insertDeliveryAttemptTx returned error: %v", err)
	}
	if attemptID != "" || inserted {
		t.Fatalf("conflicting delivery attempt = %q inserted:%v, want no insert", attemptID, inserted)
	}

	attemptID, fromCache, err := repo.ensureHookTestAttemptTx(
		context.Background(),
		ptrext.Of(fakeTx{rows: []fakeRow{{scan: scanValues("66666666-6666-6666-6666-666666666666")}}}),
		hook,
		"reply_test_123456",
		Actor{Type: "admin", ID: "admin-1"},
	)
	if err != nil {
		t.Fatalf("ensureHookTestAttemptTx returned error: %v", err)
	}
	if attemptID == "" || fromCache {
		t.Fatalf("hook test attempt = %q cache:%v, want new", attemptID, fromCache)
	}
}

func TestGeneratedDraftTransactionHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	baseDraft := testRepoDraft(StatusSuggested, now)
	baseRevision := testRepoRevision(baseDraft, "22222222-2222-2222-2222-222222222222", 1, "ai", "Thanks for the report.", now)
	generatedAt := now.Add(time.Minute)

	tx := ptrext.Of(fakeTx{rows: []fakeRow{
		feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
		{err: pgx.ErrNoRows},
		draftCycleRow(baseDraft),
		revisionRow(baseRevision),
		{scan: scanValues(generatedAt)},
	}, execs: []pgconn.CommandTag{
		pgconn.NewCommandTag("UPDATE 1"),
		pgconn.NewCommandTag("INSERT 0 1"),
	}})

	gotGeneratedAt, err := repo.storeGeneratedDraftTx(context.Background(), tx, 42, "tenant-1", "Thanks for the report.", "system")
	if err != nil {
		t.Fatalf("storeGeneratedDraftTx returned error: %v", err)
	}
	if !gotGeneratedAt.Equal(generatedAt) {
		t.Fatalf("generatedAt = %s, want %s", gotGeneratedAt, generatedAt)
	}
	if tx.execIdx != 2 {
		t.Fatalf("storeGeneratedDraftTx execs = %d, want legacy sync and event insert", tx.execIdx)
	}

	existing, err := repo.ensureWritableDraftTx(context.Background(), ptrext.Of(fakeTx{
		rows: []fakeRow{draftRow(baseDraft)},
	}), "tenant-1", 42, snapshot)
	if err != nil {
		t.Fatalf("ensureWritableDraftTx editable returned error: %v", err)
	}
	if existing.ID != baseDraft.ID {
		t.Fatalf("ensureWritableDraftTx editable = %+v, want existing draft", existing)
	}

	pendingDraft := baseDraft
	pendingDraft.Status = StatusSendPending
	_, err = repo.ensureWritableDraftTx(context.Background(), ptrext.Of(fakeTx{
		rows: []fakeRow{draftRow(pendingDraft)},
	}), "tenant-1", 42, snapshot)
	if !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("ensureWritableDraftTx pending error = %v, want ErrRequestInProgress", err)
	}

	sentDraft := baseDraft
	sentDraft.Status = StatusSent
	sentDraft.CycleNo = 3
	newDraft := baseDraft
	newDraft.CycleNo = 4
	rolledTx := ptrext.Of(fakeTx{
		rows:  []fakeRow{draftRow(sentDraft), draftCycleRow(newDraft)},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
	})
	rolledDraft, err := repo.ensureWritableDraftTx(context.Background(), rolledTx, "tenant-1", 42, snapshot)
	if err != nil {
		t.Fatalf("ensureWritableDraftTx sent returned error: %v", err)
	}
	if rolledDraft.CycleNo != 4 || rolledTx.execIdx != 1 {
		t.Fatalf("rolled draft = %+v execs=%d, want archived cycle 4", rolledDraft, rolledTx.execIdx)
	}
}

func TestGeneratedDraftLegacySyncFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	snapshot := feedbackSnapshot{Fingerprint: "source-fp", Metadata: []byte(`{}`)}
	boom := errors.New("legacy failed")

	_, err := repo.markGeneratedTx(context.Background(), ptrext.Of(fakeTx{
		rows:     []fakeRow{{scan: scanValues(now)}},
		execErrs: []error{boom},
	}), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "content", "system", snapshot)
	if err == nil {
		t.Fatalf("markGeneratedTx error = nil, want legacy sync error")
	}

	_, err = repo.markGeneratedTx(context.Background(), ptrext.Of(fakeTx{
		rows:  []fakeRow{{scan: scanValues(now)}},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")},
	}), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "content", "system", snapshot)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("markGeneratedTx missing legacy row = %v, want ErrNotFound", err)
	}

	_, err = repo.markGeneratedTx(context.Background(), ptrext.Of(fakeTx{
		rows: []fakeRow{{err: boom}},
	}), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "content", "system", snapshot)
	if err == nil {
		t.Fatalf("markGeneratedTx update error = nil, want wrapped row error")
	}
}

func TestDraftWriteHelpersReturnErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := DraftTaskRepo{}
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	draft := testRepoDraft(StatusSuggested, now)
	snapshot := feedbackSnapshot{Fingerprint: "source-fp", Metadata: []byte(`{}`)}
	boom := errors.New("write failed")

	if err := repo.archiveDraftTx(ctx, ptrext.Of(fakeTx{execErrs: []error{boom}}), draft.ID); err == nil {
		t.Fatalf("archiveDraftTx error = nil, want wrapped exec error")
	}
	if _, err := repo.insertDraftCycleTx(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: boom}}}), draft.TenantID, draft.FeedbackID, 1, snapshot); err == nil {
		t.Fatalf("insertDraftCycleTx error = nil, want wrapped row error")
	}
	if _, err := repo.insertRevisionTx(ctx, ptrext.Of(fakeTx{}), draft, "ai", " ", nil, "system"); !errors.Is(err, ErrInvalidDraftState) {
		t.Fatalf("insertRevisionTx blank content = %v, want ErrInvalidDraftState", err)
	}
	if _, err := repo.insertRevisionTx(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: boom}}}), draft, "ai", "content", nil, "system"); err == nil {
		t.Fatalf("insertRevisionTx row error = nil, want wrapped row error")
	}
	if err := repo.insertEventTx(ctx, ptrext.Of(fakeTx{execErrs: []error{boom}}), draft, draft.ActiveRevisionID, "", "edit", Actor{}, "", nil); err == nil {
		t.Fatalf("insertEventTx error = nil, want wrapped exec error")
	}
}

func TestEditDraftHelperPropagatesLegacySyncFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := DraftTaskRepo{}
	draftID := "11111111-1111-1111-1111-111111111111"
	revisionID := "22222222-2222-2222-2222-222222222222"
	boom := errors.New("legacy failed")

	err := repo.markEditedTx(ctx, ptrext.Of(fakeTx{execErrs: []error{boom}}), draftID, revisionID, "content", Actor{ID: "admin-1"})
	if err == nil {
		t.Fatalf("markEditedTx update error = nil, want wrapped exec error")
	}
	err = repo.markEditedTx(ctx, ptrext.Of(fakeTx{execErrs: []error{nil, boom}}), draftID, revisionID, "content", Actor{ID: "admin-1"})
	if err == nil {
		t.Fatalf("markEditedTx legacy sync error = nil, want wrapped exec error")
	}
	err = repo.markEditedTx(ctx, ptrext.Of(fakeTx{execs: []pgconn.CommandTag{
		pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 0"),
	}}), draftID, revisionID, "content", Actor{ID: "admin-1"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("markEditedTx missing legacy row = %v, want ErrNotFound", err)
	}
}

func TestApproveDraftTxRejectsInvalidAndHookFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	repo := DraftTaskRepo{}

	noRevision := testRepoDraft(StatusSuggested, now)
	noRevision.ActiveRevisionID = ""
	_, err := repo.approveDraftTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{draftRow(noRevision)}}), noRevision.TenantID, noRevision.FeedbackID, noRevision.Revision, Actor{})
	if !errors.Is(err, ErrInvalidDraftState) {
		t.Fatalf("approveDraftTx no active revision = %v, want ErrInvalidDraftState", err)
	}

	draft := testRepoDraft(StatusSuggested, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	_, err = repo.approveDraftTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
		{err: pgx.ErrNoRows},
	}}), draft.TenantID, draft.FeedbackID, draft.Revision, Actor{})
	if !errors.Is(err, ErrHookNotFound) {
		t.Fatalf("approveDraftTx missing hook = %v, want ErrHookNotFound", err)
	}
}

func TestApproveDraftTxWriteFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	draft := testRepoDraft(StatusSuggested, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	hook := testRepoHook(now)
	repo := DraftTaskRepo{}
	boom := errors.New("approve failed")

	for _, execErrs := range [][]error{{boom}, {nil, boom}} {
		_, err := repo.approveDraftTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{
			draftRow(draft),
			feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
			hookRow(hook),
		}, execErrs: execErrs}), draft.TenantID, draft.FeedbackID, draft.Revision, Actor{Type: "admin", ID: "admin-1"})
		if err == nil {
			t.Fatalf("approveDraftTx execErrs=%v returned nil, want error", execErrs)
		}
	}
	_, err := repo.approveDraftTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
		hookRow(hook),
		{err: boom},
	}}), draft.TenantID, draft.FeedbackID, draft.Revision, Actor{Type: "admin", ID: "admin-1"})
	if err == nil {
		t.Fatalf("approveDraftTx reload error = nil, want wrapped row error")
	}
}

func TestRejectDraftTxFailureBranches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	repo := DraftTaskRepo{}
	sent := testRepoDraft(StatusSent, now)
	boom := errors.New("reject failed")

	_, err := repo.rejectDraftTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{draftRow(sent)}}), sent.TenantID, sent.FeedbackID, sent.Revision, Actor{})
	if !errors.Is(err, ErrInvalidDraftState) {
		t.Fatalf("rejectDraftTx sent draft = %v, want ErrInvalidDraftState", err)
	}

	for _, execErrs := range [][]error{{boom}, {nil, boom}} {
		draft := testRepoDraft(StatusSuggested, now)
		_, err = repo.rejectDraftTx(context.Background(), ptrext.Of(fakeTx{rows: []fakeRow{draftRow(draft)}, execErrs: execErrs}), draft.TenantID, draft.FeedbackID, draft.Revision, Actor{Type: "admin", ID: "admin-1"})
		if err == nil {
			t.Fatalf("rejectDraftTx execErrs=%v returned nil, want error", execErrs)
		}
	}
}

func TestLoadFreshDeliveryHookMarksStaleWhenApprovedHookDisappears(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	draft := testRepoDraft(StatusApproved, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	draft.ApprovedHookID = "33333333-3333-3333-3333-333333333333"
	draft.ApprovedHookFingerprint = "hook-fp"
	tx := ptrext.Of(fakeTx{rows: []fakeRow{
		{err: pgx.ErrNoRows},
		feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
	}, execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("INSERT 0 1")}})

	repo := DraftTaskRepo{}

	_, err := repo.loadFreshDeliveryHookTx(context.Background(), tx, draft.TenantID, draft.FeedbackID, draft, Actor{Type: "admin", ID: "admin-1"})

	if !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("missing approved hook error = %v, want ErrStaleDraft", err)
	}
	if tx.execIdx != 2 {
		t.Fatalf("missing approved hook execs = %d, want stale update and event", tx.execIdx)
	}
}

func TestEnsureReplySendRedeliveryFreshTx(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	snapshot := testFeedbackSnapshot("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed")
	hook := testRepoHook(now)
	draft := testRepoDraft(StatusSendFailed, now)
	draft.SourceFingerprint = snapshot.Fingerprint
	draft.ApprovedRevisionID = draft.ActiveRevisionID
	draft.ApprovedHookID = hook.ID
	draft.ApprovedHookFingerprint = hook.URLFingerprint
	attempt := testDeliveryAttemptForScan(now, DeliveryStatusFailed)
	attempt.DraftID = draft.ID
	attempt.RevisionID = draft.ApprovedRevisionID
	attempt.HookID = hook.ID
	tx := ptrext.Of(fakeTx{rows: []fakeRow{
		draftRow(draft),
		feedbackSnapshotRow("Login fails", "web", "user-1", `{}`, "Login failure", "Cannot login", `{}`, "en", "completed"),
	}})

	repo := DraftTaskRepo{}

	if err := repo.ensureReplySendRedeliveryFreshTx(context.Background(), tx, attempt, hook, Actor{}); err != nil {
		t.Fatalf("ensureReplySendRedeliveryFreshTx returned error: %v", err)
	}

	badAttempt := attempt
	badAttempt.RevisionID = "99999999-9999-9999-9999-999999999999"
	tx = ptrext.Of(fakeTx{rows: []fakeRow{draftRow(draft)}})
	if err := repo.ensureReplySendRedeliveryFreshTx(context.Background(), tx, badAttempt, hook, Actor{}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("revision mismatch error = %v, want ErrRevisionConflict", err)
	}
}

type fakeRow struct {
	scan func(dest ...any) error
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return r.scan(dest...)
}

type fakeRows struct {
	rows    [][]any
	idx     int
	err     error
	scanErr error
}

func (r *fakeRows) Close() {}
func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeRows) Next() bool {
	if r.idx == 0 && len(r.rows) == 0 {
		return false
	}
	r.idx++
	return r.idx <= len(r.rows)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan without current row")
	}
	return scanValues(r.rows[r.idx-1]...)(dest...)
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

func scanValues(values ...any) func(dest ...any) error {
	return func(dest ...any) error {
		if len(dest) != len(values) {
			return errors.New("scan value count mismatch")
		}
		for i := range dest {
			if err := assignScanValue(dest[i], values[i]); err != nil {
				return err
			}
		}
		return nil
	}
}

func assignScanValue(dest any, value any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		return errors.New("unsupported scan destination")
	}
	elem := destValue.Elem()

	switch elem.Type() {
	case reflect.TypeOf(""):
		elem.SetString(value.(string))
	case reflect.TypeOf(0):
		elem.SetInt(int64(value.(int)))
	case reflect.TypeOf(int64(0)):
		elem.SetInt(value.(int64))
	case reflect.TypeOf(false):
		elem.SetBool(value.(bool))
	case reflect.TypeOf([]byte(nil)):
		elem.SetBytes(append([]byte(nil), value.([]byte)...))
	case reflect.TypeOf(time.Time{}), reflect.TypeOf(sql.NullString{}), reflect.TypeOf(sql.NullTime{}):
		elem.Set(reflect.ValueOf(value))
	default:
		return errors.New("unsupported scan destination")
	}
	return nil
}

type fakeTx struct {
	rows     []fakeRow
	rowIdx   int
	queries  []*fakeRows
	queryIdx int
	execs    []pgconn.CommandTag
	execErrs []error
	execIdx  int
}

func (tx *fakeTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeTx) Commit(context.Context) error          { return nil }
func (tx *fakeTx) Rollback(context.Context) error        { return nil }
func (tx *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (tx *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
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

func (tx *fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.queryIdx >= len(tx.queries) {
		return ptrext.Of(fakeRows{}), nil
	}
	rows := tx.queries[tx.queryIdx]
	tx.queryIdx++
	return rows, nil
}

func (tx *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return fakeRow{err: errors.New("unexpected QueryRow")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}
func (tx *fakeTx) Conn() *pgx.Conn { return nil }

func testRepoDraft(status string, now time.Time) Draft {
	return Draft{
		ID:                      "11111111-1111-1111-1111-111111111111",
		TenantID:                "tenant-1",
		FeedbackID:              42,
		CycleNo:                 1,
		Status:                  status,
		ActiveRevisionID:        "22222222-2222-2222-2222-222222222221",
		ApprovedHookFingerprint: "",
		ActiveContent:           "Generated reply",
		SourceFingerprint:       "source-fp",
		Revision:                7,
		CreatedAt:               now.Add(-time.Hour),
		UpdatedAt:               now,
	}
}

func testRepoRevision(draft Draft, id string, revisionNo int, origin string, content string, now time.Time) Revision {
	return Revision{
		ID:                id,
		DraftID:           draft.ID,
		TenantID:          draft.TenantID,
		FeedbackID:        draft.FeedbackID,
		CycleNo:           draft.CycleNo,
		RevisionNo:        revisionNo,
		Origin:            origin,
		Content:           content,
		SourceFingerprint: draft.SourceFingerprint,
		Metadata:          []byte(`{}`),
		CreatedBy:         "admin-1",
		CreatedAt:         now,
	}
}

func testRepoHook(now time.Time) Hook {
	return Hook{
		ID:               "33333333-3333-3333-3333-333333333333",
		TenantID:         "tenant-1",
		Name:             "Reply hook",
		URLCiphertext:    []byte("url"),
		URLKeyID:         "url-key",
		URLFingerprint:   "hook-fp",
		URLHost:          "hooks.example.test",
		SecretCiphertext: []byte("secret"),
		SecretKeyID:      "secret-key",
		Enabled:          true,
		CreatedBy:        "admin-1",
		UpdatedBy:        "admin-1",
		CreatedAt:        now.Add(-time.Hour),
		UpdatedAt:        now,
	}
}

func testFeedbackSnapshot(content, source, userID, sourceMeta, title, rationale, attrs, language, status string) feedbackSnapshot {
	return feedbackSnapshot{
		Fingerprint: fingerprint(content, source, userID, sourceMeta, title, rationale, attrs, language, status),
		Metadata:    []byte(`{"enrichment_status":"` + status + `","language":"` + language + `","source":"` + source + `"}`),
	}
}

func draftRow(d Draft) fakeRow {
	return fakeRow{scan: scanValues(
		d.ID, d.TenantID, d.FeedbackID, d.CycleNo, d.Status,
		nullStringValue(d.ActiveRevisionID), nullStringValue(d.ApprovedRevisionID),
		nullStringValue(d.SentRevisionID), nullStringValue(d.ApprovedHookID),
		d.ApprovedHookFingerprint, d.ActiveContent, d.SourceFingerprint,
		d.LastBlocker, d.ExternalDeliveryStatus, d.ExternalMessageID,
		nullTimeValue(d.GeneratedAt), d.GeneratedBy, nullTimeValue(d.EditedAt), d.EditedBy,
		nullTimeValue(d.ApprovedAt), d.ApprovedBy, nullTimeValue(d.RejectedAt), d.RejectedBy,
		nullTimeValue(d.SentAt), d.SentBy, d.Revision, d.CreatedAt, d.UpdatedAt,
	)}
}

func draftCycleRow(d Draft) fakeRow {
	return fakeRow{scan: scanValues(
		d.ID, d.TenantID, d.FeedbackID, d.CycleNo, d.Status,
		d.SourceFingerprint, d.LastBlocker, d.ExternalDeliveryStatus,
		d.ExternalMessageID, d.Revision, d.CreatedAt, d.UpdatedAt,
	)}
}

func revisionRow(rev Revision) fakeRow {
	return fakeRow{scan: scanValues(
		rev.ID, rev.DraftID, rev.TenantID, rev.FeedbackID, rev.CycleNo,
		rev.RevisionNo, rev.Origin, rev.Content, rev.SourceFingerprint,
		rev.Metadata, rev.CreatedBy, rev.CreatedAt,
	)}
}

func hookRow(h Hook) fakeRow {
	return fakeRow{scan: scanValues(
		h.ID, h.TenantID, h.Name, h.URLCiphertext, h.URLKeyID,
		h.URLFingerprint, h.URLHost, h.SecretCiphertext, h.SecretKeyID,
		h.Enabled, h.CreatedBy, h.UpdatedBy, h.DisabledAt, h.CreatedAt, h.UpdatedAt,
	)}
}

func feedbackSnapshotRow(content, source, userID, sourceMeta, title, rationale, attrs, language, status string) fakeRow {
	return fakeRow{scan: scanValues(content, source, userID, []byte(sourceMeta), title, rationale, []byte(attrs), language, status)}
}

func testDeliveryAttemptForScan(now time.Time, status string) DeliveryAttempt {
	return DeliveryAttempt{
		ID:              "55555555-5555-5555-5555-555555555555",
		TenantID:        "tenant-1",
		DraftID:         "11111111-1111-1111-1111-111111111111",
		FeedbackID:      42,
		HookID:          "33333333-3333-3333-3333-333333333333",
		HookHost:        "hooks.example.test",
		HookFingerprint: "hook-fp",
		RevisionID:      "22222222-2222-2222-2222-222222222221",
		EventType:       DeliveryEventReplySend,
		IdempotencyKey:  "reply_send_123456",
		Status:          status,
		HTTPStatus:      500,
		Attempts:        2,
		MaxAttempts:     maxReplyDeliveryAttempts,
		NextRetryAt:     ptrext.Of(now.Add(time.Minute)),
		Error:           "receiver failed",
		RequestedByType: "admin",
		RequestedBy:     "admin-1",
		RequestedAt:     now.Add(-time.Minute),
		CreatedAt:       now.Add(-time.Minute),
		UpdatedAt:       now,
	}
}

func deliveryAttemptScanRow(attempt DeliveryAttempt) fakeRow {
	return fakeRow{scan: scanValues(deliveryAttemptValues(attempt)...)}
}

func deliveryAttemptValues(attempt DeliveryAttempt) []any {
	return []any{
		attempt.ID, attempt.TenantID, attempt.DraftID, attempt.FeedbackID,
		attempt.HookID, attempt.HookHost, attempt.HookFingerprint,
		attempt.RevisionID, attempt.EventType, attempt.IdempotencyKey,
		attempt.Status, attempt.HTTPStatus, attempt.Attempts, attempt.MaxAttempts,
		nullTimeValue(attempt.NextRetryAt), attempt.ExternalMessageID, attempt.Error,
		attempt.RequestedByType, attempt.RequestedBy, attempt.RequestedAt,
		nullTimeValue(attempt.CompletedAt), attempt.CreatedAt, attempt.UpdatedAt,
	}
}

func attemptRow(attempt deliveryAttemptRow) fakeRow {
	return fakeRow{scan: scanValues(
		nullStringValue(attempt.DraftID), attempt.HookID, nullStringValue(attempt.RevisionID),
		attempt.EventType, attempt.Status, attempt.Attempts, attempt.MaxAttempts,
		attempt.RequestedByType, attempt.RequestedBy,
	)}
}

func nullStringValue(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullTimeValue(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: ptrext.Indirect(value), Valid: true}
}

func TestNullHelpers(t *testing.T) {
	t.Parallel()

	if got := nullString(sql.NullString{}); got != "" {
		t.Fatalf("nullString invalid = %q, want empty", got)
	}
	if got := nullString(sql.NullString{String: "value", Valid: true}); got != "value" {
		t.Fatalf("nullString valid = %q, want value", got)
	}
	if got := nullTime(sql.NullTime{}); got != nil {
		t.Fatalf("nullTime invalid = %v, want nil", got)
	}
	now := time.Now().UTC()
	got := nullTime(sql.NullTime{Time: now, Valid: true})
	if got == nil || !ptrext.Indirect(got).Equal(now) {
		t.Fatalf("nullTime valid = %v, want %v", got, now)
	}
}
