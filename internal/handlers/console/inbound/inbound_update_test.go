// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

// updSourceRepo layers rename recording over covSourceRepo. Config
// persistence goes through the secretlock tx (updTx below), matching
// the production write path.
type updSourceRepo struct {
	covSourceRepo
	renamedTo string
	renameErr error
}

func (f *updSourceRepo) UpdateName(_ context.Context, _ string, name string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	f.renamedTo = name
	return nil
}

// updTx extends the shared fake tx with the update path's two calls:
// QueryRow(SELECT config ... FOR UPDATE) returning the stored blob, and
// Exec(UPDATE ... SET config) whose blob argument it captures.
type updTx struct {
	covSlackTx
	storedCfg  []byte
	updatedCfg []byte
	updateErr  error
}

func (t *updTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return updRow{cfg: t.storedCfg}
}

func (t *updTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.updateErr != nil {
		return pgconn.CommandTag{}, t.updateErr
	}
	if len(args) >= 2 {
		if blob, ok := args[1].([]byte); ok {
			t.updatedCfg = blob
		}
	}
	t.lastSQL = sql
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type updRow struct{ cfg []byte }

func (r updRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		if p, ok := dest[0].(*[]byte); ok {
			*p = r.cfg // ptrext:allow out-parameter
			return nil
		}
	}
	return errors.New("unexpected scan shape")
}

func intercomBlob(t *testing.T, secrets inbound.SecretStore, region, workspace string) []byte {
	t.Helper()
	tokenEnc, err := secrets.Encrypt([]byte("stored-token"))
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]any{
		"version":                1,
		"region":                 region,
		"access_token_encrypted": tokenEnc,
		"workspace_id":           workspace,
		"start_from":             "full",
		"max_detail_fetches":     40,
		"sync_cursor":            "cursor-123",
		"sync_stats":             map[string]any{"conversations_synced": 7, "backfill_done": true},
	})
	require.NoError(t, err)
	blob, err := secrets.Encrypt(raw)
	require.NoError(t, err)
	return blob
}

const updSrcID = "00000000-0000-0000-0000-00000000c0de"

func updHandler(t *testing.T, repo *updSourceRepo, authTest intercomAuthTestFn) (*Handler, *updTx) {
	t.Helper()
	secrets := inboundtest.FakeSecrets{}
	blob := intercomBlob(t, secrets, "eu", "ws-9")
	repo.getSrc = inbound.Source{
		ID: updSrcID, TenantID: "tenant-1", Channel: channelIntercom,
		Name: "Old Name", Slug: "old-name", Enabled: true,
		Config: blob,
	}
	tx := ptrext.Of(updTx{storedCfg: blob})
	h := ptrext.Of(Handler{
		sources:          repo,
		secrets:          secrets,
		intercomAuthTest: authTest,
		intercomWithTx: func(ctx context.Context, _ *pgxpool.Pool, _ bool, fn func(context.Context, secretlock.Tx) error) error {
			return fn(ctx, tx)
		},
	})
	return h, tx
}

func updCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Auth: ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})
}

func TestUpdate_RenameAndFilters(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(updSourceRepo{})
	h, tx := updHandler(t, repo, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-9"}, nil))

	res, err := h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id:   updSrcID,
		Name: ptrext.Of("New Name"),
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
			FilterTags:        []string{"bug"},
			FilterExcludeTags: []string{"spam"},
			StartFrom:         ptrext.Of("full"),
			MaxDetailFetches:  ptrext.Of(int32(25)),
		}),
	}))
	require.NoError(t, err)
	require.Equal(t, 200, res.Status)
	require.Equal(t, "New Name", repo.renamedTo)
	require.NotNil(t, tx.updatedCfg)

	// Sync state survives; filters and budget landed; token unchanged.
	summary, err := intercom.DecodeConnSummary(tx.updatedCfg, inboundtest.FakeSecrets{})
	require.NoError(t, err)
	require.Equal(t, []string{"bug"}, summary.FilterTags)
	require.Equal(t, []string{"spam"}, summary.FilterExcludeTags)
	require.Equal(t, 25, summary.MaxDetailFetches)
	require.Equal(t, "ws-9", summary.WorkspaceID)
	region, token, err := intercom.StoredRegionAndToken(tx.updatedCfg, inboundtest.FakeSecrets{})
	require.NoError(t, err)
	require.Equal(t, "eu", region)
	require.Equal(t, "stored-token", string(token))
	intercom.WipeToken(token)
}

