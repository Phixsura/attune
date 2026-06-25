// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	protectedResourceWellKnownSuffix   = "/.well-known/oauth-protected-resource"
	authorizationServerWellKnownSuffix = "/.well-known/oauth-authorization-server"
	openIDConfigurationWellKnownSuffix = "/.well-known/openid-configuration"
)

// DiscoveryResponse is the OAuth protected resource metadata.
type DiscoveryResponse struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// AuthorizationServerMetadataResponse is OAuth 2.0 Authorization Server metadata.
type AuthorizationServerMetadataResponse struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ResourceParameterSupported        bool     `json:"resource_parameter_supported"`
	AuthorizationResponseIssSupported bool     `json:"authorization_response_iss_parameter_supported"`
	ProtectedResources                []string `json:"protected_resources,omitempty"`
}

// DiscoveryHandler serves MCP OAuth/OAuth-protected-resource metadata.
type DiscoveryHandler struct {
	resourceURL             string
	authorizationServerURL  string
	authorizationEndpoint   string
	tokenEndpoint           string
	protectedResourceMeta   string
	authorizationServerMeta string
	openIDConfigurationMeta string
}

// NewDiscoveryHandler creates a new DiscoveryHandler.
func NewDiscoveryHandler(publicBaseURL, authorizationServerURL string) *DiscoveryHandler {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	authServer := strings.TrimRight(strings.TrimSpace(authorizationServerURL), "/")
	resourceURL := base + "/mcp/v1"
	authEndpointBase := base + "/mcp/oauth"

	protectedMeta, err := WellKnownURL(resourceURL, protectedResourceWellKnownSuffix)
	if err != nil {
		panic("invalid MCP resource URL: " + err.Error())
	}
	authMeta, err := WellKnownURL(authServer, authorizationServerWellKnownSuffix)
	if err != nil {
		panic("invalid MCP authorization server URL: " + err.Error())
	}
	openIDMeta, err := WellKnownURL(authServer, openIDConfigurationWellKnownSuffix)
	if err != nil {
		panic("invalid MCP authorization server URL: " + err.Error())
	}

	return ptrext.Of(DiscoveryHandler{
		resourceURL:             resourceURL,
		authorizationServerURL:  authServer,
		authorizationEndpoint:   authEndpointBase + "/authorize",
		tokenEndpoint:           authEndpointBase + "/token",
		protectedResourceMeta:   protectedMeta,
		authorizationServerMeta: authMeta,
		openIDConfigurationMeta: openIDMeta,
	})
}

// ProtectedResourceMetadataPath returns the RFC 9728 metadata path for the MCP resource.
func (h *DiscoveryHandler) ProtectedResourceMetadataPath() string {
	return mustWellKnownPath(h.resourceURL, protectedResourceWellKnownSuffix)
}

// AuthorizationServerMetadataPath returns the RFC 8414 metadata path for the auth server.
func (h *DiscoveryHandler) AuthorizationServerMetadataPath() string {
	return mustWellKnownPath(h.authorizationServerURL, authorizationServerWellKnownSuffix)
}

// OpenIDConfigurationPath returns the RFC 8414-compatible OpenID configuration path.
func (h *DiscoveryHandler) OpenIDConfigurationPath() string {
	return mustWellKnownPath(h.authorizationServerURL, openIDConfigurationWellKnownSuffix)
}

// ProtectedResourceMetadataURL returns the full RFC 9728 metadata URL for the MCP resource.
func (h *DiscoveryHandler) ProtectedResourceMetadataURL() string {
	return h.protectedResourceMeta
}

// AuthorizationServerMetadataURL returns the full RFC 8414 metadata URL for the auth server.
func (h *DiscoveryHandler) AuthorizationServerMetadataURL() string {
	return h.authorizationServerMeta
}

// OpenIDConfigurationURL returns the full OpenID configuration URL for the auth server.
func (h *DiscoveryHandler) OpenIDConfigurationURL() string {
	return h.openIDConfigurationMeta
}

// ServeProtectedResourceMetadata handles the protected-resource discovery request.
func (h *DiscoveryHandler) ServeProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	resp := DiscoveryResponse{
		Resource:               h.resourceURL,
		AuthorizationServers:   []string{h.authorizationServerURL},
		ScopesSupported:        []string{"mcp:read", "mcp:write", "mcp:ingest"},
		BearerMethodsSupported: []string{"header"},
	}
	writeDiscoveryJSON(w, resp)
}

// ServeAuthorizationServerMetadata handles OAuth 2.0 Authorization Server metadata requests.
func (h *DiscoveryHandler) ServeAuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	resp := AuthorizationServerMetadataResponse{
		Issuer:                            h.authorizationServerURL,
		AuthorizationEndpoint:             h.authorizationEndpoint,
		TokenEndpoint:                     h.tokenEndpoint,
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		ScopesSupported:                   []string{"mcp:read", "mcp:write", "mcp:ingest"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ResourceParameterSupported:        true,
		AuthorizationResponseIssSupported: true,
		ProtectedResources:                []string{h.resourceURL},
	}
	writeDiscoveryJSON(w, resp)
}

// ServeOpenIDConfiguration provides the same metadata on the OpenID discovery path
// for clients that only know the legacy compatibility endpoint.
func (h *DiscoveryHandler) ServeOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	h.ServeAuthorizationServerMetadata(w, r)
}

// ServeHTTP preserves the previous protected-resource discovery behavior.
func (h *DiscoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ServeProtectedResourceMetadata(w, r)
}

// WellKnownPath derives the RFC 8414 / RFC 9728 path for an identifier URL.
func WellKnownPath(identifier, wellKnownSuffix string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(identifier))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("identifier must be absolute")
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "" {
		return wellKnownSuffix, nil
	}
	return wellKnownSuffix + path, nil
}

// WellKnownURL derives the RFC 8414 / RFC 9728 full metadata URL for an identifier URL.
func WellKnownURL(identifier, wellKnownSuffix string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(identifier))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("identifier must be absolute")
	}
	path, err := WellKnownPath(identifier, wellKnownSuffix)
	if err != nil {
		return "", err
	}
	return parsed.Scheme + "://" + parsed.Host + path, nil
}

func mustWellKnownPath(identifier, wellKnownSuffix string) string {
	path, err := WellKnownPath(identifier, wellKnownSuffix)
	if err != nil {
		panic(err)
	}
	return path
}

func writeDiscoveryJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
