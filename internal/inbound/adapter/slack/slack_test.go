// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestValidateConnConfig(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.SlackConnConfig{BotToken: "  xoxb-123  ", ChannelId: "  C123  "})
	inputs, err := ValidateConnConfig(cfg, true)
	require.NoError(t, err)
	require.Equal(t, "xoxb-123", inputs.BotToken)
	require.Equal(t, "C123", inputs.ChannelID)

	_, err = ValidateConnConfig(ptrext.Of(attunev1.SlackConnConfig{BotToken: "xoxb"}), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel_id")
}

func TestSlackHelpers(t *testing.T) {
	t.Parallel()
	require.Equal(t, "1700000000.123456", slackTimestampFromMicros(1700000000123456))
	micros, err := slackTimestampMicros("1700000000.123456")
	require.NoError(t, err)
	require.Equal(t, int64(1700000000123456), micros)
	require.Equal(t, "https://acme.slack.com/archives/C123/p1700000000123456", messagePermalink("https://acme.slack.com/", "C123", "1700000000.123456", "1700000000.123456"))
	require.Equal(t, "https://acme.slack.com/archives/C123/p1700000000456789?thread_ts=1700000000.123456&cid=C123", messagePermalink("https://acme.slack.com/", "C123", "1700000000.123456", "1700000000.456789"))
	require.Equal(t, "hello <world>", normalizeMessageText("  hello &lt;world&gt;  "))
	require.Equal(t, "slack_T123_C123_1700000000123456", slackIdempotencyKey("T123", "C123", "1700000000.123456"))
	require.True(t, isPermanentSlackError(errors.New("slack auth.test: invalid_auth")))
	require.True(t, isSlackDuplicateError(errors.New("idempotency key used with different request")))
}

func TestDiscoverValidateAndHistory(t *testing.T) {
	oldFactory := newAPIClient
	t.Cleanup(func() { newAPIClient = oldFactory })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"team_id": "T123",
				"team":    "Acme",
				"url":     "https://acme.slack.com/",
				"user":    "bot",
				"user_id": "U123",
				"bot_id":  "B123",
			})
		case "/conversations.list":
			require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
			q := r.URL.Query()
			require.Equal(t, "public_channel,private_channel", q.Get("types"))
			require.Equal(t, "true", q.Get("exclude_archived"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"channels": []map[string]any{
					{"id": "C2", "name": "beta", "is_private": true, "is_archived": false, "is_shared": false},
					{"id": "C1", "name": "alpha", "is_private": false, "is_archived": false, "is_shared": true},
				},
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		case "/conversations.history":
			require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
			q := r.URL.Query()
			require.Equal(t, "C1", q.Get("channel"))
			require.Equal(t, "false", q.Get("inclusive"))
			require.Equal(t, "123.000000", q.Get("oldest"))
			require.Equal(t, "15", q.Get("limit"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{
						"type":        "message",
						"user":        "U1",
						"text":        "hello",
						"ts":          "1700000000.000100",
						"bot_id":      "",
						"subtype":     "",
						"reply_count": 0,
					},
				},
			})
		case "/conversations.replies":
			require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
			q := r.URL.Query()
			require.Equal(t, "C1", q.Get("channel"))
			require.Equal(t, "1700000000.000100", q.Get("ts"))
			require.Equal(t, "false", q.Get("inclusive"))
			require.Equal(t, "1700000000.000100", q.Get("oldest"))
			require.Equal(t, "15", q.Get("limit"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{
						"type":      "message",
						"user":      "U2",
						"text":      "reply",
						"ts":        "1700000000.000200",
						"thread_ts": "1700000000.000100",
						"bot_id":    "",
						"subtype":   "",
					},
				},
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	newAPIClient = func(token string) apiClient {
		return newClient(token, server.URL)
	}

	auth, channels, err := Discover(context.Background(), "xoxb-test")
	require.NoError(t, err)
	require.Equal(t, "T123", auth.TeamID)
	require.Len(t, channels, 2)
	require.Equal(t, "alpha", channels[0].Name)
	require.Equal(t, "beta", channels[1].Name)

	auth, channel, err := ValidateChannel(context.Background(), "xoxb-test", "C1")
	require.NoError(t, err)
	require.Equal(t, "Acme", auth.TeamName)
	require.Equal(t, "C1", channel.ID)

	history, err := newClient("xoxb-test", server.URL).History(context.Background(), "C1", 123000000, 15)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "hello", strings.TrimSpace(history[0].Text))

	replies, err := newClient("xoxb-test", server.URL).Replies(context.Background(), "C1", "1700000000.000100", 1700000000000100, 15)
	require.NoError(t, err)
	require.Len(t, replies, 1)
	require.Equal(t, "reply", strings.TrimSpace(replies[0].Text))
}

