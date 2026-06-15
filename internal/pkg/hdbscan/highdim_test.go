package hdbscan

import (
	"math/rand"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestHighDimensional_WithPCA tests that PCA reduction fixes the curse of dimensionality.
func TestHighDimensional_WithPCA(t *testing.T) {
	t.Parallel()

	// Simulate 3 clusters of 10 points each in 384-dim space
	dim := 384
	pointsPerCluster := 10
	numClusters := 3

	data := make([][]float32, numClusters*pointsPerCluster)

	// Create cluster centers (orthogonal directions)
	centers := make([][]float32, numClusters)
	for c := 0; c < numClusters; c++ {
		centers[c] = make([]float32, dim)
		centers[c][c*100] = 1.0
	}

	// Generate points around each center
	rng := rand.New(rand.NewSource(42))
	for c := 0; c < numClusters; c++ {
		for p := 0; p < pointsPerCluster; p++ {
			idx := c*pointsPerCluster + p
			data[idx] = make([]float32, dim)
			for d := 0; d < dim; d++ {
				noise := float32(rng.NormFloat64() * 0.05)
				data[idx][d] = centers[c][d] + noise
			}
		}
	}

	// Compute distances to verify separation
	withinDist := cosineDistance(data[0], data[1])
	betweenDist := cosineDistance(data[0], data[pointsPerCluster])

	t.Logf("Within-cluster cosine distance: %.4f", withinDist)
	t.Logf("Between-cluster cosine distance: %.4f", betweenDist)
	t.Logf("Separation ratio: %.2f", betweenDist/withinDist)

	// Test without PCA (expected to fail)
	cNoPCA := ptrext.Of(Clusterer{MinClusterSize: 5, MinSamples: 3})
	resultNoPCA := cNoPCA.Cluster(data)
	t.Logf("Without PCA: %d clusters (expected to fail)", resultNoPCA.ClusterCount)

	// Test with PCA (expected to work)
	cWithPCA := ptrext.Of(Clusterer{MinClusterSize: 5, MinSamples: 3, ReduceDims: 10})
	result := cWithPCA.Cluster(data)
	t.Logf("With PCA(10): %d clusters (expected: %d)", result.ClusterCount, numClusters)
	t.Logf("Labels: %v", result.Labels)

	// Verify PCA version works
	if result.ClusterCount < 2 {
		t.Errorf("With PCA: expected at least 2 clusters, got %d", result.ClusterCount)
	}

	// Verify cluster assignments are roughly correct
	if result.ClusterCount >= 2 {
		for c := 0; c < numClusters; c++ {
			baseLabel := result.Labels[c*pointsPerCluster]
			sameCluster := 0
			for p := 0; p < pointsPerCluster; p++ {
				idx := c*pointsPerCluster + p
				if result.Labels[idx] == baseLabel {
					sameCluster++
				}
			}
			if sameCluster < 7 {
				t.Errorf("Cluster %d: only %d/10 points have same label", c, sameCluster)
			}
		}
	}
}

// TestHighDimensional_RealisticFeedback simulates real feedback embeddings.
func TestHighDimensional_RealisticFeedback(t *testing.T) {
	t.Parallel()

	dim := 384

	// 50 feedback items in 3 rough topics
	data := make([][]float32, 50)

	rng := rand.New(rand.NewSource(123))

	// Topic 1: "payment issues" - 15 items
	for i := 0; i < 15; i++ {
		data[i] = make([]float32, dim)
		for d := 0; d < 50; d++ {
			data[i][d] = float32(0.2 + rng.NormFloat64()*0.1)
		}
		for d := 50; d < dim; d++ {
			data[i][d] = float32(rng.NormFloat64() * 0.02)
		}
	}

	// Topic 2: "login problems" - 20 items
	for i := 15; i < 35; i++ {
		data[i] = make([]float32, dim)
		for d := 100; d < 150; d++ {
			data[i][d] = float32(0.2 + rng.NormFloat64()*0.1)
		}
		for d := 0; d < dim; d++ {
			if d < 100 || d >= 150 {
				data[i][d] = float32(rng.NormFloat64() * 0.02)
			}
		}
	}

	// Topic 3: "UI bugs" - 10 items
	for i := 35; i < 45; i++ {
		data[i] = make([]float32, dim)
		for d := 200; d < 250; d++ {
			data[i][d] = float32(0.2 + rng.NormFloat64()*0.1)
		}
		for d := 0; d < dim; d++ {
			if d < 200 || d >= 250 {
				data[i][d] = float32(rng.NormFloat64() * 0.02)
			}
		}
	}

	// 5 noise items (random)
	for i := 45; i < 50; i++ {
		data[i] = make([]float32, dim)
		for d := 0; d < dim; d++ {
			data[i][d] = float32(rng.NormFloat64() * 0.1)
		}
	}

	// With PCA reduction
	c := ptrext.Of(Clusterer{MinClusterSize: 5, MinSamples: 3, ReduceDims: 20})
	result := c.Cluster(data)

	t.Logf("Clusters found: %d (expected: ~3)", result.ClusterCount)
	t.Logf("Labels: %v", result.Labels)

	// Count noise points
	noiseCount := 0
	for _, l := range result.Labels {
		if l == -1 {
			noiseCount++
		}
	}
	t.Logf("Noise points: %d (expected: ~5)", noiseCount)

	if result.ClusterCount < 2 {
		t.Errorf("expected at least 2 clusters, got %d", result.ClusterCount)
	}
}

// TestPCA_Basic verifies PCA dimensionality reduction works correctly.
func TestPCA_Basic(t *testing.T) {
	t.Parallel()

	// Create data with clear structure in first 2 dimensions
	data := [][]float32{
		{1, 0, 0.1, 0.1, 0.1},
		{0.9, 0.1, 0.1, 0.1, 0.1},
		{0, 1, 0.1, 0.1, 0.1},
		{0.1, 0.9, 0.1, 0.1, 0.1},
	}

	pca := ptrext.Of(PCA{Components: 2, MaxIter: 100})
	reduced := pca.Reduce(data)

	if len(reduced) != 4 {
		t.Errorf("expected 4 rows, got %d", len(reduced))
	}
	if len(reduced[0]) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(reduced[0]))
	}

	// Points 0,1 should be close; points 2,3 should be close
	dist01 := cosineDistance(reduced[0], reduced[1])
	dist23 := cosineDistance(reduced[2], reduced[3])
	dist02 := cosineDistance(reduced[0], reduced[2])

	t.Logf("Distance 0-1 (same group): %.4f", dist01)
	t.Logf("Distance 2-3 (same group): %.4f", dist23)
	t.Logf("Distance 0-2 (diff group): %.4f", dist02)

	if dist02 < dist01 {
		t.Error("Between-group distance should be larger than within-group")
	}
}
