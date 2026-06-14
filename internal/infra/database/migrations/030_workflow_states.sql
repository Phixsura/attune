-- Workflow state registry: per-tenant custom states in 3 fixed categories.
CREATE TABLE IF NOT EXISTS tenant_workflow_states (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name        TEXT NOT NULL,
    color       VARCHAR(7) NOT NULL DEFAULT '#6b7280',
    category    TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    is_default  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ws_tenant_name
    ON tenant_workflow_states (tenant_id, name)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_ws_tenant_default
    ON tenant_workflow_states (tenant_id)
    WHERE is_default AND archived_at IS NULL;

ALTER TABLE tenant_workflow_states ADD CONSTRAINT chk_ws_name_length
    CHECK (length(name) BETWEEN 1 AND 48);
ALTER TABLE tenant_workflow_states ADD CONSTRAINT chk_ws_name_no_ctrl
    CHECK (name !~ '[\x00-\x1f\x7f]');
ALTER TABLE tenant_workflow_states ADD CONSTRAINT chk_ws_color_hex
    CHECK (color ~ '^#[0-9a-f]{6}$');
ALTER TABLE tenant_workflow_states ADD CONSTRAINT chk_ws_category
    CHECK (category IN ('open', 'active', 'closed'));

-- Allowed transition edges: directed graph per tenant.
CREATE TABLE IF NOT EXISTS tenant_workflow_transitions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    from_state_id UUID NOT NULL REFERENCES tenant_workflow_states(id) ON DELETE RESTRICT,
    to_state_id   UUID NOT NULL REFERENCES tenant_workflow_states(id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tenant_workflow_transitions ADD CONSTRAINT chk_wt_no_self_loop
    CHECK (from_state_id != to_state_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_wt_tenant_edge
    ON tenant_workflow_transitions (tenant_id, from_state_id, to_state_id);

CREATE INDEX IF NOT EXISTS idx_wt_from
    ON tenant_workflow_transitions (from_state_id);

CREATE INDEX IF NOT EXISTS idx_wt_to
    ON tenant_workflow_transitions (to_state_id);

-- Field-level generic audit log (serves #29 workflow + future #39).
CREATE TABLE IF NOT EXISTS feedback_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    feedback_id BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    field_name  TEXT NOT NULL DEFAULT '',
    old_value   TEXT,
    new_value   TEXT,
    comment     TEXT NOT NULL DEFAULT '',
    changed_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fal_tenant_created
    ON feedback_audit_log (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_fal_feedback_created
    ON feedback_audit_log (feedback_id, created_at DESC);

-- Add workflow columns to feedback.
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS workflow_state_id UUID
        REFERENCES tenant_workflow_states(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS workflow_updated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_uf_workflow_state
    ON user_feedback (tenant_id, workflow_state_id, created_at DESC)
    WHERE workflow_state_id IS NOT NULL;
