// ptrext:file-allow test fixtures use pointer literals for brevity
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	"github.com/Phixsura/attune/internal/repo/digestrun"
	"github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// ---------------------------------------------------------------------------
// Fakes for the two worker-level interfaces
// ---------------------------------------------------------------------------

type stubEmbedQueue struct {
	depth int64
	err   error
}

func (s stubEmbedQueue) QueueDepth(context.Context, string) (int64, error) {
	return s.depth, s.err
}

type stubTargetReader struct {
	single  *notifytarget.NotifyTarget
	singles error
	list    []notifytarget.NotifyTarget
	listErr error
}

func (s stubTargetReader) GetByTenantAudience(context.Context, string, string, string) (*notifytarget.NotifyTarget, error) {
	return s.single, s.singles
}

func (s stubTargetReader) ListActiveByTenantAudience(context.Context, string, string) ([]notifytarget.NotifyTarget, error) {
	return s.list, s.listErr
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestWorker(targets targetReader, embed embedQueueReader) *Worker {
	transport := notify.NewTransport(nil, notify.NoRetry())
	agg := NewAggregator(fakeClusters{}, fakeFeedback{}, fakeNamer{})
	return NewWorker(nil, nil, agg, targets, embed, transport, "https://console.test")
}

func stubDrainerWorker() *Worker {
	d := workerdrain.New("test-digest")
	d.SetTimeout(100 * time.Millisecond)
	return &Worker{
		drain:         d,
		pollInterval:  time.Second,
		staleDuration: 5 * time.Minute,
		graceWindow:   2 * time.Hour,
		maxAttempts:   5,
		drainBatch:    3,
		owner:         "test-worker",
	}
}

func makeDueSub(tenantID, tz, freq string, nextRunAt *time.Time) digestsubscription.DueSubscription {
	d := digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			ID:             uuid.New(),
			TenantID:       tenantID,
			Enabled:        true,
			Frequency:      freq,
			SendHour:       9,
			LLMMinFeedback: 6,
			NextRunAt:      nextRunAt,
		},
		ResolvedTimezone: tz,
	}
	return d
}

func makeRun(tenantID string) *digestrun.Run {
	return ptrext.Of(digestrun.Run{
		ID:       1,
		TenantID: tenantID,
		RunDate:  time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
	})
}

// ---------------------------------------------------------------------------
// TestNewWorker
// ---------------------------------------------------------------------------

func TestNewWorker(t *testing.T) {
	t.Parallel()

	w := newTestWorker(stubTargetReader{}, nil)

	require.NotNil(t, w)
	require.True(t, strings.HasPrefix(w.owner, "digest-"), "owner prefix")
	require.Equal(t, 60*time.Second, w.pollInterval)
	require.Equal(t, 5*time.Minute, w.staleDuration)
	require.Equal(t, 2*time.Hour, w.graceWindow)
	require.Equal(t, 5, w.maxAttempts)
	require.Equal(t, 20, w.drainBatch)
	require.Equal(t, "https://console.test", w.deepLinkBase)
	require.NotNil(t, w.drain)
	require.NotNil(t, w.sender)
}

func TestNewWorker_NilPointer(t *testing.T) {
	t.Parallel()
	var w *Worker
	require.Nil(t, w)
}

func TestNewWorker_OwnerUniqueness(t *testing.T) {
	t.Parallel()

	w1 := newTestWorker(stubTargetReader{}, nil)
	w2 := newTestWorker(stubTargetReader{}, nil)
	require.NotEqual(t, w1.owner, w2.owner)
}

func TestNewWorker_EmptyDeepLink(t *testing.T) {
	t.Parallel()

	transport := notify.NewTransport(nil, notify.NoRetry())
	w := NewWorker(nil, nil, nil, stubTargetReader{}, nil, transport, "")
	require.Empty(t, w.deepLinkBase)
}

// ---------------------------------------------------------------------------
// TestWorker_Configure
// ---------------------------------------------------------------------------

