-- Migration 093: Add tenant_custom_fields table (#202).
--
-- Tenants can define custom metadata fields for feedback beyond the
-- built-in enrichment attributes. Each field has a key, display label,
-- type (text/number/boolean/enum), and optional enum values.
-- Custom field values are stored in user_feedback.source_meta JSONB.

CREATE TABLE IF NOT EXISTS tenant_custom_fields (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    field_key   TEXT         NOT NULL,
    display_name TEXT        NOT NULL,
    field_type  TEXT         NOT NULL
        CHECK (field_type IN ('text', 'number', 'boolean', 'enum')),
    enum_values TEXT[]       DEFAULT '{}',
    required    BOOLEAN      NOT NULL DEFAULT FALSE,
    sort_order  INT          NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, field_key)
);

CREATE INDEX IF NOT EXISTS idx_custom_fields_tenant
    ON tenant_custom_fields (tenant_id);
