// SPDX-License-Identifier: Apache-2.0

package jiraissue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestSettingsAndCursorHelpers(t *testing.T) {
	conn := testConnection("https://jira.example.com")
	settings, err := settingsFromConnection(conn)
	if err != nil {
		t.Fatalf("settingsFromConnection returned error: %v", err)
	}
	cursor, err := decodeCursor([]byte(`{"updated_since":"2026-07-07T00:00:00Z","start_at":100}`))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	assertJiraSettingsBases(t, settings)
	assertJiraRequestHelpers(t, settings)
	assertJiraSearchCursorHelpers(t, settings, cursor)
}

func TestCustomRequestLabelPrefixIsRespected(t *testing.T) {
	settings := testSettings("https://jira.example.com")
	settings.requestLabelPrefix = "Acme Request Labels-"
	customerRequestID := uuid.NewString()
	label := requestLabel(settings, customerRequestID)
	if !strings.HasPrefix(label, "acme-request-labels-") {
		t.Fatalf("requestLabel = %q; want normalized custom prefix", label)
	}
	issue := jiraIssue{
		Fields: jiraIssueFields{
			Labels: []string{label},
		},
	}
	if got := extractCustomerRequestID(settings, issue); got != customerRequestID {
		t.Fatalf("extractCustomerRequestID = %q; want %q", got, customerRequestID)
	}
	if !issueHasMarker(settings, issue, customerRequestID) {
		t.Fatal("issueHasMarker returned false for custom label prefix")
	}
}

func TestProviderRegisteredAndDiscoversIssueSchema(t *testing.T) {
	provider, ok := core.Lookup(providerID)
	if !ok {
		t.Fatal("Lookup(jira) returned ok=false")
	}
	if provider.Provider() != providerID {
		t.Fatalf("provider id = %q, want jira", provider.Provider())
	}
	schemas, err := provider.Discover(context.Background(), core.Connection{})
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Type != "issue" || !containsString(schemas[0].Fields, "request_marker") {
		t.Fatalf("schemas = %#v; want issue schema with request_marker", schemas)
	}
}

func TestCheckFailureBranches(t *testing.T) {
	provider := NewProvider()
	result, err := provider.Check(context.Background(), core.Connection{})
	if err != nil || result.OK || !strings.Contains(result.Error, "project_key") {
		t.Fatalf("invalid settings Check = %+v err=%v; want failed result without top-level error", result, err)
	}

	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		w.Header().Set("X-Atlassian-Request-Id", "jira-auth-1")
		http.Error(w, `{"message":"bad credentials"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	provider = NewProvider(WithHTTPClient(server.Client()))
	result, err = provider.Check(context.Background(), testConnection(server.URL))
	if err != nil || result.OK || result.RequestID != "jira-auth-1" ||
		!strings.Contains(result.Error, "bad credentials") {
		t.Fatalf("HTTP failed Check = %+v err=%v; want provider error result", result, err)
	}

	provider = NewProvider(WithHTTPClient(ptrext.Of(http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})})))
	result, err = provider.Check(context.Background(), testConnection(server.URL))
	if err == nil || result.OK || !strings.Contains(result.Error, "dial failed") {
		t.Fatalf("transport failed Check = %+v err=%v; want top-level transport error", result, err)
	}
}

func TestCheckAndPullIssues(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()
	requestID := "jira-req-1"
	server := newJiraCheckAndPullServer(t, customerRequestID, requestID)
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	conn := testConnection(server.URL)

	check, err := provider.Check(context.Background(), conn)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	assertJiraCheckResult(t, check, requestID)

	result, err := provider.Pull(context.Background(), core.PullRequest{
		Connection: conn,
		Cursor:     []byte(`{"updated_since":"2026-07-07T00:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}
	assertJiraPullResult(t, result, server.URL, customerRequestID)
	assertJiraPullNextCursor(t, result.NextCursor)
}

func TestPullFailureBranches(t *testing.T) {
	provider := NewProvider()
	if _, err := provider.Pull(context.Background(), core.PullRequest{Connection: core.Connection{}}); err == nil {
		t.Fatal("Pull accepted invalid connection settings")
	}
	if _, err := provider.Pull(context.Background(), core.PullRequest{
		Connection: testConnection("https://jira.example.com"),
		Cursor:     []byte(`{"updated_since":"bad"}`),
	}); err == nil {
		t.Fatal("Pull accepted invalid cursor")
	}

	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		_, _ = w.Write([]byte(`{"issues":"not-an-array"}`))
	}))
	t.Cleanup(server.Close)
	provider = NewProvider(WithHTTPClient(server.Client()))
	if _, err := provider.Pull(context.Background(), core.PullRequest{Connection: testConnection(server.URL)}); err == nil {
		t.Fatal("Pull accepted malformed search response")
	}

	providerErrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		http.Error(w, `{"message":"jira unavailable"}`, http.StatusBadGateway)
	}))
	t.Cleanup(providerErrServer.Close)
	provider = NewProvider(WithHTTPClient(providerErrServer.Client()))
	if _, err := provider.Pull(context.Background(), core.PullRequest{Connection: testConnection(providerErrServer.URL)}); err == nil {
		t.Fatal("Pull swallowed provider search error")
	}
}