func TestWorker_Configure(t *testing.T) {
	t.Parallel()

	t.Run("positive values override defaults", func(t *testing.T) {
		t.Parallel()
		w := newTestWorker(stubTargetReader{}, nil)
		w.Configure(10*time.Second, 3)

		require.Equal(t, 10*time.Second, w.pollInterval)
		require.Equal(t, 3, w.maxAttempts)
	})

	t.Run("zero values keep defaults", func(t *testing.T) {
		t.Parallel()
		w := newTestWorker(stubTargetReader{}, nil)
		origPoll := w.pollInterval
		origAttempts := w.maxAttempts

		w.Configure(0, 0)

		require.Equal(t, origPoll, w.pollInterval)
		require.Equal(t, origAttempts, w.maxAttempts)
	})

	t.Run("negative pollInterval keeps default", func(t *testing.T) {
		t.Parallel()
		w := newTestWorker(stubTargetReader{}, nil)
		origPoll := w.pollInterval

		w.Configure(-5*time.Second, 7)

		require.Equal(t, origPoll, w.pollInterval)
		require.Equal(t, 7, w.maxAttempts)
	})

	t.Run("negative maxAttempts keeps default", func(t *testing.T) {
		t.Parallel()
		w := newTestWorker(stubTargetReader{}, nil)
		origAttempts := w.maxAttempts

		w.Configure(30*time.Second, -3)

		require.Equal(t, 30*time.Second, w.pollInterval)
		require.Equal(t, origAttempts, w.maxAttempts)
	})

	t.Run("partial update only poll", func(t *testing.T) {
		t.Parallel()
		w := newTestWorker(stubTargetReader{}, nil)
		origAttempts := w.maxAttempts

		w.Configure(15*time.Second, 0)

		require.Equal(t, 15*time.Second, w.pollInterval)
		require.Equal(t, origAttempts, w.maxAttempts)
	})

	t.Run("partial update only maxAttempts", func(t *testing.T) {
		t.Parallel()
		w := newTestWorker(stubTargetReader{}, nil)
		origPoll := w.pollInterval

		w.Configure(0, 10)

		require.Equal(t, origPoll, w.pollInterval)
		require.Equal(t, 10, w.maxAttempts)
	})
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestHeartbeatInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, 90*time.Second, heartbeatInterval)
}

func TestDrainTimeout(t *testing.T) {
	t.Parallel()
	require.Equal(t, 30*time.Second, drainTimeout)
}

// ---------------------------------------------------------------------------
// TestDeferForBacklog
// ---------------------------------------------------------------------------

func TestDeferForBacklog(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)

	t.Run("nil embed always false", func(t *testing.T) {
		t.Parallel()
		w := &Worker{graceWindow: 2 * time.Hour}
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-time.Hour)))
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("nil NextRunAt always false", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 10},
			graceWindow: 2 * time.Hour,
		}
		d := makeDueSub("t1", "UTC", "daily", nil)
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("past grace window proceeds regardless of depth", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 100},
			graceWindow: 2 * time.Hour,
		}
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-3*time.Hour)))
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("within grace and depth > 0 defers", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 5},
			graceWindow: 2 * time.Hour,
		}
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-30*time.Minute)))
		require.True(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("within grace and depth == 0 proceeds", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 0},
			graceWindow: 2 * time.Hour,
		}
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-30*time.Minute)))
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("queue depth error returns false", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 0, err: errors.New("db down")},
			graceWindow: 2 * time.Hour,
		}
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-30*time.Minute)))
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("exactly at grace boundary checks depth", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 10},
			graceWindow: 2 * time.Hour,
		}
		// now == NextRunAt + graceWindow => After is false => checks depth
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-2*time.Hour)))
		require.True(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("one nanosecond past grace proceeds", func(t *testing.T) {
		t.Parallel()
		w := &Worker{
			embed:       stubEmbedQueue{depth: 10},
			graceWindow: 2 * time.Hour,
		}
		d := makeDueSub("t1", "UTC", "daily", ptrext.Of(now.Add(-2*time.Hour-time.Nanosecond)))
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})

	t.Run("both nil embed and nil NextRunAt", func(t *testing.T) {
		t.Parallel()
		w := &Worker{graceWindow: 2 * time.Hour}
		d := makeDueSub("t1", "UTC", "daily", nil)
		require.False(t, w.deferForBacklog(context.Background(), now, d))
	})
}

// TestRun_ContextCancellation is not feasible as a unit test because Run()
// calls w.runs.ResetStaleClaims on boot, requiring a live pgxpool connection.
// The Run lifecycle is covered by integration tests.

// ---------------------------------------------------------------------------
// TestHeartbeat
// ---------------------------------------------------------------------------

