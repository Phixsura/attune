// SPDX-License-Identifier: Apache-2.0

// coverage_gaps_test.go closes unit-coverage gaps on paths whose behavior
// is otherwise only exercised by the live E2E stack: HTML stripping,
// rune-safe truncation, permalink regions, lifecycle wiring, poll-loop
// dispatch, stats persistence failure valves, and backoff bookkeeping.
package intercom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// normalize.go
// ---------------------------------------------------------------------------

func TestStripHTMLTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"plain text passes through", "no markup here", "no markup here"},
		{"tags removed, blocks become newlines", "<p>first</p><p>second</p>", "first\nsecond"},
		{"entities unescaped", "<b>a &amp; b &lt;ok&gt; &quot;q&quot; &#39;s&#39;&nbsp;end</b>", `a & b <ok> "q" 's' end`},
		{"unclosed tag swallows the rest", "before <a href=", "before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripHTMLTags(tt.in); got != tt.want {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncateBytesRuneSafe(t *testing.T) {
	t.Parallel()
	if got := truncateBytesRuneSafe("short", 100); got != "short" {
		t.Errorf("under-limit input changed: %q", got)
	}
	// "世" is 3 bytes; cutting at 4 must not split the second rune.
	in := "世界"
	got := truncateBytesRuneSafe(in, 4)
	if got != "世" {
		t.Errorf("truncateBytesRuneSafe(%q, 4) = %q, want %q", in, got, "世")
	}
	if got := truncateBytesRuneSafe("abcdef", 3); got != "abc" {
		t.Errorf("ascii cut = %q, want abc", got)
	}
}

func TestConversationURL_Regions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		region, wantHost string
	}{
		{"us", "app.intercom.com"},
		{"", "app.intercom.com"},
		{"EU", "app.eu.intercom.com"},
		{" au ", "app.au.intercom.com"},
	}
	for _, tt := range tests {
		got := conversationURL(tt.region, "ws-1", "42")
		want := "https://" + tt.wantHost + "/a/inbox/ws-1/inbox/conversation/42"
		if got != want {
			t.Errorf("conversationURL(%q) = %q, want %q", tt.region, got, want)
		}
	}
}

// TestBuildContent_EmptySeedBodySkipsCustomerCount covers the seed-message
// guard: a whitespace/HTML-only seed body must not count as a customer
// message. An empty part body is likewise dropped from the transcript.
func TestBuildContent_EmptySeedBodySkipsCustomerCount(t *testing.T) {
	t.Parallel()
	conv := convWithParts([]part{
		customerPart("c0", "   "), // whitespace-only body → dropped
		agentPart("a1", "hello, how can I help?"),
	})
	conv.Source.Body = "<p>  </p>"
	cr := buildContent(conv)
	if cr.customerMsgs != 0 {
		t.Errorf("customerMsgs = %d, want 0 for empty seed + empty part bodies", cr.customerMsgs)
	}
	if cr.agentMsgs != 1 {
		t.Errorf("agentMsgs = %d, want 1", cr.agentMsgs)
	}
}

// TestTruncateStructurally_FewCustomerMessagesFallsBack covers the byte-
// truncation fallback when the human thread exceeds the cap with too few
// customer messages to split structurally.
func TestTruncateStructurally_FewCustomerMessagesFallsBack(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("y", 3000)
	conv := convWithParts([]part{
		customerPart("c1", "q "+long),
		agentPart("a1", "a "+long),
	})
	cr := buildContent(conv)
	if !strings.HasSuffix(cr.text, " [truncated]") {
		t.Errorf("expected byte-truncation suffix, got tail %q", cr.text[len(cr.text)-30:])
	}
	if len(cr.text) > maxContentLen+len(" [truncated]") {
		t.Errorf("content too long: %d", len(cr.text))
	}
}

