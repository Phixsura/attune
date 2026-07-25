// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/infra/intercomclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// fakeAPIClient implements apiClient for poll tests.
type fakeAPIClient struct {
	authTestResult intercomAccount
	authTestErr    error

	// pages are returned in order by SearchConversations; the last page
	// repeats if more calls arrive.
	pages     []conversationPage
	searchErr error
	pageCalls int

	// capturedStarts records the startTime passed to each search call.
	capturedStarts []int64

	detailByID  map[string]conversation
	detailErr   error
	detailCalls int

	contactsResult []intercomContact
	contactsErr    error

	companyByID  map[string]intercomCompany
	companyErr   error
	companyCalls int

	adminsResult []intercomAdmin
	adminsErr    error
	adminCalls   int

	// rateBudget simulates X-RateLimit-Remaining. The zero value maps to
	// "unseen" (-1) so test literals that don't care are unaffected by
	// the proactive throttle.
	rateBudget int64
}

func (f *fakeAPIClient) ListAdmins(_ context.Context) ([]intercomAdmin, error) {
	f.adminCalls++
	if f.adminsErr != nil {
		return nil, f.adminsErr
	}
	return f.adminsResult, nil
}

func (f *fakeAPIClient) GetCompany(_ context.Context, id string) (intercomCompany, error) {
	f.companyCalls++
	if f.companyErr != nil {
		return intercomCompany{}, f.companyErr
	}
	if c, ok := f.companyByID[id]; ok {
		return c, nil
	}
	return intercomCompany{}, apiError{Method: "/companies/" + id, Status: 404, Code: "not_found"}
}

func (f *fakeAPIClient) RateBudget() int64 {
	if f.rateBudget == 0 {
		return -1
	}
	return f.rateBudget
}

func (f *fakeAPIClient) AuthTest(_ context.Context) (intercomAccount, error) {
	return f.authTestResult, f.authTestErr
}

func (f *fakeAPIClient) SearchConversations(_ context.Context, startTime, _ int64, _ string) (conversationPage, error) {
	f.capturedStarts = append(f.capturedStarts, startTime)
	if f.searchErr != nil {
		return conversationPage{}, f.searchErr
	}
	if len(f.pages) == 0 {
		return conversationPage{}, nil
	}
	idx := f.pageCalls
	if idx >= len(f.pages) {
		idx = len(f.pages) - 1
	}
	f.pageCalls++
	return f.pages[idx], nil
}

func (f *fakeAPIClient) GetConversation(_ context.Context, id string) (conversation, error) {
	f.detailCalls++
	if f.detailErr != nil {
		return conversation{}, f.detailErr
	}
	if c, ok := f.detailByID[id]; ok {
		return c, nil
	}
	return conversation{}, apiError{Method: "/conversations/" + id, Status: 404, Code: "not_found"}
}

func (f *fakeAPIClient) SearchContacts(_ context.Context, _ []string) ([]intercomContact, error) {
	return f.contactsResult, f.contactsErr
}

// fakeSourcesWithConfig wraps FakeSources and records UpdateConfig calls.
type fakeSourcesWithConfig struct {
	*inboundtest.FakeSources
	configUpdates map[string][]byte
}

func newFakeSourcesWithConfig() *fakeSourcesWithConfig {
	return &fakeSourcesWithConfig{ // ptrext:allow test-fixture
		FakeSources:   inboundtest.NewFakeSources(),
		configUpdates: map[string][]byte{},
	}
}

func (f *fakeSourcesWithConfig) UpdateConfig(_ context.Context, id string, config []byte) error {
	f.configUpdates[id] = config
	return nil
}

// buildTestConfig creates a double-encrypted config blob for tests.
func buildTestConfig(t *testing.T, cfg Config, secrets inbound.SecretStore) []byte {
	t.Helper()
	raw, err := json.Marshal(cfg) // ptrext:allow json-marshal
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	encrypted, err := secrets.Encrypt(raw)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	return encrypted
}

