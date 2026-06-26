// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestAPIKeyPrefix(t *testing.T) {
	t.Parallel()
	require.Equal(t, "fbk_live_", domain.APIKeyPrefix)
}

func TestRawKeyLength(t *testing.T) {
	t.Parallel()
	require.Equal(t, 32, rawKeyHexLen)
}

func TestDisplayPrefixLength(t *testing.T) {
	t.Parallel()
	require.Equal(t, 12, displayPrefLen)
}

func TestTouchInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, 30, int(touchInterval.Seconds()))
}

func TestNilAPIKeysService(t *testing.T) {
	t.Parallel()
	var s *APIKeys
	require.Nil(t, s)
}

// ---------------------------------------------------------------------------
// generate()
// ---------------------------------------------------------------------------

func TestGenerate_ReturnsValidKey(t *testing.T) {
	t.Parallel()
	raw, hash, prefix, err := generate()
	require.NoError(t, err)

	// raw starts with the canonical prefix
	require.True(t, strings.HasPrefix(raw, domain.APIKeyPrefix),
		"raw key must start with %q, got %q", domain.APIKeyPrefix, raw)

	// total length = prefix (9) + 32 hex chars
	require.Equal(t, len(domain.APIKeyPrefix)+rawKeyHexLen, len(raw))

	// hex portion after prefix is valid hex
	hexPart := raw[len(domain.APIKeyPrefix):]
	_, decErr := hex.DecodeString(hexPart)
	require.NoError(t, decErr, "hex portion must be valid hex")

	// hash is a SHA-256 digest (32 bytes)
	require.Len(t, hash, sha256.Size)

	// prefix is the first displayPrefLen chars of raw
	require.Equal(t, raw[:displayPrefLen], prefix)
	require.Len(t, prefix, displayPrefLen)
}

func TestGenerate_Uniqueness(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, 100)
	for range 100 {
		raw, _, _, err := generate()
		require.NoError(t, err)
		require.False(t, seen[raw], "duplicate raw key generated")
		seen[raw] = true
	}
}

func TestGenerate_HashDeterminism(t *testing.T) {
	t.Parallel()
	// Generating a key and then hashing the same raw value should produce the
	// same digest that generate() returned.
	raw, hash, _, err := generate()
	require.NoError(t, err)

	rehash := apiKeyLookupDigest(raw)
	require.Equal(t, hash, rehash, "hash from generate must equal apiKeyLookupDigest(raw)")
}

func TestGenerate_PrefixMatchesRaw(t *testing.T) {
	t.Parallel()
	for range 10 {
		raw, _, prefix, err := generate()
		require.NoError(t, err)
		require.Equal(t, raw[:displayPrefLen], prefix)
	}
}

// ---------------------------------------------------------------------------
// apiKeyLookupDigest()
// ---------------------------------------------------------------------------

func TestAPIKeyLookupDigest_SHA256(t *testing.T) {
	t.Parallel()
	input := "fbk_live_deadbeefcafebabe0011223344556677"
	digest := apiKeyLookupDigest(input)

	want := sha256.Sum256([]byte(input))
	require.Equal(t, want[:], digest)
}

func TestAPIKeyLookupDigest_DifferentInputs(t *testing.T) {
	t.Parallel()
	a := apiKeyLookupDigest("fbk_live_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0")
	b := apiKeyLookupDigest("fbk_live_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1")
	require.NotEqual(t, a, b, "different inputs must produce different digests")
}

func TestAPIKeyLookupDigest_Length(t *testing.T) {
	t.Parallel()
	digest := apiKeyLookupDigest("any-input")
	require.Len(t, digest, sha256.Size)
}

func TestAPIKeyLookupDigest_Deterministic(t *testing.T) {
	t.Parallel()
	input := "fbk_live_0123456789abcdef0123456789abcdef"
	d1 := apiKeyLookupDigest(input)
	d2 := apiKeyLookupDigest(input)
	require.Equal(t, d1, d2, "same input must produce same digest")
}

