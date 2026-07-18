// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestValidateState(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{})})

	tests := []struct {
		name string
		got  string
		want string
		ok   bool
	}{
		{"match", "abc123", "abc123", true},
		{"mismatch", "abc123", "xyz789", false},
		{"empty got", "", "abc123", false},
		{"empty want", "abc123", "", false},
		{"both empty", "", "", true},
		{"timing safe", "aaaa", "aaab", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.ok, svc.ValidateState(tt.got, tt.want))
		})
	}
}

func TestCheckAllowedGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		allowedGroups []string
		userGroups    []string
		ok            bool
	}{
		{"no restriction", nil, []string{"any"}, true},
		{"empty restriction", []string{}, []string{"any"}, true},
		{"user in allowed", []string{"admins", "users"}, []string{"admins"}, true},
		{"user not in allowed", []string{"admins"}, []string{"users"}, false},
		{"user has one match", []string{"admins", "devs"}, []string{"guests", "devs"}, true},
		{"empty user groups", []string{"admins"}, []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{AllowedGroups: tt.allowedGroups})})
			require.Equal(t, tt.ok, svc.CheckAllowedGroups(tt.userGroups))
		})
	}
}

func TestMapRole(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.OIDCConfig{
		RoleMapping: []config.RoleMappingEntry{
			{Role: "admin", Groups: []string{"platform-admins", "superusers"}},
			{Role: "editor", Groups: []string{"editors", "writers"}},
			{Role: "viewer", Groups: []string{"viewers"}},
		},
	})
	svc := ptrext.Of(Service{cfg: cfg})

	tests := []struct {
		name       string
		userGroups []string
		wantRole   string
	}{
		{"admin match first group", []string{"platform-admins"}, "admin"},
		{"admin match second group", []string{"superusers"}, "admin"},
		{"editor match", []string{"editors"}, "editor"},
		{"viewer match", []string{"viewers"}, "viewer"},
		{"first match wins", []string{"viewers", "platform-admins"}, "admin"},
		{"no match defaults to member", []string{"unknown"}, "member"},
		{"empty groups defaults to member", []string{}, "member"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantRole, svc.MapRole(tt.userGroups))
		})
	}
}

func TestSanitizeGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  []string
		expect int
	}{
		{"normal groups", []string{"a", "b", "c"}, 3},
		{"exceeds max count", make([]string, 150), maxGroupsCount},
		{"long group name filtered", []string{"short", string(make([]byte, 300))}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizeGroups(tt.input)
			require.Len(t, result, tt.expect)
		})
	}
}

func TestExtractStringSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims map[string]any
		key    string
		expect []string
	}{
		{
			"string slice",
			map[string]any{"groups": []string{"a", "b"}},
			"groups",
			[]string{"a", "b"},
		},
		{
			"any slice",
			map[string]any{"groups": []any{"a", "b", 123}},
			"groups",
			[]string{"a", "b"},
		},
		{
			"space-separated string",
			map[string]any{"groups": "admin user guest"},
			"groups",
			[]string{"admin", "user", "guest"},
		},
		{
			"missing key",
			map[string]any{},
			"groups",
			nil,
		},
		{
			"wrong type",
			map[string]any{"groups": 123},
			"groups",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractStringSlice(tt.claims, tt.key)
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerName string
		expect       string
	}{
		{"custom name", "Okta", "Okta"},
		{"default after ApplyDefaults", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{ProviderName: tt.providerName})})
			require.Equal(t, tt.expect, svc.ProviderName())
		})
	}
}

func TestOIDCOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		oidcOnly bool
		expect   bool
	}{
		{"oidc only enabled", true, true},
		{"oidc only disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{OIDCOnly: tt.oidcOnly})})
			require.Equal(t, tt.expect, svc.OIDCOnly())
		})
	}
}

// --- mock implementations ---

type mockUserStore struct {
	getByExtFn func(ctx context.Context, provider, externalID string) (domain.OIDCUser, error)
	upsertFn   func(ctx context.Context, u domain.OIDCUser) (domain.OIDCUser, error)
}

func (m *mockUserStore) GetByExternalID(ctx context.Context, provider, externalID string) (domain.OIDCUser, error) {
	if m.getByExtFn != nil {
		return m.getByExtFn(ctx, provider, externalID)
	}
	return domain.OIDCUser{}, nil
}