// TestTruncateStructurally_ResultStillOverCapReTruncates covers the final
// re-truncation valve: kept customer messages so large that even the
// structural head+tail assembly exceeds the cap.
func TestTruncateStructurally_ResultStillOverCapReTruncates(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("z", 1500)
	var parts []part
	for i := 0; i < 8; i++ {
		parts = append(parts, customerPart("c"+string(rune('0'+i)), huge))
	}
	conv := convWithParts(parts)
	cr := buildContent(conv)
	if len(cr.text) > maxContentLen+len(" [truncated]") {
		t.Errorf("content too long after structural truncation: %d", len(cr.text))
	}
	if !strings.HasSuffix(cr.text, " [truncated]") {
		t.Errorf("expected [truncated] suffix, got tail %q", cr.text[len(cr.text)-30:])
	}
}

// TestResolveSourceUser_SeedNameFallback covers the seed-author-name leg
// (contact empty, seed email empty, seed name present).
func TestResolveSourceUser_SeedNameFallback(t *testing.T) {
	t.Parallel()
	conv := conversation{}
	conv.Source.Author = partAuthor{Type: "user", Name: "Seed Name"}
	if got := resolveSourceUser(intercomContact{}, conv); got != "Seed Name" {
		t.Errorf("resolveSourceUser = %q, want Seed Name", got)
	}
}

// ---------------------------------------------------------------------------
// intercom.go — lifecycle
// ---------------------------------------------------------------------------

func TestAdapterLifecycle_StartIdempotentAndShutdown(t *testing.T) {
	_, _, _, deps := buildDeps(t)

	// Block the poll loop's first pass so Start/Shutdown timing is
	// deterministic: List blocks until the ticker seam releases it.
	origTicker := newPollTicker
	tickC := make(chan time.Time)
	newPollTicker = func(time.Duration) (<-chan time.Time, func()) { return tickC, func() {} }
	t.Cleanup(func() { newPollTicker = origTicker })

	a := NewAdapter().(*adapter)
	if got := a.ShutdownTimeout(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", got)
	}
	if err := a.Start(context.Background(), deps); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	// Second Start is a no-op while running.
	if err := a.Start(context.Background(), deps); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	// Shutdown with no running loop is also a no-op.
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("idle Shutdown() error = %v", err)
	}
}

func TestAdapterShutdown_ContextExpiry(t *testing.T) {
	a := NewAdapter().(*adapter)
	// Simulate a wedged poll loop: a waitgroup that never drains.
	a.wg.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() error = %v, want deadline exceeded", err)
	}
	a.wg.Done()
}

// ---------------------------------------------------------------------------
// poll.go — loop dispatch, bookkeeping, persistence valves
// ---------------------------------------------------------------------------

// TestPollLoop_TickSyncNowAndCancel drives one ticker tick, one sync-now
// dispatch, and the ctx-done exit through the real pollLoop.
func TestPollLoop_TickSyncNowAndCancel(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{
		pages: []conversationPage{{Conversations: []conversation{}}},
	})
	a := buildTestAdapter(fake, deps)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	origTicker := newPollTicker
	tickC := make(chan time.Time)
	stopped := false
	newPollTicker = func(time.Duration) (<-chan time.Time, func()) {
		return tickC, func() { stopped = true }
	}
	t.Cleanup(func() { newPollTicker = origTicker })

	ctx, cancel := context.WithCancel(context.Background())
	a.wg.Add(1)
	var loopDone sync.WaitGroup
	loopDone.Add(1)
	go func() { defer loopDone.Done(); a.pollLoop(ctx) }()

	tickC <- time.Time{}       // release one ticker pass
	a.syncNow <- src.ID        // dispatch a sync-now for the real source
	a.syncNow <- "missing-src" // Get() fails → warn path
	tickC <- time.Time{}       // drain: proves both syncNow sends were consumed
	cancel()                   // ctx done → loop exits
	loopDone.Wait()

	if !stopped {
		t.Error("ticker stop func not called on loop exit")
	}
	if len(ingestFake.Calls) != 0 {
		t.Errorf("no conversations expected, got %d ingests", len(ingestFake.Calls))
	}
	if len(fake.capturedStarts) < 3 {
		t.Errorf("search calls = %d, want >= 3 (2 loop passes + 1 sync-now)", len(fake.capturedStarts))
	}
}

