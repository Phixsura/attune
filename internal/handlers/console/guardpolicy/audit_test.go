package guardpolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestCreateRecordsAudit(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{svc: ptrext.Of(fakeService{}), audit: audit})

	_, err := h.Create(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.CreateGuardPolicyRequest{
		Policy: ptrext.Of(attunev1.GuardPolicy{
			Name:    "Block PII",
			Kind:    "default",
			Enabled: true,
		}),
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "guard_policy.create", audit.events[0].Action)
	require.Equal(t, "guard_policy", audit.events[0].TargetType)
	require.Nil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestDeleteRecordsAuditBeforeSnapshot(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{
		svc: ptrext.Of(fakeService{list: []llmguard.Policy{{
			ID:       "7b3685c9-0798-4f68-901b-46ea8cc15da2",
			TenantID: "tenant-1",
			Name:     "Block PII",
			Kind:     llmguard.KindDefault,
			Enabled:  true,
		}}}),
		audit: audit,
	})

	_, err := h.Delete(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.DeleteGuardPolicyRequest{Id: "7b3685c9-0798-4f68-901b-46ea8cc15da2"}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "guard_policy.delete", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.Nil(t, audit.events[0].After)
}
