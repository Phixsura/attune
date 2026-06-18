-- Migration 051: notify_outbox dead-queue operator surface (#33).
--
-- The retry/backoff/dead engine already exists (005_notify_outbox.sql). This
-- migration adds the columns the Console/API dead-queue + manual-retry surface
-- needs:
--
--   * failure_kind / http_status — structured failure reason. The worker today
--     only knows "terminal vs retryable"; the upstream HTTP status was thrown
--     away into a flat error string. Splitting transport-level errors
--     (dns/timeout/tls/connection) from the HTTP status lets operators triage
--     ("show me all the 5xx") without parsing last_error.
--
--   * last_manual_retry_at / retried_by / manual_retry_count — bookkeeping for
--     operator-triggered retries. The full pre-retry death snapshot is recorded
--     in the audit log (action outbox.retry); these columns give an at-a-glance
--     view in the dead-queue list without a join.
--
-- Additive + idempotent: IF NOT EXISTS on every column; the CHECK is guarded by
-- a pg_constraint lookup so re-running is a no-op. The existing
-- idx_notify_outbox_tenant_status index already serves the tenant-scoped,
-- status-filtered list query — no new index here.

ALTER TABLE notify_outbox
    ADD COLUMN IF NOT EXISTS failure_kind         TEXT,
    ADD COLUMN IF NOT EXISTS http_status          SMALLINT,
    ADD COLUMN IF NOT EXISTS last_manual_retry_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS retried_by           TEXT,
    ADD COLUMN IF NOT EXISTS manual_retry_count   SMALLINT NOT NULL DEFAULT 0;

-- Keep failure_kind in lockstep with notify.FailureKind (internal/notify).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'notify_outbox_failure_kind_chk'
    ) THEN
        ALTER TABLE notify_outbox
            ADD CONSTRAINT notify_outbox_failure_kind_chk
            CHECK (failure_kind IS NULL OR failure_kind IN
                ('http_4xx', 'http_5xx', 'timeout', 'dns', 'connection', 'tls', 'terminal', 'other'));
    END IF;
END $$;
