// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// fakeAPIClient implements apiClient for poll tests.
type fakeAPIClient struct {
	authTestResult     zendeskAccountInfo
	authTestErr        error
	ticketPage         ticketPage
	ticketPageErr      error
	commentsByTicket   map[int64][]comment
	commentsErr        error
	usersResult        []zendeskUser
	orgsResult         []zendeskOrganization
	commentCallCount   int
	refreshTokenResult oauthToken
	refreshTokenErr    error
}

func (f *fakeAPIClient) AuthTest(_ context.Context) (zendeskAccountInfo, error) {
	return f.authTestResult, f.authTestErr
}

func (f *fakeAPIClient) IncrementalTickets(_ context.Context, _ string, _ int64) (ticketPage, error) {
	return f.ticketPage, f.ticketPageErr
}

func (f *fakeAPIClient) TicketComments(_ context.Context, ticketID int64) ([]comment, error) {
	f.commentCallCount++
	if f.commentsErr != nil {
		return nil, f.commentsErr
	}
	if f.commentsByTicket != nil {
		return f.commentsByTicket[ticketID], nil
	}
	return nil, nil
}

func (f *fakeAPIClient) ShowUsers(_ context.Context, _ []int64) ([]zendeskUser, error) {
	return f.usersResult, nil
}

func (f *fakeAPIClient) ShowOrganizations(_ context.Context, _ []int64) ([]zendeskOrganization, error) {
	return f.orgsResult, nil
}

func (f *fakeAPIClient) RefreshOAuthToken(_ context.Context, _, _, _ string) (oauthToken, error) {
	return f.refreshTokenResult, f.refreshTokenErr
}

// fakeSourcesWithConfig wraps FakeSources and records UpdateConfig calls.
type fakeSourcesWithConfig struct {
	*inboundtest.FakeSources
	configUpdates   map[string][]byte
	configUpdateErr error // if set, UpdateConfig returns this error
}

func newFakeSourcesWithConfig() *fakeSourcesWithConfig {
	return &fakeSourcesWithConfig{ // ptrext:allow test-fixture
		FakeSources:   inboundtest.NewFakeSources(),
		configUpdates: map[string][]byte{},
	}
}

func (f *fakeSourcesWithConfig) UpdateConfig(_ context.Context, id string, config []byte) error {
	if f.configUpdateErr != nil {
		return f.configUpdateErr
	}
	f.configUpdates[id] = config
	return nil
}

// errorListSources is a SourceStore that always fails on List.
type errorListSources struct {
	inbound.SourceStore
	listErr error
}

func (e errorListSources) List(_ context.Context, _ string) ([]inbound.Source, error) {
	return nil, e.listErr
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
		newClient: func(_ string, _ credential) apiClient {
			return fake
		},
		lastSuccessAt: map[string]time.Time{},
	})
	a.deps = deps
	return a
}

func TestPollSource_HappyPath(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	sources, ingestFake, metrics, deps := buildHappyPathDeps(t)
	src, fake := buildHappyPathFixtures(t, sources, deps)

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	verifyHappyPathIngest(t, ingestFake)
	verifyHappyPathMeta(t, ingestFake)
	verifyHappyPathCursorAndMetrics(t, sources, metrics)
}

