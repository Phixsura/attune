package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

const (
	rawKeyHexLen   = 32 // 16 random bytes -> 32 hex chars (after prefix)
	displayPrefLen = 12 // chars shown for identification ("fbk_live_abc")
)

// APIKeys issues and verifies external API keys backed by the
// external_api_keys table. Keys are random 128-bit strings stored as
// sha256 hashes; the raw value is shown to the operator exactly once
// at issuance.
type APIKeys struct {
	repo *apikeyrepo.APIKeyRepo
	// touchCache debounces TouchLastUsed: if a key was touched within the
	// last touchInterval we skip the goroutine entirely. Prevents unbounded
	// fan-out under load (many concurrent requests authenticating with the
	// same key would otherwise fire N goroutines/sec, each holding a
	// pgxpool connection).
	touchCache sync.Map // map[uuid.UUID]time.Time (last touch)
}

const touchInterval = 30 * time.Second

func NewAPIKeys(r *apikeyrepo.APIKeyRepo) *APIKeys {
	return &APIKeys{repo: r}
}

// Issue mints a key for the given tenant and returns the raw value
// (caller must surface it once). tenantID is the TEXT id returned by
// TenantRepo.ResolveSlug or Create (UUID stored as text — see
// migration 001).
func (s *APIKeys) Issue(ctx context.Context, tenantID, label string) (raw string, keyID uuid.UUID, err error) {
	const where = "service.APIKeys.Issue"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s", where, tenantID, label)
	raw, hash, prefix, err := generate()
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}
	keyID, err = s.repo.Insert(ctx, tenantID, hash, prefix, label)
	if err != nil {
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return "", uuid.Nil, err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s,prefix:%s",
		where, tenantID, keyID, prefix)
	return raw, keyID, nil
}

// List returns every key (active + revoked) for tenantID, newest first.
func (s *APIKeys) List(ctx context.Context, tenantID string) ([]apikeyrepo.APIKeyListRow, error) {
	return s.repo.ListByTenant(ctx, tenantID)
}

// Revoke soft-deletes the key. Pass-through to repo so the tenant_id
// scope check happens at the SQL boundary.
func (s *APIKeys) Revoke(ctx context.Context, tenantID string, id uuid.UUID) error {
	return s.repo.Revoke(ctx, tenantID, id)
}

// Lookup verifies the raw key, returning its tenant and key ids on
// success. Updates last_used_at asynchronously to avoid blocking the
// request. Returns domain.ErrInvalidAPIKey on every recoverable failure
// (wrong length, hash mismatch, no row, revoked); only unexpected DB
// errors propagate as-is so middleware can map them to 500.
func (s *APIKeys) Lookup(ctx context.Context, raw string) (tenantID string, keyID uuid.UUID, err error) {
	const where = "service.APIKeys.Lookup"
	if len(raw) != len(domain.APIKeyPrefix)+rawKeyHexLen {
		logext.Warnf(ctx, "[%s] reject: bad key length,len:%d", where, len(raw))
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	sum := sha256.Sum256([]byte(raw))
	row, err := s.repo.LookupByHash(ctx, sum[:])
	if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
		logext.Warnf(ctx, "[%s] reject: hash not found", where)
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] apikey.LookupByHash failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}
	if !hmac.Equal(row.StoredHash, sum[:]) {
		logext.Warnf(ctx, "[%s] reject: hmac mismatch,key_id:%s", where, row.ID)
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	s.touchAsync(row.ID)
	return row.TenantID, row.ID, nil
}

// touchAsync debounces s.repo.TouchLastUsed: skips the goroutine if this
// key was touched within touchInterval (30s). Trades small accuracy on
// last_used_at for bounded fan-out under heavy auth load.
func (s *APIKeys) touchAsync(id uuid.UUID) {
	now := time.Now()
	if last, ok := s.touchCache.Load(id); ok {
		if t, _ := last.(time.Time); now.Sub(t) < touchInterval {
			return
		}
	}
	s.touchCache.Store(id, now)
	go s.repo.TouchLastUsed(id)
}

// generate is the random-key + hash + display-prefix construction.
// Lives here rather than in repo because the secret value never leaves
// service — repo only sees the hash.
func generate() (raw string, hash []byte, prefix string, err error) {
	buf := make([]byte, rawKeyHexLen/2)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, "", fmt.Errorf("rand: %w", err)
	}
	raw = domain.APIKeyPrefix + hex.EncodeToString(buf)
	// SHA-256 is appropriate here: we are hashing API keys (not user passwords)
	// for lookup verification. The raw key is a 192-bit random value —
	// preimage resistance of SHA-256 is sufficient; we are not defending
	// against offline brute-force of low-entropy secrets.
	sum := sha256.Sum256([]byte(raw))
	hash = sum[:]
	if len(raw) >= displayPrefLen {
		prefix = raw[:displayPrefLen]
	} else {
		prefix = raw
	}
	return raw, hash, prefix, nil
}
