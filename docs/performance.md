# Performance Characteristics

This document describes the performance targets, benchmarks, and tuning guidance
for attune's batch operations and semantic search features (#30).

## Batch Operations

### Performance Targets

| Operation | Batch Size | Target p99 | Notes |
|-----------|------------|------------|-------|
| Tag add/remove (sync) | <= 100 | < 500ms | Within single transaction |
| Workflow transition (sync) | <= 100 | < 500ms | With state validation |
| Soft delete (sync) | <= 100 | < 500ms | Sets deleted_at |
| Hard delete (sync) | <= 100 | < 1s | Cascades to tag assignments |
| Async job creation | any | < 100ms | Returns job ID immediately |
| Job status poll | - | < 50ms | Simple row lookup |

### Rate Limits

Default rate limits per tenant (configurable via `rate_limits` in config):

| Operation | Default Limit | Window |
|-----------|---------------|--------|
| Batch tag/workflow | 30 | 1 minute |
| Batch delete | 10 | 1 minute |
| Search | 60 | 1 minute |

Concurrent async jobs per tenant: 5 (default).

### Batch Size Limits

- **Sync batch operations**: Max 100 items per request (recommended <= 50 for
  sub-second response).
- **Async batch operations**: Max 500 items per job. For larger batches,
  split into multiple jobs.
- **Filter-based ID listing**: Max 10,000 IDs returned.

### Optimistic Locking

The `if_unmodified_since` parameter enables optimistic locking:
- Server compares against each item's `updated_at`
- Items modified after the timestamp return `version_conflict` error
- Use this for UI-driven batch operations where stale data is a concern

## Semantic Search

### Performance Targets

| Operation | Dataset Size | Target p99 | Notes |
|-----------|--------------|------------|-------|
| Query embedding generation | - | < 200ms | First query; cached thereafter |
| Cached embedding lookup | - | < 5ms | TTL: 1 hour |
| pgvector search | 10k items | < 100ms | With HNSW index |
| pgvector search | 100k items | < 500ms | Depends on ef_search |
| Keyword fallback | 10k items | < 50ms | ILIKE with index |

### Index Configuration

The HNSW index is configured with:
```sql
CREATE INDEX idx_user_feedback_embedding_hnsw ON user_feedback
USING hnsw (embedding vector_cosine_ops)
WITH (m = 24, ef_construction = 100);
```

Parameters:
- `m = 24`: Max connections per node (higher = better recall, more memory)
- `ef_construction = 100`: Build-time search width (higher = better index quality)
- `ef_search = 200`: Query-time search width (set per-query via `SET LOCAL`)

### Tuning ef_search

Higher `ef_search` improves recall at the cost of latency:

| ef_search | ~Recall@10 | ~Latency (10k items) |
|-----------|------------|----------------------|
| 40 | 85% | 20ms |
| 100 | 95% | 50ms |
| 200 | 98% | 100ms |
| 400 | 99% | 200ms |

Default is 200 for balanced recall/latency. Configure via search request or
server config.

### Embedding Model Compatibility

Semantic search requires query embeddings to match the model used for stored
feedback embeddings. The `embedding_model` field tracks this:

- All feedback with embeddings stores the model name
- Search queries must specify matching model
- Mismatched models return empty results (not an error)

Current supported models:
- `text-embedding-3-small` (256 dimensions, OpenAI)
- `text-embedding-3-large` (truncated to 256 dimensions)

## Running Benchmarks

### Go Benchmarks (Database Layer)

```bash
# Run all benchmarks (requires PostgreSQL with pgvector)
go test -tags=integration -bench=. -benchmem ./test/integration/postgres/feedbackbench/...

# Run specific benchmarks
go test -tags=integration -bench=BenchmarkSemanticSearch -benchmem ./test/integration/postgres/feedbackbench/...

# With more iterations for stable results
go test -tags=integration -bench=. -benchmem -count=5 ./test/integration/postgres/feedbackbench/...
```

### HTTP Load Tests

```bash
# Install hey if not present
go install github.com/rakyll/hey@latest

# Run load tests against local server
./scripts/perf-test-batch.sh

# Against production (use with caution)
BASE_URL=https://api.example.com API_KEY=xxx ./scripts/perf-test-batch.sh

# With custom parameters
REQUESTS=1000 CONCURRENCY=50 ./scripts/perf-test-batch.sh
```

## Scaling Considerations

### Database

- **Connection pooling**: Use pgbouncer or similar for high concurrency
- **Read replicas**: Search operations can use read replicas
- **Partitioning**: Consider partitioning `user_feedback` by `tenant_id` for
  multi-tenant deployments with uneven load

### Vector Search Scaling

For datasets > 100k items per tenant:
1. Increase `m` parameter (rebuild index required)
2. Consider approximate count with `EXPLAIN` for large filter queries
3. Add covering indexes for common filter combinations

### Memory

pgvector HNSW index memory usage:
- ~1.5KB per 256-dim vector in index
- 100k vectors ~ 150MB index memory
- 1M vectors ~ 1.5GB index memory

Ensure sufficient `shared_buffers` and `work_mem` for large deployments.

## Monitoring

Key metrics to monitor (exposed via `/metrics`):

```
attune_batch_operation_duration_seconds{operation="tag",batch_size="..."}
attune_batch_items_processed_total{operation="...",status="succeeded|failed|skipped"}
attune_search_duration_seconds{type="semantic|keyword"}
attune_search_results_total{type="..."}
attune_embedding_cache_hits_total
attune_embedding_cache_misses_total
attune_async_job_queue_size
attune_async_job_duration_seconds
```

Recommended alerts:
- Batch operation p99 > 1s
- Search p99 > 500ms
- Async job queue depth > 100
- Embedding cache hit rate < 80%
