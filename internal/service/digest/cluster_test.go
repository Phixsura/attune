// ptrext:file-allow test fixtures use &bool for called flags
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// fakeEmbeddingReader returns pre-generated embeddings for testing.
type fakeEmbeddingReader struct {
	embeddings []embedding.EmbeddedFeedback
	err        error
}

func (f fakeEmbeddingReader) EmbeddingsInWindow(
	_ context.Context, _ string, _, _ time.Time, _ int,
) ([]embedding.EmbeddedFeedback, error) {
	return f.embeddings, f.err
}

// fakeLLMForCluster returns a fixed cluster name.
type fakeLLMForCluster struct {
	title string
}

func (f fakeLLMForCluster) Complete(_ context.Context, _ llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	resp := struct {
		Title string `json:"title"`
	}{Title: f.title}
	b, _ := json.Marshal(resp)
	return llmclient.CompletionResponse{Text: string(b)}, nil
}

func (f fakeLLMForCluster) Close() error { return nil }

// generateClusteredEmbeddings creates n embeddings in numClusters distinct clusters.
func generateClusteredEmbeddings(n, numClusters, dim int) []embedding.EmbeddedFeedback {
	rng := rand.New(rand.NewSource(42))
	perCluster := n / numClusters
	embeddings := make([]embedding.EmbeddedFeedback, 0, n)

	for c := 0; c < numClusters; c++ {
		base := make([]float32, dim)
		base[c*50] = 1.0 // distinct direction per cluster

		for i := 0; i < perCluster; i++ {
			vec := make([]float32, dim)
			copy(vec, base)
			for d := 0; d < dim; d++ {
				vec[d] += float32(rng.NormFloat64() * 0.05)
			}
			embeddings = append(embeddings, embedding.EmbeddedFeedback{
				ID:        int64(c*perCluster + i + 1),
				Title:     "Feedback item",
				Rationale: "Some rationale",
				Embedding: vec,
			})
		}
	}
	return embeddings
}

func TestClusterNamer_HappyPath(t *testing.T) {
	t.Parallel()

	// 30 embeddings in 3 clusters
	embeddings := generateClusteredEmbeddings(30, 3, 384)

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		fakeLLMForCluster{title: "Test Theme"},
		fakeNamer{}, // fallback naive namer
	)

	themes, err := namer.Name(context.Background(), "tenant-1", "", nil, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(themes) < 2 {
		t.Errorf("expected at least 2 themes, got %d", len(themes))
	}

	// Each theme should have examples
	for i, theme := range themes {
		if theme.Count == 0 {
			t.Errorf("theme %d has 0 count", i)
		}
		if len(theme.ExampleIDs) == 0 {
			t.Errorf("theme %d has no examples", i)
		}
		t.Logf("Theme %d: %q, count=%d, examples=%v", i, theme.Title, theme.Count, theme.ExampleIDs)
	}
}

func TestClusterNamer_FallsBackToNaive(t *testing.T) {
	t.Parallel()

	// Too few embeddings to cluster
	embeddings := []embedding.EmbeddedFeedback{
		{ID: 1, Title: "A", Embedding: make([]float32, 384)},
		{ID: 2, Title: "B", Embedding: make([]float32, 384)},
	}

	naiveCalled := false
	naive := fakeNamer{
		themes: []Theme{{Title: "Naive Theme", Count: 2}},
		called: &naiveCalled,
	}

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		fakeLLMForCluster{},
		naive,
	)

	themes, err := namer.Name(context.Background(), "tenant-1", "", nil, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !naiveCalled {
		t.Error("expected naive fallback to be called")
	}
	if len(themes) != 1 || themes[0].Title != "Naive Theme" {
		t.Errorf("unexpected themes: %+v", themes)
	}
}

