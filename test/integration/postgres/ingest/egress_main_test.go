//go:build integration

package ingest_test

import (
	"os"
	"testing"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// TestMain relaxes notify's outbound egress policy: this suite drains the outbox
// to an httptest receiver on loopback, which the production default policy blocks
// as an SSRF vector (#84). Production wiring sets the policy from config at startup.
func TestMain(m *testing.M) {
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	os.Exit(m.Run())
}
