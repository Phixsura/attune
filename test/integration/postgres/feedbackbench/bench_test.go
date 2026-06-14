//go:build integration

// Package feedbackbench_test — performance benchmarks for batch and search operations (#30).
// Run with: go test -tags=integration -bench=. ./test/integration/postgres/feedbackbench/...
package feedbackbench_test

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/testdb"
)

type benchEnv struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	feedback *feedback.FeedbackRepo
	tags     *feedbacktag.Repo
	tenantID string
}

func setupBench(b *testing.B) benchEnv {
	b.Helper()

	// Use a testing.T wrapper for testdb.NewPool which requires *testing.T.
	// For benchmarks we create a mini test that just gets the pool.
	var pool *pgxpool.Pool
	t := &testing.T{}
	pool = testdb.NewPool(t)
	if pool == nil {
		b.Fatal("failed to create test pool")
	}

	ctx := context.Background()

	// Create tenant.
	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('bench-test', 'Benchmark Test Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID) // ptrext:allow scan-target
	if err != nil {
		b.Fatalf("insert tenant: %v", err)
	}

	// Ensure default workflow state exists.
	_, err = pool.Exec(ctx, `
		INSERT INTO tenant_workflow_states (id, tenant_id, name, color, category, position, is_default)
		VALUES ($1::uuid, $2, 'New', '#666666', 'open', 0, true)
		ON CONFLICT (tenant_id, name) WHERE archived_at IS NULL DO NOTHING`,
		uuid.NewString(), tenantID)
	if err != nil {
		b.Fatalf("insert workflow state: %v", err)
	}

	b.Cleanup(func() {
		pool.Close()
	})

	return benchEnv{
		ctx:      ctx,
		pool:     pool,
		feedback: feedback.NewFeedback(pool),
		tags:     feedbacktag.New(pool),
		tenantID: tenantID,
	}
}

func (e benchEnv) insertFeedback(b *testing.B, content string) int64 {
	b.Helper()
	var id int64
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, workflow_state_id)
		 VALUES ($1, 'u1', 'api', $2,
		   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))
		 RETURNING id`,
		e.tenantID, content,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		b.Fatalf("insert feedback: %v", err)
	}
	return id
}

func (e benchEnv) insertFeedbackBatch(b *testing.B, count int, prefix string) []int64 {
	b.Helper()
	ids := make([]int64, count)
	for i := 0; i < count; i++ {
		content := fmt.Sprintf("%s content item %d with some searchable text", prefix, i)
		ids[i] = e.insertFeedback(b, content)
	}
	return ids
}

func (e benchEnv) insertFeedbackWithEmbedding(b *testing.B, content, title, model string, embedding []float32) int64 {
	b.Helper()
	var id int64
	embStr := vecToString(embedding)
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, embedding, embedding_model, workflow_state_id)
		 VALUES ($1, 'u1', 'api', $2, $3, 'done', $4::vector, $5,
		   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))
		 RETURNING id`,
		e.tenantID, content, title, embStr, model,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		b.Fatalf("insert feedback with embedding: %v", err)
	}
	return id
}

func (e benchEnv) createTag(b *testing.B, name string) *feedbacktag.Tag {
	b.Helper()
	tag, err := e.tags.Create(e.ctx, feedbacktag.Tag{
		TenantID: e.tenantID,
		Name:     name,
		Color:    "#0000ff",
	})
	if err != nil {
		b.Fatalf("create tag %q: %v", name, err)
	}
	return tag
}

// vecToString converts []float32 to pgvector string format "[1.0,2.0,...]".
func vecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(f), 'f', 6, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// generateEmbedding creates a normalized 256-dim embedding.
func generateEmbedding() []float32 {
	emb := make([]float32, 256)
	var norm float32
	for i := range emb {
		emb[i] = rand.Float32()*2 - 1
		norm += emb[i] * emb[i]
	}
	norm = float32(1.0 / (float64(norm) + 1e-8))
	for i := range emb {
		emb[i] *= norm
	}
	return emb
}

