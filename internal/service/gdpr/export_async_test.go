package gdpr

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
)

type deleteStoreStub struct {
	request     *gdprrepo.Request
	completedID string
	failedID    string
}

func (s *deleteStoreStub) ClaimNextDeleteRequest(context.Context, time.Time) (*gdprrepo.Request, error) {
	return s.request, nil
}

func (s *deleteStoreStub) CompleteDeleteRequest(_ context.Context, requestID string, _ gdprrepo.Counts) error {
	s.completedID = requestID
	return nil
}

func (s *deleteStoreStub) FailDeleteRequest(_ context.Context, requestID, _ string) error {
	s.failedID = requestID
	return nil
}

type deleteExecutorStub struct {
	result *gdprrepo.DeleteResult
	err    error
}

func (s *deleteExecutorStub) ExecuteDeleteRequest(context.Context, string) (*gdprrepo.DeleteResult, error) {
	return s.result, s.err
}

func TestWorkerProcessNextDeleteCompletesAndAudits(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{
			ID:          "req-123",
			TenantID:    "tenant-1",
			CreatedBy:   "admin-1",
			SubjectHash: "hash-123",
		}),
	})
	audit := ptrext.Of(stubAudit{})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{result: ptrext.Of(gdprrepo.DeleteResult{Counts: gdprrepo.Counts{FeedbackCount: 2}})}),
		audit:          audit,
	})

	if err := worker.processNextDelete(context.Background()); err != nil {
		t.Fatalf("processNextDelete err = %v", err)
	}
	if store.completedID != "req-123" {
		t.Fatalf("completed request id = %q", store.completedID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "gdpr.delete" {
		t.Fatalf("expected gdpr.delete audit event, got %#v", audit.events)
	}
}

func TestWorkerProcessNextDeleteFailsRequest(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(deleteStoreStub{
		request: ptrext.Of(gdprrepo.Request{ID: "req-123"}),
	})
	worker := ptrext.Of(Worker{
		deleteStore:    store,
		deleteExecutor: ptrext.Of(deleteExecutorStub{err: context.DeadlineExceeded}),
		audit:          ptrext.Of(stubAudit{}),
	})

	if err := worker.processNextDelete(context.Background()); err == nil {
		t.Fatal("expected processNextDelete to return error")
	}
	if store.failedID != "req-123" {
		t.Fatalf("failed request id = %q", store.failedID)
	}
}
