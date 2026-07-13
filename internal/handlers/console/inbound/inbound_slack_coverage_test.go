// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/slack"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

type covSlackTx struct {
	execCalls int
	execErr   error
	lastSQL   string
	rowErr    error
}

func (t *covSlackTx) Begin(context.Context) (pgx.Tx, error) {
	return t, nil
}

func (t *covSlackTx) Commit(context.Context) error {
	return nil
}

func (t *covSlackTx) Rollback(context.Context) error {
	return nil
}

func (t *covSlackTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (t *covSlackTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (t *covSlackTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (t *covSlackTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (t *covSlackTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.execCalls++
	t.lastSQL = sql
	if t.execErr != nil {
		return pgconn.CommandTag{}, t.execErr
	}
	return pgconn.NewCommandTag("INSERT 1"), nil
}

func (t *covSlackTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (t *covSlackTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return covSlackRow{err: t.rowErr}
}

func (t *covSlackTx) Conn() *pgx.Conn {
	return nil
}

type covSlackRow struct {
	err error
}

func (r covSlackRow) Scan(...any) error {
	if r.err != nil {
		return r.err
	}
	return errors.New("unexpected QueryRow call")
}

type covPrimaryKeySecrets struct {
	inboundtest.FakeSecrets
}

func (covPrimaryKeySecrets) PrimaryKeyID() string { return "primary-key" }

func mustDispatcherError(t *testing.T, err error) *dispatcher.Error {
	t.Helper()
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de)) // ptrext:allow errors.As out-param
	return de
}

func TestNewHandlerWiresSlackDependencies(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil, nil, inboundtest.FakeSecrets{}, "https://attune.example.com/")
	require.NotNil(t, h)
	require.Equal(t, "https://attune.example.com", h.baseURL)
	require.NotNil(t, h.rotate)
	require.NotNil(t, h.slackWithTx)
	require.NotNil(t, h.testConn)
	require.NotNil(t, h.slackAuthTest)
	require.NotNil(t, h.slackDiscover)
	require.NotNil(t, h.slackValidateChannel)
	require.NotNil(t, h.tenantSlug)
}

func TestCreateSlack_DefaultWithTxFallback(t *testing.T) {
	t.Cleanup(func() { slack.SetAPIBaseURL("") })

	server := newSlackSuccessServer(t)
	slack.SetAPIBaseURL(server.URL)

	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID:        id,
			TenantID:  "tenant-1",
			Channel:   channelSlack,
			Name:      "Slack Feedback",
			Slug:      "slack-feedback",
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
	}

	tx := ptrext.Of(covSlackTx{execErr: errors.New("insert boom")})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
	})
	_, err := h.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestCreateSlack_SuccessAndFailures(t *testing.T) {
	t.Cleanup(func() { slack.SetAPIBaseURL("") })

	server := newSlackSuccessServer(t)
	slack.SetAPIBaseURL(server.URL)

	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID:        id,
			TenantID:  "tenant-1",
			Channel:   channelSlack,
			Name:      "Slack Feedback",
			Slug:      "slack-feedback",
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
	}

	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
	})

	result, err := h.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, 1, tx.execCalls)
	require.Contains(t, tx.lastSQL, "INSERT INTO inbound_sources")
	require.NotNil(t, result.Body)
	require.Equal(t, "Slack Feedback", result.Body.GetSource().GetName())

	t.Run("marshal failure", func(t *testing.T) {
		oldMarshal := jsonMarshal
		jsonMarshal = func(any) ([]byte, error) {
			return nil, errors.New("marshal boom")
		}
		t.Cleanup(func() { jsonMarshal = oldMarshal })

		tx := ptrext.Of(covSlackTx{})
		h := ptrext.Of(Handler{
			sources: repo,
			secrets: inboundtest.FakeSecrets{},
			slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
				return fn(ctx, tx)
			},
		})

		_, err := h.createSlack(
			context.Background(),
			ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
			ptrext.Of(attunev1.CreateInboundSourceRequest{
				Channel: "slack",
				Name:    "Slack Feedback",
				SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
					BotToken:  "xoxb-test-token",
					ChannelId: "C123",
				}),
			}),
			"Slack Feedback",
			"slack-feedback",
		)
		require.Error(t, err)
		de := mustDispatcherError(t, err)
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("reload failure", func(t *testing.T) {
		repo := ptrext.Of(covSourceRepo{getErr: errors.New("reload boom")})
		h := ptrext.Of(Handler{
			sources: repo,
			secrets: inboundtest.FakeSecrets{},
			slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
				return fn(ctx, ptrext.Of(covSlackTx{}))
			},
		})
		_, err := h.createSlack(
			context.Background(),
			ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
			ptrext.Of(attunev1.CreateInboundSourceRequest{
				Channel: "slack",
				Name:    "Slack Feedback",
				SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
					BotToken:  "xoxb-test-token",
					ChannelId: "C123",
				}),
			}),
			"Slack Feedback",
			"slack-feedback",
		)
		require.Error(t, err)
		de := mustDispatcherError(t, err)
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("audit failure", func(t *testing.T) {
		h := ptrext.Of(Handler{
			sources: repo,
			secrets: inboundtest.FakeSecrets{},
			audit:   ptrext.Of(covFailingAuditRecorder{err: errors.New("audit boom")}),
			slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
				return fn(ctx, ptrext.Of(covSlackTx{}))
			},
		})
		_, err := h.createSlack(
			context.Background(),
			ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
			ptrext.Of(attunev1.CreateInboundSourceRequest{
				Channel: "slack",
				Name:    "Slack Feedback",
				SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
					BotToken:  "xoxb-test-token",
					ChannelId: "C123",
				}),
			}),
			"Slack Feedback",
			"slack-feedback",
		)
		require.Error(t, err)
		de := mustDispatcherError(t, err)
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})
}

