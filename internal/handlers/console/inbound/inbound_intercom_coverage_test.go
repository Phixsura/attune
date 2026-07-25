// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package inbound

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

// stubIntercomAuth returns an intercomAuthTestFn with fixed results.
func stubIntercomAuth(acct intercom.AccountInfo, err error) intercomAuthTestFn {
	return func(_ context.Context, _, _ string) (intercom.AccountInfo, error) {
		return acct, err
	}
}

// ===================== createIntercom — validation ==========================

func TestCreateIntercom_MissingConfig(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
		}),
		"My Intercom",
		"my-intercom",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "intercom_config is required")
}

func TestCreateIntercom_InvalidRegion(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
			IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
				Region:      "mars",
				AccessToken: "tok",
			}),
		}),
		"My Intercom",
		"my-intercom",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "region")
}

func TestCreateIntercom_MissingToken(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
			IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
				Region: "us",
			}),
		}),
		"My Intercom",
		"my-intercom",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "access_token")
}

func TestCreateIntercom_AuthFails(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		sources:          ptrext.Of(covSourceRepo{}),
		secrets:          inboundtest.FakeSecrets{},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{}, errors.New("unauthorized: Access Token Invalid")),
	})
	_, err := h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
			IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
				Region:      "us",
				AccessToken: "bad-token",
			}),
		}),
		"My Intercom",
		"my-intercom",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "Intercom rejected the access token")
}

func TestCreateIntercom_InvalidFilterState(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
			IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
				Region:       "us",
				AccessToken:  "tok",
				FilterStates: []string{"weird"},
			}),
		}),
		"My Intercom",
		"my-intercom",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "invalid conversation state")
}

// ===================== createIntercom — happy path ==========================

func TestCreateIntercom_Success(t *testing.T) {
	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID:        id,
			TenantID:  "tenant-1",
			Channel:   channelIntercom,
			Name:      "My Intercom",
			Slug:      "my-intercom",
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
	}

	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{
			WorkspaceID: "ws42", WorkspaceName: "Acme", Region: "us",
		}, nil),
	})

	result, err := h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
			IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
				Region:      "us",
				AccessToken: "tok-123",
				StartFrom:   ptrext.Of("full"),
			}),
		}),
		"My Intercom",
		"my-intercom",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, 1, tx.execCalls)
	require.Contains(t, tx.lastSQL, "INSERT INTO inbound_sources")
	require.NotNil(t, result.Body)
	require.Equal(t, "My Intercom", result.Body.GetSource().GetName())
	require.Equal(t, "intercom", result.Body.GetSource().GetChannel())
}

// ==================== test-connection — intercom case =======================

func TestTestIntercomConnection_MissingConfig(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	_, _, fields, err := h.testIntercomConnection(context.Background(), nil)
	require.Error(t, err)
	require.Nil(t, fields)
	require.Contains(t, err.Error(), "intercom_config is required")
}

func TestTestIntercomConnection_Success(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		secrets: inboundtest.FakeSecrets{},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{
			WorkspaceID: "ws42", WorkspaceName: "Acme",
		}, nil),
	})
	targetID, title, fields, err := h.testIntercomConnection(context.Background(), ptrext.Of(attunev1.IntercomConnConfig{
		Region:      "eu",
		AccessToken: "tok",
	}))
	require.NoError(t, err)
	require.Equal(t, "ws42", targetID)
	require.Equal(t, "Tested inbound intercom connection", title)
	require.Equal(t, "eu", fields["region"])
	require.Equal(t, "ws42", fields["intercom_workspace_id"])
	require.Equal(t, "Acme", fields["intercom_workspace_name"])
}

func TestTestIntercomConnection_AuthFailure(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		secrets:          inboundtest.FakeSecrets{},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{}, errors.New("api_plan_restricted")),
	})
	targetID, _, fields, err := h.testIntercomConnection(context.Background(), ptrext.Of(attunev1.IntercomConnConfig{
		Region:      "us",
		AccessToken: "tok",
	}))
	require.Error(t, err)
	require.Equal(t, "intercom-auth", targetID)
	require.NotNil(t, fields)
	require.Contains(t, err.Error(), "plan does not allow API access")
}

// ======================= friendlyIntercomError ==============================

func TestFriendlyIntercomError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("unauthorized"), "rejected the access token"},
		{errors.New("token_expired"), "rejected the access token"},
		{errors.New("api_plan_restricted"), "plan does not allow"},
		{errors.New("rate limited (retry after 10s)"), "rate limit"},
		{errors.New("dial tcp: no such host"), "Could not reach"},
		{errors.New("something odd"), "Intercom connection failed"},
	}
	for _, tc := range cases {
		require.Contains(t, friendlyIntercomError(tc.err), tc.want, "for %v", tc.err)
	}
}
