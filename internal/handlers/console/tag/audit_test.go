package tag

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

type fakeTagRepo struct {
	current feedbacktag.Tag
}

func (f *fakeTagRepo) List(context.Context, string, bool) ([]feedbacktag.Tag, error) {
	return nil, nil
}

func (f *fakeTagRepo) Create(_ context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error) {
	f.current = t
	f.current.ID = uuid.New()
	f.current.CreatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	f.current.UpdatedAt = f.current.CreatedAt
	return ptrext.Of(f.current), nil
}

func (f *fakeTagRepo) Update(_ context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error) {
	f.current.Name = t.Name
	f.current.Color = t.Color
	f.current.Description = t.Description
	f.current.ExclusiveScope = t.ExclusiveScope
	f.current.UpdatedAt = time.Date(2026, 6, 16, 13, 0, 0, 0, time.UTC)
	return ptrext.Of(f.current), nil
}

func (f *fakeTagRepo) Archive(_ context.Context, _ string, _ uuid.UUID) error {
	now := time.Date(2026, 6, 16, 14, 0, 0, 0, time.UTC)
	f.current.ArchivedAt = ptrext.Of(now)
	return nil
}

func (f *fakeTagRepo) GetByID(context.Context, string, uuid.UUID) (*feedbacktag.Tag, error) {
	out := f.current
	return ptrext.Of(out), nil
}

func tagCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})
}

func TestCreateRecordsAudit(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{repo: ptrext.Of(fakeTagRepo{}), audit: audit})

	_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{Name: "Urgent"}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "tag.create", audit.events[0].Action)
	require.Nil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestUpdateRecordsAudit(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeTagRepo{current: feedbacktag.Tag{
		ID:       uuid.New(),
		TenantID: "tenant-1",
		Name:     "Urgent",
		Color:    "#ef4444",
	}})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{repo: repo, audit: audit})

	_, err := h.Update(tagCtx(), ptrext.Of(attunev1.UpdateTagRequest{
		Id:    repo.current.ID.String(),
		Name:  ptrext.Of("Escalated"),
		Color: ptrext.Of("#22c55e"),
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "tag.update", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestArchiveRecordsAudit(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeTagRepo{current: feedbacktag.Tag{
		ID:       uuid.New(),
		TenantID: "tenant-1",
		Name:     "Urgent",
		Color:    "#ef4444",
	}})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{repo: repo, audit: audit})

	_, err := h.Archive(tagCtx(), ptrext.Of(attunev1.ArchiveTagRequest{Id: repo.current.ID.String()}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "tag.archive", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}