func buildHappyPathDeps(t *testing.T) (*fakeSourcesWithConfig, *inboundtest.FakeIngest, *inboundtest.FakeMetrics, inbound.Deps) {
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

func buildHappyPathFixtures(t *testing.T, sources *fakeSourcesWithConfig, deps inbound.Deps) (inbound.Source, *fakeAPIClient) {
	t.Helper()
	secrets := deps.Secrets.(inboundtest.FakeSecrets)
	tokenEnc, _ := secrets.Encrypt([]byte("test-api-token"))
	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "admin@acme.com",
		APITokenEncrypted: tokenEnc,
		SyncCursor:        "prev-cursor",
		StartFrom:         "now",
	}
	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-1", TenantID: "tenant-1", Channel: channelName,
		Name: "Acme Zendesk", Slug: "acme-zendesk",
		Config: configBlob, Enabled: true,
		State: inbound.SourceState{LastUID: 1000},
	}
	sources.Put("t1", src)
	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPage: ticketPage{
			Tickets: []ticket{{
				ID: 42, Subject: "Cannot login", Description: "Getting 500 errors on login page.",
				Status: "open", Priority: "high", Type: "incident",
				Tags: []string{"login", "auth"}, RequesterID: 100, OrganizationID: 200,
				CreatedAt: "2026-07-24T10:00:00Z", UpdatedAt: "2026-07-24T11:00:00Z",
				GeneratedTimestamp: 5000, Via: ticketVia{Channel: "web"},
			}},
			AfterCursor: "new-cursor-abc", EndOfStream: true,
		},
		commentsByTicket: map[int64][]comment{
			42: {{ID: 1, Body: "Getting 500 errors on login page.", Public: true}, {ID: 2, Body: "I tried clearing cookies.", Public: true}},
		},
		usersResult: []zendeskUser{{ID: 100, Name: "Alice", Email: "alice@acme.com"}},
		orgsResult:  []zendeskOrganization{{ID: 200, Name: "Acme Corp"}},
	}
	return src, fake
}

func verifyHappyPathIngest(t *testing.T, ingestFake *inboundtest.FakeIngest) {
	t.Helper()
	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest call, got %d", len(ingestFake.Calls))
	}
	call := ingestFake.Calls[0]
	if call.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want tenant-1", call.TenantID)
	}
	if call.In.Source != channelName {
		t.Errorf("Source = %q, want %q", call.In.Source, channelName)
	}
	if call.In.SourceUser != "alice@acme.com" {
		t.Errorf("SourceUser = %q, want alice@acme.com", call.In.SourceUser)
	}
	if !strings.Contains(call.In.Content, "Cannot login") {
		t.Errorf("Content should contain subject, got %q", call.In.Content)
	}
	if !strings.Contains(call.In.Content, "I tried clearing cookies.") {
		t.Errorf("Content should contain second comment, got %q", call.In.Content)
	}
	if call.In.PageURL != "https://acme.zendesk.com/agent/tickets/42" {
		t.Errorf("PageURL = %q", call.In.PageURL)
	}
	if call.In.IdempotencyKey != "zendesk_acme_42_5000" {
		t.Errorf("IdempotencyKey = %q, want zendesk_acme_42_5000", call.In.IdempotencyKey)
	}
}

func verifyHappyPathMeta(t *testing.T, ingestFake *inboundtest.FakeIngest) {
	t.Helper()
	meta := ingestFake.Calls[0].In.SourceMeta
	if meta["zendesk_ticket_id"] != int64(42) {
		t.Errorf("zendesk_ticket_id = %v", meta["zendesk_ticket_id"])
	}
	if meta["zendesk_status"] != "open" {
		t.Errorf("zendesk_status = %v", meta["zendesk_status"])
	}
	if meta["zendesk_requester_name"] != "Alice" {
		t.Errorf("zendesk_requester_name = %v", meta["zendesk_requester_name"])
	}
	if meta["zendesk_organization_name"] != "Acme Corp" {
		t.Errorf("zendesk_organization_name = %v", meta["zendesk_organization_name"])
	}
}

func verifyHappyPathCursorAndMetrics(t *testing.T, sources *fakeSourcesWithConfig, metrics *inboundtest.FakeMetrics) {
	t.Helper()
	if _, ok := sources.configUpdates["src-1"]; !ok {
		t.Error("expected UpdateConfig to be called for cursor persistence")
	}
	stored, err := sources.Get(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if stored.State.LastUID != 5000 {
		t.Errorf("LastUID = %d, want 5000", stored.State.LastUID)
	}
	if len(metrics.Totals) == 0 {
		t.Fatal("expected at least one Total metric")
	}
	foundOK := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|ok") {
			foundOK = true
		}
	}
	if !foundOK {
		t.Errorf("expected 'ok' metric, got %v", metrics.Totals)
	}
	if len(metrics.Latencies) == 0 {
		t.Error("expected latency metric")
	}
	if len(metrics.PollLags) == 0 {
		t.Error("expected poll lag metric")
	}
}

