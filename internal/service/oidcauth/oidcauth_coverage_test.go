// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package oidcauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// --- mock implementations ---

type covMockUserStore struct {
	upsertFn func(ctx context.Context, u domain.OIDCUser) (domain.OIDCUser, error)
}

func (m *covMockUserStore) GetByExternalID(_ context.Context, _, _ string) (domain.OIDCUser, error) {
	return domain.OIDCUser{}, nil
}

func (m *covMockUserStore) Upsert(ctx context.Context, u domain.OIDCUser) (domain.OIDCUser, error) {
	return m.upsertFn(ctx, u)
}

type covMockTenantResolver struct {
	id  string
	err error
}

func (m *covMockTenantResolver) FirstActiveID(_ context.Context) (string, error) {
	return m.id, m.err
}

// --- NewService disabled ---

func TestCoverage_NewService_Disabled(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.OIDCConfig{Enabled: false})
	svc, err := NewService(t.Context(), cfg, nil, nil, nil)

	assert.Nil(t, svc)
	assert.NoError(t, err)
}

// --- FindOrCreateUser ---

func TestCoverage_FindOrCreateUser(t *testing.T) {
	t.Parallel()

	t.Run("successful upsert populates all fields", func(t *testing.T) {
		t.Parallel()

		returned := domain.OIDCUser{
			ID:          "u-42",
			Provider:    "oidc",
			ExternalID:  "ext-1",
			Email:       "alice@example.com",
			DisplayName: "Alice",
			Role:        "admin",
			Groups:      []string{"admins"},
			CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		store := &covMockUserStore{
			upsertFn: func(_ context.Context, u domain.OIDCUser) (domain.OIDCUser, error) {
				assert.Equal(t, "oidc", u.Provider)
				assert.Equal(t, "ext-1", u.ExternalID)
				assert.Equal(t, "alice@example.com", u.Email)
				assert.Equal(t, "Alice", u.DisplayName)
				assert.Equal(t, "admin", u.Role)
				assert.Equal(t, []string{"admins"}, u.Groups)
				return returned, nil
			},
		}

		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), users: store})
		claims := ptrext.Of(domain.OIDCClaims{
			Subject: "ext-1",
			Email:   "alice@example.com",
			Name:    "Alice",
			Groups:  []string{"admins"},
		})

		result, err := svc.FindOrCreateUser(t.Context(), claims, "admin")
		require.NoError(t, err)
		assert.Equal(t, "u-42", result.ID)
		assert.Equal(t, "oidc", result.Provider)
		assert.Equal(t, "ext-1", result.ExternalID)
	})

	t.Run("upsert error is propagated", func(t *testing.T) {
		t.Parallel()

		store := &covMockUserStore{
			upsertFn: func(_ context.Context, _ domain.OIDCUser) (domain.OIDCUser, error) {
				return domain.OIDCUser{}, errors.New("db connection lost")
			},
		}

		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), users: store})
		claims := ptrext.Of(domain.OIDCClaims{
			Subject: "ext-2",
			Email:   "bob@example.com",
			Name:    "Bob",
		})

		result, err := svc.FindOrCreateUser(t.Context(), claims, "member")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db connection lost")
		assert.Equal(t, domain.OIDCUser{}, result)
	})
}

// --- ResolveDefaultTenant ---

func TestCoverage_ResolveDefaultTenant(t *testing.T) {
	t.Parallel()

	t.Run("returns tenant ID on success", func(t *testing.T) {
		t.Parallel()

		resolver := &covMockTenantResolver{id: "tenant-abc", err: nil}
		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), tenants: resolver})

		result := svc.ResolveDefaultTenant(t.Context())
		assert.Equal(t, "tenant-abc", result)
	})

	t.Run("returns empty string on error", func(t *testing.T) {
		t.Parallel()

		resolver := &covMockTenantResolver{id: "", err: errors.New("lookup failed")}
		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), tenants: resolver})

		result := svc.ResolveDefaultTenant(t.Context())
		assert.Equal(t, "", result)
	})
}

