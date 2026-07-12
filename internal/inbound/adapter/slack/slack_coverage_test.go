// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestSlackHelperBranches(t *testing.T) {
	t.Parallel()

	ch, ok := findChannel([]slackChannel{{ID: "C123", Name: "feedback"}}, "c123")
	require.True(t, ok)
	require.Equal(t, "C123", ch.ID)

	_, ok = findChannel([]slackChannel{{ID: "C999", Name: "other"}}, "missing")
	require.False(t, ok)

	id, kind := messageAuthor(slackMessage{User: " U1 "})
	require.Equal(t, "U1", id)
	require.Equal(t, "user", kind)

	id, kind = messageAuthor(slackMessage{BotID: " B1 "})
	require.Equal(t, "B1", id)
	require.Equal(t, "bot", kind)

	id, kind = messageAuthor(slackMessage{})
	require.Empty(t, id)
	require.Equal(t, "unknown", kind)

	require.Equal(t, "https://example.slack.com/archives/C123/p1700000000123456", messagePermalink("https://example.slack.com/", "C123", "1700000000.123456", "1700000000.123456"))
	require.Equal(t, "https://example.slack.com/archives/C123/p1700000000456789?thread_ts=1700000000.123456&cid=C123", messagePermalink("https://example.slack.com/", "C123", "1700000000.123456", "1700000000.456789"))
	require.Empty(t, messagePermalink("", "C123", "1700000000.123456", "1700000000.123456"))

	require.Equal(t, "1700000000123000", slackTimestampSlug("1700000000.123"))
	require.Equal(t, "1700000000123456", slackTimestampSlug("1700000000.1234567"))
	require.Equal(t, "1700000000000000", slackTimestampSlug("1700000000"))

	micros, err := slackTimestampMicros("1700000000")
	require.NoError(t, err)
	require.Equal(t, int64(1700000000000000), micros)
	_, err = slackTimestampMicros("")
	require.Error(t, err)

	require.Equal(t, int64(1700000000123456), messageMicros(slackMessage{Ts: "1700000000.123456"}))
	require.Equal(t, int64(0), messageMicros(slackMessage{Ts: "not-a-timestamp"}))

	require.Equal(t, "abc-123", sanitizeKeyPart(" abc-123 "))
	require.Equal(t, "A_B", sanitizeKeyPart("A B!"))
	require.Equal(t, "unknown", sanitizeKeyPart("!!!"))

	require.Equal(t, "slack-client", ptrext.Of(client{}).String())

	require.Equal(t, "slack auth.test failed", apiError{method: "auth.test"}.Error())
	require.Equal(t, "slack auth.test: invalid_auth status=401", apiError{method: "auth.test", status: 401, code: "invalid_auth"}.Error())
	require.Equal(t, "slack auth.test: rate_limited", apiError{method: "auth.test", code: "rate_limited"}.Error())
	require.True(t, apiError{code: "missing_scope"}.Permanent())
	require.False(t, apiError{code: "transient"}.Permanent())

	require.True(t, isPermanentSlackError(apiError{code: "invalid_auth"}))
	require.False(t, isPermanentSlackError(errors.New("temporary network hiccup")))
	require.True(t, isSlackThreadNotFoundError(apiError{code: "thread_not_found"}))
	require.False(t, isSlackThreadNotFoundError(errors.New("temporary")))
}

