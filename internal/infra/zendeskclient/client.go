// SPDX-License-Identifier: Apache-2.0

package zendeskclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	// AuthModeAPIToken uses email + API token (Basic auth).
	AuthModeAPIToken = "api_token"
	// AuthModeOAuth uses OAuth 2.0 (Bearer auth).
	AuthModeOAuth = "oauth"

	maxResponseBytes = 4 << 20 // 4 MiB
)

// Client is the Zendesk API surface consumed by adapters.
type Client interface {
	AuthTest(ctx context.Context) (AccountInfo, error)
	IncrementalTickets(ctx context.Context, cursor string, startTime int64) (TicketPage, error)
	TicketComments(ctx context.Context, ticketID int64) ([]Comment, error)
	ShowUsers(ctx context.Context, ids []int64) ([]User, error)
	ShowOrganizations(ctx context.Context, ids []int64) ([]Organization, error)
	RefreshOAuthToken(ctx context.Context, refreshToken, clientID, clientSecret string) (OAuthToken, error)
}

// Credential holds the decrypted runtime secret for a configured auth mode.
type Credential struct {
	Mode         string // AuthModeAPIToken or AuthModeOAuth
	Email        string // admin email for api_token Basic auth
	APIToken     []byte // plaintext API token (api_token mode)
	AccessToken  string // OAuth access token
	RefreshToken string // OAuth refresh token (may be empty)
	ClientID     string // OAuth client ID (for refresh grant)
	ClientSecret string // OAuth client secret (for refresh grant)
}

// ---------------------------------------------------------------------------
// Test seam for API base URL override
// ---------------------------------------------------------------------------

var testBaseOverride atomic.Value

// SetTestBaseURL points the client at a different API origin (tests only).
func SetTestBaseURL(u string) {
	trimmed := strings.TrimRight(strings.TrimSpace(u), "/")
	testBaseOverride.Store(trimmed)
}

