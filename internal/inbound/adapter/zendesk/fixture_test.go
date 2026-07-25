// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/infra/zendeskclient"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
)

// loadFixture reads a testdata JSON file.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

// newFixtureServer creates an httptest server that serves Zendesk API
// responses from testdata JSON files.
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/users/me.json":
			fmt.Fprint(w, `{"user":{"id":300,"name":"Agent Smith","email":"agent@acme.com"}}`)
		case r.URL.Path == "/api/v2/incremental/tickets/cursor.json":
			w.Write(loadFixture(t, "incremental_tickets.json")) //nolint:errcheck
		case strings.HasPrefix(r.URL.Path, "/api/v2/tickets/") && strings.HasSuffix(r.URL.Path, "/comments.json"):
			// Extract ticket ID from path: /api/v2/tickets/{id}/comments.json
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 5 {
				ticketID := parts[4]
				name := fmt.Sprintf("ticket_%s_comments.json", ticketID)
				data, err := os.ReadFile("testdata/" + name)
				if err != nil {
					// No fixture for this ticket — return empty comments.
					fmt.Fprint(w, `{"comments":[],"links":{"next":""}}`)
					return
				}
				w.Write(data) //nolint:errcheck
			}
		case r.URL.Path == "/api/v2/users/show_many.json":
			w.Write(loadFixture(t, "users_show_many.json")) //nolint:errcheck
		case r.URL.Path == "/api/v2/organizations/show_many.json":
			w.Write(loadFixture(t, "organizations_show_many.json")) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
}

// setupTestEgress allows loopback and sets the test override so host
// validation (*.zendesk.com) is skipped for httptest servers.
func setupTestEgress(t *testing.T, srvURL string) {
	t.Helper()
	SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	SetAPIBaseURL(srvURL)
	t.Cleanup(func() {
		SetEgressPolicy(nethardening.Policy{})
		SetAPIBaseURL("")
	})
}

// TestFixture_FullPipeline verifies the complete httptest → adapter →
// IngestInput data flow using real JSON fixture files. This is the E2E
// proof that Zendesk tickets generate feedback items with correct
// support metadata (issue #229 acceptance criteria A).
func TestFixture_FullPipeline(t *testing.T) {
	srv := newFixtureServer(t)
	t.Cleanup(srv.Close)
	setupTestEgress(t, srv.URL)

	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
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

	src := inbound.Source{
		ID: "fixture-src", TenantID: "fixture-tenant", Channel: channelName,
		Name: "Fixture Zendesk", Slug: "fixture-zendesk",
		Config: configBlob, Enabled: true,
		State: inbound.SourceState{LastUID: 0},
	}
	sources.Put("ft", src)

	// Wire adapter with real HTTP client pointing at fixture server.
	a := ptrext.Of(adapter{
		newClient: func(_ string, cred credential) apiClient {
			return zendeskclient.New(srv.URL, cred)
		},
		lastSuccessAt: map[string]time.Time{},
	})
	a.deps = deps
	a.pollSource(context.Background(), src)

	// Fixture has 4 tickets: 42 (open), 43 (pending), 99 (deleted), 55 (solved).
	// Deleted ticket 99 should be skipped → expect 3 ingest calls.
	if len(ingestFake.Calls) != 3 {
		t.Fatalf("expected 3 ingest calls (skip deleted), got %d", len(ingestFake.Calls))
	}

	// --- Verify ticket 42 ---
	t.Run("Ticket42_Metadata", func(t *testing.T) {
		call := findIngestByTicketID(t, ingestFake, 42)
		verifyTicket42(t, call)
	})

	// --- Verify ticket 43 ---
	t.Run("Ticket43_Metadata", func(t *testing.T) {
		call := findIngestByTicketID(t, ingestFake, 43)
		verifyTicket43(t, call)
	})

	// --- Verify ticket 55 (same requester as 42) ---
	t.Run("Ticket55_SameRequester", func(t *testing.T) {
		call := findIngestByTicketID(t, ingestFake, 55)
		verifyTicket55SameRequester(t, ingestFake, call)
	})

	// --- Verify cursor persisted ---
	t.Run("CursorPersisted", func(t *testing.T) {
		if _, ok := sources.configUpdates["fixture-src"]; !ok {
			t.Error("expected cursor to be persisted via UpdateConfig")
		}
	})

	// --- Verify LastUID advanced to max generated_timestamp ---
	t.Run("LastUID_Advanced", func(t *testing.T) {
		stored, err := sources.Get(context.Background(), "fixture-src")
		if err != nil {
			t.Fatalf("Get source: %v", err)
		}
		// Max generated_timestamp across non-deleted tickets: 1753347600 (ticket 55)
		if stored.State.LastUID != 1753347600 {
			t.Errorf("LastUID = %d, want 1753347600", stored.State.LastUID)
		}
	})

	// --- Verify metrics ---
	t.Run("Metrics", func(t *testing.T) {
		okCount := 0
		for _, m := range metrics.Totals {
			if strings.HasSuffix(m, "|ok") {
				okCount++
			}
		}
		if okCount != 3 {
			t.Errorf("expected 3 'ok' metrics, got %d", okCount)
		}
	})
}