func assertJiraSettingsBases(t *testing.T, settings settings) {
	t.Helper()
	if settings.siteURL != "https://jira.example.com" || settings.apiBase != "https://jira.example.com/rest/api/3" {
		t.Fatalf("settings bases = %q / %q; want site and api base", settings.siteURL, settings.apiBase)
	}
}

func assertJiraRequestHelpers(t *testing.T, settings settings) {
	t.Helper()
	if got := requestLabel(settings, uuid.NewString()); got == "" || !strings.HasPrefix(got, defaultLabelPrefix) {
		t.Fatalf("requestLabel = %q; want prefixed marker", got)
	}
	if got := requestMarker(uuid.NewString()); got == "" || !strings.Contains(got, jiraMarkerCommentText) {
		t.Fatalf("requestMarker = %q; want marker text", got)
	}
}

func assertJiraSearchCursorHelpers(t *testing.T, settings settings, cursor cursorState) {
	t.Helper()
	rawURL, err := searchURL(settings, cursor)
	if err != nil {
		t.Fatalf("searchURL: %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse search URL: %v", err)
	}
	if got := parsed.Query().Get("jql"); !strings.Contains(got, `project = "ACME"`) || !strings.Contains(got, `updated >=`) {
		t.Fatalf("search JQL = %q; want cursor filters", got)
	}
	if !strings.Contains(rawURL, "startAt=100") {
		t.Fatalf("searchURL = %q; want cursor params", rawURL)
	}
	next, err := nextCursor(cursor, jiraSearchResponse{StartAt: 100, Total: 201, Issues: make([]jiraIssue, 100)}, time.Time{})
	if err != nil {
		t.Fatalf("nextCursor: %v", err)
	}
	var nextCursorState cursorState
	if err := json.Unmarshal(next, &nextCursorState); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode next cursor: %v", err)
	}
	if nextCursorState.StartAt != 200 || nextCursorState.UpdatedSince != "2026-07-07T00:00:00Z" {
		t.Fatalf("next cursor = %+v; want pagination to preserve watermark", nextCursorState)
	}
}

func newJiraCheckAndPullServer(t *testing.T, customerRequestID, requestID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Request-Id", requestID)
		writeJSON(t, w, map[string]any{"accountId": "bot-1", "displayName": "Attune Bot"})
	})
	mux.HandleFunc("/rest/api/3/search", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("jql"); !strings.Contains(got, `project = "ACME"`) || !strings.Contains(got, `updated >=`) {
			t.Fatalf("search JQL = %q; want project and updated filters", got)
		}
		w.Header().Set("X-Request-Id", requestID)
		writeJSON(t, w, jiraSearchResponse{
			StartAt:    0,
			MaxResults: 100,
			Total:      1,
			Issues:     []jiraIssue{jiraIssueFixture(t, "ACME-1", customerRequestID)},
		})
	})
	return httptest.NewServer(mux)
}

func assertJiraCheckResult(t *testing.T, check core.CheckResult, requestID string) {
	t.Helper()
	if !check.OK || check.RequestID != requestID {
		t.Fatalf("Check = %+v; want OK with request id", check)
	}
}

