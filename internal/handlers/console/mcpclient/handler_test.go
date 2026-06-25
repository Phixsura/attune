// SPDX-License-Identifier: Apache-2.0

package mcpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestHandler_Create_ValidatesScopes(t *testing.T) {
	h := mcpclient.NewHandler(nil, nil, nil, nil) // nil repos - we're testing validation

	auth := ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "admin",
	})

	req := ptrext.Of(mcpclient.CreateRequest{
		Name:         "test-client",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"invalid:scope"},
	})

	_, err := h.Create(context.Background(), auth, req, httptest.NewRequest(http.MethodPost, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid scope")
}

func TestHandler_Create_RequiresRedirectURI(t *testing.T) {
	h := mcpclient.NewHandler(nil, nil, nil, nil)

	auth := ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "admin",
	})

	req := ptrext.Of(mcpclient.CreateRequest{
		Name:         "test-client",
		RedirectURIs: []string{},
		Scopes:       []string{domain.MCPScopeRead},
	})

	_, err := h.Create(context.Background(), auth, req, httptest.NewRequest(http.MethodPost, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirect_uris")
}

func TestHandler_Create_ValidatesRedirectURIs(t *testing.T) {
	h := mcpclient.NewHandler(nil, nil, nil, nil)

	auth := ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "admin",
	})

	tests := []struct {
		name        string
		redirectURI string
		wantErr     string
	}{
		{
			name:        "fragment not allowed",
			redirectURI: "https://example.com/callback#fragment",
			wantErr:     "fragment not allowed",
		},
		{
			name:        "javascript scheme rejected",
			redirectURI: "javascript:alert(1)",
			wantErr:     "dangerous URI scheme",
		},
		{
			name:        "data scheme rejected",
			redirectURI: "data:text/html,<script>alert(1)</script>",
			wantErr:     "dangerous URI scheme",
		},
		{
			name:        "http requires localhost",
			redirectURI: "http://example.com/callback",
			wantErr:     "must use HTTPS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ptrext.Of(mcpclient.CreateRequest{
				Name:         "test-client",
				RedirectURIs: []string{tt.redirectURI},
				Scopes:       []string{domain.MCPScopeRead},
			})

			_, err := h.Create(context.Background(), auth, req, httptest.NewRequest(http.MethodPost, "/", nil))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