// TestPollAllSources_SkipsDisabledAndBackoff covers the enabled filter,
// the backoff skip, and the List error path of pollAllSources.
func TestPollAllSources_SkipsDisabledAndBackoff(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}

	disabled := testSource(t, sources, deps.Secrets, cfg, 0)
	disabled.ID, disabled.Enabled = "src-disabled", false
	sources.Put("t2", disabled)

	backedOff := testSource(t, sources, deps.Secrets, cfg, 0)
	backedOff.ID = "src-backoff"
	sources.Put("t3", backedOff)
	a.failureCount[backedOff.ID] = 5
	a.lastAttemptAt[backedOff.ID] = nowFn()

	a.pollAllSources(context.Background())
	// Only src-1 (enabled, no backoff) reaches the API.
	if len(fake.capturedStarts) != 1 {
		t.Errorf("search calls = %d, want 1 (disabled + backed-off skipped)", len(fake.capturedStarts))
	}
	// Cancelled ctx exits before polling anything further.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(fake.capturedStarts)
	a.pollAllSources(cancelled)
	if len(fake.capturedStarts) != before {
		t.Errorf("cancelled ctx still polled sources")
	}
}

type listErrSources struct {
	*fakeSourcesWithConfig
}

func (listErrSources) List(context.Context, string) ([]inbound.Source, error) {
	return nil, errors.New("list boom")
}

func TestPollAllSources_ListError(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	deps.Sources = listErrSources{sources}
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	a.pollAllSources(context.Background()) // must not panic; warn-and-return
	if len(fake.capturedStarts) != 0 {
		t.Errorf("search calls = %d, want 0 on list failure", len(fake.capturedStarts))
	}
}

func TestPollSingleSource_WrongChannelAndGetError(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"), WorkspaceID: "ws-1",
	}
	other := testSource(t, sources, deps.Secrets, cfg, 0)
	other.ID, other.Channel = "src-other", "zendesk"
	sources.Put("t4", other)

	a.pollSingleSource(context.Background(), "src-other") // wrong channel
	a.pollSingleSource(context.Background(), "nope")      // Get error
	if len(fake.capturedStarts) != 0 {
		t.Errorf("search calls = %d, want 0", len(fake.capturedStarts))
	}
}

func TestMarkPollBookkeeping_NilMapInit(t *testing.T) {
	t.Parallel()
	// Zero-value adapter (nil maps) — each mark* must lazily init.
	a := ptrext.Of(adapter{})
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	a.markPollAttempt("s1", now)
	if got := a.lastAttemptAt["s1"]; !got.Equal(now) {
		t.Errorf("lastAttemptAt = %v, want %v", got, now)
	}
	a.markPollFailure("s1")
	a.markPollFailure("s1")
	if a.failureCount["s1"] != 2 {
		t.Errorf("failureCount = %d, want 2", a.failureCount["s1"])
	}
	a.markPollSuccess("s1", now)
	if _, ok := a.failureCount["s1"]; ok {
		t.Error("markPollSuccess must clear the failure counter")
	}
	if got := a.lastSuccessAt["s1"]; !got.Equal(now) {
		t.Errorf("lastSuccessAt = %v, want %v", got, now)
	}
	// rememberProcessed with nil maps.
	b := ptrext.Of(adapter{})
	b.rememberProcessed("s1", conversation{ID: "9", UpdatedAt: 42})
	if b.processedKeys["s1"] == nil || len(b.processedKeys["s1"]) != 1 {
		t.Errorf("processedKeys not initialized: %+v", b.processedKeys)
	}
}

