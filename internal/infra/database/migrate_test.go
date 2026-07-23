package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrDirtyMigration_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrDirtyMigration
		contains []string
	}{
		{
			name: "includes version and filename",
			err:  ErrDirtyMigration{Version: 42, Filename: "042_add_indexes.sql"},
			contains: []string{
				"dirty migration detected",
				"version 42",
				"042_add_indexes.sql",
				"started but did not complete",
			},
		},
		{
			name: "includes recovery options with correct version",
			err:  ErrDirtyMigration{Version: 7, Filename: "007_schema.sql"},
			contains: []string{
				"Recovery options:",
				"--version 7",
				"WHERE version = 7",
			},
		},
		{
			name: "version zero edge case",
			err:  ErrDirtyMigration{Version: 0, Filename: "000_init.sql"},
			contains: []string{
				"version 0",
				"--version 0",
			},
		},
		{
			name: "large version number",
			err:  ErrDirtyMigration{Version: 999, Filename: "999_large.sql"},
			contains: []string{
				"version 999",
				"--version 999",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if msg == "" {
				t.Fatal("error message should not be empty")
			}
			for _, substr := range tc.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("error message missing %q\ngot: %s", substr, msg)
				}
			}
		})
	}
}

func TestIsNoTxMigration(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"directive on first line", "-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY ...;\n", true},
		{"no directive", "ALTER TABLE user_feedback ADD COLUMN x TEXT;\n", false},
		{
			"mention only in a later comment must NOT flip it",
			"ALTER TABLE t ADD COLUMN x TEXT;\n-- note: migrate:no-transaction is not used here\n",
			false,
		},
		{"single line with directive, no newline", "-- migrate:no-transaction", true},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNoTxMigration([]byte(c.body)); got != c.want {
				t.Fatalf("isNoTxMigration(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestMigrationExecutionBodyStripsOuterTransactionEnvelope(t *testing.T) {
	body := []byte(`-- legacy migration with explicit transaction wrapper

BEGIN;
CREATE TABLE IF NOT EXISTS example_items (
    id TEXT PRIMARY KEY
);
COMMIT;
`)

	got := string(migrationExecutionBody(body))

	require.Contains(t, got, "legacy migration")
	require.Contains(t, got, "CREATE TABLE IF NOT EXISTS example_items")
	require.NotContains(t, got, "\nBEGIN;\n")
	require.NotContains(t, got, "\nCOMMIT;\n")
}

func TestMigrationExecutionBodyKeepsPLpgSQLBlocks(t *testing.T) {
	body := []byte(`DO $$
BEGIN
  IF NOT EXISTS (SELECT 1) THEN
    RAISE NOTICE 'ok';
  END IF;
END $$;
`)

	got := string(migrationExecutionBody(body))

	require.Equal(t, string(body), got)
	require.Contains(t, got, "\nBEGIN\n")
	require.Contains(t, got, "END $$;")
}

func TestMigrationExecutionBodyKeepsOnlyInnerPLpgSQLBlocksWhenWrapped(t *testing.T) {
	body := []byte(`BEGIN;
DO $$
BEGIN
  RAISE NOTICE 'inside';
END $$;
COMMIT;
`)

	got := string(migrationExecutionBody(body))

	require.NotContains(t, got, "BEGIN;\nDO")
	require.Contains(t, got, "DO $$\nBEGIN\n")
	require.Contains(t, got, "END $$;")
	require.NotContains(t, got, "\nCOMMIT;")
}

func TestMigrationExecutionBodyStripsCaseInsensitiveLegacyEnvelope(t *testing.T) {
	body := []byte(`-- old hand-written migration

 begin  ;
ALTER TABLE example_items ADD COLUMN title TEXT;
 commit ;
`)

	got := string(migrationExecutionBody(body))

	require.Contains(t, got, "-- old hand-written migration")
	require.Contains(t, got, "ALTER TABLE example_items")
	require.NotContains(t, strings.ToLower(got), "begin")
	require.NotContains(t, strings.ToLower(got), "commit")
}

func TestMigrationExecutionBodyKeepsUnpairedTransactionControlLines(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "begin without terminal commit",
			body: "BEGIN;\nCREATE TABLE example_items (id TEXT PRIMARY KEY);\n",
		},
		{
			name: "commit without opening begin",
			body: "CREATE TABLE example_items (id TEXT PRIMARY KEY);\nCOMMIT;\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.body, string(migrationExecutionBody([]byte(tt.body))))
		})
	}
}