// buildTestAdapter creates an adapter wired with fakes for unit testing.
func buildTestAdapter(fake *fakeAPIClient, deps inbound.Deps) *adapter {
	a := ptrext.Of(adapter{
		newClient: func(_, _ string) apiClient {
			return fake
		},
		lastSuccessAt: map[string]time.Time{},
		failureCount:  map[string]int{},
		syncNow:       make(chan string, 1),
	})
	a.deps = deps
	return a
}

func buildDeps(t *testing.T) (*fakeSourcesWithConfig, *inboundtest.FakeIngest, *inboundtest.FakeMetrics, inbound.Deps) {
	t.Helper()
	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: inboundtest.FakeSecrets{},
		Metrics: metrics,
	}
	return sources, ingestFake, metrics, deps
}

func encryptedToken(t *testing.T, secrets inbound.SecretStore, token string) []byte {
	t.Helper()
	enc, err := secrets.Encrypt([]byte(token))
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	return enc
}

func testSource(t *testing.T, sources *fakeSourcesWithConfig, secrets inbound.SecretStore, cfg Config, lastUID int64) inbound.Source {
	t.Helper()
	src := inbound.Source{
		ID: "src-1", TenantID: "tenant-1", Channel: channelName,
		Name: "Acme Intercom", Slug: "acme-intercom",
		Config: buildTestConfig(t, cfg, secrets), Enabled: true,
		State: inbound.SourceState{LastUID: lastUID},
	}
	sources.Put("t1", src)
	return src
}

func fullConversation() conversation {
	return conversation{
		ID: "101", Title: "Export bug", State: "open",
		CreatedAt: 1700000000, UpdatedAt: 1700000500,
		AdminAssigneeID: 9, TeamAssigneeID: 5,
		Source: intercomclient.ConversationSource{
			Type: "conversation", Subject: "Export",
			Body:   "PDF export is broken",
			Author: partAuthor{Type: "user", ID: "c1", Name: "Alice", Email: "alice@acme.com"},
		},
		Contacts: intercomclient.ConversationContacts{
			Contacts: []contactRef{{ID: "c1", ExternalID: "ext-1"}},
		},
		Tags: intercomclient.TagList{Tags: []intercomclient.Tag{{ID: "t1", Name: "bug"}}},
		Parts: intercomclient.ConversationParts{Parts: []part{
			{ID: "p1", PartType: "comment", Body: "Thanks, checking", CreatedAt: 1700000100, Author: partAuthor{Type: "admin", ID: "a1", Name: "Bob"}},
			{ID: "p2", PartType: "note", Body: "internal-only note", CreatedAt: 1700000200, Author: partAuthor{Type: "admin", ID: "a1"}},
			{ID: "p3", PartType: "comment", Body: "Still broken", CreatedAt: 1700000300, Author: partAuthor{Type: "user", ID: "c1", Name: "Alice"}},
		}},
	}
}

