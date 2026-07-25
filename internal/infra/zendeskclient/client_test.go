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

func newTestClient(t *testing.T, handler http.Handler) zendeskclient.Client {
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
		Mode:     zendeskclient.AuthModeAPIToken,
		Email:    "admin@test.com",
		APIToken: []byte("test-token"),
	})
}

func TestAuthTest_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/users/me.json" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"user": map[string]any{"id": float64(12345), "name": "Admin", "email": "admin@test.com"},
		})
	}))
	acct, err := c.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("AuthTest() error = %v", err)
	}
	if acct.AccountID != 12345 {
		t.Errorf("AccountID = %d, want 12345", acct.AccountID)
	}
}

func TestAuthTest_Unauthorized(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Couldn't authenticate you"}`)) //nolint:errcheck
	}))
	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var ae zendeskclient.APIError
	if !errors.As(err, &ae) || !ae.Permanent() {
		t.Errorf("expected permanent APIError, got: %v", err)
	}
}

func TestIncrementalTickets_WithCursor(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := r.URL.Query().Get("cursor")
		if cur != "resume" {
			t.Errorf("cursor = %q, want resume", cur)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zendeskclient.TicketPage{ //nolint:errcheck
			Tickets:     []zendeskclient.Ticket{{ID: 1, Status: "open"}},
			AfterCursor: "next",
			EndOfStream: true,
		})
	}))
	page, err := c.IncrementalTickets(context.Background(), "resume", 0)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(page.Tickets) != 1 {
		t.Fatalf("got %d tickets, want 1", len(page.Tickets))
	}
	if !page.EndOfStream {
		t.Error("expected EndOfStream")
	}
}

func TestTicketComments_SmallTicket(t *testing.T) {
	var reqCount int
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"comments": []map[string]any{
				{"id": 1, "body": "Public", "public": true},
				{"id": 2, "body": "Internal", "public": false},
			},
			"links": map[string]string{"next": ""},
		})
	}))
	comments, err := c.TicketComments(context.Background(), 42)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if reqCount != 1 {
		t.Errorf("reqCount = %d, want 1", reqCount)
	}
	if len(comments) != 1 {
		t.Errorf("got %d public comments, want 1", len(comments))
	}
}

func TestRefreshOAuthToken_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/tokens" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if gt := r.FormValue("grant_type"); gt != "refresh_token" {
			t.Errorf("grant_type = %q", gt)
		}
		if cid := r.FormValue("client_id"); cid != "my-client-id" {
			t.Errorf("client_id = %q, want my-client-id", cid)
		}
		if cs := r.FormValue("client_secret"); cs != "my-client-secret" {
			t.Errorf("client_secret = %q, want my-client-secret", cs)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "bearer",
		})
	}))
	tok, err := c.RefreshOAuthToken(context.Background(), "old-refresh", "my-client-id", "my-client-secret")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if tok.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
}

func TestValidateHost(t *testing.T) {
	zendeskclient.SetTestBaseURL("")
	defer zendeskclient.SetTestBaseURL("")

	if err := zendeskclient.ValidateHost("https://acme.zendesk.com"); err != nil {
		t.Errorf("valid host rejected: %v", err)
	}
	if err := zendeskclient.ValidateHost("https://evil.example.com"); err == nil {
		t.Error("non-zendesk host should be rejected")
	}

	zendeskclient.SetTestBaseURL("http://127.0.0.1:9999")
	if err := zendeskclient.ValidateHost("http://127.0.0.1:9999"); err != nil {
		t.Errorf("test override should allow any host: %v", err)
	}
}

func TestBuildURL_SafeEncoding(t *testing.T) {
	zendeskclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	zendeskclient.SetTestBaseURL("http://localhost:9999")
	defer func() {
		zendeskclient.SetEgressPolicy(nethardening.Policy{})
		zendeskclient.SetTestBaseURL("")
	}()
	// We can't call buildURL directly (unexported), but we can verify
	// through an API call that params are correctly encoded.
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
	}))
	defer srv.Close()
	zendeskclient.SetTestBaseURL(srv.URL)
	c := zendeskclient.New(srv.URL, zendeskclient.Credential{Mode: zendeskclient.AuthModeAPIToken, APIToken: []byte("t")})
	_, _ = c.IncrementalTickets(context.Background(), "a&b=c", 0)
	if !strings.Contains(gotURL, "cursor=a%26b%3Dc") {
		t.Errorf("expected encoded params, got URL %s", gotURL)
	}
}
