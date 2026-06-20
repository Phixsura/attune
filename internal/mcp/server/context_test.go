// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestContextClaims(t *testing.T) {
	clientID := uuid.New()
	sessionID := uuid.New()

	claims := ptrext.Of(oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  clientID,
		SessionID: sessionID,
		Scopes:    []string{"mcp:read", "mcp:write"},
	})

	ctx := server.WithClaims(context.Background(), claims)

	assert.Equal(t, "tenant-123", server.TenantIDFromContext(ctx))
	assert.Equal(t, clientID, server.ClientIDFromContext(ctx))
	assert.Equal(t, sessionID, server.SessionIDFromContext(ctx))
	assert.Equal(t, []string{"mcp:read", "mcp:write"}, server.ScopesFromContext(ctx))
}

func TestContextClaims_Empty(t *testing.T) {
	ctx := context.Background()

	assert.Nil(t, server.ClaimsFromContext(ctx))
	assert.Equal(t, "", server.TenantIDFromContext(ctx))
	assert.Equal(t, uuid.Nil, server.ClientIDFromContext(ctx))
	assert.Equal(t, uuid.Nil, server.SessionIDFromContext(ctx))
	assert.Nil(t, server.ScopesFromContext(ctx))
}

func TestHasScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		check    string
		expected bool
	}{
		{"exact match", []string{"mcp:read"}, "mcp:read", true},
		{"no match", []string{"mcp:read"}, "mcp:write", false},
		{"write implies read", []string{"mcp:write"}, "mcp:read", true},
		{"read does not imply write", []string{"mcp:read"}, "mcp:write", false},
		{"ingest is separate", []string{"mcp:write"}, "mcp:ingest", false},
		{"empty scopes", []string{}, "mcp:read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := ptrext.Of(oauth.AccessTokenClaims{Scopes: tt.scopes})
			ctx := server.WithClaims(context.Background(), claims)
			assert.Equal(t, tt.expected, server.HasScope(ctx, tt.check))
		})
	}
}

func TestHasScope_NoClaims(t *testing.T) {
	ctx := context.Background()
	assert.False(t, server.HasScope(ctx, "mcp:read"))
}
