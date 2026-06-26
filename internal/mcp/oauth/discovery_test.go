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

func TestDiscoveryHandler_Paths(t *testing.T) {
	t.Parallel()
	h := oauth.NewDiscoveryHandler("https://attune.example.com", "https://attune.example.com/mcp/oauth")

	assert.Contains(t, h.ProtectedResourceMetadataPath(), "oauth-protected-resource")
	assert.Contains(t, h.AuthorizationServerMetadataPath(), "oauth-authorization-server")
	assert.Contains(t, h.OpenIDConfigurationPath(), "openid-configuration")
}

func TestDiscoveryHandler_URLs(t *testing.T) {
	t.Parallel()
	h := oauth.NewDiscoveryHandler("https://attune.example.com", "https://attune.example.com/mcp/oauth")

	assert.Contains(t, h.ProtectedResourceMetadataURL(), "https://attune.example.com")
	assert.Contains(t, h.AuthorizationServerMetadataURL(), "https://attune.example.com")
	assert.Contains(t, h.OpenIDConfigurationURL(), "https://attune.example.com")
}

func TestDiscoveryHandler_ServeHTTP(t *testing.T) {
	t.Parallel()
	h := oauth.NewDiscoveryHandler("https://attune.example.com", "https://attune.example.com/mcp/oauth")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp oauth.DiscoveryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Resource)
}

func TestDiscoveryHandler_OpenIDConfiguration(t *testing.T) {
	t.Parallel()
	h := oauth.NewDiscoveryHandler("https://attune.example.com", "https://attune.example.com/mcp/oauth")
	rec := httptest.NewRecorder()
	h.ServeOpenIDConfiguration(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp oauth.AuthorizationServerMetadataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "https://attune.example.com/mcp/oauth", resp.Issuer)
}

func TestWellKnownURL(t *testing.T) {
	t.Parallel()

	t.Run("root issuer", func(t *testing.T) {
		t.Parallel()
		u, err := oauth.WellKnownURL("https://example.com", "/.well-known/oauth-authorization-server")
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/.well-known/oauth-authorization-server", u)
	})

	t.Run("path issuer", func(t *testing.T) {
		t.Parallel()
		u, err := oauth.WellKnownURL("https://example.com/mcp/oauth", "/.well-known/oauth-authorization-server")
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/.well-known/oauth-authorization-server/mcp/oauth", u)
	})

	t.Run("relative URL fails", func(t *testing.T) {
		t.Parallel()
		_, err := oauth.WellKnownURL("/relative", "/.well-known/x")
		require.Error(t, err)
	})
}

func TestWellKnownPath_Errors(t *testing.T) {
	t.Parallel()

	t.Run("relative URL", func(t *testing.T) {
		t.Parallel()
		_, err := oauth.WellKnownPath("/relative", "/.well-known/x")
		require.Error(t, err)
	})

	t.Run("no scheme", func(t *testing.T) {
		t.Parallel()
		_, err := oauth.WellKnownPath("example.com", "/.well-known/x")
		require.Error(t, err)
	})
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