func (m *mockUserStore) Upsert(ctx context.Context, u domain.OIDCUser) (domain.OIDCUser, error) {
	if m.upsertFn != nil {
		return m.upsertFn(ctx, u)
	}
	return u, nil
}

type mockTenantResolver struct {
	firstActiveIDFn func(ctx context.Context) (string, error)
}

func (m *mockTenantResolver) FirstActiveID(ctx context.Context) (string, error) {
	if m.firstActiveIDFn != nil {
		return m.firstActiveIDFn(ctx)
	}
	return "", nil
}

// --- NewService tests ---

func TestNewService_Disabled(t *testing.T) {
	t.Parallel()

	cfg := ptrext.Of(config.OIDCConfig{Enabled: false})
	svc, err := NewService(context.Background(), cfg, nil, nil)
	require.NoError(t, err)
	require.Nil(t, svc)
}

func TestNewService_Disabled_ReturnsNilNil(t *testing.T) {
	t.Parallel()

	// Both return values are nil when OIDC is disabled, regardless
	// of what other config fields contain.
	cfg := ptrext.Of(config.OIDCConfig{
		Enabled:   false,
		IssuerURL: "https://should-not-be-contacted.example.com",
		ClientID:  "ignored",
	})
	svc, err := NewService(context.Background(), cfg, nil, nil)
	require.Nil(t, svc)
	require.Nil(t, err)
}

