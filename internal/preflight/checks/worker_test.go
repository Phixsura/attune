// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
)

func TestEnricherWorkers_NilConfig(t *testing.T) {
	r := checkEnricherWorkers(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestEnricherWorkers_Zero(t *testing.T) {
	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: 0},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestEnricherWorkers_Negative(t *testing.T) {
	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: -1},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestEnricherWorkers_Positive(t *testing.T) {
	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: 3},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
	if r.Message == "" {
		t.Error("expected message with worker count")
	}
}