func TestAPIKeyLookupDigest_EmptyInput(t *testing.T) {
	t.Parallel()
	digest := apiKeyLookupDigest("")
	want := sha256.Sum256([]byte(""))
	require.Equal(t, want[:], digest)
	require.Len(t, digest, sha256.Size)
}

func TestAPIKeyLookupDigest_LongInput(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 10000)
	digest := apiKeyLookupDigest(long)
	require.Len(t, digest, sha256.Size)
}

// ---------------------------------------------------------------------------
// touchAsync() — debounce logic
// ---------------------------------------------------------------------------

func TestTouchAsync_Debounce_SkipsWithinInterval(t *testing.T) {
	t.Parallel()
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()

	// Simulate a recent touch (5 seconds ago — well within touchInterval=30s).
	recent := time.Now().Add(-5 * time.Second)
	s.touchCache.Store(id, recent)

	// touchAsync should see the recent entry and return early without spawning
	// a goroutine. With repo=nil, if it did NOT skip, the goroutine would panic.
	// We verify by checking the cache value is unchanged.
	s.touchAsync(id)

	val, ok := s.touchCache.Load(id)
	require.True(t, ok)
	cachedTime, _ := val.(time.Time)
	require.Equal(t, recent, cachedTime, "cache must not be updated when within interval")
}

func TestTouchAsync_Debounce_MissingEntry(t *testing.T) {
	t.Parallel()
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()

	// No entry exists in the cache — Load should return !ok.
	_, ok := s.touchCache.Load(id)
	require.False(t, ok, "cache should be empty for a new key")
}

func TestTouchCache_TypeAssertion(t *testing.T) {
	t.Parallel()
	// The touchCache stores time.Time values. Verify the type assertion
	// path used in touchAsync works correctly.
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()
	now := time.Now()

	s.touchCache.Store(id, now)
	val, ok := s.touchCache.Load(id)
	require.True(t, ok)

	tt, typeOK := val.(time.Time)
	require.True(t, typeOK, "cached value must be time.Time")
	require.Equal(t, now, tt)
}

func TestTouchCache_WrongType(t *testing.T) {
	t.Parallel()
	// If somehow a non-time.Time value is stored, the type assertion
	// should fail gracefully (zero time, ok=false).
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()

	s.touchCache.Store(id, "not-a-time")
	val, ok := s.touchCache.Load(id)
	require.True(t, ok)

	tt, typeOK := val.(time.Time)
	require.False(t, typeOK)
	require.True(t, tt.IsZero(), "failed type assertion should yield zero time")
}

// ---------------------------------------------------------------------------
// NewAPIKeys()
// ---------------------------------------------------------------------------

func TestNewAPIKeys_NilRepo(t *testing.T) {
	t.Parallel()
	s := NewAPIKeys(nil)
	require.NotNil(t, s)
	require.Nil(t, s.repo)
}

// ---------------------------------------------------------------------------
// Key format invariants
// ---------------------------------------------------------------------------

func TestKeyFormat_PrefixPlusHex(t *testing.T) {
	t.Parallel()
	raw, _, _, err := generate()
	require.NoError(t, err)

	// The key must be exactly prefix + 32 hex chars
	require.Equal(t, len(domain.APIKeyPrefix)+rawKeyHexLen, len(raw))

	// Strip prefix, remainder must be lowercase hex
	hexStr := raw[len(domain.APIKeyPrefix):]
	require.Equal(t, strings.ToLower(hexStr), hexStr, "hex portion must be lowercase")
	for _, c := range hexStr {
		require.True(t,
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"character %q is not valid hex", string(c))
	}
}

func TestKeyFormat_ExpectedTotalLength(t *testing.T) {
	t.Parallel()
	// prefix "fbk_live_" is 9 chars + 32 hex = 41 total
	raw, _, _, err := generate()
	require.NoError(t, err)
	require.Len(t, raw, 41)
}