func TestPollSource_SkipsDeletedTickets(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("test-token"))

	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "full",
	}

	sources := newFakeSourcesWithConfig()
	ingest := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingest.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-del", TenantID: "t1", Channel: channelName,
		Name: "Del", Slug: "del", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPage: ticketPage{
			Tickets: []ticket{
				{ID: 1, Status: "deleted", GeneratedTimestamp: 100},
				{ID: 2, Status: "open", Subject: "Real", Description: "Real ticket", GeneratedTimestamp: 200, RequesterID: 10},
			},
			AfterCursor: "c1",
			EndOfStream: true,
		},
		usersResult: []zendeskUser{{ID: 10, Name: "User", Email: "u@u.com"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Only the non-deleted ticket should be ingested.
	if len(ingest.Calls) != 1 {
		t.Fatalf("expected 1 ingest call (deleted skipped), got %d", len(ingest.Calls))
	}
	if !strings.HasPrefix(ingest.Calls[0].In.IdempotencyKey, "zendesk_acme_2_") {
		t.Errorf("wrong ticket ingested: key=%s", ingest.Calls[0].In.IdempotencyKey)
	}
}

func TestPollSource_AuthFailure_DisablesSource(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("bad-token"))

	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "now",
	}

	sources := newFakeSourcesWithConfig()
	ingest := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingest.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-auth", TenantID: "t1", Channel: channelName,
		Name: "Auth", Slug: "auth", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPageErr: apiError{Method: "incremental", Status: 401, Code: "unauthorized"},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Source should be disabled.
	stored, _ := sources.Get(context.Background(), "src-auth")
	if stored.Enabled {
		t.Error("expected source to be disabled after auth failure")
	}

	// Should have auth_err metric.
	foundAuth := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|auth_err") {
			foundAuth = true
		}
	}
	if !foundAuth {
		t.Errorf("expected auth_err metric, got %v", metrics.Totals)
	}

	// No ingest calls.
	if len(ingest.Calls) != 0 {
		t.Errorf("expected 0 ingest calls, got %d", len(ingest.Calls))
	}
}

func TestPollSource_TransientError_RecordsLastError(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("token"))

	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "now",
	}

	sources := newFakeSourcesWithConfig()
	ingest := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingest.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-500", TenantID: "t1", Channel: channelName,
		Name: "Err", Slug: "err", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPageErr: apiError{Method: "incremental", Status: 500, Code: "internal"},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Source should remain enabled.
	stored, _ := sources.Get(context.Background(), "src-500")
	if !stored.Enabled {
		t.Error("source should remain enabled on transient error")
	}
	if stored.State.LastError == "" {
		t.Error("expected LastError to be set")
	}

	// Should have transient_err metric.
	foundTransient := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|transient_err") {
			foundTransient = true
		}
	}
	if !foundTransient {
		t.Errorf("expected transient_err metric, got %v", metrics.Totals)
	}
}