// --- extractClaims ---

func TestCoverage_ExtractClaims(t *testing.T) {
	t.Parallel()

	baseCfg := ptrext.Of(config.OIDCConfig{
		UserClaim:   "email",
		GroupsClaim: "groups",
	})

	t.Run("missing sub claim", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"email": "alice@example.com",
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		assert.Nil(t, result)
		assert.EqualError(t, err, "missing sub claim")
	})

	t.Run("sub claim is not a string", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"sub":   12345,
			"email": "alice@example.com",
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		assert.Nil(t, result)
		assert.EqualError(t, err, "sub claim is not a string")
	})

	t.Run("missing user claim", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"sub": "user-1",
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		assert.Nil(t, result)
		assert.EqualError(t, err, "missing or invalid email claim")
	})

	t.Run("name falls back to preferred_username", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"sub":                "user-2",
			"email":              "bob@example.com",
			"preferred_username": "bob_user",
			"groups":             []string{"dev"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		assert.Equal(t, "bob_user", result.Name)
	})

	t.Run("name falls back to email", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"sub":    "user-3",
			"email":  "carol@example.com",
			"groups": []string{"dev"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		assert.Equal(t, "carol@example.com", result.Name)
	})

	t.Run("all standard claims present", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"sub":    "user-4",
			"email":  "dave@example.com",
			"name":   "Dave Smith",
			"groups": []string{"admins", "editors"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		assert.Equal(t, "user-4", result.Subject)
		assert.Equal(t, "dave@example.com", result.Email)
		assert.Equal(t, "Dave Smith", result.Name)
		assert.Equal(t, []string{"admins", "editors"}, result.Groups)
	})

	t.Run("empty sub claim", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg})
		claims := map[string]any{
			"sub":   "",
			"email": "empty@example.com",
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		assert.Nil(t, result)
		assert.EqualError(t, err, "sub claim is not a string")
	})
}

// --- sanitizeGroups edge cases ---

func TestCoverage_SanitizeGroups(t *testing.T) {
	t.Parallel()

	t.Run("empty input returns empty", func(t *testing.T) {
		t.Parallel()

		result := sanitizeGroups([]string{})
		assert.Empty(t, result)
	})

	t.Run("all groups within limit are kept", func(t *testing.T) {
		t.Parallel()

		groups := []string{"alpha", "beta", "gamma"}
		result := sanitizeGroups(groups)
		assert.Equal(t, groups, result)
	})

	t.Run("group exactly at max length is kept", func(t *testing.T) {
		t.Parallel()

		exactLen := strings.Repeat("x", maxGroupNameLen)
		result := sanitizeGroups([]string{exactLen})
		assert.Len(t, result, 1)
		assert.Equal(t, exactLen, result[0])
	})

	t.Run("group one over max length is filtered", func(t *testing.T) {
		t.Parallel()

		overLen := strings.Repeat("x", maxGroupNameLen+1)
		result := sanitizeGroups([]string{overLen})
		assert.Empty(t, result)
	})
}

// --- extractStringSlice edge cases ---

func TestCoverage_ExtractStringSlice(t *testing.T) {
	t.Parallel()

	t.Run("any slice with only non-string items", func(t *testing.T) {
		t.Parallel()

		claims := map[string]any{
			"groups": []any{1, 2.5, true},
		}
		result := extractStringSlice(claims, "groups")
		assert.Empty(t, result)
	})

	t.Run("empty string with Fields returns empty", func(t *testing.T) {
		t.Parallel()

		claims := map[string]any{
			"groups": "",
		}
		result := extractStringSlice(claims, "groups")
		assert.Empty(t, result)
	})
}

// --- constants verification ---

func TestCoverage_Constants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 256, maxGroupNameLen)
	assert.Equal(t, 100, maxGroupsCount)
	assert.Equal(t, 30*time.Second, discoveryTimeout)
	assert.Equal(t, 15*time.Second, tokenExchangeTimeout)
	assert.Equal(t, 10*time.Second, userInfoTimeout)
}
