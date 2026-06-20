package notifytarget

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// TestMain relaxes notify's outbound egress policy for this package's tests:
// the test-send handler tests post to httptest servers on loopback, which the
// default (production) policy blocks as an SSRF vector. Production wiring sets
// this from config at startup.
//
// It also registers a minimal "raw-webhook" adapter so that TestSend
// (called by the handler's /test endpoint) can look up the channel via
// outbound.LookupEvent without blank-importing adapter packages — depguard
// forbids that from handlers/.
func TestMain(m *testing.M) {
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	outbound.ResetForTest()
	outbound.Register(&stubEventChannel{id: "raw-webhook"}) // ptrext:allow interface-registration
	outbound.Register(&stubEventChannel{id: "slack"})       // ptrext:allow interface-registration
	os.Exit(m.Run())
}

type stubEventChannel struct{ id string }

func (s *stubEventChannel) ID() string { return s.id }

func (s *stubEventChannel) RenderEvent(_ *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodPost, dst.URL,
				bytes.NewReader([]byte(`{"event_type":"test"}`)))
		},
		Check: func(_ context.Context, status int, _ []byte) error { return nil },
	}, nil
}