func TestMigrationExecutionBodyNormalizesEmbeddedLegacyWrappers(t *testing.T) {
	t.Parallel()

	names, err := LoadMigrationNames()
	require.NoError(t, err)

	normalized := 0
	for _, name := range names {
		body, err := migrationFS.ReadFile("migrations/" + name)
		require.NoError(t, err)
		execBody := migrationExecutionBody(body)
		if string(execBody) == string(body) {
			continue
		}
		normalized++
		require.False(t, isTransactionControlLine([]byte(firstEffectiveLine(execBody)), "begin"), name)
		require.False(t, isTransactionControlLine([]byte(lastEffectiveLine(execBody)), "commit"), name)
		require.False(t, isTransactionControlLine([]byte(lastEffectiveLine(execBody)), "rollback"), name)
	}

	require.GreaterOrEqual(t, normalized, 1)
}

func TestMigrationCount(t *testing.T) {
	t.Parallel()

	count := MigrationCount()
	require.Greater(t, count, 0, "should have at least one migration")
	require.Equal(t, 114, count, "should match current migration count")
}

func firstEffectiveLine(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "--") {
			return line
		}
	}
	return ""
}

func lastEffectiveLine(body []byte) string {
	lines := strings.Split(string(body), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "--") {
			return line
		}
	}
	return ""
}

func TestPublicVisibilityMigrationAllowsPublicModerationAuditActions(t *testing.T) {
	t.Parallel()

	body, err := migrationFS.ReadFile("migrations/106_public_visibility_moderation.sql")
	require.NoError(t, err)
	sql := string(body)
	for _, action := range []string{
		"public_policy.update",
		"public_request_profile.upsert",
		"moderation.approve",
		"moderation.reject",
		"moderation.hide",
		"moderation.mark_spam",
		"moderation.restore",
	} {
		require.Contains(t, sql, "'"+action+"'", "migration should allow %s audit action", action)
	}
}

func TestExternalSyncMigrationAllowsAllExternalSyncAuditActions(t *testing.T) {
	t.Parallel()

	body, err := migrationFS.ReadFile("migrations/105_external_sync_framework.sql")
	require.NoError(t, err)
	sql := string(body)
	for _, action := range []string{
		"external_connection.create",
		"external_connection.update",
		"external_connection.delete",
		"external_connection.qualify",
		"external_connection.resume",
		"external_connection.test",
		"external_sync_mapping.update",
		"external_sync_cursor.reset",
		"external_sync_run.request",
		"external_sync_run.backfill",
		"external_sync_run.retry",
		"external_sync_failure.retry",
		"external_sync_conflict.resolve",
		"external_sync_event.replay",
	} {
		require.Contains(t, sql, "'"+action+"'", "audit action must be accepted by chk_audit_action_value")
	}
}

func TestGitHubBidirectionalSyncMigrationAllowsCreateIssueAuditAction(t *testing.T) {
	t.Parallel()

	body, err := migrationFS.ReadFile("migrations/112_github_bidirectional_issue_sync.sql")
	require.NoError(t, err)
	require.Contains(t, string(body), "'customer_request.create_github_issue'")
}

func TestExternalProviderInstallationsMigrationAllowsAuditActions(t *testing.T) {
	t.Parallel()

	body, err := migrationFS.ReadFile("migrations/113_external_provider_installations.sql")
	require.NoError(t, err)
	sql := string(body)
	for _, action := range []string{
		"external_provider_installation.create",
		"external_provider_installation.delete",
		"external_provider_installation.qualify",
		"external_provider_installation.resources_select",
	} {
		require.Contains(t, sql, "'"+action+"'", "audit action must be accepted by chk_audit_action_value")
	}
}

