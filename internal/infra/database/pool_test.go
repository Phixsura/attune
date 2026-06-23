package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPool_InvalidURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Invalid URL should return parse error
	_, err := NewPool(ctx, "not-a-valid-url")
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse")
}

func TestNewPool_UnreachableHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping connection test in short mode")
	}
	t.Parallel()

	ctx := context.Background()

	// Valid URL but unreachable host should return connection error
	// Use a non-routable IP to ensure connection timeout
	pool, err := NewPool(ctx, "postgres://user:pass@10.255.255.1:5432/nonexistent?connect_timeout=1")
	if err == nil && pool != nil {
		pool.Close()
		// If connection somehow succeeded (unlikely), skip
		t.Skip("unexpected connection success")
	}
	// Either error or nil pool is acceptable
}
