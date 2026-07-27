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
	"github.com/Phixsura/attune/internal/infra/intercomclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func intercomCreateReq() *attunev1.CreateInboundSourceRequest {
	return ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "intercom",
		Name:    "My Intercom",
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
			Region:      "us",
			AccessToken: "tok-123",
		}),
	})
}

func callCreateIntercom(t *testing.T, h *Handler) (any, error) {
	t.Helper()
	return h.createIntercom(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		intercomCreateReq(),
		"My Intercom",
		"my-intercom",
	)
}

// TestCreateRoutesToIntercomChannel covers the channel dispatch arm in
// Create().
func TestCreateRoutesToIntercomChannel(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	_, err := h.Create(
		covDirectCtx("tenant-1"),
		ptrext.Of(attunev1.CreateInboundSourceRequest{
			Channel: "intercom",
			Name:    "My Intercom",
		}),
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Contains(t, de.Message, "intercom_config is required")
}

// TestCreateIntercom_EmptyWorkspaceID covers the empty id_code guard.
func TestCreateIntercom_EmptyWorkspaceID(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		sources:          ptrext.Of(covSourceRepo{}),
		secrets:          inboundtest.FakeSecrets{},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "  "}, nil),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "did not report a workspace ID")
}

// TestCreateIntercom_TxFailure covers the WithTx error leg.
func TestCreateIntercom_TxFailure(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: inboundtest.FakeSecrets{},
		intercomWithTx: func(_ context.Context, _ *pgxpool.Pool, _ bool, _ func(context.Context, secretlock.Tx) error) error {
			return errors.New("tx boom")
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
}

// TestCreateIntercom_EncryptFailureInsideTx covers encryptIntercomConfig's
// token-encrypt error propagating out of the tx closure.
func TestCreateIntercom_EncryptFailureInsideTx(t *testing.T) {
	t.Parallel()
	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: covFailingSecrets{encryptErr: errors.New("encrypt boom")},
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
}

// TestCreateIntercom_MarshalFailure covers encryptIntercomConfig's
// jsonMarshal error leg via the test seam.
func TestCreateIntercom_MarshalFailure(t *testing.T) {
	origMarshal := jsonMarshal
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
	t.Cleanup(func() { jsonMarshal = origMarshal })

	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: inboundtest.FakeSecrets{},
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
}

// TestCreateIntercom_ReloadFailure covers the post-insert Get error leg.
func TestCreateIntercom_ReloadFailure(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{getErr: errors.New("reload boom")})
	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Contains(t, de.Message, "reload failed")
}

// TestCreateIntercom_AuditFailure covers the audit-write error leg.
func TestCreateIntercom_AuditFailure(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	repo.getHook = func(id string) {
		repo.getSrc = inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: channelIntercom,
			Name: "My Intercom", Slug: "my-intercom", Enabled: true,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
	}
	tx := ptrext.Of(covSlackTx{})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
		audit:            ptrext.Of(covFailingAuditRecorder{err: errors.New("audit db down")}),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Contains(t, de.Message, "audit")
}

// TestFriendlyIntercomError_StatusMapping covers the status-code arms.
func TestFriendlyIntercomError_StatusMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"401 empty body", intercomclient.APIError{Method: "/me", Status: 401}, "rejected the access token"},
		{"403 plan restricted", intercomclient.APIError{Method: "/me", Status: 403, Code: "api_plan_restricted"}, "plan does not allow API access"},
		{"403 other code", intercomclient.APIError{Method: "/me", Status: 403, Code: "forbidden"}, "denied access for this token"},
		{"non-api unauthorized text", errors.New("request unauthorized by upstream"), "rejected the access token"},
		{"non-api plan text", errors.New("api_plan_restricted for workspace"), "plan does not allow API access"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, friendlyIntercomError(tt.err), tt.want)
		})
	}
}

// TestCreateIntercom_DefaultSeams covers the nil-seam fallbacks: the real
// intercom.AuthTest (fails fast on a cancelled ctx — no live egress) and
// the real secretlock.WithTx (fails fast on a nil pool).
func TestCreateIntercom_DefaultSeams(t *testing.T) {
	t.Parallel()

	// Default authTest: cancelled ctx fails the /me HTTP call.
	hAuth := ptrext.Of(Handler{sources: ptrext.Of(covSourceRepo{}), secrets: inboundtest.FakeSecrets{}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := hAuth.createIntercom(
		ctx,
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
		intercomCreateReq(),
		"My Intercom", "my-intercom",
	)
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Status)

	// Default withTx: nil pool is refused by secretlock.WithTx.
	hTx := ptrext.Of(Handler{
		sources:          ptrext.Of(covSourceRepo{}),
		secrets:          inboundtest.FakeSecrets{},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
	})
	_, err = callCreateIntercom(t, hTx)
	require.Error(t, err)
}

// TestCreateIntercom_EnsureWritableKeyFailure covers the
// EnsureWritableKey error leg inside the tx closure: a keyed secret
// store forces the registry check, whose QueryRow fails on the fake tx.
func TestCreateIntercom_EnsureWritableKeyFailure(t *testing.T) {
	t.Parallel()
	tx := ptrext.Of(covSlackTx{rowErr: errors.New("registry boom")})
	h := ptrext.Of(Handler{
		sources: ptrext.Of(covSourceRepo{}),
		secrets: covPrimaryKeySecrets{},
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
		intercomAuthTest: stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws42"}, nil),
	})
	_, err := callCreateIntercom(t, h)
	require.Error(t, err)
}

// TestTestIntercomConnection_ValidationError covers the ValidateConnConfig
// error leg of testIntercomConnection.
func TestTestIntercomConnection_ValidationError(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	_, _, fields, err := h.testIntercomConnection(context.Background(), ptrext.Of(attunev1.IntercomConnConfig{
		Region:      "mars",
		AccessToken: "tok",
	}))
	require.Error(t, err)
	require.Nil(t, fields)
	require.Contains(t, err.Error(), "region")
}

// TestTestIntercomConnection_DefaultAuthTest covers the nil-seam fallback
// to intercom.AuthTest (which fails fast against an unreachable host).
func TestTestIntercomConnection_DefaultAuthTest(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fail the HTTP call immediately — no live egress from unit tests
	_, _, fields, err := h.testIntercomConnection(ctx, ptrext.Of(attunev1.IntercomConnConfig{
		Region:      "us",
		AccessToken: "tok",
	}))
	require.Error(t, err)
	require.NotNil(t, fields)
}
