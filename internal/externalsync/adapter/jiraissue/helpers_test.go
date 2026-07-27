// SPDX-License-Identifier: Apache-2.0

package jiraissue

import (
	"bytes"
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

func TestJiraConfigHelpers(t *testing.T) {
	testDecodeProviderConfig(t)
	testNormalizeJiraBaseSuccesses(t)
	testNormalizeJiraBaseFailures(t)
	testSettingsFromConnectionValidation(t)
	testCursorHelpers(t)
}

func testDecodeProviderConfig(t *testing.T) {
	t.Helper()
	cfg, err := decodeProviderConfig(nil)
	if err != nil {
		t.Fatalf("decodeProviderConfig(nil) returned error: %v", err)
	}
	if cfg.SiteURL != "" || cfg.APIBaseURL != "" || cfg.ProjectKey != "" || cfg.IssueType != "" || cfg.IssueTypeID != "" || cfg.Email != "" || cfg.RequestLabelPrefix != "" || len(cfg.StatusTransitions) != 0 {
		t.Fatalf("decodeProviderConfig(nil) = %+v; want zero value", cfg)
	}
	if _, err := decodeProviderConfig([]byte("{")); err == nil {
		t.Fatal("decodeProviderConfig accepted invalid JSON")
	}
}

func testNormalizeJiraBaseSuccesses(t *testing.T) {
	t.Helper()
	siteOnlySite, siteOnlyAPI, err := normalizeJiraBases("https://jira.example.com/", "")
	if err != nil {
		t.Fatalf("normalizeJiraBases(site only) returned error: %v", err)
	}
	if siteOnlySite != "https://jira.example.com" || siteOnlyAPI != "https://jira.example.com/rest/api/3" {
		t.Fatalf("normalizeJiraBases(site only) = %q / %q; want site and api base", siteOnlySite, siteOnlyAPI)
	}
	apiOnlySite, apiOnlyAPI, err := normalizeJiraBases("", "https://jira.example.com/rest/api/3/")
	if err != nil {
		t.Fatalf("normalizeJiraBases(api only) returned error: %v", err)
	}
	if apiOnlySite != "https://jira.example.com" || apiOnlyAPI != "https://jira.example.com/rest/api/3" {
		t.Fatalf("normalizeJiraBases(api only) = %q / %q; want site and api base", apiOnlySite, apiOnlyAPI)
	}
	if _, _, err := normalizeJiraBases("", ""); err == nil {
		t.Fatal("normalizeJiraBases accepted empty bases")
	}
	if _, _, err := resolveBases("", "", ""); err == nil {
		t.Fatal("resolveBases accepted empty connection and provider bases")
	}
	fallbackAPI, fallbackSite, err := resolveBases("https://jira.example.com", "", "")
	if err != nil {
		t.Fatalf("resolveBases connection fallback returned error: %v", err)
	}
	if fallbackAPI != "https://jira.example.com/rest/api/3" || fallbackSite != "https://jira.example.com" {
		t.Fatalf("resolveBases fallback = %q / %q; want site and api base", fallbackAPI, fallbackSite)
	}
	proxySite, proxyAPI, err := normalizeJiraBases("", "https://jira-proxy.example.com/api/")
	if err != nil {
		t.Fatalf("normalizeJiraBases(proxy API) returned error: %v", err)
	}
	if proxySite != "https://jira-proxy.example.com/api" || proxyAPI != "https://jira-proxy.example.com/api" {
		t.Fatalf("normalizeJiraBases(proxy API) = %q / %q; want API base as site fallback", proxySite, proxyAPI)
	}
}

func testNormalizeJiraBaseFailures(t *testing.T) {
	t.Helper()
	if _, _, err := normalizeJiraBases("", ""); err == nil {
		t.Fatal("normalizeJiraBases accepted empty bases")
	}
	if _, _, err := resolveBases("", "", ""); err == nil {
		t.Fatal("resolveBases accepted empty connection and provider bases")
	}
	if _, _, err := normalizeJiraBases("http://[::1", ""); err == nil {
		t.Fatal("normalizeJiraBases accepted invalid site URL")
	}
	if _, _, err := normalizeJiraBases("", "http://[::1"); err == nil {
		t.Fatal("normalizeJiraBases accepted invalid API base URL")
	}
	if _, _, err := normalizeJiraBases("http://[::1", "https://jira.example.com/rest/api/3"); err == nil {
		t.Fatal("normalizeJiraBases accepted invalid explicit site URL")
	}
}

func testSettingsFromConnectionValidation(t *testing.T) {
	t.Helper()
	testSettingsFromConnectionSuccess(t)
	testSettingsFromConnectionInvalidInputs(t)
}

func testSettingsFromConnectionSuccess(t *testing.T) {
	t.Helper()
	validConn := core.Connection{
		BaseURL: "https://jira.example.com",
		ProviderConfig: mustJSON(t, providerConfig{
			ProjectKey: "ACME",
			IssueType:  "Task",
			Email:      "bot@example.com",
		}),
		Credential: []byte("jira-token"),
	}
	settings, err := settingsFromConnection(validConn)
	if err != nil {
		t.Fatalf("settingsFromConnection returned error: %v", err)
	}
	if settings.siteURL != "https://jira.example.com" || settings.apiBase != "https://jira.example.com/rest/api/3" {
		t.Fatalf("settingsFromConnection bases = %q / %q; want site and api base", settings.siteURL, settings.apiBase)
	}
	apiOnlyConn := core.Connection{
		ProviderConfig: mustJSON(t, providerConfig{
			APIBaseURL:         " https://jira.example.com/rest/api/3/ ",
			ProjectKey:         " ACME ",
			IssueTypeID:        " 10001 ",
			Email:              " bot@example.com ",
			RequestLabelPrefix: " Acme Requests ",
			StatusTransitions: map[string]string{
				" Shipped ": " Done ",
				" ":         "ignored",
				"open":      " ",
			},
		}),
		Credential: []byte(" jira-token "),
	}
	apiOnlySettings, err := settingsFromConnection(apiOnlyConn)
	if err != nil {
		t.Fatalf("settingsFromConnection(api only) returned error: %v", err)
	}
	if apiOnlySettings.siteURL != "https://jira.example.com" || apiOnlySettings.apiBase != "https://jira.example.com/rest/api/3" ||
		apiOnlySettings.issueType != "" || apiOnlySettings.issueTypeID != "10001" ||
		apiOnlySettings.requestLabelPrefix != "acme-requests-" ||
		apiOnlySettings.statusTransitions["shipped"] != "Done" || len(apiOnlySettings.statusTransitions) != 1 {
		t.Fatalf("api-only settings = %+v; want trimmed API base, issue type ID, label prefix, and transitions", apiOnlySettings)
	}
	cfgWithID := settings
	cfgWithID.issueTypeID = "10001"
	req, err := buildCreateRequest(cfgWithID, core.LocalRecord{}, localIssuePayload{Title: "Create issue"})
	if err != nil {
		t.Fatalf("buildCreateRequest with issueTypeID returned error: %v", err)
	}
	if req.Fields.IssueType.ID != "10001" || req.Fields.IssueType.Name != "" {
		t.Fatalf("buildCreateRequest issue type = %+v; want ID-only reference", req.Fields.IssueType)
	}
}

func testSettingsFromConnectionInvalidInputs(t *testing.T) {
	t.Helper()
	cases := []struct {
		name string
		conn core.Connection
	}{
		{
			name: "project key",
			conn: core.Connection{
				BaseURL: "https://jira.example.com",
				ProviderConfig: mustJSON(t, providerConfig{
					ProjectKey: "/bad",
					IssueType:  "Task",
					Email:      "bot@example.com",
				}),
				Credential: []byte("jira-token"),
			},
		},
		{
			name: "issue type",
			conn: core.Connection{
				BaseURL: "https://jira.example.com",
				ProviderConfig: mustJSON(t, providerConfig{
					ProjectKey: "ACME",
					Email:      "bot@example.com",
				}),
				Credential: []byte("jira-token"),
			},
		},
		{
			name: "email",
			conn: core.Connection{
				BaseURL: "https://jira.example.com",
				ProviderConfig: mustJSON(t, providerConfig{
					ProjectKey: "ACME",
					IssueType:  "Task",
				}),
				Credential: []byte("jira-token"),
			},
		},
		{
			name: "credential",
			conn: core.Connection{
				BaseURL: "https://jira.example.com",
				ProviderConfig: mustJSON(t, providerConfig{
					ProjectKey: "ACME",
					IssueType:  "Task",
					Email:      "bot@example.com",
				}),
			},
		},
		{
			name: "request label prefix",
			conn: core.Connection{
				BaseURL: "https://jira.example.com",
				ProviderConfig: mustJSON(t, providerConfig{
					ProjectKey:         "ACME",
					IssueType:          "Task",
					Email:              "bot@example.com",
					RequestLabelPrefix: "!!!",
				}),
				Credential: []byte("jira-token"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := settingsFromConnection(tc.conn); err == nil {
				t.Fatalf("settingsFromConnection(%s) accepted invalid config", tc.name)
			}
		})
	}
}

func testCursorHelpers(t *testing.T) {
	t.Helper()
	if got := querySince(cursorState{}); !got.IsZero() {
		t.Fatalf("querySince(empty) = %v; want zero time", got)
	}
	if got := querySince(cursorState{UpdatedSince: "bad"}); !got.IsZero() {
		t.Fatalf("querySince(invalid) = %v; want zero time", got)
	}
	ts := querySince(cursorState{UpdatedSince: "2026-07-08T11:00:00Z"})
	wantTS, _ := time.Parse(time.RFC3339, "2026-07-08T10:59:00Z")
	if !ts.Equal(wantTS) {
		t.Fatalf("querySince(valid) = %v; want %v", ts, wantTS)
	}
	if _, err := decodeCursor([]byte(`{"updated_since":"2026-07-08T11:00:00Z","start_at":-1}`)); err == nil {
		t.Fatal("decodeCursor accepted negative start_at")
	}
	if _, err := decodeCursor([]byte(`{`)); err == nil {
		t.Fatal("decodeCursor accepted invalid JSON")
	}
	if _, err := decodeCursor([]byte(`{"updated_since":"bad","start_at":0}`)); err == nil {
		t.Fatal("decodeCursor accepted invalid timestamp")
	}
	cursor, err := decodeCursor([]byte(`{"updated_since":"2026-07-08T11:00:00Z","start_at":7}`))
	if err != nil {
		t.Fatalf("decodeCursor(valid) returned error: %v", err)
	}
	raw, err := encodeCursor(cursor)
	if err != nil {
		t.Fatalf("encodeCursor returned error: %v", err)
	}
	if !strings.Contains(string(raw), `"start_at":7`) || !strings.Contains(string(raw), `"updated_since":"2026-07-08T11:00:00Z"`) {
		t.Fatalf("encodeCursor = %s; want preserved cursor fields", string(raw))
	}
}

func TestJiraHTTPHelpers(t *testing.T) {
	testRequestAndClassification(t)
	testRetryAfterTime(t)
	testExtractJiraMessage(t)
	testClassifyHTTPStatus(t)
	testClassifyError(t)
	testTruncateAndProviderMetadata(t)
}

func testRequestAndClassification(t *testing.T) {
	t.Helper()
	testRequestErrorPath(t)
	testBuildRequestHeaders(t)
	testProviderMetadata(t)
}

func testRequestErrorPath(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q; want %q", got, userAgent)
		}
		if got := r.Header.Get("Authorization"); got != "Basic "+basicAuth("bot@example.com", "jira-token") {
			t.Fatalf("Authorization = %q; want basic auth header", got)
		}
		w.Header().Set("X-Request-Id", "jira-req-123")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":" rate limit ","errorMessages":["slow down"],"errors":{"field":"invalid"}}`))
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(WithHTTPClient(server.Client()))
	body, headers, err := provider.request(context.Background(), testSettings(server.URL), http.MethodGet, server.URL+"/rest/api/3/search?token=secret", nil)
	if err == nil {
		t.Fatal("request returned nil error for 429 response")
	}
	if !bytes.Contains(body, []byte("rate limit")) {
		t.Fatalf("request body = %q; want rate limit response", string(body))
	}
	if headers.Get("X-Request-Id") != "jira-req-123" {
		t.Fatalf("request headers lost request id: %v", headers)
	}

	classified := classifyError(err)
	if classified.Kind != "rate_limited" || !classified.Retryable || classified.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("classified error = %+v; want retryable rate limit", classified)
	}
	if classified.ProviderRequestID != "jira-req-123" || classified.RetryAfter == nil || !classified.RetryAfter.After(time.Now()) {
		t.Fatalf("classified error retry metadata = %+v; want request id and retry-after", classified)
	}
	if strings.Contains(classified.Message, "token=secret") {
		t.Fatalf("classified error leaked secret in message: %q", classified.Message)
	}
	if err.Error() == "" {
		t.Fatal("provider error string should not be empty")
	}
}

