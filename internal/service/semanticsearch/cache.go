// SPDX-License-Identifier: Apache-2.0

package semanticsearch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// pgCache implements EmbeddingCache using PostgreSQL.
// Uses the query_embedding_cache table from migration 031.
type pgCache struct {
	pool *pgxpool.Pool
}

// NewPGCache creates a new PostgreSQL-backed embedding cache.
func NewPGCache(pool *pgxpool.Pool) EmbeddingCache {
	return ptrext.Of(pgCache{pool: pool})
}

// Get retrieves a cached embedding by query hash.
func (c *pgCache) Get(ctx context.Context, tenantID, queryHash string) ([]float32, bool, error) {
	var embStr *string
	err := c.pool.QueryRow(
		ctx, `
		SELECT embedding::text
		FROM query_embedding_cache
		WHERE tenant_id = $1 AND query_hash = $2 AND expires_at > NOW()`,
		tenantID, queryHash,
	).Scan(&embStr) // ptrext:allow scan-target

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache get: %w", err)
	}

	embStrVal := ptrext.Indirect(embStr)
	if embStrVal == "" {
		return nil, false, nil
	}

	embedding, err := parseVecString(embStrVal)
	if err != nil {
		return nil, false, fmt.Errorf("parse cached embedding: %w", err)
	}

	return embedding, true, nil
}

// Set stores an embedding with TTL.
func (c *pgCache) Set(ctx context.Context, tenantID, queryHash string, embedding []float32, ttl time.Duration) error {
	embStr := vecToString(embedding)
	expiresAt := time.Now().Add(ttl)

	_, err := c.pool.Exec(
		ctx, `
		INSERT INTO query_embedding_cache (tenant_id, query_hash, embedding, expires_at)
		VALUES ($1, $2, $3::vector, $4)
		ON CONFLICT (tenant_id, query_hash)
		DO UPDATE SET embedding = EXCLUDED.embedding, expires_at = EXCLUDED.expires_at`,
		tenantID, queryHash, embStr, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

// vecToString converts a []float32 to pgvector's text format: "[1.0,2.0,3.0]".
func vecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// parseVecString parses pgvector's text format "[1.0,2.0,3.0]" to []float32.
func parseVecString(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil, nil
	}
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("invalid vector format: %s", s)
	}
	s = s[1 : len(s)-1] // Remove brackets.
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	result := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("invalid float at index %d: %w", i, err)
		}
		result[i] = float32(f)
	}
	return result, nil
}

// memoryCache implements EmbeddingCache in-memory for testing.
type memoryCache struct {
	entries map[string]memoryCacheEntry
}

type memoryCacheEntry struct {
	embedding []float32
	expiresAt time.Time
}

// NewMemoryCache creates a new in-memory embedding cache (for testing).
func NewMemoryCache() EmbeddingCache {
	return ptrext.Of(memoryCache{
		entries: make(map[string]memoryCacheEntry),
	})
}

func (c *memoryCache) Get(ctx context.Context, tenantID, queryHash string) ([]float32, bool, error) {
	key := tenantID + ":" + queryHash
	entry, ok := c.entries[key]
	if !ok {
		return nil, false, nil
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false, nil
	}
	return entry.embedding, true, nil
}

func (c *memoryCache) Set(ctx context.Context, tenantID, queryHash string, embedding []float32, ttl time.Duration) error {
	key := tenantID + ":" + queryHash
	c.entries[key] = memoryCacheEntry{
		embedding: embedding,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}
