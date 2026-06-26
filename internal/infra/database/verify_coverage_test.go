// ptrext:file-allow test-fixtures
package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestErrChecksumDrift_SingleDriftMessage(t *testing.T) {
	t.Parallel()

	err := ErrChecksumDrift{Drifted: []ChecksumDrift{
		{Version: 1, Filename: "001_init.sql", Stored: "aaa", Computed: "bbb"},
	}}
	msg := err.Error()
	require.Contains(t, msg, "001_init.sql")
	require.Contains(t, msg, "aaa")
	require.Contains(t, msg, "bbb")
}

func TestErrChecksumDrift_MultipleDriftMessage(t *testing.T) {
	t.Parallel()

	err := ErrChecksumDrift{Drifted: []ChecksumDrift{
		{Version: 1, Filename: "001_init.sql", Stored: "aaa", Computed: "bbb"},
		{Version: 2, Filename: "002_data.sql", Stored: "ccc", Computed: "ddd"},
		{Version: 3, Filename: "003_more.sql", Stored: "eee", Computed: "fff"},
	}}
	msg := err.Error()
	require.Contains(t, msg, "migration checksum drift detected")
	require.Contains(t, msg, "001_init.sql")
	require.Contains(t, msg, "003_more.sql")
}

func TestErrChecksumDrift_EmptyDriftList(t *testing.T) {
	t.Parallel()

	err := ErrChecksumDrift{}
	msg := err.Error()
	require.Contains(t, msg, "migration checksum drift detected")
}

func TestErrMissingFile_IncludesVersionAndFilename(t *testing.T) {
	t.Parallel()

	err := ErrMissingFile{Version: 42, Filename: "042_feature.sql"}
	msg := err.Error()
	require.Contains(t, msg, "42")
	require.Contains(t, msg, "042_feature.sql")
}

func TestErrMissingFile_VariousVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		version  int
		filename string
	}{
		{1, "001_init.sql"},
		{99, "099_cleanup.sql"},
		{100, "100_big.sql"},
	}
	for _, tc := range cases {
		err := ErrMissingFile{Version: tc.version, Filename: tc.filename}
		require.Contains(t, err.Error(), tc.filename)
	}
}

func TestErrManifestReorder_MessageContent(t *testing.T) {
	t.Parallel()

	err := ErrManifestReorder{
		Stored:   "abcdef123456abcdef123456abcdef123456abcdef123456abcdef123456abcdef12",
		Computed: "fedcba654321fedcba654321fedcba654321fedcba654321fedcba654321fedcba65",
	}
	msg := err.Error()
	require.Contains(t, msg, "reorder")
}

func TestChecksumDrift_FieldAccess(t *testing.T) {
	t.Parallel()

	d := ChecksumDrift{
		Version:  5,
		Filename: "005_alter.sql",
		Stored:   "stored_hash",
		Computed: "computed_hash",
	}
	require.Equal(t, 5, d.Version)
	require.Equal(t, "005_alter.sql", d.Filename)
	require.Equal(t, "stored_hash", d.Stored)
	require.Equal(t, "computed_hash", d.Computed)
}

func TestChecksumStatus_FieldAccess(t *testing.T) {
	t.Parallel()

	s := ChecksumStatus{
		Verified: 10,
		Total:    12,
		Drifted:  []ChecksumDrift{{Version: 1}, {Version: 2}},
	}
	require.Equal(t, 10, s.Verified)
	require.Equal(t, 12, s.Total)
	require.Len(t, s.Drifted, 2)
}

func TestChecksumStatus_AllVerifiedNoDrift(t *testing.T) {
	t.Parallel()

	s := ChecksumStatus{
		Verified: 50,
		Total:    50,
		Drifted:  nil,
	}
	require.Equal(t, s.Verified, s.Total)
	require.Empty(t, s.Drifted)
}

func TestErrMissingFile_FieldAccess(t *testing.T) {
	t.Parallel()

	err := ErrMissingFile{Version: 7, Filename: "007_bond.sql"}
	require.Equal(t, 7, err.Version)
	require.Equal(t, "007_bond.sql", err.Filename)
}

