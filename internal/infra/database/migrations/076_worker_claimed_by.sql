-- Migration 076: Add claimed_by column to worker task tables
--
-- Part of #155 HA worker leases. Each queue table needs claimed_by so
-- heartbeat refresh only touches rows this worker instance still holds.
-- Without this, replica A's RefreshClaims could accidentally extend a row
-- that replica B legitimately re-claimed after A's lease expired.

-- reply_draft_task
ALTER TABLE reply_draft_task ADD COLUMN IF NOT EXISTS claimed_by TEXT;

-- digest_runs
ALTER TABLE digest_runs ADD COLUMN IF NOT EXISTS claimed_by TEXT;

-- embedding_task
ALTER TABLE embedding_task ADD COLUMN IF NOT EXISTS claimed_by TEXT;

-- batch_jobs (batch job worker)
ALTER TABLE batch_jobs ADD COLUMN IF NOT EXISTS claimed_by TEXT;