func TestPollSource_HappyPath(t *testing.T) {
	fixedTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	sources, ingestFake, metrics, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1", StartFrom: "now",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	summary := fullConversation()
	summary.Parts = intercomclient.ConversationParts{} // search results carry no parts
	fake := &fakeAPIClient{                            // ptrext:allow test-fixture
		pages: []conversationPage{{
			Conversations: []conversation{summary},
		}},
		detailByID:     map[string]conversation{"101": fullConversation()},
		contactsResult: []intercomContact{{ID: "c1", ExternalID: "ext-1", Email: "alice@acme.com", Name: "Alice", Role: "user"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest call, got %d", len(ingestFake.Calls))
	}
	verifyHappyPathIngest(t, ingestFake.Calls[0])
	verifyHappyPathState(t, sources, metrics, deps)
}

func verifyHappyPathIngest(t *testing.T, call inboundtest.FakeIngestCall) {
	t.Helper()
	if call.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q", call.TenantID)
	}
	if call.In.Source != channelName {
		t.Errorf("Source = %q", call.In.Source)
	}
	if call.In.SourceUser != "alice@acme.com" {
		t.Errorf("SourceUser = %q", call.In.SourceUser)
	}
	for _, want := range []string{"Export bug", "[customer] PDF export is broken", "[agent] Thanks, checking"} {
		if !strings.Contains(call.In.Content, want) {
			t.Errorf("Content missing %q: %q", want, call.In.Content)
		}
	}
	if strings.Contains(call.In.Content, "internal-only note") {
		t.Errorf("Content leaked internal note: %q", call.In.Content)
	}
	if call.In.PageURL != "https://app.intercom.com/a/inbox/ws1/inbox/conversation/101" {
		t.Errorf("PageURL = %q", call.In.PageURL)
	}
	if call.In.IdempotencyKey != "intercom_ws1_101_1700000500" {
		t.Errorf("IdempotencyKey = %q", call.In.IdempotencyKey)
	}
	meta := call.In.SourceMeta
	if meta["intercom_conversation_id"] != "101" {
		t.Errorf("meta conversation_id = %v", meta["intercom_conversation_id"])
	}
	if meta["intercom_contact_external_id"] != "ext-1" {
		t.Errorf("meta contact_external_id = %v", meta["intercom_contact_external_id"])
	}
	if meta["intercom_state"] != "open" {
		t.Errorf("meta state = %v", meta["intercom_state"])
	}
}

func verifyHappyPathState(t *testing.T, sources *fakeSourcesWithConfig, metrics *inboundtest.FakeMetrics, deps inbound.Deps) {
	t.Helper()
	// Watermark advanced to the conversation's updated_at.
	stored, err := sources.Get(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State.LastUID != 1700000500 {
		t.Errorf("LastUID = %d, want 1700000500", stored.State.LastUID)
	}

	foundOK := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|ok") {
			foundOK = true
		}
	}
	if !foundOK {
		t.Errorf("no ok metric in %v", metrics.Totals)
	}

	// Sync stats persisted with backfill done.
	blob, ok := sources.configUpdates["src-1"]
	if !ok {
		t.Fatal("expected UpdateConfig for sync stats")
	}
	decoded, _ := deps.Secrets.Decrypt(blob)
	var storedCfg Config
	if err := json.Unmarshal(decoded, &storedCfg); err != nil { // ptrext:allow json-unmarshal
		t.Fatalf("unmarshal stored config: %v", err)
	}
	if !storedCfg.SyncStats.BackfillDone {
		t.Error("BackfillDone should be true after end of results")
	}
	if storedCfg.SyncStats.ConversationsSynced != 1 {
		t.Errorf("ConversationsSynced = %d", storedCfg.SyncStats.ConversationsSynced)
	}
}

func TestPollSource_DayFlooredQueryStart(t *testing.T) {
	fixedTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	sources, _, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	// Watermark mid-day: 2023-11-14T22:13:20Z = 1700000000.
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)
	fake := &fakeAPIClient{pages: []conversationPage{{}}} // ptrext:allow test-fixture

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(fake.capturedStarts) != 1 {
		t.Fatalf("expected 1 search call, got %d", len(fake.capturedStarts))
	}
	want := (int64(1700000000) / 86400) * 86400
	if fake.capturedStarts[0] != want {
		t.Errorf("query start = %d, want day-floored %d", fake.capturedStarts[0], want)
	}
}

func TestPollSource_ClientSideWatermarkFilter(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000400)

	older := fullConversation()
	older.ID = "100"
	older.UpdatedAt = 1700000300 // <= watermark, must be skipped
	newer := fullConversation()
	newer.UpdatedAt = 1700000500

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:      []conversationPage{{Conversations: []conversation{older, newer}}},
		detailByID: map[string]conversation{"101": fullConversation()},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest (older filtered), got %d", len(ingestFake.Calls))
	}
	if ingestFake.Calls[0].In.SourceMeta["intercom_conversation_id"] != "101" {
		t.Errorf("wrong conversation ingested")
	}
}

