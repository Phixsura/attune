// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	defaultAPIBaseURL    = "https://slack.com/api"
	maxConversationPages = 100
)

var slackAPIBaseURL atomic.Value

func init() {
	slackAPIBaseURL.Store(defaultAPIBaseURL)
}

type apiClient interface {
	AuthTest(ctx context.Context) (slackAuthInfo, error)
	DiscoverChannels(ctx context.Context) ([]slackChannel, error)
	History(ctx context.Context, channelID string, oldestMicros int64, limit int) ([]slackMessage, error)
	Replies(ctx context.Context, channelID, threadTS string, oldestMicros int64, limit int) ([]slackMessage, error)
}

type clientFactory func(token string) apiClient

var newAPIClient clientFactory = func(token string) apiClient {
	return newClient(token, currentAPIBaseURL())
}

// SetAPIBaseURL points the Slack client at a different API origin.
// Empty resets to the public Slack API.
func SetAPIBaseURL(baseURL string) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = defaultAPIBaseURL
	}
	slackAPIBaseURL.Store(trimmed)
}

func currentAPIBaseURL() string {
	if v := slackAPIBaseURL.Load(); v != nil {
		if baseURL, ok := v.(string); ok && baseURL != "" {
			return baseURL
		}
	}
	return defaultAPIBaseURL
}

type client struct {
	baseURL string
	token   string
	http    *http.Client
}

func newClient(token, baseURL string) *client {
	return ptrext.Of(client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http: ptrext.Of(http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		}),
	})
}

func (c *client) AuthTest(ctx context.Context) (slackAuthInfo, error) {
	type response struct {
		OK           bool   `json:"ok"`
		Error        string `json:"error"`
		TeamID       string `json:"team_id"`
		Team         string `json:"team"`
		URL          string `json:"url"`
		BotID        string `json:"bot_id"`
		User         string `json:"user"`
		EnterpriseID string `json:"enterprise_id"`
	}
	var resp response
	if err := c.decodeJSON(ctx, http.MethodPost, "auth.test", nil, &resp); err != nil {
		return slackAuthInfo{}, err
	}
	if !resp.OK {
		return slackAuthInfo{}, apiError{method: "auth.test", code: resp.Error}
	}
	return slackAuthInfo{
		TeamID:       strings.TrimSpace(resp.TeamID),
		TeamName:     strings.TrimSpace(resp.Team),
		WorkspaceURL: strings.TrimRight(strings.TrimSpace(resp.URL), "/"),
	}, nil
}

func (c *client) DiscoverChannels(ctx context.Context) ([]slackChannel, error) {
	type response struct {
		OK               bool           `json:"ok"`
		Error            string         `json:"error"`
		Channels         []slackChannel `json:"channels"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	var out []slackChannel
	cursor := ""
	for {
		q := url.Values{}
		q.Set("types", "public_channel,private_channel")
		q.Set("exclude_archived", "true")
		q.Set("limit", "200")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp response
		if err := c.decodeJSON(ctx, http.MethodGet, "conversations.list", q, &resp); err != nil {
			return nil, err
		}
		if !resp.OK {
			return nil, apiError{method: "conversations.list", code: resp.Error}
		}
		for _, ch := range resp.Channels {
			ch.ID = strings.TrimSpace(ch.ID)
			ch.Name = strings.TrimSpace(ch.Name)
			if ch.ID == "" || ch.Name == "" || ch.IsArchived {
				continue
			}
			out = append(out, ch)
		}
		cursor = strings.TrimSpace(resp.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (c *client) History(ctx context.Context, channelID string, oldestMicros int64, limit int) ([]slackMessage, error) {
	return c.conversationMessages(ctx, "conversations.history", func(q url.Values) {
		q.Set("channel", strings.TrimSpace(channelID))
		q.Set("inclusive", "false")
		if oldestMicros > 0 {
			q.Set("oldest", slackTimestampFromMicros(oldestMicros))
		}
		if limit <= 0 {
			limit = 15
		}
		q.Set("limit", strconv.Itoa(limit))
	})
}

func (c *client) Replies(ctx context.Context, channelID, threadTS string, oldestMicros int64, limit int) ([]slackMessage, error) {
	return c.conversationMessages(ctx, "conversations.replies", func(q url.Values) {
		q.Set("channel", strings.TrimSpace(channelID))
		q.Set("ts", strings.TrimSpace(threadTS))
		q.Set("inclusive", "false")
		if oldestMicros > 0 {
			q.Set("oldest", slackTimestampFromMicros(oldestMicros))
		}
		if limit <= 0 {
			limit = 15
		}
		q.Set("limit", strconv.Itoa(limit))
	})
}

func (c *client) conversationMessages(ctx context.Context, path string, prepare func(q url.Values)) ([]slackMessage, error) {
	type response struct {
		OK               bool           `json:"ok"`
		Error            string         `json:"error"`
		Messages         []slackMessage `json:"messages"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	var out []slackMessage
	cursor := ""
	for page := 0; page < maxConversationPages; page++ {
		q := url.Values{}
		prepare(q)
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp response
		if err := c.decodeJSON(ctx, http.MethodGet, path, q, &resp); err != nil {
			return nil, err
		}
		if !resp.OK {
			return nil, apiError{method: path, code: resp.Error}
		}
		out = append(out, resp.Messages...)
		nextCursor := strings.TrimSpace(resp.ResponseMetadata.NextCursor)
		if nextCursor == "" {
			return out, nil
		}
		if nextCursor == cursor {
			return nil, fmt.Errorf("slack %s pagination stalled", path)
		}
		cursor = nextCursor
	}
	return nil, fmt.Errorf("slack %s pagination exceeded %d pages", path, maxConversationPages)
}

func (c *client) decodeJSON(ctx context.Context, method, path string, q url.Values, target any) error {
	body, status, err := c.do(ctx, method, path, q, nil)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		return apiError{method: path, code: "rate_limited"}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("slack %s decode: %w", path, err)
	}
	return nil
}

func (c *client) do(ctx context.Context, method, path string, q url.Values, body io.Reader) ([]byte, int, error) {
	u, err := url.Parse(c.baseURL + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, 0, fmt.Errorf("slack url parse: %w", err)
	}
	if q != nil {
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return raw, resp.StatusCode, apiError{method: path, status: resp.StatusCode, code: "rate_limited"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, resp.StatusCode, fmt.Errorf("slack %s status=%d body=%s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, resp.StatusCode, nil
}

type apiError struct {
	method string
	status int
	code   string
}

func (e apiError) Error() string {
	if e.code == "" {
		return "slack " + e.method + " failed"
	}
	if e.status > 0 {
		return "slack " + e.method + ": " + e.code + " status=" + strconv.Itoa(e.status)
	}
	return "slack " + e.method + ": " + e.code
}

func (e apiError) Permanent() bool {
	switch e.code {
	case "invalid_auth", "not_authed", "token_revoked", "account_inactive", "missing_scope", "channel_not_found", "not_in_channel", "no_permission", "access_denied":
		return true
	default:
		return false
	}
}

func (c *client) String() string {
	return "slack-client"
}
