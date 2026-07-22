// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestProviderRegistered(t *testing.T) {
	provider, ok := core.Lookup(providerID)
	if !ok {
		t.Fatal("Lookup(github) returned ok=false")
	}
	if provider.Provider() != providerID {
		t.Fatalf("provider id = %q, want github", provider.Provider())
	}
}

func TestConfigParsesRepoURLsAndDiscoversSchema(t *testing.T) {
	owner, repoName, err := parseRepoURL("https://github.com/acme/app.git")
	if err != nil {
		t.Fatalf("parseRepoURL returned error: %v", err)
	}
	if owner != "acme" || repoName != "app" {
		t.Fatalf("repo = %s/%s; want acme/app", owner, repoName)
	}
	if _, _, err := parseRepoURL("ssh://github.com/acme/app"); err == nil {
		t.Fatal("parseRepoURL accepted non-HTTPS URL")
	}
	if _, _, err := resolveRepo(providerConfig{Owner: "acme/org", Repo: "app"}); err == nil {
		t.Fatal("resolveRepo accepted owner with slash")
	}
	settings, err := settingsFromConnection(core.Connection{
		BaseURL:    "https://api.github.com/",
		Credential: []byte(" github-token "),
		ProviderConfig: marshalPayload(t, providerConfig{
			RepoURL: "https://github.com/acme/app.git",
		}),
	})
	if err != nil {
		t.Fatalf("settingsFromConnection returned error: %v", err)
	}
	if settings.owner != "acme" || settings.repo != "app" ||
		settings.apiBase != defaultAPIBase || settings.token != "github-token" {
		t.Fatalf("settings = %+v; want parsed repo and trimmed token", settings)
	}
}

func TestSettingsFromConnectionAppliesSafeDefaults(t *testing.T) {
	settings, err := settingsFromConnection(core.Connection{
		Credential:     []byte("github-token"),
		ProviderConfig: marshalPayload(t, providerConfig{Owner: "acme", Repo: "app"}),
	})
	if err != nil {
		t.Fatalf("settingsFromConnection returned error: %v", err)
	}
	if !settings.syncComments || settings.managedLabelPrefix != defaultManagedLabelPrefix ||
		settings.linkedExistingWritePolicy != defaultLinkedExistingWritePolicy {
		t.Fatalf("settings defaults = %+v; want comment sync and safe write policy", settings)
	}
}

func TestNormalizeProviderOptionsRejectsUnsafeManagedLabelPrefix(t *testing.T) {
	_, err := normalizeProviderOptions(providerConfig{
		ManagedLabelPrefix: "attune/\n",
	})
	if err == nil || !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("normalizeProviderOptions error = %v; want control character rejection", err)
	}
}

func TestDiscoverReturnsIssueSchema(t *testing.T) {
	provider := NewProvider(WithHTTPClient(nil))
	schemas, err := provider.Discover(context.Background(), core.Connection{})
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Type != "issue" || len(schemas[0].WritableFields) == 0 {
		t.Fatalf("schemas = %#v; want issue schema with writable fields", schemas)
	}
	if !containsString(schemas[0].Fields, "delivery_artifact.external_key") {
		t.Fatalf("schema fields = %#v; want delivery artifact discovery fields", schemas[0].Fields)
	}
}

func TestConfigRejectsInvalidConnectionSettings(t *testing.T) {
	tests := []struct {
		name string
		conn core.Connection
		want string
	}{
		{name: "bad provider config", conn: core.Connection{ProviderConfig: []byte(`{`)}, want: "decode github provider_config"},
		{name: "missing repository", conn: core.Connection{Credential: []byte("token")}, want: "requires repo_url"},
		{
			name: "bad api base",
			conn: core.Connection{
				Credential:     []byte("token"),
				ProviderConfig: marshalPayload(t, providerConfig{Owner: "acme", Repo: "app", APIBaseURL: "http://example.com"}),
			},
			want: "api_base_url must use https",
		},
		{
			name: "missing token",
			conn: core.Connection{
				ProviderConfig: marshalPayload(t, providerConfig{Owner: "acme", Repo: "app"}),
			},
			want: "credential is required",
		},
		{
			name: "unsupported linked existing policy",
			conn: core.Connection{
				Credential:     []byte("token"),
				ProviderConfig: marshalPayload(t, providerConfig{Owner: "acme", Repo: "app", LinkedExistingWritePolicy: "replace_everything"}),
			},
			want: "linked_existing_write_policy is unsupported",
		},
		{
			name: "unsupported body section mode",
			conn: core.Connection{
				Credential:     []byte("token"),
				ProviderConfig: marshalPayload(t, providerConfig{Owner: "acme", Repo: "app", BodySectionMode: "replace"}),
			},
			want: "body_section_mode is unsupported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := settingsFromConnection(tt.conn)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("settingsFromConnection error = %v; want substring %q", err, tt.want)
			}
		})
	}
}

func TestDecodeProviderConfigTrimsFields(t *testing.T) {
	cfg, err := decodeProviderConfig([]byte(`{"repo_url":" https://github.com/acme/app.git ","owner":" acme ","repo":" app ","api_base_url":" https://api.github.com/ "}`))
	if err != nil {
		t.Fatalf("decodeProviderConfig returned error: %v", err)
	}
	if cfg.RepoURL != "https://github.com/acme/app.git" || cfg.Owner != "acme" ||
		cfg.Repo != "app" || cfg.APIBaseURL != "https://api.github.com/" {
		t.Fatalf("trimmed config = %+v", cfg)
	}
}

func TestResolveRepoHelperBranches(t *testing.T) {
	if owner, repoName, err := resolveRepo(providerConfig{Owner: "acme", Repo: "app.git"}); err != nil ||
		owner != "acme" || repoName != "app" {
		t.Fatalf("resolveRepo direct = %s/%s err=%v; want acme/app", owner, repoName, err)
	}
	if _, _, err := resolveRepo(providerConfig{Owner: "acme", Repo: "team/app"}); err == nil {
		t.Fatal("resolveRepo accepted repo with slash")
	}
}

func TestParseRepoURLHelperBranches(t *testing.T) {
	setLoopbackEgress(t)

	if _, _, err := parseRepoURL("%"); err == nil {
		t.Fatal("parseRepoURL accepted malformed URL")
	}
	if _, _, err := parseRepoURL("https://github.com/acme"); err == nil {
		t.Fatal("parseRepoURL accepted missing repo path")
	}
	if owner, repoName, err := parseRepoURL("http://127.0.0.1/acme/app"); err != nil ||
		owner != "acme" || repoName != "app" {
		t.Fatalf("loopback repo URL = %s/%s err=%v; want acme/app", owner, repoName, err)
	}
}

func TestResolveAPIBaseHelperBranches(t *testing.T) {
	setLoopbackEgress(t)

	if got, err := resolveAPIBase(" https://api.github.com/ ", ""); err != nil || got != defaultAPIBase {
		t.Fatalf("resolveAPIBase conn default = %q err=%v; want %q", got, err, defaultAPIBase)
	}
	if got, err := resolveAPIBase("https://ignored.example", "http://127.0.0.1/api/"); err != nil ||
		got != "http://127.0.0.1/api" {
		t.Fatalf("resolveAPIBase config base = %q err=%v; want loopback api", got, err)
	}
	if got, err := resolveAPIBase("", ""); err != nil || got != defaultAPIBase {
		t.Fatalf("resolveAPIBase empty = %q err=%v; want default", got, err)
	}
	if _, err := resolveAPIBase("", "%"); err == nil {
		t.Fatal("resolveAPIBase accepted malformed URL")
	}
}

