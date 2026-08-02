// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestFeedbackAssignmentEscalationsValidateOwnerAndCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	ownerID := uuid.MustParse("11111111-1111-1111-1111-111111111111").String()
	repo := FeedbackRepo{pool: ptrext.Of(fakeSignalTracePool{
		rows: []fakeSignalTraceRow{
			{values: []any{int64(1), int64(1), int64(1), int64(1)}},
			{values: []any{true}},
		},
		queries: []*fakeSignalTraceRows{
			{rows: [][]any{assignmentEscalationValues(42, ownerID, now), assignmentEscalationMissingOwnerValues(43, now)}},
			{rows: [][]any{assignmentCandidateValues(42, ownerID, now)}},
		},
	})}

	queue, err := repo.FeedbackAssignmentEscalations(ctx, " tenant-a ", now, 0)
	if err != nil || queue.GeneratedAt != now || queue.Items[0].Priority != "critical" {
		t.Fatalf("FeedbackAssignmentEscalations() = %#v, %v", queue, err)
	}
	if queue.Items[1].Priority != "high" || queue.Items[1].Assignment.OwnerMemberID != nil {
		t.Fatalf("missing owner escalation = %#v, want high priority without owner", queue.Items[1])
	}
	if err := repo.ValidateAssignmentOwner(ctx, "tenant-a", ownerID); err != nil {
		t.Fatalf("ValidateAssignmentOwner() error = %v", err)
	}
	candidates, err := repo.AssignmentCandidates(ctx, "tenant-a", []int64{0, 42, 42})
	if err != nil || len(candidates) != 1 || candidates[0].Assignment.OwnerMemberID == nil {
		t.Fatalf("AssignmentCandidates() = %#v, %v", candidates, err)
	}
}

func TestFeedbackAssignmentTxPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	ownerID := uuid.MustParse("11111111-1111-1111-1111-111111111111").String()
	tx := ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{
		{values: assignmentValues(42, ownerID, now)},
		{values: assignmentValues(42, ownerID, now)},
	}})
	repo := FeedbackRepo{}

	got, err := repo.AssignmentForUpdate(ctx, tx, " tenant-a ", 42)
	if err != nil || got.OwnerMemberID == nil || got.OwnerEmail != "owner@example.test" {
		t.Fatalf("AssignmentForUpdate() = %#v, %v", got, err)
	}
	assigned, err := repo.AssignFeedbackTx(ctx, tx, "tenant-a", 42, AssignmentInput{
		OwnerMemberIDSet: true, OwnerMemberID: ptrext.Of(ownerID), SLADueAtSet: true,
		SLADueAt: ptrext.Of(now), Note: "owner set", ActorID: "admin-1",
	})
	if err != nil || assigned.FeedbackID != 42 || tx.execIdx != 1 {
		t.Fatalf("AssignFeedbackTx() = %#v exec=%d err=%v", assigned, tx.execIdx, err)
	}
}

func TestFeedbackAssignmentHelpersCoverBoundsAndErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	if normalizeAssignmentEscalationTime(time.Time{}).IsZero() || normalizeAssignmentEscalationTime(now) != now {
		t.Fatal("normalizeAssignmentEscalationTime did not handle zero and explicit values")
	}
	if normalizeAssignmentEscalationLimit(0) != defaultAssignmentEscalationLimit ||
		normalizeAssignmentEscalationLimit(999) != maxAssignmentEscalationLimit {
		t.Fatal("normalizeAssignmentEscalationLimit did not clamp values")
	}
	dueSoon := now.Add(time.Hour)
	reasons := assignmentEscalationReasons(Assignment{SLADueAt: ptrext.Of(dueSoon)}, now)
	if assignmentEscalationPriority(reasons) != "medium" || ptrext.Indirect(assignmentEscalationHoursUntilDue(ptrext.Of(dueSoon), now)) != 1 {
		t.Fatalf("due soon escalation = %v", reasons)
	}
	if !errors.Is(ptrext.Of(FeedbackRepo{}).ValidateAssignmentOwner(context.Background(), "tenant-a", "bad-id"), ErrAssignmentOwnerNotFound) {
		t.Fatal("ValidateAssignmentOwner() did not reject malformed owner ids")
	}
}

func assignmentEscalationValues(feedbackID int64, ownerID string, now time.Time) []any {
	return []any{
		feedbackID, "Export fails", "api", "bug", true, now.Add(-2 * time.Hour),
		sql.NullString{String: ownerID, Valid: true},
		sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		"admin-1",
		sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		"owner set",
		"user", "user-1", "owner@example.test", "member", "acct:acme", "Acme Corp", "source_meta",
	}
}

func assignmentEscalationMissingOwnerValues(feedbackID int64, now time.Time) []any {
	return []any{
		feedbackID, "Billing issue", "api", "bug", false, now.Add(-time.Hour),
		sql.NullString{},
		sql.NullTime{},
		"",
		sql.NullTime{},
		"", "", "", "", "", "acct:beta", "Beta", "source_meta",
	}
}

func assignmentCandidateValues(feedbackID int64, ownerID string, now time.Time) []any {
	return []any{
		feedbackID, now.Add(-2 * time.Hour), "api", "bug", true, "done", 1,
		sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		"open", true,
		sql.NullString{String: ownerID, Valid: true},
		sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		"admin-1",
		sql.NullTime{Time: now.Add(24 * time.Hour), Valid: true},
		"owner set",
		"user", "user-1", "owner@example.test", "member",
	}
}

func assignmentValues(feedbackID int64, ownerID string, now time.Time) []any {
	return []any{
		feedbackID,
		sql.NullString{String: ownerID, Valid: true},
		sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		"admin-1",
		sql.NullTime{Time: now.Add(24 * time.Hour), Valid: true},
		"owner set",
		"user", "user-1", "owner@example.test", "member",
	}
}