func TestPollSource_CommentBudget(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("token"))

	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "full",
	}

	// Create 60 tickets — more than defaultMaxCommentFetches (50).
	tickets := make([]ticket, 60)
	for i := range tickets {
		tickets[i] = ticket{
			ID:                 int64(i + 1),
			Subject:            "Ticket",
			Description:        "Description",
			Status:             "open",
			RequesterID:        10,
			GeneratedTimestamp: int64(1000 + i),
		}
	}

	sources := newFakeSourcesWithConfig()
	ingest := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingest.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-budget", TenantID: "t1", Channel: channelName,
		Name: "Budget", Slug: "budget", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPage: ticketPage{
			Tickets:     tickets,
			AfterCursor: "c-budget",
			EndOfStream: true,
		},
		usersResult: []zendeskUser{{ID: 10, Name: "User", Email: "u@u.com"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// All 60 tickets should be ingested.
	if len(ingest.Calls) != 60 {
		t.Fatalf("expected 60 ingest calls, got %d", len(ingest.Calls))
	}

	// Only defaultMaxCommentFetches comment API calls should have been made.
	if fake.commentCallCount != defaultMaxCommentFetches {
		t.Errorf("comment API calls = %d, want %d (defaultMaxCommentFetches)", fake.commentCallCount, defaultMaxCommentFetches)
	}
}

func TestPollSource_DuplicateIngest(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("token"))

	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "full",
	}

	sources := newFakeSourcesWithConfig()
	ingest := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	// Set NextErr to simulate idempotency conflict.
	ingest.NextErr = errors.New("idempotency key used with different request")
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingest.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-dup", TenantID: "t1", Channel: channelName,
		Name: "Dup", Slug: "dup", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPage: ticketPage{
			Tickets: []ticket{
				{ID: 99, Subject: "Dup", Description: "Dup ticket", Status: "open", GeneratedTimestamp: 500, RequesterID: 10},
			},
			AfterCursor: "c-dup",
			EndOfStream: true,
		},
		usersResult: []zendeskUser{{ID: 10, Name: "User", Email: "u@u.com"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Should have validate_err metric for the duplicate.
	foundValidate := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|validate_err") {
			foundValidate = true
		}
	}
	if !foundValidate {
		t.Errorf("expected validate_err metric for duplicate, got %v", metrics.Totals)
	}
}

func TestSeedStartTime_Now(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	a := ptrext.Of(adapter{lastSuccessAt: map[string]time.Time{}})
	cfg := Config{StartFrom: "now"}
	got := a.seedStartTime(cfg)
	want := fixedTime.Add(-5 * time.Minute).Unix()
	if got != want {
		t.Errorf("seedStartTime(now) = %d, want %d", got, want)
	}
}

func TestSeedStartTime_Full(t *testing.T) {
	a := ptrext.Of(adapter{lastSuccessAt: map[string]time.Time{}})
	cfg := Config{StartFrom: "full"}
	got := a.seedStartTime(cfg)
	if got != 0 {
		t.Errorf("seedStartTime(full) = %d, want 0", got)
	}
}

func TestPollSource_PersistConfigFailure(t *testing.T) {
	origNow := nowFn
	nowFn = func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = origNow })

	sources, ingestFake, _, deps := buildHappyPathDeps(t)
	sources.configUpdateErr = errors.New("disk full")
	src, fake := buildHappyPathFixtures(t, sources, deps)

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Ingest still succeeds despite config write failure.
	if len(ingestFake.Calls) != 1 {
		t.Fatalf("expected 1 ingest call, got %d", len(ingestFake.Calls))
	}
	// LastUID still advanced.
	stored, _ := sources.Get(context.Background(), "src-1")
	if stored.State.LastUID != 5000 {
		t.Errorf("LastUID = %d, want 5000", stored.State.LastUID)
	}
	// Config was NOT persisted (error returned).
	if _, ok := sources.configUpdates["src-1"]; ok {
		t.Error("config should NOT be persisted when UpdateConfig fails")
	}
}