// generateSimilarEmbedding creates an embedding similar to the base.
func generateSimilarEmbedding(base []float32, similarity float64) []float32 {
	emb := make([]float32, 256)
	var norm float32
	for i := range emb {
		noise := rand.Float32()*2 - 1
		emb[i] = float32(similarity)*base[i] + float32(1-similarity)*noise
		norm += emb[i] * emb[i]
	}
	norm = float32(1.0 / (float64(norm) + 1e-8))
	for i := range emb {
		emb[i] *= norm
	}
	return emb
}

// BenchmarkBatchUpdateTags_100 benchmarks tag updates on 100 items.
func BenchmarkBatchUpdateTags_100(b *testing.B) {
	e := setupBench(b)
	ids := e.insertFeedbackBatch(b, 100, "batch100")
	tag := e.createTag(b, "bench-tag-100")

	updates := make([]feedback.BatchTagUpdate, len(ids))
	for i, id := range ids {
		updates[i] = feedback.BatchTagUpdate{
			FeedbackID: id,
			AddTags:    []string{tag.ID.String()},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Toggle between add and remove to ensure work is done each iteration.
		if i%2 == 0 {
			for j := range updates {
				updates[j].AddTags = []string{tag.ID.String()}
				updates[j].RemoveTags = nil
			}
		} else {
			for j := range updates {
				updates[j].AddTags = nil
				updates[j].RemoveTags = []string{tag.ID.String()}
			}
		}
		_, err := e.feedback.BatchUpdateTags(e.ctx, e.tenantID, updates, nil)
		if err != nil {
			b.Fatalf("BatchUpdateTags: %v", err)
		}
	}
}

// BenchmarkBatchUpdateTags_500 benchmarks tag updates on 500 items (max batch size).
func BenchmarkBatchUpdateTags_500(b *testing.B) {
	e := setupBench(b)
	ids := e.insertFeedbackBatch(b, 500, "batch500")
	tag := e.createTag(b, "bench-tag-500")

	updates := make([]feedback.BatchTagUpdate, len(ids))
	for i, id := range ids {
		updates[i] = feedback.BatchTagUpdate{
			FeedbackID: id,
			AddTags:    []string{tag.ID.String()},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			for j := range updates {
				updates[j].AddTags = []string{tag.ID.String()}
				updates[j].RemoveTags = nil
			}
		} else {
			for j := range updates {
				updates[j].AddTags = nil
				updates[j].RemoveTags = []string{tag.ID.String()}
			}
		}
		_, err := e.feedback.BatchUpdateTags(e.ctx, e.tenantID, updates, nil)
		if err != nil {
			b.Fatalf("BatchUpdateTags: %v", err)
		}
	}
}

// BenchmarkBatchSoftDelete_100 benchmarks soft delete on 100 items.
func BenchmarkBatchSoftDelete_100(b *testing.B) {
	e := setupBench(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Create fresh items each iteration since they get deleted.
		ids := e.insertFeedbackBatch(b, 100, fmt.Sprintf("delete100_%d", i))
		b.StartTimer()

		_, err := e.feedback.BatchSoftDelete(e.ctx, e.tenantID, ids, nil)
		if err != nil {
			b.Fatalf("BatchSoftDelete: %v", err)
		}
	}
}

// BenchmarkCountByFilter_10k benchmarks count with filters on 10k items.
func BenchmarkCountByFilter_10k(b *testing.B) {
	e := setupBench(b)

	// Insert 10k items with varying urgency.
	b.Log("Inserting 10k items...")
	for i := 0; i < 10000; i++ {
		isUrgent := i%10 == 0 // 10% urgent
		_, err := e.pool.Exec(e.ctx,
			`INSERT INTO user_feedback (tenant_id, user_id, source, content, is_urgent, enrichment_status, workflow_state_id)
			 VALUES ($1, 'u1', 'api', $2, $3, 'done',
			   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))`,
			e.tenantID, fmt.Sprintf("filter test content %d", i), isUrgent)
		if err != nil {
			b.Fatalf("insert: %v", err)
		}
	}

	filter := ptrext.Of(feedback.FeedbackFilter{Urgent: ptrext.Of(true)})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.CountByFilter(e.ctx, e.tenantID, filter)
		if err != nil {
			b.Fatalf("CountByFilter: %v", err)
		}
	}
}

