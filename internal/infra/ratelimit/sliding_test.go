package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestMemorySlidingLimiter_AllowUnderLimit(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	// 5 requests under a limit of 10 should all be allowed.
	for i := 0; i < 5; i++ {
		allowed, retryAfter, err := m.Allow(ctx, "tenant-1", 10, time.Minute)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i+1, err)
		}
		if !allowed {
			t.Fatalf("request %d: should be allowed", i+1)
		}
		if retryAfter != 0 {
			t.Fatalf("request %d: retryAfter=%v, want 0", i+1, retryAfter)
		}
	}
}

func TestMemorySlidingLimiter_BlockOverLimit(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	// Fill the bucket.
	for i := 0; i < 3; i++ {
		allowed, _, _ := m.Allow(ctx, "tenant-1", 3, time.Minute)
		if !allowed {
			t.Fatalf("request %d: should be allowed", i+1)
		}
	}

	// 4th request should be blocked.
	allowed, retryAfter, err := m.Allow(ctx, "tenant-1", 3, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("4th request should be blocked")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter=%v, want positive duration", retryAfter)
	}
}

func TestMemorySlidingLimiter_WindowExpiry(t *testing.T) {
	// Use controllable time.
	now := time.Now()
	m := ptrext.Of(MemorySlidingLimiter{
		nowFunc: func() time.Time { return now },
	})
	ctx := context.Background()

	// Fill the bucket at t=0.
	for i := 0; i < 3; i++ {
		allowed, _, _ := m.Allow(ctx, "tenant-1", 3, time.Minute)
		if !allowed {
			t.Fatalf("request %d at t=0: should be allowed", i+1)
		}
	}

	// Verify blocked at t=0.
	allowed, _, _ := m.Allow(ctx, "tenant-1", 3, time.Minute)
	if allowed {
		t.Fatal("request at t=0 should be blocked")
	}

	// Advance past the window.
	now = now.Add(61 * time.Second)

	// Should be allowed again.
	allowed, retryAfter, err := m.Allow(ctx, "tenant-1", 3, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("request after window expiry should be allowed")
	}
	if retryAfter != 0 {
		t.Fatalf("retryAfter=%v, want 0", retryAfter)
	}
}

func TestMemorySlidingLimiter_IndependentKeys(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	// Fill bucket for tenant-1.
	for i := 0; i < 2; i++ {
		_, _, _ = m.Allow(ctx, "tenant-1", 2, time.Minute)
	}
	allowed, _, _ := m.Allow(ctx, "tenant-1", 2, time.Minute)
	if allowed {
		t.Fatal("tenant-1 should be blocked")
	}

	// tenant-2 should have its own bucket.
	allowed, retryAfter, err := m.Allow(ctx, "tenant-2", 2, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("tenant-2 should be allowed")
	}
	if retryAfter != 0 {
		t.Fatalf("retryAfter=%v, want 0", retryAfter)
	}
}

func TestMemorySlidingLimiter_ConcurrentAccess(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()
	const goroutines = 100
	const requestsPerGoroutine = 10

	var wg sync.WaitGroup
	var allowedCount atomic.Int32

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				allowed, _, _ := m.Allow(ctx, "shared-key", 500, time.Minute)
				if allowed {
					allowedCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// With a limit of 500, we should see exactly 500 allowed.
	if got := allowedCount.Load(); got != 500 {
		t.Fatalf("allowed=%d, want 500", got)
	}
}

func TestMemorySlidingLimiter_EmptyKeyBypass(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	// Empty key should bypass (like anonymous requests).
	for i := 0; i < 100; i++ {
		allowed, _, _ := m.Allow(ctx, "", 1, time.Minute)
		if !allowed {
			t.Fatalf("request %d with empty key should be allowed", i+1)
		}
	}
}

func TestMemorySlidingLimiter_ZeroLimitBypass(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		allowed, _, _ := m.Allow(ctx, "tenant-1", 0, time.Minute)
		if !allowed {
			t.Fatalf("request %d with zero limit should be allowed", i+1)
		}
	}
}

func TestMemorySlidingLimiter_Cleanup(t *testing.T) {
	// Use controllable time.
	now := time.Now()
	m := ptrext.Of(MemorySlidingLimiter{
		nowFunc: func() time.Time { return now },
	})
	ctx := context.Background()

	// Create activity for multiple keys.
	for i := 0; i < 3; i++ {
		_, _, _ = m.Allow(ctx, "tenant-1", 10, time.Minute)
	}
	for i := 0; i < 2; i++ {
		_, _, _ = m.Allow(ctx, "tenant-2", 10, 2*time.Minute)
	}
	_, _, _ = m.Allow(ctx, "tenant-3", 10, 30*time.Second)

	// Verify all keys exist.
	if got := m.KeyCount(); got != 3 {
		t.Fatalf("KeyCount before cleanup = %d, want 3", got)
	}

	// Advance time past the largest window (2 minutes).
	now = now.Add(3 * time.Minute)

	// Run cleanup.
	m.cleanup()

	// All keys should be removed since their timestamps are older than maxWindow.
	if got := m.KeyCount(); got != 0 {
		t.Fatalf("KeyCount after cleanup = %d, want 0", got)
	}
}

