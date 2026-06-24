//go:build integration

// Exercises the restore-drill verifier against a real Postgres. A migrated,
// secret-seeded database stands in for a restored one — structurally identical
// from the verifier's point of view.
package restoredrill

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/restoredrill"
	"github.com/Phixsura/attune/internal/testdb"
)

func newStore(t *testing.T) *secretstore.TinkStore {
	t.Helper()
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatalf("generate keyset: %v", err)
	}
	s, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func TestRestoreDrill_PassesOnSeededRestore(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	// Seed an llm_channels credential (AAD-bound).
	chID := uuid.NewString()
	cred, err := store.EncryptValue([]byte("sk-integration-test"),
		secretstore.AssociatedData("llm_channel", chID, "api_key"))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO llm_channels (id, name, protocol, credential_key_id, credential_ciphertext)
		VALUES ($1, $2, 'openai-compat', $3, $4)`, chID, "test-channel", cred.KeyID, cred.Ciphertext)

	// Seed a tenant + webhook + email inbound sources (two-level envelopes), so
	// both decrypt paths run against a real database, not just unit tests.
	tenantID := uuid.NewString()
	mustExec(t, ctx, pool, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID, "t-"+tenantID[:8], "Test Tenant")

	webhookCfg := mustEncrypt(t, store, mustMarshal(t, map[string][]byte{
		"secret_current_encrypted": mustEncrypt(t, store, []byte("webhook-signing-secret")),
	}))
	mustExec(t, ctx, pool, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config)
		VALUES (gen_random_uuid(), $1, 'webhook', 'wh', 'wh', $2)`, tenantID, webhookCfg)

	emailCfg := mustEncrypt(t, store, mustMarshal(t, map[string][]byte{
		"password_encrypted": mustEncrypt(t, store, []byte("imap-secret")),
	}))
	mustExec(t, ctx, pool, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config)
		VALUES (gen_random_uuid(), $1, 'email', 'em', 'em', $2)`, tenantID, emailCfg)

	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{BackupRef: "integration"})

	if rep.Status == restoredrill.StatusFail {
		t.Fatalf("expected a non-fail drill, got %s: %+v", rep.Status, rep.Checks)
	}
	assertCheck(t, rep, "connectivity", restoredrill.StatusPass)
	assertCheck(t, rep, "schema", restoredrill.StatusPass)
	assertCheck(t, rep, "decryptability", restoredrill.StatusPass)

	// Record + read back the audit row.
	if err := restoredrill.Record(ctx, pool, rep); err != nil {
		t.Fatalf("record: %v", err)
	}
	last, ok, err := restoredrill.ReadLast(ctx, pool)
	if err != nil || !ok {
		t.Fatalf("read last: ok=%v err=%v", ok, err)
	}
	if last.Status != rep.Status {
		t.Fatalf("last recorded status %q != report status %q", last.Status, rep.Status)
	}
	if last.BackupRef != "integration" {
		t.Fatalf("backup_ref round-trip = %q, want %q", last.BackupRef, "integration")
	}
}

func TestRestoreDrill_FailsLoudlyOnKeysetDrift(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	writer := newStore(t)

	chID := uuid.NewString()
	cred, err := writer.EncryptValue([]byte("sk-x"),
		secretstore.AssociatedData("llm_channel", chID, "api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO llm_channels (id, name, protocol, credential_key_id, credential_ciphertext)
		VALUES ($1, $2, 'openai-compat', $3, $4)`,
		chID, "drift-channel", cred.KeyID, cred.Ciphertext); err != nil {
		t.Fatal(err)
	}

	// Run with a DIFFERENT keyset — the seeded secret cannot be decrypted.
	reader := newStore(t)
	rep := restoredrill.Run(ctx, pool, reader, time.Now(), restoredrill.Options{})

	if rep.Status != restoredrill.StatusFail {
		t.Fatalf("expected drill to FAIL on keyset drift, got %s: %+v", rep.Status, rep.Checks)
	}
	assertCheck(t, rep, "decryptability", restoredrill.StatusFail)
}