func TestCreateSlack_ValidationFailures(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})

	_, err := h.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)

	_, err = h.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)
}

func TestCreateRoutesToSlackChannel(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.Create(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
		}),
	)
	require.Error(t, err)
}

func TestCreateSlackSlackBranches(t *testing.T) {
	t.Parallel()

	base := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})

	_, err := base.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)

	validateErrHandler := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: inboundtest.FakeSecrets{},
		slackValidateChannel: func(context.Context, string, string) (slack.AuthInfo, slack.Channel, error) {
			return slack.AuthInfo{}, slack.Channel{}, errors.New("channel boom")
		},
	})
	_, err = validateErrHandler.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)

	encryptErrHandler := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: covFailingSecrets{encryptErr: errors.New("encrypt boom")},
		slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, ptrext.Of(covSlackTx{}))
		},
	})
	_, err = encryptErrHandler.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)

	writableErrHandler := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: covPrimaryKeySecrets{},
		slackWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, ptrext.Of(covSlackTx{rowErr: pgx.ErrNoRows}))
		},
	})
	_, err = writableErrHandler.createSlack(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "slack",
			Name:    "Slack Feedback",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
		"Slack Feedback",
		"slack-feedback",
	)
	require.Error(t, err)
}

func TestDiscoverSlackChannelsDefaultAndUnauthorized(t *testing.T) {
	t.Cleanup(func() { slack.SetAPIBaseURL("") })

	successServer := newSlackSuccessServer(t)
	slack.SetAPIBaseURL(successServer.URL)

	h := ptrext.Of(Handler{})
	result, err := h.DiscoverSlackChannels(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.DiscoverSlackChannelsRequest{
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken: "xoxb-test-token",
			}),
		}),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetChannels(), 1)
	require.Equal(t, "C123", result.Body.GetChannels()[0].GetId())

	failServer := newSlackUnauthorizedServer(t)
	slack.SetAPIBaseURL(failServer.URL)
	_, err = h.DiscoverSlackChannels(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.DiscoverSlackChannelsRequest{
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken: "xoxb-test-token",
			}),
		}),
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusUnauthorized, de.Status)
}

func TestDiscoverSlackChannelsMissingConfig(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	_, err := h.DiscoverSlackChannels(covDirectCtx("tenant-1"), ptrext.Of(attunev1.DiscoverSlackChannelsRequest{}))
	require.Error(t, err)
}

func TestDiscoverSlackChannelsValidationFailure(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	_, err := h.DiscoverSlackChannels(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.DiscoverSlackChannelsRequest{
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{BotToken: ""}),
		}),
	)
	require.Error(t, err)
}

func TestTestConnectionSlackDefaults(t *testing.T) {
	t.Cleanup(func() { slack.SetAPIBaseURL("") })

	server := newSlackSuccessServer(t)
	slack.SetAPIBaseURL(server.URL)

	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{
		audit: audit,
	})

	noChannel, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "slack",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken: "xoxb-test-token",
			}),
		}),
	)
	require.NoError(t, err)
	require.True(t, noChannel.Body.GetOk())
	require.NotNil(t, noChannel.Body.GetLatencyMs())

	withChannel, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "slack",
			SlackConfig: ptrext.Of(attunev1.SlackConnConfig{
				BotToken:  "xoxb-test-token",
				ChannelId: "C123",
			}),
		}),
	)
	require.NoError(t, err)
	require.True(t, withChannel.Body.GetOk())
	require.NotNil(t, withChannel.Body.GetLatencyMs())
	require.Len(t, audit.events, 2)
	require.Equal(t, "inbound_source.test_connection", audit.events[0].Action)
}

func TestTestConnectionSlackMissingConfig(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	result, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "slack",
		}),
	)
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "slack_config is required")
}

func TestTestConnectionSlackBranchFailures(t *testing.T) {
	t.Parallel()

	h := ptrext.Of(Handler{})
	unsupported, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "sms",
		}),
	)
	require.NoError(t, err)
	require.False(t, unsupported.Body.GetOk())

	_, _, _, err = h.resolveTestConnection(
		context.Background(),
		ptrext.Of(attunev1.TestInboundConnectionRequest{}),
		"sms",
	)
	require.Error(t, err)

	_, _, _, err = h.testSlackConnection(context.Background(), ptrext.Of(attunev1.SlackConnConfig{BotToken: ""}))
	require.Error(t, err)

	authErrHandler := ptrext.Of(Handler{
		slackAuthTest: func(context.Context, string) (slack.AuthInfo, error) {
			return slack.AuthInfo{}, errors.New("auth boom")
		},
	})
	_, err = authErrHandler.testSlackAuth(
		context.Background(),
		slack.ConnInputs{BotToken: "xoxb-test-token"},
		map[string]any{},
	)
	require.Error(t, err)

	channelErrHandler := ptrext.Of(Handler{
		slackValidateChannel: func(context.Context, string, string) (slack.AuthInfo, slack.Channel, error) {
			return slack.AuthInfo{}, slack.Channel{}, errors.New("channel boom")
		},
	})
	_, err = channelErrHandler.testSlackChannel(
		context.Background(),
		slack.ConnInputs{BotToken: "xoxb-test-token", ChannelID: "C123"},
		map[string]any{},
	)
	require.Error(t, err)
}

func newSlackSuccessServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func newSlackUnauthorizedServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid_auth",
			})
		case "/conversations.list":
			http.Error(w, "should not reach conversations.list", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
}