func TestPollSource_StateFilterAdvancesWatermark(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
		FilterStates:         []string{"closed"},
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	openConv := fullConversation() // state=open → filtered
	fake := &fakeAPIClient{        // ptrext:allow test-fixture
		pages: []conversationPage{{Conversations: []conversation{openConv}}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 0 {
		t.Fatalf("expected 0 ingests, got %d", len(ingestFake.Calls))
	}
	if fake.detailCalls != 0 {
		t.Errorf("filtered conversation should not consume detail budget")
	}
	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.State.LastUID != 1700000500 {
		t.Errorf("watermark should advance past filtered items, got %d", stored.State.LastUID)
	}
}

func TestPollSource_DetailBudgetStopsWatermark(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
		MaxDetailFetches:     1,
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	c1 := fullConversation()
	c1.ID = "101"
	c1.UpdatedAt = 1700000500
	c2 := fullConversation()
	c2.ID = "102"
	c2.UpdatedAt = 1700000600

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages: []conversationPage{{Conversations: []conversation{c1, c2}}},
		detailByID: map[string]conversation{
			"101": c1, "102": c2,
		},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest under budget=1, got %d", len(ingestFake.Calls))
	}
	stored, _ := sources.Get(context.Background(), "src-1")
	// Watermark must stop at c1 so c2 is re-covered next tick.
	if stored.State.LastUID != 1700000500 {
		t.Errorf("LastUID = %d, want 1700000500 (budget boundary)", stored.State.LastUID)
	}
}

func TestPollSource_DetailFailureDegradesToSummary(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	summary := fullConversation()
	summary.Parts = intercomclient.ConversationParts{}
	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:     []conversationPage{{Conversations: []conversation{summary}}},
		detailErr: apiError{Method: "/conversations/101", Status: 404, Code: "not_found"},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Ingest still happens from the summary (seed message carries signal).
	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest from summary fallback, got %d", len(ingestFake.Calls))
	}
	if !strings.Contains(ingestFake.Calls[0].In.Content, "PDF export is broken") {
		t.Errorf("summary content missing: %q", ingestFake.Calls[0].In.Content)
	}
	// Source stays enabled — per-item degradation.
	stored, _ := sources.Get(context.Background(), "src-1")
	if !stored.Enabled {
		t.Error("source must stay enabled on per-conversation detail failure")
	}
}

func TestPollSource_AuthFailureDisablesSource(t *testing.T) {
	sources, _, metrics, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		searchErr: apiError{Method: "/conversations/search", Status: 401, Code: "unauthorized"},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.Enabled {
		t.Error("source should be disabled on permanent auth failure")
	}
	foundAuthErr := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|auth_err") {
			foundAuthErr = true
		}
	}
	if !foundAuthErr {
		t.Errorf("no auth_err metric in %v", metrics.Totals)
	}
}

func TestPollSource_TransientFailureKeepsSourceEnabled(t *testing.T) {
	sources, _, metrics, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		searchErr: apiError{Method: "/conversations/search", Status: 502, Code: "bad gateway"},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	stored, _ := sources.Get(context.Background(), "src-1")
	if !stored.Enabled {
		t.Error("source should stay enabled on transient failure")
	}
	if stored.State.LastError == "" {
		t.Error("LastError should record the transient failure")
	}
	foundTransient := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|transient_err") {
			foundTransient = true
		}
	}
	if !foundTransient {
		t.Errorf("no transient_err metric in %v", metrics.Totals)
	}
}

func TestPollSource_RateLimitSleepsAndRetainsSource(t *testing.T) {
	sources, _, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 0)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		searchErr: rateLimitError{Method: "/conversations/search", RetryAfter: time.Millisecond},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.Enabled != true {
		t.Error("rate limit must not disable the source")
	}
	if !strings.Contains(stored.State.LastError, "rate limited") {
		t.Errorf("LastError = %q", stored.State.LastError)
	}
}

func TestPollSource_DecryptFailure(t *testing.T) {
	sources, _, metrics, deps := buildDeps(t)
	src := inbound.Source{
		ID: "src-bad", TenantID: "tenant-1", Channel: channelName,
		Name: "Broken", Slug: "broken",
		Config: []byte{0xFF}, Enabled: true,
	}
	sources.Put("t1", src)

	a := buildTestAdapter(&fakeAPIClient{}, deps) // ptrext:allow test-fixture
	a.pollSource(context.Background(), src)

	stored, _ := sources.Get(context.Background(), "src-bad")
	if stored.State.LastError != "decrypt config" {
		t.Errorf("LastError = %q", stored.State.LastError)
	}
	foundInternal := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|internal_err") {
			foundInternal = true
		}
	}
	if !foundInternal {
		t.Errorf("no internal_err metric in %v", metrics.Totals)
	}
}

