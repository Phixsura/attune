// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ChiMux adapts a chi.Router to the inbound.Mux interface. cmd/attune
// passes a sub-router rooted at /v1/inbound; adapters call Method() with
// their channel-relative paths (e.g. "/webhook/{tenant-slug}/{source-slug}"),
// which chi translates into the standard chi method+pattern wiring.
//
// Keeping this thin wrapper at the framework boundary means the Adapter
// interface (inbound.Mux) stays router-agnostic — a future swap to
// gorilla/mux or stdlib http.ServeMux only touches this file.
type ChiMux struct {
	Router chi.Router
}

// Method satisfies inbound.Mux.
func (c *ChiMux) Method(method, pattern string, h http.Handler) {
	c.Router.Method(method, pattern, h)
}