// TestPersistSyncStats_FailureValves covers the marshal-error, encrypt-
// error, update-error, and non-updater store legs.
func TestPersistSyncStats_FailureValves(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{})
	a := buildTestAdapter(fake, deps)
	cfg := Config{Version: ConfigVersion, Region: "us", WorkspaceID: "ws-1"}

	// Non-updater store: quiet no-op.
	plainDeps := deps
	plainDeps.Sources = ptrext.Of(inboundFakeSourcesNoUpdate{inner: sources})
	aPlain := buildTestAdapter(fake, plainDeps)
	aPlain.persistSyncStats(context.Background(), "src-1", cfg, "test")

	// Marshal failure.
	origMarshal := jsonMarshal
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
	a.persistSyncStats(context.Background(), "src-1", cfg, "test")
	jsonMarshal = origMarshal

	// Encrypt failure.
	encDeps := deps
	encDeps.Secrets = failingSecrets{}
	aEnc := buildTestAdapter(fake, encDeps)
	aEnc.persistSyncStats(context.Background(), "src-1", cfg, "test")

	// UpdateConfig failure.
	sources.updateErr = errors.New("update boom")
	a.persistSyncStats(context.Background(), "src-1", cfg, "test")
	sources.updateErr = nil

	// Success leg records the update.
	a.persistSyncStats(context.Background(), "src-1", cfg, "test")
	if len(sources.configUpdates) == 0 {
		t.Error("expected a successful UpdateConfig call")
	}
}

// inboundFakeSourcesNoUpdate hides UpdateConfig so the interface assertion
// in persistSyncStats fails.
type inboundFakeSourcesNoUpdate struct{ inner inbound.SourceStore }

func (s *inboundFakeSourcesNoUpdate) Get(ctx context.Context, id string) (inbound.Source, error) {
	return s.inner.Get(ctx, id)
}

func (s *inboundFakeSourcesNoUpdate) GetBySlugs(ctx context.Context, tenantSlug, channel, sourceSlug string) (inbound.Source, error) {
	return s.inner.GetBySlugs(ctx, tenantSlug, channel, sourceSlug)
}

func (s *inboundFakeSourcesNoUpdate) List(ctx context.Context, channel string) ([]inbound.Source, error) {
	return s.inner.List(ctx, channel)
}

func (s *inboundFakeSourcesNoUpdate) UpdateState(ctx context.Context, id string, st inbound.SourceState) error {
	return s.inner.UpdateState(ctx, id, st)
}

func (s *inboundFakeSourcesNoUpdate) SetEnabled(ctx context.Context, id string, enabled bool, reason string) error {
	return s.inner.SetEnabled(ctx, id, enabled, reason)
}

type failingSecrets struct{}

func (failingSecrets) Encrypt([]byte) ([]byte, error) { return nil, errors.New("encrypt boom") }
func (failingSecrets) Decrypt([]byte) ([]byte, error) { return nil, errors.New("decrypt boom") }

// TestFetchSearchPage_CursorRetryError covers the leg where the cursor
// restart itself also fails.
func TestFetchSearchPage_CursorRetryError(t *testing.T) {
	_, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{searchErr: apiError{Method: "/conversations/search", Status: 422, Code: "bad_cursor"}})
	a := buildTestAdapter(fake, deps)
	_, dropped, err := a.fetchSearchPage(context.Background(), fake, 0, 100, "stale-cursor", "src-1", "test")
	if !dropped {
		t.Error("expected cursorDropped=true for a permanent cursor error")
	}
	if err == nil {
		t.Error("expected the restart error to propagate")
	}
	if len(fake.capturedCursors) != 2 || fake.capturedCursors[1] != "" {
		t.Errorf("capturedCursors = %v, want [stale-cursor \"\"]", fake.capturedCursors)
	}
}