func TestCursorHelpersTrimAndRejectBadJSON(t *testing.T) {
	empty, err := decodeCursor(nil)
	if err != nil || empty != (cursorState{}) {
		t.Fatalf("empty cursor = %+v err=%v; want zero cursor", empty, err)
	}
	cursor, err := decodeCursor([]byte(`{"updated_since":" 2026-07-08T01:02:03Z ","next_url":" https://api.github.com/repos/acme/app/issues?page=2 "}`))
	if err != nil {
		t.Fatalf("decodeCursor returned error: %v", err)
	}
	if cursor.UpdatedSince != "2026-07-08T01:02:03Z" ||
		cursor.NextURL != "https://api.github.com/repos/acme/app/issues?page=2" {
		t.Fatalf("cursor = %+v; want trimmed fields", cursor)
	}
	if _, err := decodeCursor([]byte(`{`)); err == nil {
		t.Fatal("decodeCursor accepted invalid JSON")
	}
	out, err := encodeCursor(cursorState{UpdatedSince: "2026-07-08T01:02:03Z"})
	if err != nil || string(out) != `{"updated_since":"2026-07-08T01:02:03Z"}` {
		t.Fatalf("encodeCursor = %s err=%v", string(out), err)
	}
}

func TestClassifyHTTPStatusBranches(t *testing.T) {
	tests := []struct {
		status        int
		body          []byte
		wantKind      string
		wantRetryable bool
	}{
		{status: http.StatusUnauthorized, wantKind: "auth_failed"},
		{status: http.StatusForbidden, body: []byte(`{"message":"secondary rate limit"}`), wantKind: "rate_limited", wantRetryable: true},
		{status: http.StatusForbidden, body: []byte(`{"message":"forbidden"}`), wantKind: "auth_failed"},
		{status: http.StatusNotFound, wantKind: "not_found"},
		{status: http.StatusUnprocessableEntity, wantKind: "validation"},
		{status: http.StatusBadGateway, wantKind: "provider_unavailable", wantRetryable: true},
		{status: http.StatusInternalServerError, wantKind: "provider_unavailable", wantRetryable: true},
		{status: http.StatusTeapot, wantKind: "provider_error"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			kind, retryable := classifyHTTPStatus(tt.status, tt.body)
			if kind != tt.wantKind || retryable != tt.wantRetryable {
				t.Fatalf("classifyHTTPStatus = %q/%t; want %q/%t", kind, retryable, tt.wantKind, tt.wantRetryable)
			}
		})
	}
}

func TestHTTPHelpersCoverRequestAndErrorBranches(t *testing.T) {
	cfg := settings{token: "github-token"}
	req, err := buildRequest(t.Context(), cfg, http.MethodPost, "https://api.github.com/repos/acme/app/issues", []byte(`{"title":"bug"}`))
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if req.Header.Get("Content-Type") == "" || req.Header.Get("User-Agent") != userAgent {
		t.Fatalf("headers = %#v; want JSON content type and user agent", req.Header)
	}
	if _, err := buildRequest(t.Context(), cfg, http.MethodGet, "://bad-url", nil); err == nil {
		t.Fatal("buildRequest accepted invalid URL")
	}

	provider := NewProvider(WithHTTPClient(ptrext.Of(http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return ptrext.Of(http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       errReadCloser{},
		}), nil
	})})))
	_, _, err = provider.request(t.Context(), cfg, http.MethodGet, "https://api.github.com/repos/acme/app", nil)
	if err == nil || !strings.Contains(err.Error(), "read github response") {
		t.Fatalf("request error = %v; want read error", err)
	}
	if got := (providerError{message: "boom"}).Error(); got != "boom" {
		t.Fatalf("providerError.Error = %q; want boom", got)
	}
}

func TestHTTPErrorMessageAndRetryHelpers(t *testing.T) {
	if retryAfterTime("") != nil {
		t.Fatal("blank Retry-After produced a timestamp")
	}
	if got := retryAfterTime("2"); got == nil || time.Until(ptrext.Indirect(got)) <= 0 {
		t.Fatalf("numeric Retry-After = %v; want future timestamp", got)
	}
	msg := githubErrorMessage(499, "https://api.github.com/repos/acme/app/issues?token=secret", nil)
	if !strings.Contains(msg, "provider error") || strings.Contains(msg, "token=secret") {
		t.Fatalf("githubErrorMessage = %q; want fallback status text and redacted query", msg)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate = %q; want abc", got)
	}
	generic := classifyError(errors.New("plain failure"))
	if generic.Kind != "provider_error" || !generic.Retryable {
		t.Fatalf("generic classification = %+v; want retryable provider_error", generic)
	}
	deadline := classifyError(context.DeadlineExceeded)
	if deadline.Kind != "provider_unavailable" || !deadline.Retryable {
		t.Fatalf("deadline classification = %+v; want retryable provider_unavailable", deadline)
	}
	if got := classifyError(nil); got != (core.SyncError{}) {
		t.Fatalf("nil classification = %+v; want zero value", got)
	}
}

func TestProviderErrorClassificationBranches(t *testing.T) {
	err := githubHTTPError(http.StatusTooManyRequests,
		"https://api.github.com/repos/acme/app/issues?token=secret",
		[]byte(`{"message":"slow down"}`), "gh-req-1", "1")
	classified := classifyError(err)
	if classified.Kind != "rate_limited" || !classified.Retryable ||
		classified.ProviderRequestID != "gh-req-1" || classified.RetryAfter == nil {
		t.Fatalf("classified provider error = %+v; want retryable rate limit", classified)
	}
	if strings.Contains(classified.Message, "token=secret") {
		t.Fatalf("classified message leaked query secret: %q", classified.Message)
	}
	if retryAfterTime("not a time") != nil {
		t.Fatal("invalid Retry-After parsed as time")
	}
	if got := extractGitHubMessage([]byte("plain text")); got != "plain text" {
		t.Fatalf("extractGitHubMessage = %q; want plain text fallback", got)
	}

	blocked := classifyError(ptrext.Of(nethardening.BlockedError{Host: "127.0.0.1", Reason: "loopback"}))
	if blocked.Kind != "validation" || blocked.Retryable {
		t.Fatalf("blocked classification = %+v; want non-retryable validation", blocked)
	}
	cancelled := classifyError(context.Canceled)
	if cancelled.Kind != "provider_unavailable" || !cancelled.Retryable {
		t.Fatalf("cancel classification = %+v; want retryable provider unavailable", cancelled)
	}
	urlErr := classifyError(ptrext.Of(url.Error{Op: "Get", URL: "https://api.github.com/repos/acme/app?token=secret", Err: errors.New("connection reset")}))
	if urlErr.Kind != "provider_unavailable" || !urlErr.Retryable ||
		strings.Contains(urlErr.Message, "token=secret") {
		t.Fatalf("url classification = %+v; want redacted retryable provider error", urlErr)
	}
}

