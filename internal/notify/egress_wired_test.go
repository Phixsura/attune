package notify

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// TestSend_DefaultPolicyBlocksLoopbackEgress proves the SSRF guard also covers
// the console "test webhook" path (tenant-pasted URL), not just delivery.
func TestSend_DefaultPolicyBlocksLoopbackEgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	saved := egressPolicy
	egressPolicy = nethardening.Policy{} // production default
	defer func() { egressPolicy = saved }()

	res := TestSend(context.Background(), notifytarget.NotifyTarget{
		DestinationType: notifytarget.DestRawWebhook,
		URL:             srv.URL,
		TimeoutSeconds:  5,
	})
	if res.OK {
		t.Fatal("test-send to loopback should be blocked under the default egress policy")
	}
}

// TestNewTransport_DefaultPolicyBlocksLoopbackEgress proves the SSRF guard is
// actually wired into NewTransport's default client — not just that the
// nethardening primitives work in isolation. The package TestMain relaxes the
// egress policy so the other tests can hit loopback httptest servers; here we
// temporarily restore the locked-down default and assert a loopback delivery is
// refused at dial time.
func TestNewTransport_DefaultPolicyBlocksLoopbackEgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	saved := egressPolicy
	egressPolicy = nethardening.Policy{} // production default: blocks loopback + private
	defer func() { egressPolicy = saved }()

	tr := NewTransport(nil, RetryPolicy{MaxAttempts: 1})
	err := tr.Send(
		context.Background(), "test",
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
		},
		func(context.Context, int, []byte) error { return nil },
	)
	if err == nil {
		t.Fatal("expected loopback egress to be blocked under the default policy")
	}
	var blocked *nethardening.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected a nethardening.BlockedError, got: %v", err)
	}
}
