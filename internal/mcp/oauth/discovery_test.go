// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

func TestDiscoveryHandler(t *testing.T) {
	handler := oauth.NewDiscoveryHandler("https://attune.example.com", "https://attune.example.com/mcp/oauth")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	handler.ServeProtectedResourceMetadata(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))

	var resp oauth.DiscoveryResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "https://attune.example.com/mcp/v1", resp.Resource)
	assert.Contains(t, resp.AuthorizationServers, "https://attune.example.com/mcp/oauth")
	assert.Contains(t, resp.ScopesSupported, "mcp:read")
	assert.Contains(t, resp.ScopesSupported, "mcp:write")
	assert.Contains(t, resp.ScopesSupported, "mcp:ingest")
	assert.Contains(t, resp.BearerMethodsSupported, "header")
}

func TestDiscoveryHandlerAuthorizationServerMetadata(t *testing.T) {
	handler := oauth.NewDiscoveryHandler("https://attune.example.com", "https://attune.example.com/mcp/oauth")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/oauth", nil)
	rec := httptest.NewRecorder()

	handler.ServeAuthorizationServerMetadata(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp oauth.AuthorizationServerMetadataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "https://attune.example.com/mcp/oauth", resp.Issuer)
	assert.Equal(t, "https://attune.example.com/mcp/oauth/authorize", resp.AuthorizationEndpoint)
	assert.Equal(t, "https://attune.example.com/mcp/oauth/token", resp.TokenEndpoint)
	assert.Contains(t, resp.GrantTypesSupported, "authorization_code")
	assert.Contains(t, resp.GrantTypesSupported, "refresh_token")
	assert.Contains(t, resp.CodeChallengeMethodsSupported, "S256")
	assert.Contains(t, resp.TokenEndpointAuthMethodsSupported, "none")
	assert.True(t, resp.ResourceParameterSupported)
	assert.True(t, resp.AuthorizationResponseIssSupported)
	assert.Contains(t, resp.ProtectedResources, "https://attune.example.com/mcp/v1")
}

func TestWellKnownPath(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		suffix     string
		want       string
	}{
		{
			name:       "root issuer",
			identifier: "https://attune.example.com",
			suffix:     "/.well-known/oauth-authorization-server",
			want:       "/.well-known/oauth-authorization-server",
		},
		{
			name:       "path issuer",
			identifier: "https://attune.example.com/mcp/oauth",
			suffix:     "/.well-known/oauth-authorization-server",
			want:       "/.well-known/oauth-authorization-server/mcp/oauth",
		},
		{
			name:       "resource path",
			identifier: "https://attune.example.com/mcp/v1",
			suffix:     "/.well-known/oauth-protected-resource",
			want:       "/.well-known/oauth-protected-resource/mcp/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := oauth.WellKnownPath(tt.identifier, tt.suffix)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
