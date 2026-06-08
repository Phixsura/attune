-- 016_create_admins.sql — local console admin login table (#66 Plan T8).
--
-- Companion to console/auth: bcrypt-hashed passwords, 5-strike lockout.
-- Bootstrap is operator-supplied via ATTUNE_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}[_FILE]
-- on the first start when this table is empty (see internal/handlers/console/auth/bootstrap.go).

CREATE TABLE IF NOT EXISTS admins (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    role            TEXT NOT NULL DEFAULT 'admin',
    failed_attempts INT  NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS admins_email_lower ON admins (LOWER(email));
