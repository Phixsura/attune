// SPDX-License-Identifier: Apache-2.0

package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type tokenBucketEntry struct {
	mu         sync.Mutex
	limiter    *rate.Limiter
	perMinute  int
	burst      int
	lastAccess time.Time // for cleanup
}

// MemoryTokenBucketLimiter is an in-process per-key token bucket limiter.
// It supports dynamic per-key RPM/burst reconfiguration on each request.
// Call StartCleanup to periodically evict idle keys and prevent unbounded memory.
type MemoryTokenBucketLimiter struct {
	buckets sync.Map // key -> *tokenBucketEntry
	nowFunc func() time.Time
}

// NewMemoryTokenBucketLimiter returns a new per-key token bucket limiter.
func NewMemoryTokenBucketLimiter() *MemoryTokenBucketLimiter {
	return ptrext.Of(MemoryTokenBucketLimiter{
		nowFunc: time.Now,
	})
}

// AllowWithInfo reports whether one request should proceed for the given key.
// RPM is the steady-state refill rate; burst is the instantaneous bucket size.
func (m *MemoryTokenBucketLimiter) AllowWithInfo(
	_ context.Context, key string, perMinute, burst int,
) (bool, RateLimitInfo, error) {
	if perMinute <= 0 || key == "" {
		return true, RateLimitInfo{Limit: perMinute, Remaining: burst}, nil
	}
	if burst <= 0 {
		burst = 1
	}

	now := m.nowFunc()
	lim := m.limiterFor(key, now, perMinute, burst)

	allowed := lim.AllowN(now, 1)
	info := RateLimitInfo{
		Limit:     perMinute,
		Remaining: clampTokens(lim.TokensAt(now), burst),
	}
	if allowed {
		return true, info, nil
	}

	reservation := lim.ReserveN(now, 1)
	if reservation.OK() {
		info.Reset = reservation.DelayFrom(now)
		reservation.CancelAt(now)
	}
	return false, info, nil
}

func (m *MemoryTokenBucketLimiter) limiterFor(key string, now time.Time, perMinute, burst int) *rate.Limiter {
	entryAny, _ := m.buckets.LoadOrStore(key, ptrext.Of(tokenBucketEntry{
		limiter:    rate.NewLimiter(ratePerSecond(perMinute), burst),
		perMinute:  perMinute,
		burst:      burst,
		lastAccess: now,
	}))
	entry := entryAny.(*tokenBucketEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.lastAccess = now

	if perMinute != entry.perMinute || burst != entry.burst {
		if perMinute > entry.perMinute || burst > entry.burst {
			// More permissive configs should take effect immediately.
			entry.limiter = rate.NewLimiter(ratePerSecond(perMinute), burst)
		} else {
			entry.limiter.SetLimitAt(now, ratePerSecond(perMinute))
			entry.limiter.SetBurstAt(now, burst)
		}
		entry.perMinute = perMinute
		entry.burst = burst
	}

	return entry.limiter
}

// StartCleanup runs a background goroutine that periodically evicts keys
// that have been idle for longer than maxIdleTime. Call this at startup
// to prevent unbounded memory growth in long-running services.
func (m *MemoryTokenBucketLimiter) StartCleanup(ctx context.Context, interval, maxIdleTime time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanup(maxIdleTime)
			}
		}
	}()
}

func (m *MemoryTokenBucketLimiter) cleanup(maxIdleTime time.Duration) {
	now := m.nowFunc()
	cutoff := now.Add(-maxIdleTime)
	m.buckets.Range(func(key, value any) bool {
		entry := value.(*tokenBucketEntry)
		entry.mu.Lock()
		idle := entry.lastAccess.Before(cutoff)
		entry.mu.Unlock()
		if idle {
			m.buckets.Delete(key)
		}
		return true
	})
}

func ratePerSecond(perMinute int) rate.Limit {
	return rate.Limit(float64(perMinute) / 60.0)
}

func clampTokens(tokens float64, burst int) int {
	remaining := int(math.Floor(tokens))
	if remaining < 0 {
		return 0
	}
	if remaining > burst {
		return burst
	}
	return remaining
}