func assertJiraPullResult(t *testing.T, result core.PullResult, baseURL, customerRequestID string) {
	t.Helper()
	if len(result.Records) != 1 {
		t.Fatalf("records len = %d; want 1", len(result.Records))
	}
	record := result.Records[0]
	if record.Key != "ACME-1" || record.LocalObjectID != customerRequestID {
		t.Fatalf("record key/local = %q/%q; want issue key and request id", record.Key, record.LocalObjectID)
	}
	if record.URL != baseURL+"/browse/ACME-1" {
		t.Fatalf("record URL = %q; want browse URL", record.URL)
	}
	var payload normalizedIssue
	if err := json.Unmarshal(record.Payload, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode normalized payload: %v", err)
	}
	if payload.RequestMarker != customerRequestID || payload.CommentCount != 1 || payload.ResolvedAt == "" {
		t.Fatalf("normalized payload = %+v; want marker, comments, and resolution timestamp", payload)
	}
	if len(payload.IssueLinks) != 1 || payload.IssueLinks[0].URL != baseURL+"/browse/ACME-2" {
		t.Fatalf("issue links = %+v; want browse URL for linked issue", payload.IssueLinks)
	}
}

func assertJiraPullNextCursor(t *testing.T, raw []byte) {
	t.Helper()
	var cursor cursorState
	if err := json.Unmarshal(raw, &cursor); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode next cursor: %v", err)
	}
	if cursor.UpdatedSince != "2026-07-08T11:00:00Z" || cursor.StartAt != 0 {
		t.Fatalf("next cursor = %+v; want watermark from search results", cursor)
	}
}

type jiraPushCreateCounts struct {
	searchCalls     int
	createCalls     int
	updateCalls     int
	commentCalls    int
	transitionGets  int
	transitionPosts int
	finalGets       int
}

type jiraPushMarkerCounts struct {
	createCalls  int
	updateCalls  int
	commentCalls int
}

func TestPushCreatesIssueAddsCommentTransitionsAndReturnsMetadata(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()
	server, counts := newJiraPushCreateServer(t, customerRequestID)
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Push(context.Background(), core.PushRequest{
		Connection: testConnection(server.URL),
		Records: []core.LocalRecord{{
			ID: customerRequestID,
			Payload: marshalPayload(t, localIssuePayload{
				Title:             "Create issue",
				Body:              "Issue body",
				Status:            "shipped",
				Priority:          "high",
				CustomerRequestID: customerRequestID,
			}),
		}},
	})
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	if counts.createCalls != 1 || counts.updateCalls != 1 || counts.commentCalls != 1 || counts.transitionGets != 1 || counts.transitionPosts != 1 || counts.finalGets != 1 {
		t.Fatalf("call counts = create:%d update:%d comment:%d transitions:%d/%d final:%d", counts.createCalls, counts.updateCalls, counts.commentCalls, counts.transitionGets, counts.transitionPosts, counts.finalGets)
	}
	if len(result.Results) != 1 || result.Results[0].Key != "ACME-1" || result.Results[0].Version != "2026-07-08T13:00:00Z" {
		t.Fatalf("push result = %+v; want created issue metadata", result.Results)
	}
}

func TestPushUpdatesExistingIssueByMarker(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()
	existing := jiraIssueFixture(t, "ACME-7", customerRequestID)
	server, counts := newJiraPushMarkerLookupServer(t, existing)
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Push(context.Background(), core.PushRequest{
		Connection: testConnection(server.URL),
		Records: []core.LocalRecord{{
			ID: customerRequestID,
			Payload: marshalPayload(t, localIssuePayload{
				Title:             "Update issue",
				Body:              "Updated body",
				Status:            "open",
				CustomerRequestID: customerRequestID,
			}),
		}},
	})
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	if counts.createCalls != 0 || counts.updateCalls != 1 || counts.commentCalls != 0 {
		t.Fatalf("call counts = create:%d update:%d comment:%d; want existing issue update without comment", counts.createCalls, counts.updateCalls, counts.commentCalls)
	}
	if len(result.Results) != 1 || result.Results[0].Key != "ACME-7" {
		t.Fatalf("push result = %+v; want existing issue key", result.Results)
	}
}

func TestPushFailureAndNoopBranches(t *testing.T) {
	provider := NewProvider()
	empty, err := provider.Push(context.Background(), core.PushRequest{})
	if err != nil || len(empty.Results) != 0 {
		t.Fatalf("empty Push = %+v err=%v; want no-op", empty, err)
	}
	if _, err := provider.Push(context.Background(), core.PushRequest{
		Connection: core.Connection{},
		Records:    []core.LocalRecord{{ID: "cr-1", Payload: []byte(`{"title":"Bug"}`)}},
	}); err == nil {
		t.Fatal("Push accepted invalid connection settings")
	}

	result, err := provider.Push(context.Background(), core.PushRequest{
		Connection: testConnection("https://jira.example.com"),
		Records: []core.LocalRecord{
			{ID: "cr-bad-json", Payload: []byte(`{`)},
			{ID: "cr-empty-title", Payload: marshalPayload(t, localIssuePayload{})},
		},
	})
	if err != nil {
		t.Fatalf("Push returned top-level error: %v", err)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results len = %d; want per-record failures", len(result.Results))
	}
	for _, row := range result.Results {
		if row.Error == nil || row.Error.Kind != "validation" || row.Retryable {
			t.Fatalf("failure row = %+v; want non-retryable validation error", row)
		}
	}
}

