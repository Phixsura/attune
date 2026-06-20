// Package ratelimit provides rate limiting for attune. This file adds
// sliding-window and concurrency limiters for Console batch/search operations
// (#30). The existing token-bucket limiter.go continues serving the ingest API.
//
// Design:
//   - Sliding window: track timestamps of requests within the window, count and
//     reject when limit exceeded. More predictable for user-facing APIs than
//     token bucket.
//   - Concurrency limiter: semaphore for max simultaneous async jobs per tenant.
//   - In-memory only. Restart drops all state — acceptable for Y1 traffic.
//     Redis/external store is a future extension point via the interfaces.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// RateLimitInfo contains rate limit state for response headers.
type RateLimitInfo struct {
	Limit     int           // Maximum requests allowed in window
	Remaining int           // Requests remaining in current window
	Reset     time.Duration // Time until window resets
}

// SlidingLimiter checks if a request should be allowed using a sliding window.
type SlidingLimiter interface {
	// Allow checks if key is under the rate limit.
	// Returns:
	//   - allowed: true if request should proceed
	//   - retryAfter: duration to wait if not allowed (0 if allowed)
	//   - err: error if limiter failed (reserved for distributed implementations)
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error)

	// AllowWithInfo is like Allow but also returns rate limit info for headers.
	AllowWithInfo(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, info RateLimitInfo, err error)
}

// ConcurrencyLimiter tracks concurrent usage limits (e.g., max async jobs).
type ConcurrencyLimiter interface {
	// Acquire tries to acquire a slot. Returns release function if acquired.
	// The release function must be called when the work completes.
	// Returns (nil, false, nil) if maxConcurrent reached.
	Acquire(ctx context.Context, key string, maxConcurrent int) (release func(), acquired bool, err error)
}

// windowEntry holds request timestamps for a single key.
type windowEntry struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// MemorySlidingLimiter is an in-process sliding window rate limiter.
// Thread-safe. Suitable for single-instance deployments.
//
// Call StartCleanup to periodically evict keys with no recent activity and
// prevent unbounded memory growth.
type MemorySlidingLimiter struct {
	windows   sync.Map // key -> *windowEntry
	nowFunc   func() time.Time
	maxWindow time.Duration // largest window seen, used for cleanup threshold
	mu        sync.RWMutex  // protects maxWindow
}

// NewMemorySlidingLimiter returns a new in-process sliding window limiter.
func NewMemorySlidingLimiter() *MemorySlidingLimiter {
	return ptrext.Of(MemorySlidingLimiter{
		nowFunc: time.Now,
	})
}

// Allow implements SlidingLimiter. It tracks request timestamps per key and
// rejects requests that would exceed the limit within the window.
func (m *MemorySlidingLimiter) Allow(
	ctx context.Context, key string, limit int, window time.Duration,
) (bool, time.Duration, error) {
	if limit <= 0 || key == "" {
		return true, 0, nil
	}

	// Track the largest window seen for cleanup purposes.
	m.mu.RLock()
	currentMax := m.maxWindow
	m.mu.RUnlock()
	if window > currentMax {
		m.mu.Lock()
		if window > m.maxWindow {
			m.maxWindow = window
		}
		m.mu.Unlock()
	}

	now := m.nowFunc()
	cutoff := now.Add(-window)

	entryAny, _ := m.windows.LoadOrStore(key, ptrext.Of(windowEntry{}))
	entry := entryAny.(*windowEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Filter out expired timestamps.
	valid := entry.timestamps[:0]
	for _, ts := range entry.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	entry.timestamps = valid

	if len(entry.timestamps) >= limit {
		// Calculate retry-after: when the oldest request expires.
		oldest := entry.timestamps[0]
		retryAfter := oldest.Add(window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter, nil
	}

	entry.timestamps = append(entry.timestamps, now)
	return true, 0, nil
}

// AllowWithInfo implements SlidingLimiter. It's like Allow but returns full
// rate limit info for setting X-RateLimit-* response headers.
func (m *MemorySlidingLimiter) AllowWithInfo(
	ctx context.Context, key string, limit int, window time.Duration,
) (bool, RateLimitInfo, error) {
	if limit <= 0 || key == "" {
		return true, RateLimitInfo{Limit: limit, Remaining: limit, Reset: window}, nil
	}

	// Track the largest window seen for cleanup purposes.
	m.mu.RLock()
	currentMax := m.maxWindow
	m.mu.RUnlock()
	if window > currentMax {
		m.mu.Lock()
		if window > m.maxWindow {
			m.maxWindow = window
		}
		m.mu.Unlock()
	}

	now := m.nowFunc()
	cutoff := now.Add(-window)

	entryAny, _ := m.windows.LoadOrStore(key, ptrext.Of(windowEntry{}))
	entry := entryAny.(*windowEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Filter out expired timestamps.
	valid := entry.timestamps[:0]
	var oldest time.Time
	for _, ts := range entry.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
			if oldest.IsZero() || ts.Before(oldest) {
				oldest = ts
			}
		}
	}
	entry.timestamps = valid

	// Calculate reset time: when the oldest request in the window expires.
	var reset time.Duration
	if len(entry.timestamps) > 0 {
		reset = oldest.Add(window).Sub(now)
		if reset < 0 {
			reset = 0
		}
	} else {
		reset = window
	}

	if len(entry.timestamps) >= limit {
		info := RateLimitInfo{
			Limit:     limit,
			Remaining: 0,
			Reset:     reset,
		}
		return false, info, nil
	}

	remaining := limit - len(entry.timestamps) - 1 // -1 for the request we're about to add
	entry.timestamps = append(entry.timestamps, now)

	info := RateLimitInfo{
		Limit:     limit,
		Remaining: remaining,
		Reset:     reset,
	}
	return true, info, nil
}

// StartCleanup runs a background goroutine that periodically evicts keys with
// no recent activity. Call this once at startup to prevent unbounded memory
// growth from abandoned rate limit keys.
//
// The cleanup runs every interval and removes keys whose newest timestamp is
// older than the largest window seen by Allow(). The goroutine exits when ctx
// is canceled.
func (m *MemorySlidingLimiter) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanup()
			}
		}
	}()
}