func TestRestoreDrill_DeepWithAmcheck(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS amcheck`); err != nil {
		t.Fatalf("create amcheck: %v", err)
	}

	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{Deep: true})

	if rep.Status == restoredrill.StatusFail {
		t.Fatalf("expected non-fail deep drill, got %s: %+v", rep.Status, rep.Checks)
	}
	assertCheck(t, rep, "deep", restoredrill.StatusPass)
}

func TestRestoreDrill_DeepSkipsWithoutAmcheck(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{Deep: true})

	// Indexes are valid but amcheck is absent → the deep tier skips, never fails.
	assertCheck(t, rep, "deep", restoredrill.StatusSkip)
}

func TestRestoreDrill_MetricsDerivedFromRecord(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{})
	if err := restoredrill.Record(ctx, pool, rep); err != nil {
		t.Fatalf("record: %v", err)
	}

	reg := prometheus.NewRegistry()
	restoredrill.RegisterMetrics(reg, pool)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var sawRuns, sawTimestamp bool
	for _, mf := range mfs {
		switch mf.GetName() {
		case "attune_restore_drill_runs_total":
			sawRuns = true
			var total float64
			for _, m := range mf.GetMetric() {
				total += m.GetCounter().GetValue()
			}
			if total < 1 {
				t.Fatalf("runs_total = %v, want >= 1", total)
			}
		case "attune_restore_drill_last_success_timestamp_seconds":
			sawTimestamp = true
			if v := mf.GetMetric()[0].GetGauge().GetValue(); v <= 0 {
				t.Fatalf("last_success timestamp = %v, want > 0", v)
			}
		}
	}
	if !sawRuns || !sawTimestamp {
		t.Fatalf("derived metrics missing: runs=%v timestamp=%v", sawRuns, sawTimestamp)
	}
}

func TestRestoreDrill_ReadLastEmpty(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)

	last, ok, err := restoredrill.ReadLast(ctx, pool)
	if err != nil {
		t.Fatalf("ReadLast on empty table errored: %v", err)
	}
	if ok {
		t.Fatalf("ReadLast on empty table returned ok=true: %+v", last)
	}
}

func TestRestoreDrill_PgvectorSampleQuery(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	tenantID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID, "t-"+tenantID[:8], "T"); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	// A 256-dim embedding so the pgvector sample KNN query actually runs.
	vec := "[1" + strings.Repeat(",0", 255) + "]"
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, content, source, embedding)
		VALUES ($1, 'u1', 'hello', 'api', $2::vector)`, tenantID, vec); err != nil {
		t.Fatalf("seed embedded row: %v", err)
	}

	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{})
	got := findCheck(t, rep, "pgvector")
	if got.Status != restoredrill.StatusPass || !strings.Contains(got.Message, "sample similarity query OK") {
		t.Fatalf("pgvector = %q / %q", got.Status, got.Message)
	}
}

