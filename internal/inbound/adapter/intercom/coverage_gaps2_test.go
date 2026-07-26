// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

// coverage_gaps2_test.go — second sweep of unit-coverage gaps: host
// validation failure, mid-tick context cancellation, rate-limit wait
// interrupted by shutdown, the backoff interval cap, permanent detail
// errors, and company-name backfill from the summary ref.
package intercom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestPollSource_HostValidationFailure covers the validateHost error leg.
func TestPollSource_HostValidationFailure(t *testing.T) {
	sources, _, metrics, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	origValidate := validateHost
	validateHost = func(string) error { return errors.New("host boom") }
	t.Cleanup(func() { validateHost = origValidate })

	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(fake.capturedStarts) != 0 {
		t.Error("host validation failure must stop before any API call")
	}
	found := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|internal_err") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected internal_err metric, got %v", metrics.Totals)
	}
}

// TestSyncPages_ContextCancelledMidTick covers the ctx-done break in the
// page loop and the ctx-done stop inside processConversationPage.
func TestSyncPages_ContextCancelledMidTick(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	fake := ptrext.Of(fakeAPIClient{
		pages: []conversationPage{{Conversations: []conversation{fullConversation()}}},
	})
	a := buildTestAdapter(fake, deps)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sr := a.syncPages(ctx, src, cfg, fake, "test")
	if len(ingestFake.Calls) != 0 {
		t.Errorf("cancelled ctx must not ingest, got %d", len(ingestFake.Calls))
	}
	if sr.watermark != 0 {
		t.Errorf("watermark advanced on a cancelled tick: %d", sr.watermark)
	}
}

// TestProcessConversationPage_CtxCancelStopsRetry covers the per-item
// ctx-done stop returning pageStopRetry.
func TestProcessConversationPage_CtxCancelStopsRetry(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)
	st := pageState{detailBudget: 5, companyCache: map[string]intercomCompany{}, tickStart: 1900000000}
	page := conversationPage{Conversations: []conversation{fullConversation()}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, stop := a.processConversationPage(ctx, fake, src, cfg, page, st, "test")
	if stop != pageStopRetry {
		t.Errorf("stop = %v, want pageStopRetry on cancelled ctx", stop)
	}
}

// TestHandleSearchFailure_RateLimitCtxDone covers the ctx-done arm of the
// rate-limit sleep.
func TestHandleSearchFailure_RateLimitCtxDone(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	a.handleSearchFailure(ctx, src, "test", rateLimitError{Method: "/x", RetryAfter: time.Hour})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("rate-limit wait ignored ctx cancellation: %v", elapsed)
	}
}

// TestShouldSkipBackoff_IntervalCap covers the 15-minute cap arm.
func TestShouldSkipBackoff_IntervalCap(t *testing.T) {
	fixedTime := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	a := ptrext.Of(adapter{
		failureCount:  map[string]int{"s1": 50}, // doubling far past the cap
		lastAttemptAt: map[string]time.Time{"s1": fixedTime.Add(-14 * time.Minute)},
	})
	if !a.shouldSkipBackoff("s1") {
		t.Error("14min since attempt with 50 failures must still skip (15min cap)")
	}
	a.lastAttemptAt["s1"] = fixedTime.Add(-16 * time.Minute)
	if a.shouldSkipBackoff("s1") {
		t.Error("16min since attempt must not skip — interval caps at 15min")
	}
}

// TestPollSource_PermanentDetailErrorCountsAuthErr covers the
// detail_auth_err metric arm of the detail degradation path.
func TestPollSource_PermanentDetailErrorCountsAuthErr(t *testing.T) {
	sources, ingestFake, metrics, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	summary := fullConversation()
	fake := ptrext.Of(fakeAPIClient{
		pages:     []conversationPage{{Conversations: []conversation{summary}}},
		detailErr: apiError{Method: "/conversations/101", Status: 403, Code: "api_plan_restricted"},
	})
	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Degrades to the summary shape — still ingests.
	if len(ingestFake.Calls) != 1 {
		t.Fatalf("ingest calls = %d, want 1 (summary degradation)", len(ingestFake.Calls))
	}
	found := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|detail_auth_err") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected detail_auth_err metric, got %v", metrics.Totals)
	}
}

