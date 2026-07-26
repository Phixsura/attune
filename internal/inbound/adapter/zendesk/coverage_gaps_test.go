// SPDX-License-Identifier: Apache-2.0

// coverage_gaps_test.go closes unit-coverage gaps on the poll loop's
// enabled/backoff dispatch filters, bookkeeping map initialization, and
// the mid-page context-cancel stop.
package zendesk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestPollAllSources_SkipsDisabledAndBackedOff covers the enabled filter,
// the shouldSkipBackoff skip, and the markPollAttempt bookkeeping in the
// dispatch loop.
func TestPollAllSources_SkipsDisabledAndBackedOff(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	sources, ingestFake, _, deps := buildHappyPathDeps(t)
	src, fake := buildHappyPathFixtures(t, sources, deps)

	disabled := src
	disabled.ID, disabled.Enabled = "src-disabled", false
	sources.Put("t-disabled", disabled)

	backedOff := src
	backedOff.ID = "src-backoff"
	sources.Put("t-backoff", backedOff)

	a := buildTestAdapter(fake, deps)
	a.failureCount[backedOff.ID] = 5
	a.lastAttemptAt[backedOff.ID] = fixedTime.Add(-time.Second)

	a.pollAllSources(context.Background())

	// Only src-1 was polled: one attempt stamp for it, none for the others.
	if _, ok := a.lastAttemptAt[src.ID]; !ok {
		t.Error("markPollAttempt not recorded for the polled source")
	}
	if _, ok := a.lastAttemptAt[disabled.ID]; ok {
		t.Error("disabled source must be skipped before markPollAttempt")
	}
	if got := a.lastAttemptAt[backedOff.ID]; !got.Equal(fixedTime.Add(-time.Second)) {
		t.Error("backed-off source must not be re-attempted")
	}
	if len(ingestFake.Calls) == 0 {
		t.Error("expected the enabled source to ingest its fixture tickets")
	}
}

// TestMarkPollBookkeeping_NilMapInit covers lazy map initialization on a
// zero-value adapter for markPollAttempt / markPollFailure.
func TestMarkPollBookkeeping_NilMapInit(t *testing.T) {
	t.Parallel()
	a := ptrext.Of(adapter{})
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	a.markPollAttempt("s1", now)
	if got := a.lastAttemptAt["s1"]; !got.Equal(now) {
		t.Errorf("lastAttemptAt = %v, want %v", got, now)
	}
	a.markPollFailure("s1")
	if a.failureCount["s1"] != 1 {
		t.Errorf("failureCount = %d, want 1", a.failureCount["s1"])
	}
}

// TestProcessTicketPage_ContextCancelStops covers the mid-page ctx-done
// stop: completed stays false so the cursor cannot advance.
func TestProcessTicketPage_ContextCancelStops(t *testing.T) {
	_, ingestFake, _, deps := buildHappyPathDeps(t)
	_ = ingestFake
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)

	src := inbound.Source{ID: "src-1", TenantID: "t1", Channel: channelName, Slug: "acme"}
	page := ticketPage{Tickets: []ticket{{ID: 1, Status: "open", Subject: "x", Description: "y"}}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := a.processTicketPage(ctx, fake, src, Config{}, page, "test")
	if res.completed {
		t.Error("cancelled ctx must leave the page incomplete (cursor frozen)")
	}
	if len(ingestFake.Calls) != 0 {
		t.Errorf("no tickets should ingest after cancel, got %d", len(ingestFake.Calls))
	}
}

// TestTruncateBytesRuneSafe_Zendesk covers the rune-safe byte cut and
// the adjacent-range fallback in assembleComments.
func TestTruncateBytesRuneSafe_Zendesk(t *testing.T) {
	t.Parallel()
	if got := truncateBytesRuneSafe("short", 100); got != "short" {
		t.Errorf("under-limit input changed: %q", got)
	}
	if got := truncateBytesRuneSafe("世界", 4); got != "世" {
		t.Errorf("truncateBytesRuneSafe(世界, 4) = %q, want 世", got)
	}
}

// TestAssembleComments_AllCustomersKeptFallsBack: agent-heavy threads
// whose customer comments all fit in the keep budget fall back to
// rune-safe byte truncation.
func TestAssembleComments_AllCustomersKeptFallsBack(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("汉", 500)
	entries := []tagged{
		{body: "agent " + long, tag: "[agent]"},
		{body: "agent " + long, tag: "[agent]"},
	}
	for i := 0; i < 5; i++ {
		entries = append(entries, tagged{body: "customer " + long, tag: "[customer]", isCustomer: true})
	}
	got := assembleComments("Ticket header", entries)
	if !strings.HasSuffix(got, " [truncated]") {
		t.Errorf("expected byte-truncation fallback, tail: %q", got[len(got)-30:])
	}
	if strings.Contains(got, "omitted") {
		t.Error("no omission marker expected when every customer comment is kept")
	}
	if len(got) > maxContentLen+len(" [truncated]") {
		t.Errorf("content too long: %d", len(got))
	}
}
