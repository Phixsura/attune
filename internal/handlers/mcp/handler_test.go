// SPDX-License-Identifier: Apache-2.0

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

type mockFeedbackReader struct{}

func (m *mockFeedbackReader) ListForConsole(_ context.Context, _ string, _ feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error) {
	return []feedback.ConsoleListRow{{ID: 1, Content: "test"}}, nil
}

func (m *mockFeedbackReader) GetForConsole(_ context.Context, _ string, _ int64) (*feedback.ConsoleDetailRow, error) {
	return nil, feedback.ErrFeedbackNotFound
}

func TestHandler_Discovery(t *testing.T) {
	cfg := mcp.Config{
		BaseURL:   "https://attune.example.com",
		JWTSecret: []byte("test-secret-key-for-jwt-signing-32b"),
		JWTIssuer: "https://attune.example.com/mcp/oauth",
	}
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{}),
	})

	h := mcp.NewHandler(cfg, deps)
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp oauth.DiscoveryResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "https://attune.example.com/mcp/v1", resp.Resource)
}

func TestHandler_Unauthorized(t *testing.T) {
	cfg := mcp.Config{
		BaseURL:   "https://attune.example.com",
		JWTSecret: []byte("test-secret-key-for-jwt-signing-32b"),
		JWTIssuer: "https://attune.example.com/mcp/oauth",
	}
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{}),
	})

	h := mcp.NewHandler(cfg, deps)
	router := h.Routes()

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_AuthenticatedRequest(t *testing.T) {
	cfg := mcp.Config{
		BaseURL:   "https://attune.example.com",
		JWTSecret: []byte("test-secret-key-for-jwt-signing-32b"),
		JWTIssuer: "https://attune.example.com/mcp/oauth",
	}
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{}),
	})

	h := mcp.NewHandler(cfg, deps)
	router := h.Routes()

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}
	token, err := h.Signer().Sign(claims, time.Hour)
	require.NoError(t, err)

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp jsonrpc.Response
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
}
