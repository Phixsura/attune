// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type slackRoundTripper func(*http.Request) (*http.Response, error)

func (f slackRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type slackErrBody struct{}

func (slackErrBody) Read([]byte) (int, error) { return 0, errors.New("read boom") }
func (slackErrBody) Close() error             { return nil }

type covSlackFailingSecrets struct {
	encryptErr error
}

func (s covSlackFailingSecrets) Encrypt([]byte) ([]byte, error) {
	return nil, s.encryptErr
}

func (s covSlackFailingSecrets) Decrypt([]byte) ([]byte, error) {
	return nil, errors.New("decrypt boom")
}

type covSlackSecondEncryptSecrets struct {
	calls     int
	secondErr error
}

func (s *covSlackSecondEncryptSecrets) Encrypt(b []byte) ([]byte, error) {
	s.calls++
	if s.calls == 2 {
		return nil, s.secondErr
	}
	return inboundtest.FakeSecrets{}.Encrypt(b)
}

func (s *covSlackSecondEncryptSecrets) Decrypt(b []byte) ([]byte, error) {
	return inboundtest.FakeSecrets{}.Decrypt(b)
}

type pollLoopStore struct {
	*inboundtest.FakeSources
	calls atomic.Int32
}

func (s *pollLoopStore) List(context.Context, string) ([]inbound.Source, error) {
	s.calls.Add(1)
	return nil, nil
}

func TestSlackClientErrorBranches(t *testing.T) {
	t.Cleanup(func() {
		SetAPIBaseURL(defaultAPIBaseURL)
		newAPIClient = func(token string) apiClient {
			return newClient(token, currentAPIBaseURL())
		}
	})

	t.Run("auth_test_decode_error", func(t *testing.T) {
		client := newClient("xoxb-test", "https://slack.example.com")
		client.http = ptrext.Of(http.Client{Transport: slackRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/auth.test" {
				return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
			}
			return ptrext.Of(http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{")),
			}), nil
		})})

		_, err := client.AuthTest(context.Background())
		require.Error(t, err)
	})

	t.Run("discover_channels_decode_error", func(t *testing.T) {
		client := newClient("xoxb-test", "https://slack.example.com")
		client.http = ptrext.Of(http.Client{Transport: slackRoundTripper(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/auth.test":
				return ptrext.Of(http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T123","team":"Acme","url":"https://acme.slack.com/"}`)),
				}), nil
			case "/conversations.list":
				return ptrext.Of(http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("{")),
				}), nil
			default:
				return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
			}
		})})

		_, err := client.DiscoverChannels(context.Background())
		require.Error(t, err)
	})

	t.Run("discover_channels_api_error", func(t *testing.T) {
		client := newClient("xoxb-test", "https://slack.example.com")
		client.http = ptrext.Of(http.Client{Transport: slackRoundTripper(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/auth.test":
				return ptrext.Of(http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T123","team":"Acme","url":"https://acme.slack.com/"}`)),
				}), nil
			case "/conversations.list":
				return ptrext.Of(http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"ok":false,"error":"missing_scope"}`)),
				}), nil
			default:
				return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
			}
		})})

		_, err := client.DiscoverChannels(context.Background())
		require.Error(t, err)
	})

	t.Run("discover_wrapper_error", func(t *testing.T) {
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
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":    false,
					"error": "missing_scope",
				})
			default:
				http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		SetAPIBaseURL(server.URL)
		_, _, err := Discover(context.Background(), "xoxb-test")
		require.Error(t, err)
	})

	t.Run("history_stalled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/conversations.history":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"messages": []map[string]any{
						{"type": "message", "text": "history-0", "ts": "1700000000.000100"},
					},
					"response_metadata": map[string]any{"next_cursor": "page-1"},
				})
			default:
				http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		client := newClient("xoxb-test", server.URL)
		_, err := client.History(context.Background(), "C123", 0, 15)
		require.Error(t, err)
	})

	t.Run("history_exceeded", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/conversations.history":
				call := calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"messages": []map[string]any{
						{"type": "message", "text": "history", "ts": fmt.Sprintf("1700000000.%06d", call)},
					},
					"response_metadata": map[string]any{"next_cursor": fmt.Sprintf("page-%03d", call)},
				})
			default:
				http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		client := newClient("xoxb-test", server.URL)
		_, err := client.History(context.Background(), "C123", 0, 15)
		require.Error(t, err)
	})

	t.Run("do_transport_error_and_read_error", func(t *testing.T) {
		client := newClient("xoxb-test", "https://slack.example.com")
		client.http = ptrext.Of(http.Client{Transport: slackRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport boom")
		})})
		_, _, err := client.do(context.Background(), http.MethodGet, "auth.test", nil, nil)
		require.Error(t, err)

		client.http = ptrext.Of(http.Client{Transport: slackRoundTripper(func(*http.Request) (*http.Response, error) {
			return ptrext.Of(http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       slackErrBody{},
			}), nil
		})})
		_, _, err = client.do(context.Background(), http.MethodGet, "auth.test", nil, nil)
		require.Error(t, err)
	})

	require.Equal(t, "slack auth.test: rate_limited", apiError{method: "auth.test", code: "rate_limited"}.Error())
}

func TestSlackThreadCacheBranchCoverage(t *testing.T) {
	t.Parallel()

	nowMicros := int64(1700000000000000)

	cache := slackThreadCache{}
	require.False(t, cache.recordHistory(slackMessage{}, nowMicros))

	cache = slackThreadCache{
		"root": {
			RootTS:           "mismatch",
			LastSeenAtMicros: nowMicros - 10,
			ReplyCount:       1,
		},
	}
	require.True(t, cache.recordHistory(slackMessage{Ts: "root", ReplyCount: 2}, nowMicros))
	require.Equal(t, "root", cache["root"].RootTS)

	require.Nil(t, cache.refreshCandidates(map[string]struct{}{}, nowMicros, 0))

	orderedCache := slackThreadCache{
		"zero": {
			RootTS:           "zero",
			LastSeenAtMicros: nowMicros,
		},
		"old": {
			RootTS:               "old",
			LastSeenAtMicros:     nowMicros,
			LastHydratedAtMicros: nowMicros - int64(slackThreadRefreshInterval/time.Microsecond),
		},
		"seen": {
			RootTS:               "seen",
			LastSeenAtMicros:     nowMicros,
			LastHydratedAtMicros: nowMicros - int64(slackThreadRefreshInterval/time.Microsecond),
		},
	}
	refresh := orderedCache.refreshCandidates(map[string]struct{}{"seen": {}}, nowMicros, 10)
	require.ElementsMatch(t, []string{"zero", "old"}, refresh)

	tieCache := slackThreadCache{
		"a": {
			RootTS:           "a",
			LastSeenAtMicros: nowMicros,
			ReplyCount:       1,
		},
		"b": {
			RootTS:           "b",
			LastSeenAtMicros: nowMicros,
			ReplyCount:       1,
		},
	}
	require.Equal(t, []string{"a"}, tieCache.refreshCandidates(map[string]struct{}{}, nowMicros, 1))

	require.False(t, (slackThreadCacheEntry{}).shouldRefresh(nowMicros))

	hydratedCache := slackThreadCache{
		"root": {
			RootTS:               "root",
			ReplyCount:           1,
			LastSeenAtMicros:     nowMicros,
			LastHydratedAtMicros: nowMicros,
		},
	}
	require.False(t, hydratedCache.markHydrated("", 1, "latest", nowMicros))
	require.True(t, hydratedCache.markHydrated("root", 2, "latest", nowMicros+1))
	require.Equal(t, "root", hydratedCache["root"].RootTS)
	require.Equal(t, 2, hydratedCache["root"].ReplyCount)
	require.Equal(t, "latest", hydratedCache["root"].LatestReplyTS)
	require.Equal(t, nowMicros+1, hydratedCache["root"].LastHydratedAtMicros)
	require.Equal(t, nowMicros+1, hydratedCache["root"].LastSeenAtMicros)

	require.False(t, slackThreadCache{}.compact(nowMicros))

	compactCache := slackThreadCache{}
	for i := 0; i < slackThreadCacheMaxEntries+1; i++ {
		key := fmt.Sprintf("root-%03d", i)
		compactCache[key] = slackThreadCacheEntry{
			RootTS:           key,
			LastSeenAtMicros: nowMicros,
		}
	}
	require.True(t, compactCache.compact(nowMicros))
	require.Len(t, compactCache, slackThreadCacheMaxEntries)

	require.Nil(t, slackThreadCache{}.snapshot(nowMicros))

	snapshotCache := slackThreadCache{
		"b": {
			RootTS:           "b",
			LastSeenAtMicros: nowMicros,
		},
		"a": {
			RootTS:           "a",
			LastSeenAtMicros: nowMicros,
		},
	}
	snapshot := snapshotCache.snapshot(nowMicros)
	require.Len(t, snapshot, 2)
	require.Equal(t, "a", snapshot[0].RootTS)
	require.Equal(t, "b", snapshot[1].RootTS)
}

func TestSlackPollAndRuntimeBranchCoverage(t *testing.T) {
	oldNow := nowFn
	oldTicker := newPollTicker
	t.Cleanup(func() {
		nowFn = oldNow
		newPollTicker = oldTicker
	})
	nowFn = func() time.Time { return time.Unix(1700000000, 0) }

	t.Run("poll_loop_ticker_branch", func(t *testing.T) {
		tickC := make(chan time.Time, 1)
		newPollTicker = func(time.Duration) (<-chan time.Time, func()) {
			return tickC, func() {}
		}

		store := ptrext.Of(pollLoopStore{FakeSources: inboundtest.NewFakeSources()})
		a := NewAdapter().(*adapter)
		require.NoError(t, a.Start(context.Background(), inbound.Deps{Sources: store}))

		tickC <- time.Now()
		require.Eventually(t, func() bool {
			return store.calls.Load() >= 2
		}, time.Second, 10*time.Millisecond)
		require.NoError(t, a.Shutdown(context.Background()))
	})

	t.Run("poll_all_sources_branches", func(t *testing.T) {
		a := ptrext.Of(adapter{
			deps: inbound.Deps{
				Ingest:  ptrext.Of(inboundtest.FakeIngest{}),
				Secrets: inboundtest.FakeSecrets{},
				Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
			},
		})

		errStore := ptrext.Of(slackSourceStore{
			FakeSources: inboundtest.NewFakeSources(),
			listErr:     errors.New("list boom"),
		})
		a.deps.Sources = errStore
		a.pollAllSources(context.Background())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		a.deps.Sources = ptrext.Of(slackSourceStore{
			FakeSources: inboundtest.NewFakeSources(),
			listSrcs:    []inbound.Source{{ID: "canceled", Enabled: true}},
		})
		a.pollAllSources(ctx)

		source := inbound.Source{
			ID:       "source-1",
			TenantID: "tenant-1",
			Channel:  ChannelName,
			Name:     "Slack Feedback",
			Slug:     "slack-feedback",
			Enabled:  true,
			Config:   []byte{0x01},
			State:    inbound.SourceState{},
		}
		enabledStore := ptrext.Of(slackSourceStore{
			FakeSources: inboundtest.NewFakeSources(),
			tenantSlug:  "tenant-x",
			listSrcs:    []inbound.Source{{Enabled: false}, source},
		})
		enabledStore.Put("tenant-x", source)
		a.deps.Sources = enabledStore
		a.pollAllSources(context.Background())
	})

	t.Run("poll_source_persist_config_failure", func(t *testing.T) {
		secrets := inboundtest.FakeSecrets{}
		rootTS := "1700000000.000100"
		replyTS := "1700000000.000200"
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
		}

		store := ptrext.Of(slackSourceStore{
			FakeSources:     inboundtest.NewFakeSources(),
			tenantSlug:      "tenant-x",
			updateConfigErr: errors.New("update config boom"),
		})
		store.Put("tenant-x", source)

		ingest := ptrext.Of(inboundtest.FakeIngest{})
		client := ptrext.Of(slackThreadClient{
			history: []slackMessage{
				{
					Type:        "message",
					User:        "U1",
					Text:        "root message",
					Ts:          rootTS,
					ReplyCount:  1,
					LatestReply: replyTS,
				},
			},
			replies: map[string][]slackMessage{
				rootTS: {
					{
						Type:     "message",
						User:     "U2",
						Text:     "reply message",
						Ts:       replyTS,
						ThreadTS: rootTS,
					},
				},
			},
		})
		a := ptrext.Of(adapter{
			deps: inbound.Deps{
				Ingest:  ingest,
				Sources: store,
				Secrets: secrets,
				Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
			},
			newClient: func(string) apiClient { return client },
		})

		a.pollSource(context.Background(), source)
		require.Greater(t, store.updateConfigCalls, 0)
	})

	t.Run("persist_slack_config_branches", func(t *testing.T) {
		cfg := Config{
			Version:      ConfigVersion,
			TeamID:       "T123",
			TeamName:     "Acme",
			WorkspaceURL: "https://acme.slack.com/",
			ChannelID:    "C123",
			ChannelName:  "feedback",
		}

		noUpdater := ptrext.Of(adapter{
			deps: inbound.Deps{
				Sources: inboundtest.NewFakeSources(),
				Secrets: inboundtest.FakeSecrets{},
			},
		})
		require.NoError(t, noUpdater.persistSlackConfig(context.Background(), "source-1", []byte("token"), cfg))

		sources := ptrext.Of(slackSourceStore{FakeSources: inboundtest.NewFakeSources(), tenantSlug: "tenant-x"})
		sources.Put("tenant-x", inbound.Source{ID: "source-1"})

		failingEncrypt := ptrext.Of(adapter{
			deps: inbound.Deps{
				Sources: sources,
				Secrets: covSlackFailingSecrets{encryptErr: errors.New("encrypt boom")},
			},
		})
		err := failingEncrypt.persistSlackConfig(context.Background(), "source-1", []byte("token"), cfg)
		require.Error(t, err)

		oldMarshal := jsonMarshal
		jsonMarshal = func(any) ([]byte, error) {
			return nil, errors.New("marshal boom")
		}
		t.Cleanup(func() { jsonMarshal = oldMarshal })
		marshalFail := ptrext.Of(adapter{
			deps: inbound.Deps{
				Sources: sources,
				Secrets: inboundtest.FakeSecrets{},
			},
		})
		err = marshalFail.persistSlackConfig(context.Background(), "source-1", []byte("token"), cfg)
		require.Error(t, err)

		secondFailSecrets := ptrext.Of(covSlackSecondEncryptSecrets{secondErr: errors.New("config encrypt boom")})
		jsonMarshal = oldMarshal
		secondFail := ptrext.Of(adapter{
			deps: inbound.Deps{
				Sources: sources,
				Secrets: secondFailSecrets,
			},
		})
		err = secondFail.persistSlackConfig(context.Background(), "source-1", []byte("token"), cfg)
		require.Error(t, err)
	})

	t.Run("apply_and_hydrate_branches", func(t *testing.T) {
		cfg := Config{
			Version:      ConfigVersion,
			TeamID:       "T123",
			TeamName:     "Acme",
			WorkspaceURL: "https://acme.slack.com/",
			ChannelID:    "C123",
			ChannelName:  "feedback",
		}
		src := inbound.Source{
			ID:       "source-1",
			TenantID: "tenant-1",
			Channel:  ChannelName,
			Name:     "Slack Feedback",
			Slug:     "slack-feedback",
		}

		t.Run("apply_history_message", func(t *testing.T) {
			metrics := ptrext.Of(inboundtest.FakeMetrics{})
			a := ptrext.Of(adapter{
				deps: inbound.Deps{
					Ingest:  ptrext.Of(inboundtest.FakeIngest{}),
					Metrics: metrics,
				},
			})
			state := slackPollState{
				src:       src,
				cache:     newSlackThreadCache(nil),
				seenRoots: map[string]struct{}{},
				scheduled: map[string]struct{}{},
				nowMicros: nowFn().UnixMicro(),
			}

			state = a.applySlackHistoryMessage(context.Background(), cfg, slackMessage{Type: "event", Ts: "1700000000.000100"}, "where", state)
			require.Empty(t, state.scheduled)

			state = a.applySlackHistoryMessage(context.Background(), cfg, slackMessage{Type: "message", Text: "hello", Ts: "bad"}, "where", state)
			require.Empty(t, state.scheduled)

			state = a.applySlackHistoryMessage(context.Background(), cfg, slackMessage{Type: "message", Text: "   ", Ts: "1700000000.000100"}, "where", state)
			require.True(t, state.cacheDirty)

			ingest := ptrext.Of(inboundtest.FakeIngest{NextErr: errors.New("boom")})
			a.deps.Ingest = ingest
			state = slackPollState{
				src:       src,
				cache:     newSlackThreadCache(nil),
				seenRoots: map[string]struct{}{},
				scheduled: map[string]struct{}{},
				nowMicros: nowFn().UnixMicro(),
			}
			state = a.applySlackHistoryMessage(context.Background(), cfg, slackMessage{
				Type:        "message",
				Text:        "root message",
				Ts:          "1700000000.000100",
				ReplyCount:  1,
				LatestReply: "1700000000.000200",
			}, "where", state)
			require.Contains(t, state.scheduled, "1700000000.000100")
			require.Equal(t, int64(1700000000000100), state.src.State.LastUID)
			require.Contains(t, metrics.Totals, "slack|tenant-1|slack-feedback|internal_err")
		})

		t.Run("apply_thread_reply", func(t *testing.T) {
			metrics := ptrext.Of(inboundtest.FakeMetrics{})
			ingest := ptrext.Of(inboundtest.FakeIngest{})
			a := ptrext.Of(adapter{
				deps: inbound.Deps{
					Ingest:  ingest,
					Metrics: metrics,
				},
			})
			state := slackThreadHydrationState{latestReplyTS: "old", replyCount: 0}
			_, changed := a.applySlackThreadReply(context.Background(), src, cfg, "1700000000.000100", slackMessage{Ts: "1700000000.000100", Type: "message", Text: "root"}, state)
			require.False(t, changed)

			_, changed = a.applySlackThreadReply(context.Background(), src, cfg, "1700000000.000100", slackMessage{Ts: "1700000000.000200", Type: "message", Subtype: "channel_join", Text: "join"}, state)
			require.False(t, changed)

			_, changed = a.applySlackThreadReply(context.Background(), src, cfg, "1700000000.000100", slackMessage{Ts: "bad", Type: "message", Text: "reply"}, state)
			require.False(t, changed)

			_, changed = a.applySlackThreadReply(context.Background(), src, cfg, "1700000000.000100", slackMessage{Ts: "1700000000.000200", Type: "message", Text: "   "}, state)
			require.False(t, changed)

			ingest.NextErr = errors.New("boom")
			_, changed = a.applySlackThreadReply(context.Background(), src, cfg, "1700000000.000100", slackMessage{Ts: "1700000000.000200", Type: "message", Text: "reply", ThreadTS: "1700000000.000100"}, state)
			require.False(t, changed)

			ingest = ptrext.Of(inboundtest.FakeIngest{})
			a.deps.Ingest = ingest
			state = slackThreadHydrationState{latestReplyTS: "old", replyCount: 0}
			updated, changed := a.applySlackThreadReply(context.Background(), src, cfg, "1700000000.000100", slackMessage{
				Ts:       "1700000000.000200",
				ThreadTS: "1700000000.000100",
				Type:     "message",
				Text:     "reply",
				User:     "U2",
			}, state)
			require.True(t, changed)
			require.Equal(t, "1700000000.000200", updated.latestReplyTS)
			require.Equal(t, 1, updated.replyIngested)
			require.Len(t, ingest.Calls, 1)
			require.Equal(t, 1, ingest.Calls[0].In.SourceMeta["slack_reply_count"])
		})

		t.Run("hydrate_thread", func(t *testing.T) {
			replyRoot := slackMessage{Type: "message", Text: "root", Ts: "1700000000.000100", ThreadTS: "1700000000.000100"}
			reply := slackMessage{Type: "message", Text: "reply", Ts: "1700000000.000200", ThreadTS: "1700000000.000100"}

			notFoundClient := ptrext.Of(slackThreadClient{repliesErr: apiError{method: "conversations.replies", code: "thread_not_found"}})
			a := ptrext.Of(adapter{deps: inbound.Deps{Metrics: ptrext.Of(inboundtest.FakeMetrics{})}})
			changed, err := a.hydrateSlackThread(context.Background(), notFoundClient, src, cfg, slackThreadCache{}, "1700000000.000100", nowFn().UnixMicro())
			require.NoError(t, err)
			require.True(t, changed)
			require.Len(t, notFoundClient.repliesCalls, 1)
			require.Equal(t, "1700000000.000100", notFoundClient.repliesCalls[0].ThreadTS)

			_, err = a.hydrateSlackThread(context.Background(), ptrext.Of(slackThreadClient{repliesErr: errors.New("temporary slack error")}), src, cfg, slackThreadCache{}, "1700000000.000100", nowFn().UnixMicro())
			require.Error(t, err)

			client := ptrext.Of(slackThreadClient{
				replies: map[string][]slackMessage{
					"1700000000.000100": {
						reply,
						replyRoot,
					},
				},
			})
			cache := slackThreadCache{
				"1700000000.000100": {
					RootTS:           "1700000000.000100",
					ReplyCount:       0,
					LastSeenAtMicros: nowFn().UnixMicro(),
				},
			}
			ingest := ptrext.Of(inboundtest.FakeIngest{})
			a = ptrext.Of(adapter{
				deps: inbound.Deps{
					Ingest:  ingest,
					Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
				},
			})
			changed, err = a.hydrateSlackThread(context.Background(), client, src, cfg, cache, "1700000000.000100", nowFn().UnixMicro())
			require.NoError(t, err)
			require.True(t, changed)
			require.Len(t, client.repliesCalls, 1)
			require.Equal(t, slackTimestampMicrosOrZero("1700000000.000100"), client.repliesCalls[0].OldestMicros)
			require.Equal(t, "1700000000.000200", cache["1700000000.000100"].LatestReplyTS)
		})
	})

	t.Run("startup_and_shutdown", func(t *testing.T) {
		a := NewAdapter().(*adapter)
		deps := inbound.Deps{Sources: inboundtest.NewFakeSources()}
		require.NoError(t, a.Start(context.Background(), deps))
		require.NoError(t, a.Start(context.Background(), deps))
		require.NoError(t, a.Shutdown(context.Background()))

		timeoutAdapter := ptrext.Of(adapter{})
		timeoutAdapter.wg.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		time.Sleep(time.Millisecond)
		err := timeoutAdapter.Shutdown(ctx)
		cancel()
		require.Error(t, err)
		timeoutAdapter.wg.Done()
	})
}