func TestIsTransientDetailError_DecodeAnd5xx(t *testing.T) {
	t.Parallel()
	if isTransientDetailError(decodeError{Method: "/conversations/1", Err: errors.New("bad json")}) {
		t.Error("decode errors are deterministic — not transient")
	}
	if !isTransientDetailError(apiError{Status: 503}) {
		t.Error("5xx API errors are transient")
	}
	if isTransientDetailError(apiError{Status: 404}) {
		t.Error("4xx API errors are permanent per-conversation")
	}
	if !isTransientDetailError(rateLimitError{RetryAfter: time.Second}) {
		t.Error("rate limits are transient")
	}
	if !isTransientDetailError(errors.New("dial tcp: timeout")) {
		t.Error("network errors are transient")
	}
}

// TestResolveCompany_FetchFailureNegativeCaches covers the GetCompany
// failure leg: bare ref is returned and negative-cached for the tick.
func TestResolveCompany_FetchFailureNegativeCaches(t *testing.T) {
	_, _, _, deps := buildDeps(t)
	fake := ptrext.Of(fakeAPIClient{companyErr: errors.New("company boom")})
	a := buildTestAdapter(fake, deps)
	cache := map[string]intercomCompany{}
	ref := ptrext.Of(intercomCompany{ID: "co-1", Name: "Bare Ref"})
	got := a.resolveCompany(context.Background(), fake, ref, cache, "test")
	if got == nil || got.Name != "Bare Ref" {
		t.Errorf("resolveCompany = %+v, want bare ref back", got)
	}
	if _, ok := cache["co-1"]; !ok {
		t.Error("failure must negative-cache the bare ref")
	}
	// Second call hits the cache, not the API.
	before := fake.companyCalls
	_ = a.resolveCompany(context.Background(), fake, ref, cache, "test")
	if fake.companyCalls != before {
		t.Error("negative cache not used on second resolve")
	}
}

// TestResolveContacts_FailureLegs covers both SearchContacts error
// modes: transient failures propagate (retry the conversation next tick
// instead of ingesting under a drifted identity); permanent failures
// degrade to the seed-author fallback.
func TestResolveContacts_FailureLegs(t *testing.T) {
	_, _, _, deps := buildDeps(t)
	conv := fullConversation()

	// Transient (network) → error propagates.
	fakeTransient := ptrext.Of(fakeAPIClient{contactsErr: errors.New("dial tcp: timeout")})
	a := buildTestAdapter(fakeTransient, deps)
	if got, err := a.resolveContacts(context.Background(), fakeTransient, conv, "test"); err == nil || got != nil {
		t.Errorf("transient failure = (%v, %v), want (nil, error)", got, err)
	}

	// Permanent (plan-gated 403) → degrade to nil map, no error.
	fakePermanent := ptrext.Of(fakeAPIClient{contactsErr: apiError{Method: "/contacts/search", Status: 403, Code: "api_plan_restricted"}})
	a2 := buildTestAdapter(fakePermanent, deps)
	if got, err := a2.resolveContacts(context.Background(), fakePermanent, conv, "test"); err != nil || got != nil {
		t.Errorf("permanent failure = (%v, %v), want (nil, nil)", got, err)
	}
}

// ---------------------------------------------------------------------------
// public.go
// ---------------------------------------------------------------------------

func TestAuthTest_PublicWrapper(t *testing.T) {
	origFactory := newAPIClient
	fake := ptrext.Of(fakeAPIClient{authTestResult: intercomAccount{WorkspaceID: "ws-9", WorkspaceName: "Acme"}})
	newAPIClient = func(region, token string) apiClient {
		if region != "eu" || token != "tok-1" {
			t.Errorf("newAPIClient(%q, %q), want (eu, tok-1)", region, token)
		}
		return fake
	}
	t.Cleanup(func() { newAPIClient = origFactory })

	acct, err := AuthTest(context.Background(), "eu", "tok-1")
	if err != nil {
		t.Fatalf("AuthTest() error = %v", err)
	}
	if acct.WorkspaceID != "ws-9" {
		t.Errorf("WorkspaceID = %q, want ws-9", acct.WorkspaceID)
	}
}

