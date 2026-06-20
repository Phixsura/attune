// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"encoding/json"
	"net/http"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// DiscoveryResponse is the OAuth protected resource metadata.
type DiscoveryResponse struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// DiscoveryHandler serves /.well-known/oauth-protected-resource.
type DiscoveryHandler struct {
	baseURL string
}

// NewDiscoveryHandler creates a new DiscoveryHandler.
func NewDiscoveryHandler(baseURL string) *DiscoveryHandler {
	return ptrext.Of(DiscoveryHandler{baseURL: baseURL})
}

// ServeHTTP handles the discovery request.
func (h *DiscoveryHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	resp := DiscoveryResponse{
		Resource:               h.baseURL + "/mcp/v1",
		AuthorizationServers:   []string{h.baseURL + "/mcp/oauth"},
		ScopesSupported:        []string{"mcp:read", "mcp:write", "mcp:ingest"},
		BearerMethodsSupported: []string{"header"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