func TestPollSource_DuplicateIngestCountsValidateErr(t *testing.T) {
	sources, ingestFake, metrics, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	ingestFake.NextErr = errors.New("idempotency key used with different request")
	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:      []conversationPage{{Conversations: []conversation{fullConversation()}}},
		detailByID: map[string]conversation{"101": fullConversation()},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	foundValidate := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|validate_err") {
			foundValidate = true
		}
	}
	if !foundValidate {
		t.Errorf("no validate_err metric in %v", metrics.Totals)
	}
}

func TestPollSource_MultiPagePagination(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	c1 := fullConversation()
	c1.ID = "101"
	c1.UpdatedAt = 1700000500
	c2 := fullConversation()
	c2.ID = "102"
	c2.UpdatedAt = 1700000600

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages: []conversationPage{
			{Conversations: []conversation{c1}, StartingAfter: "next"},
			{Conversations: []conversation{c2}},
		},
		detailByID: map[string]conversation{"101": c1, "102": c2},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 2 {
		t.Fatalf("expected 2 ingests across pages, got %d", len(ingestFake.Calls))
	}
	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.State.LastUID != 1700000600 {
		t.Errorf("LastUID = %d, want 1700000600", stored.State.LastUID)
	}
}

func TestPollSource_RateBudgetFloorStopsTick(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	c1 := fullConversation()
	c1.ID = "101"
	c1.UpdatedAt = 1700000500
	c2 := fullConversation()
	c2.ID = "102"
	c2.UpdatedAt = 1700000600

	// Two pages available, but budget below floor after page 1 → the
	// tick must stop before requesting page 2.
	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages: []conversationPage{
			{Conversations: []conversation{c1}, StartingAfter: "next"},
			{Conversations: []conversation{c2}},
		},
		detailByID: map[string]conversation{"101": c1, "102": c2},
		rateBudget: rateBudgetFloor - 1,
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if fake.pageCalls != 1 {
		t.Errorf("pageCalls = %d, want 1 (stopped by budget floor)", fake.pageCalls)
	}
	if len(ingestFake.Calls) != 1 {
		t.Errorf("ingests = %d, want 1", len(ingestFake.Calls))
	}
	// Watermark advanced only past processed items; c2 re-covered next tick.
	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.State.LastUID != 1700000500 {
		t.Errorf("LastUID = %d, want 1700000500", stored.State.LastUID)
	}
	// Source stays enabled and healthy — this is throttling, not an error.
	if !stored.Enabled {
		t.Error("budget floor must not disable the source")
	}
}

