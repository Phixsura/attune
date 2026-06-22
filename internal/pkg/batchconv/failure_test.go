// SPDX-License-Identifier: Apache-2.0

package batchconv

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

func TestItemFailuresToProto(t *testing.T) {
	t.Parallel()

	t.Run("empty slice returns nil", func(t *testing.T) {
		t.Parallel()
		got := ItemFailuresToProto(nil)
		if got != nil {
			t.Errorf("ItemFailuresToProto(nil) = %v, want nil", got)
		}
	})

	t.Run("converts multiple failures", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
		failures := []feedback.ItemFailure{
			{FeedbackID: 1, Code: "not_found", Message: "not found"},
			{FeedbackID: 2, Code: "conflict", Message: "conflict", UpdatedAt: ptrext.Of(now)},
		}

		got := ItemFailuresToProto(failures)
		if len(got) != 2 {
			t.Fatalf("ItemFailuresToProto() returned %d items, want 2", len(got))
		}
		if got[0].GetFeedbackId() != 1 || got[0].GetCode() != "not_found" {
			t.Errorf("first item = %v", got[0])
		}
		if got[1].GetFeedbackId() != 2 || got[1].GetCurrentUpdatedAt() == "" {
			t.Errorf("second item = %v", got[1])
		}
	})
}

func TestItemFailureToProto(t *testing.T) {
	t.Parallel()

	t.Run("without updated_at", func(t *testing.T) {
		t.Parallel()
		f := feedback.ItemFailure{
			FeedbackID: 123,
			Code:       "not_found",
			Message:    "feedback not found",
		}

		got := ItemFailureToProto(f)
		if got.GetFeedbackId() != 123 {
			t.Errorf("FeedbackId = %d, want 123", got.GetFeedbackId())
		}
		if got.GetCode() != "not_found" {
			t.Errorf("Code = %q, want 'not_found'", got.GetCode())
		}
		if got.GetMessage() != "feedback not found" {
			t.Errorf("Message = %q, want 'feedback not found'", got.GetMessage())
		}
		if got.CurrentUpdatedAt != nil {
			t.Errorf("CurrentUpdatedAt = %v, want nil", got.CurrentUpdatedAt)
		}
	})

	t.Run("with updated_at", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 22, 12, 30, 45, 0, time.UTC)
		f := feedback.ItemFailure{
			FeedbackID: 456,
			Code:       "conflict",
			Message:    "update conflict",
			UpdatedAt:  ptrext.Of(now),
		}

		got := ItemFailureToProto(f)
		if got.GetCurrentUpdatedAt() != "2026-06-22T12:30:45Z" {
			t.Errorf("CurrentUpdatedAt = %q, want '2026-06-22T12:30:45Z'", got.GetCurrentUpdatedAt())
		}
	})
}