func TestCheckFailureBranches(t *testing.T) {
	provider := NewProvider()
	result, err := provider.Check(t.Context(), core.Connection{})
	if err != nil || result.OK || !strings.Contains(result.Error, "requires repo_url") {
		t.Fatalf("invalid settings Check = %+v err=%v; want failed result without top-level error", result, err)
	}

	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "gh-auth-1")
		http.Error(w, `{"message":"bad credentials"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	provider = NewProvider(WithHTTPClient(server.Client()))
	result, err = provider.Check(t.Context(), testConnection(server.URL))
	if err != nil || result.OK || result.RequestID != "gh-auth-1" ||
		!strings.Contains(result.Error, "bad credentials") {
		t.Fatalf("HTTP failed Check = %+v err=%v; want provider error result", result, err)
	}

	provider = NewProvider(WithHTTPClient(ptrext.Of(http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})})))
	result, err = provider.Check(t.Context(), testConnection(server.URL))
	if err == nil || result.OK || !strings.Contains(result.Error, "dial failed") {
		t.Fatalf("transport failed Check = %+v err=%v; want top-level transport error", result, err)
	}
}

func TestCheckAndPullIssues(t *testing.T) {
	setLoopbackEgress(t)
	requestID := "req-check-1"
	customerRequestID := uuid.NewString()
	server := newCheckAndPullIssuesServer(t, requestID, customerRequestID)
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	conn := testConnection(server.URL)
	check, err := provider.Check(t.Context(), conn)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !check.OK || check.RequestID != requestID {
		t.Fatalf("Check = %+v, want OK with request id", check)
	}

	result, err := provider.Pull(t.Context(), core.PullRequest{
		Connection: conn,
		Cursor:     []byte(`{"updated_since":"2026-07-07T00:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	assertPulledIssueRecord(t, result, customerRequestID)
	assertPulledIssueComment(t, result)
	assertPulledDeliveryArtifacts(t, result)
	assertPulledIssueCursor(t, result.NextCursor)
}

func assertPulledIssueRecord(t *testing.T, result core.PullResult, customerRequestID string) {
	t.Helper()
	if len(result.Records) != 1 {
		t.Fatalf("records len = %d, want 1", len(result.Records))
	}
	record := result.Records[0]
	if record.Key != "7" || record.LocalObjectID != customerRequestID {
		t.Fatalf("record key/local = %q/%q, want 7/%s", record.Key, record.LocalObjectID, customerRequestID)
	}
	payload := normalizedIssue{}
	if err := json.Unmarshal(record.Payload, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode normalized payload: %v", err)
	}
	if payload.Title != "Sync me" || payload.Assignee != "alice" || len(payload.Labels) != 2 {
		t.Fatalf("payload = %+v, want normalized issue fields", payload)
	}
}

func assertPulledIssueComment(t *testing.T, result core.PullResult) {
	t.Helper()
	child, ok := findPulledChild(result, "comment", "9001")
	if !ok {
		t.Fatalf("children = %+v, want issue comment child", result.Children)
	}
	if child.ParentKey != "7" || child.Type != "comment" || child.Key != "9001" {
		t.Fatalf("child = %+v, want issue comment child", child)
	}
	commentPayload := normalizedComment{}
	if err := json.Unmarshal(child.Payload, &commentPayload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode comment payload: %v", err)
	}
	if commentPayload.AuthorLogin != "octo" || commentPayload.Marker != "cr-note-1" || commentPayload.BodyDigest == "" {
		t.Fatalf("comment payload = %+v, want normalized comment fields", commentPayload)
	}
}

func assertPulledDeliveryArtifacts(t *testing.T, result core.PullResult) {
	t.Helper()
	pr, ok := findPulledChild(result, "pull_request", "acme/app#313")
	if !ok {
		t.Fatalf("children = %+v, want pull request child", result.Children)
	}
	if pr.ParentKey != "7" || pr.URL != "https://github.com/acme/app/pull/313" ||
		pr.UpdatedAt.UTC().Format(time.RFC3339) != "2026-07-08T10:08:00Z" {
		t.Fatalf("pull request child = %+v, want parent/url/timestamp", pr)
	}
	prPayload := normalizedDeliveryArtifact{}
	if err := json.Unmarshal(pr.Payload, &prPayload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode pull request payload: %v", err)
	}
	if prPayload.Number != 313 || prPayload.Title != "Implement delivery graph" ||
		prPayload.Relationship != "implements" || prPayload.Assignee != "octo" {
		t.Fatalf("pull request payload = %+v, want normalized PR fields", prPayload)
	}

	commit, ok := findPulledChild(result, "commit", "acme/app@0123456789abcdef")
	if !ok {
		t.Fatalf("children = %+v, want commit child", result.Children)
	}
	commitPayload := normalizedDeliveryArtifact{}
	if err := json.Unmarshal(commit.Payload, &commitPayload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode commit payload: %v", err)
	}
	if commitPayload.CommitID != "0123456789abcdef" ||
		commitPayload.Relationship != "implements" ||
		commitPayload.HTMLURL != "https://github.com/acme/app/commit/0123456789abcdef" {
		t.Fatalf("commit payload = %+v, want normalized commit fields", commitPayload)
	}
}

func findPulledChild(result core.PullResult, childType, key string) (core.ExternalChildRecord, bool) {
	for _, child := range result.Children {
		if child.Type == childType && child.Key == key {
			return child, true
		}
	}
	return core.ExternalChildRecord{}, false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertPulledIssueCursor(t *testing.T, nextCursor []byte) {
	t.Helper()
	cursor := cursorState{}
	if err := json.Unmarshal(nextCursor, &cursor); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode cursor: %v", err)
	}
	if cursor.UpdatedSince != "2026-07-08T11:00:00Z" || cursor.NextURL != "" {
		t.Fatalf("cursor = %+v, want high watermark from last page", cursor)
	}
}

func newCheckAndPullIssuesServer(t *testing.T, requestID, customerRequestID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", requestID)
		assertGitHubHeaders(t, r)
		_, _ = w.Write([]byte(`{"full_name":"acme/app"}`))
	})
	mux.HandleFunc("/repos/acme/app/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", requestID)
		assertGitHubHeaders(t, r)
		if got := r.URL.Query().Get("since"); got != "2026-07-07T00:00:00Z" {
			t.Fatalf("since query = %q, want cursor value", got)
		}
		writeJSON(t, w, pullIssuesPayload(customerRequestID))
	})
	mux.HandleFunc("/repos/acme/app/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", requestID)
		assertGitHubHeaders(t, r)
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("comments per_page = %q, want 100", got)
		}
		writeJSON(t, w, pullIssueCommentsPayload())
	})
	mux.HandleFunc("/repos/acme/app/issues/7/timeline", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", requestID)
		assertGitHubHeaders(t, r)
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("timeline per_page = %q, want 100", got)
		}
		writeJSON(t, w, pullIssueTimelinePayload())
	})
	return httptest.NewServer(mux)
}

func pullIssuesPayload(customerRequestID string) []map[string]any {
	return []map[string]any{
		{
			"number":       7,
			"html_url":     "https://github.com/acme/app/issues/7",
			"title":        "Sync me",
			"state":        "open",
			"state_reason": nil,
			"locked":       false,
			"assignee":     map[string]any{"login": "alice"},
			"assignees":    []map[string]any{{"login": "alice"}, {"login": "bob"}},
			"labels":       []map[string]any{{"name": "bug"}, {"name": "urgent"}},
			"updated_at":   "2026-07-08T10:00:00Z",
			"closed_at":    nil,
			"comments":     1,
			"body":         "<!-- attune:customer_request_id=" + customerRequestID + " -->",
		},
		{
			"number":       8,
			"html_url":     "https://github.com/acme/app/pull/8",
			"title":        "Skip pull request",
			"state":        "open",
			"updated_at":   "2026-07-08T11:00:00Z",
			"pull_request": map[string]any{"url": "https://api.github.com/repos/acme/app/pulls/8"},
		},
	}
}

