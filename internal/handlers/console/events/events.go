// SPDX-License-Identifier: Apache-2.0

// Package events exposes the SSE endpoint for real-time Console updates.
package events

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/sse"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Handler serves the /events/stream SSE endpoint.
type Handler struct {
	broker *sse.Broker
}

// NewHandler creates the events handler with the shared broker.
func NewHandler(broker *sse.Broker) *Handler {
	return ptrext.Of(Handler{broker: broker})
}

// Routes mounts the SSE endpoint under the console router.
func (h *Handler) Routes() func(chi.Router) {
	return func(r chi.Router) {
		r.Get("/stream", h.stream)
	}
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())
	h.broker.ServeSSE(r.Context(), w, auth.TenantID)
}
