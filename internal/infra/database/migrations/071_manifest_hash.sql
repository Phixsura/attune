-- Migration 071: Add manifest hash for reordering detection.
--
-- The manifest_hash column stores a SHA-256 hash of the entire migration set
-- (filenames + checksums in order). This detects reordering that individual
-- checksum verification would miss.
--
-- Schema: single-row table to store the latest manifest hash.

CREATE TABLE IF NOT EXISTS schema_migrations_manifest (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    hash       VARCHAR(64) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE schema_migrations_manifest IS
    'Single-row table storing the manifest hash of the migration set';
COMMENT ON COLUMN schema_migrations_manifest.hash IS
    'SHA-256 hex of concatenated (filename:checksum) for all migrations in order';