func TestPollSource_ConfigDecryptFailure(t *testing.T) {
	origNow := nowFn
	nowFn = func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = origNow })

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

	src := inbound.Source{
		ID: "src-bad", TenantID: "t1", Channel: channelName,
		Name: "Bad", Slug: "bad",
		Config:  []byte("garbage-not-encrypted"),
		Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{} // ptrext:allow test-fixture
	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// No ingest calls.
	if len(ingestFake.Calls) != 0 {
		t.Errorf("expected 0 ingest calls, got %d", len(ingestFake.Calls))
	}
	// Source still enabled.
	stored, _ := sources.Get(context.Background(), "src-bad")
	if !stored.Enabled {
		t.Error("source should remain enabled on decrypt failure")
	}
	if stored.State.LastError != "decrypt config" {
		t.Errorf("LastError = %q, want 'decrypt config'", stored.State.LastError)
	}
	// internal_err metric.
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

func TestPollAllSources_ListError(t *testing.T) {
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: errorListSources{listErr: errors.New("db down")},
		Secrets: inboundtest.FakeSecrets{},
		Metrics: metrics,
	}

	a := ptrext.Of(adapter{
		newClient:     func(_ string, _ credential) apiClient { return nil },
		lastSuccessAt: map[string]time.Time{},
	})
	a.deps = deps

	// Must not panic.
	a.pollAllSources(context.Background())

	// No ingest calls — pollSource never ran.
	if len(ingestFake.Calls) != 0 {
		t.Errorf("expected 0 ingest calls, got %d", len(ingestFake.Calls))
	}
}

func TestPollSource_OAuthCursorPersistence(t *testing.T) {
	origNow := nowFn
	nowFn = func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	oauthJSON := []byte(`{"access_token":"oauth-tok-123"}`)
	oauthEnc, _ := secrets.Encrypt(oauthJSON)

	cfg := Config{
		Version:             ConfigVersion,
		AuthMode:            AuthModeOAuth,
		Subdomain:           "acme",
		OAuthTokenEncrypted: oauthEnc,
		SyncCursor:          "old-cursor",
		StartFrom:           "now",
	}

	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-oauth", TenantID: "t1", Channel: channelName,
		Name: "OAuth", Slug: "oauth", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPage: ticketPage{
			Tickets: []ticket{{
				ID: 1, Subject: "T", Description: "D", Status: "open",
				GeneratedTimestamp: 999, RequesterID: 10,
			}},
			AfterCursor: "new-oauth-cursor",
			EndOfStream: true,
		},
		usersResult: []zendeskUser{{ID: 10, Name: "U", Email: "u@u.com"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Verify cursor persisted.
	rawCfg, ok := sources.configUpdates["src-oauth"]
	if !ok {
		t.Fatal("expected UpdateConfig to be called for OAuth cursor")
	}
	// Double-decrypt to check the cursor value.
	dec1, _ := secrets.Decrypt(rawCfg)
	var saved Config
	if err := json.Unmarshal(dec1, &saved); err != nil { // ptrext:allow json-unmarshal
		t.Fatalf("unmarshal saved config: %v", err)
	}
	if saved.SyncCursor != "new-oauth-cursor" {
		t.Errorf("SyncCursor = %q, want new-oauth-cursor", saved.SyncCursor)
	}
	if saved.AuthMode != AuthModeOAuth {
		t.Errorf("AuthMode = %q, want oauth", saved.AuthMode)
	}
	if len(saved.OAuthTokenEncrypted) == 0 {
		t.Error("OAuthTokenEncrypted should be re-encrypted")
	}
}

func TestPollSource_CommentAuthFailure_DegradesGracefully(t *testing.T) {
	origNow := nowFn
	nowFn = func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("token"))
	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "full",
	}

	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}

	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "src-cauth", TenantID: "t1", Channel: channelName,
		Name: "CAuth", Slug: "cauth", Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPage: ticketPage{
			Tickets: []ticket{
				{ID: 1, Subject: "T1", Description: "D", Status: "open", GeneratedTimestamp: 100, RequesterID: 10},
				{ID: 2, Subject: "T2", Description: "D", Status: "open", GeneratedTimestamp: 200, RequesterID: 10},
			},
			AfterCursor: "c1",
			EndOfStream: true,
		},
		// Comment fetch returns permanent auth error.
		commentsErr: apiError{Method: "comments", Status: 401, Code: "unauthorized"},
		usersResult: []zendeskUser{{ID: 10, Name: "U", Email: "u@u.com"}},
	}

	a := buildTestAdapter(fake, deps)
	a.pollSource(context.Background(), src)

	// Source should still be enabled — comment auth failure degrades gracefully.
	stored, _ := sources.Get(context.Background(), "src-cauth")
	if !stored.Enabled {
		t.Error("source should stay enabled after comment auth failure (degrade, not disable)")
	}
	// comment_auth_err metric.
	foundCommentAuth := false
	for _, m := range metrics.Totals {
		if strings.HasSuffix(m, "|comment_auth_err") {
			foundCommentAuth = true
		}
	}
	if !foundCommentAuth {
		t.Errorf("expected comment_auth_err metric, got %v", metrics.Totals)
	}
	// Tickets still ingested (without comments).
	if len(ingestFake.Calls) != 2 {
		t.Errorf("expected 2 ingest calls (tickets ingested without comments), got %d", len(ingestFake.Calls))
	}
}

