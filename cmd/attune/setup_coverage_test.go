// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBuildConsoleRouter(t *testing.T) {
	t.Run("rejects missing console base url", func(t *testing.T) {
		cfg := ptrext.Of(config.Config{
			ConsoleSessionKey: strings.Repeat("a", 32),
		})

		router, err := buildConsoleRouter(cfg, nil, nil, nil, nil, nil, nil, nil)
		require.Error(t, err)
		require.Nil(t, router)
	})

	t.Run("builds with minimal wiring", func(t *testing.T) {
		cfg := ptrext.Of(config.Config{
			ConsoleBaseURL:    "https://console.example.test",
			ConsoleSessionKey: strings.Repeat("b", 32),
		})

		router, err := buildConsoleRouter(cfg, nil, nil, nil, nil, nil, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, router)
	})
}

func TestBuildMCPHandler(t *testing.T) {
	t.Run("rejects short secret", func(t *testing.T) {
		cfg := ptrext.Of(config.Config{
			MCPPublicBaseURL: "https://mcp.example.test",
			MCP: config.MCPConfig{
				OAuth: config.MCPOAuthConfig{JWTSecret: "short"},
			},
		})

		handler, err := buildMCPHandler(context.Background(), cfg, nil, nil)
		require.Error(t, err)
		require.Nil(t, handler)
	})

	t.Run("builds with issuer fallback", func(t *testing.T) {
		cfg := ptrext.Of(config.Config{
			MCPPublicBaseURL: "https://mcp.example.test",
			MCP: config.MCPConfig{
				OAuth: config.MCPOAuthConfig{
					JWTSecret: strings.Repeat("c", 32),
				},
			},
		})

		handler, err := buildMCPHandler(context.Background(), cfg, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, handler)

		mux := chi.NewRouter()
		require.NoError(t, handler.MountWellKnownRoutes(mux))
	})

	t.Run("builds with explicit issuer", func(t *testing.T) {
		cfg := ptrext.Of(config.Config{
			MCPPublicBaseURL: "https://mcp.example.test",
			MCP: config.MCPConfig{
				OAuth: config.MCPOAuthConfig{
					JWTSecret: strings.Repeat("d", 32),
					Issuer:    "https://auth.example.test/issuer",
				},
			},
		})

		handler, err := buildMCPHandler(context.Background(), cfg, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, handler)

		mux := chi.NewRouter()
		require.NoError(t, handler.MountWellKnownRoutes(mux))
	})
}
