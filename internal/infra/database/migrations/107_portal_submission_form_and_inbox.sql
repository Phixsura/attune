-- SPDX-License-Identifier: Apache-2.0
--
-- Portal submission form config and inbox indexing.

ALTER TABLE public_visibility_policies
    ADD COLUMN IF NOT EXISTS portal_submission_form JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE public_visibility_policies
SET portal_submission_form = '{}'::jsonb
WHERE portal_submission_form IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_feedback_tenant_portal_created
    ON user_feedback (tenant_id, created_at DESC, id DESC)
    WHERE source = 'portal';