func pullIssueCommentsPayload() []map[string]any {
	return []map[string]any{{
		"id":         9001,
		"html_url":   "https://github.com/acme/app/issues/7#issuecomment-9001",
		"body":       "Please sync this.\n\n<!-- attune:comment_id=cr-note-1 -->",
		"user":       map[string]any{"id": 123, "login": "octo", "html_url": "https://github.com/octo"},
		"created_at": "2026-07-08T10:05:00Z",
		"updated_at": "2026-07-08T10:06:00Z",
	}}
}

func pullIssueTimelinePayload() []map[string]any {
	return []map[string]any{
		{
			"event":      "cross-referenced",
			"created_at": "2026-07-08T10:08:00Z",
			"actor":      map[string]any{"login": "hubot"},
			"source": map[string]any{
				"type": "pull_request",
				"issue": map[string]any{
					"number":       313,
					"html_url":     "https://github.com/acme/app/pull/313",
					"title":        "Implement delivery graph",
					"state":        "open",
					"state_reason": "review_required",
					"assignee":     map[string]any{"login": "octo"},
					"assignees":    []map[string]any{{"login": "octo"}},
					"updated_at":   "2026-07-08T10:07:00Z",
					"pull_request": map[string]any{"url": "https://api.github.com/repos/acme/app/pulls/313"},
				},
			},
		},
		{
			"event":      "closed",
			"commit_id":  "0123456789abcdef",
			"commit_url": "https://api.github.com/repos/acme/app/commits/0123456789abcdef",
			"created_at": "2026-07-08T10:09:00Z",
			"actor":      map[string]any{"login": "octo"},
		},
	}
}

func TestPullFailureBranches(t *testing.T) {
	provider := NewProvider()
	if _, err := provider.Pull(t.Context(), core.PullRequest{Connection: core.Connection{}}); err == nil {
		t.Fatal("Pull accepted invalid connection settings")
	}
	if _, err := provider.Pull(t.Context(), core.PullRequest{
		Connection: testConnection(defaultAPIBase),
		Cursor:     []byte(`{"updated_since":"bad"}`),
	}); err == nil {
		t.Fatal("Pull accepted invalid cursor")
	}

	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"not":"array"}`))
	}))
	t.Cleanup(server.Close)
	provider = NewProvider(WithHTTPClient(server.Client()))
	if _, err := provider.Pull(t.Context(), core.PullRequest{Connection: testConnection(server.URL)}); err == nil {
		t.Fatal("Pull accepted malformed issues response")
	}
	if _, err := provider.Pull(t.Context(), core.PullRequest{
		Connection: testConnection(server.URL),
		Cursor:     []byte(`{"next_url":"https://evil.example/repos/acme/app/issues?page=2"}`),
	}); err == nil {
		t.Fatal("Pull accepted cross-host next cursor")
	}
}

func TestPullUsesNextLinkCursor(t *testing.T) {
	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			t.Fatalf("page query = %q, want 1", r.URL.Query().Get("page"))
		}
		w.Header().Set("Link", `<`+serverURL(r)+`/repos/acme/app/issues?page=2>; rel="next"`)
		writeJSON(t, w, []map[string]any{})
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Pull(t.Context(), core.PullRequest{
		Connection: testConnection(server.URL),
		Cursor:     []byte(`{"next_url":"` + server.URL + `/repos/acme/app/issues?page=1","updated_since":"2026-07-07T00:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	cursor := cursorState{}
	if err := json.Unmarshal(result.NextCursor, &cursor); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode cursor: %v", err)
	}
	if !strings.HasSuffix(cursor.NextURL, "/repos/acme/app/issues?page=2") {
		t.Fatalf("next cursor = %+v, want rel=next url", cursor)
	}
	if cursor.UpdatedSince != "2026-07-07T00:00:00Z" {
		t.Fatalf("updated_since = %q, want preserved watermark", cursor.UpdatedSince)
	}
}

