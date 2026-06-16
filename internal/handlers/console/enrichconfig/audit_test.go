package enrichconfig

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/service/enrich"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestUpdateRecordsAudit(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeConfigService{
		view: enrich.View{
			PromptTemplate: ptrext.Of("old {{content}}"),
			Dimensions:     domain.DimensionSet{{Name: "severity", Kind: domain.DimSingle}},
		},
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{svc: svc, audit: audit})

	_, err := h.Update(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.UpdateEnrichConfigRequest{
		PromptTemplate: ptrext.Of("new {{content}}"),
		Dimensions: []*attunev1.Dimension{{
			Name:        "severity",
			DisplayName: ptrext.Of(attunev1.I18NString{Entries: map[string]string{"default": "Severity"}}),
			Kind:        "single",
		}},
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "enrich_config.update", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}
