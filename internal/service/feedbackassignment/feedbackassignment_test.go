package feedbackassignment

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

func TestNormalizeTrimsAndClearsOwner(t *testing.T) {
	dueAt := time.Date(2026, 8, 1, 9, 30, 0, 0, time.FixedZone("SGT", 8*60*60))
	got, err := normalize(Input{
		TenantID:         " tenant-a ",
		FeedbackID:       42,
		OwnerMemberIDSet: true,
		OwnerMemberID:    ptrext.Of(" "),
		SLADueAtSet:      true,
		SLADueAt:         ptrext.Of(dueAt),
		Note:             " assign ",
		ActorID:          " user-a ",
	})
	if err != nil {
		t.Fatalf("normalize() error = %v", err)
	}
	if got.TenantID != "tenant-a" || got.ActorID != "user-a" || got.Note != "assign" {
		t.Fatalf("normalize trim = %#v", got)
	}
	if got.OwnerMemberID != nil {
		t.Fatalf("OwnerMemberID = %v, want nil clear", got.OwnerMemberID)
	}
	if got.SLADueAt == nil || !got.SLADueAt.Equal(dueAt.UTC()) {
		t.Fatalf("SLADueAt = %v, want UTC %v", got.SLADueAt, dueAt.UTC())
	}
}

func TestNormalizeBatchDedupeLimitAndIntent(t *testing.T) {
	dueAt := time.Date(2026, 8, 1, 9, 30, 0, 0, time.FixedZone("SGT", 8*60*60))
	got, err := normalizeBatch(BatchInput{
		TenantID:         " tenant-a ",
		FeedbackIDs:      []int64{42, 42, 0, -1, 43},
		OwnerMemberIDSet: true,
		OwnerMemberID:    ptrext.Of(" owner-a "),
		SLADueAtSet:      true,
		SLADueAt:         ptrext.Of(dueAt),
		Note:             " batch handoff ",
		ActorID:          " user-a ",
	})
	if err != nil {
		t.Fatalf("normalizeBatch() error = %v", err)
	}
	if got.TenantID != "tenant-a" || got.ActorID != "user-a" || got.Note != "batch handoff" {
		t.Fatalf("normalizeBatch trim = %#v", got)
	}
	if len(got.FeedbackIDs) != 2 || got.FeedbackIDs[0] != 42 || got.FeedbackIDs[1] != 43 {
		t.Fatalf("FeedbackIDs = %v, want [42 43]", got.FeedbackIDs)
	}
	if got.OwnerMemberID == nil || ptrext.Indirect(got.OwnerMemberID) != "owner-a" {
		t.Fatalf("OwnerMemberID = %v, want owner-a", got.OwnerMemberID)
	}
	if got.SLADueAt == nil || !got.SLADueAt.Equal(dueAt.UTC()) {
		t.Fatalf("SLADueAt = %v, want UTC %v", got.SLADueAt, dueAt.UTC())
	}
}

func TestNormalizeBatchRejectsEmptyIntentAndLargeSelection(t *testing.T) {
	if _, err := normalizeBatch(BatchInput{
		TenantID:    "tenant-a",
		FeedbackIDs: []int64{1},
		ActorID:     "user-a",
	}); err == nil {
		t.Fatal("normalizeBatch() error = nil, want empty intent validation error")
	}

	ids := make([]int64, 0, MaxBatchSize+1)
	for i := int64(1); i <= MaxBatchSize+1; i++ {
		ids = append(ids, i)
	}
	if _, err := normalizeBatch(BatchInput{
		TenantID:         "tenant-a",
		FeedbackIDs:      ids,
		OwnerMemberIDSet: true,
		OwnerMemberID:    ptrext.Of("owner-a"),
		ActorID:          "user-a",
	}); err == nil {
		t.Fatal("normalizeBatch() error = nil, want batch-size validation error")
	}
}

func TestAssignmentAuditEntriesCaptureChangedFields(t *testing.T) {
	oldDue := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newDue := oldDue.Add(24 * time.Hour)
	entries := assignmentAuditEntries(
		Input{TenantID: "tenant-a", FeedbackID: 7, ActorID: "user-a", Note: "handoff"},
		feedbackrepo.Assignment{
			FeedbackID:    7,
			OwnerMemberID: ptrext.Of("owner-a"),
			SLADueAt:      ptrext.Of(oldDue),
			Note:          "old",
		},
		feedbackrepo.Assignment{
			FeedbackID:    7,
			OwnerMemberID: ptrext.Of("owner-b"),
			SLADueAt:      ptrext.Of(newDue),
			Note:          "new",
		},
	)
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	wantFields := []string{"owner_member_id", "feedback_sla_due_at", "owner_assignment_note"}
	for i, field := range wantFields {
		if entries[i].FieldName != field || entries[i].EntityType != "feedback_assignment" {
			t.Fatalf("entry[%d] = %#v, want field %s", i, entries[i], field)
		}
	}
}