func TestJiraWriteHelperBranches(t *testing.T) {
	setLoopbackEgress(t)
	provider := NewProvider()
	cfg := testSettings("https://jira.example.com")
	issue := jiraIssue{
		Key: "ACME-1",
		Fields: jiraIssueFields{
			Status: jiraStatus{StatusCategory: jiraStatusCategory{Key: "done"}},
		},
	}

	if err := provider.updateIssue(context.Background(), cfg, issue, core.LocalRecord{}, localIssuePayload{}); err != nil {
		t.Fatalf("empty updateIssue returned error: %v", err)
	}
	if err := provider.ensureRequestComment(context.Background(), cfg, issue, localIssuePayload{}); err != nil {
		t.Fatalf("empty ensureRequestComment returned error: %v", err)
	}
	if err := provider.ensureRequestedStatus(context.Background(), cfg, issue, "shipped"); err != nil {
		t.Fatalf("matching status category returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/ACME-1/transitions" {
			writeJSON(t, w, map[string]any{"transitions": []map[string]any{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	provider = NewProvider(WithHTTPClient(server.Client()))
	cfg = testSettings(server.URL)
	err := provider.ensureRequestedStatus(context.Background(), cfg, jiraIssue{Key: "ACME-1"}, "shipped")
	if err == nil || !strings.Contains(err.Error(), "does not expose a transition") {
		t.Fatalf("ensureRequestedStatus error = %v; want missing transition validation", err)
	}
	if err := provider.ensureRequestedStatus(context.Background(), cfg, jiraIssue{Key: "ACME-1"}, "planned"); err != nil {
		t.Fatalf("planned status without transition should be skippable, got %v", err)
	}
}

func TestJiraProviderWriteEdgeBranches(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()

	t.Run("external key not found falls back to marker search", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/ACME-404":
				http.Error(w, `{"message":"missing"}`, http.StatusNotFound)
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/search":
				writeJSON(t, w, jiraSearchResponse{
					StartAt:    0,
					MaxResults: 100,
					Total:      1,
					Issues:     []jiraIssue{jiraIssueFixture(t, "ACME-22", customerRequestID)},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		provider := NewProvider(WithHTTPClient(server.Client()))
		issue, err := provider.resolveWriteIssue(context.Background(), testSettings(server.URL), localIssuePayload{
			ExternalKey:       "ACME-404",
			CustomerRequestID: customerRequestID,
		})
		if err != nil {
			t.Fatalf("resolveWriteIssue returned error: %v", err)
		}
		if issue == nil || issue.Key != "ACME-22" {
			t.Fatalf("resolved issue = %+v; want marker search match", issue)
		}
	})

	t.Run("create issue rejects malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{`))
		}))
		t.Cleanup(server.Close)

		provider := NewProvider(WithHTTPClient(server.Client()))
		_, err := provider.createIssue(context.Background(), testSettings(server.URL), core.LocalRecord{ID: customerRequestID}, localIssuePayload{
			Title:             "Bad create response",
			CustomerRequestID: customerRequestID,
		})
		if err == nil || !strings.Contains(err.Error(), "decode jira issue response") {
			t.Fatalf("createIssue error = %v; want decode validation error", err)
		}
	})

	t.Run("get issue rejects malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/ACME-1" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{`))
		}))
		t.Cleanup(server.Close)

		provider := NewProvider(WithHTTPClient(server.Client()))
		_, err := provider.getIssue(context.Background(), testSettings(server.URL), "ACME-1")
		if err == nil || !strings.Contains(err.Error(), "decode jira issue response") {
			t.Fatalf("getIssue error = %v; want decode validation error", err)
		}
	})
}

func TestJiraPushStageFailureBranches(t *testing.T) {
	setLoopbackEgress(t)

	t.Run("update failure returns per-record error", func(t *testing.T) {
		customerRequestID := uuid.NewString()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			if r.URL.Path != "/rest/api/3/issue/ACME-1" {
				http.NotFound(w, r)
				return
			}
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, jiraIssueFixture(t, "ACME-1", customerRequestID))
			case http.MethodPut:
				http.Error(w, `{"message":"update failed"}`, http.StatusBadGateway)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		result, err := NewProvider(WithHTTPClient(server.Client())).Push(context.Background(), core.PushRequest{
			Connection: testConnection(server.URL),
			Records: []core.LocalRecord{{
				ID: customerRequestID,
				Payload: marshalPayload(t, localIssuePayload{
					ExternalKey:       "ACME-1",
					Title:             "Update issue",
					CustomerRequestID: customerRequestID,
				}),
			}},
		})
		if err != nil {
			t.Fatalf("Push returned top-level error: %v", err)
		}
		assertSingleFailedJiraWrite(t, result, "ACME-1", "update failed")
	})

	t.Run("comment failure returns per-record error", func(t *testing.T) {
		customerRequestID := uuid.NewString()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/ACME-1":
				writeJSON(t, w, jiraIssue{
					Key: "ACME-1",
					Fields: jiraIssueFields{
						Summary: "Update issue",
						Updated: "2026-07-08T11:00:00Z",
					},
				})
			case r.Method == http.MethodPut && r.URL.Path == "/rest/api/3/issue/ACME-1":
				w.WriteHeader(http.StatusNoContent)
			case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/ACME-1/comment":
				http.Error(w, `{"message":"comment failed"}`, http.StatusBadGateway)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		result, err := NewProvider(WithHTTPClient(server.Client())).Push(context.Background(), core.PushRequest{
			Connection: testConnection(server.URL),
			Records: []core.LocalRecord{{
				ID: customerRequestID,
				Payload: marshalPayload(t, localIssuePayload{
					ExternalKey:       "ACME-1",
					Title:             "Update issue",
					CustomerRequestID: customerRequestID,
				}),
			}},
		})
		if err != nil {
			t.Fatalf("Push returned top-level error: %v", err)
		}
		assertSingleFailedJiraWrite(t, result, "ACME-1", "comment failed")
	})

	t.Run("final refresh failure returns per-record error", func(t *testing.T) {
		customerRequestID := uuid.NewString()
		getCalls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			if r.URL.Path != "/rest/api/3/issue/ACME-1" {
				http.NotFound(w, r)
				return
			}
			switch r.Method {
			case http.MethodGet:
				getCalls++
				if getCalls == 1 {
					writeJSON(t, w, jiraIssueFixture(t, "ACME-1", customerRequestID))
					return
				}
				http.Error(w, `{"message":"refresh failed"}`, http.StatusBadGateway)
			case http.MethodPut:
				w.WriteHeader(http.StatusNoContent)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		result, err := NewProvider(WithHTTPClient(server.Client())).Push(context.Background(), core.PushRequest{
			Connection: testConnection(server.URL),
			Records: []core.LocalRecord{{
				ID: customerRequestID,
				Payload: marshalPayload(t, localIssuePayload{
					ExternalKey:       "ACME-1",
					Title:             "Update issue",
					CustomerRequestID: customerRequestID,
				}),
			}},
		})
		if err != nil {
			t.Fatalf("Push returned top-level error: %v", err)
		}
		assertSingleFailedJiraWrite(t, result, "ACME-1", "refresh failed")
	})
}

func TestJiraTransitionProviderBranches(t *testing.T) {
	setLoopbackEgress(t)

	t.Run("configured transition is preferred", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/ACME-1/transitions" {
				http.NotFound(w, r)
				return
			}
			writeJSON(t, w, map[string]any{"transitions": []map[string]any{
				transitionJSON("90", "Release", "done"),
				transitionJSON("31", "Done", "done"),
			}})
		}))
		t.Cleanup(server.Close)

		cfg := testSettings(server.URL)
		cfg.statusTransitions = map[string]string{"shipped": "Release"}
		provider := NewProvider(WithHTTPClient(server.Client()))
		transition, err := provider.chooseTransition(context.Background(), cfg, "ACME-1", "shipped")
		if err != nil {
			t.Fatalf("chooseTransition returned error: %v", err)
		}
		if transition == nil || transition.ID != "90" {
			t.Fatalf("chosen transition = %+v; want configured Release transition", transition)
		}
	})

	t.Run("list transitions rejects malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			_, _ = w.Write([]byte(`{"transitions":`))
		}))
		t.Cleanup(server.Close)

		provider := NewProvider(WithHTTPClient(server.Client()))
		_, err := provider.listTransitions(context.Background(), testSettings(server.URL), "ACME-1")
		if err == nil || !strings.Contains(err.Error(), "decode jira transitions response") {
			t.Fatalf("listTransitions error = %v; want decode error", err)
		}
	})

	t.Run("empty transition id is a no-op", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, map[string]any{"transitions": []map[string]any{
					transitionJSON("", "Done", "done"),
				}})
			case http.MethodPost:
				t.Fatal("transition POST should not be called for an empty transition id")
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		provider := NewProvider(WithHTTPClient(server.Client()))
		err := provider.ensureRequestedStatus(context.Background(), testSettings(server.URL), jiraIssue{Key: "ACME-1"}, "shipped")
		if err != nil {
			t.Fatalf("ensureRequestedStatus returned error: %v", err)
		}
	})

	t.Run("transition post failure bubbles up", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertJiraHeaders(t, r)
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, map[string]any{"transitions": []map[string]any{
					transitionJSON("31", "Done", "done"),
				}})
			case http.MethodPost:
				http.Error(w, `{"message":"workflow failed"}`, http.StatusBadGateway)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)

		provider := NewProvider(WithHTTPClient(server.Client()))
		err := provider.ensureRequestedStatus(context.Background(), testSettings(server.URL), jiraIssue{Key: "ACME-1"}, "shipped")
		if err == nil || !strings.Contains(err.Error(), "workflow failed") {
			t.Fatalf("ensureRequestedStatus error = %v; want transition provider failure", err)
		}
	})
}

func newJiraPushCreateServer(t *testing.T, customerRequestID string) (*httptest.Server, *jiraPushCreateCounts) {
	t.Helper()
	counts := ptrext.Of(jiraPushCreateCounts{})
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		counts.searchCalls++
		got := r.URL.Query().Get("jql")
		switch counts.searchCalls {
		case 1:
			if !strings.Contains(got, `project = "ACME"`) || !strings.Contains(got, `labels = "attune-customer-request-`) || !strings.Contains(got, customerRequestID) || !strings.Contains(got, "ORDER BY updated DESC, id DESC") {
				t.Fatalf("search JQL = %q; want project, label marker, and ordering filters", got)
			}
		case 2:
			if !strings.Contains(got, `project = "ACME"`) || !strings.Contains(got, `text ~ "`) || !strings.Contains(got, customerRequestID) || !strings.Contains(got, "ORDER BY updated DESC, id DESC") {
				t.Fatalf("search JQL = %q; want project, marker text, and ordering filters", got)
			}
		default:
			t.Fatalf("unexpected search call %d with JQL %q", counts.searchCalls, got)
		}
		writeJSON(t, w, jiraSearchResponse{StartAt: 0, MaxResults: 100, Total: 0, Issues: []jiraIssue{}})
	})
	mux.HandleFunc("/rest/api/3/issue", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		counts.createCalls++
		got := map[string]any{}
		decodeRequest(t, r, ptrext.Of(got))
		assertJiraCreateFields(t, got["fields"].(map[string]any), customerRequestID)
		writeJSON(t, w, map[string]any{
			"id":   "10001",
			"key":  "ACME-1",
			"self": serverURL(r) + "/rest/api/3/issue/ACME-1",
		})
	})
	mux.HandleFunc("/rest/api/3/issue/ACME-1", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		switch r.Method {
		case http.MethodPut:
			counts.updateCalls++
			got := map[string]any{}
			decodeRequest(t, r, ptrext.Of(got))
			assertJiraUpdateFields(t, got["fields"].(map[string]any))
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			counts.finalGets++
			writeJSON(t, w, jiraIssue{
				ID:   "10001",
				Key:  "ACME-1",
				Self: serverURL(r) + "/rest/api/3/issue/ACME-1",
				Fields: jiraIssueFields{
					Summary: "Create issue",
					Status: jiraStatus{
						ID:   "51",
						Name: "Done",
						StatusCategory: jiraStatusCategory{
							ID:   "3",
							Key:  "done",
							Name: "Done",
						},
					},
					Labels:    []string{requestLabel(testSettings(serverURL(r)), customerRequestID)},
					Project:   jiraProject{Key: "ACME"},
					IssueType: jiraIssueType{Name: "Task"},
					Updated:   "2026-07-08T13:00:00Z",
					Created:   "2026-07-08T12:00:00Z",
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/rest/api/3/issue/ACME-1/comment", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		counts.commentCalls++
		got := map[string]any{}
		decodeRequest(t, r, ptrext.Of(got))
		assertJiraCommentContainsMarker(t, got, customerRequestID)
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/rest/api/3/issue/ACME-1/transitions", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		switch r.Method {
		case http.MethodGet:
			counts.transitionGets++
			if got := r.URL.Query().Get("expand"); got != "transitions.fields" {
				t.Fatalf("expand = %q; want transitions.fields", got)
			}
			writeJSON(t, w, map[string]any{
				"transitions": []map[string]any{
					{
						"id":   "31",
						"name": "Done",
						"to": map[string]any{
							"id":   "51",
							"name": "Done",
							"statusCategory": map[string]any{
								"id":   "3",
								"key":  "done",
								"name": "Done",
							},
						},
					},
				},
			})
		case http.MethodPost:
			counts.transitionPosts++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux), counts
}

func newJiraPushMarkerLookupServer(t *testing.T, existing jiraIssue) (*httptest.Server, *jiraPushMarkerCounts) {
	t.Helper()
	counts := ptrext.Of(jiraPushMarkerCounts{})
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeJSON(t, w, jiraSearchResponse{StartAt: 0, MaxResults: 100, Total: 1, Issues: []jiraIssue{existing}})
	})
	mux.HandleFunc("/rest/api/3/issue/ACME-7", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		switch r.Method {
		case http.MethodPut:
			counts.updateCalls++
			got := map[string]any{}
			decodeRequest(t, r, ptrext.Of(got))
			assertJiraUpdateFields(t, got["fields"].(map[string]any))
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			writeJSON(t, w, existing)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/rest/api/3/issue/ACME-7/comment", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		counts.commentCalls++
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/rest/api/3/issue", func(w http.ResponseWriter, r *http.Request) {
		assertJiraHeaders(t, r)
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		counts.createCalls++
		t.Fatal("create should not be called when marker lookup succeeds")
	})
	return httptest.NewServer(mux), counts
}

func assertJiraCreateFields(t *testing.T, fields map[string]any, customerRequestID string) {
	t.Helper()
	if got := fields["summary"]; got != "Create issue" {
		t.Fatalf("create summary = %v; want Create issue", got)
	}
	if _, ok := fields["project"]; !ok {
		t.Fatal("create request missing project")
	}
	if _, ok := fields["issuetype"]; !ok {
		t.Fatal("create request missing issuetype")
	}
	raw, _ := json.Marshal(fields)
	if !strings.Contains(string(raw), customerRequestID) {
		t.Fatalf("create fields = %s; want request marker", string(raw))
	}
}

func assertJiraUpdateFields(t *testing.T, fields map[string]any) {
	t.Helper()
	if _, ok := fields["project"]; ok {
		t.Fatal("update request should omit project")
	}
	if _, ok := fields["issuetype"]; ok {
		t.Fatal("update request should omit issuetype")
	}
}

func assertJiraCommentContainsMarker(t *testing.T, got map[string]any, customerRequestID string) {
	t.Helper()
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), customerRequestID) {
		t.Fatalf("comment payload = %s; want request marker", string(raw))
	}
}

func TestClassifyErrorAndValidationHelpers(t *testing.T) {
	if got := classifyError(jiraHTTPError(http.StatusTooManyRequests, "https://jira.example.com/rest/api/3/search?token=secret", []byte(`{"message":"rate limit"}`), "req-1", "1")); got.Kind != "rate_limited" || !got.Retryable {
		t.Fatalf("classified error = %+v; want retryable rate limit", got)
	}
	if strings.Contains(classifyError(jiraHTTPError(http.StatusTooManyRequests, "https://jira.example.com/rest/api/3/search?token=secret", []byte(`{"message":"rate limit"}`), "req-1", "1")).Message, "token=secret") {
		t.Fatal("classified message leaked query secret")
	}
	if _, err := decodeCursor([]byte(`{"updated_since":"bad"}`)); err == nil {
		t.Fatal("decodeCursor accepted invalid timestamp")
	}
	if _, err := settingsFromConnection(core.Connection{ProviderConfig: []byte(`{"project_key":"ACME","issue_type":"Task","email":"bot@example.com"}`)}); err == nil {
		t.Fatal("settingsFromConnection accepted missing credential")
	}
}

func jiraIssueFixture(t *testing.T, key, customerRequestID string) jiraIssue {
	t.Helper()
	description := adfDocument("Issue body\n\n" + requestMarker(customerRequestID))
	commentBody := adfDocument("Follow-up\n\n" + requestMarker(customerRequestID))
	rawDescription, err := json.Marshal(description)
	if err != nil {
		t.Fatalf("marshal description: %v", err)
	}
	rawCommentBody, err := json.Marshal(commentBody)
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}
	return jiraIssue{
		ID:   "10001",
		Key:  key,
		Self: "https://jira.example.com/rest/api/3/issue/" + key,
		Fields: jiraIssueFields{
			Summary:     "Sync me",
			Description: rawDescription,
			Status: jiraStatus{
				ID:   "21",
				Name: "In Progress",
				StatusCategory: jiraStatusCategory{
					ID:   "4",
					Key:  "indeterminate",
					Name: "In Progress",
				},
			},
			Labels: []string{"bug", requestLabel(testSettings("https://jira.example.com"), customerRequestID)},
			Project: jiraProject{
				ID:  "10000",
				Key: "ACME",
			},
			IssueType: jiraIssueType{
				ID:   "10001",
				Name: "Task",
			},
			Comment: jiraCommentPage{
				Total: 1,
				Comments: []jiraComment{
					{
						ID:   "20001",
						Body: rawCommentBody,
						Author: ptrext.Of(jiraUser{
							DisplayName: "Alice",
						}),
						Created: "2026-07-08T10:00:00Z",
						Updated: "2026-07-08T10:10:00Z",
					},
				},
			},
			Resolution: ptrext.Of(jiraResolution{
				ID:   "1",
				Name: "Done",
			}),
			ResolutionDate: "2026-07-08T12:00:00Z",
			Updated:        "2026-07-08T11:00:00Z",
			Created:        "2026-07-07T09:00:00Z",
			IssueLinks: []jiraIssueLink{
				{
					Type: jiraLinkType{Name: "Blocks"},
					OutwardIssue: ptrext.Of(jiraLinkedIssue{
						ID:   "10002",
						Key:  "ACME-2",
						Self: "https://jira.example.com/rest/api/3/issue/ACME-2",
					}),
				},
			},
		},
	}
}

