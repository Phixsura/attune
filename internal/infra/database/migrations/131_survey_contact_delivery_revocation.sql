-- SPDX-License-Identifier: Apache-2.0
--
-- Support synchronous revocation of queued survey emails after a tenant-wide
-- customer unsubscribe.

CREATE INDEX IF NOT EXISTS idx_survey_invitations_contact_delivery_queue
    ON survey_invitations (tenant_id, contact_id)
    WHERE distribution_mode = 'contact_email'
      AND delivery_status IN ('pending', 'delayed')
      AND response_status <> 'completed'
      AND suppression_status = 'not_suppressed';
