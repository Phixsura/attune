// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPerIndexTimeout_IsReasonable(t *testing.T) {
	t.Parallel()
	// The per-index timeout must be positive and bounded — too short
	// skips large indexes, too long lets a single index hog the drill.
	require.Greater(t, perIndexTimeout, 5*time.Second)
	require.LessOrEqual(t, perIndexTimeout, 5*time.Minute)
}

func TestBtreeIndexFilter_ExcludesSystemSchemas(t *testing.T) {
	t.Parallel()
	require.Contains(t, btreeIndexFilter, "pg_catalog")
	require.Contains(t, btreeIndexFilter, "information_schema")
}

func TestBtreeIndexFilter_OnlyRegularIndexes(t *testing.T) {
	t.Parallel()
	// relkind = 'i' is for regular indexes (excludes 'I' for partitioned).
	require.Contains(t, btreeIndexFilter, "c.relkind = 'i'")
}

func TestBtreeIndexFilter_OnlyBtreeAM(t *testing.T) {
	t.Parallel()
	require.Contains(t, btreeIndexFilter, "am.amname = 'btree'")
}

func TestBtreeIndexFilter_RequiresReadyAndValid(t *testing.T) {
	t.Parallel()
	require.Contains(t, btreeIndexFilter, "i.indisready")
	require.Contains(t, btreeIndexFilter, "i.indisvalid")
}

func TestDeepDetail_FieldNames(t *testing.T) {
	t.Parallel()
	d := deepDetail{IndexesChecked: 10, IndexesTotal: 15}
	require.Equal(t, 10, d.IndexesChecked)
	require.Equal(t, 15, d.IndexesTotal)
}
