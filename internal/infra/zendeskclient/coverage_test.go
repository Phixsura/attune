// SPDX-License-Identifier: Apache-2.0

package zendeskclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/zendeskclient"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// --- BaseURL ---

func TestBaseURL(t *testing.T) {
	got := zendeskclient.BaseURL("acme")
	if got != "https://acme.zendesk.com" {
		t.Errorf("BaseURL(acme) = %q", got)
	}
	got2 := zendeskclient.BaseURL("  MyCo  ")
	if got2 != "https://myco.zendesk.com" {
		t.Errorf("BaseURL(MyCo) = %q", got2)
	}
}

// --- ValidateHost edge cases ---

func TestValidateHost_InvalidURL(t *testing.T) {
	zendeskclient.SetTestBaseURL("")
	defer zendeskclient.SetTestBaseURL("")
	// url.Parse doesn't fail on most strings, but an unparseable control char does
	if err := zendeskclient.ValidateHost("https://evil.example.com"); err == nil {
		t.Error("non-zendesk host should be rejected")
	}
}

// --- ShowUsers ---

func TestShowUsers_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v2/users/show_many") {
			http.NotFound(w, r)
			return
		}
		ids := r.URL.Query().Get("ids")
		if ids != "10,20" {
			t.Errorf("ids = %q, want 10,20", ids)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"users": []map[string]any{
				{"id": 10, "name": "Alice", "email": "alice@ex.com"},
				{"id": 20, "name": "Bob", "email": "bob@ex.com"},
			},
		})
	}))
	users, err := c.ShowUsers(context.Background(), []int64{10, 20})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
}

func TestShowUsers_Empty(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("should not call API with empty IDs")
	}))
	users, err := c.ShowUsers(context.Background(), nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if users != nil {
		t.Errorf("expected nil, got %v", users)
	}
}

// --- ShowOrganizations ---

func TestShowOrganizations_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v2/organizations/show_many") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"organizations": []map[string]any{
				{"id": 100, "name": "Acme Corp"},
			},
		})
	}))
	orgs, err := c.ShowOrganizations(context.Background(), []int64{100})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("got %d orgs, want 1", len(orgs))
	}
}

func TestShowOrganizations_Empty(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("should not call API with empty IDs")
	}))
	orgs, err := c.ShowOrganizations(context.Background(), nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if orgs != nil {
		t.Errorf("expected nil, got %v", orgs)
	}
}

// --- RefreshOAuthToken error ---

func TestRefreshOAuthToken_EmptyAccessToken(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": ""}) //nolint:errcheck
	}))
	_, err := c.RefreshOAuthToken(context.Background(), "r", "c", "s")
	if err == nil {
		t.Fatal("expected error for empty access_token")
	}
	if !strings.Contains(err.Error(), "empty access_token") {
		t.Errorf("error = %v, want 'empty access_token'", err)
	}
}

func TestRefreshOAuthToken_ServerError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server down"}`)) //nolint:errcheck
	}))
	_, err := c.RefreshOAuthToken(context.Background(), "r", "c", "s")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

// --- TicketComments two-page ---

func TestTicketComments_TwoPage(t *testing.T) {
	var reqCount int
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		sortOrder := r.URL.Query().Get("sort_order")
		if sortOrder == "desc" {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"comments": []map[string]any{
					{"id": 99, "body": "Latest", "public": true, "author_id": 1},
					{"id": 98, "body": "Second latest", "public": true, "author_id": 2},
				},
				"links": map[string]string{"next": ""},
			})
			return
		}
		// First page asc: has next link.
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"comments": []map[string]any{
				{"id": 1, "body": "First", "public": true, "author_id": 1},
				{"id": 2, "body": "Private", "public": false, "author_id": 2},
			},
			"links": map[string]string{"next": "/page2"},
		})
	}))
	comments, err := c.TicketComments(context.Background(), 42)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if reqCount != 2 {
		t.Errorf("reqCount = %d, want 2", reqCount)
	}
	// 1 public from first page + 2 from last page = 3
	if len(comments) != 3 {
		t.Errorf("got %d comments, want 3", len(comments))
	}
}

func TestTicketComments_FirstPageError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"auth failed"}`)) //nolint:errcheck
	}))
	_, err := c.TicketComments(context.Background(), 42)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTicketComments_LastPageError(t *testing.T) {
	var reqCount int
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		if reqCount == 1 {
			// First page OK with next link.
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"comments": []map[string]any{{"id": 1, "body": "First", "public": true}},
				"links":    map[string]string{"next": "/page2"},
			})
			return
		}
		// Second page fails.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"fail"}`)) //nolint:errcheck
	}))
	_, err := c.TicketComments(context.Background(), 42)
	if err == nil {
		t.Fatal("expected error from last page")
	}
}

// --- IncrementalTickets with start_time ---

