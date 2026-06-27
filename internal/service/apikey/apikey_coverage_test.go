// ptrext:file-allow test-fixtures

// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// ---------------------------------------------------------------------------
// 1. generate() — random key construction
// ---------------------------------------------------------------------------

func TestGenerate_OutputFormat(t *testing.T) {
	t.Parallel()

	t.Run("no error", func(t *testing.T) {
		t.Parallel()
		raw, hash, prefix, err := generate()
		require.NoError(t, err)
		require.NotEmpty(t, raw)
		require.NotEmpty(t, hash)
		require.NotEmpty(t, prefix)
	})

	t.Run("raw starts with prefix", func(t *testing.T) {
		t.Parallel()
		raw, _, _, err := generate()
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(raw, domain.APIKeyPrefix),
			"raw key should start with %q, got %q", domain.APIKeyPrefix, raw)
	})

	t.Run("raw has correct total length", func(t *testing.T) {
		t.Parallel()
		raw, _, _, err := generate()
		require.NoError(t, err)
		// prefix (9 chars "fbk_live_") + 32 hex chars = 41
		expectedLen := len(domain.APIKeyPrefix) + rawKeyHexLen
		require.Len(t, raw, expectedLen)
	})

	t.Run("hex portion is valid lowercase hex", func(t *testing.T) {
		t.Parallel()
		raw, _, _, err := generate()
		require.NoError(t, err)
		hexPart := raw[len(domain.APIKeyPrefix):]
		_, decErr := hex.DecodeString(hexPart)
		require.NoError(t, decErr, "hex portion %q should be valid hex", hexPart)
		require.Equal(t, strings.ToLower(hexPart), hexPart,
			"hex portion should be lowercase")
	})

	t.Run("hash is 32 bytes (SHA-256)", func(t *testing.T) {
		t.Parallel()
		_, hash, _, err := generate()
		require.NoError(t, err)
		require.Len(t, hash, sha256.Size)
	})

	t.Run("hash matches apiKeyLookupDigest of raw", func(t *testing.T) {
		t.Parallel()
		raw, hash, _, err := generate()
		require.NoError(t, err)
		recomputed := apiKeyLookupDigest(raw)
		require.Equal(t, recomputed, hash)
	})

	t.Run("prefix is displayPrefLen chars", func(t *testing.T) {
		t.Parallel()
		_, _, prefix, err := generate()
		require.NoError(t, err)
		require.Len(t, prefix, displayPrefLen)
	})

	t.Run("prefix is first displayPrefLen chars of raw", func(t *testing.T) {
		t.Parallel()
		raw, _, prefix, err := generate()
		require.NoError(t, err)
		require.Equal(t, raw[:displayPrefLen], prefix)
	})

	t.Run("prefix starts with API key prefix", func(t *testing.T) {
		t.Parallel()
		_, _, prefix, err := generate()
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(prefix, domain.APIKeyPrefix),
			"display prefix should start with %q", domain.APIKeyPrefix)
	})
}

// TestGenerate_Uniqueness, TestAPIKeyLookupDigest_* are in service_test.go.

// ---------------------------------------------------------------------------
// 3. UpdateEnvironment — validation before repo call
// ---------------------------------------------------------------------------

func TestUpdateEnvironment_InvalidEnvironment(t *testing.T) {
	t.Parallel()

	// With a nil repo, only the validation path is exercised.
	// Invalid env returns before any repo call, so nil repo is safe.
	svc := ptrext.Of(APIKeys{})
	ctx := context.Background()
	keyID := uuid.New()

	cases := []struct {
		name string
		env  string
	}{
		{name: "empty string", env: ""},
		{name: "random string", env: "foobar"},
		{name: "uppercase valid", env: "Production"},
		{name: "with spaces", env: " production"},
		{name: "prod shorthand", env: "prod"},
		{name: "dev shorthand", env: "dev"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := svc.UpdateEnvironment(ctx, "tenant-1", keyID, tc.env)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid environment")
		})
	}
}

