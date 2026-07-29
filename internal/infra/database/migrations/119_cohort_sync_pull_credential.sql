-- 119: Add pull credential columns for cohort sync (#233).
--
-- The push credential (credential_key_id + credential_ciphertext) is the API
-- key Attune generates for webhook authentication. The pull credential stores
-- the provider's own API key + secret needed for PullCohort / Check operations
-- (e.g. Amplitude API key + Secret Key, Mixpanel service account credentials).

ALTER TABLE cohort_sources
  ADD COLUMN IF NOT EXISTS pull_credential_key_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS pull_credential_ciphertext BYTEA NOT NULL DEFAULT '';
