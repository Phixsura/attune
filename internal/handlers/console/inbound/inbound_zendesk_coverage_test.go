// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/zendesk"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

// stubZendeskAuth returns a zendeskAuthTestFn that returns the given
// account info or error.
func stubZendeskAuth(acct zendesk.AccountInfo, err error) zendeskAuthTestFn {
	return func(_ context.Context, _ zendesk.ConnInputs) (zendesk.AccountInfo, error) {
		return acct, err
	}
}

// ====================== createZendesk — validation =========================

func TestCreateZendesk_MissingConfig(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "zendesk_config is required")
}

func TestCreateZendesk_InvalidSubdomain(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "INVALID!!",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "subdomain")
}

func TestCreateZendesk_MissingEmailForAPIToken(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "email")
}

func TestCreateZendesk_AuthFails(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		sources:         ptrext.Of(covSourceRepo{}),
		secrets:         inboundtest.FakeSecrets{},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{}, errors.New("invalid credentials")),
	})
	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "Zendesk connection failed")
}

func TestCreateZendesk_InvalidStartFrom(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		sources:         ptrext.Of(covSourceRepo{}),
		secrets:         inboundtest.FakeSecrets{},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 1}, nil),
	})
	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
				StartFrom: ptrext.Of("invalid"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "start_from")
}

// ====================== createZendesk — happy path =========================

func TestCreateZendesk_APIToken_Success(t *testing.T) {
	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID:        id,
			TenantID:  "tenant-1",
			Channel:   channelZendesk,
			Name:      "My Zendesk",
			Slug:      "my-zendesk",
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
	}

	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		zendeskWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 12345}, nil),
	})

	result, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, 1, tx.execCalls)
	require.Contains(t, tx.lastSQL, "INSERT INTO inbound_sources")
	require.NotNil(t, result.Body)
	require.Equal(t, "My Zendesk", result.Body.GetSource().GetName())
	require.Equal(t, "zendesk", result.Body.GetSource().GetChannel())
}

func TestCreateZendesk_OAuth_Success(t *testing.T) {
	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID:        id,
			TenantID:  "tenant-1",
			Channel:   channelZendesk,
			Name:      "Zendesk OAuth",
			Slug:      "zendesk-oauth",
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
	}

	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		zendeskWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 99}, nil),
	})

	result, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "Zendesk OAuth",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain:           "mycompany",
				AuthMode:            "oauth",
				OauthAccessToken:    ptrext.Of("access-token-123"),
				OauthClientIdV2:     ptrext.Of("client-id"),
				OauthClientSecretV2: ptrext.Of("client-secret"),
			}),
		}),
		"Zendesk OAuth",
		"zendesk-oauth",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "zendesk", result.Body.GetSource().GetChannel())
}

// ====================== createZendesk — error paths ========================

func TestCreateZendesk_InsertFails(t *testing.T) {
	tx := ptrext.Of(covSlackTx{execErr: errors.New("insert boom")})
	h := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: inboundtest.FakeSecrets{},
		zendeskWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 1}, nil),
	})

	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestCreateZendesk_ReloadFails(t *testing.T) {
	repo := ptrext.Of(covSourceRepo{getErr: errors.New("reload boom")})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		zendeskWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, ptrext.Of(covSlackTx{}))
		},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 1}, nil),
	})

	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestCreateZendesk_AuditFails(t *testing.T) {
	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: channelZendesk,
			Name: "My Zendesk", Slug: "my-zendesk", Enabled: true,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
	}
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		audit:   ptrext.Of(covFailingAuditRecorder{err: errors.New("audit boom")}),
		zendeskWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, ptrext.Of(covSlackTx{}))
		},
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 1}, nil),
	})

	_, err := h.createZendesk(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
		"My Zendesk",
		"my-zendesk",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

// ====================== encryptZendeskConfig ===============================

func TestEncryptZendeskConfig_APIToken(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	inputs := zendesk.ConnInputs{
		Subdomain: "mycompany",
		AuthMode:  zendesk.AuthModeAPIToken,
		Email:     "admin@example.com",
		APIToken:  "my-secret-token",
	}
	envelope, err := h.encryptZendeskConfig(inputs, ptrext.Of(attunev1.ZendeskConnConfig{}))
	require.NoError(t, err)
	require.NotEmpty(t, envelope)

	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)

	var cfg zendesk.Config
	require.NoError(t, json.Unmarshal(decOuter, &cfg))
	require.Equal(t, zendesk.ConfigVersion, cfg.Version)
	require.Equal(t, "api_token", cfg.AuthMode)
	require.Equal(t, "mycompany", cfg.Subdomain)
	require.Equal(t, "admin@example.com", cfg.Email)
	require.Equal(t, "now", cfg.StartFrom)
	require.NotEmpty(t, cfg.APITokenEncrypted)

	decToken, err := inboundtest.FakeSecrets{}.Decrypt(cfg.APITokenEncrypted)
	require.NoError(t, err)
	require.Equal(t, "my-secret-token", string(decToken))
}

