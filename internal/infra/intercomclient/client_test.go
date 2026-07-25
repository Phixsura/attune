// SPDX-License-Identifier: Apache-2.0

package intercomclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/intercomclient"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

func newTestClient(t *testing.T, handler http.Handler) intercomclient.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	intercomclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	t.Cleanup(func() {
		srv.Close()
		intercomclient.SetTestBaseURL("")
		intercomclient.SetEgressPolicy(nethardening.Policy{})
	})
	intercomclient.SetTestBaseURL(srv.URL)
	return intercomclient.New("us", "test-token")
}

func TestAuthTest_Success(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q", got)
		}
		if got := r.Header.Get("Intercom-Version"); got != "2.16" {
			t.Errorf("version header = %q", got)
		}
		fmt.Fprint(w, `{"type":"admin","email":"admin@example.com","app":{"type":"app","id_code":"abc123workspace","name":"Acme","region":"US"}}`)
	}))

	info, err := client.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("AuthTest: %v", err)
	}
	if info.WorkspaceID != "abc123workspace" {
		t.Errorf("WorkspaceID = %q", info.WorkspaceID)
	}
	if info.WorkspaceName != "Acme" {
		t.Errorf("WorkspaceName = %q", info.WorkspaceName)
	}
	if info.Region != "us" {
		t.Errorf("Region = %q", info.Region)
	}
	if info.AdminEmail != "admin@example.com" {
		t.Errorf("AdminEmail = %q", info.AdminEmail)
	}
}

func TestAuthTest_Unauthorized(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error.list","errors":[{"code":"unauthorized","message":"Access Token Invalid"}]}`)
	}))

	_, err := client.AuthTest(context.Background())
	var ae intercomclient.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %v", err)
	}
	if ae.Code != "unauthorized" || ae.Status != http.StatusUnauthorized {
		t.Errorf("APIError = %+v", ae)
	}
	if !ae.Permanent() {
		t.Error("401 unauthorized should be permanent")
	}
}

func TestAuthTest_PlanRestricted(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"type":"error.list","errors":[{"code":"api_plan_restricted","message":"Active subscription needed."}]}`)
	}))

	_, err := client.AuthTest(context.Background())
	var ae intercomclient.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %v", err)
	}
	if ae.Code != "api_plan_restricted" {
		t.Errorf("Code = %q", ae.Code)
	}
	if !ae.Permanent() {
		t.Error("api_plan_restricted should be permanent")
	}
}

func TestSearchConversations_QueryShapeAndPagination(t *testing.T) {
	var captured map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations/search" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil { // ptrext:allow json-decode-out-param
			t.Fatalf("decode body: %v", err)
		}
		fmt.Fprint(w, `{
			"type":"conversation.list",
			"total_count":2,
			"pages":{"type":"pages","next":{"page":2,"starting_after":"cursor-abc"}},
			"conversations":[
				{"id":"101","state":"open","created_at":1700000000,"updated_at":1700000100,
				 "source":{"type":"conversation","subject":"","body":"help please","author":{"type":"user","id":"c1","name":"Alice"}},
				 "contacts":{"contacts":[{"id":"c1","external_id":"ext-1"}]}},
				{"id":"102","state":"closed","created_at":1700000200,"updated_at":1700000300,
				 "source":{"type":"email","subject":"Bug","body":"broken","author":{"type":"lead","id":"c2"}},
				 "contacts":{"contacts":[{"id":"c2","external_id":""}]}}
			]}`)
	}))

	page, err := client.SearchConversations(context.Background(), 1699999999, 1700009999, "prev-cursor")
	if err != nil {
		t.Fatalf("SearchConversations: %v", err)
	}
	if len(page.Conversations) != 2 {
		t.Fatalf("got %d conversations", len(page.Conversations))
	}
	if page.StartingAfter != "cursor-abc" {
		t.Errorf("StartingAfter = %q", page.StartingAfter)
	}
	if page.TotalCount != 2 {
		t.Errorf("TotalCount = %d", page.TotalCount)
	}
	if page.Conversations[0].UpdatedAt != 1700000100 {
		t.Errorf("UpdatedAt = %d", page.Conversations[0].UpdatedAt)
	}

	verifySearchRequestShape(t, captured)
}

// verifySearchRequestShape pins the exact wire shape of the search body:
// AND query over updated_at bounds, ascending sort, cursor pagination.
func verifySearchRequestShape(t *testing.T, captured map[string]any) {
	t.Helper()
	query := captured["query"].(map[string]any)
	if query["operator"] != "AND" {
		t.Errorf("query operator = %v", query["operator"])
	}
	filters := query["value"].([]any)
	if len(filters) != 2 {
		t.Fatalf("got %d filters", len(filters))
	}
	f0 := filters[0].(map[string]any)
	if f0["field"] != "updated_at" || f0["operator"] != ">" || f0["value"].(float64) != 1699999999 {
		t.Errorf("filter[0] = %v", f0)
	}
	f1 := filters[1].(map[string]any)
	if f1["field"] != "updated_at" || f1["operator"] != "<" {
		t.Errorf("filter[1] = %v", f1)
	}
	pag := captured["pagination"].(map[string]any)
	if pag["per_page"].(float64) != 150 {
		t.Errorf("per_page = %v", pag["per_page"])
	}
	if pag["starting_after"] != "prev-cursor" {
		t.Errorf("starting_after = %v", pag["starting_after"])
	}
	sort := captured["sort"].(map[string]any)
	if sort["field"] != "updated_at" || sort["order"] != "ascending" {
		t.Errorf("sort = %v", sort)
	}
}

func TestSearchConversations_LastPage(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"type":"conversation.list","total_count":0,"pages":{"type":"pages"},"conversations":[]}`)
	}))

	page, err := client.SearchConversations(context.Background(), 0, 100, "")
	if err != nil {
		t.Fatalf("SearchConversations: %v", err)
	}
	if page.StartingAfter != "" {
		t.Errorf("StartingAfter = %q, want empty on last page", page.StartingAfter)
	}
}