func TestPollSource_TagFilterSkipsDetailBudget(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
		FilterExcludeTags:    []string{"bug"}, // fullConversation carries tag "bug"
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages: []conversationPage{{Conversations: []conversation{fullConversation()}}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 0 {
		t.Fatalf("expected 0 ingests, got %d", len(ingestFake.Calls))
	}
	if fake.detailCalls != 0 {
		t.Errorf("tag-filtered conversation should not consume detail budget")
	}
	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.State.LastUID != 1700000500 {
		t.Errorf("watermark should advance past filtered items, got %d", stored.State.LastUID)
	}
}

func TestPollSource_CompanyProfileResolvedAndCached(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	// Two conversations sharing one company → exactly one GetCompany call.
	c1 := fullConversation()
	c1.ID = "101"
	c1.UpdatedAt = 1700000500
	c1.Company = &intercomclient.Company{ID: "co-9", Name: "Customer Co"} // ptrext:allow test-fixture
	c2 := fullConversation()
	c2.ID = "102"
	c2.UpdatedAt = 1700000600
	c2.Company = &intercomclient.Company{ID: "co-9", Name: "Customer Co"} // ptrext:allow test-fixture

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:      []conversationPage{{Conversations: []conversation{c1, c2}}},
		detailByID: map[string]conversation{"101": c1, "102": c2},
		companyByID: map[string]intercomCompany{
			"co-9": {
				ID: "co-9", Name: "Customer Co",
				MonthlySpend: 1200, Size: 85, Industry: "Software",
				Plan: intercomclient.CompanyPlan{ID: "p1", Name: "Pro"},
			},
		},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 2 {
		t.Fatalf("expected 2 ingests, got %d", len(ingestFake.Calls))
	}
	if fake.companyCalls != 1 {
		t.Errorf("companyCalls = %d, want 1 (per-tick cache)", fake.companyCalls)
	}
	meta := ingestFake.Calls[0].In.SourceMeta
	if meta["intercom_company_monthly_spend"] != 1200 {
		t.Errorf("monthly_spend = %v", meta["intercom_company_monthly_spend"])
	}
	if meta["intercom_company_plan"] != "Pro" {
		t.Errorf("plan = %v", meta["intercom_company_plan"])
	}
	if meta["intercom_company_size"] != 85 {
		t.Errorf("size = %v", meta["intercom_company_size"])
	}
	if meta["intercom_company_industry"] != "Software" {
		t.Errorf("industry = %v", meta["intercom_company_industry"])
	}
}

func TestPollSource_TeammateResolvedLazilyOncePerTick(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	// Two assigned conversations → exactly one ListAdmins call.
	c1 := fullConversation()
	c1.ID = "101"
	c1.UpdatedAt = 1700000500
	c1.AdminAssigneeID = 9
	c2 := fullConversation()
	c2.ID = "102"
	c2.UpdatedAt = 1700000600
	c2.AdminAssigneeID = 9

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:        []conversationPage{{Conversations: []conversation{c1, c2}}},
		detailByID:   map[string]conversation{"101": c1, "102": c2},
		adminsResult: []intercomAdmin{{ID: "9", Name: "Sam Support", Email: "sam@acme.example"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	if len(ingestFake.Calls) != 2 {
		t.Fatalf("expected 2 ingests, got %d", len(ingestFake.Calls))
	}
	if fake.adminCalls != 1 {
		t.Errorf("adminCalls = %d, want 1 (lazy, once per tick)", fake.adminCalls)
	}
	meta := ingestFake.Calls[0].In.SourceMeta
	if meta["intercom_teammate_name"] != "Sam Support" {
		t.Errorf("teammate_name = %v", meta["intercom_teammate_name"])
	}

	// Unassigned conversation → ListAdmins never called.
	sources2, ingest2, _, deps2 := buildDeps(t)
	c3 := fullConversation()
	c3.AdminAssigneeID = 0
	c3.TeamAssigneeID = 0
	src2 := testSource(t, sources2, deps2.Secrets, cfg, 1700000000)
	fake2 := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:      []conversationPage{{Conversations: []conversation{c3}}},
		detailByID: map[string]conversation{"101": c3},
	}
	a2 := buildTestAdapter(fake2, deps2)
	a2.pollSource(context.Background(), src2)
	if fake2.adminCalls != 0 {
		t.Errorf("adminCalls = %d, want 0 for unassigned conversations", fake2.adminCalls)
	}
	if _, ok := ingest2.Calls[0].In.SourceMeta["intercom_teammate_name"]; ok {
		t.Error("teammate_name should be absent when unassigned")
	}
}

func TestPollSource_CompanyResolutionFailureDegrades(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	c1 := fullConversation()
	c1.Company = &intercomclient.Company{ID: "co-gone", Name: "Ghost Co"} // ptrext:allow test-fixture

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:      []conversationPage{{Conversations: []conversation{c1}}},
		detailByID: map[string]conversation{"101": c1},
		companyErr: apiError{Method: "/companies/co-gone", Status: 404, Code: "not_found"},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Ingest proceeds with the embedded reference; no profile keys.
	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest, got %d", len(ingestFake.Calls))
	}
	meta := ingestFake.Calls[0].In.SourceMeta
	if meta["intercom_company_name"] != "Ghost Co" {
		t.Errorf("company_name = %v", meta["intercom_company_name"])
	}
	if _, ok := meta["intercom_company_monthly_spend"]; ok {
		t.Error("monthly_spend should be absent on resolution failure")
	}
}

func TestSeedWatermark(t *testing.T) {
	fixedTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	a := buildTestAdapter(&fakeAPIClient{}, inboundtest.DepsFor(nil, nil, nil)) // ptrext:allow test-fixture
	if got := a.seedWatermark(Config{StartFrom: "now"}); got != fixedTime.Add(-5*time.Minute).Unix() {
		t.Errorf("seedWatermark(now) = %d", got)
	}
	if got := a.seedWatermark(Config{StartFrom: "full"}); got != 0 {
		t.Errorf("seedWatermark(full) = %d", got)
	}
	if got := a.seedWatermark(Config{}); got != 0 {
		t.Errorf("seedWatermark(default) = %d", got)
	}
}

func TestShouldSkipBackoff(t *testing.T) {
	fixedTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	a := buildTestAdapter(&fakeAPIClient{}, inboundtest.DepsFor(nil, nil, nil)) // ptrext:allow test-fixture
	// Below threshold — never skips.
	a.failureCount["s"] = 2
	a.lastSuccessAt["s"] = fixedTime.Add(-time.Second)
	if a.shouldSkipBackoff("s") {
		t.Error("2 failures should not back off")
	}
	// 3 failures → 120s interval; 1s since success → skip.
	a.failureCount["s"] = 3
	if !a.shouldSkipBackoff("s") {
		t.Error("3 failures 1s after success should skip")
	}
	// Interval elapsed → no skip.
	a.lastSuccessAt["s"] = fixedTime.Add(-3 * time.Minute)
	if a.shouldSkipBackoff("s") {
		t.Error("elapsed interval should not skip")
	}
	// Success resets.
	a.markPollSuccess("s", fixedTime)
	if a.shouldSkipBackoff("s") {
		t.Error("after success reset, no skip")
	}
}

func TestTriggerSync_PollsSingleSource(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
		WorkspaceID:          "ws1",
	}
	src := testSource(t, sources, deps.Secrets, cfg, 1700000000)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		pages:      []conversationPage{{Conversations: []conversation{fullConversation()}}},
		detailByID: map[string]conversation{"101": fullConversation()},
	}
	a := buildTestAdapter(fake, deps)
	a.pollSingleSource(context.Background(), src.ID)

	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest via sync-now, got %d", len(ingestFake.Calls))
	}
}