func TestMemorySlidingLimiter_CleanupKeepsActiveKeys(t *testing.T) {
	// Use controllable time.
	now := time.Now()
	m := ptrext.Of(MemorySlidingLimiter{
		nowFunc: func() time.Time { return now },
	})
	ctx := context.Background()

	// Create activity for two keys at t=0.
	_, _, _ = m.Allow(ctx, "old-tenant", 10, time.Minute)
	_, _, _ = m.Allow(ctx, "new-tenant", 10, time.Minute)

	// Advance time 30 seconds.
	now = now.Add(30 * time.Second)

	// Add fresh activity to new-tenant.
	_, _, _ = m.Allow(ctx, "new-tenant", 10, time.Minute)

	// Advance past the 1-minute window for the original timestamps.
	now = now.Add(40 * time.Second) // Total: 70 seconds from start.

	// Run cleanup.
	m.cleanup()

	// old-tenant should be removed, new-tenant should remain (has recent activity).
	if got := m.KeyCount(); got != 1 {
		t.Fatalf("KeyCount after cleanup = %d, want 1", got)
	}

	// Verify new-tenant still works.
	allowed, _, _ := m.Allow(ctx, "new-tenant", 10, time.Minute)
	if !allowed {
		t.Fatal("new-tenant should still be allowed")
	}
}

func TestMemorySlidingLimiter_CleanupNoActivity(t *testing.T) {
	m := NewMemorySlidingLimiter()

	// Cleanup with no prior Allow calls should not panic.
	m.cleanup()

	if got := m.KeyCount(); got != 0 {
		t.Fatalf("KeyCount = %d, want 0", got)
	}
}

func TestMemorySlidingLimiter_StartCleanup(t *testing.T) {
	// Use controllable time.
	now := time.Now()
	m := ptrext.Of(MemorySlidingLimiter{
		nowFunc: func() time.Time { return now },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Add some keys.
	_, _, _ = m.Allow(ctx, "tenant-1", 10, 50*time.Millisecond)
	_, _, _ = m.Allow(ctx, "tenant-2", 10, 50*time.Millisecond)

	if got := m.KeyCount(); got != 2 {
		t.Fatalf("KeyCount before cleanup = %d, want 2", got)
	}

	// Advance past the window.
	now = now.Add(100 * time.Millisecond)

	// Start cleanup with a short interval.
	m.StartCleanup(ctx, 10*time.Millisecond)

	// Wait for cleanup to run.
	time.Sleep(50 * time.Millisecond)

	// Keys should be cleaned up.
	if got := m.KeyCount(); got != 0 {
		t.Fatalf("KeyCount after cleanup = %d, want 0", got)
	}
}

func TestMemorySlidingLimiter_StartCleanupCancellation(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx, cancel := context.WithCancel(context.Background())

	// Add a key.
	_, _, _ = m.Allow(ctx, "tenant-1", 10, time.Hour)

	// Start cleanup.
	m.StartCleanup(ctx, 10*time.Millisecond)

	// Cancel immediately.
	cancel()

	// Give it time to potentially run.
	time.Sleep(30 * time.Millisecond)

	// Key should still exist (cleanup goroutine exited).
	if got := m.KeyCount(); got != 1 {
		t.Fatalf("KeyCount = %d, want 1 (key should remain after cancel)", got)
	}
}

func TestMemorySlidingLimiter_KeyCount(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	if got := m.KeyCount(); got != 0 {
		t.Fatalf("initial KeyCount = %d, want 0", got)
	}

	_, _, _ = m.Allow(ctx, "tenant-1", 10, time.Minute)
	if got := m.KeyCount(); got != 1 {
		t.Fatalf("KeyCount after 1 key = %d, want 1", got)
	}

	_, _, _ = m.Allow(ctx, "tenant-2", 10, time.Minute)
	if got := m.KeyCount(); got != 2 {
		t.Fatalf("KeyCount after 2 keys = %d, want 2", got)
	}

	// Same key should not increase count.
	_, _, _ = m.Allow(ctx, "tenant-1", 10, time.Minute)
	if got := m.KeyCount(); got != 2 {
		t.Fatalf("KeyCount after same key = %d, want 2", got)
	}
}

func TestMemoryConcurrencyLimiter_AcquireAndRelease(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	release1, acquired, err := m.Acquire(ctx, "tenant-1", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("first acquire should succeed")
	}

	release2, acquired, err := m.Acquire(ctx, "tenant-1", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("second acquire should succeed")
	}

	// Third should fail.
	_, acquired, err = m.Acquire(ctx, "tenant-1", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Fatal("third acquire should fail at max concurrency")
	}

	// Release one slot.
	release1()

	// Now should succeed.
	release3, acquired, err := m.Acquire(ctx, "tenant-1", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("acquire after release should succeed")
	}

	// Clean up.
	release2()
	release3()
}

func TestMemoryConcurrencyLimiter_IndependentKeys(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	// Fill tenant-1.
	release1, _, _ := m.Acquire(ctx, "tenant-1", 1)
	defer release1()

	_, acquired, _ := m.Acquire(ctx, "tenant-1", 1)
	if acquired {
		t.Fatal("tenant-1 second acquire should fail")
	}

	// tenant-2 should have its own limit.
	release2, acquired, err := m.Acquire(ctx, "tenant-2", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("tenant-2 should be independent")
	}
	release2()
}

func TestMemoryConcurrencyLimiter_ConcurrentAccess(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()
	const goroutines = 50
	const maxConcurrent = 5

	var wg sync.WaitGroup
	var activeCount atomic.Int32
	var maxSeen atomic.Int32

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, acquired, _ := m.Acquire(ctx, "shared-key", maxConcurrent)
			if !acquired {
				return
			}
			current := activeCount.Add(1)
			// Track max concurrent ever seen.
			for {
				old := maxSeen.Load()
				if current <= old {
					break
				}
				if maxSeen.CompareAndSwap(old, current) {
					break
				}
			}
			// Simulate some work.
			time.Sleep(time.Millisecond)
			activeCount.Add(-1)
			release()
		}()
	}

	wg.Wait()

	if got := maxSeen.Load(); got > int32(maxConcurrent) {
		t.Fatalf("max concurrent=%d, exceeded limit %d", got, maxConcurrent)
	}
}