func TestWipeCred_ZeroesAllBytes(t *testing.T) {
	cred := credential{
		APIToken: []byte{1, 2, 3, 4, 5},
	}
	wipeCred(cred)
	for i, b := range cred.APIToken {
		if b != 0 {
			t.Errorf("APIToken[%d] = %d, want 0", i, b)
		}
	}
	// wipeBytes(nil) must not panic.
	wipeBytes(nil)
}

func TestPollSource_OAuthRefreshFlow(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	oauthTok := oauthToken{AccessToken: "old-token", RefreshToken: "my-refresh"}
	tokJSON, _ := json.Marshal(oauthTok) // ptrext:allow json-marshal
	tokEnc, _ := secrets.Encrypt(tokJSON)
	cfg := Config{
		Version:             ConfigVersion,
		AuthMode:            AuthModeOAuth,
		Subdomain:           "acme",
		OAuthTokenEncrypted: tokEnc,
		SyncCursor:          "c",
		StartFrom:           "now",
	}

	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}
	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "oauth-src", TenantID: "t1", Channel: channelName,
		Name: "OAuth ZD", Slug: "oauth-zd",
		Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	callCount := 0
	fake := &fakeAPIClient{ // ptrext:allow test-fixture
		ticketPageErr: apiError{Status: 401, Code: "unauthorized"}, // first call fails
		refreshTokenResult: oauthToken{
			AccessToken:  "new-token",
			RefreshToken: "new-refresh",
		},
	}
	// Override IncrementalTickets to succeed on second call.
	a := ptrext.Of(adapter{
		newClient: func(_ string, _ credential) apiClient {
			callCount++
			if callCount > 1 {
				// After refresh, return success.
				fake.ticketPageErr = nil
				fake.ticketPage = ticketPage{
					Tickets:     []ticket{{ID: 1, Subject: "Test", Status: "open", GeneratedTimestamp: 100}},
					AfterCursor: "new-cursor",
					EndOfStream: true,
				}
			}
			return fake
		},
		lastSuccessAt: map[string]time.Time{},
		failureCount:  map[string]int{},
		syncNow:       make(chan string, 1),
	})
	a.deps = deps
	a.pollSource(context.Background(), src)

	// Source should NOT be disabled — refresh succeeded.
	stored, _ := sources.Get(context.Background(), "oauth-src")
	if !stored.Enabled {
		t.Error("source should still be enabled after successful OAuth refresh")
	}
}