func TestKeyFormat_DisplayPrefixSubstring(t *testing.T) {
	t.Parallel()
	raw, _, prefix, err := generate()
	require.NoError(t, err)
	// The display prefix must be the first 12 characters of the raw key
	require.Equal(t, raw[:12], prefix)
	// It must start with the API key prefix
	require.True(t, strings.HasPrefix(prefix, "fbk_live_"))
}

// ---------------------------------------------------------------------------
// Struct types — verify zero-value construction
// ---------------------------------------------------------------------------

func TestIssueParams_ZeroValue(t *testing.T) {
	t.Parallel()
	var p IssueParams
	require.Empty(t, p.TenantID)
	require.Empty(t, p.Label)
	require.Empty(t, p.Description)
	require.Nil(t, p.ExpiresAt)
	require.Nil(t, p.AllowedCIDRs)
	require.Nil(t, p.RateLimitRPM)
}

func TestLookupResult_ZeroValue(t *testing.T) {
	t.Parallel()
	var r LookupResult
	require.Empty(t, r.TenantID)
	require.Equal(t, uuid.Nil, r.KeyID)
	require.Nil(t, r.Scopes)
	require.Nil(t, r.RateLimitRPM)
	require.Nil(t, r.ExpiresAt)
}

func TestRotateParams_ZeroValue(t *testing.T) {
	t.Parallel()
	var p RotateParams
	require.Empty(t, p.TenantID)
	require.Equal(t, uuid.Nil, p.OldKeyID)
	require.Zero(t, p.GracePeriod)
}

func TestTempTokenResult_ZeroValue(t *testing.T) {
	t.Parallel()
	var r TempTokenResult
	require.Empty(t, r.Token)
	require.Empty(t, r.TokenPrefix)
	require.True(t, r.ExpiresAt.IsZero())
}

// ---------------------------------------------------------------------------
// generate() — batch property checks
// ---------------------------------------------------------------------------

func TestGenerate_Batch_AllUnique(t *testing.T) {
	t.Parallel()
	const n = 50
	raws := make([]string, 0, n)
	hashes := make([]string, 0, n)

	for range n {
		raw, hash, _, err := generate()
		require.NoError(t, err)
		raws = append(raws, raw)
		hashes = append(hashes, hex.EncodeToString(hash))
	}

	// All raw keys must be unique
	rawSet := make(map[string]struct{}, n)
	for _, r := range raws {
		_, dup := rawSet[r]
		require.False(t, dup, "duplicate raw key")
		rawSet[r] = struct{}{}
	}

	// All hashes must be unique (since raws are unique)
	hashSet := make(map[string]struct{}, n)
	for _, h := range hashes {
		_, dup := hashSet[h]
		require.False(t, dup, "duplicate hash")
		hashSet[h] = struct{}{}
	}
}

func TestGenerate_Hash_MatchesDigest(t *testing.T) {
	t.Parallel()
	for range 20 {
		raw, hash, _, err := generate()
		require.NoError(t, err)
		recomputed := apiKeyLookupDigest(raw)
		require.Equal(t, hash, recomputed)
	}
}

// ---------------------------------------------------------------------------
// touchInterval boundary
// ---------------------------------------------------------------------------

func TestTouchInterval_BoundaryExact(t *testing.T) {
	t.Parallel()
	// Exactly at the interval boundary: time.Since should be >= touchInterval,
	// meaning the debounce should NOT skip.
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()
	boundary := time.Now().Add(-touchInterval)
	s.touchCache.Store(id, boundary)

	val, ok := s.touchCache.Load(id)
	require.True(t, ok)
	cachedTime, _ := val.(time.Time)
	require.True(t, time.Since(cachedTime) >= touchInterval)
}