// BenchmarkListIDsByFilter_10k benchmarks ID listing with filters on 10k items.
func BenchmarkListIDsByFilter_10k(b *testing.B) {
	e := setupBench(b)

	// Insert 10k items.
	b.Log("Inserting 10k items...")
	for i := 0; i < 10000; i++ {
		_, err := e.pool.Exec(e.ctx,
			`INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, workflow_state_id)
			 VALUES ($1, 'u1', 'api', $2, 'done',
			   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))`,
			e.tenantID, fmt.Sprintf("list test content %d searchable keyword", i))
		if err != nil {
			b.Fatalf("insert: %v", err)
		}
	}

	filter := ptrext.Of(feedback.FeedbackFilter{Q: "searchable"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.ListIDsByFilter(e.ctx, e.tenantID, filter, 1000)
		if err != nil {
			b.Fatalf("ListIDsByFilter: %v", err)
		}
	}
}

// BenchmarkKeywordSearch_1k benchmarks keyword search on 1k items.
func BenchmarkKeywordSearch_1k(b *testing.B) {
	e := setupBench(b)

	// Insert 1k items with varied content.
	topics := []string{"login", "payment", "dashboard", "settings", "profile", "notification", "search", "export", "import", "sync"}
	b.Log("Inserting 1k items...")
	for i := 0; i < 1000; i++ {
		topic := topics[i%len(topics)]
		_, err := e.pool.Exec(e.ctx,
			`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, workflow_state_id)
			 VALUES ($1, 'u1', 'api', $2, $3, 'done',
			   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))`,
			e.tenantID,
			fmt.Sprintf("User reported issue with %s functionality item %d", topic, i),
			fmt.Sprintf("%s Issue #%d", topic, i))
		if err != nil {
			b.Fatalf("insert: %v", err)
		}
	}

	params := ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "login",
		Limit:    20,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.KeywordSearch(e.ctx, params)
		if err != nil {
			b.Fatalf("KeywordSearch: %v", err)
		}
	}
}

// BenchmarkKeywordSearch_10k benchmarks keyword search on 10k items.
func BenchmarkKeywordSearch_10k(b *testing.B) {
	e := setupBench(b)

	// Insert 10k items.
	topics := []string{"login", "payment", "dashboard", "settings", "profile", "notification", "search", "export", "import", "sync"}
	b.Log("Inserting 10k items...")
	for i := 0; i < 10000; i++ {
		topic := topics[i%len(topics)]
		_, err := e.pool.Exec(e.ctx,
			`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, workflow_state_id)
			 VALUES ($1, 'u1', 'api', $2, $3, 'done',
			   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))`,
			e.tenantID,
			fmt.Sprintf("User reported issue with %s functionality item %d with detailed description", topic, i),
			fmt.Sprintf("%s Issue #%d", topic, i))
		if err != nil {
			b.Fatalf("insert: %v", err)
		}
	}

	params := ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "login",
		Limit:    20,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.KeywordSearch(e.ctx, params)
		if err != nil {
			b.Fatalf("KeywordSearch: %v", err)
		}
	}
}

