-- Classification quality dashboard and drift detection (#161).
--
-- Semantic extraction runs are successful classification events. Failure
-- events record failed enrichment attempts so parse and terminal rates can be
-- computed without inferring event history from the current user_feedback row.

ALTER TABLE semantic_extraction_runs
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS logical_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS provider_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS channel_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS channel_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_semantic_runs_quality_rollup
    ON semantic_extraction_runs (tenant_id, id, created_at);

CREATE TABLE IF NOT EXISTS classification_quality_failure_events (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '',
    feedback_id     BIGINT REFERENCES user_feedback(id) ON DELETE SET NULL,
    event_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_kind      TEXT NOT NULL DEFAULT 'attempt_failed',
    reason_class    TEXT NOT NULL DEFAULT 'other_err',
    logical_model   TEXT NOT NULL DEFAULT '',
    provider_model  TEXT NOT NULL DEFAULT '',
    channel_id      TEXT NOT NULL DEFAULT '',
    channel_name    TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT '',
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    terminal        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (event_kind IN ('attempt_failed')),
    CHECK (reason_class IN ('llm_err', 'parse_err', 'other_err'))
);

CREATE INDEX IF NOT EXISTS idx_quality_failure_events_rollup
    ON classification_quality_failure_events (tenant_id, id, event_at);

CREATE INDEX IF NOT EXISTS idx_quality_failure_events_window
    ON classification_quality_failure_events (tenant_id, event_at DESC);

CREATE TABLE IF NOT EXISTS classification_quality_value_buckets (
    tenant_id               TEXT NOT NULL,
    bucket_start            TIMESTAMPTZ NOT NULL,
    bucket_width            TEXT NOT NULL,
    dimension_name          TEXT NOT NULL,
    dimension_value_hash    TEXT NOT NULL DEFAULT '',
    dimension_value_display TEXT NOT NULL DEFAULT '',
    value_status            TEXT NOT NULL,
    source                  TEXT NOT NULL DEFAULT '',
    logical_model           TEXT NOT NULL DEFAULT '',
    provider_model          TEXT NOT NULL DEFAULT '',
    channel_id              TEXT NOT NULL DEFAULT '',
    appearance_count        BIGINT NOT NULL DEFAULT 0 CHECK (appearance_count >= 0),
    event_count             BIGINT NOT NULL DEFAULT 0 CHECK (event_count >= 0),
    confidence_count        BIGINT NOT NULL DEFAULT 0 CHECK (confidence_count >= 0),
    confidence_sum          DOUBLE PRECISION NOT NULL DEFAULT 0,
    low_confidence_count    BIGINT NOT NULL DEFAULT 0 CHECK (low_confidence_count >= 0),
    sample_feedback_ids     BIGINT[] NOT NULL DEFAULT '{}',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        tenant_id, bucket_width, bucket_start, dimension_name,
        dimension_value_hash, value_status, source, logical_model,
        provider_model, channel_id
    ),
    CHECK (bucket_width IN ('hour', 'day')),
    CHECK (value_status IN ('configured', 'off_list', 'unknown_dimension', 'all'))
);

CREATE INDEX IF NOT EXISTS idx_quality_value_buckets_window
    ON classification_quality_value_buckets (tenant_id, bucket_width, bucket_start);

CREATE INDEX IF NOT EXISTS idx_quality_value_buckets_dimension
    ON classification_quality_value_buckets (tenant_id, dimension_name, bucket_start DESC);

CREATE TABLE IF NOT EXISTS classification_quality_signal_buckets (
    tenant_id                    TEXT NOT NULL,
    bucket_start                 TIMESTAMPTZ NOT NULL,
    bucket_width                 TEXT NOT NULL,
    source                       TEXT NOT NULL DEFAULT '',
    logical_model                TEXT NOT NULL DEFAULT '',
    provider_model               TEXT NOT NULL DEFAULT '',
    channel_id                   TEXT NOT NULL DEFAULT '',
    classification_event_count   BIGINT NOT NULL DEFAULT 0 CHECK (classification_event_count >= 0),
    failed_attempt_count         BIGINT NOT NULL DEFAULT 0 CHECK (failed_attempt_count >= 0),
    parse_failure_count          BIGINT NOT NULL DEFAULT 0 CHECK (parse_failure_count >= 0),
    terminal_failure_count       BIGINT NOT NULL DEFAULT 0 CHECK (terminal_failure_count >= 0),
    terminal_parse_failure_count BIGINT NOT NULL DEFAULT 0 CHECK (terminal_parse_failure_count >= 0),
    off_list_count               BIGINT NOT NULL DEFAULT 0 CHECK (off_list_count >= 0),
    unknown_dimension_count      BIGINT NOT NULL DEFAULT 0 CHECK (unknown_dimension_count >= 0),
    confidence_count             BIGINT NOT NULL DEFAULT 0 CHECK (confidence_count >= 0),
    confidence_sum               DOUBLE PRECISION NOT NULL DEFAULT 0,
    low_confidence_count         BIGINT NOT NULL DEFAULT 0 CHECK (low_confidence_count >= 0),
    sample_feedback_ids          BIGINT[] NOT NULL DEFAULT '{}',
    low_confidence_sample_feedback_ids BIGINT[] NOT NULL DEFAULT '{}',
    off_list_sample_feedback_ids       BIGINT[] NOT NULL DEFAULT '{}',
    parse_failure_sample_feedback_ids  BIGINT[] NOT NULL DEFAULT '{}',
    terminal_failure_sample_feedback_ids BIGINT[] NOT NULL DEFAULT '{}',
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        tenant_id, bucket_width, bucket_start, source, logical_model,
        provider_model, channel_id
    ),
    CHECK (bucket_width IN ('hour', 'day'))
);

CREATE INDEX IF NOT EXISTS idx_quality_signal_buckets_window
    ON classification_quality_signal_buckets (tenant_id, bucket_width, bucket_start);

CREATE TABLE IF NOT EXISTS classification_quality_rollup_state (
    tenant_id             TEXT NOT NULL,
    bucket_width          TEXT NOT NULL,
    last_semantic_run_id  BIGINT NOT NULL DEFAULT 0 CHECK (last_semantic_run_id >= 0),
    last_failure_event_id BIGINT NOT NULL DEFAULT 0 CHECK (last_failure_event_id >= 0),
    recompute_from        TIMESTAMPTZ,
    data_through          TIMESTAMPTZ,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, bucket_width),
    CHECK (bucket_width IN ('hour', 'day'))
);
