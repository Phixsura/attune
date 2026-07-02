-- Migration 094: add a full-text index for feedback lexical search quality.

CREATE INDEX IF NOT EXISTS idx_user_feedback_search_document_simple
    ON user_feedback
    USING GIN (
        to_tsvector(
            'simple'::regconfig,
            COALESCE(content, '') || ' ' ||
            COALESCE(enriched_title, '') || ' ' ||
            COALESCE(enriched_display_title, '') || ' ' ||
            COALESCE(enriched_rationale, '') || ' ' ||
            COALESCE(source, '') || ' ' ||
            COALESCE(type, '') || ' ' ||
            COALESCE(user_id, '') || ' ' ||
            COALESCE(page_url, '')
        )
    )
    WHERE deleted_at IS NULL;
