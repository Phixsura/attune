-- Migration 054: owner-scope outbox claim renewal (#64).
--
-- ClaimBatch stamps claimed_by with the worker instance's id; the lease
-- heartbeat (RefreshClaims) only renews rows it still owns. Without this, under
-- multiple outbox worker replicas, replica A's heartbeat could keep re-stamping
-- claimed_at on a row replica B had legitimately re-claimed after A's claim aged
-- out — stranding the row until A's batch ended. NULL = unclaimed / legacy.

ALTER TABLE notify_outbox ADD COLUMN IF NOT EXISTS claimed_by TEXT;