func TestShouldSkipBackoff(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	a := ptrext.Of(adapter{
		lastSuccessAt: map[string]time.Time{},
		failureCount:  map[string]int{},
		syncNow:       make(chan string, 1),
	})

	// No failures → no skip.
	if a.shouldSkipBackoff("src-1") {
		t.Error("expected false with 0 failures")
	}

	// 2 failures → no skip.
	a.failureCount["src-1"] = 2
	a.lastSuccessAt["src-1"] = fixedTime.Add(-30 * time.Second)
	if a.shouldSkipBackoff("src-1") {
		t.Error("expected false with 2 failures")
	}

	// 3 failures + recent last success → skip.
	a.failureCount["src-1"] = 3
	a.lastSuccessAt["src-1"] = fixedTime.Add(-10 * time.Second)
	if !a.shouldSkipBackoff("src-1") {
		t.Error("expected true with 3 failures and recent poll")
	}

	// 3 failures + old last success → no skip (interval elapsed).
	a.lastSuccessAt["src-1"] = fixedTime.Add(-5 * time.Minute)
	if a.shouldSkipBackoff("src-1") {
		t.Error("expected false with 3 failures and old poll")
	}
}

func TestTriggerSync_NonBlocking(t *testing.T) {
	a := ptrext.Of(adapter{
		syncNow: make(chan string, 1),
	})
	// First call should succeed.
	a.TriggerSync("src-1")
	// Second call should not block (channel full → dropped).
	a.TriggerSync("src-2")
	// Drain and verify first was received.
	got := <-a.syncNow
	if got != "src-1" {
		t.Errorf("got %q, want src-1", got)
	}
}

func TestSyncPages_MultiPage(t *testing.T) {
	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("token"))
	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "a@a.com",
		APITokenEncrypted: tokenEnc,
		StartFrom:         "now",
	}
	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: metrics,
	}
	configBlob := buildTestConfig(t, cfg, secrets)
	src := inbound.Source{
		ID: "mp-src", TenantID: "t1", Channel: channelName,
		Name: "Multi", Slug: "multi",
		Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	pageCount := 0
	a := ptrext.Of(adapter{
		newClient: func(_ string, _ credential) apiClient {
			return &multiPageFakeClient{pageCount: &pageCount, maxPages: 3} // ptrext:allow test-fixture
		},
		lastSuccessAt: map[string]time.Time{},
		failureCount:  map[string]int{},
		syncNow:       make(chan string, 1),
	})
	a.deps = deps
	a.pollSource(context.Background(), src)

	// Should have fetched 3 pages.
	if pageCount != 3 {
		t.Errorf("pageCount = %d, want 3", pageCount)
	}
	// Should have ingested 3 tickets (one per page).
	if len(ingestFake.Calls) != 3 {
		t.Errorf("ingest calls = %d, want 3", len(ingestFake.Calls))
	}
}

// multiPageFakeClient simulates multi-page incremental export responses.
type multiPageFakeClient struct {
	pageCount *int
	maxPages  int
}

func (m *multiPageFakeClient) AuthTest(_ context.Context) (zendeskAccountInfo, error) {
	return zendeskAccountInfo{}, nil
}

func (m *multiPageFakeClient) IncrementalTickets(_ context.Context, _ string, _ int64) (ticketPage, error) { // ptrext:allow test-counter
	*m.pageCount++     // ptrext:allow test-counter
	pc := *m.pageCount // ptrext:allow test-counter
	return ticketPage{
		Tickets:     []ticket{{ID: int64(pc), Subject: "T", Status: "open", RequesterID: 1, GeneratedTimestamp: int64(pc * 100)}},
		AfterCursor: "cursor-" + string(rune('a'+pc)),
		EndOfStream: pc >= m.maxPages,
	}, nil
}

func (m *multiPageFakeClient) TicketComments(_ context.Context, _ int64) ([]comment, error) {
	return []comment{{ID: 1, Body: "desc", Public: true, AuthorID: 1}}, nil
}

func (m *multiPageFakeClient) ShowUsers(_ context.Context, _ []int64) ([]zendeskUser, error) {
	return []zendeskUser{{ID: 1, Name: "A", Email: "a@a.com"}}, nil
}

func (m *multiPageFakeClient) ShowOrganizations(_ context.Context, _ []int64) ([]zendeskOrganization, error) {
	return nil, nil
}

func (m *multiPageFakeClient) RefreshOAuthToken(_ context.Context, _, _, _ string) (oauthToken, error) {
	return oauthToken{}, nil
}