func testBuildRequestHeaders(t *testing.T) {
	t.Helper()
	base := "https://jira.example.com"
	if _, err := buildRequest(context.Background(), testSettings(base), http.MethodGet, "http://[::1", nil); err == nil {
		t.Fatal("buildRequest accepted malformed URL")
	}
	req, err := buildRequest(context.Background(), testSettings(base), http.MethodPost, base+"/rest/api/3/issue", []byte(`{"summary":"Create issue"}`))
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q; want JSON", got)
	}
	if got := req.Header.Get("Authorization"); got != "Basic "+basicAuth("bot@example.com", "jira-token") {
		t.Fatalf("buildRequest Authorization = %q; want basic auth header", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q; want application/json", got)
	}

	emptyReq, err := buildRequest(context.Background(), testSettings(base), http.MethodGet, base+"/rest/api/3/myself", nil)
	if err != nil {
		t.Fatalf("buildRequest(nil payload) returned error: %v", err)
	}
	if got := emptyReq.Header.Get("Content-Type"); got != "" {
		t.Fatalf("nil payload should not set content type, got %q", got)
	}
}

func testProviderMetadata(t *testing.T) {
	t.Helper()
	if got := NewProvider().Provider(); got != providerID {
		t.Fatalf("Provider() = %q; want %q", got, providerID)
	}
	schemas, err := NewProvider().Discover(context.Background(), core.Connection{})
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Type != "issue" || len(schemas[0].RequiredFields) != 1 {
		t.Fatalf("Discover returned unexpected schemas: %+v", schemas)
	}
	if got := NewProvider().ClassifyError(validationError("jira issue %s", "missing")); got.Kind != "validation" {
		t.Fatalf("ClassifyError(validation) = %+v; want validation", got)
	}
}

