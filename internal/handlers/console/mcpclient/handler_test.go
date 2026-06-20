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
	h := mcpclient.NewHandler(nil) // nil repo - we're testing validation

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
	h := mcpclient.NewHandler(nil)

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
