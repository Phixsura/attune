-- Migration 077: Add enrichment_claimed_by column to user_feedback
--
-- Part of #155 HA worker leases. The enricher uses per-row claims; adding
-- claimed_by enables fencing so MarkDone/MarkFailed only succeed if this
-- worker still holds the claim.

ALTER TABLE user_feedback ADD COLUMN IF NOT EXISTS enrichment_claimed_by TEXT;