func testRetryAfterTime(t *testing.T) {
	t.Helper()
	if got := retryAfterTime(""); got != nil {
		t.Fatalf("retryAfterTime(empty) = %v; want nil", got)
	}
	if got := retryAfterTime("not-a-time"); got != nil {
		t.Fatalf("retryAfterTime(invalid) = %v; want nil", got)
	}
	if got := retryAfterTime("2"); got == nil || !got.After(time.Now()) {
		t.Fatalf("retryAfterTime(seconds) = %v; want future time", got)
	}
	httpDate := "Wed, 21 Oct 2015 07:28:00 GMT"
	if got := retryAfterTime(httpDate); got == nil || !got.Equal(time.Date(2015, 10, 21, 7, 28, 0, 0, time.UTC)) {
		t.Fatalf("retryAfterTime(http date) = %v; want parsed date", got)
	}
}

func testExtractJiraMessage(t *testing.T) {
	t.Helper()
	if got := extractJiraMessage([]byte(`{"message":" primary ","errorMessages":[" one ","two"],"errors":{"field":"bad"}}`)); !strings.Contains(got, "primary") || !strings.Contains(got, "one") || !strings.Contains(got, "two") || !strings.Contains(got, "field: bad") {
		t.Fatalf("extractJiraMessage(json) = %q; want message, list, and field error text", got)
	}
	if got := extractJiraMessage([]byte(`{"errors":{"":"fallback"}}`)); got != "fallback" {
		t.Fatalf("extractJiraMessage(empty key) = %q; want fallback", got)
	}
	if got := extractJiraMessage([]byte(`{"errors":{"onlykey":""}}`)); got != "onlykey" {
		t.Fatalf("extractJiraMessage(empty value) = %q; want key only", got)
	}
	if got := extractJiraMessage([]byte("plain text")); got != "plain text" {
		t.Fatalf("extractJiraMessage(plain text) = %q; want trimmed input", got)
	}
	if got := jiraErrorMessage(0, "https://jira.example.com/rest/api/3/search", nil); !strings.Contains(got, "provider error") {
		t.Fatalf("jiraErrorMessage(0) = %q; want provider error fallback", got)
	}
}