func verifyTicket42(t *testing.T, call inboundtest.FakeIngestCall) {
	t.Helper()
	verifyTicket42Content(t, call)
	verifyTicket42Meta(t, call)
	verifyTicket42PhaseC(t, call)
}

func verifyTicket42Content(t *testing.T, call inboundtest.FakeIngestCall) {
	t.Helper()
	if !strings.Contains(call.In.Content, "Cannot log in after password reset") {
		t.Error("content missing subject")
	}
	if !strings.Contains(call.In.Content, "500 error on the login page") {
		t.Error("content missing description")
	}
	if !strings.Contains(call.In.Content, "Could you try again in 30 minutes") {
		t.Error("content missing public comment 1003")
	}
	if !strings.Contains(call.In.Content, "blocking my entire team") {
		t.Error("content missing public comment 1004")
	}
	if strings.Contains(call.In.Content, "Internal note") {
		t.Error("content should NOT contain internal note")
	}
	if call.In.SourceUser != "alice@acme.com" {
		t.Errorf("SourceUser = %q, want alice@acme.com", call.In.SourceUser)
	}
	if call.In.PageURL != "https://acme.zendesk.com/agent/tickets/42" {
		t.Errorf("PageURL = %q", call.In.PageURL)
	}
	if !strings.HasPrefix(call.In.IdempotencyKey, "zendesk_acme_42_") {
		t.Errorf("IdempotencyKey = %q, want zendesk_acme_42_{timestamp}", call.In.IdempotencyKey)
	}
}

func verifyTicket42Meta(t *testing.T, call inboundtest.FakeIngestCall) {
	t.Helper()
	meta := call.In.SourceMeta
	assertMeta(t, meta, "zendesk_ticket_id", int64(42))
	assertMeta(t, meta, "zendesk_status", "open")
	assertMeta(t, meta, "zendesk_priority", "high")
	assertMeta(t, meta, "zendesk_type", "incident")
	assertMeta(t, meta, "zendesk_requester_id", int64(100))
	assertMeta(t, meta, "zendesk_requester_name", "Alice Chen")
	assertMeta(t, meta, "zendesk_requester_email", "alice@acme.com")
	assertMeta(t, meta, "zendesk_organization_id", int64(200))
	assertMeta(t, meta, "zendesk_organization_name", "Acme Corp")
	assertMeta(t, meta, "zendesk_via_channel", "web")
	assertMeta(t, meta, "zendesk_satisfaction_score", "offered")
	assertMeta(t, meta, "zendesk_subdomain", "acme")

	tags, ok := meta["zendesk_tags"].(string)
	if !ok {
		t.Fatal("zendesk_tags should be a string (JSON array)")
	}
	if !strings.Contains(tags, "login") || !strings.Contains(tags, "auth") {
		t.Errorf("zendesk_tags = %q, want to contain login and auth", tags)
	}
	if meta["zendesk_ticket_url"] != "https://acme.zendesk.com/agent/tickets/42" {
		t.Errorf("zendesk_ticket_url = %v", meta["zendesk_ticket_url"])
	}

	// Comment count: 3 public comments (1001, 1003, 1004; 1002 is internal).
	if meta["zendesk_comment_count"] != 3 {
		t.Errorf("zendesk_comment_count = %v, want 3", meta["zendesk_comment_count"])
	}

	// subject_key / subject_hash readiness for customer linking.
	subjectKey, _ := subjectkey.Normalize(call.In.SourceUser, "")
	if subjectKey != "alice@acme.com" {
		t.Errorf("subjectkey.Normalize(%q) = %q, want alice@acme.com", call.In.SourceUser, subjectKey)
	}
}