// BenchmarkSemanticSearch_1k benchmarks vector search on 1k embedded items.
func BenchmarkSemanticSearch_1k(b *testing.B) {
	e := setupBench(b)

	// Create a base embedding and insert items with varying similarity.
	baseEmb := generateEmbedding()
	b.Log("Inserting 1k embedded items...")
	for i := 0; i < 1000; i++ {
		similarity := 0.3 + 0.7*rand.Float64() // 0.3-1.0 similarity
		emb := generateSimilarEmbedding(baseEmb, similarity)
		embStr := vecToString(emb)
		_, err := e.pool.Exec(e.ctx,
			`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, embedding, embedding_model, workflow_state_id)
			 VALUES ($1, 'u1', 'api', $2, $3, 'done', $4::vector, 'test-model',
			   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))`,
			e.tenantID,
			fmt.Sprintf("Semantic search test content %d", i),
			fmt.Sprintf("Test Item #%d", i),
			embStr)
		if err != nil {
			b.Fatalf("insert: %v", err)
		}
	}

	params := ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       e.tenantID,
		Embedding:      baseEmb,
		EmbeddingModel: "test-model",
		Limit:          20,
		MinSimilarity:  0.5,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.SemanticSearch(e.ctx, params)
		if err != nil {
			b.Fatalf("SemanticSearch: %v", err)
		}
	}
}

// BenchmarkSemanticSearch_10k benchmarks vector search on 10k embedded items.
func BenchmarkSemanticSearch_10k(b *testing.B) {
	e := setupBench(b)

	// Create a base embedding and insert items.
	baseEmb := generateEmbedding()
	b.Log("Inserting 10k embedded items (this may take a minute)...")
	for i := 0; i < 10000; i++ {
		similarity := 0.3 + 0.7*rand.Float64()
		emb := generateSimilarEmbedding(baseEmb, similarity)
		embStr := vecToString(emb)
		_, err := e.pool.Exec(e.ctx,
			`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, embedding, embedding_model, workflow_state_id)
			 VALUES ($1, 'u1', 'api', $2, $3, 'done', $4::vector, 'test-model',
			   (SELECT id FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))`,
			e.tenantID,
			fmt.Sprintf("Semantic search test content %d with more text", i),
			fmt.Sprintf("Test Item #%d", i),
			embStr)
		if err != nil {
			b.Fatalf("insert: %v", err)
		}
	}

	params := ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       e.tenantID,
		Embedding:      baseEmb,
		EmbeddingModel: "test-model",
		Limit:          20,
		MinSimilarity:  0.5,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.SemanticSearch(e.ctx, params)
		if err != nil {
			b.Fatalf("SemanticSearch: %v", err)
		}
	}
}

// BenchmarkFindSimilarFeedback benchmarks finding similar items.
func BenchmarkFindSimilarFeedback(b *testing.B) {
	e := setupBench(b)

	// Insert source with embedding.
	baseEmb := generateEmbedding()
	sourceID := e.insertFeedbackWithEmbedding(b, "source feedback", "Source", "test-model", baseEmb)

	// Insert 1000 potentially similar items.
	b.Log("Inserting 1k similar items...")
	for i := 0; i < 1000; i++ {
		similarity := 0.3 + 0.7*rand.Float64()
		emb := generateSimilarEmbedding(baseEmb, similarity)
		e.insertFeedbackWithEmbedding(b, fmt.Sprintf("similar item %d", i), fmt.Sprintf("Similar #%d", i), "test-model", emb)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.FindSimilarFeedback(e.ctx, e.tenantID, sourceID, 10, 0.5)
		if err != nil {
			b.Fatalf("FindSimilarFeedback: %v", err)
		}
	}
}

// BenchmarkGetEmbeddingStats benchmarks embedding statistics retrieval.
func BenchmarkGetEmbeddingStats(b *testing.B) {
	e := setupBench(b)

	// Insert a mix of embedded and non-embedded items.
	baseEmb := generateEmbedding()
	b.Log("Inserting 1k items (50% with embeddings)...")
	for i := 0; i < 1000; i++ {
		if i%2 == 0 {
			emb := generateSimilarEmbedding(baseEmb, 0.8)
			e.insertFeedbackWithEmbedding(b, fmt.Sprintf("embedded item %d", i), fmt.Sprintf("Embedded #%d", i), "test-model", emb)
		} else {
			e.insertFeedback(b, fmt.Sprintf("non-embedded item %d", i))
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.feedback.GetEmbeddingStats(e.ctx, e.tenantID)
		if err != nil {
			b.Fatalf("GetEmbeddingStats: %v", err)
		}
	}
}
