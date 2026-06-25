-- Migration: fix_foreign_key_cascades
-- Add ON DELETE CASCADE/SET NULL to foreign keys that were missing it.
-- This prevents orphan rows when parent records are deleted.
-- Note: Uses IF EXISTS checks for tables that may not exist in all environments.

-- notify_outbox.feedback_id: SET NULL so delivery history is preserved
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'notify_outbox') THEN
        ALTER TABLE notify_outbox DROP CONSTRAINT IF EXISTS notify_outbox_feedback_id_fkey;
        IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'notify_outbox' AND column_name = 'feedback_id') THEN
            ALTER TABLE notify_outbox
                ADD CONSTRAINT notify_outbox_feedback_id_fkey
                FOREIGN KEY (feedback_id) REFERENCES user_feedback(id) ON DELETE SET NULL;
        END IF;
    END IF;
END $$;

-- llm_usage_audit.feedback_id: SET NULL to preserve audit trail (table may not exist)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'llm_usage_audit') THEN
        ALTER TABLE llm_usage_audit DROP CONSTRAINT IF EXISTS llm_usage_audit_feedback_id_fkey;
        IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'llm_usage_audit' AND column_name = 'feedback_id') THEN
            ALTER TABLE llm_usage_audit
                ADD CONSTRAINT llm_usage_audit_feedback_id_fkey
                FOREIGN KEY (feedback_id) REFERENCES user_feedback(id) ON DELETE SET NULL;
        END IF;
    END IF;
END $$;

-- tenant_enrich_prompt_active_version: CASCADE since version deletion should clear active (table may not exist)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'tenant_enrich_prompt_active_version') THEN
        ALTER TABLE tenant_enrich_prompt_active_version
            DROP CONSTRAINT IF EXISTS tenant_enrich_prompt_active_version_tenant_id_version_id_fkey;
        ALTER TABLE tenant_enrich_prompt_active_version
            DROP CONSTRAINT IF EXISTS fk_version;
        IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'tenant_enrich_prompt_versions') THEN
            ALTER TABLE tenant_enrich_prompt_active_version
                ADD CONSTRAINT tenant_enrich_prompt_active_version_version_fkey
                FOREIGN KEY (tenant_id, version_id) REFERENCES tenant_enrich_prompt_versions(tenant_id, id) ON DELETE CASCADE;
        END IF;
    END IF;
END $$;