func testClassifyHTTPStatus(t *testing.T) {
	t.Helper()
	statusCases := []struct {
		name      string
		status    int
		body      []byte
		wantKind  string
		wantRetry bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantKind: "auth_failed"},
		{name: "forbidden rate limited", status: http.StatusForbidden, body: []byte(`{"message":"rate limit"}`), wantKind: "rate_limited", wantRetry: true},
		{name: "forbidden auth", status: http.StatusForbidden, wantKind: "auth_failed"},
		{name: "not found", status: http.StatusNotFound, wantKind: "not_found"},
		{name: "too many requests", status: http.StatusTooManyRequests, wantKind: "rate_limited", wantRetry: true},
		{name: "bad request", status: http.StatusBadRequest, wantKind: "validation"},
		{name: "unprocessable", status: http.StatusUnprocessableEntity, wantKind: "validation"},
		{name: "conflict", status: http.StatusConflict, wantKind: "conflict"},
		{name: "bad gateway", status: http.StatusBadGateway, wantKind: "provider_unavailable", wantRetry: true},
		{name: "service unavailable", status: http.StatusServiceUnavailable, wantKind: "provider_unavailable", wantRetry: true},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, wantKind: "provider_unavailable", wantRetry: true},
		{name: "internal server error", status: http.StatusInternalServerError, wantKind: "provider_unavailable", wantRetry: true},
		{name: "other", status: http.StatusTeapot, wantKind: "provider_error"},
	}
	for _, tc := range statusCases {
		t.Run(tc.name, func(t *testing.T) {
			kind, retryable := classifyHTTPStatus(tc.status, tc.body)
			if kind != tc.wantKind || retryable != tc.wantRetry {
				t.Fatalf("classifyHTTPStatus(%d) = %q, %t; want %q, %t", tc.status, kind, retryable, tc.wantKind, tc.wantRetry)
			}
		})
	}
}

func testClassifyError(t *testing.T) {
	t.Helper()
	testClassifyErrorProvider(t)
	testClassifyErrorFallbacks(t)
}

func testClassifyErrorProvider(t *testing.T) {
	t.Helper()
	providerErr := jiraHTTPError(http.StatusTooManyRequests, "https://jira.example.com/rest/api/3/search?token=secret", []byte(`{"message":"rate limit"}`), "jira-req-123", "2")
	classified := classifyError(providerErr)
	if classified.Kind != "rate_limited" || !classified.Retryable || classified.ProviderRequestID != "jira-req-123" {
		t.Fatalf("classifyError(provider error) = %+v; want rate limited metadata", classified)
	}
	if strings.Contains(classified.Message, "token=secret") {
		t.Fatalf("classifyError(provider error) leaked secret: %q", classified.Message)
	}
}