func TestErrManifestReorder_FieldAccess(t *testing.T) {
	t.Parallel()

	err := ErrManifestReorder{Stored: "abc", Computed: "def"}
	require.Equal(t, "abc", err.Stored)
	require.Equal(t, "def", err.Computed)
}

func TestChecksumShort_BoundaryCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{"empty_string", ""},
		{"three_chars", "abc"},
		{"twelve_chars", "abcdef123456"},
		{"thirteen_chars", "abcdef1234567"},
		{"full_sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ChecksumShort(tc.input)
			if len(tc.input) <= 12 {
				require.Equal(t, tc.input, got)
			} else {
				require.True(t, len(got) <= 15)
				require.Contains(t, got, "...")
			}
		})
	}
}

func TestChecksum_EmptyBody(t *testing.T) {
	t.Parallel()

	cs := Checksum(nil)
	require.Len(t, cs, 64, "SHA-256 hex digest is 64 chars")
}

func TestChecksum_DifferentInputsProduceDifferentOutput(t *testing.T) {
	t.Parallel()

	cs1 := Checksum([]byte("hello"))
	cs2 := Checksum([]byte("world"))
	require.NotEqual(t, cs1, cs2)
}

func TestChecksum_SameInputProducesSameOutput(t *testing.T) {
	t.Parallel()

	cs1 := Checksum([]byte("deterministic"))
	cs2 := Checksum([]byte("deterministic"))
	require.Equal(t, cs1, cs2)
}

func TestManifestHash_EmptyListProducesHash(t *testing.T) {
	t.Parallel()

	h, err := ManifestHash(nil)
	require.NoError(t, err)
	require.Len(t, h, 64)
}

func TestManifestHash_OrderSensitive(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.True(t, len(names) >= 2)

	h1, err := ManifestHash(names[:2])
	require.NoError(t, err)

	reversed := []string{names[1], names[0]}
	h2, err := ManifestHash(reversed)
	require.NoError(t, err)

	require.NotEqual(t, h1, h2, "different order should produce different hash")
}

func TestComputeChecksum_ExistingFile(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	cs, err := ComputeChecksum(names[0])
	require.NoError(t, err)
	require.Len(t, cs, 64)
}

func TestComputeChecksum_NonexistentFile(t *testing.T) {
	t.Parallel()

	_, err := ComputeChecksum("nonexistent_file.sql")
	require.Error(t, err)
}

func TestComputeChecksum_Stable(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	cs1, err := ComputeChecksum(names[0])
	require.NoError(t, err)
	cs2, err := ComputeChecksum(names[0])
	require.NoError(t, err)
	require.Equal(t, cs1, cs2)
}

func TestLockConstants_Stability(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, int64(0), LockAuditPruner)
	require.NotEqual(t, int64(0), LockIdempotencyPruner)
	require.NotEqual(t, int64(0), LockMCPPruner)
	require.NotEqual(t, LockAuditPruner, LockIdempotencyPruner)
	require.NotEqual(t, LockAuditPruner, LockMCPPruner)
	require.NotEqual(t, LockIdempotencyPruner, LockMCPPruner)
}

func TestFnv64_StableOutput(t *testing.T) {
	t.Parallel()

	for i := 0; i < 50; i++ {
		require.Equal(t, fnv64("test"), fnv64("test"))
	}
}

func TestFnv64_DifferentKeys(t *testing.T) {
	t.Parallel()

	h1 := fnv64("audit-pruner")
	h2 := fnv64("idempotency-pruner")
	h3 := fnv64("mcp-pruner")

	require.NotEqual(t, h1, h2)
	require.NotEqual(t, h1, h3)
	require.NotEqual(t, h2, h3)
}

func TestFnv64_EmptyInput(t *testing.T) {
	t.Parallel()

	h := fnv64("")
	require.NotEqual(t, int64(0), h, "empty string should still produce non-zero hash")
}

func TestAdvisoryLock_NilSafeRelease(t *testing.T) {
	t.Parallel()

	var lock *AdvisoryLock
	err := lock.Release(t.Context())
	require.NoError(t, err)
}

