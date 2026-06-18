package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestCreateStateRecordsAudit(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{states: ptrext.Of(fakeStateStore{}), service: ptrext.Of(fakeWorkflowService{}), audit: audit})

	_, err := h.CreateState(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.CreateStateRequest{
		Name:        "open",
		DisplayName: ptrext.Of(attunev1.I18NString{Entries: map[string]string{"default": "Open"}}),
		Category:    "open",
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "workflow_state.create", audit.events[0].Action)
	require.Nil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestArchiveStateRecordsAudit(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{
		states: ptrext.Of(fakeStateStore{states: []workflowstate.WorkflowState{{
			ID:        "s-1",
			TenantID:  "tenant-1",
			Name:      "Open",
			Category:  "open",
			CreatedAt: now,
			UpdatedAt: now,
		}}}),
		service: ptrext.Of(fakeWorkflowService{}),
		audit:   audit,
	})

	_, err := h.ArchiveState(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.ArchiveStateRequest{Id: "s-1"}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "workflow_state.archive", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestUpdateStateRecordsDistinctBeforeAndAfter(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{
		states: ptrext.Of(fakeStateStore{states: []workflowstate.WorkflowState{{
			ID:          "s-1",
			TenantID:    "tenant-1",
			Name:        "open",
			DisplayName: domain.I18nString{"default": "Open"},
			Color:       "#3b82f6",
			Category:    "open",
			CreatedAt:   now,
			UpdatedAt:   now,
		}}}),
		service: ptrext.Of(fakeWorkflowService{}),
		audit:   audit,
	})

	_, err := h.UpdateState(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.UpdateStateRequest{
		Id:          "s-1",
		DisplayName: ptrext.Of(attunev1.I18NString{Entries: map[string]string{"default": "Renamed"}}),
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	before := audit.events[0].Before.(map[string]any)
	after := audit.events[0].After.(map[string]any)
	require.Equal(t, "open", before["name"]) // stable key, unchanged by rename
	require.Equal(t, "open", after["name"])
	require.Equal(t, "Open", before["display_name"].(domain.I18nString)["default"])
	require.Equal(t, "Renamed", after["display_name"].(domain.I18nString)["default"])
}

func TestSeedDefaultsRecordsAudit(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{
		states: ptrext.Of(fakeStateStore{
			states: []workflowstate.WorkflowState{{ID: "s-1", TenantID: "tenant-1", Name: "Open", Category: "open", CreatedAt: now, UpdatedAt: now}},
			transitions: []workflowstate.Transition{{
				ID:          "t-1",
				TenantID:    "tenant-1",
				FromStateID: "s-1",
				ToStateID:   "s-2",
				CreatedAt:   now,
			}},
		}),
		service: ptrext.Of(fakeWorkflowService{}),
		audit:   audit,
	})

	_, err := h.SeedDefaults(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.SeedDefaultsRequest{}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "workflow_seed_defaults.run", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}