func testClassifyErrorFallbacks(t *testing.T) {
	t.Helper()
	if got := classifyError(nil); got.Kind != "" || got.Retryable {
		t.Fatalf("classifyError(nil) = %+v; want zero value", got)
	}
	if got := classifyError(validationError("jira local record payload is required")); got.Kind != "validation" || got.Retryable {
		t.Fatalf("classifyError(validation) = %+v; want non-retryable validation", got)
	}
	if got := classifyError(ptrext.Of(nethardening.BlockedError{Host: "jira.example.com", Reason: "private network"})); got.Kind != "validation" || got.Retryable {
		t.Fatalf("classifyError(blocked) = %+v; want validation", got)
	}
	if got := classifyError(context.Canceled); got.Kind != "provider_unavailable" || !got.Retryable {
		t.Fatalf("classifyError(context canceled) = %+v; want retryable provider_unavailable", got)
	}
	if got := classifyError(ptrext.Of(url.Error{Op: "Get", URL: "https://secret.example.com/path?token=secret", Err: errors.New("boom")})); got.Kind != "provider_unavailable" || !got.Retryable || strings.Contains(got.Message, "token=secret") {
		t.Fatalf("classifyError(url error) = %+v; want redacted retryable provider_unavailable", got)
	}
	if got := classifyError(errors.New("boom")); got.Kind != "provider_error" || !got.Retryable {
		t.Fatalf("classifyError(generic) = %+v; want retryable provider_error", got)
	}
}

func testTruncateAndProviderMetadata(t *testing.T) {
	t.Helper()
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate(long) = %q; want prefix", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("truncate(short) = %q; want original", got)
	}
	if got := validationError("jira issue %s", "missing").Error(); got != "jira issue missing" {
		t.Fatalf("validationError.Error() = %q; want formatted message", got)
	}
}

func TestJiraIssueAndTransitionHelpers(t *testing.T) {
	cfg := testSettings("https://jira.example.com")
	customerRequestID := uuid.NewString()
	testIssueURLHelpers(t, cfg)
	testCommentAuthorAndTimeHelpers(t)
	testIssueTextAndMarkerHelpers(t, cfg, customerRequestID)
	testTransitionHelpers(t)
	testNormalizeLocalPayload(t, customerRequestID)
}

func testIssueURLHelpers(t *testing.T, cfg settings) {
	t.Helper()
	if got := browseBaseURL(cfg); got != "https://jira.example.com" {
		t.Fatalf("browseBaseURL(site) = %q; want site URL", got)
	}
	cfg.siteURL = ""
	if got := browseBaseURL(cfg); got != "https://jira.example.com" {
		t.Fatalf("browseBaseURL(api fallback) = %q; want derived site URL", got)
	}
	if got := issueURLFromKey(cfg, ""); got != "" {
		t.Fatalf("issueURLFromKey(empty) = %q; want empty string", got)
	}
	if got := issueURLFromKey(cfg, " ACME-1 "); got != "https://jira.example.com/browse/ACME-1" {
		t.Fatalf("issueURLFromKey = %q; want browse URL", got)
	}
	if got := issueURLFromIssue(cfg, jiraIssue{Self: "https://jira.example.com/rest/api/3/issue/ACME-2"}); got != "https://jira.example.com/rest/api/3/issue/ACME-2" {
		t.Fatalf("issueURLFromIssue fallback = %q; want self URL", got)
	}
	if got := issueURL(settings{apiBase: "https://jira.example.com/rest/api/3"}, "ACME-3"); got != "https://jira.example.com/browse/ACME-3" {
		t.Fatalf("issueURL(api fallback) = %q; want browse URL", got)
	}
	if got := issueURL(settings{}, "ACME-4"); got != "" {
		t.Fatalf("issueURL(empty bases) = %q; want empty URL", got)
	}
	if got := issueURLFromKey(settings{}, "ACME-5"); got != "ACME-5" {
		t.Fatalf("issueURLFromKey(no base) = %q; want issue key fallback", got)
	}
}

func testCommentAuthorAndTimeHelpers(t *testing.T) {
	t.Helper()
	if got := commentAuthor(nil); got != "" {
		t.Fatalf("commentAuthor(nil) = %q; want empty", got)
	}
	if got := commentAuthor(ptrext.Of(jiraUser{DisplayName: "  Alice  "})); got != "Alice" {
		t.Fatalf("commentAuthor(display name) = %q; want trimmed name", got)
	}
	if got := commentAuthor(ptrext.Of(jiraUser{Email: "  alice@example.com  "})); got != "alice@example.com" {
		t.Fatalf("commentAuthor(email) = %q; want trimmed email", got)
	}
	if got := commentAuthor(ptrext.Of(jiraUser{AccountID: "  account-1  "})); got != "account-1" {
		t.Fatalf("commentAuthor(account id) = %q; want trimmed account id", got)
	}
	if got := issueVersionTime(time.Time{}); got != "" {
		t.Fatalf("issueVersionTime(zero) = %q; want empty", got)
	}
	ts := time.Date(2026, 7, 8, 11, 0, 0, 0, time.UTC)
	if got := issueVersionTime(ts); got != "2026-07-08T11:00:00Z" {
		t.Fatalf("issueVersionTime = %q; want RFC3339Nano UTC", got)
	}
	if got := normalizedTime("bad"); got != "" {
		t.Fatalf("normalizedTime(invalid) = %q; want empty", got)
	}
	if got := normalizedTime("2026-07-08 11:00"); got != "2026-07-08T11:00:00Z" {
		t.Fatalf("normalizedTime(layout) = %q; want normalized UTC timestamp", got)
	}
	if got, err := parseJiraTime("2026/07/08 11:00"); err != nil || !got.Equal(ts) {
		t.Fatalf("parseJiraTime = %v, %v; want %v, nil", got, err, ts)
	}
	if got := issueCommentCount(jiraCommentPage{Comments: []jiraComment{{ID: "1"}, {ID: "2"}}}); got != 2 {
		t.Fatalf("issueCommentCount(comments) = %d; want comment length", got)
	}
}

