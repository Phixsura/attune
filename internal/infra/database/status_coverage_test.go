// ptrext:file-allow test fixtures use struct pointers for MigrationDetail.
package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBuildMigrationStatus_AppliedWithChecksum(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql", "002_users.sql", "003_data.sql"}
	appliedMap := map[int]MigrationDetail{
		1: {Version: 1, Filename: "001_init.sql", Status: "applied", Checksum: ptrext.Of("abc123")},
		2: {Version: 2, Filename: "002_users.sql", Status: "applied", Checksum: ptrext.Of("def456")},
	}

	status := buildMigrationStatus(names, appliedMap)

	require.Equal(t, 3, status.Total)
	require.Equal(t, 2, status.Applied)
	require.Equal(t, 1, status.Pending)
}

func TestBuildMigrationStatus_AppliedNilChecksum(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql"}
	appliedMap := map[int]MigrationDetail{
		1: {Version: 1, Filename: "001_init.sql", Status: "applied"},
	}

	status := buildMigrationStatus(names, appliedMap)
	require.Equal(t, 1, status.Applied)
}

func TestBuildMigrationStatus_DuplicatePrefixesInNames(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql", "001_other.sql", "002_data.sql"}
	appliedMap := map[int]MigrationDetail{}

	status := buildMigrationStatus(names, appliedMap)

	require.True(t, len(status.Duplicates) > 0, "should detect duplicate prefix 001")
}

func TestMigrationDetail_JSONMarshalFull(t *testing.T) {
	t.Parallel()

	detail := MigrationDetail{
		Version:    1,
		Filename:   "001_init.sql",
		Status:     "applied",
		AppliedAt:  ptrext.Of("2025-01-01T00:00:00Z"),
		DurationMs: ptrext.Of(42),
		AppliedBy:  ptrext.Of("v1.0.0"),
		Checksum:   ptrext.Of("abc123def456"),
	}

	data, err := json.Marshal(detail)
	require.NoError(t, err)

	var m map[string]any
	err = json.Unmarshal(data, &m)
	require.NoError(t, err)

	require.Equal(t, float64(1), m["version"])
	require.Equal(t, "001_init.sql", m["filename"])
	require.Equal(t, "applied", m["status"])
	require.Equal(t, "2025-01-01T00:00:00Z", m["applied_at"])
	require.Equal(t, float64(42), m["duration_ms"])
	require.Equal(t, "v1.0.0", m["applied_by"])
	require.Equal(t, "abc123def456", m["checksum"])
}

func TestMigrationDetail_JSONOmitsNilPointers(t *testing.T) {
	t.Parallel()

	detail := MigrationDetail{
		Version:  2,
		Filename: "002_users.sql",
		Status:   "pending",
	}

	data, err := json.Marshal(detail)
	require.NoError(t, err)

	var m map[string]any
	err = json.Unmarshal(data, &m)
	require.NoError(t, err)

	_, hasAppliedAt := m["applied_at"]
	_, hasDuration := m["duration_ms"]
	_, hasAppliedBy := m["applied_by"]
	_, hasChecksum := m["checksum"]

	require.False(t, hasAppliedAt, "applied_at should be omitted")
	require.False(t, hasDuration, "duration_ms should be omitted")
	require.False(t, hasAppliedBy, "applied_by should be omitted")
	require.False(t, hasChecksum, "checksum should be omitted")
}

func TestMigrationStatus_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	status := MigrationStatus{
		Total:   5,
		Applied: 3,
		Pending: 2,
		Migrations: []MigrationDetail{
			{Version: 1, Filename: "001_init.sql", Status: "applied"},
		},
		Elapsed: "12ms",
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded MigrationStatus
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Equal(t, 5, decoded.Total)
	require.Equal(t, 3, decoded.Applied)
	require.Equal(t, 2, decoded.Pending)
	require.Len(t, decoded.Migrations, 1)
}

func TestBuildMigrationQuery_BothContainTable(t *testing.T) {
	t.Parallel()

	for _, extended := range []bool{true, false} {
		q := buildMigrationQuery(extended)
		require.Contains(t, q, "ORDER BY")
		require.Contains(t, q, "schema_migrations_feedback")
	}
}