func TestMemoryConcurrencyLimiter_DoubleRelease(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	release, acquired, _ := m.Acquire(ctx, "tenant-1", 2)
	if !acquired {
		t.Fatal("acquire should succeed")
	}

	// Release twice should be safe (no-op on second call).
	release()
	release()

	// Verify count is correct (should be 0, not -1).
	if got := m.CurrentCount("tenant-1"); got != 0 {
		t.Fatalf("count=%d after double release, want 0", got)
	}
}

func TestMemoryConcurrencyLimiter_EmptyKeyBypass(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	release, acquired, err := m.Acquire(ctx, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("empty key should bypass")
	}
	release()
}

func TestMemoryConcurrencyLimiter_ZeroLimitBypass(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	release, acquired, err := m.Acquire(ctx, "tenant-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("zero limit should bypass")
	}
	release()
}

func TestMemoryConcurrencyLimiter_CurrentCount(t *testing.T) {
	m := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	if got := m.CurrentCount("nonexistent"); got != 0 {
		t.Fatalf("count for nonexistent key = %d, want 0", got)
	}

	release1, _, _ := m.Acquire(ctx, "tenant-1", 10)
	if got := m.CurrentCount("tenant-1"); got != 1 {
		t.Fatalf("count after 1 acquire = %d, want 1", got)
	}

	release2, _, _ := m.Acquire(ctx, "tenant-1", 10)
	if got := m.CurrentCount("tenant-1"); got != 2 {
		t.Fatalf("count after 2 acquires = %d, want 2", got)
	}

	release1()
	if got := m.CurrentCount("tenant-1"); got != 1 {
		t.Fatalf("count after 1 release = %d, want 1", got)
	}

	release2()
	if got := m.CurrentCount("tenant-1"); got != 0 {
		t.Fatalf("count after all releases = %d, want 0", got)
	}
}

func TestMemorySlidingLimiter_AllowWithInfo(t *testing.T) {
	m := NewMemorySlidingLimiter()
	ctx := context.Background()

	// First request: should have full remaining
	allowed, info, err := m.AllowWithInfo(ctx, "tenant-1", 5, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("first request should be allowed")
	}
	if info.Limit != 5 {
		t.Fatalf("info.Limit = %d, want 5", info.Limit)
	}
	if info.Remaining != 4 {
		t.Fatalf("info.Remaining = %d, want 4", info.Remaining)
	}
	if info.Reset <= 0 {
		t.Fatalf("info.Reset = %v, want > 0", info.Reset)
	}

	// Use up remaining quota
	for i := 0; i < 4; i++ {
		allowed, _, _ = m.AllowWithInfo(ctx, "tenant-1", 5, time.Minute)
		if !allowed {
			t.Fatalf("request %d should be allowed", i+2)
		}
	}

	// 6th request should be blocked with remaining=0
	allowed, info, err = m.AllowWithInfo(ctx, "tenant-1", 5, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("6th request should be blocked")
	}
	if info.Remaining != 0 {
		t.Fatalf("info.Remaining = %d, want 0", info.Remaining)
	}
	if info.Reset <= 0 {
		t.Fatal("info.Reset should be positive when blocked")
	}
}
