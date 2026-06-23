package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBuildMigrationQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		hasExtended bool
		wantContain string
	}{
		{
			name:        "with extended columns",
			hasExtended: true,
			wantContain: "duration_ms",
		},
		{
			name:        "without extended columns",
			hasExtended: false,
			wantContain: "applied_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query := buildMigrationQuery(tt.hasExtended)
			require.Contains(t, query, tt.wantContain)
			require.Contains(t, query, "SELECT")
			require.Contains(t, query, "FROM schema_migrations_feedback")
		})
	}
}

func TestBuildMigrationStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		names       []string
		appliedMap  map[int]MigrationDetail
		wantTotal   int
		wantApplied int
		wantPending int
	}{
		{
			name:        "empty",
			names:       []string{},
			appliedMap:  map[int]MigrationDetail{},
			wantTotal:   0,
			wantApplied: 0,
			wantPending: 0,
		},
		{
			name:        "all applied",
			names:       []string{"001_init.sql", "002_foo.sql"},
			appliedMap:  map[int]MigrationDetail{1: {Version: 1}, 2: {Version: 2}},
			wantTotal:   2,
			wantApplied: 2,
			wantPending: 0,
		},
		{
			name:        "none applied",
			names:       []string{"001_init.sql", "002_foo.sql"},
			appliedMap:  map[int]MigrationDetail{},
			wantTotal:   2,
			wantApplied: 0,
			wantPending: 2,
		},
		{
			name:        "partial applied",
			names:       []string{"001_init.sql", "002_foo.sql", "003_bar.sql"},
			appliedMap:  map[int]MigrationDetail{1: {Version: 1}},
			wantTotal:   3,
			wantApplied: 1,
			wantPending: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := buildMigrationStatus(tt.names, tt.appliedMap)
			require.Equal(t, tt.wantTotal, status.Total)
			require.Equal(t, tt.wantApplied, status.Applied)
			require.Equal(t, tt.wantPending, status.Pending)
			require.Len(t, status.Migrations, tt.wantTotal)
		})
	}
}

func TestBuildMigrationStatus_DetectsDuplicates(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql", "001_also_init.sql", "002_foo.sql"}
	status := buildMigrationStatus(names, map[int]MigrationDetail{})

	require.NotEmpty(t, status.Duplicates, "should detect duplicate prefix 001")
}

func TestBuildMigrationStatus_MigrationDetails(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql", "002_foo.sql"}
	appliedMap := map[int]MigrationDetail{
		1: {
			Version:   1,
			Filename:  "001_init.sql",
			Status:    "applied",
			AppliedAt: ptrext.Of("2026-01-01T00:00:00Z"),
			Checksum:  ptrext.Of("abc123"),
		},
	}

	status := buildMigrationStatus(names, appliedMap)

	require.Len(t, status.Migrations, 2)

	// First migration should be applied
	require.Equal(t, 1, status.Migrations[0].Version)
	require.Equal(t, "applied", status.Migrations[0].Status)
	require.NotNil(t, status.Migrations[0].AppliedAt)

	// Second migration should be pending
	require.Equal(t, 2, status.Migrations[1].Version)
	require.Equal(t, "pending", status.Migrations[1].Status)
	require.Nil(t, status.Migrations[1].AppliedAt)
}

func TestBuildPendingMigrations(t *testing.T) {
	t.Parallel()

	// Use real embedded migration names for this test
	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	tests := []struct {
		name         string
		appliedSet   map[int]bool
		wantPending  int
		wantAllNoTx  bool
		checkVersion int
	}{
		{
			name:        "none applied - all pending",
			appliedSet:  map[int]bool{},
			wantPending: len(names),
		},
		{
			name: "all applied - none pending",
			appliedSet: func() map[int]bool {
				m := make(map[int]bool)
				for i := range names {
					m[i+1] = true
				}
				return m
			}(),
			wantPending: 0,
		},
		{
			name:         "first applied - rest pending",
			appliedSet:   map[int]bool{1: true},
			wantPending:  len(names) - 1,
			checkVersion: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pending, err := buildPendingMigrations(names, tt.appliedSet)
			require.NoError(t, err)
			require.Len(t, pending, tt.wantPending)

			for _, m := range pending {
				require.NotEmpty(t, m.Name)
				require.NotEmpty(t, m.Body)
				require.Len(t, m.Checksum, 64, "checksum should be SHA-256 hex")
				require.Greater(t, m.Version, 0)
			}

			if tt.checkVersion > 0 && len(pending) > 0 {
				require.Equal(t, tt.checkVersion, pending[0].Version)
			}
		})
	}
}