func currentTestOverride() string {
	if v := testBaseOverride.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// New creates a Zendesk API client for the given base URL and credential.
func New(baseURL string, cred Credential) Client {
	if override := currentTestOverride(); override != "" {
		baseURL = override
	}
	return ptrext.Of(httpClient{
		base: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		cred: cred,
		http: ptrext.Of(http.Client{
			Transport: GuardedTransport(),
			Timeout:   30 * time.Second,
		}),
	})
}

// BaseURL returns the Zendesk API base URL for a subdomain.
func BaseURL(subdomain string) string {
	return "https://" + strings.TrimSpace(strings.ToLower(subdomain)) + ".zendesk.com"
}

// ValidateHost ensures the target host is *.zendesk.com in production.
// The test-override path (SetTestBaseURL) is exempt.
func ValidateHost(base string) error {
	if currentTestOverride() != "" {
		return nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("zendesk: invalid base URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".zendesk.com") {
		return fmt.Errorf("zendesk: host %q is not a *.zendesk.com domain", host)
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP client implementation
// ---------------------------------------------------------------------------

type httpClient struct {
	base string
	cred Credential
	http *http.Client
}

func (c *httpClient) AuthTest(ctx context.Context) (AccountInfo, error) {
	type meResponse struct {
		User struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
			URL   string `json:"url"`
		} `json:"user"`
	}
	var resp meResponse
	if err := c.getJSON(ctx, "/api/v2/users/me.json", nil, &resp); err != nil { // ptrext:allow json-decode-out-param
		return AccountInfo{}, err
	}
	subdomain := extractSubdomain(c.base)
	return AccountInfo{
		Subdomain: subdomain,
		AccountID: resp.User.ID,
		URL:       c.base,
	}, nil
}

func (c *httpClient) IncrementalTickets(ctx context.Context, cursor string, startTime int64) (TicketPage, error) {
	params := map[string]string{}
	if cursor != "" {
		params["cursor"] = cursor
	} else {
		params["start_time"] = strconv.FormatInt(startTime, 10)
	}
	var resp TicketPage
	if err := c.getJSON(ctx, "/api/v2/incremental/tickets/cursor.json", params, &resp); err != nil { // ptrext:allow json-decode-out-param
		return TicketPage{}, err
	}
	return resp, nil
}

func (c *httpClient) TicketComments(ctx context.Context, ticketID int64) ([]Comment, error) {
	type commentsResponse struct {
		Comments []Comment `json:"comments"`
		Links    struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	path := fmt.Sprintf("/api/v2/tickets/%d/comments.json", ticketID)

	// First page: oldest first, up to 10 comments.
	var firstResp commentsResponse
	if err := c.getJSON(ctx, path, map[string]string{
		"sort_order": "asc",
		"page[size]": "10",
	}, &firstResp); err != nil { // ptrext:allow json-decode-out-param
		return nil, err
	}
	var result []Comment
	for i := range firstResp.Comments {
		if firstResp.Comments[i].Public {
			result = append(result, firstResp.Comments[i])
		}
	}
	if firstResp.Links.Next == "" {
		return result, nil
	}

	// Last page: newest first, up to 5 comments (for recent context).
	var lastResp commentsResponse
	if err := c.getJSON(ctx, path, map[string]string{
		"sort_order": "desc",
		"page[size]": "5",
	}, &lastResp); err != nil { // ptrext:allow json-decode-out-param
		return nil, err
	}
	seen := make(map[int64]bool, len(result))
	for _, cm := range result {
		seen[cm.ID] = true
	}
	var tail []Comment
	for i := len(lastResp.Comments) - 1; i >= 0; i-- {
		cm := lastResp.Comments[i]
		if cm.Public && !seen[cm.ID] {
			tail = append(tail, cm)
		}
	}
	return append(result, tail...), nil
}

func (c *httpClient) ShowUsers(ctx context.Context, ids []int64) ([]User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	type usersResponse struct {
		Users []User `json:"users"`
	}
	var resp usersResponse
	if err := c.getJSON(ctx, "/api/v2/users/show_many.json", map[string]string{
		"ids": joinInt64s(ids),
	}, &resp); err != nil { // ptrext:allow json-decode-out-param
		return nil, err
	}
	return resp.Users, nil
}

func (c *httpClient) ShowOrganizations(ctx context.Context, ids []int64) ([]Organization, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	type orgsResponse struct {
		Organizations []Organization `json:"organizations"`
	}
	var resp orgsResponse
	if err := c.getJSON(ctx, "/api/v2/organizations/show_many.json", map[string]string{
		"ids": joinInt64s(ids),
	}, &resp); err != nil { // ptrext:allow json-decode-out-param
		return nil, err
	}
	return resp.Organizations, nil
}

func (c *httpClient) RefreshOAuthToken(ctx context.Context, refreshToken, clientID, clientSecret string) (OAuthToken, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	var tok OAuthToken
	if err := c.postForm(ctx, "/oauth/tokens", form, &tok); err != nil { // ptrext:allow json-decode-out-param
		return OAuthToken{}, fmt.Errorf("oauth refresh: %w", err)
	}
	if tok.AccessToken == "" {
		return OAuthToken{}, fmt.Errorf("oauth refresh: empty access_token in response")
	}
	return tok, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (c *httpClient) getJSON(ctx context.Context, path string, params map[string]string, target any) error {
	body, status, headers, err := c.do(ctx, http.MethodGet, path, params)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		return RateLimitError{Method: path, RetryAfter: ParseRetryAfter(headers)}
	}
	if status == http.StatusUnauthorized {
		return APIError{Method: path, Status: status, Code: "unauthorized"}
	}
	if status == http.StatusForbidden {
		return APIError{Method: path, Status: status, Code: "forbidden"}
	}
	if status < 200 || status >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return APIError{Method: path, Status: status, Code: msg}
	}
	if err := json.Unmarshal(body, target); err != nil { // ptrext:allow json-unmarshal
		return fmt.Errorf("zendesk %s decode: %w", path, err)
	}
	return nil
}

func (c *httpClient) postForm(ctx context.Context, path string, form url.Values, target any) error {
	u, err := c.buildURL(path, nil)
	if err != nil {
		return fmt.Errorf("zendesk url build: %w", err)
	}
	body := strings.NewReader(form.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return APIError{Method: path, Status: resp.StatusCode, Code: msg}
	}
	if err := json.Unmarshal(raw, target); err != nil { // ptrext:allow json-unmarshal
		return fmt.Errorf("zendesk %s decode: %w", path, err)
	}
	return nil
}

func (c *httpClient) do(ctx context.Context, method, path string, params map[string]string) ([]byte, int, http.Header, error) {
	u, err := c.buildURL(path, params)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("zendesk url build: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	return raw, resp.StatusCode, resp.Header, nil
}

func (c *httpClient) buildURL(path string, params map[string]string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	parsed, err := url.Parse(c.base)
	if err != nil {
		return "", err
	}
	pathPart, queryPart, _ := strings.Cut(path, "?")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(pathPart, "/")
	q := parsed.Query()
	if queryPart != "" {
		inlineQ, _ := url.ParseQuery(queryPart)
		for k, vals := range inlineQ {
			for _, v := range vals {
				q.Set(k, v)
			}
		}
	}
	for k, v := range params {
		q.Set(k, v)
	}
	if len(q) > 0 {
		parsed.RawQuery = q.Encode()
	}
	return parsed.String(), nil
}

func (c *httpClient) setAuth(req *http.Request) {
	switch c.cred.Mode {
	case AuthModeAPIToken:
		tokenVal := string(c.cred.APIToken)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
			[]byte(c.cred.Email+"/token:"+tokenVal),
		))
	case AuthModeOAuth:
		if c.cred.AccessToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.cred.AccessToken)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractSubdomain(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if idx := strings.Index(u, ".zendesk.com"); idx > 0 {
		return u[:idx]
	}
	if idx := strings.Index(u, "."); idx > 0 {
		return u[:idx]
	}
	return u
}

func joinInt64s(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