// TestUpdate_OmittedFieldsPreserved locks the PATCH contract: absent
// optional scalars keep their stored values — a token-only rotation
// must not flip start_from or reset the detail budget.
func TestUpdate_OmittedFieldsPreserved(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(updSourceRepo{})
	h, tx := updHandler(t, repo, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-9"}, nil))

	res, err := h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id: updSrcID,
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
			AccessToken: "rotated-token",
			// start_from / max_detail_fetches deliberately absent.
		}),
	}))
	require.NoError(t, err)
	require.Equal(t, 200, res.Status)
	summary, err := intercom.DecodeConnSummary(tx.updatedCfg, inboundtest.FakeSecrets{})
	require.NoError(t, err)
	require.Equal(t, "full", summary.StartFrom, "omitted start_from must keep stored value")
	require.Equal(t, 40, summary.MaxDetailFetches, "omitted budget must keep stored value")
}

func TestUpdate_TokenRotationPinnedToWorkspace(t *testing.T) {
	t.Parallel()

	// Same workspace → rotation accepted and persisted.
	repo := ptrext.Of(updSourceRepo{})
	h, tx := updHandler(t, repo, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-9"}, nil))
	res, err := h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id: updSrcID,
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
			AccessToken: "rotated-token",
		}),
	}))
	require.NoError(t, err)
	require.Equal(t, 200, res.Status)
	_, token, err := intercom.StoredRegionAndToken(tx.updatedCfg, inboundtest.FakeSecrets{})
	require.NoError(t, err)
	require.Equal(t, "rotated-token", string(token))
	intercom.WipeToken(token)

	// Different workspace → rejected before persisting anything.
	repo2 := ptrext.Of(updSourceRepo{})
	h2, tx2 := updHandler(t, repo2, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-OTHER"}, nil))
	_, err = h2.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id: updSrcID,
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{
			AccessToken: "foreign-token",
		}),
	}))
	require.Error(t, err)
	de := mustDispatcherError(t, err)
	require.Contains(t, de.Message, "different Intercom workspace")
	require.Nil(t, tx2.updatedCfg)
	require.Empty(t, repo2.renamedTo)
}

func TestUpdate_ValidationLegs(t *testing.T) {
	t.Parallel()

	// Region change rejected.
	repo := ptrext.Of(updSourceRepo{})
	h, _ := updHandler(t, repo, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-9"}, nil))
	_, err := h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id:             updSrcID,
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{Region: "us"}),
	}))
	require.Error(t, err)
	require.Contains(t, mustDispatcherError(t, err).Message, "region is immutable")

	// Empty update rejected.
	_, err = h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{Id: updSrcID}))
	require.Error(t, err)
	require.Contains(t, mustDispatcherError(t, err).Message, "nothing to update")

	// Over-long name rejected (same 200-char cap as the create path).
	long := strings.Repeat("x", 201)
	_, err = h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id:   updSrcID,
		Name: ptrext.Of(long),
	}))
	require.Error(t, err)
	require.Contains(t, mustDispatcherError(t, err).Message, "200 characters")

	// intercom_config on a non-intercom source rejected.
	repoWh := ptrext.Of(updSourceRepo{})
	hWh, _ := updHandler(t, repoWh, nil)
	repoWh.getSrc.Channel = "webhook"
	_, err = hWh.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id:             updSrcID,
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{FilterTags: []string{"x"}}),
	}))
	require.Error(t, err)
	require.Contains(t, mustDispatcherError(t, err).Message, "only applies to intercom")

	// Invalid state filter propagates the validation error.
	repoBad := ptrext.Of(updSourceRepo{})
	hBad, _ := updHandler(t, repoBad, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-9"}, nil))
	_, err = hBad.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id:             updSrcID,
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{FilterStates: []string{"bogus"}}),
	}))
	require.Error(t, err)
}

// TestUpdate_ValidationRunsBeforeRename locks the no-partial-write
// contract: a request combining a rename with an invalid config leg
// must not persist the rename.
func TestUpdate_ValidationRunsBeforeRename(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(updSourceRepo{})
	h, tx := updHandler(t, repo, stubIntercomAuth(intercom.AccountInfo{WorkspaceID: "ws-9"}, nil))
	_, err := h.Update(updCtx(), ptrext.Of(attunev1.UpdateInboundSourceRequest{
		Id:             updSrcID,
		Name:           ptrext.Of("Should Not Persist"),
		IntercomConfig: ptrext.Of(attunev1.IntercomConnConfig{FilterStates: []string{"bogus"}}),
	}))
	require.Error(t, err)
	require.Empty(t, repo.renamedTo, "rename must not persist when config validation fails")
	require.Nil(t, tx.updatedCfg)
}