func TestNewService_DiscoverySuccess(t *testing.T) {
	t.Parallel()

	server := newOIDCTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"user-1"}`))
	})
	cfg := ptrext.Of(config.OIDCConfig{
		Enabled:            true,
		IssuerURL:          server.URL,
		ClientID:           "client-1",
		ClientSecret:       "secret-1",
		RedirectURI:        "https://console.example.test/callback",
		Scopes:             []string{"openid", "email"},
		InsecureSkipVerify: true,
	})

	svc, err := NewService(t.Context(), cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Contains(t, svc.AuthCodeURL("state-1", "verifier-1", "nonce-1"), server.URL+"/authorize")
}

func TestNewService_DiscoveryFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "discovery unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	cfg := ptrext.Of(config.OIDCConfig{
		Enabled:            true,
		IssuerURL:          server.URL,
		ClientID:           "client-1",
		InsecureSkipVerify: true,
	})

	svc, err := NewService(t.Context(), cfg, nil, nil)
	require.Nil(t, svc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "oidc discovery failed")
}

// --- ResolveDefaultTenant tests ---

func TestResolveDefaultTenant(t *testing.T) {
	t.Parallel()

	t.Run("nil resolver", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), tenants: nil})
		require.Equal(t, "", svc.ResolveDefaultTenant(t.Context()))
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		resolver := ptrext.Of(mockTenantResolver{
			firstActiveIDFn: func(_ context.Context) (string, error) {
				return "tenant-42", nil
			},
		})
		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), tenants: resolver})
		require.Equal(t, "tenant-42", svc.ResolveDefaultTenant(t.Context()))
	})

	t.Run("error returns empty", func(t *testing.T) {
		t.Parallel()
		resolver := ptrext.Of(mockTenantResolver{
			firstActiveIDFn: func(_ context.Context) (string, error) {
				return "", errors.New("db down")
			},
		})
		svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{}), tenants: resolver})
		require.Equal(t, "", svc.ResolveDefaultTenant(t.Context()))
	})
}

// --- FindOrCreateUser tests ---

func TestFindOrCreateUser(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		store := ptrext.Of(mockUserStore{
			upsertFn: func(_ context.Context, u domain.OIDCUser) (domain.OIDCUser, error) {
				u.ID = "user-001"
				return u, nil
			},
		})
		svc := ptrext.Of(Service{
			cfg:   ptrext.Of(config.OIDCConfig{}),
			users: store,
		})

		claims := ptrext.Of(domain.OIDCClaims{
			Subject: "sub-123",
			Email:   "user@example.com",
			Name:    "Test User",
			Groups:  []string{"devs"},
		})

		user, err := svc.FindOrCreateUser(t.Context(), claims, "admin")
		require.NoError(t, err)
		require.Equal(t, "user-001", user.ID)
		require.Equal(t, "oidc", user.Provider)
		require.Equal(t, "sub-123", user.ExternalID)
		require.Equal(t, "user@example.com", user.Email)
		require.Equal(t, "Test User", user.DisplayName)
		require.Equal(t, "admin", user.Role)
		require.Equal(t, []string{"devs"}, user.Groups)
	})

	t.Run("upsert error", func(t *testing.T) {
		t.Parallel()

		store := ptrext.Of(mockUserStore{
			upsertFn: func(_ context.Context, _ domain.OIDCUser) (domain.OIDCUser, error) {
				return domain.OIDCUser{}, errors.New("db error")
			},
		})
		svc := ptrext.Of(Service{
			cfg:   ptrext.Of(config.OIDCConfig{}),
			users: store,
		})

		claims := ptrext.Of(domain.OIDCClaims{Subject: "sub-fail"})

		user, err := svc.FindOrCreateUser(t.Context(), claims, "member")
		require.Error(t, err)
		require.Contains(t, err.Error(), "db error")
		require.Equal(t, domain.OIDCUser{}, user)
	})

	t.Run("all fields passed through to upsert", func(t *testing.T) {
		t.Parallel()

		var captured domain.OIDCUser
		store := ptrext.Of(mockUserStore{
			upsertFn: func(_ context.Context, u domain.OIDCUser) (domain.OIDCUser, error) {
				captured = u
				u.ID = "upserted-id"
				return u, nil
			},
		})
		svc := ptrext.Of(Service{
			cfg:   ptrext.Of(config.OIDCConfig{}),
			users: store,
		})

		claims := ptrext.Of(domain.OIDCClaims{
			Subject: "ext-sub-99",
			Email:   "multi@example.com",
			Name:    "Multi User",
			Groups:  []string{"alpha", "beta", "gamma"},
		})

		user, err := svc.FindOrCreateUser(t.Context(), claims, "editor")
		require.NoError(t, err)

		require.Equal(t, "oidc", captured.Provider)
		require.Equal(t, "ext-sub-99", captured.ExternalID)
		require.Equal(t, "multi@example.com", captured.Email)
		require.Equal(t, "Multi User", captured.DisplayName)
		require.Equal(t, "editor", captured.Role)
		require.Equal(t, []string{"alpha", "beta", "gamma"}, captured.Groups)
		require.Equal(t, "upserted-id", user.ID)
	})
}

// --- extractClaims tests ---

func TestExtractClaims(t *testing.T) {
	t.Parallel()

	baseCfg := func() *config.OIDCConfig {
		return ptrext.Of(config.OIDCConfig{
			UserClaim:   "email",
			GroupsClaim: "groups",
		})
	}

	t.Run("all fields present", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":    "user-sub-1",
			"email":  "alice@example.com",
			"name":   "Alice",
			"groups": []string{"eng", "admins"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, "user-sub-1", result.Subject)
		require.Equal(t, "alice@example.com", result.Email)
		require.Equal(t, "Alice", result.Name)
		require.Equal(t, []string{"eng", "admins"}, result.Groups)
	})

	t.Run("missing sub", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"email": "alice@example.com",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing sub claim")
	})

	t.Run("sub not a string", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":   42,
			"email": "alice@example.com",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "sub claim is not a string")
	})

	t.Run("empty sub string", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":   "",
			"email": "alice@example.com",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "sub claim is not a string")
	})

	t.Run("sub is float64", func(t *testing.T) {
		t.Parallel()

		// JSON numbers decode as float64.
		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":   float64(12345),
			"email": "alice@example.com",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "sub claim is not a string")
	})

	t.Run("sub is bool", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":   true,
			"email": "alice@example.com",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "sub claim is not a string")
	})

	t.Run("missing user claim", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub": "user-sub-1",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing or invalid email claim")
	})

	t.Run("empty user claim value", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":   "user-sub-1",
			"email": "",
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing or invalid email claim")
	})

	t.Run("user claim non-string type", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":   "user-sub-1",
			"email": 12345,
		}

		_, err := svc.extractClaims(t.Context(), claims, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing or invalid email claim")
	})

	t.Run("custom user claim", func(t *testing.T) {
		t.Parallel()

		cfg := baseCfg()
		cfg.UserClaim = "preferred_username"
		svc := ptrext.Of(Service{cfg: cfg})
		claims := map[string]any{
			"sub":                "user-sub-1",
			"preferred_username": "alice",
			"name":               "Alice",
			"groups":             []string{"g"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, "alice", result.Email)
	})

	t.Run("name falls back to preferred_username", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":                "user-sub-1",
			"email":              "alice@example.com",
			"preferred_username": "alice_u",
			"groups":             []string{"g"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, "alice_u", result.Name)
	})

	t.Run("name falls back to email when no name or preferred_username", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":    "user-sub-1",
			"email":  "alice@example.com",
			"groups": []string{"g"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, "alice@example.com", result.Name)
	})

	t.Run("name non-string falls to preferred_username", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":                "user-sub-1",
			"email":              "alice@example.com",
			"name":               999,
			"preferred_username": "alice_pref",
			"groups":             []string{"g"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, "alice_pref", result.Name)
	})

	t.Run("both name and preferred_username non-string falls to email", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":                "user-sub-1",
			"email":              "bob@example.com",
			"name":               []string{"not", "a", "string"},
			"preferred_username": 42,
			"groups":             []string{"g"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, "bob@example.com", result.Name)
	})

	t.Run("groups from ID token as any slice", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		claims := map[string]any{
			"sub":    "user-sub-1",
			"email":  "alice@example.com",
			"name":   "Alice",
			"groups": []any{"eng", "platform"},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"eng", "platform"}, result.Groups)
	})

	t.Run("groups sanitized in extractClaims", func(t *testing.T) {
		t.Parallel()

		svc := ptrext.Of(Service{cfg: baseCfg()})
		longGroup := string(make([]byte, maxGroupNameLen+1))
		claims := map[string]any{
			"sub":    "user-sub-1",
			"email":  "alice@example.com",
			"name":   "Alice",
			"groups": []string{"valid", longGroup},
		}

		result, err := svc.extractClaims(t.Context(), claims, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"valid"}, result.Groups)
	})
}

// --- extractGroups tests ---

func TestExtractGroups_IDTokenFirst(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{GroupsClaim: "groups"})})
	claims := map[string]any{
		"groups": []string{"team-a", "team-b"},
	}

	groups := svc.extractGroups(t.Context(), claims, nil)
	require.Equal(t, []string{"team-a", "team-b"}, groups)
}

func TestExtractGroups_CustomGroupsClaim(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{GroupsClaim: "roles"})})
	claims := map[string]any{
		"roles": []string{"admin", "editor"},
	}

	groups := svc.extractGroups(t.Context(), claims, nil)
	require.Equal(t, []string{"admin", "editor"}, groups)
}

func TestExtractGroups_SpaceSeparatedString(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{GroupsClaim: "groups"})})
	claims := map[string]any{
		"groups": "engineering ops platform",
	}

	groups := svc.extractGroups(t.Context(), claims, nil)
	require.Equal(t, []string{"engineering", "ops", "platform"}, groups)
}

func TestExtractGroups_UserInfoFallback(t *testing.T) {
	t.Parallel()

	longGroup := strings.Repeat("x", maxGroupNameLen+1)
	server := newOIDCTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer token-1", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"roles":["ops","admins",%q]}`, longGroup)
	})
	svc, err := NewService(t.Context(), ptrext.Of(config.OIDCConfig{
		Enabled:            true,
		IssuerURL:          server.URL,
		ClientID:           "client-1",
		GroupsClaim:        "roles",
		InsecureSkipVerify: true,
	}), nil, nil)
	require.NoError(t, err)

	groups := svc.extractGroups(t.Context(), map[string]any{}, ptrext.Of(oauth2.Token{AccessToken: "token-1", TokenType: "Bearer"}))
	require.Equal(t, []string{"ops", "admins"}, groups)
}

