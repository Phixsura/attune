// SPDX-License-Identifier: Apache-2.0

package hdbscan

import (
	"math"
	"math/rand"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestClusterer_EmptyData(t *testing.T) {
	t.Parallel()

	c := ptrext.Of(Clusterer{MinClusterSize: 3})
	result := c.Cluster(nil)

	if result.ClusterCount != 0 {
		t.Errorf("expected 0 clusters, got %d", result.ClusterCount)
	}
}

func TestClusterer_SinglePoint(t *testing.T) {
	t.Parallel()

	c := ptrext.Of(Clusterer{MinClusterSize: 1})
	data := [][]float32{{1.0, 0.0, 0.0}}
	result := c.Cluster(data)

	if len(result.Labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(result.Labels))
	}
}

func TestClusterer_TwoClearClusters(t *testing.T) {
	t.Parallel()

	// Two groups of points that are clearly separated
	// Group 1: points around [1, 0, 0]
	// Group 2: points around [0, 1, 0]
	data := [][]float32{
		// Cluster 1: similar to [1, 0, 0]
		{1.0, 0.0, 0.0},
		{0.95, 0.05, 0.0},
		{0.9, 0.1, 0.0},
		{0.98, 0.02, 0.0},
		// Cluster 2: similar to [0, 1, 0]
		{0.0, 1.0, 0.0},
		{0.05, 0.95, 0.0},
		{0.1, 0.9, 0.0},
		{0.02, 0.98, 0.0},
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 3})
	result := c.Cluster(data)

	if result.ClusterCount != 2 {
		t.Errorf("expected 2 clusters, got %d", result.ClusterCount)
	}

	// Check that first 4 points are in one cluster
	cluster0 := result.Labels[0]
	for i := 1; i < 4; i++ {
		if result.Labels[i] != cluster0 {
			t.Errorf("expected point %d in same cluster as point 0", i)
		}
	}

	// Check that last 4 points are in another cluster
	cluster1 := result.Labels[4]
	for i := 5; i < 8; i++ {
		if result.Labels[i] != cluster1 {
			t.Errorf("expected point %d in same cluster as point 4", i)
		}
	}

	// Two clusters should be different
	if cluster0 == cluster1 {
		t.Error("expected two different clusters")
	}
}

func TestClusterer_ThreeClusters(t *testing.T) {
	t.Parallel()

	// Three orthogonal directions with enough points per cluster
	// MinClusterSize=3 and MinSamples=3 requires at least 4 points per cluster
	// so the 3rd nearest neighbor stays within the cluster
	data := [][]float32{
		// Cluster 1: [1, 0, 0] direction
		{1.0, 0.0, 0.0},
		{0.98, 0.02, 0.0},
		{0.95, 0.05, 0.0},
		{0.92, 0.08, 0.0},
		// Cluster 2: [0, 1, 0] direction
		{0.0, 1.0, 0.0},
		{0.02, 0.98, 0.0},
		{0.05, 0.95, 0.0},
		{0.08, 0.92, 0.0},
		// Cluster 3: [0, 0, 1] direction
		{0.0, 0.0, 1.0},
		{0.0, 0.02, 0.98},
		{0.0, 0.05, 0.95},
		{0.0, 0.08, 0.92},
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 3})
	result := c.Cluster(data)

	if result.ClusterCount != 3 {
		t.Errorf("expected 3 clusters, got %d", result.ClusterCount)
	}
}

func TestClusterer_NoisePoints(t *testing.T) {
	t.Parallel()

	// With allow_single_cluster=false (default) and min_cluster_size=4,
	// if the only cluster found is the root, all points become noise.
	// This matches Python scikit-learn HDBSCAN behavior.
	data := [][]float32{
		// Dense cluster (5 points)
		{1.0, 0.0, 0.0},
		{0.95, 0.05, 0.0},
		{0.9, 0.1, 0.0},
		{0.98, 0.02, 0.0},
		{0.92, 0.08, 0.0},
		// Outliers (2 points)
		{0.0, 1.0, 0.0},
		{0.0, 0.0, 1.0},
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 4})
	result := c.Cluster(data)

	// With min_cluster_size=4 and allow_single_cluster=false,
	// only root cluster is formed -> all points become noise (-1).
	// This is expected: Python HDBSCAN also returns all -1 for this case.
	t.Logf("Labels: %v", result.Labels)
	t.Logf("ClusterCount: %d", result.ClusterCount)

	// Verify this matches Python's behavior
	if result.ClusterCount != 0 {
		t.Errorf("expected 0 clusters (all noise), got %d", result.ClusterCount)
	}
	for i, l := range result.Labels {
		if l != -1 {
			t.Errorf("point %d: expected -1 (noise), got %d", i, l)
		}
	}
}

func TestCosineDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a, b     []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 1.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: 2.0,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0.9, 0.1, 0},
			expected: 1.0 - (0.9 / (1.0 * math.Sqrt(0.82))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineDistance(tt.a, tt.b)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("cosineDistance(%v, %v) = %f, expected %f", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestClusterer_RealisticEmbeddings(t *testing.T) {
	t.Parallel()

	// Simulate realistic 384-dim embeddings with clear clusters
	// We'll create 3 clusters of 10 points each
	dim := 384
	data := make([][]float32, 30)

	// Cluster 1: base vector with small perturbations
	base1 := make([]float32, dim)
	base1[0] = 1.0
	for i := 0; i < 10; i++ {
		data[i] = make([]float32, dim)
		copy(data[i], base1)
		// Add small noise
		data[i][i%dim] += 0.05
	}

	// Cluster 2: different base
	base2 := make([]float32, dim)
	base2[100] = 1.0
	for i := 10; i < 20; i++ {
		data[i] = make([]float32, dim)
		copy(data[i], base2)
		data[i][(i-10)%dim] += 0.05
	}

	// Cluster 3: another different base
	base3 := make([]float32, dim)
	base3[200] = 1.0
	for i := 20; i < 30; i++ {
		data[i] = make([]float32, dim)
		copy(data[i], base3)
		data[i][(i-20)%dim] += 0.05
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 5})
	result := c.Cluster(data)

	t.Logf("Found %d clusters", result.ClusterCount)
	t.Logf("Labels: %v", result.Labels)

	if result.ClusterCount < 2 {
		t.Errorf("expected at least 2 clusters for realistic embeddings, got %d", result.ClusterCount)
	}
}

func TestClusterer_AllIdenticalPoints(t *testing.T) {
	t.Parallel()

	// All embeddings at exact same location - should handle gracefully
	data := make([][]float32, 10)
	for i := range data {
		data[i] = []float32{1.0, 0.0, 0.0}
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 3})
	result := c.Cluster(data)

	// All identical points: MST has zero edges, should produce 1 cluster or all noise
	t.Logf("Labels: %v, ClusterCount: %d", result.Labels, result.ClusterCount)
	// Should not panic
}

func TestClusterer_TwoPoints(t *testing.T) {
	t.Parallel()

	data := [][]float32{{1, 0, 0}, {0, 1, 0}}
	c := ptrext.Of(Clusterer{MinClusterSize: 2})
	result := c.Cluster(data)

	// Two points with MinClusterSize=2: may form 1 cluster or all noise
	t.Logf("Labels: %v, ClusterCount: %d", result.Labels, result.ClusterCount)
}

func TestClusterer_MinClusterSizeGreaterThanN(t *testing.T) {
	t.Parallel()

	data := [][]float32{{1, 0}, {0, 1}, {0.5, 0.5}}
	c := ptrext.Of(Clusterer{MinClusterSize: 10})
	result := c.Cluster(data)

	// All should be noise since MinClusterSize > n
	for i, label := range result.Labels {
		if label != -1 {
			t.Errorf("point %d: expected noise (-1), got %d", i, label)
		}
	}
}

func TestClusterer_HighDimensions1536(t *testing.T) {
	t.Parallel()

	// OpenAI ada-002 embedding dimension
	dim := 1536
	pointsPerCluster := 10
	numClusters := 3

	data := make([][]float32, numClusters*pointsPerCluster)
	rng := rand.New(rand.NewSource(42))

	for c := 0; c < numClusters; c++ {
		for p := 0; p < pointsPerCluster; p++ {
			idx := c*pointsPerCluster + p
			data[idx] = make([]float32, dim)
			data[idx][c*400] = 1.0 // distinct direction per cluster
			for d := 0; d < dim; d++ {
				data[idx][d] += float32(rng.NormFloat64() * 0.05)
			}
		}
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 5, ReduceDims: 20})
	result := c.Cluster(data)

	t.Logf("1536-dim: %d clusters found", result.ClusterCount)
	if result.ClusterCount < 2 {
		t.Errorf("expected at least 2 clusters for 1536-dim data with PCA, got %d", result.ClusterCount)
	}
}

func TestCosineDistance_ZeroVector(t *testing.T) {
	t.Parallel()

	zero := []float32{0, 0, 0}
	unit := []float32{1, 0, 0}

	got := cosineDistance(zero, unit)
	if got != 1.0 {
		t.Errorf("expected 1.0 for zero vector, got %f", got)
	}
}

func TestCosineDistance_MismatchedLengths(t *testing.T) {
	t.Parallel()

	a := []float32{1, 0}
	b := []float32{1, 0, 0}

	got := cosineDistance(a, b)
	if got != 1.0 {
		t.Errorf("expected 1.0 for mismatched lengths, got %f", got)
	}
}

func BenchmarkClusterer_100Points(b *testing.B) {
	// Generate 100 random-ish points
	dim := 384
	data := make([][]float32, 100)
	for i := range data {
		data[i] = make([]float32, dim)
		data[i][i%dim] = 1.0
		data[i][(i*7)%dim] = 0.5
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 5})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Cluster(data)
	}
}

func BenchmarkClusterer_500Points(b *testing.B) {
	dim := 384
	data := make([][]float32, 500)
	for i := range data {
		data[i] = make([]float32, dim)
		data[i][i%dim] = 1.0
		data[i][(i*7)%dim] = 0.5
	}

	c := ptrext.Of(Clusterer{MinClusterSize: 5})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Cluster(data)
	}
}