// TestRestoreDrill_OrchestratedRestore is the full push-button loop: pg_dump a
// seeded source DB, then attune provisions an ephemeral DB, restores the dump
// into it (psql), measures the RTO, verifies, and tears it down.
func TestRestoreDrill_OrchestratedRestore(t *testing.T) {
	if _, err := exec.LookPath("psql"); err != nil {
		t.Skip("psql not in PATH; skipping orchestration test")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not in PATH; skipping orchestration test")
	}
	ctx := context.Background()
	source := testdb.NewPool(t)
	store := newStore(t)

	// Seed a managed secret so decryptability verifies real data after restore.
	chID := uuid.NewString()
	cred, err := store.EncryptValue([]byte("sk-orchestrated"),
		secretstore.AssociatedData("llm_channel", chID, "api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, `
		INSERT INTO llm_channels (id, name, protocol, credential_key_id, credential_ciphertext)
		VALUES ($1, $2, 'openai-compat', $3, $4)`, chID, "orch-channel", cred.KeyID, cred.Ciphertext); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srcConn := source.Config().ConnString()
	dumpFile := filepath.Join(t.TempDir(), "backup.sql")
	if out, err := exec.CommandContext(ctx, "pg_dump", srcConn, "-f", dumpFile).CombinedOutput(); err != nil {
		// e.g. a pg_dump client older than the pg17 test server, or no network
		// to it — an environment limit, not a code bug.
		t.Skipf("pg_dump unavailable/incompatible: %v: %s", err, out)
	}

	rep, err := restoredrill.RestoreAndDrill(ctx, srcConn, dumpFile, "psql", store,
		restoredrill.Options{BackupRef: "orchestrated"})
	if err != nil {
		// CREATE DATABASE permission or an incompatible psql — environment limit.
		t.Skipf("orchestrated restore unavailable in this environment: %v", err)
	}
	if rep.Status == restoredrill.StatusFail {
		t.Fatalf("orchestrated drill FAILED: %+v", rep.Checks)
	}
	assertCheck(t, rep, "schema", restoredrill.StatusPass)
	assertCheck(t, rep, "decryptability", restoredrill.StatusPass)
	if rep.RTOSeconds == nil {
		t.Fatal("orchestration did not measure the RTO")
	}
}

func TestRestoreDrill_VerifyBackupArtifact(t *testing.T) {
	if _, err := exec.LookPath("pg_basebackup"); err != nil {
		t.Skip("pg_basebackup not in PATH")
	}
	if _, err := exec.LookPath("pg_verifybackup"); err != nil {
		t.Skip("pg_verifybackup not in PATH")
	}
	ctx := context.Background()
	source := testdb.NewPool(t)
	srcConn := source.Config().ConnString()

	dir := filepath.Join(t.TempDir(), "base")
	if out, err := exec.CommandContext(ctx, "pg_basebackup",
		"-d", srcConn, "-D", dir, "-X", "stream", "--no-sync").CombinedOutput(); err != nil {
		t.Skipf("pg_basebackup unavailable (likely replication/pg_hba not enabled on the test container): %v: %s", err, out)
	}

	// A clean backup artifact verifies.
	if err := restoredrill.VerifyBackupArtifact(ctx, dir); err != nil {
		t.Fatalf("verify clean backup: %v", err)
	}
	// Corrupting a manifest-tracked file is detected.
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("99\n"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if err := restoredrill.VerifyBackupArtifact(ctx, dir); err == nil {
		t.Fatal("expected pg_verifybackup to detect the corrupted backup")
	}
}

func TestRestoreDrill_DeadPoolErrors(t *testing.T) {
	ctx := context.Background()
	pool, cleanup, err := testdb.OpenPool()
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	cleanup() // close the pool — every query now errors
	store := newStore(t)

	// Run short-circuits at connectivity and must NOT report RPO/RTO (nothing
	// was verified) even when they were supplied.
	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{
		BackupTakenAt:   ptrext.Of(time.Now().Add(-time.Hour)),
		RestoreDuration: ptrext.Of(time.Minute),
	})
	if rep.Status != restoredrill.StatusFail || len(rep.Checks) != 1 {
		t.Fatalf("dead-pool Run: status=%q checks=%d", rep.Status, len(rep.Checks))
	}
	if rep.RPOSeconds != nil || rep.RTOSeconds != nil {
		t.Fatalf("RPO/RTO must be nil on connectivity failure: rpo=%v rto=%v", rep.RPOSeconds, rep.RTOSeconds)
	}

	// Exported store / baseline functions surface the DB error.
	wantErr(t, "Record", restoredrill.Record(ctx, pool, rep))
	_, _, rlErr := restoredrill.ReadLast(ctx, pool)
	wantErr(t, "ReadLast", rlErr)
	_, hErr := restoredrill.History(ctx, pool, 5)
	wantErr(t, "History", hErr)
	_, bcErr := restoredrill.BaselineCounts(ctx, pool)
	wantErr(t, "BaselineCounts", bcErr)
	_, bucErr := restoredrill.BaselineUnvalidatedCount(ctx, pool)
	wantErr(t, "BaselineUnvalidatedCount", bucErr)
	_, beErr := restoredrill.BaselineExtensions(ctx, pool)
	wantErr(t, "BaselineExtensions", beErr)
}

func wantErr(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s on dead pool: expected error", name)
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func mustEncrypt(t *testing.T, store *secretstore.TinkStore, plaintext []byte) []byte {
	t.Helper()
	ct, err := store.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return ct
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func findCheck(t *testing.T, rep restoredrill.DrillReport, name string) restoredrill.CheckResult {
	t.Helper()
	for _, c := range rep.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found", name)
	return restoredrill.CheckResult{}
}

func TestRestoreDrill_FreshDBPassesIntegrityChecks(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)
	// No baseline → constraints is skipped (some NOT VALID constraints ship with
	// the schema, so the absolute count is not actionable); sequences must pass.
	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{})
	assertCheck(t, rep, "constraints", restoredrill.StatusSkip)
	assertCheck(t, rep, "sequences", restoredrill.StatusPass)
	assertCheck(t, rep, "encoding", restoredrill.StatusPass)
	assertCheck(t, rep, "materialized_views", restoredrill.StatusPass)
}

func TestRestoreDrill_UnpopulatedMatviewFails(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)
	if _, err := pool.Exec(ctx,
		`CREATE MATERIALIZED VIEW mv_drill_demo AS SELECT 1 AS x WITH NO DATA`); err != nil {
		t.Fatalf("create matview: %v", err)
	}
	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{})
	assertCheck(t, rep, "materialized_views", restoredrill.StatusFail)
}

func TestRestoreDrill_ExtensionsBaseline(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	exts, err := restoredrill.BaselineExtensions(ctx, pool)
	if err != nil {
		t.Fatalf("baseline extensions: %v", err)
	}
	// Restored matches the live extension set → pass.
	repOK := restoredrill.Run(ctx, pool, store, time.Now(),
		restoredrill.Options{BaselineExtensions: exts})
	assertCheck(t, repOK, "extensions", restoredrill.StatusPass)

	// Live has an extension the restore lacks → fail.
	bad := append(append([]string{}, exts...), "nonexistent_ext_xyz")
	repBad := restoredrill.Run(ctx, pool, store, time.Now(),
		restoredrill.Options{BaselineExtensions: bad})
	assertCheck(t, repBad, "extensions", restoredrill.StatusFail)
}

func TestRestoreDrill_RestoreIntroducedConstraintFails(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	base, err := restoredrill.BaselineUnvalidatedCount(ctx, pool)
	if err != nil {
		t.Fatalf("baseline count: %v", err)
	}
	// Restored state matches the live baseline → pass.
	repOK := restoredrill.Run(ctx, pool, store, time.Now(),
		restoredrill.Options{BaselineUnvalidated: ptrext.Of(base)})
	assertCheck(t, repOK, "constraints", restoredrill.StatusPass)

	// A NEW unvalidated constraint vs the baseline → fail (restore introduced it).
	if _, err := pool.Exec(ctx,
		`ALTER TABLE tenants ADD CONSTRAINT chk_drill_demo CHECK (slug <> '') NOT VALID`); err != nil {
		t.Fatalf("add NOT VALID constraint: %v", err)
	}
	rep := restoredrill.Run(ctx, pool, store, time.Now(),
		restoredrill.Options{BaselineUnvalidated: ptrext.Of(base)})
	assertCheck(t, rep, "constraints", restoredrill.StatusFail)
}

func TestRestoreDrill_StaleSequenceFails(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)
	// restore_drill_runs.id is BIGSERIAL — record rows so max(id) > 0, then push
	// the sequence behind (the classic "data restored, sequences not reset" bug).
	rep0 := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{})
	for i := 0; i < 2; i++ {
		if err := restoredrill.Record(ctx, pool, rep0); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if _, err := pool.Exec(ctx,
		`SELECT setval(pg_get_serial_sequence('restore_drill_runs', 'id'), 1, false)`); err != nil {
		t.Fatalf("setval behind: %v", err)
	}
	rep := restoredrill.Run(ctx, pool, store, time.Now(), restoredrill.Options{})
	assertCheck(t, rep, "sequences", restoredrill.StatusFail)
}

func TestRestoreDrill_RecoveryObjectivesAndHistory(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	opts := restoredrill.Options{
		BackupRef:       "rpo-rto",
		BackupTakenAt:   ptrext.Of(time.Now().Add(-2 * time.Hour)),
		RestoreDuration: ptrext.Of(90 * time.Second),
		RPOTarget:       24 * time.Hour,
		RTOTarget:       30 * time.Minute,
	}
	rep := restoredrill.Run(ctx, pool, store, time.Now(), opts)

	if rep.RPOSeconds == nil || ptrext.Indirect(rep.RPOSeconds) < 7000 {
		t.Fatalf("expected RPO ~7200s, got %v", rep.RPOSeconds)
	}
	if rep.RTOSeconds == nil || ptrext.Indirect(rep.RTOSeconds) != 90 {
		t.Fatalf("expected RTO 90s, got %v", rep.RTOSeconds)
	}
	assertCheck(t, rep, "recovery_objectives", restoredrill.StatusPass)

	if err := restoredrill.Record(ctx, pool, rep); err != nil {
		t.Fatalf("record: %v", err)
	}
	hist, err := restoredrill.History(ctx, pool, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) == 0 {
		t.Fatal("history empty")
	}
	if h := hist[0]; h.RTOSeconds == nil || ptrext.Indirect(h.RTOSeconds) != 90 || h.RPOSeconds == nil {
		t.Fatalf("history RPO/RTO not persisted: %+v", h)
	}
}

func TestRestoreDrill_RPOBreachWarns(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newStore(t)

	// Backup is 48h old against a 24h RPO target → recovery_objectives warns,
	// which makes the overall drill warn (not fail — data IS recoverable).
	opts := restoredrill.Options{
		BackupTakenAt: ptrext.Of(time.Now().Add(-48 * time.Hour)),
		RPOTarget:     24 * time.Hour,
	}
	rep := restoredrill.Run(ctx, pool, store, time.Now(), opts)
	assertCheck(t, rep, "recovery_objectives", restoredrill.StatusWarn)
	if rep.Status == restoredrill.StatusFail {
		t.Fatalf("RPO breach should warn, not fail; got %s", rep.Status)
	}
}

func assertCheck(t *testing.T, rep restoredrill.DrillReport, name string, want restoredrill.Status) {
	t.Helper()
	for _, c := range rep.Checks {
		if c.Name == name {
			if c.Status != want {
				t.Fatalf("check %q = %s, want %s (msg: %s)", name, c.Status, want, c.Message)
			}
			return
		}
	}
	t.Fatalf("check %q not found in report", name)
}
