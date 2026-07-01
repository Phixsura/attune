// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package notify

import (
	"net/http"
	"testing"
	"time"
)

func TestNoRetry_Values(t *testing.T) {
	t.Parallel()

	p := NoRetry()
	if p.MaxAttempts != 1 {
		t.Fatalf("NoRetry().MaxAttempts = %d, want 1", p.MaxAttempts)
	}
	if p.BaseDelay != 0 {
		t.Fatalf("NoRetry().BaseDelay = %v, want 0", p.BaseDelay)
	}
	if p.MaxDelay != 0 {
		t.Fatalf("NoRetry().MaxDelay = %v, want 0", p.MaxDelay)
	}
}

func TestDefaultRetry_Values(t *testing.T) {
	t.Parallel()

	p := DefaultRetry()
	if p.MaxAttempts != 5 {
		t.Fatalf("DefaultRetry().MaxAttempts = %d, want 5", p.MaxAttempts)
	}
	if p.BaseDelay != 1*time.Second {
		t.Fatalf("DefaultRetry().BaseDelay = %v, want 1s", p.BaseDelay)
	}
	if p.MaxDelay != 10*time.Minute {
		t.Fatalf("DefaultRetry().MaxDelay = %v, want 10m", p.MaxDelay)
	}
}

func TestRetryPolicy_Backoff_ZeroBaseDelay(t *testing.T) {
	t.Parallel()

	p := RetryPolicy{BaseDelay: 0, MaxDelay: 10 * time.Second}
	for n := 1; n <= 5; n++ {
		if got := p.backoff(n); got != 0 {
			t.Errorf("backoff(%d) with zero BaseDelay = %v, want 0", n, got)
		}
	}
}

func TestRetryPolicy_Backoff_NegativeBaseDelay(t *testing.T) {
	t.Parallel()

	p := RetryPolicy{BaseDelay: -1 * time.Second, MaxDelay: 10 * time.Second}
	if got := p.backoff(1); got != 0 {
		t.Fatalf("backoff(1) with negative BaseDelay = %v, want 0", got)
	}
}

func TestRetryPolicy_Backoff_ZeroMaxDelay(t *testing.T) {
	t.Parallel()

	// When MaxDelay is 0, clamping should be skipped.
	p := RetryPolicy{BaseDelay: 1 * time.Second, MaxDelay: 0}
	if got := p.backoff(10); got <= 0 {
		t.Fatalf("backoff(10) with zero MaxDelay = %v, want > 0 (no clamp)", got)
	}
}

func TestRetryPolicy_Backoff_BaseEqualMax(t *testing.T) {
	t.Parallel()

	p := RetryPolicy{BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Second}
	// n=1: 5s, n=2: 10s -> clamped to 5s
	if got := p.backoff(1); got != 5*time.Second {
		t.Errorf("backoff(1) = %v, want 5s", got)
	}
	if got := p.backoff(2); got != 5*time.Second {
		t.Errorf("backoff(2) = %v, want 5s (clamped)", got)
	}
}

func TestNewTransport_ClampsMaxAttemptsToOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		max  int
	}{
		{"zero", 0},
		{"negative", -3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// We pass a non-nil http.Client to avoid the default client creation
			// which needs the egress policy set up.
			tr := NewTransport(nil, RetryPolicy{MaxAttempts: tc.max})
			if tr.retry.MaxAttempts != 1 {
				t.Fatalf("NewTransport clamp: MaxAttempts = %d, want 1", tr.retry.MaxAttempts)
			}
		})
	}
}

func TestNewTransport_PreservesValidMaxAttempts(t *testing.T) {
	t.Parallel()

	tr := NewTransport(nil, RetryPolicy{MaxAttempts: 7, BaseDelay: time.Second, MaxDelay: time.Minute})
	if tr.retry.MaxAttempts != 7 {
		t.Fatalf("MaxAttempts = %d, want 7", tr.retry.MaxAttempts)
	}
	if tr.retry.BaseDelay != time.Second {
		t.Fatalf("BaseDelay = %v, want 1s", tr.retry.BaseDelay)
	}
	if tr.retry.MaxDelay != time.Minute {
		t.Fatalf("MaxDelay = %v, want 1m", tr.retry.MaxDelay)
	}
}

func TestRetryAfterDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(30 * time.Second).Format(http.TimeFormat)
	past := now.Add(-30 * time.Second).Format(http.TimeFormat)

	cases := []struct {
		name     string
		value    string
		maxDelay time.Duration
		want     time.Duration
	}{
		{name: "empty", value: "", want: 0},
		{name: "seconds", value: "3", want: 3 * time.Second},
		{name: "seconds whitespace", value: " 2 ", want: 2 * time.Second},
		{name: "zero seconds", value: "0", want: 0},
		{name: "negative seconds", value: "-1", want: 0},
		{name: "invalid", value: "soon", want: 0},
		{name: "http date", value: future, want: 30 * time.Second},
		{name: "past http date", value: past, want: 0},
		{name: "clamped seconds", value: "30", maxDelay: 5 * time.Second, want: 5 * time.Second},
		{name: "clamped http date", value: future, maxDelay: 5 * time.Second, want: 5 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := retryAfterDelay(tc.value, now, tc.maxDelay)
			if got != tc.want {
				t.Fatalf("retryAfterDelay(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
