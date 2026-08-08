// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestPublicResponseQualityFlagsKnownAutomationAndFastHostedCompletion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 2, 0, 3, 0, time.UTC)
	invitation := repo.Invitation{
		ResponseStatus: repo.ResponseStarted,
		OpenedAt:       pointerToTime(now.Add(-2 * time.Second)),
		ID:             uuid.New(),
	}
	got := publicResponseQualityFlags(invitation, "Mozilla/5.0 HeadlessChrome", "203.0.113.9", now)
	want := []string{"automated_client", "submitted_too_quickly"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("publicResponseQualityFlags() = %#v, want %#v", got, want)
	}
}

func TestPublicResponseQualityFlagsMarkMissingContextAndDirectSubmission(t *testing.T) {
	t.Parallel()

	got := publicResponseQualityFlags(repo.Invitation{ResponseStatus: repo.ResponseNotStarted}, "", "", time.Now().UTC())
	want := []string{"missing_user_agent", "missing_client_address", "submitted_without_page_visit"}
	if len(got) != len(want) {
		t.Fatalf("publicResponseQualityFlags() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("quality flag %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPublicResponseQualityMetadataDropsUnknownCallerFlags(t *testing.T) {
	t.Parallel()

	metadata := publicResponseQualityMetadata([]string{
		"automated_client", "automated_client", "not-a-supported-reason",
	})
	if got := repo.ResponseQualityStatus(metadata); got != repo.ResponseQualityStatusFlagged {
		t.Fatalf("quality status = %q, want flagged", got)
	}
	reasons := repo.ResponseQualityReasons(metadata)
	if len(reasons) != 1 || reasons[0] != "automated_client" {
		t.Fatalf("quality reasons = %#v, want one supported reason", reasons)
	}
}

func pointerToTime(value time.Time) *time.Time {
	return ptrext.Of(value)
}