func TestGetConversation_PlaintextAndParts(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations/101" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("display_as"); got != "plaintext" {
			t.Errorf("display_as = %q", got)
		}
		fmt.Fprint(w, `{
			"id":"101","title":"Export bug","state":"open","priority":"priority",
			"created_at":1700000000,"updated_at":1700000500,
			"admin_assignee_id":9,"team_assignee_id":5,
			"source":{"type":"conversation","subject":"Export","body":"PDF export is broken","author":{"type":"user","id":"c1","name":"Alice","email":"alice@example.com"}},
			"contacts":{"contacts":[{"id":"c1","external_id":"ext-1"}]},
			"tags":{"tags":[{"id":"t1","name":"bug"}]},
			"conversation_rating":{"rating":3,"remark":"meh"},
			"ai_agent_participated":true,
			"conversation_parts":{"conversation_parts":[
				{"id":"p1","part_type":"comment","body":"Thanks, checking","created_at":1700000100,"author":{"type":"admin","id":"a1","name":"Bob"}},
				{"id":"p2","part_type":"note","body":"internal only","created_at":1700000200,"author":{"type":"admin","id":"a1"}},
				{"id":"p3","part_type":"comment","body":"Still broken","created_at":1700000300,"author":{"type":"user","id":"c1","name":"Alice"},"redacted":false}
			]}}`)
	}))

	conv, err := client.GetConversation(context.Background(), "101")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if conv.ID != "101" || conv.Title != "Export bug" {
		t.Errorf("conv = %+v", conv)
	}
	if len(conv.Parts.Parts) != 3 {
		t.Fatalf("got %d parts", len(conv.Parts.Parts))
	}
	if conv.Parts.Parts[1].PartType != "note" {
		t.Errorf("part[1].PartType = %q", conv.Parts.Parts[1].PartType)
	}
	if conv.Rating == nil || conv.Rating.Rating != 3 {
		t.Errorf("rating = %+v", conv.Rating)
	}
	if !conv.AIAgentParticipated {
		t.Error("AIAgentParticipated should be true")
	}
	if len(conv.Tags.Tags) != 1 || conv.Tags.Tags[0].Name != "bug" {
		t.Errorf("tags = %+v", conv.Tags)
	}
}

func TestSearchContacts_ChunksAndDecode(t *testing.T) {
	var calls int
	var batchSizes []int
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/contacts/search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil { // ptrext:allow json-decode-out-param
			t.Fatalf("decode: %v", err)
		}
		filters := body["query"].(map[string]any)["value"].([]any)
		ids := filters[0].(map[string]any)["value"].([]any)
		batchSizes = append(batchSizes, len(ids))
		fmt.Fprintf(w, `{"type":"list","data":[{"id":"c%d","external_id":"ext","role":"user","email":"u@example.com","name":"U"}]}`, calls)
	}))

	// 30 IDs → 2 chunks (25 + 5).
	ids := make([]string, 30)
	for i := range ids {
		ids[i] = "c" + strconv.Itoa(i)
	}
	contacts, err := client.SearchContacts(context.Background(), ids)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(batchSizes) != 2 || batchSizes[0] != 25 || batchSizes[1] != 5 {
		t.Errorf("batchSizes = %v", batchSizes)
	}
	if len(contacts) != 2 {
		t.Errorf("got %d contacts", len(contacts))
	}
}

