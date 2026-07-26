// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package customerrequest

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// TestFeedbackSourceMetaTx covers the promote-time attribution read:
// happy path, not-found mapping, and generic query failure.
func TestFeedbackSourceMetaTx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := Repo{}

	source, meta, err := repo.FeedbackSourceMetaTx(ctx, &fakeRepoTx{
		rows: []fakeRepoRow{{values: []any{"intercom", map[string]any{"intercom_contact_email": "a@b.c"}}}},
	}, "tenant-a", 42)
	require.NoError(t, err)
	require.Equal(t, "intercom", source)
	require.Equal(t, "a@b.c", meta["intercom_contact_email"])

	_, _, err = repo.FeedbackSourceMetaTx(ctx, &fakeRepoTx{
		rows: []fakeRepoRow{{err: pgx.ErrNoRows}},
	}, "tenant-a", 42)
	require.ErrorIs(t, err, ErrFeedbackNotFound)

	_, _, err = repo.FeedbackSourceMetaTx(ctx, &fakeRepoTx{
		rows: []fakeRepoRow{{err: errors.New("db boom")}},
	}, "tenant-a", 42)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrFeedbackNotFound)
}