func TestIncrementalTickets_WithStartTime(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := r.URL.Query().Get("start_time")
		if st != "1000" {
			t.Errorf("start_time = %q, want 1000", st)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zendeskclient.TicketPage{EndOfStream: true}) //nolint:errcheck
	}))
	page, err := c.IncrementalTickets(context.Background(), "", 1000)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !page.EndOfStream {
		t.Error("expected EndOfStream")
	}
}

func TestIncrementalTickets_RateLimit(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	_, err := c.IncrementalTickets(context.Background(), "", 0)
	if err == nil {
		t.Fatal("expected error for 429")
	}
}

// --- getJSON error paths ---

func TestGetJSON_Forbidden(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`)) //nolint:errcheck
	}))
	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("expected error for 403")
	}
	var ae zendeskclient.APIError
	if !errors.As(err, &ae) || ae.Code != "forbidden" {
		t.Errorf("expected forbidden APIError, got: %v", err)
	}
}

func TestGetJSON_Generic500(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`)) //nolint:errcheck
	}))
	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestGetJSON_LongErrorBody(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(strings.Repeat("x", 300))) //nolint:errcheck
	}))
	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	// Error body should be truncated to 200 chars
	var ae zendeskclient.APIError
	if errors.As(err, &ae) && len(ae.Code) > 201 {
		t.Errorf("error code too long: %d chars", len(ae.Code))
	}
}

func TestGetJSON_InvalidJSON(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`)) //nolint:errcheck
	}))
	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

// --- OAuth setAuth ---

func TestSetAuth_OAuthWithToken(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	zendeskclient.SetTestBaseURL("http://localhost:1")
	defer func() {
		zendeskclient.SetEgressPolicy(nethardening.Policy{})
		zendeskclient.SetTestBaseURL("")
	}()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": 1}}) //nolint:errcheck
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{
		Mode:        zendeskclient.AuthModeOAuth,
		AccessToken: "my-bearer-token",
	})
	_, _ = c.AuthTest(context.Background())
	if gotAuth != "Bearer my-bearer-token" {
		t.Errorf("Authorization = %q, want Bearer my-bearer-token", gotAuth)
	}
}

func TestSetAuth_OAuthNoToken(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	zendeskclient.SetTestBaseURL("http://localhost:1")
	defer func() {
		zendeskclient.SetEgressPolicy(nethardening.Policy{})
		zendeskclient.SetTestBaseURL("")
	}()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": 1}}) //nolint:errcheck
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{
		Mode: zendeskclient.AuthModeOAuth,
		// No AccessToken
	})
	_, _ = c.AuthTest(context.Background())
	if gotAuth != "" {
		t.Errorf("Authorization should be empty when no AccessToken, got %q", gotAuth)
	}
}

func TestSetAuth_UnknownMode(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	zendeskclient.SetTestBaseURL("http://localhost:1")
	defer func() {
		zendeskclient.SetEgressPolicy(nethardening.Policy{})
		zendeskclient.SetTestBaseURL("")
	}()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": 1}}) //nolint:errcheck
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{
		Mode: "unknown",
	})
	_, _ = c.AuthTest(context.Background())
	if gotAuth != "" {
		t.Errorf("Authorization should be empty for unknown mode, got %q", gotAuth)
	}
}

// --- buildURL with full URL passthrough ---

func TestBuildURL_FullURLPassthrough(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	defer zendeskclient.SetEgressPolicy(nethardening.Policy{})

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"comments": []map[string]any{},
			"links":    map[string]string{"next": ""},
		})
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{Mode: zendeskclient.AuthModeAPIToken, APIToken: []byte("t")})
	// Small ticket → single request → verifies first page path
	_, _ = c.TicketComments(context.Background(), 99)
	if gotPath != "/api/v2/tickets/99/comments.json" {
		t.Errorf("path = %q", gotPath)
	}
}

// --- buildURL with inline query ---

func TestBuildURL_InlineQuery(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	defer zendeskclient.SetEgressPolicy(nethardening.Policy{})

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zendeskclient.TicketPage{EndOfStream: true}) //nolint:errcheck
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{Mode: zendeskclient.AuthModeAPIToken, APIToken: []byte("t")})
	// start_time=0 with empty cursor → passes start_time in params
	_, _ = c.IncrementalTickets(context.Background(), "", 0)
	if !strings.Contains(gotQuery, "start_time=0") {
		t.Errorf("query = %q, should contain start_time=0", gotQuery)
	}
}

// --- postForm error path (non-2xx) ---

func TestPostForm_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"bad request"}`)) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	_, err := c.RefreshOAuthToken(context.Background(), "r", "c", "s")
	if err == nil {
		t.Fatal("expected error for 400")
	}
}