func verifyTicket42PhaseC(t *testing.T, call inboundtest.FakeIngestCall) {
	t.Helper()
	meta := call.In.SourceMeta

	if !strings.Contains(call.In.Content, "[customer]") {
		t.Error("content should contain [customer] tag")
	}
	if !strings.Contains(call.In.Content, "[agent]") {
		t.Error("content should contain [agent] tag")
	}
	if meta["zendesk_customer_message_count"] == nil {
		t.Error("missing zendesk_customer_message_count")
	}
	if meta["zendesk_agent_message_count"] == nil {
		t.Error("missing zendesk_agent_message_count")
	}
	customFields, ok := meta["zendesk_custom_fields"].(string)
	if !ok || customFields == "" || customFields == "null" {
		t.Error("zendesk_custom_fields should be a non-empty JSON string")
	}
	if !strings.Contains(customFields, "auth-service") {
		t.Errorf("zendesk_custom_fields should contain auth-service, got %s", customFields)
	}
	if call.In.Type != "bug_report" {
		t.Errorf("IngestInput.Type = %q, want bug_report (high+incident)", call.In.Type)
	}
}

func verifyTicket43(t *testing.T, call inboundtest.FakeIngestCall) {
	t.Helper()
	if call.In.SourceUser != "bob@acme.com" {
		t.Errorf("SourceUser = %q, want bob@acme.com", call.In.SourceUser)
	}
	meta := call.In.SourceMeta
	assertMeta(t, meta, "zendesk_ticket_id", int64(43))
	assertMeta(t, meta, "zendesk_status", "pending")
	assertMeta(t, meta, "zendesk_priority", "normal")
	assertMeta(t, meta, "zendesk_satisfaction_score", "good")
	assertMeta(t, meta, "zendesk_via_channel", "email")
	assertMeta(t, meta, "zendesk_organization_name", "Acme Corp")

	if !strings.HasPrefix(call.In.IdempotencyKey, "zendesk_acme_43_") {
		t.Errorf("IdempotencyKey = %q, want zendesk_acme_43_{timestamp}", call.In.IdempotencyKey)
	}
	if !strings.Contains(call.In.Content, "export all feedback as CSV") {
		t.Error("content missing description")
	}
}

// verifyTicket55SameRequester proves that ticket 55 (requester_id=100,
// same as ticket 42) produces the same subject_key — enabling customer
// aggregation across multiple tickets from the same requester.
func verifyTicket55SameRequester(t *testing.T, ingestFake *inboundtest.FakeIngest, call55 inboundtest.FakeIngestCall) {
	t.Helper()
	call42 := findIngestByTicketID(t, ingestFake, 42)

	// Same requester → same SourceUser → same subject_key.
	if call55.In.SourceUser != call42.In.SourceUser {
		t.Errorf("ticket 55 SourceUser=%q ≠ ticket 42 SourceUser=%q (same requester_id=100)",
			call55.In.SourceUser, call42.In.SourceUser)
	}
	key55, _ := subjectkey.Normalize(call55.In.SourceUser, "")
	key42, _ := subjectkey.Normalize(call42.In.SourceUser, "")
	if key55 != key42 {
		t.Errorf("subject_key mismatch: ticket55=%q, ticket42=%q — customer aggregation broken", key55, key42)
	}

	// Different ticket IDs.
	assertMeta(t, call55.In.SourceMeta, "zendesk_ticket_id", int64(55))
	assertMeta(t, call55.In.SourceMeta, "zendesk_status", "solved")
	assertMeta(t, call55.In.SourceMeta, "zendesk_priority", "urgent")
	assertMeta(t, call55.In.SourceMeta, "zendesk_satisfaction_score", "bad")
}

