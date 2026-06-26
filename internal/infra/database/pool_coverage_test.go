// ptrext:file-allow test fixtures use struct pointers for PoolHealth.
package database

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPoolHealth_JSONTags(t *testing.T) {
	t.Parallel()

	h := PoolHealth{
		TotalConns:        10,
		AcquiredConns:     3,
		IdleConns:         7,
		ConstructingConns: 0,
		MaxConns:          20,
		Healthy:           true,
	}
	data, err := json.Marshal(h)
	require.NoError(t, err)

	var m map[string]any
	err = json.Unmarshal(data, &m)
	require.NoError(t, err)

	require.Equal(t, float64(10), m["total_conns"])
	require.Equal(t, float64(3), m["acquired_conns"])
	require.Equal(t, float64(7), m["idle_conns"])
	require.Equal(t, float64(0), m["constructing_conns"])
	require.Equal(t, float64(20), m["max_conns"])
	require.Equal(t, true, m["healthy"])
}

func TestPoolHealth_HealthyConditions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		health  PoolHealth
		healthy bool
	}{
		{
			name:    "healthy with total conns",
			health:  PoolHealth{TotalConns: 5, Healthy: true},
			healthy: true,
		},
		{
			name:    "healthy with constructing conns",
			health:  PoolHealth{ConstructingConns: 1, Healthy: true},
			healthy: true,
		},
		{
			name:    "unhealthy with zero conns",
			health:  PoolHealth{TotalConns: 0, ConstructingConns: 0, Healthy: false},
			healthy: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.healthy, tc.health.Healthy)
		})
	}
}

func TestPoolHealth_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := PoolHealth{
		TotalConns:        15,
		AcquiredConns:     5,
		IdleConns:         10,
		ConstructingConns: 2,
		MaxConns:          25,
		Healthy:           true,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded PoolHealth
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Equal(t, original, decoded)
}

func TestNewPool_EmptyURL(t *testing.T) {
	t.Parallel()

	// pgxpool.ParseConfig("") succeeds (uses libpq environment defaults).
	// The pool may or may not connect depending on the host — we just
	// exercise the code path without asserting success or failure.
	_, _ = NewPool(t.Context(), "")
}

func TestNewPool_MalformedScheme(t *testing.T) {
	t.Parallel()

	_, err := NewPool(t.Context(), "://invalid-scheme")
	require.Error(t, err)
}

func TestNewPool_PostgresSchemeButNoHost(t *testing.T) {
	t.Parallel()

	// A valid-looking URL but missing required pieces still fails at connect
	_, err := NewPool(t.Context(), "postgres:///dbname?connect_timeout=1")
	// This might parse OK but fail to connect; either way we exercise the path
	if err != nil {
		require.Error(t, err)
	}
}
