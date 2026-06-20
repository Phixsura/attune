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
	handler := oauth.NewDiscoveryHandler("https://attune.example.com")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

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