func TestAPIErrorStatus(t *testing.T) {
	t.Parallel()
	status, code, ok := APIErrorStatus(apiError{Status: 401, Code: "unauthorized"})
	if !ok || status != 401 || code != "unauthorized" {
		t.Errorf("APIErrorStatus(api) = (%d, %q, %t)", status, code, ok)
	}
	status, code, ok = APIErrorStatus(errors.New("plain"))
	if ok || status != 0 || code != "" {
		t.Errorf("APIErrorStatus(plain) = (%d, %q, %t), want zero values", status, code, ok)
	}
}

// TestTruncateStructurally_KeepsAgentRepliesInKeptRanges: agent messages
// between kept customer messages must survive structural truncation, and
// the omission marker counts every dropped message.
func TestTruncateStructurally_KeepsAgentRepliesInKeptRanges(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 700)
	var parts []part
	// 8 customer messages, each followed by an agent reply.
	for i := 0; i < 8; i++ {
		parts = append(parts,
			customerPart(fmt.Sprintf("c%d", i), fmt.Sprintf("question %d %s", i, long)),
			agentPart(fmt.Sprintf("a%d", i), fmt.Sprintf("answer %d", i)),
		)
	}
	conv := convWithParts(parts)
	cr := buildContent(conv)

	// Head range: customers 0-2 AND the agent replies between them.
	for _, want := range []string{"question 0", "answer 0", "question 1", "answer 1", "question 2"} {
		if !strings.Contains(cr.text, want) {
			t.Errorf("kept head range missing %q", want)
		}
	}
	// Tail range: customers 6-7 with the agent reply between them.
	for _, want := range []string{"question 6", "answer 6", "question 7"} {
		if !strings.Contains(cr.text, want) {
			t.Errorf("kept tail range missing %q", want)
		}
	}
	// Omitted middle: customers 3-5 and their agent replies = the
	// marker counts messages, not customer messages.
	if strings.Contains(cr.text, "question 4") {
		t.Error("omitted middle leaked into the transcript")
	}
	if !strings.Contains(cr.text, "[... 7 messages omitted ...]") {
		t.Errorf("omission marker must count all dropped messages, text tail: %q",
			cr.text[max(0, len(cr.text)-200):])
	}
}

// TestStripHTMLTags_EntitiesWithoutTags: entity unescaping must not
// depend on the body containing a tag.
func TestStripHTMLTags_EntitiesWithoutTags(t *testing.T) {
	t.Parallel()
	if got := stripHTMLTags("Tom &amp; Jerry &gt; others"); got != "Tom & Jerry > others" {
		t.Errorf("stripHTMLTags = %q, want entities unescaped without tags", got)
	}
	if got := stripHTMLTags("no entities here"); got != "no entities here" {
		t.Errorf("plain text changed: %q", got)
	}
}

// TestTruncateStructurally_AllCustomersKeptFallsBack: agent-heavy
// threads whose customer messages all fit in the keep budget fall back
// to byte truncation (nothing structural to omit).
func TestTruncateStructurally_AllCustomersKeptFallsBack(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("w", 900)
	parts := []part{
		agentPart("a0", "intro "+long),
		agentPart("a1", "more "+long),
	}
	for i := 0; i < 5; i++ {
		parts = append(parts, customerPart(fmt.Sprintf("c%d", i), fmt.Sprintf("q%d %s", i, long)))
	}
	conv := convWithParts(parts)
	cr := buildContent(conv)
	if !strings.HasSuffix(cr.text, " [truncated]") {
		t.Errorf("expected byte-truncation fallback, tail: %q", cr.text[len(cr.text)-30:])
	}
	if strings.Contains(cr.text, "omitted") {
		t.Errorf("no omission marker expected when every customer message is kept")
	}
}
