-- Tag registry: one row per named tag per tenant.
CREATE TABLE IF NOT EXISTS tenant_feedback_tags (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        TEXT         NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name             TEXT         NOT NULL,
    color            VARCHAR(7)   NOT NULL DEFAULT '#6b7280',
    description      TEXT         NOT NULL DEFAULT '',
    exclusive_scope  TEXT,
    archived_at      TIMESTAMPTZ,
    usage_count      INT          NOT NULL DEFAULT 0 CHECK (usage_count >= 0),
    created_by       TEXT         NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_tenant_feedback_tags_tenant
    ON tenant_feedback_tags (tenant_id) WHERE archived_at IS NULL;

ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_name_length
    CHECK (length(name) BETWEEN 1 AND 48);
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_name_no_ctrl
    CHECK (name !~ '[\x00-\x1f\x7f]');
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_color_hex
    CHECK (color ~ '^#[0-9a-f]{6}$');
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_description_length
    CHECK (length(description) <= 200);
ALTER TABLE tenant_feedback_tags ADD CONSTRAINT chk_tag_scope_length
    CHECK (exclusive_scope IS NULL OR length(exclusive_scope) BETWEEN 1 AND 32);

-- Junction table: per-assignment audit trail.
CREATE TABLE IF NOT EXISTS feedback_tag_assignments (
    feedback_id      BIGINT       NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    tag_id           UUID         NOT NULL REFERENCES tenant_feedback_tags(id) ON DELETE CASCADE,
    created_by       TEXT         NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (feedback_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_feedback_tag_assignments_tag
    ON feedback_tag_assignments (tag_id, feedback_id);
