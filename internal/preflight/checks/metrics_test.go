// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/preflight"
)

func TestMetricsRegistry_Runs(t *testing.T) {
	r := checkMetricsRegistry(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusPass && r.Status != preflight.StatusWarn {
		t.Errorf("status = %q; want pass or warn", r.Status)
	}
}
