// SPDX-License-Identifier: Apache-2.0

package llmclient

import (
	"os"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// TestMain relaxes the outbound egress policy for the package's tests: they spin
// up httptest servers on loopback, which the default (production) policy blocks
// as an SSRF vector. Production wiring sets this from config at startup.
func TestMain(m *testing.M) {
	SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	os.Exit(m.Run())
}