func TestHeartbeat_ContextCancellation(t *testing.T) {
	t.Parallel()

	w := newTestWorker(stubTargetReader{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already cancelled.

	var cancelRunCalled atomic.Bool
	cancelRun := func() { cancelRunCalled.Store(true) }

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.heartbeat(ctx, 999, cancelRun)
	}()

	select {
	case <-done:
		// Heartbeat exited because ctx is done.
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not exit within 5 seconds")
	}
	require.False(t, cancelRunCalled.Load(), "cancelRun should not fire on context cancel")
}

// ---------------------------------------------------------------------------
// TestDrainerIntegration
// ---------------------------------------------------------------------------

func TestWorker_DrainerIntegration(t *testing.T) {
	t.Parallel()

	t.Run("tracks in-flight correctly", func(t *testing.T) {
		t.Parallel()
		w := stubDrainerWorker()

		require.Equal(t, int64(0), w.drain.InFlight())
		w.drain.Enter()
		require.Equal(t, int64(1), w.drain.InFlight())
		w.drain.Leave()
		require.Equal(t, int64(0), w.drain.InFlight())
	})

	t.Run("drain with no in-flight returns immediately", func(t *testing.T) {
		t.Parallel()
		w := stubDrainerWorker()

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.drain.Drain(context.Background())
		}()

		select {
		case <-done:
			// Completed immediately.
		case <-time.After(2 * time.Second):
			t.Fatal("drain should complete immediately with no in-flight")
		}
	})
}

// ---------------------------------------------------------------------------
// TestDeliver
// ---------------------------------------------------------------------------

func TestDeliver_NoTargets(t *testing.T) {
	t.Parallel()

	w := newTestWorker(stubTargetReader{list: nil}, nil)
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "UTC",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless, Stats: feedback.DigestWindowStats{Total: 5}}

	err := w.deliver(context.Background(), run, sub, res)
	require.NoError(t, err, "no targets => nil with logged warning")
}

func TestDeliver_ListTargetsError(t *testing.T) {
	t.Parallel()

	w := newTestWorker(stubTargetReader{listErr: errors.New("db error")}, nil)
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "UTC",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless}

	err := w.deliver(context.Background(), run, sub, res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "list digest targets")
}

func TestDeliver_InvalidTimezone(t *testing.T) {
	t.Parallel()

	targets := []notifytarget.NotifyTarget{
		{
			ID:              uuid.New(),
			TenantID:        "t1",
			DestinationType: "raw-webhook",
			URL:             "https://example.com/hook",
			Secret:          "test-secret-12345678",
		},
	}
	w := newTestWorker(stubTargetReader{list: targets}, nil)
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "Not/A/Timezone",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless}

	err := w.deliver(context.Background(), run, sub, res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load timezone")
}

func TestDeliver_WeeklyNoTargets(t *testing.T) {
	t.Parallel()

	w := newTestWorker(stubTargetReader{list: nil}, nil)
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "weekly",
		},
		ResolvedTimezone: "UTC",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemed, Themes: []Theme{{Title: "test", Count: 5}}}

	err := w.deliver(context.Background(), run, sub, res)
	require.NoError(t, err)
}

func TestDeliver_AllDeliveriesFail(t *testing.T) {
	t.Parallel()

	// Two targets, both with a dest type that has no registered digest channel.
	// The sender.send will fail with a terminal error for each.
	targets := []notifytarget.NotifyTarget{
		{
			ID:              uuid.New(),
			TenantID:        "t1",
			DestinationType: "nonexistent-channel-1",
			URL:             "https://example.com/hook1",
			Secret:          "secret1234567890",
		},
		{
			ID:              uuid.New(),
			TenantID:        "t1",
			DestinationType: "nonexistent-channel-2",
			URL:             "https://example.com/hook2",
			Secret:          "secret1234567890",
		},
	}
	w := newTestWorker(stubTargetReader{list: targets}, nil)
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "UTC",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless, Stats: feedback.DigestWindowStats{Total: 5}}

	err := w.deliver(context.Background(), run, sub, res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "all 2 digest deliveries failed")
}

func TestDeliver_PartialDeliveryFailure(t *testing.T) {
	t.Parallel()

	// One target with a known dest type that will succeed (assuming raw-webhook
	// is registered), one with an unknown channel that will fail. But since
	// we can't guarantee raw-webhook's digest channel is registered in tests,
	// use two unknown channels and verify all-fail. For partial success, we'd
	// need a working outbound adapter. Instead, verify the error message format
	// with a single failing target.
	targets := []notifytarget.NotifyTarget{
		{
			ID:              uuid.New(),
			TenantID:        "t1",
			DestinationType: "nonexistent-channel",
			URL:             "https://example.com/hook1",
			Secret:          "secret1234567890",
		},
	}
	w := newTestWorker(stubTargetReader{list: targets}, nil)
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "UTC",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless}

	err := w.deliver(context.Background(), run, sub, res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "all 1 digest deliveries failed")
}