func testIssueTextAndMarkerHelpers(t *testing.T, cfg settings, customerRequestID string) {
	t.Helper()
	testIssueTextHelpers(t)
	testExtractCustomerRequestIDHelpers(t, cfg, customerRequestID)
	testIssueHasMarkerHelpers(t, cfg, customerRequestID)
}

func testIssueTextHelpers(t *testing.T) {
	t.Helper()
	rawDoc := mustJSON(t, adfDocument("Hello\nWorld"))
	if got := issueText(rawDoc); got != "Hello\nWorld" {
		t.Fatalf("issueText(adf) = %q; want plain text", got)
	}
	if got := issueText(json.RawMessage("not-json")); got != "not-json" {
		t.Fatalf("issueText(fallback) = %q; want raw text", got)
	}
	if got := issueText(nil); got != "" {
		t.Fatalf("issueText(nil) = %q; want empty", got)
	}
	if got := adfText(map[string]any{"type": "hardBreak"}); got != "\n" {
		t.Fatalf("adfText(hardBreak) = %q; want newline", got)
	}
	if got := adfText(map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "hello"}, map[string]any{"type": "hardBreak"}}}); got != "hello\n" {
		t.Fatalf("adfText(paragraph) = %q; want paragraph text with newline", got)
	}
	if got := adfText([]any{map[string]any{"type": "text", "text": "a"}, map[string]any{"type": "text", "text": "b"}}); got != "ab" {
		t.Fatalf("adfText(slice) = %q; want concatenated text", got)
	}
	if got := adfText(map[string]any{"type": "mention"}); got != "" {
		t.Fatalf("adfText(unknown) = %q; want empty", got)
	}
	if _, err := writeRequestPayload(map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("writeRequestPayload accepted an unsupported value")
	}
}

func testExtractCustomerRequestIDHelpers(t *testing.T, cfg settings, customerRequestID string) {
	t.Helper()
	rawDoc := mustJSON(t, adfDocument("Hello\nWorld"))
	labelIssue := jiraIssue{Fields: jiraIssueFields{Labels: []string{requestLabel(cfg, customerRequestID)}}}
	if got := extractCustomerRequestID(cfg, labelIssue); got != customerRequestID {
		t.Fatalf("extractCustomerRequestID(label) = %q; want %q", got, customerRequestID)
	}
	descIssue := jiraIssue{Fields: jiraIssueFields{Description: rawDoc}}
	if got := extractCustomerRequestID(cfg, descIssue); got != "" {
		t.Fatalf("extractCustomerRequestID(description without marker) = %q; want empty", got)
	}
	descMarkerDoc := mustJSON(t, adfDocument("Request attune:customer_request_id="+customerRequestID))
	descIssue = jiraIssue{Fields: jiraIssueFields{Description: descMarkerDoc}}
	if got := extractCustomerRequestID(cfg, descIssue); got != customerRequestID {
		t.Fatalf("extractCustomerRequestID(description) = %q; want %q", got, customerRequestID)
	}
	commentIssue := jiraIssue{
		Fields: jiraIssueFields{
			Comment: jiraCommentPage{
				Comments: []jiraComment{{
					Body: mustJSON(t, adfDocument("Comment attune:customer_request_id="+customerRequestID)),
				}},
			},
		},
	}
	if got := extractCustomerRequestID(cfg, commentIssue); got != customerRequestID {
		t.Fatalf("extractCustomerRequestID(comment) = %q; want %q", got, customerRequestID)
	}
	if got := extractCustomerRequestIDFromText("attune:customer_request_id=" + customerRequestID); got != customerRequestID {
		t.Fatalf("extractCustomerRequestIDFromText = %q; want %q", got, customerRequestID)
	}
	if got := extractCustomerRequestIDFromText("attune:customer_request_id=not-a-uuid"); got != "" {
		t.Fatalf("extractCustomerRequestIDFromText(invalid) = %q; want empty", got)
	}
	if got := extractCustomerRequestIDFromLabels(cfg, []string{defaultLabelPrefix + "not-a-uuid"}); got != "" {
		t.Fatalf("extractCustomerRequestIDFromLabels(invalid) = %q; want empty", got)
	}
	if got := markerCustomerRequestID(""); got != "" {
		t.Fatalf("markerCustomerRequestID(empty) = %q; want empty", got)
	}
	if got := markerCustomerRequestID("not-a-uuid"); got != "" {
		t.Fatalf("markerCustomerRequestID(invalid) = %q; want empty", got)
	}
	if got := markerCustomerRequestID("prefix " + jiraMarkerCommentText + customerRequestID); got != customerRequestID {
		t.Fatalf("markerCustomerRequestID(text marker) = %q; want %q", got, customerRequestID)
	}
}