// TestResolveCompany_EmptyNameBackfilledFromRef covers the name-backfill
// arm when the full company profile has no name.
func TestResolveCompany_EmptyNameBackfilledFromRef(t *testing.T) {
	_, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{
		companyByID: map[string]intercomCompany{
			"co-1": {ID: "co-1", MonthlySpend: 500}, // no name in the profile
		},
	})
	a := buildTestAdapter(fake, deps)
	cache := map[string]intercomCompany{}
	ref := ptrext.Of(intercomCompany{ID: "co-1", Name: "Ref Name"})
	got := a.resolveCompany(context.Background(), fake, ref, cache, "test")
	if got == nil || got.Name != "Ref Name" {
		t.Errorf("resolveCompany name = %+v, want backfill from ref", got)
	}
	if got.MonthlySpend != 500 {
		t.Errorf("MonthlySpend = %v, want profile value 500", got.MonthlySpend)
	}
}

// TestSyncPages_CursorDroppedClearsPersistedCursor covers the
// cursorDropped arm inside the page loop: the stale persisted cursor is
// cleared and the tick continues from a window restart.
func TestSyncPages_CursorDroppedClearsPersistedCursor(t *testing.T) {
	fixedTime := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	sources, _, _, deps := buildDeps(t)
	watermark := fixedTime.Add(-time.Hour).Unix()
	queryStart := (watermark / daySeconds) * daySeconds
	queryEnd := (fixedTime.Unix()/daySeconds + 2) * daySeconds
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
		SyncCursor: "stale-cursor", SyncWindowStart: queryStart, SyncWindowEnd: queryEnd,
	}
	src := testSource(t, sources, deps.Secrets, cfg, watermark)

	calls := 0
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	a.newClient = func(_, _ string) apiClient { return cursorRejectingClient{inner: fake, calls: &calls} }
	a.pollSource(context.Background(), src)

	if calls < 2 {
		t.Errorf("search calls = %d, want >= 2 (cursor retry)", calls)
	}
	// The persisted config must no longer carry the stale cursor.
	if got := sources.configUpdates["src-1"]; got != nil {
		if strings.Contains(string(got), "stale-cursor") {
			t.Error("stale cursor still persisted after drop")
		}
	}
}

// cursorRejectingClient fails the first cursor-bearing search with a
// permanent error and returns empty results afterward.
type cursorRejectingClient struct {
	inner *fakeAPIClient
	calls *int
}

func (c cursorRejectingClient) AuthTest(ctx context.Context) (intercomAccount, error) {
	return c.inner.AuthTest(ctx)
}

func (c cursorRejectingClient) SearchConversations(ctx context.Context, startTime, endTime int64, startingAfter string) (conversationPage, error) {
	*c.calls++
	if startingAfter != "" {
		return conversationPage{}, apiError{Method: "/conversations/search", Status: 422, Code: "bad_cursor"}
	}
	return c.inner.SearchConversations(ctx, startTime, endTime, startingAfter)
}

func (c cursorRejectingClient) GetConversation(ctx context.Context, id string) (conversation, error) {
	return c.inner.GetConversation(ctx, id)
}

func (c cursorRejectingClient) SearchContacts(ctx context.Context, ids []string) ([]intercomContact, error) {
	return c.inner.SearchContacts(ctx, ids)
}

func (c cursorRejectingClient) ListAdmins(ctx context.Context) ([]intercomAdmin, error) {
	return c.inner.ListAdmins(ctx)
}

func (c cursorRejectingClient) GetCompany(ctx context.Context, id string) (intercomCompany, error) {
	return c.inner.GetCompany(ctx, id)
}

func (c cursorRejectingClient) RateBudget() int64 { return c.inner.RateBudget() }