func testSettings(apiBase string) settings {
	return settings{
		siteURL:            strings.TrimRight(apiBase, "/"),
		apiBase:            strings.TrimRight(apiBase, "/") + defaultAPIPath,
		projectKey:         "ACME",
		issueType:          "Task",
		email:              "bot@example.com",
		token:              "jira-token",
		requestLabelPrefix: defaultLabelPrefix,
	}
}

func testConnection(apiBase string) core.Connection {
	raw, err := json.Marshal(providerConfig{
		SiteURL:    strings.TrimRight(apiBase, "/"),
		ProjectKey: "ACME",
		IssueType:  "Task",
		Email:      "bot@example.com",
	})
	if err != nil {
		panic(err)
	}
	return core.Connection{
		Provider:       providerID,
		ProviderConfig: raw,
		Credential:     []byte("jira-token"),
	}
}

func setLoopbackEgress(t *testing.T) {
	t.Helper()
	core.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	t.Cleanup(func() { core.SetEgressPolicy(nethardening.Policy{}) })
}

func assertJiraHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	want := "Basic " + basicAuth("bot@example.com", "jira-token")
	if got := r.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q; want %q", got, want)
	}
	if got := r.Header.Get("User-Agent"); got != userAgent {
		t.Fatalf("User-Agent = %q; want %q", got, userAgent)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func decodeRequest(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode request: %v", err)
	}
}

func marshalPayload(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func serverURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func transitionJSON(id, name, category string) map[string]any {
	return map[string]any{
		"id":   id,
		"name": name,
		"to": map[string]any{
			"id":   id + "-to",
			"name": name,
			"statusCategory": map[string]any{
				"key": category,
			},
		},
	}
}

func assertSingleFailedJiraWrite(t *testing.T, result core.PushResult, key, message string) {
	t.Helper()
	if len(result.Results) != 1 {
		t.Fatalf("push results len = %d; want one failure", len(result.Results))
	}
	row := result.Results[0]
	if row.Key != key || row.Error == nil || !row.Retryable || !strings.Contains(row.Error.Message, message) {
		t.Fatalf("push result = %+v; want retryable failure for %s containing %q", row, key, message)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