func TestCheckPgvectorVersion_Accepted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		version string
		wantErr bool
	}{
		{"0.5.0", false},
		{"0.5.1", false},
		{"0.6.0", false},
		{"1.0.0", false},
		{"10.20.30", false},
		{"0.4.0", true},
		{"0.4.99", true},
		{"0.3.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			err := checkPgvectorVersion(tc.version)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPgvectorSentinelErrors_NonEmpty(t *testing.T) {
	t.Parallel()

	require.NotNil(t, ErrPgvectorNotInstalled)
	require.NotNil(t, ErrPgvectorVersionTooOld)
	require.NotEmpty(t, ErrPgvectorNotInstalled.Error())
	require.NotEmpty(t, ErrPgvectorVersionTooOld.Error())
}

func TestConfirmLarkDelete_ConfirmedBypassesDB(t *testing.T) {
	t.Parallel()

	err := ConfirmLarkDelete(t.Context(), nil, true)
	require.NoError(t, err)
}

func TestDestructiveMigrationGuard_SentinelNotEmpty(t *testing.T) {
	t.Parallel()

	require.NotNil(t, ErrDestructiveMigrationGuard)
	require.NotEmpty(t, ErrDestructiveMigrationGuard.Error())
}

func TestMigrationCount_GreaterThanZero(t *testing.T) {
	t.Parallel()

	count := MigrationCount()
	require.True(t, count > 0, "should have at least one migration")
}

func TestIsNoTxMigration_VariousInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"with_directive", "-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY;", true},
		{"without_directive", "CREATE TABLE test();", false},
		{"empty_body", "", false},
		{"comment_only", "-- just a comment", false},
		{"directive_not_first_line", "CREATE TABLE test();\n-- migrate:no-transaction", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isNoTxMigration([]byte(tc.body)))
		})
	}
}

func TestErrDirtyMigration_MessageContent(t *testing.T) {
	t.Parallel()

	err := ErrDirtyMigration{Version: 42, Filename: "042_feature.sql"}
	msg := err.Error()
	require.Contains(t, msg, "42")
	require.Contains(t, msg, "042_feature.sql")
}

func TestErrDirtyMigration_FieldValues(t *testing.T) {
	t.Parallel()

	err := ErrDirtyMigration{Version: 10, Filename: "010_test.sql"}
	require.Equal(t, 10, err.Version)
	require.Equal(t, "010_test.sql", err.Filename)
}

func TestRecordMigrationSQL_HasInsert(t *testing.T) {
	t.Parallel()

	sql := recordMigrationSQL()
	require.Contains(t, sql, "INSERT")
	require.Contains(t, sql, "schema_migrations_feedback")
}

func TestMarkMigrationStartedSQL_HasInsert(t *testing.T) {
	t.Parallel()

	sql := markMigrationStartedSQL()
	require.Contains(t, sql, "INSERT")
	require.Contains(t, sql, trackerTable)
}

func TestMarkMigrationCompleteSQL_HasUpdate(t *testing.T) {
	t.Parallel()

	sql := markMigrationCompleteSQL()
	require.Contains(t, sql, "UPDATE")
	require.Contains(t, sql, trackerTable)
}

func TestRecordMigrationLegacySQL_HasInsert(t *testing.T) {
	t.Parallel()

	sql := recordMigrationLegacySQL()
	require.Contains(t, sql, "INSERT")
}

func TestMigrationLockKey_NonZero(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, int64(0), migrationLockKey)
}

func TestLoadMigrationNames_AllSorted(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	for i := 1; i < len(names); i++ {
		require.True(t, names[i-1] < names[i], "names should be sorted: %s >= %s", names[i-1], names[i])
	}
}

func TestLoadMigrationNames_SQLSuffix(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)

	for _, name := range names {
		require.True(t, len(name) > 4 && name[len(name)-4:] == ".sql",
			"migration %q should end with .sql", name)
	}
}

func TestLoadMigrationNames_NumericPrefix(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)

	for _, name := range names {
		require.True(t, len(name) >= 4, "name too short: %s", name)
		prefix := name[:3]
		for _, c := range prefix {
			require.True(t, c >= '0' && c <= '9', "prefix char %c not a digit in %s", c, name)
		}
	}
}