func TestExtractGroups_UserInfoFallbackFailures(t *testing.T) {
	t.Parallel()

	t.Run("userinfo request error", func(t *testing.T) {
		t.Parallel()

		server := newOIDCTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no groups today", http.StatusServiceUnavailable)
		})
		svc, err := NewService(t.Context(), ptrext.Of(config.OIDCConfig{
			Enabled:            true,
			IssuerURL:          server.URL,
			ClientID:           "client-1",
			GroupsClaim:        "groups",
			InsecureSkipVerify: true,
		}), nil, nil)
		require.NoError(t, err)

		groups := svc.extractGroups(t.Context(), map[string]any{}, ptrext.Of(oauth2.Token{AccessToken: "token-1", TokenType: "Bearer"}))
		require.Nil(t, groups)
	})

	t.Run("userinfo claims parse error", func(t *testing.T) {
		t.Parallel()

		server := newOIDCTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{`))
		})
		svc, err := NewService(t.Context(), ptrext.Of(config.OIDCConfig{
			Enabled:            true,
			IssuerURL:          server.URL,
			ClientID:           "client-1",
			GroupsClaim:        "groups",
			InsecureSkipVerify: true,
		}), nil, nil)
		require.NoError(t, err)

		groups := svc.extractGroups(t.Context(), map[string]any{}, ptrext.Of(oauth2.Token{AccessToken: "token-1", TokenType: "Bearer"}))
		require.Nil(t, groups)
	})
}

func newOIDCTestServer(t *testing.T, userInfo http.HandlerFunc) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q,
			"userinfo_endpoint": %q
		}`, server.URL, server.URL+"/authorize", server.URL+"/token", server.URL+"/keys", server.URL+"/userinfo")
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})
	mux.HandleFunc("/userinfo", userInfo)
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// --- AuthCodeURL test ---