func TestUpdateEnvironment_ValidEnvironments(t *testing.T) {
	t.Parallel()

	validEnvs := []string{"production", "staging", "development", "test"}
	for _, env := range validEnvs {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			ok := domain.IsValidEnvironment(env)
			require.True(t, ok, "%q should be a valid environment", env)
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Lookup — length validation before repo
// ---------------------------------------------------------------------------

func TestLookup_BadLength(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(APIKeys{})
	ctx := context.Background()

	validLen := len(domain.APIKeyPrefix) + rawKeyHexLen

	cases := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "too short", raw: "fbk_live_abc"},
		{name: "one char short", raw: strings.Repeat("x", validLen-1)},
		{name: "one char long", raw: strings.Repeat("x", validLen+1)},
		{name: "much too long", raw: strings.Repeat("x", validLen*2)},
		{name: "prefix only", raw: domain.APIKeyPrefix},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := svc.Lookup(ctx, tc.raw)
			require.ErrorIs(t, err, domain.ErrInvalidAPIKey)
		})
	}
}

// ---------------------------------------------------------------------------
// 5. LookupWithScopesAndIP — length validation before repo
// ---------------------------------------------------------------------------

func TestLookupWithScopesAndIP_BadLength(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(APIKeys{})
	ctx := context.Background()

	cases := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "too short by half", raw: "fbk_live_0123456789abcdef"},
		{name: "way too long", raw: domain.APIKeyPrefix + strings.Repeat("a", rawKeyHexLen+10)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, _, _, err := svc.LookupWithScopesAndIP(ctx, tc.raw, "127.0.0.1")
			require.ErrorIs(t, err, domain.ErrInvalidAPIKey)
		})
	}
}

// ---------------------------------------------------------------------------
// 6. LookupFull — length validation before repo
// ---------------------------------------------------------------------------

func TestLookupFull_BadLength(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(APIKeys{})
	ctx := context.Background()

	cases := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "short", raw: "fbk_live_short"},
		{name: "one too many", raw: domain.APIKeyPrefix + strings.Repeat("f", rawKeyHexLen+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := svc.LookupFull(ctx, tc.raw, "10.0.0.1")
			require.ErrorIs(t, err, domain.ErrInvalidAPIKey)
			require.Nil(t, result)
		})
	}
}

// ---------------------------------------------------------------------------
// 7. NewAPIKeys — constructor
// ---------------------------------------------------------------------------

func TestNewAPIKeys_NonNil(t *testing.T) {
	t.Parallel()

	svc := NewAPIKeys(nil)
	require.NotNil(t, svc, "constructor should return non-nil")
}

func TestNewAPIKeys_RepoFieldSet(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(apikeyrepo.APIKeyRepo{})
	svc := NewAPIKeys(repo)
	require.NotNil(t, svc)
	require.Equal(t, repo, svc.repo, "repo field should reference the passed-in repo")
}

// ---------------------------------------------------------------------------
// 8. IssueParams — struct fields
// ---------------------------------------------------------------------------

func TestIssueParams_Fields(t *testing.T) {
	t.Parallel()

	now := time.Now()
	p := IssueParams{
		TenantID:     "tenant-42",
		Label:        "my-key",
		Description:  "integration key",
		ExpiresAt:    ptrext.Of(now),
		AllowedCIDRs: []string{"10.0.0.0/8", "192.168.1.0/24"},
		RateLimitRPM: ptrext.Of(1000),
	}

	require.Equal(t, "tenant-42", p.TenantID)
	require.Equal(t, "my-key", p.Label)
	require.Equal(t, "integration key", p.Description)
	require.NotNil(t, p.ExpiresAt)
	require.Equal(t, now, ptrext.Indirect(p.ExpiresAt))
	require.Len(t, p.AllowedCIDRs, 2)
	require.Equal(t, 1000, ptrext.Indirect(p.RateLimitRPM))
}

func TestIssueParams_Defaults(t *testing.T) {
	t.Parallel()

	var p IssueParams
	require.Empty(t, p.TenantID)
	require.Empty(t, p.Label)
	require.Empty(t, p.Description)
	require.Nil(t, p.ExpiresAt)
	require.Nil(t, p.AllowedCIDRs)
	require.Nil(t, p.RateLimitRPM)
}

// ---------------------------------------------------------------------------
// 9. RotateParams — struct fields
// ---------------------------------------------------------------------------

