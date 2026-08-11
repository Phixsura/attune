package outbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/webhooksub"
)

// subStubChannel stands in for the raw-webhook adapter (depguard bans
// importing real adapters from the framework, tests included). It mirrors
// the contract subscription sends rely on: POST to dst.URL, signature
// header derived from the per-subscription secret, CheckWebhook semantics.
type subStubChannel struct{}

func (s *subStubChannel) ID() string { return "raw-webhook" }

func (s *subStubChannel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, nil)
			if err != nil {
				return nil, err
			}
			if dst.Secret != "" {
				req.Header.Set("X-Attune-Signature", "stub-sig-"+dst.Secret[:4])
			}
			return req, nil
		},
		Check: outbound.CheckWebhook("sub-test"),
	}, nil
}

// registerRawWebhookStub registers the stub under the real raw-webhook id
// and unregisters on cleanup (same mechanism as registerTestChannel, which
// requires *testStubChannel).
func registerRawWebhookStub(t *testing.T) {
	t.Helper()
	outbound.Register(ptrext.Of(subStubChannel{}))
	t.Cleanup(func() { outbound.UnregisterForTest("raw-webhook") })
}

type fakeSubscriptionStore struct {
	sub *webhooksub.Subscription
	err error

	disabled []struct {
		id     uuid.UUID
		reason string
	}
	disableErr error
}

func (f *fakeSubscriptionStore) GetByIDAny(_ context.Context, _ uuid.UUID) (*webhooksub.Subscription, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sub, nil
}

func (f *fakeSubscriptionStore) Disable(_ context.Context, id uuid.UUID, reason string) error {
	f.disabled = append(f.disabled, struct {
		id     uuid.UUID
		reason string
	}{id, reason})
	return f.disableErr
}

func activeSubscription(url string) *webhooksub.Subscription {
	return ptrext.Of(webhooksub.Subscription{
		ID:         uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
		TenantID:   "t1",
		TargetURL:  url,
		Secret:     "0123456789abcdef",
		EventTypes: []string{"feedback.created"},
		Status:     webhooksub.StatusActive,
	})
}

func subscriptionOutboxRow(id int64, subID string) outboxrepo.OutboxRow {
	return outboxrepo.OutboxRow{
		ID:                id,
		TenantID:          "t1",
		DestinationType:   DestSubscriptionWebhook,
		DestinationTarget: subID,
		Audience:          "subscription",
		Payload: []byte(`{"version":"2","event_type":"feedback.created",` +
			`"delivered_at":"2026-07-29T00:00:00Z",` +
			`"feedback":{"id":7,"tenant_id":"t1","content":"c","source":"api",` +
			`"user_id":"u","submitted_at":"2026-07-29T00:00:00Z",` +
			`"enriched":{"title":"t","attrs":{},"is_urgent":false,"rationale":"r",` +
			`"enriched_at":"2026-07-29T00:00:00Z"}}}`),
		Status: outboxrepo.OutboxStatusPending,
	}
}

func testWorkerWithSubs(
	outboxStore *workerFakeOutboxStore,
	subs *fakeSubscriptionStore,
	transport *notify.Transport,
) *OutboxWorker {
	w := testWorker(outboxStore, ptrext.Of(workerFakeTargetStore{}), transport)
	w.subs = subs
	return w
}

func TestSubscriptionRowDeliversToSubscriptionURL(t *testing.T) {
	registerRawWebhookStub(t)
	var requests atomic.Int32
	var gotSig atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		gotSig.Store(r.Header.Get("X-Attune-Signature"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{sub: activeSubscription(srv.URL)})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(srv.Client(), notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(301, subs.sub.ID.String()))

	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, []int64{301}, outboxStore.deliveredIDs)
	require.NotEmpty(t, gotSig.Load(), "delivery must carry X-Attune-Signature")
	require.Empty(t, subs.disabled)
}

