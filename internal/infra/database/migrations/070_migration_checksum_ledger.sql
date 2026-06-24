-- Migration 070: Extend tracker with checksum, duration, binary version, success flag.
--
-- #150: Add migration integrity tracking for enterprise deployments.
-- Existing rows get empty checksum (legacy marker) and success=true.
--
-- New columns:
--   checksum    - SHA-256 hex of migration file content
--   duration_ms - Execution time in milliseconds
--   applied_by  - Binary version that applied this migration
--   success     - FALSE if migration started but crashed before completion

ALTER TABLE schema_migrations_feedback
    ADD COLUMN IF NOT EXISTS checksum    VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS duration_ms INT,
    ADD COLUMN IF NOT EXISTS applied_by  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS success     BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN schema_migrations_feedback.checksum IS
    'SHA-256 hex of migration file content; empty string for legacy rows (pre-070)';
COMMENT ON COLUMN schema_migrations_feedback.duration_ms IS
    'Execution time in milliseconds; NULL for legacy rows';
COMMENT ON COLUMN schema_migrations_feedback.applied_by IS
    'Binary version that applied this migration (e.g. "attune v0.9.0"); empty for legacy rows';
COMMENT ON COLUMN schema_migrations_feedback.success IS
    'FALSE if migration started but crashed before recording completion (dirty state)';