func TestBuildMigrationQuery_ExtendedColumns(t *testing.T) {
	t.Parallel()

	extended := buildMigrationQuery(true)
	legacy := buildMigrationQuery(false)

	require.Contains(t, extended, "duration_ms")
	require.Contains(t, extended, "applied_by")
	require.Contains(t, extended, "checksum")
	require.NotContains(t, legacy, "duration_ms")
}

func TestMigrationStatusWrappersReturnAcquireErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableMigrationPool(t)

	_, err := GetMigrationStatus(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "acquire connection") {
		t.Fatalf("GetMigrationStatus() error = %v, want acquire connection", err)
	}

	_, err = GetPendingMigrations(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "acquire connection") {
		t.Fatalf("GetPendingMigrations() error = %v, want acquire connection", err)
	}
}

func TestBuildPendingMigrations_AllAppliedYieldsEmpty(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	appliedSet := make(map[int]bool)
	for i := range names {
		appliedSet[i+1] = true
	}

	pending, err := buildPendingMigrations(names, appliedSet)
	require.NoError(t, err)
	require.Empty(t, pending, "all applied should yield zero pending")
}

func TestBuildPendingMigrations_NoneAppliedYieldsAll(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	pending, err := buildPendingMigrations(names, map[int]bool{})
	require.NoError(t, err)
	require.Len(t, pending, len(names))

	for _, mf := range pending {
		require.NotEmpty(t, mf.Name)
		require.NotEmpty(t, mf.Checksum)
		require.NotEmpty(t, mf.Body)
		require.True(t, mf.Version > 0)
	}
}

func TestBuildPendingMigrations_Partial(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.True(t, len(names) >= 3, "need at least 3 migrations for this test")

	appliedSet := map[int]bool{1: true, 2: true}

	pending, err := buildPendingMigrations(names, appliedSet)
	require.NoError(t, err)
	require.Equal(t, len(names)-2, len(pending))
	require.Equal(t, 3, pending[0].Version)
}

func TestBuildMigrationStatus_EmptyNilNames(t *testing.T) {
	t.Parallel()

	status := buildMigrationStatus(nil, map[int]MigrationDetail{})
	require.Equal(t, 0, status.Total)
	require.Equal(t, 0, status.Applied)
	require.Equal(t, 0, status.Pending)
	require.Empty(t, status.Migrations)
}

func TestBuildPendingMigrations_ChecksumStable(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	pending1, err := buildPendingMigrations(names[:1], map[int]bool{})
	require.NoError(t, err)

	pending2, err := buildPendingMigrations(names[:1], map[int]bool{})
	require.NoError(t, err)

	require.Equal(t, pending1[0].Checksum, pending2[0].Checksum)
}

func TestBuildMigrationStatus_PreservesAppliedDetails(t *testing.T) {
	t.Parallel()

	names := []string{"001_init.sql", "002_users.sql"}
	appliedMap := map[int]MigrationDetail{
		1: {
			Version:    1,
			Filename:   "001_init.sql",
			Status:     "applied",
			AppliedAt:  ptrext.Of("2025-06-01T10:00:00Z"),
			DurationMs: ptrext.Of(150),
			AppliedBy:  ptrext.Of("v0.9.0"),
			Checksum:   ptrext.Of("sha256abc"),
		},
	}

	status := buildMigrationStatus(names, appliedMap)
	require.Len(t, status.Migrations, 2)

	applied := status.Migrations[0]
	require.Equal(t, "applied", applied.Status)
	require.Equal(t, "2025-06-01T10:00:00Z", ptrext.Indirect(applied.AppliedAt))
	require.Equal(t, 150, ptrext.Indirect(applied.DurationMs))
	require.Equal(t, "v0.9.0", ptrext.Indirect(applied.AppliedBy))
	require.Equal(t, "sha256abc", ptrext.Indirect(applied.Checksum))

	pending := status.Migrations[1]
	require.Equal(t, "pending", pending.Status)
	require.Nil(t, pending.AppliedAt)
}

func TestMigrationFile_NoTxFieldSet(t *testing.T) {
	t.Parallel()

	mf := MigrationFile{
		Name:     "050_notx.sql",
		Version:  50,
		Body:     []byte("-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY ...;"),
		Checksum: "abc",
		NoTx:     true,
	}
	require.True(t, mf.NoTx)

	mf2 := MigrationFile{
		Name:     "001_init.sql",
		Version:  1,
		Body:     []byte("CREATE TABLE test();"),
		Checksum: "def",
		NoTx:     false,
	}
	require.False(t, mf2.NoTx)
}
