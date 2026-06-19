package attune

import (
	"net/http"
	"testing"
	"time"
)

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusRequestTimeout, true},         // 408
		{http.StatusTooManyRequests, true},        // 429
		{http.StatusInternalServerError, true},    // 500
		{http.StatusBadGateway, true},             // 502
		{http.StatusServiceUnavailable, true},     // 503
		{http.StatusBadRequest, false},            // 400
		{http.StatusUnauthorized, false},          // 401
		{http.StatusForbidden, false},             // 403
		{http.StatusNotFound, false},              // 404
		{http.StatusConflict, false},              // 409 (idempotency conflict is permanent)
		{http.StatusUnprocessableEntity, false},   // 422
		{http.StatusRequestEntityTooLarge, false}, // 413
		{http.StatusOK, false},
	}
	for _, c := range cases {
		if got := IsRetryable(c.status); got != c.want {
			t.Errorf("IsRetryable(%d) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestBackoffDelaySequence(t *testing.T) {
	// rand()=0.5 => jitter factor (0.5*2-1)=0 => delay equals the exponential term.
	noJitter := func() float64 { return 0.5 }
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 200 * time.Millisecond},
		{1, 400 * time.Millisecond},
		{2, 800 * time.Millisecond},
		{3, 1600 * time.Millisecond},
		{4, 2000 * time.Millisecond}, // capped at 2s
		{5, 2000 * time.Millisecond}, // stays capped
	}
	for _, c := range cases {
		if got := backoffDelay(c.attempt, noJitter); got != c.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestBackoffDelayJitterBounds(t *testing.T) {
	// Across the full random range, delay stays within ±25% of the exponential term.
	for _, r := range []float64{0, 0.25, 0.5, 0.75, 1} {
		d := backoffDelay(2, func() float64 { return r }) // exp term = 800ms
		if d < 600*time.Millisecond || d > 1000*time.Millisecond {
			t.Errorf("backoffDelay(2, rand=%v) = %v, want within [600ms,1000ms]", r, d)
		}
	}
}

func TestBackoffDelayNeverNegative(t *testing.T) {
	if d := backoffDelay(0, func() float64 { return 0 }); d < 0 {
		t.Errorf("backoffDelay produced negative delay %v", d)
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	h := http.Header{"Retry-After": []string{"5"}}
	d, ok := ParseRetryAfter(h, time.Unix(0, 0))
	if !ok || d != 5*time.Second {
		t.Errorf("ParseRetryAfter(5) = %v,%v want 5s,true", d, ok)
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	future := now.Add(10 * time.Second)
	h := http.Header{"Retry-After": []string{future.Format(http.TimeFormat)}}
	d, ok := ParseRetryAfter(h, now)
	if !ok || d != 10*time.Second {
		t.Errorf("ParseRetryAfter(date+10s) = %v,%v want 10s,true", d, ok)
	}
}

func TestParseRetryAfterCappedAt60s(t *testing.T) {
	h := http.Header{"Retry-After": []string{"99999"}}
	d, ok := ParseRetryAfter(h, time.Unix(0, 0))
	if !ok || d != 60*time.Second {
		t.Errorf("ParseRetryAfter(99999) = %v,%v want 60s,true", d, ok)
	}
}

func TestParseRetryAfterNegativeIgnored(t *testing.T) {
	h := http.Header{"Retry-After": []string{"-5"}}
	if _, ok := ParseRetryAfter(h, time.Unix(0, 0)); ok {
		t.Error("ParseRetryAfter(-5) should not be ok")
	}
}

func TestParseRetryAfterMissingOrJunk(t *testing.T) {
	if _, ok := ParseRetryAfter(http.Header{}, time.Unix(0, 0)); ok {
		t.Error("missing Retry-After should not be ok")
	}
	if _, ok := ParseRetryAfter(http.Header{"Retry-After": []string{"soon"}}, time.Unix(0, 0)); ok {
		t.Error("junk Retry-After should not be ok")
	}
}