func TestSearchContacts_Empty(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no request expected for empty ID list")
	}))
	contacts, err := client.SearchContacts(context.Background(), nil)
	if err != nil || contacts != nil {
		t.Errorf("got %v, %v", contacts, err)
	}
}

func TestRateLimit_UsesResetHeader(t *testing.T) {
	resetAt := time.Now().Add(7 * time.Second).Unix()
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	_, err := client.AuthTest(context.Background())
	var rle intercomclient.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("want RateLimitError, got %v", err)
	}
	if rle.RetryAfter < time.Second || rle.RetryAfter > 10*time.Second {
		t.Errorf("RetryAfter = %s, want ~7s", rle.RetryAfter)
	}
}

func TestGetCompany_Profile(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/companies/co-9" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"type":"company","id":"co-9","name":"Customer Co","monthly_spend":1200,"size":85,"industry":"Software","plan":{"type":"plan","id":"p1","name":"Pro"}}`)
	}))

	company, err := client.GetCompany(context.Background(), "co-9")
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if company.MonthlySpend != 1200 || company.Size != 85 || company.Plan.Name != "Pro" || company.Industry != "Software" {
		t.Errorf("company = %+v", company)
	}
}

func TestRateBudget_TracksRemainingHeader(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4321")
		fmt.Fprint(w, `{"type":"admin","email":"a@b.c","app":{"id_code":"ws","name":"W","region":"US"}}`)
	}))

	if got := client.RateBudget(); got != -1 {
		t.Errorf("RateBudget before first response = %d, want -1", got)
	}
	if _, err := client.AuthTest(context.Background()); err != nil {
		t.Fatalf("AuthTest: %v", err)
	}
	if got := client.RateBudget(); got != 4321 {
		t.Errorf("RateBudget = %d, want 4321", got)
	}
}

func TestRateLimit_FallbackWithoutHeader(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	_, err := client.AuthTest(context.Background())
	var rle intercomclient.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("want RateLimitError, got %v", err)
	}
	if rle.RetryAfter != 10*time.Second {
		t.Errorf("RetryAfter = %s, want 10s fallback", rle.RetryAfter)
	}
}

func TestServerError_NotPermanent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "bad gateway")
	}))

	_, err := client.AuthTest(context.Background())
	var ae intercomclient.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %v", err)
	}
	if ae.Permanent() {
		t.Error("502 should not be permanent")
	}
}

func TestBaseURL_Regions(t *testing.T) {
	cases := map[string]string{
		"us":      "https://api.intercom.io",
		"eu":      "https://api.eu.intercom.io",
		"au":      "https://api.au.intercom.io",
		"US":      "https://api.intercom.io",
		" eu ":    "https://api.eu.intercom.io",
		"unknown": "https://api.intercom.io",
	}
	for region, want := range cases {
		if got := intercomclient.BaseURL(region); got != want {
			t.Errorf("BaseURL(%q) = %q, want %q", region, got, want)
		}
	}
}

func TestValidRegion(t *testing.T) {
	for _, ok := range []string{"us", "eu", "au", " US "} {
		if !intercomclient.ValidRegion(ok) {
			t.Errorf("ValidRegion(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "asia", "us-east"} {
		if intercomclient.ValidRegion(bad) {
			t.Errorf("ValidRegion(%q) = true", bad)
		}
	}
}

func TestValidateHost(t *testing.T) {
	intercomclient.SetTestBaseURL("")
	if err := intercomclient.ValidateHost("https://api.intercom.io"); err != nil {
		t.Errorf("api.intercom.io rejected: %v", err)
	}
	if err := intercomclient.ValidateHost("https://api.eu.intercom.io"); err != nil {
		t.Errorf("api.eu.intercom.io rejected: %v", err)
	}
	if err := intercomclient.ValidateHost("https://evil.example.com"); err == nil {
		t.Error("evil.example.com accepted")
	}
	// Test override exempts host validation.
	intercomclient.SetTestBaseURL("http://127.0.0.1:9999")
	defer intercomclient.SetTestBaseURL("")
	if err := intercomclient.ValidateHost("http://127.0.0.1:9999"); err != nil {
		t.Errorf("test override rejected: %v", err)
	}
}

func TestAPIError_Messages(t *testing.T) {
	e := intercomclient.APIError{Method: "/me", Status: 401, Code: "unauthorized"}
	if e.Error() == "" {
		t.Error("empty error message")
	}
	empty := intercomclient.APIError{Method: "/me"}
	if empty.Error() == "" {
		t.Error("empty error message for codeless error")
	}
}