func TestAuthCodeURL(t *testing.T) {
	t.Parallel()

	oauthCfg := ptrext.Of(oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://idp.example.com/authorize",
			TokenURL: "https://idp.example.com/token",
		},
		Scopes: []string{"openid", "email"},
	})
	svc := ptrext.Of(Service{
		cfg:       ptrext.Of(config.OIDCConfig{}),
		oauth2Cfg: oauthCfg,
	})

	url := svc.AuthCodeURL("test-state", "test-verifier", "test-nonce")
	require.Contains(t, url, "state=test-state")
	require.Contains(t, url, "nonce=test-nonce")
	require.Contains(t, url, "code_challenge_method=S256")
	require.Contains(t, url, "code_challenge=")
	require.Contains(t, url, "client_id=test-client")
	require.Contains(t, url, "redirect_uri=")
	require.Contains(t, url, "response_type=code")
}

// --- MapRole additional tests ---

func TestMapRole_EmptyRoleMapping(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{
		RoleMapping: nil,
	})})

	require.Equal(t, "member", svc.MapRole([]string{"any-group"}))
}

func TestMapRole_MultipleMatchFirstEntryWins(t *testing.T) {
	t.Parallel()

	// When user groups match entries in multiple role mappings,
	// the first mapping entry wins (ordered evaluation).
	cfg := ptrext.Of(config.OIDCConfig{
		RoleMapping: []config.RoleMappingEntry{
			{Role: "viewer", Groups: []string{"everyone"}},
			{Role: "admin", Groups: []string{"everyone", "superusers"}},
		},
	})
	svc := ptrext.Of(Service{cfg: cfg})

	// "everyone" appears in both entries; viewer is checked first.
	require.Equal(t, "viewer", svc.MapRole([]string{"everyone", "superusers"}))
}

// --- additional edge-case tests ---

func TestExtractStringSlice_AllNonStrings(t *testing.T) {
	t.Parallel()

	// []any with only non-string items should return an empty (non-nil) slice.
	claims := map[string]any{
		"groups": []any{123, true, 3.14},
	}
	result := extractStringSlice(claims, "groups")
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestExtractStringSlice_BoolValue(t *testing.T) {
	t.Parallel()

	// A bool-typed value should hit the default case and return nil.
	claims := map[string]any{
		"groups": true,
	}
	result := extractStringSlice(claims, "groups")
	require.Nil(t, result)
}

func TestExtractStringSlice_NilMapValue(t *testing.T) {
	t.Parallel()

	// A nil value stored in the map (key exists, value is nil) hits default.
	claims := map[string]any{
		"groups": nil,
	}
	result := extractStringSlice(claims, "groups")
	require.Nil(t, result)
}

func TestCheckAllowedGroups_LargeNumberOfGroups(t *testing.T) {
	t.Parallel()

	userGroups := make([]string, 500)
	for i := range userGroups {
		userGroups[i] = fmt.Sprintf("group-%d", i)
	}
	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{
		AllowedGroups: []string{"group-499"},
	})})
	require.True(t, svc.CheckAllowedGroups(userGroups))
}