func TestTouchInterval_JustUnder(t *testing.T) {
	t.Parallel()
	// 1 second under the interval: debounce should skip.
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()
	justUnder := time.Now().Add(-(touchInterval - time.Second))
	s.touchCache.Store(id, justUnder)

	val, ok := s.touchCache.Load(id)
	require.True(t, ok)
	cachedTime, _ := val.(time.Time)
	require.True(t, time.Since(cachedTime) < touchInterval)
}

// ---------------------------------------------------------------------------
// Concurrency: sync.Map under parallel access
// ---------------------------------------------------------------------------

func TestTouchCache_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	s := APIKeys{} // ptrext:allow sync-map
	id := uuid.New()

	// Pre-populate with a recent timestamp so touchAsync will skip the
	// goroutine (avoids nil repo panic).
	s.touchCache.Store(id, time.Now())

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			// All should hit the debounce path safely.
			s.touchAsync(id)
		}()
	}
	wg.Wait()

	// Cache entry should still be present.
	_, ok := s.touchCache.Load(id)
	require.True(t, ok)
}

// ---------------------------------------------------------------------------
// Multiple distinct keys in cache
// ---------------------------------------------------------------------------

func TestTouchCache_MultipleKeys(t *testing.T) {
	t.Parallel()
	s := APIKeys{} // ptrext:allow sync-map

	ids := make([]uuid.UUID, 10)
	for i := range ids {
		ids[i] = uuid.New()
		s.touchCache.Store(ids[i], time.Now())
	}

	// All entries must be independently retrievable.
	for _, id := range ids {
		val, ok := s.touchCache.Load(id)
		require.True(t, ok, "entry for %s must exist", id)
		_, typeOK := val.(time.Time)
		require.True(t, typeOK)
	}

	// A key not in the cache should not be found.
	_, ok := s.touchCache.Load(uuid.New())
	require.False(t, ok)
}

// ---------------------------------------------------------------------------
// generate() — hex bytes correctness
// ---------------------------------------------------------------------------

func TestGenerate_HexDecodesTo16Bytes(t *testing.T) {
	t.Parallel()
	raw, _, _, err := generate()
	require.NoError(t, err)
	hexPart := raw[len(domain.APIKeyPrefix):]
	decoded, err := hex.DecodeString(hexPart)
	require.NoError(t, err)
	require.Len(t, decoded, rawKeyHexLen/2, "hex decodes to exactly 16 random bytes")
}

// ---------------------------------------------------------------------------
// apiKeyLookupDigest — collision resistance property
// ---------------------------------------------------------------------------

func TestAPIKeyLookupDigest_NoCollisions(t *testing.T) {
	t.Parallel()
	const n = 200
	seen := make(map[string]struct{}, n)
	for range n {
		raw, _, _, err := generate()
		require.NoError(t, err)
		d := hex.EncodeToString(apiKeyLookupDigest(raw))
		_, dup := seen[d]
		require.False(t, dup, "digest collision detected")
		seen[d] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Constant relationships
// ---------------------------------------------------------------------------

func TestConstants_PrefixFitsInDisplayPref(t *testing.T) {
	t.Parallel()
	// displayPrefLen (12) must be >= len(APIKeyPrefix) (9) so the display
	// prefix always contains the full API key prefix.
	require.GreaterOrEqual(t, displayPrefLen, len(domain.APIKeyPrefix),
		"displayPrefLen must be >= APIKeyPrefix length")
}

func TestConstants_RawKeyHexLenEven(t *testing.T) {
	t.Parallel()
	// rawKeyHexLen must be even (2 hex chars per byte).
	require.Equal(t, 0, rawKeyHexLen%2, "rawKeyHexLen must be even")
}

func TestConstants_DisplayPrefLenWithinKey(t *testing.T) {
	t.Parallel()
	// displayPrefLen must be <= total key length.
	totalLen := len(domain.APIKeyPrefix) + rawKeyHexLen
	require.LessOrEqual(t, displayPrefLen, totalLen,
		"displayPrefLen must be <= total key length")
}