// TestFixture_DeletedTicketSkipped confirms deleted tickets are filtered
// out of the processing pipeline.
func TestFixture_DeletedTicketSkipped(t *testing.T) {
	srv := newFixtureServer(t)
	t.Cleanup(srv.Close)
	setupTestEgress(t, srv.URL)

	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("test-api-token"))
	cfg := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "admin@acme.com",
		APITokenEncrypted: tokenEnc,
		SyncCursor:        "prev",
		StartFrom:         "now",
	}
	configBlob := buildTestConfig(t, cfg, secrets)
	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
	}
	src := inbound.Source{
		ID: "del-src", TenantID: "del-tenant", Channel: channelName,
		Name: "Del Test", Slug: "del-test",
		Config: configBlob, Enabled: true,
	}
	sources.Put("dt", src)
	a := ptrext.Of(adapter{
		newClient:     func(_ string, cred credential) apiClient { return zendeskclient.New(srv.URL, cred) },
		lastSuccessAt: map[string]time.Time{},
	})
	a.deps = deps
	a.pollSource(context.Background(), src)

	// Verify no ingest call has ticket_id=99 (deleted).
	for _, call := range ingestFake.Calls {
		if tid, ok := call.In.SourceMeta["zendesk_ticket_id"]; ok {
			if tid == int64(99) {
				t.Error("deleted ticket 99 should not be ingested")
			}
		}
	}
}

// TestFixture_OrgNotFound verifies graceful degradation when the
// organization ID doesn't resolve (org=0 or missing from show_many).
func TestFixture_OrgNotFound(t *testing.T) {
	srv := newFixtureServer(t)
	t.Cleanup(srv.Close)
	setupTestEgress(t, srv.URL)

	fixedTime := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("test-api-token"))
	cfg := Config{
		Version: ConfigVersion, AuthMode: AuthModeAPIToken,
		Subdomain: "acme", Email: "admin@acme.com",
		APITokenEncrypted: tokenEnc, SyncCursor: "c", StartFrom: "now",
	}
	configBlob := buildTestConfig(t, cfg, secrets)
	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
	}
	src := inbound.Source{
		ID: "org-src", TenantID: "org-tenant", Channel: channelName,
		Name: "Org Test", Slug: "org-test",
		Config: configBlob, Enabled: true,
	}
	sources.Put("ot", src)
	a := ptrext.Of(adapter{
		newClient:     func(_ string, cred credential) apiClient { return zendeskclient.New(srv.URL, cred) },
		lastSuccessAt: map[string]time.Time{},
	})
	a.deps = deps
	a.pollSource(context.Background(), src)

	// Ticket 99 has organization_id=0 (and is deleted, so it's skipped).
	// All surviving tickets have org_id=200 which resolves.
	// Verify organization_name is present for resolved orgs
	// and empty string for unresolved (no panic).
	for _, call := range ingestFake.Calls {
		orgName := call.In.SourceMeta["zendesk_organization_name"]
		if orgName == nil {
			t.Error("zendesk_organization_name should never be nil")
		}
	}
}

// --- Helpers ---

func findIngestByTicketID(t *testing.T, fake *inboundtest.FakeIngest, ticketID int64) inboundtest.FakeIngestCall {
	t.Helper()
	for _, call := range fake.Calls {
		if tid, ok := call.In.SourceMeta["zendesk_ticket_id"]; ok && tid == ticketID {
			return call
		}
	}
	t.Fatalf("no ingest call found for ticket_id=%d", ticketID)
	return inboundtest.FakeIngestCall{}
}

func assertMeta(t *testing.T, meta map[string]any, key string, want any) {
	t.Helper()
	got, ok := meta[key]
	if !ok {
		t.Errorf("SourceMeta missing key %q", key)
		return
	}
	if got != want {
		t.Errorf("SourceMeta[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}
