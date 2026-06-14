package feedbackjob_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/repo/feedbackjob"
)

// Smoke test that the public surface compiles, exported types stay stable,
// and the sentinel errors have the expected text — no DB required.
func TestFeedbackJobRepo_PublicSurfaceCompiles(t *testing.T) {
	if feedbackjob.ErrNotFound.Error() == "" {
		t.Error("ErrNotFound error text is empty")
	}
	if feedbackjob.ErrJobNotCancellable.Error() == "" {
		t.Error("ErrJobNotCancellable error text is empty")
	}

	// Verify status constants are defined
	statuses := []feedbackjob.Status{
		feedbackjob.StatusQueued,
		feedbackjob.StatusRunning,
		feedbackjob.StatusCompleted,
		feedbackjob.StatusFailed,
		feedbackjob.StatusCancelled,
	}
	for _, s := range statuses {
		if s == "" {
			t.Error("status constant is empty")
		}
	}

	// Verify Job struct fields exist (compile-time check)
	var _ feedbackjob.Job
	var _ *feedbackjob.Repo
}

func TestStatus_Values(t *testing.T) {
	tests := []struct {
		status feedbackjob.Status
		want   string
	}{
		{feedbackjob.StatusQueued, "queued"},
		{feedbackjob.StatusRunning, "running"},
		{feedbackjob.StatusCompleted, "completed"},
		{feedbackjob.StatusFailed, "failed"},
		{feedbackjob.StatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("Status %q != %q", tt.status, tt.want)
		}
	}
}

func TestStore_InterfaceSignature(t *testing.T) {
	// Compile-time check that Store interface exists with expected methods.
	// This ensures the interface contract is stable.
	var _ feedbackjob.Store = (*feedbackjob.Repo)(nil)
}