func TestTriggerSync_IgnoresWrongChannelAndDisabled(t *testing.T) {
	sources, ingestFake, _, deps := buildDeps(t)
	// Wrong channel.
	sources.Put("t1", inbound.Source{
		ID: "src-slack", TenantID: "tenant-1", Channel: "slack",
		Slug: "s", Enabled: true,
	})
	// Disabled.
	cfg := Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: encryptedToken(t, deps.Secrets, "tok"),
	}
	sources.Put("t1", inbound.Source{
		ID: "src-off", TenantID: "tenant-1", Channel: channelName,
		Slug: "off", Enabled: false,
		Config: buildTestConfig(t, cfg, deps.Secrets),
	})

	a := buildTestAdapter(&fakeAPIClient{}, deps) // ptrext:allow test-fixture
	a.pollSingleSource(context.Background(), "src-slack")
	a.pollSingleSource(context.Background(), "src-off")
	a.pollSingleSource(context.Background(), "missing")

	if len(ingestFake.Calls) != 0 {
		t.Fatalf("expected 0 ingests, got %d", len(ingestFake.Calls))
	}
}

func TestTriggerSync_NonBlocking(t *testing.T) {
	a := NewAdapter().(interface{ TriggerSync(string) })
	// Fill the buffer, second call must not block.
	a.TriggerSync("a")
	done := make(chan struct{})
	go func() {
		a.TriggerSync("b")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("TriggerSync blocked with full buffer")
	}
}
