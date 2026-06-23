package checks

import (
	"context"
	"fmt"
	"net/url"

	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "config:parse",
		Category: preflight.CategoryConfig,
		Run:      checkConfigParse,
	})
	preflight.Register(preflight.Check{
		Name:     "config:base_url",
		Category: preflight.CategoryConfig,
		Run:      checkConfigBaseURL,
	})
	preflight.Register(preflight.Check{
		Name:     "config:tls_consistency",
		Category: preflight.CategoryConfig,
		Run:      checkTLSConsistency,
	})
}

func checkConfigParse(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "config:parse",
		Category: preflight.CategoryConfig,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		r.Remediation = "Ensure --config points to a valid YAML file."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = "Config loaded successfully"
	return r
}

func checkConfigBaseURL(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "config:base_url",
		Category: preflight.CategoryConfig,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	base := env.Cfg.ConsoleBaseURL
	if base == "" {
		r.Status = preflight.StatusFail
		r.Message = "console.base_url is empty"
		r.Remediation = "Set console.base_url to the public URL operators use to reach the Console (e.g. https://feedback.example.com)."
		return r
	}
	parsed, err := url.ParseRequestURI(base)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "console.base_url is not a valid URL"
		r.Remediation = "Set console.base_url to a valid URL (e.g. https://feedback.example.com)."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("Configured (%s)", parsed.Scheme)
	return r
}

func checkTLSConsistency(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "config:tls_consistency",
		Category: preflight.CategoryConfig,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	base := env.Cfg.ConsoleBaseURL
	if base == "" {
		r.Status = preflight.StatusSkipped
		r.Message = "No base_url configured"
		return r
	}
	parsed, err := url.Parse(base)
	if err != nil {
		r.Status = preflight.StatusSkipped
		r.Message = "Cannot parse base_url"
		return r
	}
	if parsed.Scheme != "https" {
		r.Status = preflight.StatusWarn
		r.Message = "Non-HTTPS base_url — session cookies transmit in cleartext"
		r.Remediation = "Set console.base_url to an https:// URL. HTTP is acceptable only for local development."
		return r
	}
	if env.Cfg.Security.AllowLoopbackEgress {
		r.Status = preflight.StatusWarn
		r.Message = "allow_loopback_egress is on with HTTPS base_url"
		r.Remediation = "Disable security.allow_loopback_egress in production. It permits SSRF to loopback addresses."
		return r
	}
	if env.Cfg.Security.AllowPrivateEgress {
		r.Status = preflight.StatusWarn
		r.Message = "allow_private_egress is on with HTTPS base_url"
		r.Remediation = "Disable security.allow_private_egress in production. It permits SSRF to private network addresses."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = "No insecure flags with HTTPS"
	return r
}
