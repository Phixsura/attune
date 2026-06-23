// Package system implements Console system administration endpoints.
package system

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
)

// PreflightHandler serves the production readiness preflight report.
type PreflightHandler struct {
	env *preflight.Environment
}

// NewPreflightHandler creates a handler with the given check environment.
func NewPreflightHandler(env *preflight.Environment) *PreflightHandler {
	return ptrext.Of(PreflightHandler{env: env})
}

// ServeHTTP runs all preflight checks and returns an application/health+json response.
func (h *PreflightHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	report := preflight.RunAll(ctx, h.env)

	w.Header().Set("Content-Type", "application/health+json")
	if report.Status == preflight.StatusFail {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}
