// SPDX-License-Identifier: Apache-2.0

package intercomclient

import (
	"bytes"
	"context"
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
	// apiVersion is pinned on every request so behavior does not drift
	// when the workspace's default API version changes.
	apiVersion = "2.16"

	maxResponseBytes = 4 << 20 // 4 MiB

	// searchPageSize is the max page size for search APIs.
	searchPageSize = 150

	// contactChunkSize bounds contact IN-batch resolution. Search filter
	// groups allow 15 filters; a single IN array stays one filter, and 25
	// keeps request bodies small.
	contactChunkSize = 25
)

// Region tokens accepted in configs. Each maps to a dedicated API host —
// EU/AU workspaces fail cross-region calls.
const (
	RegionUS = "us"
	RegionEU = "eu"
	RegionAU = "au"
)

// Client is the Intercom API surface consumed by adapters.
type Client interface {
	// AuthTest validates the token and returns workspace info (GET /me).
	AuthTest(ctx context.Context) (AccountInfo, error)
	// SearchConversations returns one page of conversations with
	// updated_at > startTime AND updated_at < endTime, sorted by
	// updated_at ascending. startingAfter continues pagination.
	SearchConversations(ctx context.Context, startTime, endTime int64, startingAfter string) (ConversationPage, error)
	// GetConversation fetches the full thread with plain-text parts.
	GetConversation(ctx context.Context, id string) (Conversation, error)
	// SearchContacts batch-resolves contact IDs.
	SearchContacts(ctx context.Context, ids []string) ([]Contact, error)
	// RateBudget returns the most recent X-RateLimit-Remaining value seen
	// on any response, or -1 before the first response. Callers use it to
	// self-throttle before Intercom starts returning 429s — private apps
	// share one 10k req/min budget per app across the whole workspace.
	RateBudget() int64
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

// New creates an Intercom API client for the given region and access token.
func New(region, accessToken string) Client {
	base := BaseURL(region)
	if override := currentTestOverride(); override != "" {
		base = override
	}
	c := ptrext.Of(httpClient{
		base:   base,
		token:  accessToken,
		region: normalizeRegion(region),
		http: ptrext.Of(http.Client{
			Transport: GuardedTransport(),
			Timeout:   30 * time.Second,
		}),
	})
	c.rateBudget.Store(-1)
	return c
}

// BaseURL returns the Intercom API base URL for a region token.
func BaseURL(region string) string {
	switch normalizeRegion(region) {
	case RegionEU:
		return "https://api.eu.intercom.io"
	case RegionAU:
		return "https://api.au.intercom.io"
	default:
		return "https://api.intercom.io"
	}
}

// ValidRegion reports whether the region token is one of us/eu/au.
func ValidRegion(region string) bool {
	switch normalizeRegion(region) {
	case RegionUS, RegionEU, RegionAU:
		return true
	default:
		return false
	}
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}

// ValidateHost ensures the target host is *.intercom.io in production.
// The test-override path (SetTestBaseURL) is exempt.
func ValidateHost(base string) error {
	if currentTestOverride() != "" {
		return nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("intercom: invalid base URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "api.intercom.io" && !strings.HasSuffix(host, ".intercom.io") {
		return fmt.Errorf("intercom: host %q is not an *.intercom.io domain", host)
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP client implementation
// ---------------------------------------------------------------------------

type httpClient struct {
	base   string
	token  string
	region string
	http   *http.Client
	// rateBudget is the last-seen X-RateLimit-Remaining (-1 = unseen).
	rateBudget atomic.Int64
}

// RateBudget reports the most recent X-RateLimit-Remaining seen.
func (c *httpClient) RateBudget() int64 { return c.rateBudget.Load() }

func (c *httpClient) AuthTest(ctx context.Context) (AccountInfo, error) {
	type meResponse struct {
		Email string `json:"email"`
		App   struct {
			IDCode string `json:"id_code"`
			Name   string `json:"name"`
			Region string `json:"region"`
		} `json:"app"`
	}
	var resp meResponse
	if err := c.getJSON(ctx, "/me", &resp); err != nil { // ptrext:allow json-decode-out-param
		return AccountInfo{}, err
	}
	region := c.region
	if resp.App.Region != "" {
		region = strings.ToLower(resp.App.Region)
	}
	return AccountInfo{
		WorkspaceID:   resp.App.IDCode,
		WorkspaceName: resp.App.Name,
		Region:        region,
		AdminEmail:    resp.Email,
	}, nil
}

// searchBody is the request shape for POST /conversations/search and
// POST /contacts/search.
type searchBody struct {
	Query      searchQuery       `json:"query"`
	Pagination *searchPagination `json:"pagination,omitempty"`
	Sort       *searchSort       `json:"sort,omitempty"`
}

type searchQuery struct {
	Operator string         `json:"operator"`
	Value    []searchFilter `json:"value"`
}

type searchFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type searchPagination struct {
	PerPage       int    `json:"per_page"`
	StartingAfter string `json:"starting_after,omitempty"`
}

type searchSort struct {
	Field string `json:"field"`
	Order string `json:"order"`
}

// pagesEnvelope decodes the cursor pagination block on search responses.
type pagesEnvelope struct {
	Next struct {
		StartingAfter string `json:"starting_after"`
	} `json:"next"`
}

func (c *httpClient) SearchConversations(ctx context.Context, startTime, endTime int64, startingAfter string) (ConversationPage, error) {
	body := searchBody{
		Query: searchQuery{
			Operator: "AND",
			Value: []searchFilter{
				{Field: "updated_at", Operator: ">", Value: startTime},
				{Field: "updated_at", Operator: "<", Value: endTime},
			},
		},
		Pagination: ptrext.Of(searchPagination{
			PerPage:       searchPageSize,
			StartingAfter: startingAfter,
		}),
		Sort: ptrext.Of(searchSort{Field: "updated_at", Order: "ascending"}),
	}
	type searchResponse struct {
		Conversations []Conversation `json:"conversations"`
		TotalCount    int64          `json:"total_count"`
		Pages         pagesEnvelope  `json:"pages"`
	}
	var resp searchResponse
	if err := c.postJSON(ctx, "/conversations/search", body, &resp); err != nil { // ptrext:allow json-decode-out-param
		return ConversationPage{}, err
	}
	return ConversationPage{
		Conversations: resp.Conversations,
		TotalCount:    resp.TotalCount,
		StartingAfter: resp.Pages.Next.StartingAfter,
	}, nil
}

func (c *httpClient) GetConversation(ctx context.Context, id string) (Conversation, error) {
	var conv Conversation
	path := "/conversations/" + url.PathEscape(id) + "?display_as=plaintext"
	if err := c.getJSON(ctx, path, &conv); err != nil { // ptrext:allow json-decode-out-param
		return Conversation{}, err
	}
	return conv, nil
}

func (c *httpClient) SearchContacts(ctx context.Context, ids []string) ([]Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result []Contact
	for start := 0; start < len(ids); start += contactChunkSize {
		end := start + contactChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		body := searchBody{
			Query: searchQuery{
				Operator: "AND",
				Value: []searchFilter{
					{Field: "id", Operator: "IN", Value: chunk},
				},
			},
			Pagination: ptrext.Of(searchPagination{PerPage: contactChunkSize}),
		}
		type contactsResponse struct {
			Data []Contact `json:"data"`
		}
		var resp contactsResponse
		if err := c.postJSON(ctx, "/contacts/search", body, &resp); err != nil { // ptrext:allow json-decode-out-param
			return result, err
		}
		result = append(result, resp.Data...)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (c *httpClient) getJSON(ctx context.Context, path string, target any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, target)
}

func (c *httpClient) postJSON(ctx context.Context, path string, body any, target any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("intercom %s encode: %w", path, err)
	}
	return c.doJSON(ctx, http.MethodPost, path, raw, target)
}

func (c *httpClient) doJSON(ctx context.Context, method, path string, body []byte, target any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Intercom-Version", apiVersion)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		if remaining, perr := strconv.ParseInt(v, 10, 64); perr == nil {
			c.rateBudget.Store(remaining)
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return RateLimitError{Method: path, RetryAfter: parseRateLimitReset(resp.Header)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return APIError{Method: path, Status: resp.StatusCode, Code: extractErrorCode(raw)}
	}
	if err := json.Unmarshal(raw, target); err != nil { // ptrext:allow json-unmarshal
		return fmt.Errorf("intercom %s decode: %w", path, err)
	}
	return nil
}

// extractErrorCode pulls the first error code from Intercom's error.list
// envelope, falling back to a truncated raw body.
func extractErrorCode(raw []byte) string {
	var env struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Errors) > 0 && env.Errors[0].Code != "" { // ptrext:allow json-unmarshal
		return env.Errors[0].Code
	}
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}
