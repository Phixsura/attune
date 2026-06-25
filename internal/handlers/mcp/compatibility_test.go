// SPDX-License-Identifier: Apache-2.0

package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
)

const compatCodeVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

// These cases codify the discovery/auth patterns used across mainstream
// remote MCP clients so we do not regress one host while fixing another.
func TestCompatibilityMatrix(t *testing.T) {
	t.Parallel()

	h, root := newCompatibilityRouter(t)

	t.Run("root protected resource metadata", func(t *testing.T) {
		rec := serve(root, http.MethodGet, "/.well-known/oauth-protected-resource/mcp/v1", nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp oauth.DiscoveryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "https://attune.example.com/mcp/v1", resp.Resource)
		assert.Equal(t, []string{"https://attune.example.com/mcp/oauth"}, resp.AuthorizationServers)
	})

	t.Run("legacy protected resource metadata", func(t *testing.T) {
		rec := serve(root, http.MethodGet, "/mcp/.well-known/oauth-protected-resource", nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp oauth.DiscoveryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "https://attune.example.com/mcp/v1", resp.Resource)
	})

	t.Run("root authorization server metadata", func(t *testing.T) {
		rec := serve(root, http.MethodGet, "/.well-known/oauth-authorization-server/mcp/oauth", nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp oauth.AuthorizationServerMetadataResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "https://attune.example.com/mcp/oauth", resp.Issuer)
		assert.True(t, resp.ResourceParameterSupported)
		assert.True(t, resp.AuthorizationResponseIssSupported)
	})

	t.Run("legacy authorization server metadata", func(t *testing.T) {
		rec := serve(root, http.MethodGet, "/mcp/oauth/.well-known/oauth-authorization-server", nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp oauth.AuthorizationServerMetadataResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "https://attune.example.com/mcp/oauth/token", resp.TokenEndpoint)
	})

	t.Run("openid configuration compatibility path", func(t *testing.T) {
		rec := serve(root, http.MethodGet, "/.well-known/openid-configuration/mcp/oauth", nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp oauth.AuthorizationServerMetadataResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "https://attune.example.com/mcp/oauth/authorize", resp.AuthorizationEndpoint)
	})

	t.Run("unauthenticated tool call challenge", func(t *testing.T) {
		rec := serve(root, http.MethodPost, "/mcp/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"1","method":"list_feedback"}`), map[string]string{
			"Content-Type": "application/json",
		})
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata")
	})

	t.Run("invalid token challenge", func(t *testing.T) {
		rec := serve(root, http.MethodPost, "/mcp/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"1","method":"list_feedback"}`), map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer invalid-token",
		})
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
	})

	t.Run("insufficient scope challenge", func(t *testing.T) {
		rec := serve(root, http.MethodPost, "/mcp/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"2","method":"submit_feedback","params":{"content":"hello"}}`), map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + newTestTokenWithScopes(t, h, []string{"mcp:read"}),
		})
		require.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `scope="mcp:ingest"`)

		var resp jsonrpc.Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotNil(t, resp.Error)
		assert.Equal(t, "insufficient scope", resp.Error.Message)
	})

	t.Run("authorize success includes issuer", func(t *testing.T) {
		challenge := oauth.GenerateCodeChallenge(compatCodeVerifier)
		rec := serve(root, http.MethodGet, "/mcp/oauth/authorize?"+url.Values{
			"client_id":             {uuid.New().String()},
			"redirect_uri":          {"https://attune.example.com/callback"},
			"response_type":         {"code"},
			"scope":                 {"mcp:read"},
			"state":                 {"compat-state"},
			"resource":              {"https://attune.example.com/mcp/v1"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}.Encode(), nil, nil)
		require.Equal(t, http.StatusFound, rec.Code)
		location, err := rec.Result().Location()
		require.NoError(t, err)
		assert.Equal(t, "https://attune.example.com/mcp/oauth", location.Query().Get("iss"))
		assert.Equal(t, "compat-state", location.Query().Get("state"))
		assert.NotEmpty(t, location.Query().Get("code"))
	})
}

func TestCompatibilityResourceParameterHandling(t *testing.T) {
	t.Parallel()

	_, root := newCompatibilityRouter(t)
	challenge := oauth.GenerateCodeChallenge(compatCodeVerifier)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp/oauth/authorize?"+url.Values{
		"client_id":             {uuid.New().String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"state":                 {"resource-ok"},
		"resource":              {"https://attune.example.com/mcp/v1"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	root.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)

	location, err := rec.Result().Location()
	require.NoError(t, err)
	assert.Equal(t, "https://attune.example.com/mcp/oauth", location.Query().Get("iss"))
	assert.NotEmpty(t, location.Query().Get("code"))

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/mcp/oauth/authorize?"+url.Values{
		"client_id":             {uuid.New().String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"state":                 {"resource-bad"},
		"resource":              {"https://evil.example/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	root.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)

	location, err = rec.Result().Location()
	require.NoError(t, err)
	assert.Equal(t, "invalid_target", location.Query().Get("error"))
}

func newCompatibilityRouter(t *testing.T) (*mcp.Handler, chi.Router) {
	t.Helper()

	h := newTestHandler()
	root := chi.NewRouter()
	require.NoError(t, h.MountWellKnownRoutes(root))
	root.Mount("/mcp", h.Routes())
	return h, root
}

func serve(r http.Handler, method, target string, body *bytes.Buffer, headers map[string]string) *httptest.ResponseRecorder {
	var reader *bytes.Buffer
	if body == nil {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = body
	}
	req := httptest.NewRequest(method, target, reader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
