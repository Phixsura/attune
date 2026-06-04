-- Phase 5 M6 weekly digest — bookkeeping column on tenants. NULL means
-- "never sent yet"; scheduler treats NULL as eligible immediately.
--
-- Lives on tenants (not a separate digest_state table) because exactly
-- one timestamp per tenant; no need for history (delivered/dead state
-- already lives in notify_outbox + lark-bot is delivered inline anyway).

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS last_digest_sent_at TIMESTAMPTZ;

-- Scheduler filter: WHERE last_digest_sent_at IS NULL OR < $cutoff.
-- Cheap because tenants table is small (Y1 < 400).
