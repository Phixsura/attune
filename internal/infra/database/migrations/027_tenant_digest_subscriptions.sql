-- Daily digest subscriptions (#27).
--
-- digest_subscriptions is the per-tenant scheduled roll-up config: a first-class
-- entity (NOT an `audience` value) that owns the schedule (frequency / local
-- send hour / weekday / timezone), the LLM theme threshold, and an optional
-- naming-prompt override. v1 allows one row per tenant (UNIQUE tenant_id); the
-- entity generalizes to many later.
--
-- Delivery reuses the existing raw-webhook outbox: the digest is POSTed to the
-- tenant's notify target whose audience='digest', resolved exactly like every
-- outbox row — by (tenant_id, destination_type, audience). So this migration
-- also extends the tenant_notify_targets.audience CHECK with 'digest', a routing
-- filter that keeps digest targets out of per-event dispatch.
--
-- Idempotent: re-runs ignored via IF NOT EXISTS / DROP-then-ADD.

CREATE TABLE IF NOT EXISTS digest_subscriptions (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled          BOOLEAN     NOT NULL DEFAULT TRUE,
    frequency        TEXT        NOT NULL DEFAULT 'daily'
        CHECK (frequency IN ('daily', 'weekly')),
    send_hour        SMALLINT    NOT NULL DEFAULT 9
        CHECK (send_hour BETWEEN 0 AND 23),
    byweekday        SMALLINT
        CHECK (byweekday BETWEEN 0 AND 6),   -- Go time.Weekday (Sun=0); used when frequency='weekly'
    timezone         TEXT,                    -- IANA override (validated on write); NULL => tenants.timezone
    llm_min_feedback INT         NOT NULL DEFAULT 6
        CHECK (llm_min_feedback >= 1),        -- >= this many enriched rows => LLM themes; fewer => themeless list
    send_on_empty    BOOLEAN     NOT NULL DEFAULT FALSE,
    theme_prompt     TEXT,
    next_run_at      TIMESTAMPTZ,             -- materialized UTC instant of the next fire (NULL => due now)
    last_run_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id)
);

-- Scheduler hot path: pick due, enabled subscriptions.
CREATE INDEX IF NOT EXISTS idx_digest_subscriptions_due
    ON digest_subscriptions (next_run_at)
    WHERE enabled;

-- Extend the audience routing filter with 'digest'. Digest targets are excluded
-- from per-event dispatch (selectOutboxTargets) and addressed only by the digest
-- worker. The original CHECK is the unnamed inline one from migration 004, which
-- Postgres named tenant_notify_targets_audience_check.
ALTER TABLE tenant_notify_targets
    DROP CONSTRAINT IF EXISTS tenant_notify_targets_audience_check;
ALTER TABLE tenant_notify_targets
    ADD CONSTRAINT tenant_notify_targets_audience_check
    CHECK (audience IN ('pool', 'radar', 'all', 'digest'));
