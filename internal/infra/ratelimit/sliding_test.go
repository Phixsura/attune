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