func TestClusterNamer_ZeroClusters(t *testing.T) {
	t.Parallel()

	// All identical embeddings -> no distinct clusters
	embeddings := make([]embedding.EmbeddedFeedback, 20)
	for i := range embeddings {
		embeddings[i] = embedding.EmbeddedFeedback{
			ID:        int64(i + 1),
			Title:     "Same",
			Embedding: make([]float32, 384), // all zeros
		}
	}

	naiveCalled := false
	naive := fakeNamer{
		themes: []Theme{{Title: "Fallback", Count: 20}},
		called: &naiveCalled,
	}

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		fakeLLMForCluster{},
		naive,
	)

	_, err := namer.Name(context.Background(), "tenant-1", "", nil, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !naiveCalled {
		t.Error("expected naive fallback when HDBSCAN finds 0 clusters")
	}
}

func TestClusterNamer_LowCoverageFallback(t *testing.T) {
	t.Parallel()

	// 10 embeddings but 20 rows -> 50% coverage < 80% threshold
	embeddings := generateClusteredEmbeddings(10, 2, 384)
	rows := make([]feedback.DigestFeedbackRow, 20)
	for i := range rows {
		rows[i] = feedback.DigestFeedbackRow{ID: int64(i + 1), Title: "Item"}
	}

	naiveCalled := false
	naive := fakeNamer{
		themes: []Theme{{Title: "Naive Theme", Count: 20}},
		called: &naiveCalled,
	}

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		fakeLLMForCluster{},
		naive,
	)

	themes, err := namer.Name(context.Background(), "tenant-1", "", rows, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !naiveCalled {
		t.Error("expected naive fallback due to low embedding coverage")
	}
	if len(themes) != 1 || themes[0].Title != "Naive Theme" {
		t.Errorf("unexpected themes: %+v", themes)
	}
}

func TestClusterNamer_100Items(t *testing.T) {
	t.Parallel()

	// 100 embeddings in 5 clusters (proposal: "100-item digest should show 5-10 themes")
	embeddings := generateClusteredEmbeddings(100, 5, 384)

	namer := newClusterNamer(
		fakeEmbeddingReader{embeddings: embeddings},
		fakeLLMForCluster{title: "Cluster Theme"},
		fakeNamer{},
	)

	themes, err := namer.Name(context.Background(), "tenant-1", "", nil, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(themes) < 3 || len(themes) > 10 {
		t.Errorf("expected 3-10 themes for 100 items, got %d", len(themes))
	}

	totalCount := 0
	for i, th := range themes {
		totalCount += th.Count
		t.Logf("Theme %d: count=%d, examples=%d", i, th.Count, len(th.ExampleIDs))
	}

	// Most items should be clustered (allow some noise)
	if totalCount < 80 {
		t.Errorf("expected at least 80 items clustered, got %d", totalCount)
	}
}

func BenchmarkClusterNamer_vs_Naive(b *testing.B) {
	embeddings := generateClusteredEmbeddings(100, 5, 384)
	rows := make([]feedback.DigestFeedbackRow, 100)
	for i := range rows {
		rows[i] = feedback.DigestFeedbackRow{ID: int64(i + 1), Title: embeddings[i].Title}
	}

	b.Run("Cluster", func(b *testing.B) {
		namer := newClusterNamer(
			fakeEmbeddingReader{embeddings: embeddings},
			fakeLLMForCluster{title: "Theme"},
			fakeNamer{},
		)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = namer.Name(context.Background(), "tenant", "", nil, time.Time{}, time.Time{})
		}
	})

	b.Run("Naive", func(b *testing.B) {
		namer := fakeNamer{themes: []Theme{{Title: "A", Count: 50}, {Title: "B", Count: 50}}}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = namer.Name(context.Background(), "tenant", "", rows, time.Time{}, time.Time{})
		}
	})
}

func TestSelectClosestToCentroid(t *testing.T) {
	t.Parallel()

	members := []embedding.EmbeddedFeedback{
		{ID: 1, Embedding: []float32{1, 0, 0}},
		{ID: 2, Embedding: []float32{0.9, 0.1, 0}},
		{ID: 3, Embedding: []float32{0.5, 0.5, 0}}, // furthest
		{ID: 4, Embedding: []float32{0.95, 0.05, 0}},
	}
	centroid := []float32{1, 0, 0}

	result := selectClosestToCentroid(members, centroid, 2)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// ID 1 should be first (exact match)
	if result[0].ID != 1 {
		t.Errorf("expected ID 1 first, got %d", result[0].ID)
	}
}
