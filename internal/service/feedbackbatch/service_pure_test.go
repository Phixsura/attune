// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package feedbackbatch

import (
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
)

func TestOperationType_AllVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		op   *attunev1.BatchOperation
		want string
	}{
		{"nil", nil, "unknown"},
		{"empty", &attunev1.BatchOperation{}, "unknown"},
		{
			"tag",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Tag{
					Tag: &attunev1.BatchTagOp{AddTagIds: []string{"t1"}},
				},
			},
			"tag",
		},
		{
			"workflow",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Workflow{
					Workflow: &attunev1.BatchWorkflowOp{ToStateId: "s1"},
				},
			},
			"workflow",
		},
		{
			"delete soft",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Delete{
					Delete: &attunev1.BatchDeleteOp{Hard: false},
				},
			},
			"delete",
		},
		{
			"delete hard",
			&attunev1.BatchOperation{
				Op: &attunev1.BatchOperation_Delete{
					Delete: &attunev1.BatchDeleteOp{Hard: true},
				},
			},
			"delete",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := operationType(tc.op)
			if got != tc.want {
				t.Fatalf("operationType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateRequest_Parallel(t *testing.T) {
	t.Parallel()

	svc := &service{}

	cases := []struct {
		name    string
		req     *BatchRequest
		wantErr error
	}{
		{
			"valid with IDs and tag op",
			&BatchRequest{
				FeedbackIDs: []int64{1},
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
				},
			},
			nil,
		},
		{
			"valid with filter and delete op",
			&BatchRequest{
				Filter: &attunev1.FeedbackFilter{},
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Delete{Delete: &attunev1.BatchDeleteOp{}},
				},
			},
			nil,
		},
		{
			"nil operation",
			&BatchRequest{FeedbackIDs: []int64{1}},
			ErrNoOperation,
		},
		{
			"empty op oneof",
			&BatchRequest{
				FeedbackIDs: []int64{1},
				Operation:   &attunev1.BatchOperation{},
			},
			ErrNoOperation,
		},
		{
			"no IDs and no filter",
			&BatchRequest{
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
				},
			},
			ErrNoSelection,
		},
		{
			"both IDs and filter",
			&BatchRequest{
				FeedbackIDs: []int64{1},
				Filter:      &attunev1.FeedbackFilter{},
				Operation: &attunev1.BatchOperation{
					Op: &attunev1.BatchOperation_Tag{Tag: &attunev1.BatchTagOp{}},
				},
			},
			ErrMixedSelection,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := svc.validateRequest(tc.req)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validateRequest() = %v, want nil", err)
				}
			} else {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("validateRequest() = %v, want %v", err, tc.wantErr)
				}
			}
		})
	}
}

func TestStatusToProto_AllStatuses(t *testing.T) {
	t.Parallel()

	svc := &service{}

	cases := []struct {
		input feedbackjob.Status
		want  attunev1.JobStatus
	}{
		{feedbackjob.StatusQueued, attunev1.JobStatus_JOB_STATUS_QUEUED},
		{feedbackjob.StatusRunning, attunev1.JobStatus_JOB_STATUS_RUNNING},
		{feedbackjob.StatusCompleted, attunev1.JobStatus_JOB_STATUS_COMPLETED},
		{feedbackjob.StatusFailed, attunev1.JobStatus_JOB_STATUS_FAILED},
		{feedbackjob.StatusCancelled, attunev1.JobStatus_JOB_STATUS_CANCELLED},
		{feedbackjob.Status("bogus"), attunev1.JobStatus_JOB_STATUS_UNSPECIFIED},
		{feedbackjob.Status(""), attunev1.JobStatus_JOB_STATUS_UNSPECIFIED},
	}

	for _, tc := range cases {
		t.Run(string(tc.input)+"_status", func(t *testing.T) {
			t.Parallel()
			got := svc.statusToProto(tc.input)
			if got != tc.want {
				t.Fatalf("statusToProto(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestJobToProto_RunningJobHasRetryAfter(t *testing.T) {
	t.Parallel()

	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:        "j-running",
		TenantID:  "t1",
		Status:    feedbackjob.StatusRunning,
		Total:     200,
		Progress:  75,
		CreatedAt: now,
		StartedAt: ptrext.Of(now),
	}
	resp := svc.jobToProto(job)

	if resp.RetryAfterSeconds != 2 {
		t.Fatalf("running job RetryAfterSeconds = %d, want 2", resp.RetryAfterSeconds)
	}
	if resp.StartedAt == nil {
		t.Fatal("StartedAt should be set for running job")
	}
	if resp.CompletedAt != nil {
		t.Fatal("CompletedAt should be nil for running job")
	}
}

func TestJobToProto_CancelledJobNoRetryAfter(t *testing.T) {
	t.Parallel()

	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:        "j-cancel",
		TenantID:  "t1",
		Status:    feedbackjob.StatusCancelled,
		Total:     50,
		Progress:  10,
		CreatedAt: now,
	}
	resp := svc.jobToProto(job)

	if resp.RetryAfterSeconds != 0 {
		t.Fatalf("cancelled job RetryAfterSeconds = %d, want 0", resp.RetryAfterSeconds)
	}
	if resp.Status != attunev1.JobStatus_JOB_STATUS_CANCELLED {
		t.Fatalf("Status = %v, want CANCELLED", resp.Status)
	}
}

func TestJobToProto_FailedJobWithError(t *testing.T) {
	t.Parallel()

	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:        "j-fail",
		TenantID:  "t1",
		Status:    feedbackjob.StatusFailed,
		Total:     100,
		Progress:  42,
		Error:     "database connection lost",
		CreatedAt: now,
	}
	resp := svc.jobToProto(job)

	if resp.Error == nil {
		t.Fatal("Error should be set for failed job")
	}
	if ptrext.Indirect(resp.Error) != "database connection lost" {
		t.Fatalf("Error = %q, want %q", ptrext.Indirect(resp.Error), "database connection lost")
	}
}

func TestJobToProto_CompletedWithBadResult(t *testing.T) {
	t.Parallel()

	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:        "j-bad-result",
		TenantID:  "t1",
		Status:    feedbackjob.StatusCompleted,
		Total:     10,
		Progress:  10,
		Result:    []byte("not valid json"),
		CreatedAt: now,
	}
	resp := svc.jobToProto(job)

	// Bad JSON in Result should not panic; Result should be nil.
	if resp.Result != nil {
		t.Fatal("Result should be nil for unparseable JSON")
	}
	if resp.Status != attunev1.JobStatus_JOB_STATUS_COMPLETED {
		t.Fatalf("Status = %v, want COMPLETED", resp.Status)
	}
}

func TestJobToProto_CompletedWithNilResult(t *testing.T) {
	t.Parallel()

	svc := &service{}
	now := time.Now()

	job := &feedbackjob.Job{
		ID:        "j-nil-result",
		TenantID:  "t1",
		Status:    feedbackjob.StatusCompleted,
		Total:     10,
		Progress:  10,
		Result:    nil,
		CreatedAt: now,
	}
	resp := svc.jobToProto(job)

	if resp.Result != nil {
		t.Fatal("Result should be nil when job.Result is nil")
	}
}