func TestPullUsesIssueHintWithoutAdvancingCursor(t *testing.T) {
	setLoopbackEgress(t)
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42":
			writeJSON(t, w, map[string]any{
				"number":     42,
				"html_url":   "https://github.com/acme/app/issues/42",
				"title":      "Webhook issue",
				"state":      "open",
				"updated_at": "2026-07-08T10:00:00Z",
				"comments":   1,
				"body":       "",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42/comments":
			writeJSON(t, w, []map[string]any{{
				"id":         777,
				"html_url":   "https://github.com/acme/app/issues/42#issuecomment-777",
				"body":       "A webhook comment",
				"user":       map[string]any{"login": "octo"},
				"created_at": "2026-07-08T10:01:00Z",
				"updated_at": "2026-07-08T10:02:00Z",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Pull(t.Context(), core.PullRequest{
		Connection:    testConnection(server.URL),
		Cursor:        []byte(`{"updated_since":"2026-07-07T00:00:00Z"}`),
		InputMetadata: []byte(`{"event_type":"issue_comment","external_key":"acme/app#42","comment_id":"777"}`),
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].Key != "42" || len(result.Children) != 1 {
		t.Fatalf("result = %+v, want one issue and one comment", result)
	}
	cursor := cursorState{}
	if err := json.Unmarshal(result.NextCursor, &cursor); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode cursor: %v", err)
	}
	if cursor.UpdatedSince != "2026-07-07T00:00:00Z" {
		t.Fatalf("cursor = %+v, want original cursor", cursor)
	}
	if len(requests) != 2 || strings.Contains(requests[0], "/issues?") ||
		!strings.Contains(requests[1], "/issues/42/comments") {
		t.Fatalf("requests = %#v, want targeted issue plus comment pull", requests)
	}
}

func TestPullDeletedIssueCommentHintEmitsTombstone(t *testing.T) {
	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42":
			writeJSON(t, w, map[string]any{
				"number":     42,
				"html_url":   "https://github.com/acme/app/issues/42",
				"title":      "Webhook issue",
				"state":      "open",
				"updated_at": "2026-07-08T10:00:00Z",
				"comments":   0,
				"body":       "",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42/comments":
			writeJSON(t, w, []map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Pull(t.Context(), core.PullRequest{
		Connection:    testConnection(server.URL),
		Cursor:        []byte(`{"updated_since":"2026-07-07T00:00:00Z"}`),
		InputMetadata: []byte(`{"event_type":"issue_comment","action":"deleted","issue_number":42,"comment_id":777}`),
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(result.Children) != 1 || !result.Children[0].Deleted || result.Children[0].Key != "777" {
		t.Fatalf("children = %+v, want deleted comment tombstone", result.Children)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(result.Children[0].Payload, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode deleted comment payload: %v", err)
	}
	if payload["deleted"] != true {
		t.Fatalf("deleted payload = %+v, want deleted=true", payload)
	}
}

func TestIssueURLHelpers(t *testing.T) {
	cfg := settings{owner: "acme", repo: "app", apiBase: "https://api.github.com"}
	if _, err := issueURL(cfg, 0); err == nil {
		t.Fatal("issueURL accepted non-positive issue number")
	}
	if got, err := issueURL(cfg, 42); err != nil || !strings.HasSuffix(got, "/repos/acme/app/issues/42") {
		t.Fatalf("issueURL = %q err=%v; want repository issue URL", got, err)
	}
	if _, err := issueCommentsURL(settings{apiBase: "%"}, 42); err == nil {
		t.Fatal("issueCommentsURL accepted malformed API base")
	}
	if _, err := repoAPIURL(settings{apiBase: "%"}); err == nil {
		t.Fatal("repoAPIURL accepted malformed API base")
	}
}

func TestValidateNextURLHelpers(t *testing.T) {
	cfg := settings{owner: "acme", repo: "app", apiBase: "https://api.github.com"}
	if _, err := validateNextURL(cfg, "https://api.github.com/repos/acme/other/issues?page=2"); err == nil {
		t.Fatal("validateNextURL returned nil error, want repository path validation error")
	}
	if _, err := validateNextURL(cfg, "https://uploads.github.com/repos/acme/app/issues?page=2"); err == nil {
		t.Fatal("validateNextURL accepted different API host")
	}
	if _, err := validateNextURL(cfg, "%"); err == nil {
		t.Fatal("validateNextURL accepted malformed URL")
	}
	if got, err := validateNextURL(cfg, "https://api.github.com/repos/acme/app/issues/42"); err != nil || got == "" {
		t.Fatalf("validateNextURL valid subpath = %q err=%v; want accepted", got, err)
	}
	if !pathHasBase("/anything", "") || pathHasBase("/repos/acme/app/issues-old", "/repos/acme/app/issues") {
		t.Fatal("pathHasBase did not honor empty base and segment boundaries")
	}
}

func TestIssuesURLAndCursorHelpers(t *testing.T) {
	cfg := settings{owner: "acme", repo: "app", apiBase: "https://api.github.com"}
	url, err := issuesURL(cfg, cursorState{UpdatedSince: "2026-07-08T01:02:03Z"})
	if err != nil {
		t.Fatalf("issuesURL returned error: %v", err)
	}
	if !strings.Contains(url, "since=2026-07-08T01%3A02%3A03Z") || !strings.Contains(url, "per_page=100") {
		t.Fatalf("issuesURL = %q; want since and pagination parameters", url)
	}
	if got, err := issuesURL(cfg, cursorState{NextURL: "https://api.github.com/repos/acme/app/issues?page=2"}); err != nil ||
		got != "https://api.github.com/repos/acme/app/issues?page=2" {
		t.Fatalf("issuesURL next cursor = %q err=%v; want validated cursor URL", got, err)
	}
	next, err := nextCursor(cfg, cursorState{UpdatedSince: "2026-07-07T00:00:00Z"}, time.Time{}, "")
	if err != nil || string(next) != `{"updated_since":"2026-07-07T00:00:00Z"}` {
		t.Fatalf("nextCursor no updates = %s err=%v", string(next), err)
	}
	if _, err := nextCursor(cfg, cursorState{}, time.Time{}, `<https://evil.example/repos/acme/app/issues?page=2>; rel="next"`); err == nil {
		t.Fatal("nextCursor accepted cross-host Link cursor")
	}
}

func TestPushCreatesAndUpdatesIssues(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()
	commentMarker := managedCommentID(customerRequestID)
	state := ptrext.Of(pushIssueServerState{customerRequestID: customerRequestID, commentMarker: commentMarker})
	server := newPushIssueServer(t, state)
	t.Cleanup(server.Close)

	createPayload := marshalPayload(t, localIssuePayload{
		Title:             "Create issue",
		Body:              "Issue body",
		Labels:            []string{"attune/request"},
		CustomerRequestID: customerRequestID,
	})
	updatePayload := marshalPayload(t, localIssuePayload{
		ExternalKey:       "acme/app#42",
		State:             "closed",
		Title:             "Should not overwrite",
		Body:              "Updated request context",
		Labels:            []string{"attune/status/shipped"},
		CustomerRequestID: customerRequestID,
	})
	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Push(t.Context(), core.PushRequest{
		Connection: testConnection(server.URL),
		Records: []core.LocalRecord{
			{ID: customerRequestID, Payload: createPayload},
			{ID: customerRequestID, Payload: updatePayload},
		},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !state.seenCreate || !state.seenUpdate {
		t.Fatalf("seen create/update = %t/%t, want both", state.seenCreate, state.seenUpdate)
	}
	if !state.seenCreateComment || !state.seenUpdateComment || state.commentLists != 2 {
		t.Fatalf("comment sync seen create/update/lists = %t/%t/%d; want create, update, two lists",
			state.seenCreateComment, state.seenUpdateComment, state.commentLists)
	}
	if len(result.Results) != 2 || result.Results[0].Key != "42" || result.Results[1].Version != "2026-07-08T13:00:00Z" {
		t.Fatalf("push results = %+v, want create and update result metadata", result.Results)
	}
}

type pushIssueServerState struct {
	customerRequestID string
	commentMarker     string
	seenCreate        bool
	seenUpdate        bool
	seenCreateComment bool
	seenUpdateComment bool
	commentLists      int
}

func newPushIssueServer(t *testing.T, state *pushIssueServerState) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/issues", func(w http.ResponseWriter, r *http.Request) {
		assertGitHubHeaders(t, r)
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		state.handleCreateIssue(t, w, r)
	})
	mux.HandleFunc("/repos/acme/app/issues/42", func(w http.ResponseWriter, r *http.Request) {
		assertGitHubHeaders(t, r)
		if r.Method == http.MethodGet {
			state.handleGetIssue(t, w)
			return
		}
		if r.Method == http.MethodPatch {
			state.handleUpdateIssue(t, w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/repos/acme/app/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		assertGitHubHeaders(t, r)
		if r.Method == http.MethodGet {
			state.handleListComments(t, w, r)
			return
		}
		if r.Method == http.MethodPost {
			state.handleCreateComment(t, w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/repos/acme/app/issues/comments/9001", func(w http.ResponseWriter, r *http.Request) {
		assertGitHubHeaders(t, r)
		if r.Method != http.MethodPatch {
			http.NotFound(w, r)
			return
		}
		state.handleUpdateComment(t, w, r)
	})
	return httptest.NewServer(mux)
}

func (s *pushIssueServerState) handleCreateIssue(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	s.seenCreate = true
	req := ptrext.Of(issueWriteRequest{})
	decodeRequest(t, r, req)
	if req.Title != "Create issue" || !strings.Contains(req.Body, s.customerRequestID) {
		t.Fatalf("create request = %+v, want title and customer request marker", req)
	}
	writeJSON(t, w, map[string]any{
		"number":     42,
		"html_url":   "https://github.com/acme/app/issues/42",
		"title":      req.Title,
		"state":      "open",
		"updated_at": "2026-07-08T12:00:00Z",
	})
}

func (s *pushIssueServerState) handleGetIssue(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeJSON(t, w, map[string]any{
		"number":     42,
		"html_url":   "https://github.com/acme/app/issues/42",
		"title":      "Engineer title",
		"state":      "open",
		"updated_at": "2026-07-08T12:30:00Z",
		"labels":     []map[string]string{{"name": "engineer"}, {"name": "attune/status/open"}},
	})
}

func (s *pushIssueServerState) handleUpdateIssue(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	s.seenUpdate = true
	req := ptrext.Of(issueWriteRequest{})
	decodeRequest(t, r, req)
	if req.State != "closed" {
		t.Fatalf("update state = %q, want closed", req.State)
	}
	if req.Title != "" || req.Body != "" || len(req.Labels) != 0 {
		t.Fatalf("safe update request = %+v; want no title/body/labels overwrite", req)
	}
	writeJSON(t, w, map[string]any{
		"number":     42,
		"html_url":   "https://github.com/acme/app/issues/42",
		"title":      "Create issue",
		"state":      "closed",
		"updated_at": "2026-07-08T13:00:00Z",
	})
}

func (s *pushIssueServerState) handleListComments(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	s.commentLists++
	if got := r.URL.Query().Get("per_page"); got != "100" {
		t.Fatalf("comment list per_page = %q, want 100", got)
	}
	if s.commentLists == 1 {
		writeJSON(t, w, []map[string]any{})
		return
	}
	writeJSON(t, w, []map[string]any{{
		"id":         9001,
		"html_url":   "https://github.com/acme/app/issues/42#issuecomment-9001",
		"body":       "Old Attune context\n\n<!-- attune:comment_id=" + s.commentMarker + " -->",
		"created_at": "2026-07-08T12:01:00Z",
		"updated_at": "2026-07-08T12:01:00Z",
	}})
}

func (s *pushIssueServerState) handleCreateComment(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	s.seenCreateComment = true
	req := ptrext.Of(commentWriteRequest{})
	decodeRequest(t, r, req)
	if !strings.Contains(req.Body, s.commentMarker) || !strings.Contains(req.Body, s.customerRequestID) {
		t.Fatalf("create comment body = %q; want Attune markers", req.Body)
	}
	writeJSON(t, w, map[string]any{
		"id":         9001,
		"html_url":   "https://github.com/acme/app/issues/42#issuecomment-9001",
		"body":       req.Body,
		"updated_at": "2026-07-08T12:02:00Z",
	})
}

func (s *pushIssueServerState) handleUpdateComment(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	s.seenUpdateComment = true
	req := ptrext.Of(commentWriteRequest{})
	decodeRequest(t, r, req)
	if !strings.Contains(req.Body, "Updated request context") || !strings.Contains(req.Body, s.commentMarker) {
		t.Fatalf("update comment body = %q; want refreshed context and marker", req.Body)
	}
	writeJSON(t, w, map[string]any{
		"id":         9001,
		"html_url":   "https://github.com/acme/app/issues/42#issuecomment-9001",
		"body":       req.Body,
		"updated_at": "2026-07-08T13:01:00Z",
	})
}

func TestPushWriteManagedFieldsMergesLabelsAndAllowsReopen(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()
	seenUpdate := false
	syncComments := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertGitHubHeaders(t, r)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42":
			writeJSON(t, w, map[string]any{
				"number":     42,
				"html_url":   "https://github.com/acme/app/issues/42",
				"title":      "Existing title",
				"state":      "closed",
				"updated_at": "2026-07-08T12:30:00Z",
				"labels":     []map[string]string{{"name": "engineer"}, {"name": "attune/status/old"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/app/issues/42":
			seenUpdate = true
			req := ptrext.Of(issueWriteRequest{})
			decodeRequest(t, r, req)
			if req.Title != "Managed title" || req.State != "open" ||
				!strings.Contains(req.Body, customerRequestID) {
				t.Fatalf("managed update request = %+v; want title, reopen, and marked body", req)
			}
			wantLabels := []string{"attune/request", "attune/status/open", "engineer"}
			if strings.Join(req.Labels, ",") != strings.Join(wantLabels, ",") {
				t.Fatalf("managed labels = %v; want %v", req.Labels, wantLabels)
			}
			writeJSON(t, w, map[string]any{
				"number":     42,
				"html_url":   "https://github.com/acme/app/issues/42",
				"title":      req.Title,
				"state":      "open",
				"updated_at": "2026-07-08T13:00:00Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Push(t.Context(), core.PushRequest{
		Connection: testConnectionWithConfig(providerConfig{
			Owner:                     "acme",
			Repo:                      "app",
			APIBaseURL:                server.URL,
			DefaultLabels:             []string{"attune/request"},
			SyncComments:              ptrext.Of(syncComments),
			AllowReopen:               true,
			LinkedExistingWritePolicy: linkedExistingWriteManagedFields,
		}),
		Records: []core.LocalRecord{{
			ID: customerRequestID,
			Payload: marshalPayload(t, localIssuePayload{
				ExternalKey:       "42",
				Title:             "Managed title",
				Body:              "Managed body",
				Status:            "open",
				Labels:            []string{"attune/status/open"},
				CustomerRequestID: customerRequestID,
			}),
		}},
	})
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	if !seenUpdate || len(result.Results) != 1 || result.Results[0].Version != "2026-07-08T13:00:00Z" {
		t.Fatalf("push result = %+v seenUpdate=%t; want managed update", result, seenUpdate)
	}
}

func TestPushReturnsIssueMetadataWhenManagedCommentFails(t *testing.T) {
	setLoopbackEgress(t)
	customerRequestID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertGitHubHeaders(t, r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/issues":
			writeJSON(t, w, map[string]any{
				"number":     42,
				"html_url":   "https://github.com/acme/app/issues/42",
				"title":      "Create issue",
				"state":      "open",
				"updated_at": "2026-07-08T12:00:00Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42/comments":
			http.Error(w, `{"message":"comment outage"}`, http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Push(t.Context(), core.PushRequest{
		Connection: testConnection(server.URL),
		Records: []core.LocalRecord{{
			ID: customerRequestID,
			Payload: marshalPayload(t, localIssuePayload{
				Title:             "Create issue",
				Body:              "Issue body",
				CustomerRequestID: customerRequestID,
			}),
		}},
	})
	if err != nil {
		t.Fatalf("Push returned top-level error: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Key != "42" ||
		result.Results[0].URL != "https://github.com/acme/app/issues/42" ||
		result.Results[0].Error == nil || !result.Results[0].Retryable {
		t.Fatalf("push result = %+v; want issue metadata plus retryable comment failure", result)
	}
}

func TestPushReturnsFailureRowsForInvalidRecords(t *testing.T) {
	provider := NewProvider()
	result, err := provider.Push(t.Context(), core.PushRequest{
		Connection: testConnection(defaultAPIBase),
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

func TestPushFailureBranches(t *testing.T) {
	provider := NewProvider()
	empty, err := provider.Push(t.Context(), core.PushRequest{})
	if err != nil || len(empty.Results) != 0 {
		t.Fatalf("empty Push = %+v err=%v; want no-op", empty, err)
	}
	if _, err := provider.Push(t.Context(), core.PushRequest{
		Connection: core.Connection{},
		Records:    []core.LocalRecord{{ID: "cr-1", Payload: []byte(`{"title":"Bug"}`)}},
	}); err == nil {
		t.Fatal("Push accepted invalid connection settings")
	}

	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	t.Cleanup(server.Close)
	provider = NewProvider(WithHTTPClient(server.Client()))
	result, err := provider.Push(t.Context(), core.PushRequest{
		Connection: testConnection(server.URL),
		Records: []core.LocalRecord{{
			ID:      uuid.NewString(),
			Payload: marshalPayload(t, localIssuePayload{Title: "Create issue"}),
		}},
	})
	if err != nil {
		t.Fatalf("Push returned top-level error: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Error == nil ||
		result.Results[0].Error.Kind != "provider_error" {
		t.Fatalf("Push result = %+v; want provider_error row for bad response", result)
	}
}

func TestClassifyHTTPErrorRedactsURL(t *testing.T) {
	setLoopbackEgress(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "Wed, 08 Jul 2026 03:04:05 GMT")
		w.Header().Set("X-GitHub-Request-Id", "gh-rate-1")
		http.Error(w, `{"message":"secondary rate limit"}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	_, err := provider.Pull(t.Context(), core.PullRequest{
		Connection: testConnection(server.URL),
		Cursor:     []byte(`{"next_url":"` + server.URL + `/repos/acme/app/issues?token=secret"}`),
	})
	if err == nil {
		t.Fatal("Pull returned nil error, want rate limit")
	}
	classified := provider.ClassifyError(err)
	if classified.Kind != "rate_limited" || classified.HTTPStatus != http.StatusTooManyRequests || !classified.Retryable {
		t.Fatalf("classified = %+v, want retryable rate limit", classified)
	}
	if classified.ProviderRequestID != "gh-rate-1" || classified.RetryAfter == nil ||
		classified.RetryAfter.UTC().Format(time.RFC3339) != "2026-07-08T03:04:05Z" {
		t.Fatalf("classified diagnostics = %+v, want request id and retry-after", classified)
	}
	if strings.Contains(classified.Message, "token=secret") || strings.Contains(classified.Message, "/repos/acme") {
		t.Fatalf("classified message leaked URL detail: %q", classified.Message)
	}
}

func testConnection(apiBase string) core.Connection {
	return testConnectionWithConfig(providerConfig{Owner: "acme", Repo: "app", APIBaseURL: apiBase})
}

func testConnectionWithConfig(cfg providerConfig) core.Connection {
	raw, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return core.Connection{
		Provider:       providerID,
		ProviderConfig: raw,
		Credential:     []byte("github-token"),
	}
}

func setLoopbackEgress(t *testing.T) {
	t.Helper()
	core.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	t.Cleanup(func() { core.SetEgressPolicy(nethardening.Policy{}) })
}

func assertGitHubHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer github-token" {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
	if got := r.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
		t.Fatalf("X-GitHub-Api-Version = %q, want %q", got, githubAPIVersion)
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

func TestMarkerRoundTrip(t *testing.T) {
	id := uuid.NewString()
	body := withCustomerRequestMarker("hello", "", id)
	if got := extractCustomerRequestID(body); got != id {
		t.Fatalf("extractCustomerRequestID = %q, want %q", got, id)
	}
	if strings.Count(withCustomerRequestMarker(body, "", id), customerRequestMarker) != 1 {
		t.Fatal("marker was appended more than once")
	}
}

func TestIssuePayloadTimeBranches(t *testing.T) {
	now := time.Date(2026, 7, 8, 1, 2, 3, 4, time.FixedZone("SGT", 8*60*60))
	if issueVersion(apiIssue{}) != "" {
		t.Fatal("zero issue version should be empty")
	}
	if got := commentVersion(apiComment{}); got != "" {
		t.Fatalf("commentVersion zero = %q; want empty", got)
	}
	if got := optionalNonZeroTimeString(time.Time{}); got != "" {
		t.Fatalf("optionalNonZeroTimeString zero = %q; want empty", got)
	}
	if got := optionalTimeString(nil); got != "" {
		t.Fatalf("optionalTimeString nil = %q; want empty", got)
	}
	if got := optionalTimeString(ptrext.Of(now)); got != "2026-07-07T17:02:03.000000004Z" {
		t.Fatalf("optionalTimeString = %q; want UTC timestamp", got)
	}
}

func TestDecodeLocalPayloadBranches(t *testing.T) {
	payload, err := decodeLocalPayload(core.LocalRecord{Payload: []byte(`{"external_key":" 42 ","body":"","state":" ","status":"shipped","priority":" high ","display_id":" CR-1 ","labels":[" bug ",""],"customer_request_id":" id "}`)})
	if err != nil {
		t.Fatalf("decodeLocalPayload returned error: %v", err)
	}
	if !payload.BodySet || payload.ExternalKey != "42" || payload.Status != "shipped" ||
		payload.Priority != "high" || payload.DisplayID != "CR-1" ||
		!strings.EqualFold(strings.Join(payload.Labels, ","), "bug") {
		t.Fatalf("payload = %+v; want trimmed fields and body presence", payload)
	}
	if _, err := decodeLocalPayload(core.LocalRecord{}); err == nil {
		t.Fatal("decodeLocalPayload accepted empty payload")
	}
	if _, err := decodeLocalPayload(core.LocalRecord{Payload: []byte(`{`)}); err == nil {
		t.Fatal("decodeLocalPayload accepted invalid JSON")
	}
	noBody, err := decodeLocalPayload(core.LocalRecord{Payload: []byte(`{"title":"No body"}`)})
	if err != nil {
		t.Fatalf("decodeLocalPayload without body returned error: %v", err)
	}
	if noBody.BodySet {
		t.Fatal("decodeLocalPayload marked absent body as present")
	}
}

func TestDecodeCommentPayloadBranches(t *testing.T) {
	now := time.Date(2026, 7, 8, 1, 2, 3, 4, time.FixedZone("SGT", 8*60*60))
	if _, err := decodeCommentsResponse([]byte(`not-json`)); err == nil {
		t.Fatal("decodeCommentsResponse accepted invalid JSON")
	}
	if _, err := decodeCommentResponse([]byte(`not-json`)); err == nil {
		t.Fatal("decodeCommentResponse accepted invalid JSON")
	}
	comment := normalizedCommentPayload(apiComment{
		ID:        77,
		Body:      "hello",
		HTMLURL:   "https://github.com/acme/app/issues/42#issuecomment-77",
		CreatedAt: now,
		UpdatedAt: now,
		User:      nil,
	})
	if comment.AuthorLogin != "" || comment.CreatedAt == "" || comment.UpdatedAt == "" {
		t.Fatalf("normalizedCommentPayload = %+v; want anonymous timestamped comment", comment)
	}
}

func TestIssuePayloadBuildUpdateRequestBranches(t *testing.T) {
	payload, err := decodeLocalPayload(core.LocalRecord{Payload: []byte(`{"external_key":" 42 ","body":"","state":" ","status":"shipped","priority":" high ","display_id":" CR-1 ","labels":[" bug ",""],"customer_request_id":" id "}`)})
	if err != nil {
		t.Fatalf("decodeLocalPayload returned error: %v", err)
	}
	req, err := buildUpdateRequest(settings{linkedExistingWritePolicy: defaultLinkedExistingWritePolicy}, core.LocalRecord{ID: uuid.NewString()}, payload, apiIssue{})
	if err != nil {
		t.Fatalf("buildUpdateRequest returned error: %v", err)
	}
	if req.State != "closed" || req.Body != "" || len(req.Labels) != 0 {
		t.Fatalf("safe update request = %+v; want close-only update", req)
	}
	managedReq, err := buildUpdateRequest(
		settings{
			linkedExistingWritePolicy: linkedExistingWriteManagedFields,
			managedLabelPrefix:        defaultManagedLabelPrefix,
			defaultLabels:             []string{"attune/request"},
		},
		core.LocalRecord{ID: uuid.NewString()},
		payload,
		apiIssue{Labels: []apiLabel{{Name: "engineer"}, {Name: "attune/status/old"}}},
	)
	if err != nil {
		t.Fatalf("managed buildUpdateRequest returned error: %v", err)
	}
	if managedReq.State != "closed" || managedReq.Body == "" ||
		strings.Join(managedReq.Labels, ",") != "attune/request,bug,engineer" {
		t.Fatalf("managed update request = %+v; want marked body and merged labels", managedReq)
	}
	if _, err := buildUpdateRequest(settings{}, core.LocalRecord{}, localIssuePayload{ExternalKey: "ISS-1"}, apiIssue{}); err == nil {
		t.Fatal("buildUpdateRequest accepted non-numeric external key")
	}
}

func TestNormalizeIssueWriteKey(t *testing.T) {
	cfg := settings{owner: "acme", repo: "app"}
	if _, key, err := normalizeIssueWriteKey(cfg, " 42 "); err != nil || key != "42" {
		t.Fatalf("numeric issue key = %q, %v; want 42", key, err)
	}
	if number, key, err := normalizeIssueWriteKey(cfg, "Acme/app.git#42"); err != nil || number != 42 || key != "42" {
		t.Fatalf("repository issue key = %d/%q, %v; want 42/42", number, key, err)
	}
	if _, _, err := normalizeIssueWriteKey(cfg, "other/app#42"); err == nil {
		t.Fatal("normalizeIssueWriteKey accepted a different owner")
	}
	if _, _, err := normalizeIssueWriteKey(cfg, "acme/app#0"); err == nil {
		t.Fatal("normalizeIssueWriteKey accepted a non-positive issue number")
	}
}

func TestIssuePayloadMarkerBranches(t *testing.T) {
	if got := withCustomerRequestMarker("", "not-a-uuid", ""); got != "" {
		t.Fatalf("marker with invalid IDs = %q; want empty body", got)
	}
	requestID := uuid.NewString()
	if got := withCustomerRequestMarker("already marked <!-- attune:customer_request_id="+requestID+" -->", requestID, ""); !strings.Contains(got, requestID) {
		t.Fatalf("withCustomerRequestMarker removed existing marker: %q", got)
	}
	if got := markerID(uuid.NewString(), "not-a-uuid"); got == "" {
		t.Fatal("markerID did not fall back to record ID")
	}
	if got := extractCustomerRequestID("no marker here"); got != "" {
		t.Fatalf("extractCustomerRequestID = %q; want empty for no marker", got)
	}
	if got := extractCustomerRequestID("<!-- attune:customer_request_id=000000000000000000000000000000000000 -->"); got != "" {
		t.Fatalf("extractCustomerRequestID = %q; want empty for malformed uuid", got)
	}
	if got := extractAttuneCommentID("<!-- attune:comment_id=comment-1 -->"); got != "comment-1" {
		t.Fatalf("extractAttuneCommentID = %q; want comment-1", got)
	}
}

func TestManagedCommentBodyBranches(t *testing.T) {
	requestID := uuid.NewString()
	if body, marker := managedCommentBody(core.LocalRecord{}, localIssuePayload{}); body != "" || marker != "" {
		t.Fatalf("managedCommentBody without uuid = %q/%q; want empty", body, marker)
	}
	body, marker := managedCommentBody(core.LocalRecord{ID: requestID}, localIssuePayload{})
	if marker == "" || !strings.Contains(body, "No request context provided.") {
		t.Fatalf("managedCommentBody empty body = %q/%q; want fallback context", body, marker)
	}
	longBody, _ := managedCommentBody(core.LocalRecord{ID: requestID}, localIssuePayload{Body: strings.Repeat("x", 5000)})
	if !strings.Contains(longBody, strings.Repeat("x", 4500)) ||
		strings.Contains(longBody, strings.Repeat("x", 4501)) {
		t.Fatal("managedCommentBody did not truncate long body to 4500 runes")
	}
	if got := truncateString("hello", 0); got != "" {
		t.Fatalf("truncateString max=0 = %q; want empty", got)
	}
	if got := truncateString("hi", 10); got != "hi" {
		t.Fatalf("truncateString short = %q; want original", got)
	}
}

func TestGitHubStateBranches(t *testing.T) {
	if got := githubState(localIssuePayload{Status: "cancelled"}); got != "closed" {
		t.Fatalf("githubState cancelled = %q; want closed", got)
	}
	if got := githubState(localIssuePayload{Status: "triaged"}); got != "" {
		t.Fatalf("githubState default = %q; want no implicit reopen", got)
	}
	if got := githubState(localIssuePayload{State: "closed", Status: "open"}); got != "closed" {
		t.Fatalf("githubState explicit = %q; want explicit state", got)
	}
	if got := githubStateForUpdate(localIssuePayload{Status: "open"}, false); got != "" {
		t.Fatalf("githubStateForUpdate reopen disabled = %q; want omitted", got)
	}
	if got := githubStateForUpdate(localIssuePayload{Status: "open"}, true); got != "open" {
		t.Fatalf("githubStateForUpdate reopen enabled = %q; want open", got)
	}
	if got := githubStateForUpdate(localIssuePayload{State: "OPEN"}, false); got != "" {
		t.Fatalf("githubStateForUpdate explicit reopen disabled = %q; want omitted", got)
	}
	if got := githubState(localIssuePayload{}); got != "open" {
		t.Fatalf("githubState empty payload = %q; want open", got)
	}
}

func TestIssuePayloadWriteAndTargetBranches(t *testing.T) {
	if _, err := decodeIssueResponse([]byte(`not-json`)); err == nil {
		t.Fatal("decodeIssueResponse accepted invalid JSON")
	}
	if body, err := writeRequestPayload(issueWriteRequest{Title: "Bug"}); err != nil ||
		!strings.Contains(string(body), `"title":"Bug"`) {
		t.Fatalf("writeRequestPayload = %s err=%v", string(body), err)
	}
	if body, err := writeCommentPayload("hello"); err != nil ||
		!strings.Contains(string(body), `"body":"hello"`) {
		t.Fatalf("writeCommentPayload = %s err=%v", string(body), err)
	}
	if _, err := issueCommentURL(settings{apiBase: "%"}, 1); err == nil {
		t.Fatal("issueCommentURL accepted malformed API base")
	}
	if _, err := issueCommentURL(settings{}, 0); err == nil {
		t.Fatal("issueCommentURL accepted non-positive comment id")
	}
}

func TestPositiveNumberHelpers(t *testing.T) {
	if got := positiveInt(float64(42)); got != 42 {
		t.Fatalf("positiveInt float64 = %d; want 42", got)
	}
	if got := positiveInt(42); got != 42 {
		t.Fatalf("positiveInt int = %d; want 42", got)
	}
	if got := positiveInt(" 42 "); got != 42 {
		t.Fatalf("positiveInt string = %d; want 42", got)
	}
	for _, value := range []any{float64(-1), -1, "nope", nil} {
		if got := positiveInt(value); got != 0 {
			t.Fatalf("positiveInt(%v) = %d; want 0", value, got)
		}
	}
	if got := positiveInt64(float64(42)); got != 42 {
		t.Fatalf("positiveInt64 float64 = %d; want 42", got)
	}
	if got := positiveInt64(int64(42)); got != 42 {
		t.Fatalf("positiveInt64 int64 = %d; want 42", got)
	}
	if got := positiveInt64(42); got != 42 {
		t.Fatalf("positiveInt64 int = %d; want 42", got)
	}
	if got := positiveInt64(" 42 "); got != 42 {
		t.Fatalf("positiveInt64 string = %d; want 42", got)
	}
	for _, value := range []any{float64(-1), int64(-1), -1, "nope", nil} {
		if got := positiveInt64(value); got != 0 {
			t.Fatalf("positiveInt64(%v) = %d; want 0", value, got)
		}
	}
	if got := issueNumberFromExternalKey("not-a-repo-key"); got != 0 {
		t.Fatalf("issueNumberFromExternalKey invalid = %d; want 0", got)
	}
}

func TestDecodeCursorRejectsInvalidTime(t *testing.T) {
	_, err := decodeCursor([]byte(`{"updated_since":"not-time"}`))
	if err == nil {
		t.Fatal("decodeCursor returned nil error, want invalid time")
	}
}

func TestIssueVersionUsesUTC(t *testing.T) {
	ts := time.Date(2026, 7, 8, 10, 0, 0, 0, time.FixedZone("SGT", 8*60*60))
	got := issueVersion(apiIssue{UpdatedAt: ts})
	if got != "2026-07-08T02:00:00Z" {
		t.Fatalf("issueVersion = %q, want UTC RFC3339", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReadCloser) Close() error {
	return nil
}

var _ io.ReadCloser = errReadCloser{}
