package notifytarget

import (
	"os"
	"testing"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// TestMain relaxes notify's outbound egress policy for this package's tests:
// the test-send handler tests post to httptest servers on loopback, which the
// default (production) policy blocks as an SSRF vector. Production wiring sets
// this from config at startup.
func TestMain(m *testing.M) {
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	os.Exit(m.Run())
}