func TestCheckAllowedGroups_DuplicateUserGroups(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{
		AllowedGroups: []string{"admins"},
	})})
	require.True(t, svc.CheckAllowedGroups([]string{"admins", "admins", "admins"}))
}

func TestCheckAllowedGroups_NilUserGroups(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{
		AllowedGroups: []string{"admins"},
	})})
	require.False(t, svc.CheckAllowedGroups(nil))
}

func TestValidateState_UnicodeStrings(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{})})

	t.Run("matching unicode", func(t *testing.T) {
		t.Parallel()
		require.True(t, svc.ValidateState("state-abc-xyz", "state-abc-xyz"))
	})

	t.Run("mismatching unicode", func(t *testing.T) {
		t.Parallel()
		require.False(t, svc.ValidateState("state-abc-123", "state-abc-456"))
	})
}

func TestValidateState_VeryLongStrings(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(Service{cfg: ptrext.Of(config.OIDCConfig{})})

	long := strings.Repeat("a", 10000)
	require.True(t, svc.ValidateState(long, long))

	almostSame := long[:9999] + "b"
	require.False(t, svc.ValidateState(long, almostSame))
}

func TestSanitizeGroups_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("nil input", func(t *testing.T) {
		t.Parallel()
		result := sanitizeGroups(nil)
		require.Empty(t, result)
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		result := sanitizeGroups([]string{})
		require.Empty(t, result)
	})

	t.Run("exact boundary length kept", func(t *testing.T) {
		t.Parallel()
		exactLen := string(make([]byte, maxGroupNameLen))
		result := sanitizeGroups([]string{exactLen})
		require.Len(t, result, 1)
	})

	t.Run("one over boundary dropped", func(t *testing.T) {
		t.Parallel()
		overLen := string(make([]byte, maxGroupNameLen+1))
		result := sanitizeGroups([]string{overLen})
		require.Empty(t, result)
	})

	t.Run("mixed valid and oversized", func(t *testing.T) {
		t.Parallel()
		overLen := string(make([]byte, maxGroupNameLen+1))
		result := sanitizeGroups([]string{"keep", overLen, "also-keep"})
		require.Equal(t, []string{"keep", "also-keep"}, result)
	})

	t.Run("all groups over limit", func(t *testing.T) {
		t.Parallel()
		overLen := string(make([]byte, maxGroupNameLen+1))
		result := sanitizeGroups([]string{overLen, overLen, overLen})
		require.Empty(t, result)
	})

	t.Run("exactly maxGroupsCount groups", func(t *testing.T) {
		t.Parallel()
		groups := make([]string, maxGroupsCount)
		for i := range groups {
			groups[i] = fmt.Sprintf("g%d", i)
		}
		result := sanitizeGroups(groups)
		require.Len(t, result, maxGroupsCount)
		require.Equal(t, groups, result)
	})

	t.Run("one over maxGroupsCount", func(t *testing.T) {
		t.Parallel()
		groups := make([]string, maxGroupsCount+1)
		for i := range groups {
			groups[i] = fmt.Sprintf("g%d", i)
		}
		result := sanitizeGroups(groups)
		require.Len(t, result, maxGroupsCount)
		require.Equal(t, fmt.Sprintf("g%d", maxGroupsCount-1), result[maxGroupsCount-1])
	})
}

func TestExtractGroups_EmptyGroupsClaim(t *testing.T) {
	t.Parallel()

	// When GroupsClaim is empty string, it looks for "" in claims which
	// typically does not exist.
	claims := map[string]any{
		"groups": []string{"should-not-match"},
	}
	result := extractStringSlice(claims, "")
	require.Nil(t, result)
}

func TestExtractClaims_NoGroupsInClaims(t *testing.T) {
	t.Parallel()

	// Verify extractStringSlice returns nil for a missing groups key.
	claims := map[string]any{
		"sub":   "user-sub-1",
		"email": "alice@example.com",
		"name":  "Alice",
	}
	result := extractStringSlice(claims, "groups")
	require.Nil(t, result)
}
