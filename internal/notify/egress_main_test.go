package notify

import (
	"os"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// TestMain relaxes the outbound egress policy for this package's tests, which
// post to httptest servers on loopback that the default (production) policy
// blocks as an SSRF vector. Production wiring sets this from config at startup.
func TestMain(m *testing.M) {
	SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	os.Exit(m.Run())
}