func TestDiscoverUsesConfiguredAPIBaseURL(t *testing.T) {
	t.Cleanup(func() { SetAPIBaseURL(defaultAPIBaseURL) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"team_id": "T999",
				"team":    "Mock Slack",
				"url":     "http://mock.slack.local",
			})
		case "/conversations.list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"channels": []map[string]any{
					{"id": "C999", "name": "feedback", "is_private": false, "is_archived": false, "is_shared": false},
				},
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	SetAPIBaseURL(server.URL)

	auth, channels, err := Discover(context.Background(), "xoxb-mock")
	require.NoError(t, err)
	require.Equal(t, "T999", auth.TeamID)
	require.Len(t, channels, 1)
	require.Equal(t, "feedback", channels[0].Name)
}

func TestClientPaginatesHistoryAndReplies(t *testing.T) {
	t.Parallel()

	type call struct {
		Cursor string
		Limit  string
		Oldest string
		TS     string
	}

	var mu sync.Mutex
	var historyCalls []call
	var replyCalls []call

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		record := func(dst []call) []call {
			mu.Lock()
			dst = append(dst, call{
				Cursor: q.Get("cursor"),
				Limit:  q.Get("limit"),
				Oldest: q.Get("oldest"),
				TS:     q.Get("ts"),
			})
			mu.Unlock()
			return dst
		}

		encode := func(messages []map[string]any, nextCursor string) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":       true,
				"messages": messages,
				"response_metadata": map[string]any{
					"next_cursor": nextCursor,
				},
			})
		}

		switch r.URL.Path {
		case "/conversations.history":
			historyCalls = record(historyCalls)
			switch q.Get("cursor") {
			case "":
				encode([]map[string]any{
					{"type": "message", "text": "history-1", "ts": "1700000000.000100"},
				}, "history-page-2")
			case "history-page-2":
				encode([]map[string]any{
					{"type": "message", "text": "history-2", "ts": "1700000000.000200"},
				}, "")
			default:
				http.Error(w, "unexpected history cursor", http.StatusBadRequest)
			}
		case "/conversations.replies":
			replyCalls = record(replyCalls)
			switch q.Get("cursor") {
			case "":
				encode([]map[string]any{
					{"type": "message", "text": "root", "ts": "1700000000.000300", "thread_ts": "1700000000.000300"},
					{"type": "message", "text": "reply-1", "ts": "1700000000.000400", "thread_ts": "1700000000.000300"},
				}, "reply-page-2")
			case "reply-page-2":
				encode([]map[string]any{
					{"type": "message", "text": "reply-2", "ts": "1700000000.000500", "thread_ts": "1700000000.000300"},
				}, "")
			default:
				http.Error(w, "unexpected replies cursor", http.StatusBadRequest)
			}
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client := newClient("xoxb-test", server.URL)

	history, err := client.History(context.Background(), "C1", 123000000, 15)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "history-1", strings.TrimSpace(history[0].Text))
	require.Equal(t, "history-2", strings.TrimSpace(history[1].Text))

	replies, err := client.Replies(context.Background(), "C1", "1700000000.000300", 1700000000000300, 15)
	require.NoError(t, err)
	require.Len(t, replies, 3)
	require.Equal(t, "root", strings.TrimSpace(replies[0].Text))
	require.Equal(t, "reply-1", strings.TrimSpace(replies[1].Text))
	require.Equal(t, "reply-2", strings.TrimSpace(replies[2].Text))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, historyCalls, 2)
	require.Equal(t, []call{
		{Cursor: "", Limit: "15", Oldest: "123.000000"},
		{Cursor: "history-page-2", Limit: "15", Oldest: "123.000000"},
	}, historyCalls)
	require.Len(t, replyCalls, 2)
	require.Equal(t, []call{
		{Cursor: "", Limit: "15", Oldest: "1700000000.000300", TS: "1700000000.000300"},
		{Cursor: "reply-page-2", Limit: "15", Oldest: "1700000000.000300", TS: "1700000000.000300"},
	}, replyCalls)
}

