package notifytarget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestTestNotifyTargetRecordsAudit(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(targetServer.Close)

	id := uuid.New()
	repo := ptrext.Of(fakeNotifyRepo{
		getRow: ptrext.Of(notifytarget.NotifyTarget{
			ID:              id,
			TenantID:        "tenant-1",
			DestinationType: notifytarget.DestRawWebhook,
			Audience:        notifytarget.AudienceAll,
			URL:             targetServer.URL,
			TimeoutSeconds:  1,
			CreatedAt:       time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		}),
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(NotifyTargetsHandler{repo: repo, audit: audit})

	_, err := h.Test(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.TestNotifyTargetRequest{Id: id.String()}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "notify_target.test", audit.events[0].Action)
	require.Nil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}
