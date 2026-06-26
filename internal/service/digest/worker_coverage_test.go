// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package digest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/digestsubscription"
)

// mockEmbedReader implements embedQueueReader for unit tests.
type mockEmbedReader struct {
	depth int64
	err   error
}

func (m *mockEmbedReader) QueueDepth(_ context.Context, _ string) (int64, error) {
	return m.depth, m.err
}

// ---------------------------------------------------------------------------
// NewWorker constructor
// ---------------------------------------------------------------------------

func TestCoverage_NewWorker_NilDeps(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, nil, "")
	require.NotNil(t, w, "NewWorker with all-nil deps must return non-nil Worker")
}

func TestCoverage_NewWorker_Defaults(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, nil, "")

	require.Equal(t, 60*time.Second, w.pollInterval, "default pollInterval")
	require.Equal(t, 5*time.Minute, w.staleDuration, "default staleDuration")
	require.Equal(t, 2*time.Hour, w.graceWindow, "default graceWindow")
	require.Equal(t, 5, w.maxAttempts, "default maxAttempts")
	require.Equal(t, 20, w.drainBatch, "default drainBatch")
}

func TestCoverage_NewWorker_OwnerPrefix(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil, nil, nil, "")
	require.True(t, strings.HasPrefix(w.owner, "digest-"), "owner must start with digest-, got %s", w.owner)
}

func TestCoverage_NewWorker_UniqueOwner(t *testing.T) {
	t.Parallel()
	w1 := NewWorker(nil, nil, nil, nil, nil, nil, "")
	w2 := NewWorker(nil, nil, nil, nil, nil, nil, "")
	require.NotEqual(t, w1.owner, w2.owner, "two workers must have different owners")
}

func TestCoverage_NewWorker_DeepLinkBase(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		require.Empty(t, w.deepLinkBase)
	})

	t.Run("set", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "https://console.example.com")
		require.Equal(t, "https://console.example.com", w.deepLinkBase)
	})
}

// ---------------------------------------------------------------------------
// Configure
// ---------------------------------------------------------------------------

func TestCoverage_Configure(t *testing.T) {
	t.Parallel()

	t.Run("both positive", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(10*time.Second, 3)
		require.Equal(t, 10*time.Second, w.pollInterval)
		require.Equal(t, 3, w.maxAttempts)
	})

	t.Run("zero keeps defaults", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(0, 0)
		require.Equal(t, 60*time.Second, w.pollInterval)
		require.Equal(t, 5, w.maxAttempts)
	})

	t.Run("negative keeps defaults", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(-1*time.Second, -3)
		require.Equal(t, 60*time.Second, w.pollInterval)
		require.Equal(t, 5, w.maxAttempts)
	})

	t.Run("partial pollInterval only", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(30*time.Second, 0)
		require.Equal(t, 30*time.Second, w.pollInterval)
		require.Equal(t, 5, w.maxAttempts, "maxAttempts unchanged")
	})

	t.Run("partial maxAttempts only", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.Configure(0, 10)
		require.Equal(t, 60*time.Second, w.pollInterval, "pollInterval unchanged")
		require.Equal(t, 10, w.maxAttempts)
	})
}

// ---------------------------------------------------------------------------
// deferForBacklog
// ---------------------------------------------------------------------------

func TestCoverage_DeferForBacklog(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	ctx := context.Background()

	t.Run("embed nil", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		// w.embed is nil by default when passed nil
		sub := digestsubscription.DueSubscription{
			Subscription: digestsubscription.Subscription{
				TenantID:  "t1",
				NextRunAt: ptrext.Of(now.Add(-10 * time.Minute)),
			},
			ResolvedTimezone: "UTC",
		}
		require.False(t, w.deferForBacklog(ctx, now, sub))
	})

	t.Run("NextRunAt nil", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.embed = ptrext.Of(mockEmbedReader{depth: 5})
		sub := digestsubscription.DueSubscription{
			Subscription: digestsubscription.Subscription{
				TenantID:  "t1",
				NextRunAt: nil,
			},
			ResolvedTimezone: "UTC",
		}
		require.False(t, w.deferForBacklog(ctx, now, sub))
	})

	t.Run("grace expired", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.embed = ptrext.Of(mockEmbedReader{depth: 5})
		// NextRunAt is 3 hours ago; grace window is 2h, so expired.
		sub := digestsubscription.DueSubscription{
			Subscription: digestsubscription.Subscription{
				TenantID:  "t1",
				NextRunAt: ptrext.Of(now.Add(-3 * time.Hour)),
			},
			ResolvedTimezone: "UTC",
		}
		require.False(t, w.deferForBacklog(ctx, now, sub))
	})

	t.Run("depth zero within grace", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.embed = ptrext.Of(mockEmbedReader{depth: 0})
		sub := digestsubscription.DueSubscription{
			Subscription: digestsubscription.Subscription{
				TenantID:  "t1",
				NextRunAt: ptrext.Of(now.Add(-10 * time.Minute)),
			},
			ResolvedTimezone: "UTC",
		}
		require.False(t, w.deferForBacklog(ctx, now, sub))
	})

	t.Run("depth positive within grace", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.embed = ptrext.Of(mockEmbedReader{depth: 42})
		sub := digestsubscription.DueSubscription{
			Subscription: digestsubscription.Subscription{
				TenantID:  "t1",
				NextRunAt: ptrext.Of(now.Add(-10 * time.Minute)),
			},
			ResolvedTimezone: "UTC",
		}
		require.True(t, w.deferForBacklog(ctx, now, sub))
	})

	t.Run("QueueDepth error", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, nil, nil, nil, "")
		w.embed = ptrext.Of(mockEmbedReader{depth: 0, err: errors.New("db down")})
		sub := digestsubscription.DueSubscription{
			Subscription: digestsubscription.Subscription{
				TenantID:  "t1",
				NextRunAt: ptrext.Of(now.Add(-10 * time.Minute)),
			},
			ResolvedTimezone: "UTC",
		}
		require.False(t, w.deferForBacklog(ctx, now, sub))
	})
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestCoverage_Constants(t *testing.T) {
	t.Parallel()

	t.Run("heartbeatInterval", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 90*time.Second, heartbeatInterval)
	})

	t.Run("drainTimeout", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 30*time.Second, drainTimeout)
	})
}