func testTransitionHelpers(t *testing.T) {
	t.Helper()
	transitions := jiraTestTransitions()
	testFindTransitionHelpers(t, transitions)
	testChooseHeuristicTransitionHelpers(t, transitions)
	testStatusCategoryHelpers(t)
	testCanSkipTransitionHelpers(t)
}

func jiraTestTransitions() []jiraTransition {
	newTransition := func(id, name, category string) jiraTransition {
		transition := jiraTransition{ID: id, Name: name}
		transition.To.ID = id + "-to"
		transition.To.Name = name
		transition.To.StatusCategory.Key = category
		return transition
	}
	return []jiraTransition{
		newTransition("11", "Done", "done"),
		newTransition("12", "In Progress", "indeterminate"),
		newTransition("13", "To Do", "new"),
	}
}

func testFindTransitionHelpers(t *testing.T, transitions []jiraTransition) {
	t.Helper()
	if got := findTransition(transitions, "11"); got == nil || got.Name != "Done" {
		t.Fatalf("findTransition by ID = %+v; want Done", got)
	}
	if got := findTransition(transitions, "done"); got == nil || got.Name != "Done" {
		t.Fatalf("findTransition by status name = %+v; want Done", got)
	}
	if got := findTransition(transitions, "new"); got == nil || got.Name != "To Do" {
		t.Fatalf("findTransition by category = %+v; want To Do", got)
	}
	if got := findTransition(transitions, "missing"); got != nil {
		t.Fatalf("findTransition(missing) = %+v; want nil", got)
	}
}

func testChooseHeuristicTransitionHelpers(t *testing.T, transitions []jiraTransition) {
	t.Helper()
	if got := chooseHeuristicTransition(transitions, "shipped"); got == nil || got.To.StatusCategory.Key != "done" {
		t.Fatalf("chooseHeuristicTransition(shipped) = %+v; want done transition", got)
	}
	if got := chooseHeuristicTransition(transitions, "in_progress"); got == nil || got.To.StatusCategory.Key != "indeterminate" {
		t.Fatalf("chooseHeuristicTransition(in_progress) = %+v; want indeterminate transition", got)
	}
	if got := chooseHeuristicTransition(transitions, "planned"); got == nil || got.To.StatusCategory.Key != "new" {
		t.Fatalf("chooseHeuristicTransition(planned) = %+v; want new transition", got)
	}
	if got := chooseHeuristicTransition(transitions, "unknown"); got != nil {
		t.Fatalf("chooseHeuristicTransition(unknown) = %+v; want nil", got)
	}
}

func testStatusCategoryHelpers(t *testing.T) {
	t.Helper()
	if !statusCategoryMatches("done", "shipped") || !statusCategoryMatches("indeterminate", "in_progress") || !statusCategoryMatches("new", "planned") {
		t.Fatal("statusCategoryMatches should recognize Jira categories")
	}
	if statusCategoryMatches("done", "planned") || statusCategoryMatches("new", "shipped") || statusCategoryMatches("done", "unknown") {
		t.Fatal("statusCategoryMatches should reject mismatched categories")
	}
}

func testCanSkipTransitionHelpers(t *testing.T) {
	t.Helper()
	if !canSkipTransition("planned") || !canSkipTransition("open") || canSkipTransition("shipped") {
		t.Fatal("canSkipTransition returned unexpected result")
	}
}

func testIssueHasMarkerHelpers(t *testing.T, cfg settings, customerRequestID string) {
	t.Helper()
	labelIssue := jiraIssue{Fields: jiraIssueFields{Labels: []string{requestLabel(cfg, customerRequestID)}}}
	descIssue := jiraIssue{Fields: jiraIssueFields{Description: mustJSON(t, adfDocument("Hello\nWorld"))}}
	descMarkerDoc := mustJSON(t, adfDocument("Request attune:customer_request_id="+customerRequestID))
	descIssueWithMarker := jiraIssue{Fields: jiraIssueFields{Description: descMarkerDoc}}
	commentIssue := jiraIssue{
		Fields: jiraIssueFields{
			Comment: jiraCommentPage{
				Comments: []jiraComment{{
					Body: mustJSON(t, adfDocument("Comment attune:customer_request_id="+customerRequestID)),
				}},
			},
		},
	}
	if !issueHasMarker(cfg, labelIssue, customerRequestID) || !issueHasMarker(cfg, descIssueWithMarker, customerRequestID) || !issueHasMarker(cfg, commentIssue, customerRequestID) {
		t.Fatal("issueHasMarker should match label, description, and comment markers")
	}
	if issueHasMarker(cfg, labelIssue, "not-a-uuid") {
		t.Fatal("issueHasMarker should reject invalid marker input")
	}
	if issueHasMarker(cfg, jiraIssue{}, customerRequestID) {
		t.Fatal("issueHasMarker should not match empty issue")
	}
	if issueHasMarker(cfg, descIssue, customerRequestID) {
		t.Fatal("issueHasMarker should not match issue without marker")
	}
}