func TestDetectDuplicatePrefixes_Clean(t *testing.T) {
	t.Parallel()

	err := DetectDuplicatePrefixes([]string{"001_a.sql", "002_b.sql", "003_c.sql"})
	require.NoError(t, err)
}

func TestDetectDuplicatePrefixes_WithCollision(t *testing.T) {
	t.Parallel()

	err := DetectDuplicatePrefixes([]string{"001_a.sql", "001_b.sql", "002_c.sql"})
	require.Error(t, err)

	var dupErr ErrDuplicatePrefix
	require.ErrorAs(t, err, &dupErr)
	require.Len(t, dupErr.Duplicates, 1)
	require.Equal(t, 1, dupErr.Duplicates[0].Prefix)
}

func TestDetectDuplicatePrefixes_EmptyInput(t *testing.T) {
	t.Parallel()

	err := DetectDuplicatePrefixes(nil)
	require.NoError(t, err)
}

func TestCountSemicolons_Various(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty_input", "", 0},
		{"no_semicolons_at_all", "SELECT 1", 0},
		{"single_semicolon", "SELECT 1;", 1},
		{"two_semicolons_found", "SELECT 1; SELECT 2;", 2},
		{"semicolon_in_comment", "-- SELECT 1;\nSELECT 2;", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, countSemicolons([]byte(tc.body)))
		})
	}
}

func TestVerifyNoTxDirectives_EmbeddedMigrations(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)
	err = VerifyNoTxDirectives(names)
	require.NoError(t, err, "real migrations should pass no-tx verification")
}

func TestErrDuplicatePrefix_MessageContent(t *testing.T) {
	t.Parallel()

	err := ErrDuplicatePrefix{Duplicates: []DuplicatePrefix{
		{Prefix: 1, Files: []string{"001_a.sql", "001_b.sql"}},
	}}
	msg := err.Error()
	require.Contains(t, msg, "1")
	require.Contains(t, msg, "001_a.sql")
	require.Contains(t, msg, "001_b.sql")
}

func TestDuplicatePrefix_FieldValues(t *testing.T) {
	t.Parallel()

	dp := DuplicatePrefix{
		Prefix: 42,
		Files:  []string{"042_a.sql", "042_b.sql"},
	}
	require.Equal(t, 42, dp.Prefix)
	require.Len(t, dp.Files, 2)
}

func TestNoTxViolation_FieldValues(t *testing.T) {
	t.Parallel()

	v := NoTxViolation{
		Filename: "050_concurrent.sql",
		Reason:   "multiple statements",
	}
	require.Equal(t, "050_concurrent.sql", v.Filename)
	require.Equal(t, "multiple statements", v.Reason)
}

func TestErrNoTxViolation_SingleViolationMessage(t *testing.T) {
	t.Parallel()

	err := ErrNoTxViolation{Violations: []NoTxViolation{
		{Filename: "050_concurrent.sql", Reason: "multi-statement"},
	}}
	msg := err.Error()
	require.Contains(t, msg, "050_concurrent.sql")
	require.Contains(t, msg, "multi-statement")
}

func TestErrNoTxViolation_TwoViolationsMessage(t *testing.T) {
	t.Parallel()

	err := ErrNoTxViolation{Violations: []NoTxViolation{
		{Filename: "a.sql", Reason: "r1"},
		{Filename: "b.sql", Reason: "r2"},
	}}
	msg := err.Error()
	require.Contains(t, msg, "2")
	require.Contains(t, msg, "a.sql")
	require.Contains(t, msg, "b.sql")
}

func TestPoolHealth_ZeroValues(t *testing.T) {
	t.Parallel()

	h := PoolHealth{}
	require.Equal(t, int32(0), h.TotalConns)
	require.Equal(t, int32(0), h.AcquiredConns)
	require.Equal(t, int32(0), h.IdleConns)
	require.Equal(t, int32(0), h.ConstructingConns)
	require.Equal(t, int32(0), h.MaxConns)
	require.False(t, h.Healthy)
}

func TestPoolHealth_UsingPtrExt(t *testing.T) {
	t.Parallel()

	hp := ptrext.Of(PoolHealth{TotalConns: 5, Healthy: true})
	require.Equal(t, int32(5), hp.TotalConns)
	require.True(t, hp.Healthy)
}