func TestEncryptZendeskConfig_OAuth(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	inputs := zendesk.ConnInputs{
		Subdomain:         "mycompany",
		AuthMode:          zendesk.AuthModeOAuth,
		OAuthClientSecret: "my-oauth-secret",
	}
	envelope, err := h.encryptZendeskConfig(inputs, ptrext.Of(attunev1.ZendeskConnConfig{StartFrom: ptrext.Of("full")}))
	require.NoError(t, err)
	require.NotEmpty(t, envelope)

	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)

	var cfg zendesk.Config
	require.NoError(t, json.Unmarshal(decOuter, &cfg))
	require.Equal(t, zendesk.ConfigVersion, cfg.Version)
	require.Equal(t, "oauth", cfg.AuthMode)
	require.Equal(t, "full", cfg.StartFrom)
	require.NotEmpty(t, cfg.OAuthTokenEncrypted)

	decTok, err := inboundtest.FakeSecrets{}.Decrypt(cfg.OAuthTokenEncrypted)
	require.NoError(t, err)
	require.Contains(t, string(decTok), "my-oauth-secret")
}

func TestEncryptZendeskConfig_EncryptFails(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: covFailingSecrets{encryptErr: errors.New("encrypt boom")}})
	inputs := zendesk.ConnInputs{
		Subdomain: "mycompany",
		AuthMode:  zendesk.AuthModeAPIToken,
		Email:     "admin@example.com",
		APIToken:  "tok",
	}
	_, err := h.encryptZendeskConfig(inputs, ptrext.Of(attunev1.ZendeskConnConfig{}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypt zendesk api_token")
}

// ====================== Create routes to zendesk ===========================

func TestCreateRoutesToZendeskChannel(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.Create(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "zendesk",
			Name:    "My Zendesk",
		}),
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Contains(t, de.Message, "zendesk_config is required")
}

func TestCreateZendesk_ViaHTTP_MissingConfig(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{"channel":"zendesk","name":"My Zendesk"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "zendesk_config is required")
}

// ====================== testZendeskConnection ==============================

func TestTestConnectionZendesk_MissingConfig(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	result, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "zendesk",
		}),
	)
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "zendesk_config is required")
}

func TestTestConnectionZendesk_InvalidSubdomain(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	result, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "BAD!!",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok"),
			}),
		}),
	)
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "subdomain")
}

func TestTestConnectionZendesk_Success(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{
		audit:           audit,
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 42}, nil),
	})

	result, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
	)
	require.NoError(t, err)
	require.True(t, result.Body.GetOk())
	require.NotNil(t, result.Body.GetLatencyMs())
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source.test_connection", audit.events[0].Action)
}

func TestTestConnectionZendesk_AuthFails(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{
		audit:           audit,
		zendeskAuthTest: stubZendeskAuth(zendesk.AccountInfo{}, errors.New("invalid credentials")),
	})

	result, err := h.TestConnection(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.TestInboundConnectionRequest{
			Channel: "zendesk",
			ZendeskConfig: ptrext.Of(attunev1.ZendeskConnConfig{
				Subdomain: "mycompany",
				AuthMode:  "api_token",
				Email:     ptrext.Of("admin@example.com"),
				ApiToken:  ptrext.Of("tok123"),
			}),
		}),
	)
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "invalid credentials")
	require.Len(t, audit.events, 1)
}

func TestTestConnectionZendesk_ViaHTTP_Success(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	h.zendeskAuthTest = stubZendeskAuth(zendesk.AccountInfo{Subdomain: "mycompany", AccountID: 1}, nil)
	body := `{"channel":"zendesk","zendeskConfig":{"subdomain":"mycompany","authMode":"api_token","email":"admin@example.com","apiToken":"tok123"}}`
	w := serveTestConnection(h, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok":true`)
}

func TestTestConnectionZendesk_ViaHTTP_MissingConfig(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveTestConnection(h, `{"channel":"zendesk"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "zendesk_config is required")
}
