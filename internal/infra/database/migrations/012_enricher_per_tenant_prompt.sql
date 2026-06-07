-- Per-tenant enricher overrides (#10).
--
-- enrich_prompt_template
--   NULL → use the built-in default prompt.
--   Non-NULL → custom prompt body using the same {{content}}/{{modules}}
--     token contract as the default. Rendered server-side via
--     strings.Replacer (single-pass, no expression evaluation, no
--     text/template SSTI surface).
--
-- enrich_modules
--   NULL or empty → free-form: the LLM may emit any module strings; the
--     enricher passes them through unchanged. Backwards-compatible with
--     pre-#10 behavior.
--   Non-empty → whitelist: after LLM response is parsed, modules are
--     filtered case-insensitively against this set; entries are dropped
--     if they don't match, and the configured spelling is the canonical
--     value stored on user_feedback.enriched_modules. Drops are surfaced
--     as a "suggested_module" signal (metric + log), never as labels.
--
-- See docs/proposals/2026-06-06-enricher-per-tenant-prompt.md.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS enrich_prompt_template TEXT,
    ADD COLUMN IF NOT EXISTS enrich_modules         TEXT[];
