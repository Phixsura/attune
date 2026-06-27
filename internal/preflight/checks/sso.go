// SPDX-License-Identifier: Apache-2.0

package checks

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "sso:oidc_enabled",
		Category: preflight.CategoryAuth,
		Run:      checkOIDCEnabled,
	})
	preflight.Register(preflight.Check{
		Name:     "sso:issuer_reachable",
		Category: preflight.CategoryAuth,
		Run:      checkSSOIssuerReachable,
	})
	preflight.Register(preflight.Check{
		Name:     "sso:redirect_uri_match",
		Category: preflight.CategoryAuth,
		Run:      checkRedirectURIMatch,
	})
}

func checkOIDCEnabled(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "sso:oidc_enabled",
		Category: preflight.CategoryAuth,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	if !env.Cfg.OIDC.Enabled {
		r.Status = preflight.StatusFail
		r.Message = "OIDC is not enabled"
		r.Remediation = "Set oidc.enabled: true in config.yaml and configure OIDC settings."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = "OIDC is enabled"
	return r
}

var ssoClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)} // ptrext:allow otelhttp wrapping

func checkSSOIssuerReachable(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "sso:issuer_reachable",
		Category: preflight.CategoryAuth,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	if !env.Cfg.OIDC.Enabled {
		r.Status = preflight.StatusSkipped
		r.Message = "OIDC not enabled"
		return r
	}
	issuer := env.Cfg.OIDC.IssuerURL
	if issuer == "" {
		r.Status = preflight.StatusFail
		r.Message = "OIDC issuer_url is empty"
		r.Remediation = "Set oidc.issuer_url to your identity provider's issuer URL."
		return r
	}

	wellKnown := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, wellKnown, nil)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Invalid OIDC discovery URL"
		r.Remediation = "Check oidc.issuer_url format."
		return r
	}
	resp, err := ssoClient.Do(req)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "OIDC discovery unreachable"
		r.Remediation = "Ensure the OIDC issuer is reachable from this host. Check network/firewall settings."
		return r
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("OIDC discovery returned HTTP %d", resp.StatusCode)
		r.Remediation = "Check oidc.issuer_url — the well-known endpoint should return 200."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = "OIDC issuer reachable"
	return r
}

func checkRedirectURIMatch(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "sso:redirect_uri_match",
		Category: preflight.CategoryAuth,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	if !env.Cfg.OIDC.Enabled {
		r.Status = preflight.StatusSkipped
		r.Message = "OIDC not enabled"
		return r
	}

	redirectURI := env.Cfg.OIDC.RedirectURI
	consoleBaseURL := env.Cfg.ConsoleBaseURL

	if redirectURI == "" {
		r.Status = preflight.StatusFail
		r.Message = "OIDC redirect_uri is empty"
		r.Remediation = "Set oidc.redirect_uri to the callback URL."
		return r
	}
	if consoleBaseURL == "" {
		r.Status = preflight.StatusFail
		r.Message = "Console base_url is empty"
		r.Remediation = "Set console.base_url to the console origin."
		return r
	}

	redirectParsed, err := url.Parse(redirectURI)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("Invalid redirect_uri: %v", err)
		return r
	}

	consoleParsed, err := url.Parse(consoleBaseURL)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("Invalid console.base_url: %v", err)
		return r
	}

	if !strings.EqualFold(redirectParsed.Host, consoleParsed.Host) {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("redirect_uri host (%s) does not match console.base_url host (%s)",
			redirectParsed.Host, consoleParsed.Host)
		r.Remediation = "Ensure oidc.redirect_uri and console.base_url use the same hostname."
		return r
	}

	r.Status = preflight.StatusPass
	r.Message = "redirect_uri matches console.base_url"
	return r
}
