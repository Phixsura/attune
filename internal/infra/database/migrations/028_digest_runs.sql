-- Daily digest run ledger (#27).
--
-- digest_runs is the exactly-once guard: one row per (tenant, local run_date).
-- The worker claims a day with INSERT ... ON CONFLICT (tenant_id, run_date) DO
-- NOTHING — the winning insert does the work; a duplicate tick or a second
-- replica no-ops. Shaped like the taskoutbox tables (status + attempts + retry
-- backoff) so a crashed run is recoverable on the next tick. run_date is the
-- date in the tenant's LOCAL timezone, so "at most one digest per tenant per
-- local day" holds across restarts and DST.
--
-- Idempotent: re-runs ignored via IF NOT EXISTS.

CREATE TABLE IF NOT EXISTS digest_runs (
    id              BIGSERIAL   PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subscription_id UUID        NOT NULL REFERENCES digest_subscriptions(id) ON DELETE CASCADE,
    run_date        DATE        NOT NULL,            -- date in the tenant's local tz
    status          TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'sent', 'skipped_empty', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    feedback_count  INT         NOT NULL DEFAULT 0,  -- total feedback in the window (clustered + not)
    theme_count     INT         NOT NULL DEFAULT 0,
    claimed_at      TIMESTAMPTZ,
    next_retry_at   TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_date)
);

-- Recover stale/failed runs (mirrors the taskoutbox claim index).
CREATE INDEX IF NOT EXISTS idx_digest_runs_claim
    ON digest_runs (status, next_retry_at, created_at)
    WHERE status IN ('pending', 'failed', 'processing');
