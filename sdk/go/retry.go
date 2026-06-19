package attune

import (
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	baseDelay     = 200 * time.Millisecond
	maxDelay      = 2 * time.Second
	jitterRatio   = 0.25
	maxRetryAfter = 60 * time.Second
)

// IsRetryable reports whether an HTTP status warrants a retry. Only transient
// statuses qualify: 408 (request timeout), 429 (rate limited), and any 5xx.
// Deterministic 4xx — including 409 IDEMPOTENCY_CONFLICT — are permanent. It is
// exported so callers building their own retry loop reuse the SDK's policy.
func IsRetryable(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= 500
}

// BackoffDelay returns the wait before the given retry attempt (0-indexed):
// min(2s, 200ms*2^attempt) with ±25% jitter. Exported for callers reusing the
// SDK's backoff policy.
func BackoffDelay(attempt int) time.Duration {
	return backoffDelay(attempt, rand.Float64)
}

// backoffDelay is the testable core: rand returns a value in [0,1) so tests can
// make the delay deterministic.
func backoffDelay(attempt int, rand func() float64) time.Duration {
	exp := math.Min(float64(maxDelay), float64(baseDelay)*math.Pow(2, float64(attempt)))
	jitter := exp * jitterRatio * (rand()*2 - 1)
	d := exp + jitter
	if d < 0 {
		d = 0
	}
	return time.Duration(d)
}

// ParseRetryAfter reads a Retry-After header in either delta-seconds or
// HTTP-date form and returns the wait, capped at 60s. ok is false when the
// header is absent, malformed, or in the past. Exported for parity with the
// retry policy helpers.
func ParseRetryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	raw := strings.TrimSpace(h.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs < 0 {
			return 0, false
		}
		return capRetryAfter(time.Duration(secs) * time.Second), true
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return capRetryAfter(d), true
	}
	return 0, false
}

func capRetryAfter(d time.Duration) time.Duration {
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