func TestPollSourceHydratesThreadRepliesAndPersistsCache(t *testing.T) {
	oldNow := nowFn
	t.Cleanup(func() { nowFn = oldNow })
	nowFn = func() time.Time { return time.Unix(1700000000, 0) }

	secrets := inboundtest.FakeSecrets{}
	encConfig := encryptedSlackConfig(t, secrets, Config{
		Version:        ConfigVersion,
		TokenEncrypted: mustEncryptSlackToken(t, secrets, "xoxb-test-token"),
		TeamID:         "T123",
		TeamName:       "Acme",
		WorkspaceURL:   "https://acme.slack.com/",
		ChannelID:      "C123",
		ChannelName:    "feedback",
	})
	source := inbound.Source{
		ID:       "source-1",
		TenantID: "tenant-1",
		Channel:  ChannelName,
		Name:     "Slack Feedback",
		Slug:     "slack-feedback",
		Config:   encConfig,
		Enabled:  true,
		State: inbound.SourceState{
			LastUID: slackTimestampMicrosOrZero("1700000000.000100"),
		},
		CreatedAt: time.Unix(1700000000, 0),
		UpdatedAt: time.Unix(1700000000, 0),
	}

	store := ptrext.Of(slackSourceStore{FakeSources: inboundtest.NewFakeSources(), tenantSlug: "tenant-x"})
	store.Put("tenant-x", source)

	ingest := ptrext.Of(inboundtest.FakeIngest{})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	client := ptrext.Of(slackThreadClient{
		auth: slackAuthInfo{
			TeamID:       "T123",
			TeamName:     "Acme",
			WorkspaceURL: "https://acme.slack.com/",
		},
		history: []slackMessage{
			{
				Type:        "message",
				User:        "U1",
				Text:        "root message",
				Ts:          "1700000000.000100",
				ReplyCount:  1,
				LatestReply: "1700000000.000200",
			},
		},
		replies: map[string][]slackMessage{
			"1700000000.000100": {
				{
					Type:     "message",
					User:     "U2",
					Text:     "reply message",
					Ts:       "1700000000.000200",
					ThreadTS: "1700000000.000100",
					Subtype:  "",
					BotID:    "",
				},
			},
		},
	})
	a := ptrext.Of(adapter{
		deps: inbound.Deps{
			Ingest:  ingest,
			Sources: store,
			Secrets: secrets,
			Metrics: metrics,
		},
		newClient: func(string) apiClient {
			return client
		},
	})

	a.pollSource(context.Background(), source)

	require.Len(t, ingest.Calls, 2)
	require.Equal(t, "root message", strings.TrimSpace(ingest.Calls[0].In.Content))
	require.Equal(t, "reply message", strings.TrimSpace(ingest.Calls[1].In.Content))
	require.Equal(t, "https://acme.slack.com/archives/C123/p1700000000000200?thread_ts=1700000000.000100&cid=C123", ingest.Calls[1].In.PageURL)
	require.Len(t, client.repliesCalls, 1)
	require.Equal(t, "C123", client.repliesCalls[0].ChannelID)
	require.Equal(t, "1700000000.000100", client.repliesCalls[0].ThreadTS)
	require.Equal(t, slackTimestampMicrosOrZero("1700000000.000100"), client.repliesCalls[0].OldestMicros)
	require.Equal(t, 15, client.repliesCalls[0].Limit)
	require.Greater(t, store.updateConfigCalls, 0)
	require.NotEmpty(t, store.lastConfig)

	decoded, err := secrets.Decrypt(store.lastConfig)
	require.NoError(t, err)
	var stored Config
	require.NoError(t, json.Unmarshal(decoded, &stored))
	require.Len(t, stored.ThreadCache, 1)
	require.Equal(t, "1700000000.000100", stored.ThreadCache[0].RootTS)
	require.Equal(t, "1700000000.000200", stored.ThreadCache[0].LatestReplyTS)
	require.Greater(t, stored.ThreadCache[0].LastHydratedAtMicros, int64(0))
}

