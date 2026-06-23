// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/preflight"
)

func TestTinkKeyset_NilConfig(t *testing.T) {
	r := checkTinkKeyset(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTinkKeyset_Empty(t *testing.T) {
	r := checkTinkKeyset(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTinkKeyset_InvalidJSON(t *testing.T) {
	r := checkTinkKeyset(context.Background(), &preflight.Environment{
		Cfg: &config.Config{Secrets: config.SecretsConfig{TinkKeyset: "not-json"}},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTinkKeyset_ValidRoundTrip(t *testing.T) {
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatalf("generate keyset: %v", err)
	}
	r := checkTinkKeyset(context.Background(), &preflight.Environment{
		Cfg: &config.Config{Secrets: config.SecretsConfig{TinkKeyset: raw}},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass; message = %q", r.Status, r.Message)
	}
}