func TestCustomerRequestDeliveryArtifactsMigrationDefinesProjectionContract(t *testing.T) {
	t.Parallel()

	body, err := migrationFS.ReadFile("migrations/114_customer_request_delivery_artifacts.sql")
	require.NoError(t, err)
	sql := string(body)
	for _, value := range []string{
		"customer_request_delivery_artifacts",
		"'pull_request'",
		"'commit'",
		"'deployment'",
		"'release'",
		"'project_item'",
		"'support_ticket'",
		"'implements'",
		"'ships_in'",
		"'tracked_by'",
		"idx_customer_request_delivery_artifacts_unique",
		"jsonb_typeof(payload) = 'object'",
	} {
		require.Contains(t, sql, value, "delivery artifact migration should include %s", value)
	}
}

func TestRecordMigrationSQL(t *testing.T) {
	t.Parallel()

	sql := recordMigrationSQL()
	require.Contains(t, sql, "INSERT INTO schema_migrations_feedback")
	require.Contains(t, sql, "version")
	require.Contains(t, sql, "filename")
	require.Contains(t, sql, "checksum")
	require.Contains(t, sql, "duration_ms")
	require.Contains(t, sql, "applied_by")
	require.Contains(t, sql, "success")
}

func TestMarkMigrationStartedSQL(t *testing.T) {
	t.Parallel()

	sql := markMigrationStartedSQL()
	require.Contains(t, sql, "INSERT INTO schema_migrations_feedback")
	require.Contains(t, sql, "success")
	require.Contains(t, sql, "FALSE") // dirty marker
}

func TestMarkMigrationCompleteSQL(t *testing.T) {
	t.Parallel()

	sql := markMigrationCompleteSQL()
	require.Contains(t, sql, "UPDATE schema_migrations_feedback")
	require.Contains(t, sql, "success = TRUE")
	require.Contains(t, sql, "duration_ms")
}

func TestRecordMigrationLegacySQL(t *testing.T) {
	t.Parallel()

	sql := recordMigrationLegacySQL()
	require.Contains(t, sql, "INSERT INTO schema_migrations_feedback")
	require.Contains(t, sql, "version")
	require.Contains(t, sql, "filename")
	// Legacy SQL should NOT have extended columns
	require.NotContains(t, sql, "checksum")
	require.NotContains(t, sql, "duration_ms")
}

func TestErrDirtyMigration_Fields(t *testing.T) {
	t.Parallel()

	err := ErrDirtyMigration{
		Version:  42,
		Filename: "042_test.sql",
	}

	require.Equal(t, 42, err.Version)
	require.Equal(t, "042_test.sql", err.Filename)
}

func TestIsNoTxMigration_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "directive with extra whitespace",
			body: "-- migrate:no-transaction  \nCREATE INDEX ...",
			want: true,
		},
		{
			name: "directive with tab before",
			body: "\t-- migrate:no-transaction\nCREATE INDEX ...",
			want: true, // Contains checks anywhere in first line
		},
		{
			name: "directive in middle of file (second line)",
			body: "-- Some comment\n-- migrate:no-transaction\nCREATE INDEX ...",
			want: false, // must be on first line
		},
		{
			name: "directive with different casing",
			body: "-- MIGRATE:NO-TRANSACTION\nCREATE INDEX ...",
			want: false, // case sensitive
		},
		{
			name: "directive with prefix text on same line",
			body: "xxx -- migrate:no-transaction\nCREATE INDEX ...",
			want: true, // Contains checks anywhere in first line
		},
		{
			name: "only whitespace before directive",
			body: "  -- migrate:no-transaction\nCREATE INDEX ...",
			want: true, // Contains checks anywhere in first line
		},
		{
			name: "no newline just directive",
			body: "-- migrate:no-transaction",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isNoTxMigration([]byte(tt.body))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMigrationLockKey(t *testing.T) {
	t.Parallel()

	// Verify the lock key is a stable constant
	require.Equal(t, int64(0x7AEC0ADBA51C042), migrationLockKey)
}
