# Semantic search quality platform

| | |
|---|---|
| **Issue** | [#162](https://github.com/Phixsura/attune/issues/162) |
| **Status** | Implemented |
| **Started** | 2026-07-02T18:05:00+08:00 |
| **Related** | [#25](../06/2026-06-12-embedding-clustering.md), [#30](../06/2026-06-14-batch-ops-semantic-search.md), [#171](../06/2026-06-30-console-accessibility-keyboard-triage.md), [semantic search operator workflow](2026-07-02-semantic-search-operator-workflow.md) |

---

## Problem

The feedback workbench now exposes semantic search inside the operator workflow,
but that makes the next product gap visible: attune can retrieve semantically
similar feedback, while top-tier search and feedback-intelligence products turn
retrieval into a measurable decision system.

Before this PR, search behavior was useful but limited:

- lexical search is PostgreSQL `ILIKE` over content and titles;
- hybrid ranking uses weighted score blending rather than rank fusion;
- search tests prove request and UI correctness, but not relevance quality;
- returned hits expose match type and scores, but not the evidence behind a hit;
- semantic result sets are not durable selectors, saved views, or monitored
  operational surfaces;
- customer impact context is not part of ranking or filtering;
- embedding coverage and fallback reasons are observable only indirectly.

This proposal is a platform design spawned by #162's gap analysis. This PR
implements the quality foundation: relevance metrics primitives, PostgreSQL
lexical search, reciprocal rank fusion, ranking metadata, evidence snippets,
coverage metadata, fallback reasons, and Console presentation.

This proposal upgrades search from a workflow feature into a measurable
search-quality and operator-intelligence platform.

## Goals / Non-goals

### Goals

- Add a measurable relevance-quality loop with golden queries, reproducible
  ranking metrics, and regression checks.
- Replace score-scale blending with reciprocal rank fusion across semantic and
  lexical candidate lists.
- Introduce a lexical scorer stronger than `ILIKE`, starting with PostgreSQL
  full-text search and a swappable scorer interface.
- Return evidence with each hit: matched fields, snippets, ranks, and fallback
  reasons.
- Make high-value semantic searches durable through saved views and safe batch
  selectors.
- Add customer and account context to ranking, filtering, and triage views.
- Expose search health: embedding coverage, index freshness, cache hit rate,
  fallback reasons, latency, and cost signals.
- Preserve tenant isolation, existing filters, existing Console workflow, and
  the current `/fb/v1/console/feedback/search` route shape where possible.

### Non-goals

- Do not add a separate external search service as the first implementation
  choice. The first target is PostgreSQL-backed retrieval so deployment stays
  simple.
- Do not replace enrichment classification, clustering, or workflow state
  machines.
- Do not make LLM-generated answers the primary search result. Feedback rows and
  their evidence remain the source of truth.
- Do not expose cross-tenant relevance data, shared embeddings, or global
  query logs.
- Do not allow unbounded batch operations over semantic results.
- Do not include raw customer query text, snippets, or feedback content in logs,
  metrics labels, or long-lived evaluation fixtures.
- Do not require saved semantic views, customer/account impact boosts, or a
  distributed rate limiter before improving the core search result quality.

## Review Findings Addressed

This proposal intentionally fixes several risks in the first draft:

| Finding | Resolution |
|---|---|
| The design was too large for one PR. | Define quality-foundation scope separately from saved views, customer context, health APIs, and distributed rate limiting. |
| It reused #162 as though all work belonged in the same issue. | Treat this as the implemented search-quality extension to #162 while keeping broader platform surfaces explicit. |
| The proto sketch referenced `SearchCoverage` without defining it. | Define `SearchCoverage` and ranking signal fields in the contract sketch. |
| Saved run storage used a single `BIGINT[]`, which is awkward for TTL, auditing, and large result sets. | Replace it with `semantic_search_run_results` rows capped per run. |
| Evidence snippets and evaluation fixtures had weak privacy boundaries. | Add snippet length/redaction rules and fixture anonymization requirements. |
| PostgreSQL full-text search was described as if it solved all language cases. | Add language constraints, CJK fallback behavior, and scorer replacement criteria. |

## Implemented Scope

| Slice | Status in this PR | Notes |
|---|---|---|
| Relevance quality fixtures and baseline | Implemented | `internal/service/semanticsearch/searchquality` computes recall, precision, MRR, NDCG, zero-result rate, tenant leaks, filter leaks, and must-not-match counts; `testdata/search` commits synthetic JSONL fixtures, fixture reference validation, and a deterministic baseline report. The CLI fails explicitly when the current ranking version, top-k, or query count drifts from the committed baseline. |
| PostgreSQL lexical scorer | Implemented | `FeedbackRepo.LexicalSearch` uses `to_tsvector('simple')`, `plainto_tsquery`, field-aware partial fallback, snippets, ranks, and scores. Partial fallback escapes SQL `LIKE` wildcards so user text remains literal. |
| Lexical index | Implemented | Migration `094_feedback_search_quality.sql` adds the matching GIN expression index for live feedback rows. |
| RRF hybrid ranking | Implemented | Semantic and lexical candidates are collected independently, deduplicated, and fused with `rrf_k = 60`. |
| Evidence and metadata contract | Implemented | Proto, Go handlers, OpenAPI, Console TS types, and Node SDK types now expose fallback reason, ranking version, coverage, per-channel ranks, fused score, evidence, and ranking signals. |
| Console evidence display | Implemented | The feedback workbench shows coverage, fallback reason, ranking version, match rank tooltips, and compact evidence snippets. |
| Search health metrics | Implemented | Prometheus now exposes fallback reason counts and embedding coverage ratio alongside existing query count, latency, result count, and cache metrics. |
| Input hardening and browser workflow gates | Implemented | Search query validation now rejects empty, overlong, and unsupported-control-character input before FTS or embedding work. Playwright covers the terminal semantic-search workflow, fallback state, result evidence, detail opening, document overflow, and axe checks. |
| Performance harness | Implemented | `scripts/perf-test-search.sh` provides HTTP load timing and optional PostgreSQL `EXPLAIN (ANALYZE, BUFFERS)` for the lexical search plan on real tenant data. |
| Saved semantic views | Designed | Schema and selector semantics are specified for a dedicated implementation issue. |
| Customer/account context boosts | Designed | Data model and transparent boost semantics are specified for a dedicated implementation issue. |
| Search health admin API and distributed limiter | Designed | Metrics, admin API shape, and limiter behavior are specified for dedicated implementation issues. |

## Industry Baseline

| Product / technology | Relevant pattern | attune design response |
|---|---|---|
| Azure AI Search | Hybrid retrieval uses reciprocal rank fusion to merge full-text and vector result lists. | Make RRF the default hybrid ranking primitive. |
| Weaviate hybrid search | Keyword and vector search are fused with tunable weighting. | Keep tunable lexical/vector weighting, but make the score combination rank-based. |
| Algolia NeuralSearch | Keyword and neural relevance are exposed through one operational search surface. | Keep search inside the feedback workbench and avoid a separate semantic silo. |
| Elasticsearch | RRF and hybrid ranking are explicit search primitives. | Track per-channel rank and fused rank in every hit. |
| GitHub code search | Exact search remains important beside semantic discovery. | Keep keyword mode and lexical candidates even when semantic search is available. |
| Linear search | Search respects active work context and supports fast issue navigation. | Reuse workbench filters, queue state, and detail-sheet navigation. |
| Zendesk Intelligent Triage | AI classification powers views, routing, reports, and agent correction loops. | Make semantic views durable and keep operator correction signals. |
| Productboard | AI links feedback to product work and summarizes evidence. | Attach search hits to feedback detail, cluster, workflow, and product-context surfaces. |
| Pendo Listen | Natural-language exploration is paired with feedback evidence and themes. | Return evidence snippets and theme/context metadata with each hit. |
| Enterpret | Customer-feedback intelligence emphasizes taxonomy, segmentation, and impact. | Add account, segment, plan, and impact facts to filters and ranking boosts. |

## Current attune State

| Layer | Current behavior | Gap |
|---|---|---|
| Console | Operators can switch keyword/semantic mode, act on semantic results, and inspect coverage, fallback, ranking version, rank tooltip, and compact evidence context. | No saved semantic views, query history, durable selectors, or account-impact ranking controls. |
| Handler | Validates trimmed query and limit, maps filters, hydrates tags/workflow, and returns fallback reason, coverage, ranking metadata, and evidence. | No run ID or saved-search selector contract. |
| Service | Checks embedding availability, caches query embeddings, runs semantic and lexical search, fuses candidates with RRF, and records fallback/coverage metrics. | No distributed limiter or durable search-run persistence. |
| Repository | pgvector HNSW for semantic search; PostgreSQL full-text lexical search with field-aware literal partial fallback. | Lexical relevance still lacks BM25-grade tuning, typo tolerance, and language-specific tokenization beyond the simple configuration. |
| Data | Feedback has embeddings, enriched attributes, cluster fields, workflow state, tags. | Customer/account facts and durable search selector state are missing. |
| Observability | Query count, duration, result count, cache hit/miss. | Missing fallback reason, coverage SLO, index freshness, per-channel rank, quality metrics, and tenant budget. |

## Proposal

### 1. Relevance evaluation harness

Add a first-class relevance test suite before changing ranking behavior.

Data files:

- `testdata/search/golden_feedback.jsonl`
- `testdata/search/golden_queries.jsonl`
- `testdata/search/golden_expected.jsonl`

Fixture rules:

- Use synthetic or explicitly anonymized feedback.
- Do not commit raw tenant exports.
- Include an attribution/license note when public datasets are used.
- Keep IDs deterministic and local to the fixture.
- Store precomputed embeddings only for public/synthetic text.

Golden query schema:

```json
{
  "id": "checkout-invoice-failure",
  "query": "customers cannot complete checkout after selecting invoice billing",
  "filters": {"attrs": [{"dim": "severity", "value": "P0"}]},
  "relevant_feedback_ids": [101, 118, 144],
  "must_not_match_feedback_ids": [203],
  "intent": "incident_triage"
}
```

Metrics:

- `recall_at_10`
- `precision_at_10`
- `mrr_at_10`
- `ndcg_at_10`
- `zero_result_rate`
- `filter_leak_count`
- `tenant_leak_count`

Implementation:

- Add `internal/service/semanticsearch/searchquality` test helpers.
- Add deterministic synthetic fixtures for service-level ranking tests. The
  current baseline has 25 synthetic queries across support, security,
  integration, mobile, workflow, and accessibility scenarios.
- Add PostgreSQL integration fixtures for SQL scorer behavior.
- Record baseline metrics for current RRF hybrid search.
- Gate ranking changes by comparing aggregate and per-query metrics against the
  committed baseline.
- Fail with a contract-specific error when the ranking version, top-k cutoff, or
  query count changes without rewriting the committed baseline.
- Output a machine-readable report, for example
  `testdata/search/baseline/semanticsearch.v1.json`, so CI failures can show
  which query regressed.

Acceptance:

- Tenant and filter leak counts are always zero.
- Hybrid search improves or preserves aggregate `ndcg_at_10` and `mrr_at_10`
  against the committed baseline.
- Queries with exact IDs, copied titles, or error tokens still rank the exact
  lexical match above unrelated semantic matches.

### 2. Lexical scorer stronger than `ILIKE`

Introduce a lexical candidate generator beside pgvector search.

Repository API:

```go
type LexicalSearchParams struct {
    TenantID string
    Query    string
    Limit    int
    Filter   *FeedbackFilter
}

type LexicalSearchHit struct {
    Feedback *SearchFeedback
    Rank     int
    Score    float64
    Fields   []string
    Snippets []SearchSnippet
}
```

Database changes:

- Add a generated or maintained `search_document` `tsvector` over:
  `content`, `enriched_title`, `enriched_display_title`,
  `enriched_rationale`, selected textual enriched attributes, and source IDs.
- Add a GIN index on the `search_document` expression.
- Keep tenant, soft-delete, workflow, tag, and attr filters as ordinary
  predicates around the lexical query.
- Add optional `pg_trgm` support for short token, ID-like token, and
  typo-tolerant fallback.
- Keep `ILIKE` only as a compatibility fallback when full-text search cannot be
  used.

Language behavior:

- Use PostgreSQL `simple` configuration for the first scorer so multilingual
  feedback does not disappear because of incorrect stemming.
- Add English-weighted stemming only for rows with reliable `language = 'en'`.
- For CJK and other segmentation-heavy languages, rely on exact token, trigram,
  title, and semantic candidates until a language-aware tokenizer is introduced.

Ranking behavior:

- Use `websearch_to_tsquery` or `plainto_tsquery` depending on query shape.
- Weight fields:
  - exact ID/source token match: highest lexical priority;
  - enriched title/display title: high;
  - content: normal;
  - rationale and textual attrs: supporting.
- Normalize lexical candidates by rank, not raw score.

The lexical scorer is named generically so a BM25 implementation can replace the
PostgreSQL scorer without changing service or handler contracts.

Replacement criteria:

- PostgreSQL full-text search becomes insufficient if golden-query regressions
  cluster around long keyword queries, heavy typo tolerance, or CJK lexical
  matching.
- A dedicated search backend requires an updated deployment proposal covering
  indexing, backfill, disaster recovery, and tenant isolation.

### 3. Reciprocal rank fusion

Replace weighted raw-score blending with RRF.

Formula:

```text
fused_score = sum(channel_weight / (rrf_k + channel_rank))
```

Defaults:

- `rrf_k = 60`
- `semantic_weight = 0.70`
- `lexical_weight = 0.30`

Behavior:

- Run semantic and lexical candidate generation independently.
- Keep enough candidates per channel, `limit * 4` capped at 100.
- Deduplicate by feedback ID.
- Preserve per-channel rank and raw score on each hit.
- Assign match type:
  - `hybrid` when both channels returned the row;
  - `semantic` when only vector retrieval returned the row;
  - `keyword` when only lexical retrieval returned the row.

RRF makes hybrid ranking robust when semantic similarity and lexical scores have
different scales.

### 4. Evidence and explainability contract

Extend the search response with evidence that operators can trust.

Proto shape:

```proto
message SearchCoverage {
  int32 total_live_feedback = 1;
  int32 total_with_embeddings = 2;
  string embedding_model = 3;
}

message SearchEvidence {
  string field = 1;
  string snippet = 2;
  string reason = 3;
}

message SemanticSearchHit {
  Feedback feedback = 1;
  float similarity = 2;
  float keyword_score = 3;
  string match_type = 4;
  int32 semantic_rank = 5;
  int32 lexical_rank = 6;
  float fused_score = 7;
  repeated SearchEvidence evidence = 8;
  repeated string ranking_signals = 9;
}

message SemanticSearchResponse {
  repeated SemanticSearchHit hits = 1;
  string embedding_model = 2;
  int32 total_with_embeddings = 3;
  bool used_keyword_fallback = 4;
  optional string fallback_reason = 5;
  string ranking_version = 6;
  optional SearchCoverage coverage = 7;
}
```

Evidence sources:

- lexical matched fields and highlighted snippets;
- semantic title/content summary for vector-only hits;
- filters that constrained the result set;
- cluster label or workflow state when it helps explain why the item belongs in
  the current working set.

Safety rules:

- Snippets are capped before response serialization.
- Snippets are generated only from fields the current user can already view.
- Snippets are never written to infrastructure logs.
- Audit rows store the search run metadata and result IDs, not full snippets.
- Export paths that include snippets must pass the same permission checks as
  feedback detail export.

Console behavior:

- Show a compact evidence snippet below the title.
- Keep score badges as secondary metadata.
- Display fallback reason in the degraded-state banner.
- Allow opening the row detail without leaving the search context.

### 5. Durable semantic views and safe selectors

Add saved search views for operational work.

Database:

```sql
CREATE TABLE feedback_search_views (
    id UUID PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    query TEXT NOT NULL,
    filter JSONB NOT NULL DEFAULT '{}'::jsonb,
    mode TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_feedback_search_views_name
    ON feedback_search_views (tenant_id, owner_user_id, lower(name))
    WHERE archived_at IS NULL;
```

Add `semantic_search_runs` for audit and batch safety:

```sql
CREATE TABLE semantic_search_runs (
    id UUID PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    query TEXT NOT NULL,
    filter JSONB NOT NULL DEFAULT '{}'::jsonb,
    ranking_version TEXT NOT NULL,
    result_count INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE semantic_search_run_results (
    run_id UUID NOT NULL REFERENCES semantic_search_runs(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    feedback_id BIGINT NOT NULL,
    rank INT NOT NULL,
    match_type TEXT NOT NULL,
    fused_score DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (run_id, feedback_id)
);

CREATE INDEX idx_semantic_search_run_results_run_rank
    ON semantic_search_run_results (run_id, rank);

CREATE INDEX idx_semantic_search_runs_expiry
    ON semantic_search_runs (expires_at);
```

Batch behavior:

- Batch operations can target a saved run by `search_run_id`.
- The request still requires `confirm_count` and `max_affected`.
- Runs store at most 500 result rows.
- Runs expire after seven days unless attached to a saved view.
- The service revalidates tenant ownership, soft-delete status, and optimistic
  concurrency before mutating rows.
- Selector state is bounded by result IDs, not by re-running a changed query at
  mutation time.

Console behavior:

- Save the current semantic query and filters as a view.
- Open a saved semantic view from the feedback queue deck.
- Show when a view was last run and how many rows it returned.

### 6. Customer and impact context

Add optional customer/account facts to ranking and filters without requiring a
full CRM integration.

Data model:

```sql
CREATE TABLE feedback_context_facts (
    feedback_id BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    account_id TEXT,
    customer_id TEXT,
    segment TEXT,
    plan TEXT,
    revenue_bucket TEXT,
    lifecycle_stage TEXT,
    churn_risk TEXT,
    source_confidence DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (feedback_id)
);
```

Ranking boosts:

- urgent feedback;
- affected account count;
- enterprise or high-impact segment;
- repeated cluster membership;
- recent unresolved workflow state;
- terminal enrichment failures when the active queue is terminal-focused.

Contract:

- Boosts must be transparent in hit metadata.
- Operators can disable impact boosts for pure relevance search.
- The default search ordering remains relevance-first, impact-aware.

### 7. Search operations and governance

Expose a search health surface for admins.

Metrics:

- `attune_search_queries_total{tenant_id,type,fallback_reason}`
- `attune_search_query_duration_seconds{tenant_id,type}`
- `attune_search_results_count{tenant_id}`
- `attune_search_coverage_ratio{tenant_id,model}`
- `attune_search_index_freshness_seconds{tenant_id,index}`
- `attune_search_eval_ndcg{ranking_version}`
- `attune_search_cost_tokens_total{tenant_id,purpose}`

Metric constraints:

- Do not label metrics with raw query text, saved view name, feedback ID, or
  source content.
- Keep `tenant_id` labels consistent with the existing project metrics pattern.
- Use bounded enum values for `type`, `fallback_reason`, `model`, and `index`.

Admin API:

- embedding coverage by tenant and model;
- lexical index freshness;
- query cache hit/miss;
- fallback reason distribution;
- saved view count and run count;
- rate-limit and budget usage.

Rate limiting:

- Keep the `ratelimit.SlidingLimiter` interface.
- Add a PostgreSQL-backed implementation for multi-process deployments.
- Preserve fail-open behavior only when the limiter storage is temporarily
  unavailable and log the condition with tenant and limiter key.

## Ranking Versioning

Every search response carries a `ranking_version`, for example:

```text
rrf.pgfts.v1.k60
```

The version is stored in `semantic_search_runs` and in quality baselines. This
lets us compare relevance metrics across ranking changes and explain why a saved
run produced a specific ordering.

## Implementation Plan

| Step | Scope | Status |
|---|---|---|
| 1 | Add relevance metric primitives for service-level quality checks. | Implemented |
| 2 | Add the lexical scorer API and PostgreSQL full-text implementation. | Implemented |
| 3 | Replace weighted blending with RRF while preserving the existing request contract. | Implemented |
| 4 | Add rank, evidence, fallback, coverage, and ranking-version metadata to proto, Go handlers, TS clients, OpenAPI, and Console result rows. | Implemented |
| 5 | Add golden relevance fixtures and a machine-readable baseline report. | Implemented |
| 6 | Add `feedback_search_views` and `semantic_search_runs`. | Designed |
| 7 | Wire saved semantic views into the Console queue deck and batch selector. | Designed |
| 8 | Add context facts and transparent impact boosts. | Designed |
| 9 | Add search admin health metrics and PostgreSQL-backed distributed rate limiting. | Designed |

## Scope Slices

The platform is split into reviewable slices so each acceptance gate can be
verified independently.

| Slice | Scope | Acceptance gate |
|---|---|---|
| P0-A | Relevance fixtures, baseline report, metrics, and regression comparison. | Metric calculations cover recall, precision, MRR, NDCG, zero-result rate, tenant leaks, filter leaks, and must-not-match counts; committed synthetic fixtures validate their corpus/query/result references and reproduce the baseline report. |
| P0-B | PostgreSQL lexical scorer with full-text and field-aware partial fallback. | Exact-token queries rank lexical matches first; integration tests cover snippets, filters, and literal `%` / `_` wildcard handling. |
| P0-C | RRF merge and ranking version metadata. | Service tests cover deduplication, ranks, fused score, fallback reasons, and candidate fallback behavior. |
| P1-A | Evidence contract and Console snippets. | Snippets obey permission and length rules; fallback reason is visible. |
| P1-B | Search health metrics and admin status API. | Coverage ratio, fallback reason, and cache hit/miss are visible without query text; freshness/status API is outside this implementation. |
| P1-C | Saved semantic views and run snapshots. | Saved views can be opened; batch selectors require capped run ID plus confirmation. |
| P2-A | Customer/account context facts and transparent impact boosts. | Boost signals are visible and can be disabled. |
| P2-B | PostgreSQL-backed distributed rate limiter. | Multi-process rate-limit integration test proves shared tenant quota. |

## Alternatives considered

| Alternative | Decision |
|---|---|
| Keep `ILIKE` and weighted score blending | Rejected. It cannot support exact-token quality, scale-independent fusion, or measurable improvement against top-tier hybrid search patterns. |
| Move immediately to Elasticsearch/OpenSearch | Rejected as the first step. It adds operational dependency before measuring whether PostgreSQL-backed lexical retrieval is insufficient. |
| Use only semantic search | Rejected. IDs, pasted text, product names, error codes, and exact titles require lexical retrieval. |
| Use an LLM to rerank every query | Rejected as the default path. Latency, cost, and determinism are poor for the main operator workflow. Reranking can be an explicit premium path after deterministic retrieval is measurable. |
| Store only saved query text, not result snapshots | Rejected for batch operations. Safe mutation needs a bounded, auditable selector. |

## Risks / Tradeoffs

- PostgreSQL full-text search is not BM25. The scorer interface keeps this
  replaceable, while the quality harness determines whether a stronger lexical
  backend is required.
- RRF improves scale independence but can over-promote weak channel matches when
  candidate lists are too broad. Candidate limits and quality metrics must be
  tuned together.
- Evidence snippets may leak sensitive source text into logs or audit exports if
  handled carelessly. Snippets must stay in response bodies and audit summaries,
  not infrastructure logs.
- Saved search runs can become stale as feedback is deleted, restored, or moved
  through workflow states. Batch execution must revalidate rows.
- Customer context boosts can encode tenant-specific business assumptions.
  Boosts must be configurable and visible in hit metadata.

## Verification

- `go test ./internal/service/semanticsearch ./internal/service/semanticsearch/searchquality ./internal/repo/feedback ./internal/handlers/console/feedback`
- `go test -tags=integration ./test/integration/postgres/feedbacksearch`
- `cd console && pnpm tsc -b --noEmit`
- `cd console && pnpm biome check`
- `cd console && pnpm vitest run src/routes/_authed.feedback.test.tsx src/features/feedback/components/semantic-search.test.tsx src/features/feedback/hooks/use-semantic-search.test.tsx`
- `cd console && pnpm exec vite build`
- `buf lint`
- `make proto` when the remote Buf generator host is available
- Search-quality metric primitives:
  - tenant leak count is represented as a first-class metric;
  - filter leak count is represented as a first-class metric;
  - `ndcg_at_k` and `mrr_at_k` are computed for ranked outputs;
  - committed synthetic fixtures validate corpus/query/result references and reproduce `testdata/search/baseline/semanticsearch.v1.json`;
  - exact-token lexical behavior is covered by PostgreSQL integration tests.

## Open Questions

- Should saved semantic views be personal first, tenant-shared first, or both?
- What is the initial retention policy for `semantic_search_runs` in private
  deployments with stricter audit requirements?
- Which customer/account context source should become the first supported
  importer?

## References

- Azure AI Search hybrid ranking: <https://learn.microsoft.com/en-us/azure/search/hybrid-search-ranking>
- Weaviate hybrid search: <https://docs.weaviate.io/weaviate/search/hybrid>
- Algolia NeuralSearch: <https://www.algolia.com/doc/guides/ai-relevance/neuralsearch/>
- Elasticsearch reciprocal rank fusion: <https://www.elastic.co/guide/en/elasticsearch/reference/current/rrf.html>
- pgvector: <https://github.com/pgvector/pgvector>
- Linear search: <https://linear.app/docs/search>
- Zendesk intelligent triage resources: <https://support.zendesk.com/hc/en-us/articles/4471123173402-Intelligent-triage-resources>
- Productboard AI: <https://support.productboard.com/hc/en-us/articles/15113485128467-Productboard-AI>
- Pendo Listen AI feedback exploration: <https://support.pendo.io/hc/en-us/articles/37717114561819-Explore-feedback-with-AI-in-Listen>
