package digestsubscription

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/tenant"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

type fakeSubscriptionRepo struct {
	current *digestsubrepo.Subscription
	upsert  *digestsubrepo.Subscription
}

func (f *fakeSubscriptionRepo) GetByTenant(context.Context, string) (*digestsubrepo.Subscription, error) {
	if f.current == nil {
		return nil, digestsubrepo.ErrNotFound
	}
	out := ptrext.Indirect(f.current)
	return ptrext.Of(out), nil
}

func (f *fakeSubscriptionRepo) Upsert(_ context.Context, s digestsubrepo.Subscription) (*digestsubrepo.Subscription, error) {
	out := s
	out.CreatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	out.UpdatedAt = out.CreatedAt
	f.current = ptrext.Of(out)
	f.upsert = ptrext.Of(out)
	return ptrext.Of(out), nil
}

func (f *fakeSubscriptionRepo) DeleteByTenant(context.Context, string) error {
	if f.current == nil {
		return digestsubrepo.ErrNotFound
	}
	f.current = nil
	return nil
}

type fakeTenantReader struct {
	clustering bool
	timezone   string
}

func (f *fakeTenantReader) GetByID(context.Context, string) (*tenant.Tenant, error) {
	return ptrext.Of(tenant.Tenant{Timezone: f.timezone}), nil
}

func (f *fakeTenantReader) GetClusteringEnabled(context.Context, string) (bool, error) {
	return f.clustering, nil
}

func (f *fakeTenantReader) SetClusteringEnabled(_ context.Context, _ string, enabled bool) error {
	f.clustering = enabled
	return nil
}

func digestCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
		}),
	})
}

func TestUpsertRecordsAudit(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeSubscriptionRepo{current: ptrext.Of(digestsubrepo.Subscription{
		Enabled:        true,
		Frequency:      "daily",
		SendHour:       8,
		LLMMinFeedback: 6,
	})})
	tenants := ptrext.Of(fakeTenantReader{clustering: false, timezone: "Asia/Shanghai"})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{repo: repo, tenants: tenants, audit: audit})

	_, err := h.Upsert(digestCtx(), ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
		Enabled:           true,
		Frequency:         "weekly",
		SendHour:          9,
		Byweekday:         ptrext.Of(int32(1)),
		ClusteringEnabled: true,
	}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "digest_subscription.upsert", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestDeleteRecordsAudit(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeSubscriptionRepo{current: ptrext.Of(digestsubrepo.Subscription{
		Enabled:        true,
		Frequency:      "daily",
		SendHour:       8,
		LLMMinFeedback: 6,
	})})
	tenants := ptrext.Of(fakeTenantReader{clustering: true, timezone: "Asia/Shanghai"})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{repo: repo, tenants: tenants, audit: audit})

	_, err := h.Delete(digestCtx(), ptrext.Of(attunev1.DeleteDigestSubscriptionRequest{}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "digest_subscription.delete", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.Nil(t, audit.events[0].After)
}