func TestPollSourceRefreshesHeartbeatWithoutWatermarkAdvance(t *testing.T) {
	oldNow := nowFn
	t.Cleanup(func() { nowFn = oldNow })
	nowFn = func() time.Time { return time.Unix(1700000000, 0) }

	secrets := inboundtest.FakeSecrets{}
	rootTS := "1700000000.000100"
	encConfig := encryptedSlackConfig(t, secrets, Config{
		Version:        ConfigVersion,
		TokenEncrypted: mustEncryptSlackToken(t, secrets, "xoxb-test-token"),
		TeamID:         "T123",
		TeamName:       "Acme",
		WorkspaceURL:   "https://acme.slack.com/",
		ChannelID:      "C123",
		ChannelName:    "feedback",
	})
	source := inbound.Source{
		ID:       "source-1",
		TenantID: "tenant-1",
		Channel:  ChannelName,
		Name:     "Slack Feedback",
		Slug:     "slack-feedback",
		Config:   encConfig,
		Enabled:  true,
		State: inbound.SourceState{
			LastUID: slackTimestampMicrosOrZero(rootTS),
		},
		CreatedAt: time.Unix(1700000000, 0),
		UpdatedAt: time.Unix(1700000000, 0),
	}

	store := ptrext.Of(slackSourceStore{FakeSources: inboundtest.NewFakeSources(), tenantSlug: "tenant-x"})
	store.Put("tenant-x", source)

	ingest := ptrext.Of(inboundtest.FakeIngest{})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	client := ptrext.Of(slackThreadClient{
		auth: slackAuthInfo{
			TeamID:       "T123",
			TeamName:     "Acme",
			WorkspaceURL: "https://acme.slack.com/",
		},
		history: []slackMessage{
			{
				Type:        "message",
				User:        "U1",
				Text:        "root message",
				Ts:          rootTS,
				ReplyCount:  1,
				LatestReply: "1700000000.000200",
			},
		},
		replies: map[string][]slackMessage{
			rootTS: {
				{
					Type:     "message",
					User:     "U2",
					Text:     "reply message",
					Ts:       "1700000000.000200",
					ThreadTS: rootTS,
					Subtype:  "",
					BotID:    "",
				},
			},
		},
	})
	a := ptrext.Of(adapter{
		deps: inbound.Deps{
			Ingest:  ingest,
			Sources: store,
			Secrets: secrets,
			Metrics: metrics,
		},
		newClient: func(string) apiClient {
			return client
		},
	})

	a.pollSource(context.Background(), source)

	updated, err := store.Get(context.Background(), source.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.State.LastEventAt)
	require.True(t, updated.State.LastEventAt.Equal(time.Unix(1700000000, 0)))
	require.Equal(t, slackTimestampMicrosOrZero(rootTS), updated.State.LastUID)
	require.Len(t, ingest.Calls, 2)
	require.Len(t, client.repliesCalls, 1)
}

