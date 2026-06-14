//go:build integration

// Package feedbacksearch_test — integration tests for semantic and keyword search (#30).
// Tests the feedback search methods against a real PostgreSQL database with pgvector.
package feedbacksearch_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	feedback *feedback.FeedbackRepo
	tenantID string
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()

	// Create tenant.
	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('search-test','Search Test Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	return env{
		ctx:      ctx,
		pool:     pool,
		feedback: feedback.NewFeedback(pool),
		tenantID: tenantID,
	}
}

func (e env) insertFeedback(t *testing.T, content, title string) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status)
		 VALUES ($1, 'u1', 'api', $2, $3, 'done') RETURNING id`,
		e.tenantID, content, title,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return id
}

func (e env) insertFeedbackWithEmbedding(t *testing.T, content, title, model string, embedding []float32) int64 {
	t.Helper()
	var id int64
	embStr := vecToString(embedding)
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, embedding, embedding_model)
		 VALUES ($1, 'u1', 'api', $2, $3, 'done', $4::vector, $5) RETURNING id`,
		e.tenantID, content, title, embStr, model,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert feedback with embedding: %v", err)
	}
	return id
}

func (e env) insertUrgentFeedback(t *testing.T, content, title string) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, is_urgent)
		 VALUES ($1, 'u1', 'api', $2, $3, 'done', true) RETURNING id`,
		e.tenantID, content, title,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert urgent feedback: %v", err)
	}
	return id
}

// generateEmbedding creates a 256-dim embedding with optional similarity to a base embedding.
func generateEmbedding(base []float32, similarity float64) []float32 {
	emb := make([]float32, 256)
	if base != nil && similarity > 0 {
		// Mix base with random noise based on similarity.
		for i := 0; i < 256; i++ {
			noise := rand.Float32()*2 - 1 // -1 to 1
			emb[i] = float32(similarity)*base[i] + float32(1-similarity)*noise
		}
	} else {
		// Pure random.
		for i := 0; i < 256; i++ {
			emb[i] = rand.Float32()*2 - 1
		}
	}
	// Normalize.
	var norm float32
	for _, v := range emb {
		norm += v * v
	}
	norm = float32(1.0 / float64(norm+1e-8))
	for i := range emb {
		emb[i] *= norm
	}
	return emb
}

// vecToString converts []float32 to pgvector string format "[1.0,2.0,...]".
func vecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	result := "["
	for i, f := range v {
		if i > 0 {
			result += ","
		}
		result += string(rune(48 + int(f*10)%10)) // Simplified; just need valid format.
	}
	// Actually use a proper format.
	result = "["
	for i, f := range v {
		if i > 0 {
			result += ","
		}
		result += formatFloat(f)
	}
	result += "]"
	return result
}

func formatFloat(f float32) string {
	// Simple float formatting without importing strconv to keep test code simple.
	// For proper formatting, use strconv.FormatFloat.
	if f >= 0 {
		return "0." + intToStr(int((f+0.5)*1000)%1000)
	}
	return "-0." + intToStr(int((-f+0.5)*1000)%1000)
}

func intToStr(n int) string {
	if n == 0 {
		return "000"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// TestPG_KeywordSearch tests basic keyword search functionality.
func TestPG_KeywordSearch(t *testing.T) {
	e := setup(t)

	// Insert feedback with searchable content.
	id1 := e.insertFeedback(t, "The login button is broken", "Login Bug")
	id2 := e.insertFeedback(t, "Payment processing fails", "Payment Error")
	_ = e.insertFeedback(t, "Dashboard loads slowly", "Performance Issue")

	// Search for "login".
	results, err := e.feedback.KeywordSearch(e.ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "login",
		Limit:    10,
	}))
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].ID != id1 {
		t.Errorf("result id = %d, want %d", results[0].ID, id1)
	}

	// Search for "payment" in title.
	results, err = e.feedback.KeywordSearch(e.ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "Payment",
		Limit:    10,
	}))
	if err != nil {
		t.Fatalf("KeywordSearch payment: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].ID != id2 {
		t.Errorf("result id = %d, want %d", results[0].ID, id2)
	}
}

// TestPG_KeywordSearchWithFilter tests keyword search with additional filters.
func TestPG_KeywordSearchWithFilter(t *testing.T) {
	e := setup(t)

	_ = e.insertFeedback(t, "urgent login issue", "Login Problem")
	id2 := e.insertUrgentFeedback(t, "critical login failure", "Login Critical")

	// Search with urgent filter.
	results, err := e.feedback.KeywordSearch(e.ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "login",
		Limit:    10,
		Filter:   ptrext.Of(feedback.FeedbackFilter{Urgent: ptrext.Of(true)}),
	}))
	if err != nil {
		t.Fatalf("KeywordSearch with filter: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].ID != id2 {
		t.Errorf("result id = %d, want %d", results[0].ID, id2)
	}
}

// TestPG_KeywordSearchLimit tests that limit is honored.
func TestPG_KeywordSearchLimit(t *testing.T) {
	e := setup(t)

	// Insert 5 items with "test" in content.
	for i := 0; i < 5; i++ {
		e.insertFeedback(t, "test content number "+intToStr(i), "Test "+intToStr(i))
	}

	// Search with limit 2.
	results, err := e.feedback.KeywordSearch(e.ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "test",
		Limit:    2,
	}))
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("results = %d, want 2", len(results))
	}
}

// TestPG_HasEmbedding tests checking for embedding existence.
func TestPG_HasEmbedding(t *testing.T) {
	e := setup(t)

	// Initially no embeddings.
	has, err := e.feedback.HasEmbedding(e.ctx, e.tenantID)
	if err != nil {
		t.Fatalf("HasEmbedding: %v", err)
	}
	if has {
		t.Error("should have no embeddings initially")
	}

	// Add feedback with embedding.
	emb := generateEmbedding(nil, 0)
	_ = e.insertFeedbackWithEmbedding(t, "embedded content", "Embedded", "text-embedding-3-small", emb)

	// Now should have embeddings.
	has, err = e.feedback.HasEmbedding(e.ctx, e.tenantID)
	if err != nil {
		t.Fatalf("HasEmbedding after insert: %v", err)
	}
	if !has {
		t.Error("should have embeddings after insert")
	}
}

// TestPG_GetEmbeddingStats tests embedding statistics.
func TestPG_GetEmbeddingStats(t *testing.T) {
	e := setup(t)

	// Add some feedback, some with embeddings.
	e.insertFeedback(t, "no embedding 1", "No Emb 1")
	e.insertFeedback(t, "no embedding 2", "No Emb 2")
	emb := generateEmbedding(nil, 0)
	e.insertFeedbackWithEmbedding(t, "with embedding", "With Emb", "text-embedding-3-small", emb)

	stats, err := e.feedback.GetEmbeddingStats(e.ctx, e.tenantID)
	if err != nil {
		t.Fatalf("GetEmbeddingStats: %v", err)
	}

	if stats.TotalFeedback != 3 {
		t.Errorf("TotalFeedback = %d, want 3", stats.TotalFeedback)
	}
	if stats.EmbeddedCount != 1 {
		t.Errorf("EmbeddedCount = %d, want 1", stats.EmbeddedCount)
	}
	if stats.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("EmbeddingModel = %q, want %q", stats.EmbeddingModel, "text-embedding-3-small")
	}
	// Roughly 33.33%.
	if stats.EmbeddingPercent < 30 || stats.EmbeddingPercent > 40 {
		t.Errorf("EmbeddingPercent = %f, want ~33", stats.EmbeddingPercent)
	}
}

// TestPG_SemanticSearch tests vector similarity search.
func TestPG_SemanticSearch(t *testing.T) {
	e := setup(t)

	// Create a base embedding for "login issues".
	baseEmb := make([]float32, 256)
	for i := range baseEmb {
		baseEmb[i] = float32(i) / 256.0
	}
	// Normalize.
	var norm float32
	for _, v := range baseEmb {
		norm += v * v
	}
	norm = 1.0 / norm
	for i := range baseEmb {
		baseEmb[i] = baseEmb[i] * norm
	}

	// Insert feedback with embeddings at varying similarity.
	// High similarity to query.
	similar := make([]float32, 256)
	copy(similar, baseEmb)
	for i := range similar {
		similar[i] += 0.01 * (rand.Float32() - 0.5)
	}
	id1 := e.insertFeedbackWithEmbedding(t, "login button broken", "Login Bug", "test-model", similar)

	// Medium similarity - different direction.
	medium := make([]float32, 256)
	for i := range medium {
		medium[i] = baseEmb[i]*0.5 + 0.5*(rand.Float32()-0.5)
	}
	_ = e.insertFeedbackWithEmbedding(t, "payment fails", "Payment Error", "test-model", medium)

	// Low similarity - mostly random.
	low := make([]float32, 256)
	for i := range low {
		low[i] = rand.Float32() - 0.5
	}
	_ = e.insertFeedbackWithEmbedding(t, "dashboard slow", "Performance", "test-model", low)

	// Search with the base embedding.
	hits, err := e.feedback.SemanticSearch(e.ctx, ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       e.tenantID,
		Embedding:      baseEmb,
		EmbeddingModel: "test-model",
		Limit:          10,
		MinSimilarity:  0.1, // Low threshold to get all results.
	}))
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}

	if len(hits) == 0 {
		t.Fatal("expected some hits")
	}

	// The first hit should be the most similar (id1).
	if hits[0].Feedback.ID != id1 {
		t.Errorf("first hit = %d, want %d (most similar)", hits[0].Feedback.ID, id1)
	}

	// Results should be ordered by similarity descending.
	for i := 1; i < len(hits); i++ {
		if hits[i].Similarity > hits[i-1].Similarity {
			t.Errorf("results not ordered by similarity: hits[%d]=%f > hits[%d]=%f",
				i, hits[i].Similarity, i-1, hits[i-1].Similarity)
		}
	}
}

// TestPG_SemanticSearchMinSimilarity tests filtering by minimum similarity.
func TestPG_SemanticSearchMinSimilarity(t *testing.T) {
	e := setup(t)

	// Create embeddings with known similarities.
	baseEmb := make([]float32, 256)
	for i := range baseEmb {
		baseEmb[i] = 1.0 / 16.0 // Unit vector in 256-D.
	}

	// Insert identical embedding (similarity ~1.0).
	e.insertFeedbackWithEmbedding(t, "exact match", "Exact", "test-model", baseEmb)

	// Insert very different embedding.
	diffEmb := make([]float32, 256)
	for i := range diffEmb {
		diffEmb[i] = -baseEmb[i] // Opposite direction.
	}
	e.insertFeedbackWithEmbedding(t, "opposite direction", "Opposite", "test-model", diffEmb)

	// Search with high min similarity - should only get exact match.
	hits, err := e.feedback.SemanticSearch(e.ctx, ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       e.tenantID,
		Embedding:      baseEmb,
		EmbeddingModel: "test-model",
		Limit:          10,
		MinSimilarity:  0.9, // High threshold.
	}))
	if err != nil {
		t.Fatalf("SemanticSearch high min: %v", err)
	}

	if len(hits) != 1 {
		t.Errorf("hits = %d, want 1 (only exact match)", len(hits))
	}
}

// TestPG_SemanticSearchModelMismatch tests that model must match.
func TestPG_SemanticSearchModelMismatch(t *testing.T) {
	e := setup(t)

	emb := make([]float32, 256)
	for i := range emb {
		emb[i] = 0.1
	}

	// Insert with one model.
	e.insertFeedbackWithEmbedding(t, "content", "Title", "model-a", emb)

	// Search with different model - should get no results.
	hits, err := e.feedback.SemanticSearch(e.ctx, ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       e.tenantID,
		Embedding:      emb,
		EmbeddingModel: "model-b", // Different model!
		Limit:          10,
		MinSimilarity:  0.1,
	}))
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}

	if len(hits) != 0 {
		t.Errorf("hits = %d, want 0 (model mismatch)", len(hits))
	}
}

// TestPG_SemanticSearchWithFilter tests vector search with additional filters.
func TestPG_SemanticSearchWithFilter(t *testing.T) {
	e := setup(t)

	emb := make([]float32, 256)
	for i := range emb {
		emb[i] = 0.1
	}

	// Insert normal and urgent feedback with same embedding.
	e.insertFeedbackWithEmbedding(t, "normal issue", "Normal", "test-model", emb)

	// Insert urgent feedback.
	var id2 int64
	err := e.pool.QueryRow(e.ctx,
		`INSERT INTO user_feedback (tenant_id, user_id, source, content, enriched_title, enrichment_status, embedding, embedding_model, is_urgent)
		 VALUES ($1, 'u1', 'api', 'urgent issue', 'Urgent', 'done', $2::vector, 'test-model', true) RETURNING id`,
		e.tenantID, vecToString(emb),
	).Scan(&id2) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert urgent: %v", err)
	}

	// Search with urgent filter.
	hits, err := e.feedback.SemanticSearch(e.ctx, ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       e.tenantID,
		Embedding:      emb,
		EmbeddingModel: "test-model",
		Limit:          10,
		MinSimilarity:  0.1,
		Filter:         ptrext.Of(feedback.FeedbackFilter{Urgent: ptrext.Of(true)}),
	}))
	if err != nil {
		t.Fatalf("SemanticSearch with filter: %v", err)
	}

	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Feedback.ID != id2 {
		t.Errorf("hit id = %d, want %d", hits[0].Feedback.ID, id2)
	}
}

// TestPG_GetFeedbackEmbedding tests retrieving a single feedback's embedding.
func TestPG_GetFeedbackEmbedding(t *testing.T) {
	e := setup(t)

	// Insert feedback without embedding.
	id1 := e.insertFeedback(t, "no embedding", "No Emb")

	emb, _, err := e.feedback.GetFeedbackEmbedding(e.ctx, e.tenantID, id1)
	if err != nil {
		t.Fatalf("GetFeedbackEmbedding: %v", err)
	}
	if emb != nil {
		t.Errorf("expected nil embedding, got %d dims", len(emb))
	}

	// Insert feedback with embedding.
	expectedEmb := make([]float32, 256)
	for i := range expectedEmb {
		expectedEmb[i] = float32(i) / 256.0
	}
	id2 := e.insertFeedbackWithEmbedding(t, "with embedding", "With Emb", "test-model", expectedEmb)

	emb, model, err := e.feedback.GetFeedbackEmbedding(e.ctx, e.tenantID, id2)
	if err != nil {
		t.Fatalf("GetFeedbackEmbedding with emb: %v", err)
	}
	if len(emb) != 256 {
		t.Errorf("embedding dims = %d, want 256", len(emb))
	}
	if model != "test-model" {
		t.Errorf("model = %q, want %q", model, "test-model")
	}
}

// TestPG_FindSimilarFeedback tests finding similar feedback items.
func TestPG_FindSimilarFeedback(t *testing.T) {
	e := setup(t)

	// Create embeddings.
	baseEmb := make([]float32, 256)
	for i := range baseEmb {
		baseEmb[i] = 1.0 / 16.0
	}

	// Insert source feedback.
	sourceID := e.insertFeedbackWithEmbedding(t, "source feedback", "Source", "test-model", baseEmb)

	// Insert similar feedback (same embedding).
	similarID := e.insertFeedbackWithEmbedding(t, "similar feedback", "Similar", "test-model", baseEmb)

	// Insert different feedback.
	diffEmb := make([]float32, 256)
	for i := range diffEmb {
		diffEmb[i] = -baseEmb[i]
	}
	_ = e.insertFeedbackWithEmbedding(t, "different feedback", "Different", "test-model", diffEmb)

	// Find similar to source.
	hits, err := e.feedback.FindSimilarFeedback(e.ctx, e.tenantID, sourceID, 5, 0.5)
	if err != nil {
		t.Fatalf("FindSimilarFeedback: %v", err)
	}

	// Should find the similar one but not the source itself.
	found := false
	for _, h := range hits {
		if h.Feedback.ID == sourceID {
			t.Error("should not include source in similar results")
		}
		if h.Feedback.ID == similarID {
			found = true
		}
	}
	if !found {
		t.Error("should find similar feedback")
	}
}

// TestPG_SearchExcludesDeleted tests that deleted items are excluded.
func TestPG_SearchExcludesDeleted(t *testing.T) {
	e := setup(t)

	// Insert and soft-delete a feedback.
	id := e.insertFeedback(t, "deleted searchable content", "Deleted Item")
	_, err := e.pool.Exec(e.ctx, `UPDATE user_feedback SET deleted_at = NOW() WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// Insert a normal feedback.
	id2 := e.insertFeedback(t, "normal searchable content", "Normal Item")

	// Search should only find the non-deleted one.
	results, err := e.feedback.KeywordSearch(e.ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: e.tenantID,
		Query:    "searchable",
		Limit:    10,
	}))
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("results = %d, want 1", len(results))
	}
	if len(results) > 0 && results[0].ID != id2 {
		t.Errorf("result id = %d, want %d", results[0].ID, id2)
	}
}

// Note: Go 1.20+ automatically seeds the global rand, so no init needed.