func TestSubscriptionRowDisabledSubscriptionDeadNoAttempt(t *testing.T) {
	registerRawWebhookStub(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(srv.Close)

	sub := activeSubscription(srv.URL)
	sub.Status = webhooksub.StatusDisabled
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{sub: sub})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(srv.Client(), notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(302, sub.ID.String()))

	require.Equal(t, int32(0), requests.Load(), "no HTTP attempt for a disabled subscription")
	require.Len(t, outboxStore.dead, 1)
	require.Contains(t, outboxStore.dead[0].reason, "subscription")
	require.Empty(t, outboxStore.failed)
}

func TestSubscriptionRowMissingSubscriptionDead(t *testing.T) {
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{err: webhooksub.ErrSubscriptionNotFound})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(http.DefaultClient, notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(303, uuid.NewString()))

	require.Len(t, outboxStore.dead, 1)
	require.Empty(t, outboxStore.failed)
}

func TestSubscriptionRow410DisablesSubscription(t *testing.T) {
	registerRawWebhookStub(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	t.Cleanup(srv.Close)

	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{sub: activeSubscription(srv.URL)})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(srv.Client(), notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(304, subs.sub.ID.String()))

	require.Len(t, subs.disabled, 1, "410 must auto-disable the subscription")
	require.Equal(t, subs.sub.ID, subs.disabled[0].id)
	require.Equal(t, webhooksub.ReasonGone, subs.disabled[0].reason)
	require.Len(t, outboxStore.dead, 1)
	require.Equal(t, "gone", outboxStore.dead[0].reason[:4])
	require.Empty(t, outboxStore.failed)
}

func TestSubscriptionRow500RetriesWithoutDisable(t *testing.T) {
	registerRawWebhookStub(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{sub: activeSubscription(srv.URL)})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(srv.Client(), notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(305, subs.sub.ID.String()))

	require.Empty(t, subs.disabled, "5xx must not disable the subscription")
	require.Len(t, outboxStore.failed, 1)
	require.Equal(t, 30*time.Second, outboxStore.failed[0].nextDelay)
	require.Empty(t, outboxStore.dead)
}

func TestSubscriptionRowNilStoreDead(t *testing.T) {
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	w := testWorker(outboxStore, ptrext.Of(workerFakeTargetStore{}), notify.NewTransport(http.DefaultClient, notify.NoRetry()))
	// SetSubscriptionStore never called — subscription rows must go dead,
	// not panic (deployments that don't wire the automation surface).
	w.SetSubscriptionStore(nil)

	w.processRow(context.Background(), subscriptionOutboxRow(306, uuid.NewString()))

	require.Len(t, outboxStore.dead, 1)
	require.Contains(t, outboxStore.dead[0].reason, "not configured")
}

func TestSubscriptionRowInvalidIDDead(t *testing.T) {
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(http.DefaultClient, notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(307, "not-a-uuid"))

	require.Len(t, outboxStore.dead, 1)
	require.Contains(t, outboxStore.dead[0].reason, "invalid subscription id")
}

func TestSubscriptionRowLookupErrorRetries(t *testing.T) {
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{err: errors.New("db down")})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(http.DefaultClient, notify.NoRetry()))

	w.processRow(context.Background(), subscriptionOutboxRow(308, uuid.NewString()))

	// transient lookup failure → retry path, not dead
	require.Len(t, outboxStore.failed, 1)
	require.Empty(t, outboxStore.dead)
}

func TestSubscriptionRowBadPayloadDead(t *testing.T) {
	registerRawWebhookStub(t)
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	subs := ptrext.Of(fakeSubscriptionStore{sub: activeSubscription("https://example.test/hook")})
	w := testWorkerWithSubs(outboxStore, subs, notify.NewTransport(http.DefaultClient, notify.NoRetry()))

	row := subscriptionOutboxRow(309, subs.sub.ID.String())
	row.Payload = []byte("{not-json")
	w.processRow(context.Background(), row)

	require.Len(t, outboxStore.dead, 1)
	require.Empty(t, outboxStore.failed)
}
