# Embedding-Cluster Digest Theme Extraction

| Field | Value |
|-------|-------|
| Issue | #27 (Digest Enrichment) |
| Status | Implemented |
| Started | 2026-06-15 |
| Related | #25 (Embedding backfill) |

## Problem

Current digest theme extraction (`internal/service/digest/naive.go`) has critical quality issues:

1. **Hardcoded 3 themes** — `naiveThemeCap = 3` regardless of data volume
2. **Pure LLM grouping** — asks LLM to read all feedback and group in one call
3. **Inaccurate assignments** — LLM "guesses" which IDs belong to which theme
4. **Wrong examples** — example quotes often don't match the theme title
5. **No semantic foundation** — ignores existing embedding infrastructure

Real-world test (100 feedback items):
```
📊 Daily Digest — 2026-06-15
1. [NEW] 搜索功能问题 — 4 reports
2. [NEW] 支付与退款问题 — 6 reports  
3. [NEW] 客服与响应问题 — 10 reports
   > "第三方登录失败"  ← Wrong! This is login, not customer service
```

## Goals

1. **Automatic theme count** — discover 5-15 themes based on data, not hardcoded
2. **Semantic clustering** — use embedding similarity, not LLM guessing
3. **Accurate examples** — select quotes closest to cluster centroid
4. **LLM for naming only** — clustering does grouping, LLM does labeling
5. **Reuse existing infra** — pgvector embeddings from #25

## Non-goals

- Hierarchical topics (future iteration)
- Cross-tenant topic discovery
- Real-time topic assignment (batch is fine)

## Proposal

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Digest Theme Pipeline                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. FETCH         2. CLUSTER           3. NAME          4. RENDER│
│  ┌─────────┐      ┌───────────┐       ┌─────────┐      ┌───────┐│
│  │ pgvector│ ───▶ │ HDBSCAN   │ ───▶  │ LLM     │ ───▶ │ Card  ││
│  │ vectors │      │ (in-memory)│       │ per-cluster│   │ JSON  ││
│  └─────────┘      └───────────┘       └─────────┘      └───────┘│
│                                                                  │
│  Already exists   New: Go impl        Simplified      Unchanged  │
│  from #25         or call Python      prompt                     │
└─────────────────────────────────────────────────────────────────┘
```

### Step 1: Fetch Embeddings

```sql
SELECT f.id, f.title, f.content, e.embedding
FROM user_feedback f
JOIN feedback_embeddings e ON f.id = e.feedback_id
WHERE f.tenant_id = $1 
  AND f.created_at BETWEEN $2 AND $3
  AND f.enrichment_status = 'done'
```

### Step 2: Cluster (HDBSCAN with PCA)

**Implementation**: Pure Go HDBSCAN with PCA dimensionality reduction.

High-dimensional embeddings (384 dims) suffer from the "curse of dimensionality"
where all pairwise distances converge. BERTopic uses UMAP; we use PCA as a
simpler alternative that works well for feedback clustering.

Pipeline:
1. PCA: 384 dims → 20 dims (preserves variance, removes noise dimensions)
2. HDBSCAN: density-based clustering on reduced space

Parameters:
```go
type Clusterer struct {
    MinClusterSize int  // 3: minimum feedback per theme
    MinSamples     int  // 2: core point density  
    ReduceDims     int  // 20: PCA target dimensions
}
```

Tested: Without PCA finds 1 cluster; with PCA(20) correctly finds 3 clusters
on synthetic 384-dim data with separation ratio 2.2.

### Step 3: Name Clusters (LLM)

For each cluster, send only its members to LLM:

```go
prompt := fmt.Sprintf(`Name this group of %d user feedback items.
Give a specific, actionable theme title (3-8 words).

Feedback:
%s

Respond with JSON: {"title": "...", "summary": "one sentence"}`,
    len(cluster.Members),
    formatMembers(cluster.Members), // id: title — rationale
)
```

Much simpler than current naive prompt which asks LLM to group AND name.

### Step 4: Select Examples

For each cluster:
1. Compute centroid = mean(member embeddings)
2. Rank members by cosine similarity to centroid
3. Pick top 2-3 as representative examples

```go
func selectExamples(members []FeedbackRow, embeddings [][]float32, n int) []FeedbackRow {
    centroid := meanVector(embeddings)
    type scored struct {
        row   FeedbackRow
        score float64
    }
    var items []scored
    for i, emb := range embeddings {
        items = append(items, scored{members[i], cosineSim(emb, centroid)})
    }
    sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })
    result := make([]FeedbackRow, 0, n)
    for i := 0; i < n && i < len(items); i++ {
        result = append(result, items[i].row)
    }
    return result
}
```

### Fallback

If clustering fails or returns 0 clusters:
- Fall back to current naive LLM path
- Log warning for observability

If embedding coverage < 80%:
- Use naive path
- Log that embeddings are incomplete

## Alternatives Considered

### A) BERTopic via Python

Pros: Mature, handles UMAP+HDBSCAN+c-TF-IDF
Cons: Requires Python sidecar, deployment complexity

### B) k-means clustering

Pros: Simpler algorithm
Cons: Requires specifying k upfront, doesn't handle noise

### C) LLM-only with better prompt

Pros: No new code
Cons: Doesn't scale, still prone to grouping errors

## Risks / Tradeoffs

| Risk | Mitigation |
|------|------------|
| Go HDBSCAN quality | Compare with Python reference impl in tests |
| Large tenant perf | Cap at 1000 feedback per digest, sample if more |
| Empty clusters | MinClusterSize=3 filters noise to "unclustered" |
| LLM naming cost | 1 call per cluster (5-10) vs 1 call total — ~5x cost but much better quality |

## Implementation Plan

1. **Add HDBSCAN Go package** — `internal/pkg/hdbscan/` with core algorithm
2. **Add cluster namer** — `internal/service/digest/cluster.go` 
3. **Update aggregator** — Use cluster namer when embeddings available
4. **Integration test** — Real LLM + real embeddings test
5. **Benchmark** — Compare naive vs cluster on same dataset

## Verification

1. **Unit tests**: Cluster algorithm correctness
2. **Integration test**: `TestRealLLM_ClusterThemes` with real embeddings
3. **E2E test**: 100-item digest should show 5-10 themes with accurate examples
4. **Quality check**: Example quotes must match theme title semantically

## References

- [BERTopic HDBSCAN docs](https://maartengr.github.io/BERTopic/getting_started/clustering/clustering.html)
- [HDBSCAN paper](https://arxiv.org/abs/1911.02282)
- [k-LLMmeans: Summaries as Centroids](https://arxiv.org/html/2502.09667v1)
- Intercom Topics Explorer architecture
- Dovetail Magic Cluster (Amazon Bedrock)
