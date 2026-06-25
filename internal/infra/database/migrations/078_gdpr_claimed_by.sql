-- Migration 078: Add claimed_by column to GDPR tables
--
-- Part of #155 HA worker leases. GDPR export/delete jobs need claimed_by
-- for fencing so HeartbeatExportJob and Complete/Fail only succeed if
-- this worker still holds the claim.

-- gdpr_export_jobs
ALTER TABLE gdpr_export_jobs ADD COLUMN IF NOT EXISTS claimed_by TEXT;

-- gdpr_requests (unified export/delete requests table)
ALTER TABLE gdpr_requests ADD COLUMN IF NOT EXISTS claimed_by TEXT;
ALTER TABLE gdpr_requests ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE gdpr_requests ADD COLUMN IF NOT EXISTS last_heartbeat TIMESTAMPTZ;