// cleanup removes keys with no timestamps within the current max window.
// Keys that have been inactive longer than the largest window seen are evicted.
func (m *MemorySlidingLimiter) cleanup() {
	m.mu.RLock()
	maxWindow := m.maxWindow
	m.mu.RUnlock()

	// If no Allow() calls have been made yet, nothing to clean.
	if maxWindow == 0 {
		return
	}

	now := m.nowFunc()
	cutoff := now.Add(-maxWindow)

	m.windows.Range(func(key, value any) bool {
		entry := value.(*windowEntry)
		entry.mu.Lock()

		// Filter out expired timestamps.
		valid := entry.timestamps[:0]
		for _, ts := range entry.timestamps {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}
		entry.timestamps = valid
		isEmpty := len(entry.timestamps) == 0

		entry.mu.Unlock()

		// Remove entries with no remaining timestamps.
		if isEmpty {
			m.windows.Delete(key)
		}
		return true
	})
}

// KeyCount returns the number of tracked keys. Useful for testing and metrics.
func (m *MemorySlidingLimiter) KeyCount() int {
	count := 0
	m.windows.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// concurrencyEntry tracks concurrent slots for a single key.
type concurrencyEntry struct {
	mu      sync.Mutex
	current int
}

// MemoryConcurrencyLimiter is an in-process concurrency limiter.
// Thread-safe. Suitable for single-instance deployments.
type MemoryConcurrencyLimiter struct {
	slots sync.Map // key -> *concurrencyEntry
}

// NewMemoryConcurrencyLimiter returns a new in-process concurrency limiter.
func NewMemoryConcurrencyLimiter() *MemoryConcurrencyLimiter {
	return ptrext.Of(MemoryConcurrencyLimiter{})
}

// Acquire implements ConcurrencyLimiter. It tries to acquire a slot for the key.
// If maxConcurrent slots are already in use, returns (nil, false, nil).
// On success, returns a release function that must be called when work completes.
func (m *MemoryConcurrencyLimiter) Acquire(
	ctx context.Context, key string, maxConcurrent int,
) (func(), bool, error) {
	if maxConcurrent <= 0 || key == "" {
		return func() {}, true, nil
	}

	entryAny, _ := m.slots.LoadOrStore(key, ptrext.Of(concurrencyEntry{}))
	entry := entryAny.(*concurrencyEntry)

	entry.mu.Lock()
	if entry.current >= maxConcurrent {
		entry.mu.Unlock()
		return nil, false, nil
	}
	entry.current++
	entry.mu.Unlock()

	released := false
	return func() {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if !released {
			entry.current--
			released = true
		}
	}, true, nil
}

// CurrentCount returns the current number of acquired slots for a key.
// Useful for testing and metrics.
func (m *MemoryConcurrencyLimiter) CurrentCount(key string) int {
	entryAny, ok := m.slots.Load(key)
	if !ok {
		return 0
	}
	entry := entryAny.(*concurrencyEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.current
}
