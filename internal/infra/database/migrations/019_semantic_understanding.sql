-- Semantic understanding layer foundation (#21).
--
-- Stage 1: enrich Dimensions can carry model-facing definitions and renderer
-- metadata; seed the customer-feedback pack with a Customer tone dimension
-- under the stable machine key `sentiment`.
--
-- Stage 2: semantic_extraction_runs records the evidence/provenance for each
-- full LLM extraction while user_feedback.enriched_attrs remains the fast
-- current-state snapshot used by list/detail/stats queries.

ALTER TABLE tenants
    ALTER COLUMN enrich_dimensions SET DEFAULT '[
       {
         "name": "type",
         "display_name": {"default": "Type", "zh": "类型", "en": "Type"},
         "kind": "single",
         "taxonomy": [
           {"value": "bug",      "display_name": {"default": "Bug",      "zh": "缺陷", "en": "Bug"}},
           {"value": "feature",  "display_name": {"default": "Feature",  "zh": "特性", "en": "Feature"}},
           {"value": "question", "display_name": {"default": "Question", "zh": "咨询", "en": "Question"}},
           {"value": "other",    "display_name": {"default": "Other",    "zh": "其他", "en": "Other"}}
         ],
         "urgent_set": [],
         "required": false
       },
       {
         "name": "severity",
         "display_name": {"default": "Severity", "zh": "严重程度", "en": "Severity"},
         "kind": "single",
         "taxonomy": [
           {"value": "critical", "display_name": {"default": "Critical", "zh": "严重", "en": "Critical"}},
           {"value": "major",    "display_name": {"default": "Major",    "zh": "重要", "en": "Major"}},
           {"value": "minor",    "display_name": {"default": "Minor",    "zh": "次要", "en": "Minor"}}
         ],
         "urgent_set": ["critical"],
         "required": false
       },
       {
         "name": "sentiment",
         "display_name": {"default": "Customer tone", "zh": "客户语气", "en": "Customer tone"},
         "description": {
           "default": "The emotional tone of the user toward the product or team.",
           "zh": "用户对产品或团队表达出的情绪状态。",
           "en": "The emotional tone of the user toward the product or team."
         },
         "kind": "single",
         "taxonomy": [
           {
             "value": "positive",
             "display_name": {"default": "Positive", "zh": "正向", "en": "Positive"},
             "description": {
               "default": "Praise, appreciation, delight, or successful outcome.",
               "zh": "表扬、感谢、满意或成功完成任务。",
               "en": "Praise, appreciation, delight, or successful outcome."
             }
           },
           {
             "value": "neutral",
             "display_name": {"default": "Neutral", "zh": "中性", "en": "Neutral"},
             "description": {
               "default": "Factual report or request with little emotional charge.",
               "zh": "事实性反馈或请求，几乎没有明显情绪。",
               "en": "Factual report or request with little emotional charge."
             }
           },
           {
             "value": "negative",
             "display_name": {"default": "Negative", "zh": "负向", "en": "Negative"},
             "description": {
               "default": "Dissatisfaction, complaint, disappointment, or criticism.",
               "zh": "不满、抱怨、失望或批评。",
               "en": "Dissatisfaction, complaint, disappointment, or criticism."
             }
           },
           {
             "value": "frustrated",
             "display_name": {"default": "Frustrated", "zh": "挫败", "en": "Frustrated"},
             "description": {
               "default": "Repeated failure, blocked task, refund/cancel/escalation language, or patience running out.",
               "zh": "反复失败、任务受阻、退款/取消/升级投诉，或用户耐心正在耗尽。",
               "en": "Repeated failure, blocked task, refund/cancel/escalation language, or patience running out."
             }
           }
         ],
         "urgent_set": [],
         "required": false,
         "examples": [
           "positive: Thanks, this solved our workflow",
           "neutral: Could you add CSV export?",
           "negative: This is disappointing and confusing",
           "frustrated: I paid for this and it still fails every time"
         ],
         "extraction_hint": "Use frustrated only when patience is running out: repeated failure, blocked work, refund/cancel/escalation language, or clearly heightened anger.",
         "renderer": {
           "kind": "enum_badge",
           "values": {
             "positive": {"icon": "smile", "tone": "success"},
             "neutral": {"icon": "minus", "tone": "muted"},
             "negative": {"icon": "frown", "tone": "warning"},
             "frustrated": {"icon": "flame", "tone": "danger"}
           }
         }
       },
       {
         "name": "labels",
         "display_name": {"default": "Labels", "zh": "标签", "en": "Labels"},
         "kind": "multi",
         "taxonomy": [],
         "urgent_set": [],
         "required": false
       }
   ]'::jsonb;