func testNormalizeLocalPayload(t *testing.T, customerRequestID string) {
	t.Helper()
	if _, err := normalizeLocalPayload(core.LocalRecord{}); err == nil {
		t.Fatal("normalizeLocalPayload accepted empty payload")
	}
	if _, err := normalizeLocalPayload(core.LocalRecord{Payload: []byte(`{"labels":"not-an-array"}`)}); err == nil {
		t.Fatal("normalizeLocalPayload accepted payload with invalid label shape")
	}
	payload, err := normalizeLocalPayload(core.LocalRecord{Payload: mustJSON(t, map[string]any{
		"title":               "  Update issue  ",
		"body":                "  Body text  ",
		"labels":              []string{"Beta", "alpha", "beta"},
		"status":              " shipped ",
		"priority":            " high ",
		"display_id":          "  REQ-1  ",
		"customer_request_id": customerRequestID,
	})})
	if err != nil {
		t.Fatalf("normalizeLocalPayload returned error: %v", err)
	}
	if payload.Title != "Update issue" || payload.Body != "Body text" || !payload.BodySet || payload.DisplayID != "REQ-1" || payload.Status != "shipped" || payload.Priority != "high" {
		t.Fatalf("normalizeLocalPayload = %+v; want trimmed payload", payload)
	}
	if len(payload.Labels) != 2 || payload.Labels[0] != "alpha" || payload.Labels[1] != "beta" {
		t.Fatalf("normalizeLocalPayload labels = %v; want normalized labels", payload.Labels)
	}
	emptyBodyPayload, err := normalizeLocalPayload(core.LocalRecord{Payload: mustJSON(t, map[string]any{"title": "Update", "labels": []string{"one"}})})
	if err != nil {
		t.Fatalf("normalizeLocalPayload(no body) returned error: %v", err)
	}
	if emptyBodyPayload.BodySet {
		t.Fatal("normalizeLocalPayload should not set BodySet when body field is absent")
	}
}

func TestJiraIssueNormalizationBranches(t *testing.T) {
	testJiraIssueNormalizesMetadataBranches(t)
	testJiraIssueNormalizationErrorBranches(t)
}

func testJiraIssueNormalizesMetadataBranches(t *testing.T) {
	t.Helper()
	cfg := testSettings("https://jira.example.com")
	customerRequestID := uuid.NewString()
	issue := jiraIssueFixture(t, "ACME-9", customerRequestID)
	issue.Fields.Assignee = ptrext.Of(jiraUser{DisplayName: "  Assignee  "})
	issue.Fields.Reporter = ptrext.Of(jiraUser{DisplayName: " Reporter "})
	issue.Fields.Comment.Total = 0
	issue.Fields.IssueLinks = append(issue.Fields.IssueLinks, jiraIssueLink{
		Type: jiraLinkType{Name: "Relates"},
		InwardIssue: ptrext.Of(jiraLinkedIssue{
			Key: " ACME-8 ",
		}),
	})

	records, maxUpdated, err := normalizeIssues(cfg, []jiraIssue{issue})
	if err != nil {
		t.Fatalf("normalizeIssues returned error: %v", err)
	}
	if len(records) != 1 || records[0].Key != "ACME-9" || records[0].LocalObjectID != customerRequestID || maxUpdated.IsZero() {
		t.Fatalf("normalized records = %+v max=%v; want one record with marker and watermark", records, maxUpdated)
	}
	var payload normalizedIssue
	if err := json.Unmarshal(records[0].Payload, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode normalized payload: %v", err)
	}
	if payload.Assignee != "Assignee" || payload.Reporter != "Reporter" || payload.CommentCount != 1 || len(payload.IssueLinks) != 2 {
		t.Fatalf("normalized payload = %+v; want assignee, reporter, count fallback, and both link directions", payload)
	}
}

func testJiraIssueNormalizationErrorBranches(t *testing.T) {
	t.Helper()
	cfg := testSettings("https://jira.example.com")
	if _, _, err := normalizeIssues(cfg, []jiraIssue{{Fields: jiraIssueFields{Updated: "bad"}}}); err == nil {
		t.Fatal("normalizeIssues accepted invalid updated timestamp")
	}
	if _, err := decodeIssueResponse([]byte(`{`)); err == nil {
		t.Fatal("decodeIssueResponse accepted invalid JSON")
	}
	if !isNotFound(jiraHTTPError(http.StatusNotFound, "https://jira.example.com/rest/api/3/issue/ACME-404", nil, "", "")) {
		t.Fatal("isNotFound should recognize Jira 404 provider errors")
	}
	if isNotFound(nil) || isNotFound(errors.New("boom")) {
		t.Fatal("isNotFound should reject nil and generic errors")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return raw
}
