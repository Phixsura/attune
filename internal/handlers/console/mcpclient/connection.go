// SPDX-License-Identifier: Apache-2.0

package mcpclient

import (
	"strings"

	mcpoauth "github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ConnectionProfile describes the public MCP endpoints for one attune deployment.
type ConnectionProfile struct {
	ServerURL                            string `json:"server_url"`
	ResourceURL                          string `json:"resource_url"`
	OAuthIssuer                          string `json:"oauth_issuer"`
	AuthorizationEndpoint                string `json:"authorization_endpoint"`
	TokenEndpoint                        string `json:"token_endpoint"`
	ProtectedResourceMetadataURL         string `json:"protected_resource_metadata_url"`
	AuthorizationServerMetadataURL       string `json:"authorization_server_metadata_url"`
	OpenIDConfigurationURL               string `json:"openid_configuration_url"`
	LegacyProtectedResourceMetadataURL   string `json:"legacy_protected_resource_metadata_url"`
	LegacyAuthorizationServerMetadataURL string `json:"legacy_authorization_server_metadata_url"`
	LegacyOpenIDConfigurationURL         string `json:"legacy_openid_configuration_url"`
}

func newConnectionProfile(publicBaseURL, issuer string) *ConnectionProfile {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return nil
	}

	authIssuer := strings.TrimRight(strings.TrimSpace(issuer), "/")
	if authIssuer == "" {
		authIssuer = base + "/mcp/oauth"
	}

	discovery := mcpoauth.NewDiscoveryHandler(base, authIssuer)

	return ptrext.Of(ConnectionProfile{
		ServerURL:                            base + "/mcp",
		ResourceURL:                          base + "/mcp/v1",
		OAuthIssuer:                          authIssuer,
		AuthorizationEndpoint:                base + "/mcp/oauth/authorize",
		TokenEndpoint:                        base + "/mcp/oauth/token",
		ProtectedResourceMetadataURL:         discovery.ProtectedResourceMetadataURL(),
		AuthorizationServerMetadataURL:       discovery.AuthorizationServerMetadataURL(),
		OpenIDConfigurationURL:               discovery.OpenIDConfigurationURL(),
		LegacyProtectedResourceMetadataURL:   base + "/mcp/.well-known/oauth-protected-resource",
		LegacyAuthorizationServerMetadataURL: base + "/mcp/oauth/.well-known/oauth-authorization-server",
		LegacyOpenIDConfigurationURL:         base + "/mcp/oauth/.well-known/openid-configuration",
	})
}
