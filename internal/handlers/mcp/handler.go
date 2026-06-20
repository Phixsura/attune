// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Handler holds the MCP server components.
type Handler struct {
	signer     *oauth.JWTSigner
	discovery  *oauth.DiscoveryHandler
	dispatcher *jsonrpc.Dispatcher
	auth       *server.AuthMiddleware
}

// Config holds MCP handler configuration.
type Config struct {
	BaseURL   string
	JWTSecret []byte
	JWTIssuer string
}

// NewHandler creates a new MCP handler.
func NewHandler(cfg Config, deps *tools.Deps) *Handler {
	signer := oauth.NewJWTSigner(cfg.JWTSecret, cfg.JWTIssuer)
	discovery := oauth.NewDiscoveryHandler(cfg.BaseURL)
	auth := server.NewAuthMiddleware(signer)

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)
	tools.RegisterWriteTools(d, deps)
	tools.RegisterIngestTools(d, deps)

	return ptrext.Of(Handler{
		signer:     signer,
		discovery:  discovery,
		dispatcher: d,
		auth:       auth,
	})
}

// Routes returns the chi router for MCP endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/.well-known/oauth-protected-resource", h.discovery.ServeHTTP)

	r.Group(func(r chi.Router) {
		r.Use(h.auth.Wrap)
		r.Post("/v1", jsonrpc.NewHandler(h.dispatcher).ServeHTTP)
	})

	return r
}

// Signer returns the JWT signer for issuing tokens.
func (h *Handler) Signer() *oauth.JWTSigner {
	return h.signer
}