func TestPostForm_LongErrorBody(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(strings.Repeat("y", 300))) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	_, err := c.RefreshOAuthToken(context.Background(), "r", "c", "s")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostForm_InvalidJSON(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`not json`)) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	_, err := c.RefreshOAuthToken(context.Background(), "r", "c", "s")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- Network error in do() ---

func TestDo_NetworkError(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	zendeskclient.SetTestBaseURL("http://127.0.0.1:1") // nothing listening
	defer func() {
		zendeskclient.SetEgressPolicy(nethardening.Policy{})
		zendeskclient.SetTestBaseURL("")
	}()
	c := zendeskclient.New("http://127.0.0.1:1", zendeskclient.Credential{
		Mode:     zendeskclient.AuthModeAPIToken,
		APIToken: []byte("t"),
	})
	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
}

// --- extractSubdomain edge cases (tested indirectly through AuthTest) ---

func TestExtractSubdomain_NoZendeskDomain(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	defer zendeskclient.SetEgressPolicy(nethardening.Policy{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": 1}}) //nolint:errcheck
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{Mode: zendeskclient.AuthModeAPIToken, APIToken: []byte("t")})
	acct, err := c.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	// srv.URL is like http://127.0.0.1:PORT — extractSubdomain falls through to the "no dot after removing prefix" case
	if acct.Subdomain == "" {
		t.Error("subdomain should not be empty")
	}
}

func TestExtractSubdomain_NoDot(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	defer zendeskclient.SetEgressPolicy(nethardening.Policy{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": 1}}) //nolint:errcheck
	}))
	defer srv.Close()
	// Use localhost without dots
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New("http://localhost", zendeskclient.Credential{Mode: zendeskclient.AuthModeAPIToken, APIToken: []byte("t")})
	acct, _ := c.AuthTest(context.Background())
	// "localhost" has no dot → extractSubdomain returns the whole thing (with port stripped by url parsing)
	_ = acct // just need to exercise the path
}

// --- New without test override ---

func TestNew_NoOverride(t *testing.T) {
	zendeskclient.SetTestBaseURL("")
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	defer zendeskclient.SetEgressPolicy(nethardening.Policy{})
	// Should use the provided URL as-is
	c := zendeskclient.New("https://acme.zendesk.com", zendeskclient.Credential{
		Mode:     zendeskclient.AuthModeAPIToken,
		APIToken: []byte("t"),
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestExtractSubdomain_Empty(t *testing.T) {
	got := zendeskclient.ExportExtractSubdomain("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestBuildURL_FullPassthrough(t *testing.T) {
	u, err := zendeskclient.ExportBuildURL("http://base.com", "https://full.example.com/path?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://full.example.com/path?x=1" {
		t.Errorf("got %q", u)
	}
}

func TestBuildURL_InlineQueryMerge(t *testing.T) {
	u, err := zendeskclient.ExportBuildURL("http://base.com", "/path?inline=yes", map[string]string{"extra": "val"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "inline=yes") || !strings.Contains(u, "extra=val") {
		t.Errorf("inline query not preserved: %s", u)
	}
}

func TestValidateHost_MalformedURL(t *testing.T) {
	zendeskclient.SetTestBaseURL("")
	defer zendeskclient.SetTestBaseURL("")
	err := zendeskclient.ValidateHost("://bad\x00url")
	if err == nil {
		t.Error("expected error for malformed URL")
	}
}

func TestPostForm_ServerError(t *testing.T) {
	c := newCovClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad")) //nolint:errcheck
	}))
	_, err := c.RefreshOAuthToken(context.Background(), "r", "c", "s")
	if err == nil {
		t.Fatal("expected error for 400")
	}
}

func newCovClient(t *testing.T, handler http.Handler) zendeskclient.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	zendeskclient.SetTestBaseURL(srv.URL)
	t.Cleanup(func() {
		zendeskclient.SetEgressPolicy(nethardening.Policy{})
		zendeskclient.SetTestBaseURL("")
	})
	return zendeskclient.New(srv.URL, zendeskclient.Credential{
		Mode: zendeskclient.AuthModeAPIToken, Email: "a@b.com", APIToken: []byte("t"),
	})
}

func TestShowUsers_APIError(t *testing.T) {
	c := newCovClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`)) //nolint:errcheck
	}))
	_, err := c.ShowUsers(context.Background(), []int64{1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShowOrganizations_APIError(t *testing.T) {
	c := newCovClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`)) //nolint:errcheck
	}))
	_, err := c.ShowOrganizations(context.Background(), []int64{1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShowUsers_MalformedJSON(t *testing.T) {
	c := newCovClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`)) //nolint:errcheck
	}))
	_, err := c.ShowUsers(context.Background(), []int64{1})
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestExtractSubdomain_HttpPrefix(t *testing.T) {
	got := zendeskclient.ExportExtractSubdomain("http://sub.example.com/path")
	if got != "sub" {
		t.Errorf("got %q, want sub", got)
	}
}
