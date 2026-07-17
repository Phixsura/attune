// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures with inline struct pointers

package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
)

func TestCheckOIDCEnabled_NilConfig(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{Cfg: nil}
	r := checkOIDCEnabled(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
	if r.Message != "Config not loaded" {
		t.Errorf("Message = %q, want %q", r.Message, "Config not loaded")
	}
}

func TestCheckOIDCEnabled_Disabled(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: false},
		},
	}
	r := checkOIDCEnabled(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
	if r.Remediation == "" {
		t.Error("Remediation should not be empty")
	}
}

func TestCheckOIDCEnabled_Enabled(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true},
		},
	}
	r := checkOIDCEnabled(context.Background(), env)
	if r.Status != preflight.StatusPass {
		t.Errorf("Status = %v, want Pass", r.Status)
	}
}

func TestCheckSSOIssuerReachable_NilConfig(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{Cfg: nil}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckSSOIssuerReachable_OIDCDisabled(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: false},
		},
	}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusSkipped {
		t.Errorf("Status = %v, want Skipped", r.Status)
	}
}

func TestCheckSSOIssuerReachable_EmptyIssuer(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true, IssuerURL: ""},
		},
	}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckSSOIssuerReachable_InvalidIssuerURL(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true, IssuerURL: "://bad"},
		},
	}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
	if r.Message != "Invalid OIDC discovery URL" {
		t.Errorf("Message = %q, want invalid discovery URL", r.Message)
	}
}

func TestCheckSSOIssuerReachable_UnreachableIssuer(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true, IssuerURL: "http://127.0.0.1:1"},
		},
	}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
	if r.Message != "OIDC discovery unreachable" {
		t.Errorf("Message = %q, want unreachable discovery", r.Message)
	}
}

func TestCheckSSOIssuerReachable_Success(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true, IssuerURL: server.URL},
		},
	}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusPass {
		t.Errorf("Status = %v, want Pass, Message = %s", r.Status, r.Message)
	}
}

func TestCheckSSOIssuerReachable_Non200(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true, IssuerURL: server.URL},
		},
	}
	r := checkSSOIssuerReachable(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckRedirectURIMatch_NilConfig(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{Cfg: nil}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckRedirectURIMatch_OIDCDisabled(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: false},
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusSkipped {
		t.Errorf("Status = %v, want Skipped", r.Status)
	}
}

func TestCheckRedirectURIMatch_EmptyRedirectURI(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC: config.OIDCConfig{Enabled: true, RedirectURI: ""},
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckRedirectURIMatch_EmptyConsoleBaseURL(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC:           config.OIDCConfig{Enabled: true, RedirectURI: "https://example.com/callback"},
			ConsoleBaseURL: "",
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckRedirectURIMatch_InvalidRedirectURI(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC:           config.OIDCConfig{Enabled: true, RedirectURI: "://bad"},
			ConsoleBaseURL: "https://console.example.com",
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
	if r.Message == "" {
		t.Error("expected invalid redirect_uri message")
	}
}

func TestCheckRedirectURIMatch_InvalidConsoleBaseURL(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC:           config.OIDCConfig{Enabled: true, RedirectURI: "https://console.example.com/auth/callback"},
			ConsoleBaseURL: "://bad",
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
	if r.Message == "" {
		t.Error("expected invalid console.base_url message")
	}
}

func TestCheckRedirectURIMatch_HostMismatch(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC:           config.OIDCConfig{Enabled: true, RedirectURI: "https://auth.example.com/callback"},
			ConsoleBaseURL: "https://console.example.com",
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusFail {
		t.Errorf("Status = %v, want Fail", r.Status)
	}
}

func TestCheckRedirectURIMatch_Success(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC:           config.OIDCConfig{Enabled: true, RedirectURI: "https://console.example.com/auth/callback"},
			ConsoleBaseURL: "https://console.example.com",
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusPass {
		t.Errorf("Status = %v, want Pass", r.Status)
	}
}

func TestCheckRedirectURIMatch_CaseInsensitive(t *testing.T) {
	t.Parallel()
	env := &preflight.Environment{
		Cfg: &config.Config{
			OIDC:           config.OIDCConfig{Enabled: true, RedirectURI: "https://Console.Example.COM/auth/callback"},
			ConsoleBaseURL: "https://console.example.com",
		},
	}
	r := checkRedirectURIMatch(context.Background(), env)
	if r.Status != preflight.StatusPass {
		t.Errorf("Status = %v, want Pass (host comparison should be case-insensitive)", r.Status)
	}
}
