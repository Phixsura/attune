// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
)

func TestSessionKey_NilConfig(t *testing.T) {
	r := checkSessionKey(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestSessionKey_Empty(t *testing.T) {
	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestSessionKey_TooShort(t *testing.T) {
	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleSessionKey: "shortkey"},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestSessionKey_Valid(t *testing.T) {
	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing"},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
}

func TestOIDCReachable_NilConfig(t *testing.T) {
	r := checkOIDCReachable(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestOIDCReachable_Disabled(t *testing.T) {
	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{Enabled: false}},
	})
	if r.Status != preflight.StatusSkipped {
		t.Errorf("status = %q; want skipped", r.Status)
	}
}

func TestOIDCReachable_EmptyIssuer(t *testing.T) {
	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{Enabled: true}},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestOIDCReachable_ServerOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"issuer":"test"}`))
	}))
	defer srv.Close()

	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: srv.URL,
		}},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
}

func TestOIDCReachable_Server500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: srv.URL,
		}},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestOIDCReachable_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := checkOIDCReachable(ctx, &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: srv.URL,
		}},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail on timeout", r.Status)
	}
}

func TestOIDCReachable_UnreachableHost(t *testing.T) {
	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: "http://127.0.0.1:1",
		}},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}
