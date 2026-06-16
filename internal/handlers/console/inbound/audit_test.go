package inbound

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
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

func TestTestConnectionRecordsAudit(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{
		testConn: stubProbe(nil),
		audit:    audit,
	})

	_, err := h.TestConnection(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host:     "imap.example.com",
			Port:     993,
			Tls:      true,
			Username: "ops@example.com",
			Password: "secret-pw",
		}),
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source.test_connection", audit.events[0].Action)
	require.Nil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}
