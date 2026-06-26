// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test stub fixtures use address-of for interface satisfaction

package auditevidence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestWorkerProcessNext_NoJobAvailable(t *testing.T) {
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return nil, nil
		},
	}
	w := NewWorker(store, nil, nil)
	if err := w.ProcessNextForTest(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkerProcessNext_ClaimError(t *testing.T) {
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return nil, errors.New("claim failed")
		},
	}
	w := NewWorker(store, nil, nil)
	err := w.ProcessNextForTest(context.Background())
	if err == nil || err.Error() != "claim failed" {
		t.Fatalf("expected claim error, got %v", err)
	}
}

func TestWorkerProcessNext_FetchFails_MarksJobFailed(t *testing.T) {
	var failedErr string
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:       "job-1",
				TenantID: "t1",
				Status:   aerepo.JobRunning,
			}), nil
		},
		failJobFn: func(_ context.Context, _, _, errMsg string) (int64, error) {
			failedErr = errMsg
			return 1, nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{}, errors.New("list error")
		},
	}
	w := NewWorker(store, lister, nil)
	err := w.ProcessNextForTest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if failedErr != "list error" {
		t.Errorf("fail reason = %q, want 'list error'", failedErr)
	}
}

func TestWorkerProcessNext_CompletesSuccessfully(t *testing.T) {
	var completedID string
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:         "job-1",
				TenantID:   "t1",
				CreatedBy:  "admin-1",
				FilterJSON: json.RawMessage("{}"),
				Status:     aerepo.JobRunning,
			}), nil
		},
		completeJobFn: func(_ context.Context, jobID, _, _ string, _ []byte, _ int, _ time.Time) (int64, error) {
			completedID = jobID
			return 1, nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{
				Items: []auditlogrepo.Entry{
					{ID: 1, TenantID: "t1", Action: "api_key.create", Summary: "test"},
				},
			}, nil
		},
	}
	w := NewWorker(store, lister, nil)
	if err := w.ProcessNextForTest(context.Background()); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if completedID != "job-1" {
		t.Errorf("completed job = %s, want job-1", completedID)
	}
}

func TestWorkerProcessNext_ReClaimedJobIsNoop(t *testing.T) {
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:         "job-1",
				TenantID:   "t1",
				CreatedBy:  "admin-1",
				FilterJSON: json.RawMessage("{}"),
				Status:     aerepo.JobRunning,
			}), nil
		},
		completeJobFn: func(context.Context, string, string, string, []byte, int, time.Time) (int64, error) {
			return 0, nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{
				Items: []auditlogrepo.Entry{{ID: 1, TenantID: "t1", Action: "test", Summary: "s"}},
			}, nil
		},
	}
	w := NewWorker(store, lister, nil)
	if err := w.ProcessNextForTest(context.Background()); err != nil {
		t.Fatalf("expected nil for re-claimed job, got %v", err)
	}
}

func TestWorkerOptions(t *testing.T) {
	key := make([]byte, 32)
	w := NewWorker(nil, nil, nil,
		WithWorkerExportTTL(48*time.Hour),
		WithWorkerSigningKey(key),
	)
	if w.exportTTL != 48*time.Hour {
		t.Errorf("exportTTL = %v, want 48h", w.exportTTL)
	}
	if len(w.signKey) != 32 {
		t.Errorf("signKey len = %d, want 32", len(w.signKey))
	}
}

func TestWorkerAuditExpiry(t *testing.T) {
	recorder := &stubAuditRecorder{}
	w := NewWorker(nil, nil, recorder)
	w.auditExpiry(context.Background(), 3)
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].Action != "audit_evidence.expire" {
		t.Errorf("action = %s", recorder.events[0].Action)
	}
}

func TestWorkerAuditExpiry_NilRecorder(t *testing.T) {
	w := NewWorker(nil, nil, nil)
	w.auditExpiry(context.Background(), 5)
}
