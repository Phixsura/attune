# Semantic search operator workflow

| | |
|---|---|
| **Issue** | [#162](https://github.com/Phixsura/attune/issues/162) |
| **Status** | Implemented |
| **Started** | 2026-07-02T16:27:08+08:00 |
| **Related** | [#30](../06/2026-06-14-batch-ops-semantic-search.md), [#171](../06/2026-06-30-console-accessibility-keyboard-triage.md) |

---

## Problem

attune already has most of the semantic search infrastructure: a proto contract,
console handler, tenant-scoped service, pgvector repository queries, query
embedding cache, metrics, and standalone Console search components. The missing
piece is the operator workflow requested by #162.

Today the feedback workbench still uses the ordinary list endpoint for the main
search box. Operators can type exact text and combine it with filters, but they
cannot ask natural-language questions such as "billing complaints after failed
checkout" and then act on the returned working set with the same detail,
selection, and queue context as the ordinary feedback list.

The result is a split implementation: the backend can answer semantic search
requests, but the production Console path does not expose that capability.

## Goals / Non-goals

### Goals

- Add an explicit search mode to the feedback workbench so operators can choose
  keyword search or semantic search without leaving the page.
- Reuse the existing `POST /fb/v1/console/feedback/search` endpoint and send the
  active feedback filters with semantic requests.
- Make semantic results become the current visible working set, preserving
  detail-sheet navigation and result metadata.
- Show clear fallback, empty, loading, and error states so operators understand
  when semantic search is unavailable and keyword results are being shown.
- Keep keyboard and accessible-name behavior aligned with the existing Console
  accessibility work.
- Fix small backend correctness issues that directly affect #162 semantics.
- Land the first search-quality foundation needed for the workflow: PostgreSQL
  lexical scoring, reciprocal rank fusion, evidence snippets, fallback reasons,
  coverage metadata, and ranking-version visibility.

### Non-goals

- Do not replace the ordinary paginated feedback list endpoint.
- Do not add query-language syntax, saved searches, or global command palette
  behavior.
- Do not redesign feedback clustering, enrichment, or workflow state machines.
- Do not add a dedicated external search service in this PR.
- Do not add a distributed rate limiter here. The current service keeps the
  existing in-memory limiter; production multi-instance rate limiting should be
  handled as an infrastructure change.

## Current code reality

| Area | Current state | Consequence for #162 |
|---|---|---|
| Proto | `SearchService.SemanticSearch` is defined at `/fb/v1/console/feedback/search` with query text, filters, weights, and result metadata. | No new endpoint shape is required for the first workflow integration. |
| Handler | The Console handler validates query and limit, maps filters, and returns rate-limit and empty-query errors. | The UI can call the existing route through `useSemanticSearch`. |
| Service | The service checks embeddings, generates or caches query embeddings, verifies model compatibility, fuses semantic and lexical candidates with RRF, and records metrics. | Backend behavior is usable and now exposes fallback reasons, coverage, evidence, and ranking version metadata. |
| Repository | Semantic search uses pgvector HNSW and tenant filters. Lexical fallback uses PostgreSQL full-text search with a literal partial-match fallback. | The workflow can ship with a measurable baseline while leaving a dedicated search backend out of scope. |
| Console components | `SemanticSearchBar`, `SearchResults`, and `useSemanticSearch` exist with focused tests. | They should be wired into `FeedbackPage` instead of remaining standalone. |
| Feedback page | The main page holds filters, queue mode, detail selection, batch selection, and the ordinary keyword search input. | Semantic results must plug into this page rather than a separate modal. |

## Industry findings

| Product / technology | Pattern | attune decision |
|---|---|---|
| Linear search | Search lives in the active work surface and respects the user's current scope. | Put semantic search inside the feedback workbench filter surface. |
| GitHub semantic code search | Semantic retrieval helps when exact terms are unknown, but exact search remains available. | Keep a visible keyword mode and add semantic mode beside it. |
| Zendesk Intelligent Triage | AI classification feeds queues, routing, views, and reporting. | Returned results must remain operational, not just informational. |
| Dovetail | AI search gives evidence-backed discovery over customer feedback. | Preserve result snippets and match badges so operators can trust matches. |
| Productboard AI | Feedback discovery connects to product action and duplicate evidence. | Keep detail-sheet navigation and cluster/workflow context attached. |
| Pendo Listen | Natural-language exploration still combines with filters. | Send current filters with semantic search requests. |
| Enterpret | Taxonomy and themes are part of the action loop. | Avoid stripping enriched metadata from search result rows. |
| Algolia NeuralSearch | Neural and keyword relevance are combined rather than exposed as separate data stores. | Treat semantic search as a mode over the same feedback corpus. |
| Azure AI Search | Hybrid retrieval combines vector and full-text results, commonly with reciprocal rank fusion. | Use RRF for the hybrid result set now and expose ranking version metadata. |
| Weaviate hybrid search | Operators can tune keyword/vector weighting and inspect score behavior. | Preserve match-type and score metadata in the UI. |

## Proposal

### 1. Add search mode to the feedback filter surface

Add a compact segmented control beside the existing search input:

- Keyword mode keeps the ordinary list search behavior.
- Semantic mode submits the query through `useSemanticSearch`.
- Clearing the query clears the active semantic result set.

This keeps the page familiar while making semantic search discoverable.

### 2. Reuse active filters

Build the semantic request filter from the same state used by the ordinary list
query: urgent, tag, workflow state, enrichment status, terminal-failed-only, and
attribute filters. The visible result set should represent "semantic search
within the current workbench scope."

### 3. Make semantic results the working set

When a semantic response is active:

- derive the displayed feedback rows from `hits[].feedback`;
- keep result metadata by feedback ID for match-type and score badges;
- preserve row click/detail-sheet behavior;
- avoid infinite-scroll controls because semantic search returns a bounded
  ranked set.

Selection and batch actions continue to operate on the returned row IDs. Durable
semantic selectors would be required before adding "select all semantic
matches."

### 4. Surface fallback and error states

The UI should distinguish:

- loading semantic search;
- no results for the current query and filters;
- semantic unavailable with keyword fallback;
- rate limited or other request errors.

The API exposes `used_keyword_fallback`, `fallback_reason`, coverage, evidence,
and `ranking_version`, so the Console can show a degraded-state explanation and
the ranking contract used for a result set.

### 5. Tighten backend correctness

Fix correctness details that directly affect operator trust:

- filter embedding availability checks to live feedback rows so deleted feedback
  does not make a tenant appear semantically searchable;
- trim search queries before execution and reject whitespace-only requests;
- treat `%`, `_`, and `\` as literal characters in lexical partial-match
  fallback;
- expose fallback reasons and ranking version metadata through the contract and
  Console.

## Alternatives considered

| Alternative | Why not |
|---|---|
| New semantic search page | Splits the operator flow and loses selection/detail/filter context. |
| Replace keyword search entirely with semantic search | Exact search remains important for IDs, titles, and pasted customer text. |
| Add a modal command palette first | Useful for command-driven navigation, but #162 is specifically about the feedback operator workflow. |
| Ship the UI without ranking-quality work | Rejected. The operator workflow is much more trustworthy when it ships with lexical scoring, RRF metadata, fallback reasons, and a baseline gate. |

## Risks / tradeoffs

- Semantic search returns a bounded result set. Operators should not interpret
  it as "all matching feedback" until durable semantic selectors exist.
- PostgreSQL full-text search is a pragmatic first lexical scorer, not a
  dedicated BM25/search-service replacement. The committed relevance baseline
  should guide any future scorer swap.
- In-memory rate limiting is not multi-instance safe. This PR preserves current
  behavior and avoids expanding infrastructure scope.

## Implementation plan

1. Add this proposal and update `CHANGELOG.md`.
2. Wire `useSemanticSearch` into `FeedbackPage`.
3. Add search mode UI, semantic result metadata, fallback banners, and empty
   states.
4. Keep keyword mode behavior unchanged.
5. Fix backend correctness issues for live-row embedding checks, trimmed
   queries, and literal partial-match fallback.
6. Add PostgreSQL lexical scoring, RRF metadata, evidence snippets, fallback
   reasons, coverage metadata, and search-quality baseline checks.
7. Add focused Console tests for mode switching, semantic request filters,
   fallback state, and result selection.
8. Add or update backend tests for search fallback reasons, ranking metadata,
   and repository lexical behavior.

## Verification

- `go test ./internal/repo/feedback ./internal/service/semanticsearch ./internal/handlers/console/feedback`
- `go test -tags=integration ./test/integration/postgres/feedbacksearch`
- `go build ./...`
- `go vet ./internal/handlers/console/feedback ./internal/repo/feedback ./internal/service/semanticsearch ./cmd/attune`
- `cd console && pnpm tsc -b --noEmit`
- `cd console && pnpm biome check`
- `cd console && pnpm vitest run src/routes/_authed.feedback.test.tsx src/features/feedback/components/semantic-search.test.tsx src/features/feedback/hooks/use-semantic-search.test.tsx`
- `cd console && pnpm exec vite build`
- `buf lint`
- `bash scripts/check-search-quality.sh`
- `scripts/lint-artifacts.sh --strict`
- `scripts/lint-slog.sh --strict`
- `scripts/lint-rawptr.sh`
- `scripts/lint-errorcode.sh`
- `scripts/lint-integration-layout.sh`
- `git diff --check`

## References

- [Issue #162](https://github.com/Phixsura/attune/issues/162)
- [Linear search documentation](https://linear.app/docs/search)
- [Azure AI Search hybrid ranking](https://learn.microsoft.com/en-us/azure/search/hybrid-search-ranking)
- [Weaviate hybrid search](https://docs.weaviate.io/weaviate/search/hybrid)
- [pgvector README](https://github.com/pgvector/pgvector)