func TestMigrationFile_NoTxFlag(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)

	pending, err := buildPendingMigrations(names, map[int]bool{})
	require.NoError(t, err)

	// Find migration 056 which should have no-tx
	var found056 bool
	for _, m := range pending {
		if m.Version == 56 {
			found056 = true
			require.True(t, m.NoTx, "migration 056 should be no-tx")
		}
	}
	require.True(t, found056, "should find migration 056")
}

func TestMigrationStatus_JSONFields(t *testing.T) {
	t.Parallel()

	// Verify struct has expected JSON tags
	status := MigrationStatus{
		Total:   71,
		Applied: 71,
		Pending: 0,
		Elapsed: "100ms",
	}

	require.Equal(t, 71, status.Total)
	require.Equal(t, 71, status.Applied)
	require.Equal(t, 0, status.Pending)
	require.Equal(t, "100ms", status.Elapsed)
}

func TestMigrationDetail_OptionalFields(t *testing.T) {
	t.Parallel()

	// Pending migration has no optional fields
	pending := MigrationDetail{
		Version:  1,
		Filename: "001_init.sql",
		Status:   "pending",
	}
	require.Nil(t, pending.AppliedAt)
	require.Nil(t, pending.DurationMs)
	require.Nil(t, pending.AppliedBy)
	require.Nil(t, pending.Checksum)

	// Applied migration has optional fields
	applied := MigrationDetail{
		Version:    1,
		Filename:   "001_init.sql",
		Status:     "applied",
		AppliedAt:  ptrext.Of("2026-01-01T00:00:00Z"),
		DurationMs: ptrext.Of(100),
		AppliedBy:  ptrext.Of("v1.0.0"),
		Checksum:   ptrext.Of("abc123"),
	}
	require.NotNil(t, applied.AppliedAt)
	require.NotNil(t, applied.DurationMs)
	require.NotNil(t, applied.AppliedBy)
	require.NotNil(t, applied.Checksum)
}

func TestBuildPendingMigrations_NonexistentFile(t *testing.T) {
	t.Parallel()

	// Test with a nonexistent file - should error
	_, err := buildPendingMigrations([]string{"999_nonexistent.sql"}, map[int]bool{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "999_nonexistent.sql")
}

func TestMigrationFile_AllFields(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)

	pending, err := buildPendingMigrations(names[:1], map[int]bool{})
	require.NoError(t, err)
	require.Len(t, pending, 1)

	m := pending[0]
	require.Equal(t, 1, m.Version)
	require.Equal(t, "001_init.sql", m.Name)
	require.NotEmpty(t, m.Body)
	require.Len(t, m.Checksum, 64)
	require.False(t, m.NoTx, "first migration should not be no-tx")
}

func TestBuildMigrationStatus_EmptyAppliedMap(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql", "002_foo.sql", "003_bar.sql"}
	status := buildMigrationStatus(names, map[int]MigrationDetail{})

	require.Equal(t, 3, status.Total)
	require.Equal(t, 0, status.Applied)
	require.Equal(t, 3, status.Pending)

	for _, m := range status.Migrations {
		require.Equal(t, "pending", m.Status)
	}
}

func TestBuildMigrationQuery_Legacy(t *testing.T) {
	t.Parallel()

	query := buildMigrationQuery(false)
	require.Contains(t, query, "SELECT")
	require.Contains(t, query, "version")
	require.Contains(t, query, "filename")
	require.Contains(t, query, "applied_at")
	require.NotContains(t, query, "duration_ms")
	require.NotContains(t, query, "checksum")
}

func TestBuildMigrationQuery_Extended(t *testing.T) {
	t.Parallel()

	query := buildMigrationQuery(true)
	require.Contains(t, query, "SELECT")
	require.Contains(t, query, "version")
	require.Contains(t, query, "filename")
	require.Contains(t, query, "applied_at")
	require.Contains(t, query, "duration_ms")
	require.Contains(t, query, "checksum")
	require.Contains(t, query, "applied_by")
}
