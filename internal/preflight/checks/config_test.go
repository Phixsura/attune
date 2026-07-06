// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
)

func TestConfigParse_NilConfig(t *testing.T) {
	r := checkConfigParse(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestConfigParse_Valid(t *testing.T) {
	r := checkConfigParse(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
}

func TestConfigBaseURL_Empty(t *testing.T) {
	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestConfigBaseURL_InvalidURL(t *testing.T) {
	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "://bad"},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestConfigBaseURL_Valid(t *testing.T) {
	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "https://feedback.example.com"},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
	if r.Message != "Configured (https)" {
		t.Errorf("message = %q; want 'Configured (https)'", r.Message)
	}
}

func TestConfigBaseURL_HTTPScheme(t *testing.T) {
	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "http://localhost:8080"},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
	if r.Message != "Configured (http)" {
		t.Errorf("message = %q; want 'Configured (http)'", r.Message)
	}
}

func TestConfigBaseURL_NilConfig(t *testing.T) {
	r := checkConfigBaseURL(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTLSConsistency_NilConfig(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTLSConsistency_NoBaseURL(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	if r.Status != preflight.StatusSkipped {
		t.Errorf("status = %q; want skipped", r.Status)
	}
}

func TestTLSConsistency_ProductionPrivateEgressWithoutBaseURLFails(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			Profile: config.ProfileProduction,
			Security: config.SecurityConfig{
				AllowPrivateEgress: true,
			},
		},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTLSConsistency_HTTPWarns(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "http://localhost:8080"},
	})
	if r.Status != preflight.StatusWarn {
		t.Errorf("status = %q; want warn", r.Status)
	}
	if r.Remediation == "" {
		t.Error("expected remediation for HTTP base_url")
	}
}

func TestTLSConsistency_HTTPSClean(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "https://feedback.example.com"},
	})
	if r.Status != preflight.StatusPass {
		t.Errorf("status = %q; want pass", r.Status)
	}
}

func TestTLSConsistency_LoopbackWithHTTPS(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			ConsoleBaseURL: "https://feedback.example.com",
			Security:       config.SecurityConfig{AllowLoopbackEgress: true},
		},
	})
	if r.Status != preflight.StatusWarn {
		t.Errorf("status = %q; want warn", r.Status)
	}
}

func TestTLSConsistency_PrivateEgressWithHTTPS(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			ConsoleBaseURL: "https://feedback.example.com",
			Security:       config.SecurityConfig{AllowPrivateEgress: true},
		},
	})
	if r.Status != preflight.StatusWarn {
		t.Errorf("status = %q; want warn", r.Status)
	}
	if r.Remediation == "" {
		t.Error("expected remediation for private egress with HTTPS")
	}
}

func TestTLSConsistency_ProductionHTTPFails(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			Profile:        config.ProfileProduction,
			ConsoleBaseURL: "http://feedback.example.com",
		},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
	if r.Remediation == "" {
		t.Error("expected remediation for production HTTP base_url")
	}
}

func TestTLSConsistency_ProductionLoopbackFlagFails(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			Profile:        config.ProfileProduction,
			ConsoleBaseURL: "https://feedback.example.com",
			Security:       config.SecurityConfig{AllowLoopbackEgress: true},
		},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
}

func TestTLSConsistency_ProductionPrivateEgressFlagFails(t *testing.T) {
	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			Profile:        config.ProfileProduction,
			ConsoleBaseURL: "https://feedback.example.com",
			Security:       config.SecurityConfig{AllowPrivateEgress: true},
		},
	})
	if r.Status != preflight.StatusFail {
		t.Errorf("status = %q; want fail", r.Status)
	}
	if r.Remediation == "" {
		t.Error("expected remediation for production private egress")
	}
}