func TestRotateParams_Fields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := RotateParams{
		TenantID:    "tenant-1",
		OldKeyID:    id,
		GracePeriod: 24 * time.Hour,
	}

	require.Equal(t, "tenant-1", p.TenantID)
	require.Equal(t, id, p.OldKeyID)
	require.Equal(t, 24*time.Hour, p.GracePeriod)
}

// ---------------------------------------------------------------------------
// 10. LookupResult — struct fields
// ---------------------------------------------------------------------------

func TestLookupResult_Fields(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	now := time.Now()
	r := LookupResult{
		TenantID:     "t-1",
		KeyID:        keyID,
		Scopes:       []domain.Scope{"read", "write"},
		RateLimitRPM: ptrext.Of(500),
		ExpiresAt:    ptrext.Of(now),
	}

	require.Equal(t, "t-1", r.TenantID)
	require.Equal(t, keyID, r.KeyID)
	require.Len(t, r.Scopes, 2)
	require.Equal(t, domain.Scope("read"), r.Scopes[0])
	require.Equal(t, domain.Scope("write"), r.Scopes[1])
	require.Equal(t, 500, ptrext.Indirect(r.RateLimitRPM))
	require.Equal(t, now, ptrext.Indirect(r.ExpiresAt))
}

func TestLookupResult_NilOptionalFields(t *testing.T) {
	t.Parallel()

	r := LookupResult{
		TenantID: "t-2",
		KeyID:    uuid.New(),
	}

	require.Nil(t, r.Scopes)
	require.Nil(t, r.RateLimitRPM)
	require.Nil(t, r.ExpiresAt)
}

// ---------------------------------------------------------------------------
// 11. TempTokenResult — struct fields
// ---------------------------------------------------------------------------

func TestTempTokenResult_Fields(t *testing.T) {
	t.Parallel()

	now := time.Now()
	r := TempTokenResult{
		Token:       "fbk_live_abcdef0123456789abcdef0123456789ab",
		TokenPrefix: "fbk_live_abc",
		ExpiresAt:   now,
	}

	require.Equal(t, "fbk_live_abcdef0123456789abcdef0123456789ab", r.Token)
	require.Equal(t, "fbk_live_abc", r.TokenPrefix)
	require.Equal(t, now, r.ExpiresAt)
}

// ---------------------------------------------------------------------------
// 12. Package constants — sanity checks for values used by generate()
// ---------------------------------------------------------------------------

func TestConstantsRelationship(t *testing.T) {
	t.Parallel()

	t.Run("displayPrefLen fits inside raw key", func(t *testing.T) {
		t.Parallel()
		totalLen := len(domain.APIKeyPrefix) + rawKeyHexLen
		require.Less(t, displayPrefLen, totalLen,
			"displayPrefLen (%d) should be less than total key length (%d)",
			displayPrefLen, totalLen)
	})

	t.Run("rawKeyHexLen is even", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 0, rawKeyHexLen%2,
			"rawKeyHexLen should be even (it is the hex encoding of N bytes)")
	})

	t.Run("touchInterval is positive", func(t *testing.T) {
		t.Parallel()
		require.Greater(t, touchInterval, time.Duration(0))
	})
}

// ---------------------------------------------------------------------------
// 13. generate() round-trip: hash can be recomputed from raw
// ---------------------------------------------------------------------------

func TestGenerate_RoundTrip(t *testing.T) {
	t.Parallel()

	raw, hash, _, err := generate()
	require.NoError(t, err)

	// The hash returned by generate() should be the same as hashing the raw key
	// through apiKeyLookupDigest.
	recomputed := apiKeyLookupDigest(raw)
	require.Equal(t, hash, recomputed, "hash should round-trip through apiKeyLookupDigest")
}

// ---------------------------------------------------------------------------
// 14. LookupWithScopes — delegates to LookupWithScopesAndIP, same validation
// ---------------------------------------------------------------------------

func TestLookupWithScopes_BadLength(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(APIKeys{})
	ctx := context.Background()

	_, _, _, err := svc.LookupWithScopes(ctx, "too_short")
	require.ErrorIs(t, err, domain.ErrInvalidAPIKey)
}
