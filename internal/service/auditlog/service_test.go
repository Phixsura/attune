package auditlog

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
)

type stubRepo struct {
	insertCalled bool
}

func (s *stubRepo) Insert(_ context.Context, _ auditlogrepo.Entry) error {
	s.insertCalled = true
	return nil
}

func (s *stubRepo) List(context.Context, auditlogrepo.ListFilter) (auditlogrepo.ListResult, error) {
	return auditlogrepo.ListResult{}, nil
}

func (s *stubRepo) PruneBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestRecordRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := New(repo)

	err := svc.Record(context.Background(), Event{
		TenantID:   "tenant-1",
		Actor:      Actor{Type: "admin", ID: "user-1"},
		Action:     "unknown.action",
		TargetType: "member",
		TargetID:   "member-1",
	})
	if err == nil {
		t.Fatal("Record error = nil, want unsupported action error")
	}
	if repo.insertCalled {
		t.Fatal("Record called repo.Insert for unsupported action")
	}
}