UPDATE tenants
SET enrich_dimensions = enrich_dimensions || '[
       {
         "name": "sentiment",
         "display_name": {"default": "Customer tone", "zh": "客户语气", "en": "Customer tone"},
         "description": {
           "default": "The emotional tone of the user toward the product or team.",
           "zh": "用户对产品或团队表达出的情绪状态。",
           "en": "The emotional tone of the user toward the product or team."
         },
         "kind": "single",
         "taxonomy": [
           {
             "value": "positive",
             "display_name": {"default": "Positive", "zh": "正向", "en": "Positive"},
             "description": {
               "default": "Praise, appreciation, delight, or successful outcome.",
               "zh": "表扬、感谢、满意或成功完成任务。",
               "en": "Praise, appreciation, delight, or successful outcome."
             }
           },
           {
             "value": "neutral",
             "display_name": {"default": "Neutral", "zh": "中性", "en": "Neutral"},
             "description": {
               "default": "Factual report or request with little emotional charge.",
               "zh": "事实性反馈或请求，几乎没有明显情绪。",
               "en": "Factual report or request with little emotional charge."
             }
           },
           {
             "value": "negative",
             "display_name": {"default": "Negative", "zh": "负向", "en": "Negative"},
             "description": {
               "default": "Dissatisfaction, complaint, disappointment, or criticism.",
               "zh": "不满、抱怨、失望或批评。",
               "en": "Dissatisfaction, complaint, disappointment, or criticism."
             }
           },
           {
             "value": "frustrated",
             "display_name": {"default": "Frustrated", "zh": "挫败", "en": "Frustrated"},
             "description": {
               "default": "Repeated failure, blocked task, refund/cancel/escalation language, or patience running out.",
               "zh": "反复失败、任务受阻、退款/取消/升级投诉，或用户耐心正在耗尽。",
               "en": "Repeated failure, blocked task, refund/cancel/escalation language, or patience running out."
             }
           }
         ],
         "urgent_set": [],
         "required": false,
         "examples": [
           "positive: Thanks, this solved our workflow",
           "neutral: Could you add CSV export?",
           "negative: This is disappointing and confusing",
           "frustrated: I paid for this and it still fails every time"
         ],
         "extraction_hint": "Use frustrated only when patience is running out: repeated failure, blocked work, refund/cancel/escalation language, or clearly heightened anger.",
         "renderer": {
           "kind": "enum_badge",
           "values": {
             "positive": {"icon": "smile", "tone": "success"},
             "neutral": {"icon": "minus", "tone": "muted"},
             "negative": {"icon": "frown", "tone": "warning"},
             "frustrated": {"icon": "flame", "tone": "danger"}
           }
         }
       }
   ]'::jsonb,
    updated_at = NOW()
WHERE NOT enrich_dimensions @> '[{"name":"sentiment"}]'::jsonb
  AND enrich_dimensions @> '[{"name":"type"}]'::jsonb
  AND enrich_dimensions @> '[{"name":"severity"}]'::jsonb
  AND enrich_dimensions @> '[{"name":"labels"}]'::jsonb;

CREATE TABLE IF NOT EXISTS semantic_extraction_runs (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject_type   TEXT NOT NULL,
    subject_id     BIGINT NOT NULL,
    domain_pack    TEXT NOT NULL DEFAULT 'customer_feedback',
    schema_version TEXT NOT NULL DEFAULT 'customer_feedback.v1',
    prompt_version TEXT NOT NULL DEFAULT 'default',
    model          TEXT NOT NULL DEFAULT '',
    guard_summary  JSONB NOT NULL DEFAULT '{}'::jsonb,
    attrs          JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence     JSONB NOT NULL DEFAULT '{}'::jsonb,
    rationale      JSONB NOT NULL DEFAULT '{}'::jsonb,
    dropped_attrs  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (subject_type <> ''),
    CHECK (domain_pack <> ''),
    CHECK (schema_version <> ''),
    CHECK (prompt_version <> '')
);

CREATE INDEX IF NOT EXISTS idx_semantic_extraction_runs_tenant_created
    ON semantic_extraction_runs (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_semantic_extraction_runs_subject
    ON semantic_extraction_runs (subject_type, subject_id, created_at DESC);