func TestPollSourcePreservesErrorWhenThreadHydrationFails(t *testing.T) {
	oldNow := nowFn
	t.Cleanup(func() { nowFn = oldNow })
	nowFn = func() time.Time { return time.Unix(1700000000, 0) }

	secrets := inboundtest.FakeSecrets{}
	rootTS := "1700000000.000100"
	encConfig := encryptedSlackConfig(t, secrets, Config{
		Version:        ConfigVersion,
		TokenEncrypted: mustEncryptSlackToken(t, secrets, "xoxb-test-token"),
		TeamID:         "T123",
		TeamName:       "Acme",
		WorkspaceURL:   "https://acme.slack.com/",
		ChannelID:      "C123",
		ChannelName:    "feedback",
	})
	source := inbound.Source{
		ID:       "source-1",
		TenantID: "tenant-1",
		Channel:  ChannelName,
		Name:     "Slack Feedback",
		Slug:     "slack-feedback",
		Config:   encConfig,
		Enabled:  true,
		State: inbound.SourceState{
			LastUID: slackTimestampMicrosOrZero(rootTS),
		},
		CreatedAt: time.Unix(1700000000, 0),
		UpdatedAt: time.Unix(1700000000, 0),
	}

	store := ptrext.Of(slackSourceStore{FakeSources: inboundtest.NewFakeSources(), tenantSlug: "tenant-x"})
	store.Put("tenant-x", source)

	ingest := ptrext.Of(inboundtest.FakeIngest{})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	client := ptrext.Of(slackThreadClient{
		auth: slackAuthInfo{
			TeamID:       "T123",
			TeamName:     "Acme",
			WorkspaceURL: "https://acme.slack.com/",
		},
		history: []slackMessage{
			{
				Type:        "message",
				User:        "U1",
				Text:        "root message",
				Ts:          rootTS,
				ReplyCount:  1,
				LatestReply: "1700000000.000200",
			},
		},
		repliesErr: errors.New("temporary slack error"),
	})
	a := ptrext.Of(adapter{
		deps: inbound.Deps{
			Ingest:  ingest,
			Sources: store,
			Secrets: secrets,
			Metrics: metrics,
		},
		newClient: func(string) apiClient {
			return client
		},
	})

	a.pollSource(context.Background(), source)

	updated, err := store.Get(context.Background(), source.ID)
	require.NoError(t, err)
	require.Nil(t, updated.State.LastEventAt)
	require.Equal(t, slackTimestampMicrosOrZero("1700000000.000100"), updated.State.LastUID)
	require.Equal(t, "thread hydrate: transient", updated.State.LastError)
	require.Len(t, ingest.Calls, 1)
	require.Len(t, client.repliesCalls, 1)
}

type slackThreadClient struct {
	auth         slackAuthInfo
	history      []slackMessage
	replies      map[string][]slackMessage
	repliesErr   error
	repliesCalls []slackThreadReplyCall
}

type slackThreadReplyCall struct {
	ChannelID    string
	ThreadTS     string
	OldestMicros int64
	Limit        int
}

func (c *slackThreadClient) AuthTest(context.Context) (slackAuthInfo, error) {
	return c.auth, nil
}

func (c *slackThreadClient) DiscoverChannels(context.Context) ([]slackChannel, error) {
	return nil, nil
}

func (c *slackThreadClient) History(context.Context, string, int64, int) ([]slackMessage, error) {
	return append([]slackMessage(nil), c.history...), nil
}

func (c *slackThreadClient) Replies(_ context.Context, channelID, threadTS string, oldestMicros int64, limit int) ([]slackMessage, error) {
	c.repliesCalls = append(c.repliesCalls, slackThreadReplyCall{
		ChannelID:    channelID,
		ThreadTS:     threadTS,
		OldestMicros: oldestMicros,
		Limit:        limit,
	})
	if c.repliesErr != nil {
		return nil, c.repliesErr
	}
	return append([]slackMessage(nil), c.replies[threadTS]...), nil
}

type slackSourceStore struct {
	*inboundtest.FakeSources
	tenantSlug        string
	updateConfigCalls int
	lastConfig        []byte
}

func (s *slackSourceStore) UpdateConfig(_ context.Context, id string, config []byte) error {
	src, err := s.Get(context.Background(), id)
	if err != nil {
		return err
	}
	src.Config = append([]byte(nil), config...)
	s.Put(s.tenantSlug, src)
	s.lastConfig = append([]byte(nil), config...)
	s.updateConfigCalls++
	return nil
}

func encryptedSlackConfig(t *testing.T, secrets inboundtest.FakeSecrets, cfg Config) []byte {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)
	return enc
}

func mustEncryptSlackToken(t *testing.T, secrets inboundtest.FakeSecrets, token string) []byte {
	t.Helper()
	enc, err := secrets.Encrypt([]byte(token))
	require.NoError(t, err)
	return enc
}
