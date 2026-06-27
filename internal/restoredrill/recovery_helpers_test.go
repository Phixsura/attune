// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRecoveryDurations_Both(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	opts := Options{
		BackupTakenAt:   ptrext.Of(time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)),
		RestoreDuration: ptrext.Of(5 * time.Minute),
	}

	rpo, rto := opts.recoveryDurations(start)
	require.NotNil(t, rpo)
	require.Equal(t, 2*time.Hour, ptrext.Indirect(rpo))
	require.NotNil(t, rto)
	require.Equal(t, 5*time.Minute, ptrext.Indirect(rto))
}

func TestRecoveryDurations_NeitherSet(t *testing.T) {
	t.Parallel()

	opts := Options{}
	rpo, rto := opts.recoveryDurations(time.Now())
	require.Nil(t, rpo)
	require.Nil(t, rto)
}

func TestRecoveryDurations_FutureBackupClamped(t *testing.T) {
	t.Parallel()

	// BackupTakenAt is AFTER startedAt (clock skew). RPO should be clamped to 0.
	start := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	opts := Options{
		BackupTakenAt: ptrext.Of(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)),
	}

	rpo, rto := opts.recoveryDurations(start)
	require.NotNil(t, rpo)
	require.Equal(t, time.Duration(0), ptrext.Indirect(rpo))
	require.Nil(t, rto)
}

func TestRecoveryDurations_OnlyRestore(t *testing.T) {
	t.Parallel()

	opts := Options{
		RestoreDuration: ptrext.Of(10 * time.Minute),
	}

	rpo, rto := opts.recoveryDurations(time.Now())
	require.Nil(t, rpo)
	require.NotNil(t, rto)
	require.Equal(t, 10*time.Minute, ptrext.Indirect(rto))
}
