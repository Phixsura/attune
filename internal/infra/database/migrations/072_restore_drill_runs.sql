-- Migration 072: Restore-drill run history (#151).
--
-- Records each backup/restore drill — a verification of an already-restored,
-- throwaway database (schema/migration state, row counts, pgvector, and
-- decryptability of managed Tink secrets). The drill runs against the restored
-- target but records its summary HERE, in production, so the readiness surface
-- (preflight backup:restore_drill) and audit evidence can read the latest
-- result. Append-only history; never contains decrypted plaintext.

CREATE TABLE IF NOT EXISTS restore_drill_runs (
    id             BIGSERIAL PRIMARY KEY,
    ran_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    status         TEXT NOT NULL CHECK (status IN ('pass', 'warn', 'fail', 'skipped')),
    backup_ref     TEXT NOT NULL DEFAULT '',
    schema_version INTEGER NOT NULL DEFAULT 0,
    duration_ms    BIGINT NOT NULL DEFAULT 0,
    rpo_seconds    BIGINT,
    rto_seconds    BIGINT,
    report         JSONB NOT NULL,
    attune_version TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_restore_drill_runs_ran_at
    ON restore_drill_runs (ran_at DESC);

COMMENT ON TABLE restore_drill_runs IS
    'Append-only history of backup/restore drills (#151), written by attune restore-drill run --record.';
COMMENT ON COLUMN restore_drill_runs.status IS
    'Drill verdict: pass | warn | fail.';
COMMENT ON COLUMN restore_drill_runs.report IS
    'Full DrillReport JSON for audit evidence; never contains decrypted plaintext.';