func TestSlackConfigBranches(t *testing.T) {
	t.Parallel()

	_, err := validateSlackConnConfig(nil, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "slack_config is required")

	_, err = validateSlackConnConfig(ptrext.Of(attunev1.SlackConnConfig{ChannelId: "C123"}), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bot_token")

	inputs, err := validateSlackConnConfig(ptrext.Of(attunev1.SlackConnConfig{BotToken: "  xoxb-test  "}), false)
	require.NoError(t, err)
	require.Equal(t, "xoxb-test", inputs.BotToken)
	require.Empty(t, inputs.ChannelID)

	secrets := inboundtest.FakeSecrets{}
	happyRaw := covMustEncryptedSlackConfig(t, secrets, Config{
		Version:        ConfigVersion,
		TokenEncrypted: covMustEncryptSlackToken(t, secrets, "xoxb-test-token"),
		TeamID:         "T123",
		TeamName:       "Acme",
		WorkspaceURL:   "https://acme.slack.com/",
		ChannelID:      "C123",
		ChannelName:    "feedback",
	})
	cfg, token, err := parseConfig(happyRaw, secrets)
	require.NoError(t, err)
	require.Equal(t, "C123", cfg.ChannelID)
	require.Equal(t, "xoxb-test-token", string(token))

	_, _, err = parseConfig([]byte{0x01}, secrets)
	require.Error(t, err)

	invalidJSON, err := secrets.Encrypt([]byte("{"))
	require.NoError(t, err)
	_, _, err = parseConfig(invalidJSON, secrets)
	require.Error(t, err)

	invalidVersion := covMustEncryptedSlackConfig(t, secrets, Config{
		Version:        2,
		TokenEncrypted: covMustEncryptSlackToken(t, secrets, "xoxb-test-token"),
		ChannelID:      "C123",
	})
	_, _, err = parseConfig(invalidVersion, secrets)
	require.Error(t, err)

	missingChannel := covMustEncryptedSlackConfig(t, secrets, Config{
		Version:        ConfigVersion,
		TokenEncrypted: covMustEncryptSlackToken(t, secrets, "xoxb-test-token"),
	})
	_, _, err = parseConfig(missingChannel, secrets)
	require.Error(t, err)

	missingToken := covMustEncryptedSlackConfig(t, secrets, Config{
		Version:   ConfigVersion,
		ChannelID: "C123",
	})
	_, _, err = parseConfig(missingToken, secrets)
	require.Error(t, err)

	badInnerToken := covMustEncryptedSlackConfig(t, secrets, Config{
		Version:        ConfigVersion,
		TokenEncrypted: []byte{0x01},
		ChannelID:      "C123",
	})
	_, _, err = parseConfig(badInnerToken, secrets)
	require.Error(t, err)

	micros, err := slackTimestampMicros("1700000000.1234567")
	require.NoError(t, err)
	require.Equal(t, int64(1700000000123456), micros)
	_, err = slackTimestampMicros("1700000000.bad")
	require.Error(t, err)
	require.Equal(t, "0.000000", slackTimestampFromMicros(-1))
}

func TestSlackClientBranches(t *testing.T) {
	t.Cleanup(func() {
		SetAPIBaseURL(defaultAPIBaseURL)
		newAPIClient = func(token string) apiClient {
			return newClient(token, currentAPIBaseURL())
		}
	})

	SetAPIBaseURL("  https://slack.example.com/api/  ")
	require.Equal(t, "https://slack.example.com/api", currentAPIBaseURL())
	slackAPIBaseURL.Store("")
	require.Equal(t, defaultAPIBaseURL, currentAPIBaseURL())
	SetAPIBaseURL("")

	badClient := newClient("xoxb-test", "://bad")
	_, _, err := badClient.do(context.Background(), http.MethodGet, "auth.test", nil, nil)
	require.Error(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"team_id": "T123",
				"team":    "Acme",
				"url":     "https://acme.slack.com/",
			})
		case "/conversations.list":
			page := r.URL.Query().Get("cursor")
			if page == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"channels": []map[string]any{
						{"ID": "C2", "Name": "beta", "IsPrivate": true, "IsArchived": false, "IsShared": false},
						{"ID": "", "Name": "skip", "IsPrivate": false, "IsArchived": false, "IsShared": false},
						{"ID": "C1", "Name": "alpha", "IsPrivate": false, "IsArchived": false, "IsShared": true},
						{"ID": "C3", "Name": "archived", "IsPrivate": false, "IsArchived": true, "IsShared": false},
					},
					"response_metadata": map[string]any{"next_cursor": "page-2"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"channels": []map[string]any{
					{"ID": "C4", "Name": "gamma", "IsPrivate": false, "IsArchived": false, "IsShared": false},
				},
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		case "/conversations.history":
			if r.URL.Query().Get("limit") != "15" {
				http.Error(w, "missing default limit", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"type": "message", "text": "history-0", "ts": "1700000000.000050"},
				},
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		case "/conversations.replies":
			if r.URL.Query().Get("limit") != "15" {
				http.Error(w, "missing default limit", http.StatusBadRequest)
				return
			}
			switch r.URL.Query().Get("cursor") {
			case "":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"messages": []map[string]any{
						{"type": "message", "text": "root", "ts": "1700000000.000200", "thread_ts": "1700000000.000200"},
						{"type": "message", "text": "reply-1", "ts": "1700000000.000300", "thread_ts": "1700000000.000200"},
					},
					"response_metadata": map[string]any{"next_cursor": "reply-page-2"},
				})
			case "reply-page-2":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"messages": []map[string]any{
						{"type": "message", "text": "reply-2", "ts": "1700000000.000400", "thread_ts": "1700000000.000200"},
					},
					"response_metadata": map[string]any{"next_cursor": ""},
				})
			default:
				http.Error(w, "unexpected cursor", http.StatusBadRequest)
			}
		case "/rate-limited":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"rate_limited"}`))
		case "/server-error":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/bad-json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{"))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client := newClient("  xoxb-test  ", server.URL)

	auth, err := client.AuthTest(context.Background())
	require.NoError(t, err)
	require.Equal(t, "T123", auth.TeamID)

	channels, err := client.DiscoverChannels(context.Background())
	require.NoError(t, err)
	require.Len(t, channels, 3)
	require.Equal(t, []string{"alpha", "beta", "gamma"}, []string{channels[0].Name, channels[1].Name, channels[2].Name})

	history, err := client.History(context.Background(), "C1", 0, 0)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "history-0", strings.TrimSpace(history[0].Text))

	replies, err := client.Replies(context.Background(), "C1", "1700000000.000200", 0, 0)
	require.NoError(t, err)
	require.Len(t, replies, 3)
	require.Equal(t, "reply-2", strings.TrimSpace(replies[2].Text))

	err = client.decodeJSON(context.Background(), http.MethodGet, "rate-limited", nil, ptrext.Of(struct{}{}))
	require.Error(t, err)

	_, _, err = newClient("xoxb-test", server.URL).do(context.Background(), http.MethodGet, "server-error", nil, nil)
	require.Error(t, err)

	err = newClient("xoxb-test", server.URL).decodeJSON(context.Background(), http.MethodGet, "bad-json", nil, ptrext.Of(struct{}{}))
	require.Error(t, err)

	_, _, err = ValidateChannel(context.Background(), "xoxb-test", "C999")
	require.Error(t, err)
}

func TestSlackPublicWrapperErrors(t *testing.T) {
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid_auth",
			})
		case "/conversations.list":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(failServer.Close)

	SetAPIBaseURL(failServer.URL)
	_, _, err := Discover(context.Background(), "xoxb-test")
	require.Error(t, err)
	require.True(t, isPermanentSlackError(err))

	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"team_id": "T123",
				"team":    "Acme",
				"url":     "https://acme.slack.com/",
			})
		case "/conversations.list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"channels": []map[string]any{
					{"id": "C123", "name": "feedback", "is_private": false, "is_archived": false, "is_shared": false},
				},
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(okServer.Close)

	SetAPIBaseURL(okServer.URL)
	_, _, err = ValidateChannel(context.Background(), "xoxb-test", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "slack_config.channel_id")

	SetAPIBaseURL(defaultAPIBaseURL)
}

func TestSlackPollHelpers(t *testing.T) {
	t.Parallel()

	nowMicros := int64(1700000000000000)
	rootTS := "1700000000.000100"
	cache := slackThreadCache{
		rootTS: {
			RootTS:               rootTS,
			LastSeenAtMicros:     nowMicros,
			ReplyCount:           1,
			LastHydratedAtMicros: nowMicros - int64(slackThreadRefreshInterval/time.Microsecond) + 1,
			LatestReplyTS:        "1700000000.000200",
		},
		"old": {
			RootTS:           "old",
			LastSeenAtMicros: nowMicros,
			ReplyCount:       1,
		},
	}

	require.True(t, (slackThreadCacheEntry{RootTS: "x", LastSeenAtMicros: nowMicros}).shouldRefresh(nowMicros))
	require.False(t, (slackThreadCacheEntry{RootTS: "x", LastSeenAtMicros: nowMicros - int64(slackThreadCacheTTL/time.Microsecond) - 1}).shouldRefresh(nowMicros))

	require.True(t, shouldHydrateSlackThread(slackMessage{Ts: rootTS, ReplyCount: 1}, slackThreadCache{}, nowMicros))
	require.True(t, shouldHydrateSlackThread(slackMessage{Ts: rootTS, ReplyCount: 2}, cache, nowMicros))
	require.True(t, shouldHydrateSlackThread(slackMessage{Ts: rootTS, ReplyCount: 1, LatestReply: "1700000000.000300"}, cache, nowMicros))
	require.False(t, shouldHydrateSlackThread(slackMessage{Ts: rootTS, ReplyCount: 1}, cache, nowMicros))

	replies := []slackMessage{
		{Ts: "1700000000.000100"},
		{Ts: "1700000000.000200"},
		{Ts: "1700000000.000300"},
	}
	require.Equal(t, "1700000000.000300", latestReplyTSFromBatch(replies, "1700000000.000100"))
	require.Empty(t, latestReplyTSFromBatch([]slackMessage{{Ts: "1700000000.000100"}}, "1700000000.000100"))

	state := slackThreadHydrationState{replyCount: 1, replyIngested: 2, latestReplyTS: "old"}
	updatedState, changed := finalizeSlackThreadHydration(replies, "1700000000.000100", state)
	require.True(t, changed)
	require.Equal(t, "1700000000.000300", updatedState.latestReplyTS)
	require.Equal(t, 2, updatedState.replyCount)

	require.False(t, isIngestibleSlackMessage(slackMessage{Type: "event"}))
	require.True(t, isIngestibleSlackMessage(slackMessage{Type: "message"}))
	require.True(t, isIngestibleSlackMessage(slackMessage{Type: "message", Subtype: "bot_message"}))

	mut := []byte("abc")
	wipeBytes(mut)
	require.Equal(t, []byte{0, 0, 0}, mut)
}

func TestSlackAdapterLifecycle(t *testing.T) {
	t.Parallel()

	a := NewAdapter().(*adapter)
	require.Equal(t, ChannelName, a.Channel())
	require.Equal(t, 10*time.Second, a.ShutdownTimeout())
}

func covMustEncryptedSlackConfig(t *testing.T, secrets inboundtest.FakeSecrets, cfg Config) []byte {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)
	return enc
}

func covMustEncryptSlackToken(t *testing.T, secrets inboundtest.FakeSecrets, token string) []byte {
	t.Helper()
	enc, err := secrets.Encrypt([]byte(token))
	require.NoError(t, err)
	return enc
}
