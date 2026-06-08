// SPDX-License-Identifier: Apache-2.0

package email

import (
	"testing"
	"time"
)

// TestPollLagSeconds_DefaultZero — for a source the adapter has not yet
// observed succeed, lag is 0. Matches the "first scrape after boot"
// Prometheus convention (we don't alert on a metric we've never seen).
func TestPollLagSeconds_DefaultZero(t *testing.T) {
	a := NewAdapter().(*adapter)
	now := time.Unix(1_700_000_000, 0)
	if got := a.pollLagSeconds("never-polled", now); got != 0 {
		t.Errorf("pollLagSeconds for unseen source = %v, want 0", got)
	}
}

// TestPollLagSeconds_AfterSuccess — after a successful poll, lag for the
// next attempt is the wall-clock delta since the success stamp.
func TestPollLagSeconds_AfterSuccess(t *testing.T) {
	a := NewAdapter().(*adapter)
	start := time.Unix(1_700_000_000, 0)
	a.markPollSuccess("mailroom", start)

	check := func(elapsed time.Duration) {
		t.Helper()
		got := a.pollLagSeconds("mailroom", start.Add(elapsed))
		if got != elapsed.Seconds() {
			t.Errorf("pollLagSeconds after %v = %v, want %v",
				elapsed, got, elapsed.Seconds())
		}
	}
	check(0)
	check(30 * time.Second)
	check(5 * time.Minute)
}

// TestMarkPollSuccess_PerSlugIsolation — markPollSuccess updates only
// the keyed slug; other sources keep their own stamps (or "unseen"
// status). Catches a regression where the helper accidentally clobbers
// all keys with the latest write.
func TestMarkPollSuccess_PerSlugIsolation(t *testing.T) {
	a := NewAdapter().(*adapter)
	t1 := time.Unix(1_700_000_000, 0)
	t2 := t1.Add(10 * time.Minute)
	a.markPollSuccess("alpha", t1)
	a.markPollSuccess("beta", t2)

	if got := a.pollLagSeconds("alpha", t2); got != (10 * time.Minute).Seconds() {
		t.Errorf("alpha lag at t2 = %v, want %v", got, (10 * time.Minute).Seconds())
	}
	if got := a.pollLagSeconds("beta", t2); got != 0 {
		t.Errorf("beta lag at t2 = %v, want 0", got)
	}
	if got := a.pollLagSeconds("gamma", t2); got != 0 {
		t.Errorf("gamma (never-polled) lag at t2 = %v, want 0", got)
	}
}
