-- migrate:no-transaction
--
-- Migration 136: NPS audience subject lookup index (#236).
--
-- Relationship-NPS resolves active cohort memberships to consented contacts by
-- (tenant_id, subject_key). The existing subject identity index places
-- subject_hash before subject_key and cannot seek this join directly. Build a
-- narrow, concurrent index so large cohorts do not scan every tenant contact.
--
-- This file must remain one idempotent statement: non-transactional migrations
-- may be retried after a process interruption.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_customer_notification_contacts_subject_key
    ON customer_notification_contacts (tenant_id, subject_key)
    WHERE subject_key <> '';
