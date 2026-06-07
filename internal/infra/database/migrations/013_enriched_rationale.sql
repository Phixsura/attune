-- enriched_rationale (#10 follow-up): the LLM's short explanation for
-- the kind / severity it picked. Already on the wire to webhook
-- consumers via the outbox envelope (Snapshot.Rationale), but until
-- now there was no column for it on user_feedback — so MarkDone
-- silently dropped it on every classification, the GET feedback
-- detail endpoint never returned it, and the console SPA had no way
-- to surface the AI's reasoning to the human reviewing the row.
--
-- TEXT so we don't fight on prompts that nudge the model toward longer
-- justifications later; the default prompt asks for ≤30 chars.

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS enriched_rationale TEXT;