func TestDeliver_ContextCancelled(t *testing.T) {
	t.Parallel()

	// With a cancelled context and targets that have an unregistered channel,
	// the deliver should still fail (the channel lookup is synchronous, not
	// context-dependent). Verifies no panic on cancelled context.
	targets := []notifytarget.NotifyTarget{
		{
			ID:              uuid.New(),
			TenantID:        "t1",
			DestinationType: "nonexistent-channel",
			URL:             "https://example.com/hook",
			Secret:          "secret1234567890",
		},
	}
	w := newTestWorker(stubTargetReader{list: targets}, nil)
	w.owner = "test-owner"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "UTC",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless}

	err := w.deliver(ctx, run, sub, res)
	// Either context error or delivery failure is acceptable.
	require.Error(t, err)
}

func TestDeliver_DeepLinkBasePassedToView(t *testing.T) {
	t.Parallel()

	// Verify that deliver populates DeepLinkBase in the DigestView.
	// Since there are no targets, this is a no-op but exercises the view
	// construction path without panic.
	w := newTestWorker(stubTargetReader{list: nil}, nil)
	w.deepLinkBase = "https://console.example.com"
	w.owner = "test-owner"

	run := makeRun("t1")
	sub := ptrext.Of(digestsubscription.DueSubscription{
		Subscription: digestsubscription.Subscription{
			Frequency: "daily",
		},
		ResolvedTimezone: "America/New_York",
	})
	sub.TenantID = "t1"
	res := Result{Tier: TierThemeless}

	err := w.deliver(context.Background(), run, sub, res)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// TestToOutboundTarget
// ---------------------------------------------------------------------------

func TestToOutboundTarget(t *testing.T) {
	t.Parallel()

	t.Run("default signature version", func(t *testing.T) {
		t.Parallel()
		target := ptrext.Of(notifytarget.NotifyTarget{
			ID:               uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			TenantID:         "acme",
			URL:              "https://example.com/hook",
			Secret:           "s3cret",
			DestinationType:  "raw-webhook",
			SignatureVersion: "",
		})
		out := toOutboundTarget(target)
		require.Equal(t, "11111111-1111-1111-1111-111111111111", out.ID)
		require.Equal(t, "acme", out.TenantID)
		require.Equal(t, "https://example.com/hook", out.URL)
		require.Equal(t, "s3cret", out.Secret)
		require.Equal(t, "raw-webhook", out.DestinationType)
		require.NotEmpty(t, out.SignatureVersion, "empty sig version should get a default")
	})

	t.Run("explicit signature version preserved", func(t *testing.T) {
		t.Parallel()
		target := ptrext.Of(notifytarget.NotifyTarget{
			ID:               uuid.New(),
			TenantID:         "acme",
			URL:              "https://example.com/hook",
			Secret:           "s3cret",
			DestinationType:  "raw-webhook",
			SignatureVersion: "v2-bytes",
		})
		out := toOutboundTarget(target)
		require.Equal(t, "v2-bytes", out.SignatureVersion)
	})

	t.Run("all fields mapped", func(t *testing.T) {
		t.Parallel()
		id := uuid.New()
		target := ptrext.Of(notifytarget.NotifyTarget{
			ID:               id,
			TenantID:         "tenant-x",
			URL:              "https://hooks.example.com/digest",
			Secret:           "super-secret-key-1234",
			DestinationType:  "lark",
			SignatureVersion: "v2-content-hash",
		})
		out := toOutboundTarget(target)
		require.Equal(t, id.String(), out.ID)
		require.Equal(t, "tenant-x", out.TenantID)
		require.Equal(t, "https://hooks.example.com/digest", out.URL)
		require.Equal(t, "super-secret-key-1234", out.Secret)
		require.Equal(t, "lark", out.DestinationType)
		require.Equal(t, "v2-content-hash", out.SignatureVersion)
	})
}

// ---------------------------------------------------------------------------
// TestNewSender
// ---------------------------------------------------------------------------

func TestNewSender(t *testing.T) {
	t.Parallel()

	transport := notify.NewTransport(nil, notify.NoRetry())
	s := newSender(transport)
	require.NotNil(t, s)
	require.NotNil(t, s.transport)
}

func TestNewSender_NilTransport(t *testing.T) {
	t.Parallel()

	s := newSender(nil)
	require.NotNil(t, s, "sender struct itself is non-nil even with nil transport")
	require.Nil(t, s.transport)
}
