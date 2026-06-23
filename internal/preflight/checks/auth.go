package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "auth:session_key",
		Category: preflight.CategoryAuth,
		Run:      checkSessionKey,
	})
	preflight.Register(preflight.Check{
		Name:     "auth:oidc_reachable",
		Category: preflight.CategoryAuth,
		Run:      checkOIDCReachable,
	})
}

func checkSessionKey(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "auth:session_key",
		Category: preflight.CategoryAuth,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	key := env.Cfg.ConsoleSessionKey
	if key == "" {
		r.Status = preflight.StatusFail
		r.Message = "Console session key not configured"
		r.Remediation = "Set console.session_key to a random string of at least 32 bytes."
		return r
	}
	if len(key) < 32 {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("Console session key too short (%d bytes, need >= 32)", len(key))
		r.Remediation = "Set console.session_key to a random string of at least 32 bytes."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("Session key configured (%d bytes)", len(key))
	return r
}

var oidcClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)} // ptrext:allow otelhttp wrapping

func checkOIDCReachable(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "auth:oidc_reachable",
		Category: preflight.CategoryAuth,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	if !env.Cfg.OIDC.Enabled {
		r.Status = preflight.StatusSkipped
		r.Message = "OIDC not configured"
		return r
	}
	issuer := env.Cfg.OIDC.IssuerURL
	if issuer == "" {
		r.Status = preflight.StatusFail
		r.Message = "OIDC enabled but issuer_url is empty"
		r.Remediation = "Set oidc.issuer_url to your identity provider's issuer URL."
		return r
	}

	wellKnown := issuer + "/.well-known/openid-configuration"
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, wellKnown, nil)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Invalid OIDC discovery URL"
		r.Remediation = "Check oidc.issuer_url format."
		return r
	}
	resp, err := oidcClient.Do(req)
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
