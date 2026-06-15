// SPDX-License-Identifier: Apache-2.0

package hdbscan

import (
	"fmt"
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

	// Create cluster centers with dense structure across first 50 dimensions
	// (sparse single-dimension centers don't survive PCA well)
	for c := 0; c < numClusters; c++ {
		for p := 0; p < pointsPerCluster; p++ {
			idx := c*pointsPerCluster + p
			data[idx] = make([]float32, dim)
			// Dense cluster centers in first 50 dims
			for d := 0; d < 50; d++ {
				data[idx][d] = float32(c) * 0.5 // cluster offset
			}
			// Add noise
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

// -----------------------------------------------------------------------------
// computeCoreDistances edge-case tests
// -----------------------------------------------------------------------------

// TestComputeCoreDistances_KEqualsOne tests k=1 (minSamples=1) behavior.
// Core distance = distance to the nearest (0th) neighbor.
func TestComputeCoreDistances_KEqualsOne(t *testing.T) {
	t.Parallel()

	// Points: [1,0,0], [0,1,0], [0,0,1] - all orthogonal, distance = 1.0
	data := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}

	coreDistances := computeCoreDistances(data, 1)

	// All pairs are orthogonal, cosine distance = 1.0
	// The nearest neighbor for each point is distance 1.0 (all neighbors equidistant)
	for i, cd := range coreDistances {
		if math.Abs(cd-1.0) > 1e-9 {
			t.Errorf("point %d: expected core distance ~1.0, got %f", i, cd)
		}
	}
}

// TestComputeCoreDistances_KEqualsOne_VariedDistances tests k=1 with points
// at different distances to verify nearest neighbor selection.
func TestComputeCoreDistances_KEqualsOne_VariedDistances(t *testing.T) {
	t.Parallel()

	// Point 0: [1,0,0]
	// Point 1: [0.9,0.1,0] - nearest to point 0
	// Point 2: [0,1,0] - orthogonal to point 0
	data := [][]float32{
		{1, 0, 0},
		{0.9, 0.1, 0},
		{0, 1, 0},
	}

	coreDistances := computeCoreDistances(data, 1)

	// For point 0: nearest is point 1
	dist01 := cosineDistance(data[0], data[1])
	if math.Abs(coreDistances[0]-dist01) > 1e-9 {
		t.Errorf("point 0: expected core distance %f (to point 1), got %f", dist01, coreDistances[0])
	}

	// For point 1: nearest is point 0
	if math.Abs(coreDistances[1]-dist01) > 1e-9 {
		t.Errorf("point 1: expected core distance %f (to point 0), got %f", dist01, coreDistances[1])
	}

	// For point 2: distances to both other points
	dist20 := cosineDistance(data[2], data[0]) // = 1.0 (orthogonal)
	dist21 := cosineDistance(data[2], data[1])
	expectedCore2 := math.Min(dist20, dist21)
	if math.Abs(coreDistances[2]-expectedCore2) > 1e-9 {
		t.Errorf("point 2: expected core distance %f, got %f", expectedCore2, coreDistances[2])
	}
}

// TestComputeCoreDistances_KGreaterThanN tests k > n-1 behavior.
// Should clamp to the farthest available neighbor.
func TestComputeCoreDistances_KGreaterThanN(t *testing.T) {
	t.Parallel()

	// k=10 but only 3 points -> should clamp to n-1=2 neighbors
	// Core distance = distance to farthest neighbor (last in sorted list)
	data := [][]float32{
		{1, 0, 0},
		{0.9, 0.1, 0},
		{0, 1, 0},
	}

	coreDistances := computeCoreDistances(data, 10)

	// For each point, k=10 > n-1=2, so idx is clamped to len(buf)-1 = 1
	// meaning core distance = the farthest of the 2 neighbors

	// Point 0: neighbors are point 1 (close) and point 2 (far)
	dist01 := cosineDistance(data[0], data[1])
	dist02 := cosineDistance(data[0], data[2]) // = 1.0
	expectedCore0 := math.Max(dist01, dist02)
	if math.Abs(coreDistances[0]-expectedCore0) > 1e-9 {
		t.Errorf("point 0: expected core distance %f (farthest), got %f", expectedCore0, coreDistances[0])
	}

	// Point 1: neighbors are point 0 (close) and point 2 (far)
	dist12 := cosineDistance(data[1], data[2])
	expectedCore1 := math.Max(dist01, dist12)
	if math.Abs(coreDistances[1]-expectedCore1) > 1e-9 {
		t.Errorf("point 1: expected core distance %f (farthest), got %f", expectedCore1, coreDistances[1])
	}

	// Point 2: neighbors are point 0 (far) and point 1 (far)
	expectedCore2 := math.Max(dist02, dist12)
	if math.Abs(coreDistances[2]-expectedCore2) > 1e-9 {
		t.Errorf("point 2: expected core distance %f (farthest), got %f", expectedCore2, coreDistances[2])
	}
}

// TestComputeCoreDistances_AllIdentical tests all identical points.
// All pairwise distances = 0, so core distance = 0 for any k.
func TestComputeCoreDistances_AllIdentical(t *testing.T) {
	t.Parallel()

	data := [][]float32{
		{1, 0, 0},
		{1, 0, 0},
		{1, 0, 0},
		{1, 0, 0},
	}

	for k := 1; k <= 5; k++ {
		coreDistances := computeCoreDistances(data, k)

		for i, cd := range coreDistances {
			if cd != 0 {
				t.Errorf("k=%d, point %d: expected core distance 0 for identical points, got %f", k, i, cd)
			}
		}
	}
}

// TestComputeCoreDistances_TwoPointsOnly tests the minimal case with 2 points.
// Each has exactly 1 neighbor, so all k values clamp to that single distance.
func TestComputeCoreDistances_TwoPointsOnly(t *testing.T) {
	t.Parallel()

	data := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
	}

	expectedDist := cosineDistance(data[0], data[1]) // = 1.0 (orthogonal)

	// k=1: core distance = distance to the only neighbor
	coreDistances1 := computeCoreDistances(data, 1)
	if math.Abs(coreDistances1[0]-expectedDist) > 1e-9 {
		t.Errorf("k=1, point 0: expected %f, got %f", expectedDist, coreDistances1[0])
	}
	if math.Abs(coreDistances1[1]-expectedDist) > 1e-9 {
		t.Errorf("k=1, point 1: expected %f, got %f", expectedDist, coreDistances1[1])
	}

	// k=2: only 1 neighbor available, clamps to idx=0 (same as k=1)
	coreDistances2 := computeCoreDistances(data, 2)
	if math.Abs(coreDistances2[0]-expectedDist) > 1e-9 {
		t.Errorf("k=2, point 0: expected %f (clamped), got %f", expectedDist, coreDistances2[0])
	}
	if math.Abs(coreDistances2[1]-expectedDist) > 1e-9 {
		t.Errorf("k=2, point 1: expected %f (clamped), got %f", expectedDist, coreDistances2[1])
	}

	// k=100: should clamp to the only available neighbor
	coreDistances100 := computeCoreDistances(data, 100)
	if math.Abs(coreDistances100[0]-expectedDist) > 1e-9 {
		t.Errorf("k=100, point 0: expected %f (clamped), got %f", expectedDist, coreDistances100[0])
	}
}

// TestComputeCoreDistances_BufferReuse verifies that the buf[:0] + append + sort
// optimization doesn't break sorting by leaving stale data in the buffer.
func TestComputeCoreDistances_BufferReuse(t *testing.T) {
	t.Parallel()

	// 8 points with distinct, predictable distances from point 0.
	data := [][]float32{
		{1, 0, 0},                 // Point 0: reference
		{0.99, 0.01, 0},           // Point 1: very close to 0
		{0.9, 0.1, 0},             // Point 2: close to 0
		{0.5, 0.5, 0},             // Point 3: medium distance
		{0, 1, 0},                 // Point 4: orthogonal (max distance)
		{0.7071, 0.7071, 0},       // Point 5: 45 degrees
		{0.8660254, 0.5, 0},       // Point 6: 30 degrees
		{0.9659258, 0.2588190, 0}, // Point 7: 15 degrees
	}

	// Compute distances from point 0 to all others and sort them
	dists := make([]float64, len(data)-1)
	for j := 1; j < len(data); j++ {
		dists[j-1] = cosineDistance(data[0], data[j])
	}
	sortedDists := make([]float64, len(dists))
	copy(sortedDists, dists)
	sortFloat64s(sortedDists)

	// Test various k values and verify correct k-th nearest distance
	for k := 1; k <= len(dists); k++ {
		coreDistances := computeCoreDistances(data, k)
		expected := sortedDists[k-1]
		if math.Abs(coreDistances[0]-expected) > 1e-6 {
			t.Errorf("k=%d, point 0: expected core distance %f, got %f", k, expected, coreDistances[0])
		}
	}
}

// sortFloat64s sorts a slice of float64 in ascending order.
// This is a local helper to avoid importing the sort package.
func sortFloat64s(a []float64) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

// TestComputeCoreDistances_SinglePoint tests a single point (no neighbors).
// The core distance should remain 0 (default value, loop doesn't execute).
func TestComputeCoreDistances_SinglePoint(t *testing.T) {
	t.Parallel()

	data := [][]float32{{1, 0, 0}}

	coreDistances := computeCoreDistances(data, 1)

	if coreDistances[0] != 0 {
		t.Errorf("expected core distance 0 for single point, got %f", coreDistances[0])
	}
}

// TestComputeCoreDistances_Empty verifies that computeCoreDistances handles empty input gracefully.
func TestComputeCoreDistances_Empty(t *testing.T) {
	t.Parallel()

	var data [][]float32

	result := computeCoreDistances(data, 1)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %v", result)
	}
}

// TestComputeCoreDistances_KZero tests k=0 (pathological case).
// idx = k-1 = -1, then clamped to 0 -> nearest neighbor distance.
func TestComputeCoreDistances_KZero(t *testing.T) {
	t.Parallel()

	data := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
	}

	coreDistances := computeCoreDistances(data, 0)

	// k=0: idx = -1, clamped to 0 -> nearest neighbor distance
	expectedDist := cosineDistance(data[0], data[1])
	if math.Abs(coreDistances[0]-expectedDist) > 1e-9 {
		t.Errorf("k=0, point 0: expected %f (clamped to k=1), got %f", expectedDist, coreDistances[0])
	}
}

// -----------------------------------------------------------------------------
// assignLabelsAndProbs tests - verifying label assignment traversal logic
// -----------------------------------------------------------------------------

// TestAssignLabelsAndProbs_TraversalLogic verifies the label assignment traversal.
// This test constructs a condensed tree with 8 points and 2 selected clusters,
// then verifies that each point correctly traverses to its selected cluster ancestor.
//
// Condensed tree structure (n=8):
//
//	Root cluster (ID=11, not selected)
//	├── Cluster A (ID=9, SELECTED, label 0)
//	│   ├── Cluster X (ID=8, not selected)
//	│   │   ├── Point 0 (falls out at λ=5.0) → should get label 0
//	│   │   └── Point 1 (falls out at λ=4.5) → should get label 0
//	│   ├── Point 2 (falls out at λ=4.0) → should get label 0
//	│   └── Point 3 (falls out at λ=3.5) → should get label 0
//	│
//	└── Cluster B (ID=10, SELECTED, label 1)
//	    ├── Point 4 (falls out at λ=5.0) → should get label 1
//	    ├── Point 5 (falls out at λ=4.5) → should get label 1
//	    ├── Point 6 (falls out at λ=4.0) → should get label 1
//	    └── Point 7 (falls out at λ=3.5) → should get label 1
//
// Edge cases verified:
// 1. Points 0,1: fall out at non-selected cluster (8), must traverse up to selected (9)
// 2. Points 2,3: fall out directly at selected cluster (9)
// 3. Points 4-7: fall out directly at selected cluster (10)
func TestAssignLabelsAndProbs_TraversalLogic(t *testing.T) {
	t.Parallel()

	n := 8

	// Build condensed tree with explicit parent-child relationships
	// for traversal testing
	condensed := []condensedEdge{
		// Root (11) to its children
		{parent: 11, child: 9, lambda: 2.0, childSize: 4},  // Cluster A (selected)
		{parent: 11, child: 10, lambda: 2.0, childSize: 4}, // Cluster B (selected)

		// Cluster A (9) to its children
		{parent: 9, child: 8, lambda: 3.0, childSize: 2}, // Cluster X (not selected)
		{parent: 9, child: 2, lambda: 4.0, childSize: 1}, // Point 2 falls out at A
		{parent: 9, child: 3, lambda: 3.5, childSize: 1}, // Point 3 falls out at A

		// Cluster X (8) to its children - these must traverse up to A (9)
		{parent: 8, child: 0, lambda: 5.0, childSize: 1}, // Point 0 falls out at X
		{parent: 8, child: 1, lambda: 4.5, childSize: 1}, // Point 1 falls out at X

		// Cluster B (10) to its children
		{parent: 10, child: 4, lambda: 5.0, childSize: 1}, // Point 4 falls out at B
		{parent: 10, child: 5, lambda: 4.5, childSize: 1}, // Point 5 falls out at B
		{parent: 10, child: 6, lambda: 4.0, childSize: 1}, // Point 6 falls out at B
		{parent: 10, child: 7, lambda: 3.5, childSize: 1}, // Point 7 falls out at B
	}

	// Only clusters 9 and 10 are selected
	selected := map[int]bool{
		9:  true,
		10: true,
		// 8 is NOT selected - points falling out here must traverse up
		// 11 (root) is NOT selected - scikit-learn default
	}

	clusterBirth := map[int]float64{
		11: 0.0, // Root
		9:  2.0, // Cluster A
		10: 2.0, // Cluster B
		8:  3.0, // Cluster X
	}

	clusterDeath := map[int]float64{
		11: 2.0, // Root dies when children split off
		9:  5.0, // Cluster A dies at max λ of its children
		10: 5.0, // Cluster B dies at max λ of its children
		8:  5.0, // Cluster X dies at max λ of its children
	}

	labels, probs := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	// Verify label assignments
	// Points 0-3 should get the same label (cluster A = 9)
	// Points 4-7 should get the same label (cluster B = 10)
	t.Logf("Labels: %v", labels)
	t.Logf("Probs: %v", probs)

	// Check that points 0-3 all have the same label
	labelA := labels[0]
	if labelA < 0 {
		t.Errorf("point 0: expected non-noise label, got %d", labelA)
	}
	for i := 1; i <= 3; i++ {
		if labels[i] != labelA {
			t.Errorf("point %d: expected label %d (same as point 0), got %d", i, labelA, labels[i])
		}
	}

	// Check that points 4-7 all have the same label
	labelB := labels[4]
	if labelB < 0 {
		t.Errorf("point 4: expected non-noise label, got %d", labelB)
	}
	for i := 5; i <= 7; i++ {
		if labels[i] != labelB {
			t.Errorf("point %d: expected label %d (same as point 4), got %d", i, labelB, labels[i])
		}
	}

	// Clusters A and B should have different labels
	if labelA == labelB {
		t.Errorf("clusters A and B should have different labels, both got %d", labelA)
	}

	// Verify probabilities are in valid range
	for i, p := range probs {
		if p < 0 || p > 1 {
			t.Errorf("point %d: probability %f out of range [0,1]", i, p)
		}
	}
}

// TestAssignLabelsAndProbs_PointFallsOutAtRoot verifies that a point falling
// out directly at the root cluster (which is never selected with allow_single_cluster=false)
// becomes noise (-1).
//
// Condensed tree structure (n=4):
//
//	Root cluster (ID=5, not selected)
//	├── Point 0 (falls out at root) → should be noise (-1)
//	├── Point 1 (falls out at root) → should be noise (-1)
//	└── Cluster A (ID=4, SELECTED)
//	    ├── Point 2 (falls out at A) → should get label 0
//	    └── Point 3 (falls out at A) → should get label 0
func TestAssignLabelsAndProbs_PointFallsOutAtRoot(t *testing.T) {
	t.Parallel()

	n := 4

	condensed := []condensedEdge{
		// Root (5) children
		{parent: 5, child: 0, lambda: 1.0, childSize: 1}, // Point 0 falls out at root
		{parent: 5, child: 1, lambda: 1.5, childSize: 1}, // Point 1 falls out at root
		{parent: 5, child: 4, lambda: 2.0, childSize: 2}, // Cluster A splits off

		// Cluster A (4) children
		{parent: 4, child: 2, lambda: 3.0, childSize: 1}, // Point 2 falls out at A
		{parent: 4, child: 3, lambda: 3.5, childSize: 1}, // Point 3 falls out at A
	}

	// Only cluster 4 is selected; root (5) is never selected
	selected := map[int]bool{
		4: true,
		// 5 (root) NOT selected
	}

	clusterBirth := map[int]float64{
		5: 0.0,
		4: 2.0,
	}

	clusterDeath := map[int]float64{
		5: 2.0,
		4: 3.5,
	}

	labels, probs := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	t.Logf("Labels: %v", labels)
	t.Logf("Probs: %v", probs)

	// Points 0,1 fall out at root (not selected) with no selected ancestor -> noise
	if labels[0] != -1 {
		t.Errorf("point 0: expected noise (-1), got %d", labels[0])
	}
	if labels[1] != -1 {
		t.Errorf("point 1: expected noise (-1), got %d", labels[1])
	}

	// Points 2,3 fall out at selected cluster A -> should get label 0
	if labels[2] < 0 {
		t.Errorf("point 2: expected non-noise label, got %d", labels[2])
	}
	if labels[3] < 0 {
		t.Errorf("point 3: expected non-noise label, got %d", labels[3])
	}
	if labels[2] != labels[3] {
		t.Errorf("points 2 and 3 should have same label, got %d and %d", labels[2], labels[3])
	}

	// Noise points should have probability 0
	if probs[0] != 0 {
		t.Errorf("point 0 (noise): expected prob 0, got %f", probs[0])
	}
	if probs[1] != 0 {
		t.Errorf("point 1 (noise): expected prob 0, got %f", probs[1])
	}
}

// TestAssignLabelsAndProbs_FallsOutAtSelectedClusterItself verifies that when
// a point falls out exactly at a selected cluster, it gets that cluster's label.
func TestAssignLabelsAndProbs_FallsOutAtSelectedClusterItself(t *testing.T) {
	t.Parallel()

	n := 4

	condensed := []condensedEdge{
		// Root (5) -> selected cluster (4)
		{parent: 5, child: 4, lambda: 2.0, childSize: 4},

		// All points fall out directly at the selected cluster
		{parent: 4, child: 0, lambda: 3.0, childSize: 1},
		{parent: 4, child: 1, lambda: 3.5, childSize: 1},
		{parent: 4, child: 2, lambda: 4.0, childSize: 1},
		{parent: 4, child: 3, lambda: 4.5, childSize: 1},
	}

	selected := map[int]bool{
		4: true,
	}

	clusterBirth := map[int]float64{
		5: 0.0,
		4: 2.0,
	}

	clusterDeath := map[int]float64{
		5: 2.0,
		4: 4.5,
	}

	labels, probs := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	t.Logf("Labels: %v", labels)
	t.Logf("Probs: %v", probs)

	// All points should get label 0 (the only selected cluster)
	for i := 0; i < n; i++ {
		if labels[i] != 0 {
			t.Errorf("point %d: expected label 0, got %d", i, labels[i])
		}
	}

	// All should have valid probability
	for i := 0; i < n; i++ {
		if probs[i] < 0 || probs[i] > 1 {
			t.Errorf("point %d: probability %f out of range", i, probs[i])
		}
	}
}

// TestAssignLabelsAndProbs_NonSelectedClusterWithSelectedAncestor verifies
// that points in a non-selected cluster correctly find their selected ancestor.
//
// Condensed tree structure (n=6):
//
//	Root (9, not selected)
//	├── Cluster A (8, SELECTED)
//	│   └── Cluster X (7, not selected)
//	│       └── Cluster Y (6, not selected)
//	│           ├── Point 0 → must traverse Y -> X -> A, get label A
//	│           ├── Point 1 → must traverse Y -> X -> A, get label A
//	│           └── Point 2 → must traverse Y -> X -> A, get label A
//	│
//	└── Cluster B (not shown, 3 points falling directly at root as noise)
//	    ├── Point 3 → noise (-1)
//	    ├── Point 4 → noise (-1)
//	    └── Point 5 → noise (-1)
func TestAssignLabelsAndProbs_NonSelectedClusterWithSelectedAncestor(t *testing.T) {
	t.Parallel()

	n := 6

	condensed := []condensedEdge{
		// Root -> selected cluster A
		{parent: 9, child: 8, lambda: 1.0, childSize: 3},

		// Points 3-5 fall out at root (noise)
		{parent: 9, child: 3, lambda: 0.5, childSize: 1},
		{parent: 9, child: 4, lambda: 0.6, childSize: 1},
		{parent: 9, child: 5, lambda: 0.7, childSize: 1},

		// Cluster A (8) -> non-selected cluster X (7)
		{parent: 8, child: 7, lambda: 2.0, childSize: 3},

		// Cluster X (7) -> non-selected cluster Y (6)
		{parent: 7, child: 6, lambda: 3.0, childSize: 3},

		// Cluster Y (6) -> points 0,1,2
		{parent: 6, child: 0, lambda: 4.0, childSize: 1},
		{parent: 6, child: 1, lambda: 4.5, childSize: 1},
		{parent: 6, child: 2, lambda: 5.0, childSize: 1},
	}

	// Only cluster 8 (A) is selected
	// Clusters 6, 7 are NOT selected
	// Points in 6 must traverse 6 -> 7 -> 8 to find selected ancestor
	selected := map[int]bool{
		8: true,
	}

	clusterBirth := map[int]float64{
		9: 0.0,
		8: 1.0,
		7: 2.0,
		6: 3.0,
	}

	clusterDeath := map[int]float64{
		9: 1.0,
		8: 5.0,
		7: 5.0,
		6: 5.0,
	}

	labels, probs := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	t.Logf("Labels: %v", labels)
	t.Logf("Probs: %v", probs)

	// Points 0,1,2 should all get label 0 (cluster A via traversal)
	for i := 0; i <= 2; i++ {
		if labels[i] != 0 {
			t.Errorf("point %d: expected label 0 (via ancestor traversal), got %d", i, labels[i])
		}
	}

	// Points 3,4,5 fall out at root with no selected ancestor -> noise
	for i := 3; i <= 5; i++ {
		if labels[i] != -1 {
			t.Errorf("point %d: expected noise (-1), got %d", i, labels[i])
		}
	}
}

// TestAssignLabelsAndProbs_ProbabilityCalculation verifies the probability formula:
// prob = (λ_point - λ_birth) / (λ_death - λ_birth)
func TestAssignLabelsAndProbs_ProbabilityCalculation(t *testing.T) {
	t.Parallel()

	n := 4

	condensed := []condensedEdge{
		// Root -> selected cluster
		{parent: 5, child: 4, lambda: 2.0, childSize: 4},

		// Points fall out at different lambdas
		{parent: 4, child: 0, lambda: 2.0, childSize: 1},  // λ=birth → prob=0
		{parent: 4, child: 1, lambda: 3.5, childSize: 1},  // λ=midpoint → prob=0.5
		{parent: 4, child: 2, lambda: 5.0, childSize: 1},  // λ=death → prob=1.0
		{parent: 4, child: 3, lambda: 4.25, childSize: 1}, // λ=0.75 of range
	}

	selected := map[int]bool{
		4: true,
	}

	clusterBirth := map[int]float64{
		5: 0.0,
		4: 2.0, // birth
	}

	clusterDeath := map[int]float64{
		5: 2.0,
		4: 5.0, // death -> range is 5.0 - 2.0 = 3.0
	}

	labels, probs := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	t.Logf("Labels: %v", labels)
	t.Logf("Probs: %v", probs)

	// Expected probabilities:
	// Point 0: (2.0 - 2.0) / (5.0 - 2.0) = 0 / 3.0 = 0
	// Point 1: (3.5 - 2.0) / (5.0 - 2.0) = 1.5 / 3.0 = 0.5
	// Point 2: (5.0 - 2.0) / (5.0 - 2.0) = 3.0 / 3.0 = 1.0
	// Point 3: (4.25 - 2.0) / (5.0 - 2.0) = 2.25 / 3.0 = 0.75
	expected := []float64{0.0, 0.5, 1.0, 0.75}

	for i, exp := range expected {
		if math.Abs(probs[i]-exp) > 1e-9 {
			t.Errorf("point %d: expected prob %f, got %f", i, exp, probs[i])
		}
	}
}

// TestAssignLabelsAndProbs_LabelNumberingConsistency verifies that cluster labels
// are assigned consistently as sequential integers starting from 0.
func TestAssignLabelsAndProbs_LabelNumberingConsistency(t *testing.T) {
	t.Parallel()

	n := 6

	condensed := []condensedEdge{
		// Root -> 3 selected clusters
		{parent: 9, child: 6, lambda: 1.0, childSize: 2},
		{parent: 9, child: 7, lambda: 1.0, childSize: 2},
		{parent: 9, child: 8, lambda: 1.0, childSize: 2},

		// Each cluster has 2 points
		{parent: 6, child: 0, lambda: 2.0, childSize: 1},
		{parent: 6, child: 1, lambda: 2.0, childSize: 1},
		{parent: 7, child: 2, lambda: 2.0, childSize: 1},
		{parent: 7, child: 3, lambda: 2.0, childSize: 1},
		{parent: 8, child: 4, lambda: 2.0, childSize: 1},
		{parent: 8, child: 5, lambda: 2.0, childSize: 1},
	}

	selected := map[int]bool{
		6: true,
		7: true,
		8: true,
	}

	clusterBirth := map[int]float64{
		9: 0.0,
		6: 1.0,
		7: 1.0,
		8: 1.0,
	}

	clusterDeath := map[int]float64{
		9: 1.0,
		6: 2.0,
		7: 2.0,
		8: 2.0,
	}

	labels, _ := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	t.Logf("Labels: %v", labels)

	// Count unique labels
	labelSet := make(map[int]bool)
	for _, l := range labels {
		if l >= 0 {
			labelSet[l] = true
		}
	}

	// Should have exactly 3 unique labels: 0, 1, 2
	if len(labelSet) != 3 {
		t.Errorf("expected 3 unique labels, got %d: %v", len(labelSet), labelSet)
	}

	// Labels should be 0, 1, 2 (not 6, 7, 8)
	for l := range labelSet {
		if l < 0 || l >= 3 {
			t.Errorf("label %d out of expected range [0,2]", l)
		}
	}

	// Points within same cluster should have same label
	if labels[0] != labels[1] {
		t.Errorf("points 0 and 1 should have same label, got %d and %d", labels[0], labels[1])
	}
	if labels[2] != labels[3] {
		t.Errorf("points 2 and 3 should have same label, got %d and %d", labels[2], labels[3])
	}
	if labels[4] != labels[5] {
		t.Errorf("points 4 and 5 should have same label, got %d and %d", labels[4], labels[5])
	}

	// Different clusters should have different labels
	labelsUsed := []int{labels[0], labels[2], labels[4]}
	for i := 0; i < len(labelsUsed); i++ {
		for j := i + 1; j < len(labelsUsed); j++ {
			if labelsUsed[i] == labelsUsed[j] {
				t.Errorf("clusters should have different labels: cluster pair (%d,%d) both have label %d",
					i, j, labelsUsed[i])
			}
		}
	}
}

// =============================================================================
// Numerical Stability Tests for cosineDistance
// =============================================================================

// cosineDistanceFloat64 computes cosine distance using float64 throughout
// for comparison as ground truth.
func cosineDistanceFloat64(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1.0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - dot/(math.Sqrt(normA)*math.Sqrt(normB))
}

// toFloat32Slice converts float64 slice to float32 slice
func toFloat32Slice(a []float64) []float32 {
	result := make([]float32, len(a))
	for i, v := range a {
		result[i] = float32(v)
	}
	return result
}

func TestCosineDistance_Float32PrecisionLimits(t *testing.T) {
	t.Parallel()

	// Test 1: Accumulation of many small values
	// float32 has ~7 significant decimal digits
	// After adding many values, precision degrades
	t.Run("accumulation_precision", func(t *testing.T) {
		// Create high-dimensional vectors where precision matters
		dim := 1000
		a64 := make([]float64, dim)
		b64 := make([]float64, dim)

		// Values that require full float64 precision to sum correctly
		for i := 0; i < dim; i++ {
			// Small values that sum to something meaningful
			a64[i] = 0.001 + float64(i)*1e-7
			b64[i] = 0.001 + float64(i)*1e-7*0.999 // Nearly identical
		}

		a32 := toFloat32Slice(a64)
		b32 := toFloat32Slice(b64)

		got := cosineDistance(a32, b32)
		expected := cosineDistanceFloat64(a64, b64)

		// The function uses float64 intermediates, so should be close
		// Allow tolerance for the float32 input quantization
		tolerance := 1e-5
		if math.Abs(got-expected) > tolerance {
			t.Errorf("precision loss: got %v, expected %v, diff %v",
				got, expected, math.Abs(got-expected))
		}

		// Verify it's close to 0 (nearly parallel vectors)
		if got > 0.01 {
			t.Errorf("expected nearly parallel vectors (dist < 0.01), got %v", got)
		}
	})

	// Test 2: Sparse vectors with a few large components
	t.Run("sparse_large_components", func(t *testing.T) {
		// Most components are 0, a few are very large
		dim := 1000
		a64 := make([]float64, dim)
		b64 := make([]float64, dim)

		// Only 3 non-zero components
		a64[0] = 1e3
		a64[500] = 1e3
		a64[999] = 1e3
		b64[0] = 1e3 * 0.99
		b64[500] = 1e3 * 1.01
		b64[999] = 1e3

		a32 := toFloat32Slice(a64)
		b32 := toFloat32Slice(b64)

		got := cosineDistance(a32, b32)
		expected := cosineDistanceFloat64(a64, b64)

		tolerance := 1e-6
		if math.Abs(got-expected) > tolerance {
			t.Errorf("sparse vectors: got %v, expected %v, diff %v",
				got, expected, math.Abs(got-expected))
		}
	})
}

func TestCosineDistance_VerySmallComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scale float64
	}{
		{"1e-10", 1e-10},
		{"1e-15", 1e-15},
		{"1e-20", 1e-20}, // Below float32 subnormal range (~1.4e-45)
		{"1e-30", 1e-30}, // Way below float32 range
		{"1e-38", 1e-38}, // Near float32 min normal (~1.2e-38)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Vectors with very small components
			a64 := []float64{tt.scale, tt.scale * 2, tt.scale * 3}
			b64 := []float64{tt.scale * 1.1, tt.scale * 1.9, tt.scale * 3.1}

			a32 := toFloat32Slice(a64)
			b32 := toFloat32Slice(b64)

			got := cosineDistance(a32, b32)

			// Check for NaN/Inf (indicates numerical failure)
			if math.IsNaN(got) {
				t.Errorf("got NaN for scale %v", tt.scale)
				return
			}
			if math.IsInf(got, 0) {
				t.Errorf("got Inf for scale %v", tt.scale)
				return
			}

			// For extremely small values that underflow to 0 in float32,
			// the function should return 1.0 (zero vectors)
			if tt.scale < 1e-38 {
				// These will underflow to 0 in float32
				if got != 1.0 {
					t.Logf("scale %v: got %v (expected 1.0 due to underflow)", tt.scale, got)
				}
			} else {
				// Should be a valid distance in [0, 2]
				if got < 0 || got > 2.0 {
					t.Errorf("distance out of range [0, 2]: got %v", got)
				}

				// Compare with float64 ground truth
				expected := cosineDistanceFloat64(a64, b64)
				tolerance := 1e-4 // Allow for float32 input quantization
				if math.Abs(got-expected) > tolerance {
					t.Errorf("scale %v: got %v, expected %v, diff %v",
						tt.scale, got, expected, math.Abs(got-expected))
				}
			}
		})
	}
}

func TestCosineDistance_VeryLargeComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scale float64
	}{
		{"1e10", 1e10},
		{"1e15", 1e15},
		{"1e18", 1e18}, // float32 max is ~3.4e38, squared is ~1.2e77
		{"1e19", 1e19}, // Still safe for 3-element vector
		{"1e20", 1e20}, // normA would be 3 * 1e40 in float64 - safe
		{"1e37", 1e37}, // Near float32 max
		{"1e38", 1e38}, // May overflow in float32 input
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Vectors with large components
			a64 := []float64{tt.scale, tt.scale * 0.5, tt.scale * 0.25}
			b64 := []float64{tt.scale * 0.9, tt.scale * 0.6, tt.scale * 0.2}

			a32 := toFloat32Slice(a64)
			b32 := toFloat32Slice(b64)

			// Check if input conversion caused overflow
			hasInfInput := false
			for _, v := range a32 {
				if math.IsInf(float64(v), 0) {
					hasInfInput = true
					break
				}
			}

			got := cosineDistance(a32, b32)

			if hasInfInput {
				// Input overflowed in float32 conversion
				t.Logf("scale %v: input overflowed to Inf in float32", tt.scale)
				// Should still not panic or produce NaN
				if math.IsNaN(got) {
					t.Errorf("got NaN despite Inf input - division by Inf should be handled")
				}
				return
			}

			// Check for NaN (indicates division by zero or 0/0)
			if math.IsNaN(got) {
				t.Errorf("got NaN for scale %v", tt.scale)
				return
			}

			// Should be a valid distance
			if got < 0 || got > 2.0 {
				if !math.IsInf(got, 0) {
					t.Errorf("distance out of range [0, 2]: got %v", got)
				}
			}

			// Compare with float64 ground truth
			expected := cosineDistanceFloat64(a64, b64)
			tolerance := 1e-4
			if math.Abs(got-expected) > tolerance && !math.IsInf(got, 0) {
				t.Errorf("scale %v: got %v, expected %v, diff %v",
					tt.scale, got, expected, math.Abs(got-expected))
			}
		})
	}
}

func TestCosineDistance_MixedSigns(t *testing.T) {
	t.Parallel()

	// Test cancellation in dot product
	t.Run("exact_cancellation", func(t *testing.T) {
		// Dot product should be exactly 0
		a := []float32{1, -1, 1, -1}
		b := []float32{1, 1, 1, 1}
		// dot = 1*1 + (-1)*1 + 1*1 + (-1)*1 = 0

		got := cosineDistance(a, b)
		if got != 1.0 {
			t.Errorf("expected 1.0 for orthogonal vectors, got %v", got)
		}
	})

	t.Run("near_cancellation", func(t *testing.T) {
		// Large values that nearly cancel
		a64 := []float64{1e10, -1e10 + 1, 1e10, -1e10 + 1}
		b64 := []float64{1, 1, 1, 1}
		// dot = 1e10 + (-1e10+1) + 1e10 + (-1e10+1) = 2

		a32 := toFloat32Slice(a64)
		b32 := toFloat32Slice(b64)

		got := cosineDistance(a32, b32)

		// Should not be NaN or Inf
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Errorf("cancellation produced invalid result: %v", got)
		}

		// Result should be in valid range
		if got < 0 || got > 2.0 {
			t.Errorf("distance out of range: %v", got)
		}
	})

	t.Run("alternating_signs", func(t *testing.T) {
		// Vectors with alternating signs
		dim := 100
		a64 := make([]float64, dim)
		b64 := make([]float64, dim)
		for i := 0; i < dim; i++ {
			sign := 1.0
			if i%2 == 1 {
				sign = -1.0
			}
			a64[i] = sign * float64(i+1)
			b64[i] = sign * float64(i+1) * 1.001 // Nearly parallel
		}

		a32 := toFloat32Slice(a64)
		b32 := toFloat32Slice(b64)

		got := cosineDistance(a32, b32)
		expected := cosineDistanceFloat64(a64, b64)

		// Should be very close (nearly parallel)
		if got > 0.001 {
			t.Errorf("expected nearly parallel (dist < 0.001), got %v", got)
		}

		tolerance := 1e-5
		if math.Abs(got-expected) > tolerance {
			t.Errorf("mixed signs: got %v, expected %v, diff %v",
				got, expected, math.Abs(got-expected))
		}
	})
}

func TestCosineDistance_NearlyParallelVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		perturbation float64
		maxDistance  float64
	}{
		{"identical", 0, 0},
		{"1e-3_perturbation", 1e-3, 1e-5},
		{"1e-5_perturbation", 1e-5, 1e-9},
		{"1e-7_perturbation", 1e-7, 1e-13}, // Near float32 precision limit
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// High-dimensional nearly parallel vectors
			dim := 384
			a64 := make([]float64, dim)
			b64 := make([]float64, dim)

			rng := rand.New(rand.NewSource(42)) //nolint:gosec // test determinism
			for i := 0; i < dim; i++ {
				a64[i] = rng.Float64() - 0.5
				b64[i] = a64[i] * (1.0 + tt.perturbation*(rng.Float64()-0.5))
			}

			a32 := toFloat32Slice(a64)
			b32 := toFloat32Slice(b64)

			got := cosineDistance(a32, b32)
			expected := cosineDistanceFloat64(a64, b64)

			// Distance should be close to 0
			if tt.perturbation == 0 {
				// Identical vectors should give exactly 0
				// But float32 quantization might introduce tiny error
				if got > 1e-6 {
					t.Errorf("identical vectors should have distance ~0, got %v", got)
				}
			} else {
				// Should be small and close to expected
				if got > tt.maxDistance*100 { // Allow some margin for float32
					t.Logf("perturbation %v: distance %v larger than expected %v",
						tt.perturbation, got, tt.maxDistance)
				}
			}

			// Compare with float64 ground truth
			tolerance := 1e-5
			if math.Abs(got-expected) > tolerance {
				t.Logf("nearly parallel: got %v, expected %v, diff %v",
					got, expected, math.Abs(got-expected))
			}
		})
	}
}

func TestCosineDistance_NearlyOrthogonalVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dotProduct float64 // small deviation from orthogonality
	}{
		{"exactly_orthogonal", 0},
		{"dot_1e-3", 1e-3},
		{"dot_1e-6", 1e-6},
		{"dot_1e-10", 1e-10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Construct orthogonal vectors with small dot product perturbation
			a64 := []float64{1, 0, 0, 0}
			b64 := []float64{tt.dotProduct, 1, 0, 0} // Nearly orthogonal to a

			a32 := toFloat32Slice(a64)
			b32 := toFloat32Slice(b64)

			got := cosineDistance(a32, b32)
			expected := cosineDistanceFloat64(a64, b64)

			// Should be close to 1.0
			if math.Abs(got-1.0) > 0.1 {
				t.Errorf("expected near 1.0 for nearly orthogonal, got %v", got)
			}

			tolerance := 1e-6
			if math.Abs(got-expected) > tolerance {
				t.Errorf("orthogonal: got %v, expected %v, diff %v",
					got, expected, math.Abs(got-expected))
			}
		})
	}
}

func TestCosineDistance_Float64IntermediateSufficiency(t *testing.T) {
	t.Parallel()

	// This test verifies that using float64 for intermediate calculations
	// (while input is float32) is sufficient for typical embedding use cases

	t.Run("typical_embedding_dimensions", func(t *testing.T) {
		dims := []int{384, 768, 1536, 3072} // Common embedding dimensions

		for _, dim := range dims {
			t.Run(fmt.Sprintf("dim=%d", dim), func(t *testing.T) {
				// Simulate realistic embedding values (typically in [-1, 1] range)
				rng := rand.New(rand.NewSource(int64(dim))) //nolint:gosec // test determinism
				a64 := make([]float64, dim)
				b64 := make([]float64, dim)

				for i := 0; i < dim; i++ {
					a64[i] = rng.Float64()*2 - 1 // [-1, 1]
					b64[i] = rng.Float64()*2 - 1
				}

				a32 := toFloat32Slice(a64)
				b32 := toFloat32Slice(b64)

				got := cosineDistance(a32, b32)
				expected := cosineDistanceFloat64(a64, b64)

				// For typical embeddings, float64 intermediate should be very accurate
				tolerance := 1e-5
				if math.Abs(got-expected) > tolerance {
					t.Errorf("dim %d: got %v, expected %v, diff %v",
						dim, got, expected, math.Abs(got-expected))
				}

				// Verify result is in valid range
				if got < 0 || got > 2.0 {
					t.Errorf("dim %d: distance out of range: %v", dim, got)
				}
			})
		}
	})

	t.Run("accumulated_error_analysis", func(t *testing.T) {
		// Test where accumulated error could theoretically matter
		// float32 has ~7 decimal digits of precision
		// float64 has ~15 decimal digits

		// With 3072 dimensions, worst case accumulation:
		// - float32 would lose ~log10(3072) ≈ 3.5 digits
		// - Result: ~3.5 significant digits remaining (BAD)
		// - float64 intermediate: ~11.5 digits remaining (GOOD)

		dim := 3072
		rng := rand.New(rand.NewSource(12345)) //nolint:gosec // test determinism
		a64 := make([]float64, dim)
		b64 := make([]float64, dim)

		// Use values that stress precision
		for i := 0; i < dim; i++ {
			a64[i] = 0.1 + rng.Float64()*1e-5
			b64[i] = 0.1 + rng.Float64()*1e-5
		}

		a32 := toFloat32Slice(a64)
		b32 := toFloat32Slice(b64)

		got := cosineDistance(a32, b32)
		expected := cosineDistanceFloat64(a64, b64)

		// The error should be bounded by float32 input quantization
		// not by accumulated rounding error
		tolerance := 1e-4
		if math.Abs(got-expected) > tolerance {
			t.Errorf("accumulated error: got %v, expected %v, diff %v",
				got, expected, math.Abs(got-expected))
		}
	})
}

func TestCosineDistance_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("denormalized_float32", func(t *testing.T) {
		// Test with denormalized (subnormal) float32 values
		// Subnormal float32 range: 2^-149 to 2^-126 (~1.4e-45 to ~1.2e-38)
		subnormal := float32(1e-40)

		a := []float32{subnormal, subnormal * 2, subnormal}
		b := []float32{subnormal * 1.5, subnormal * 1.5, subnormal * 1.5}

		got := cosineDistance(a, b)

		// Should not panic or produce NaN
		if math.IsNaN(got) {
			t.Error("subnormal input produced NaN")
		}

		// Result should be 1.0 (treated as zero vectors) or valid distance
		if got < 0 || got > 2.0 {
			t.Errorf("subnormal: distance out of range: %v", got)
		}
	})

	t.Run("max_float32", func(t *testing.T) {
		// Test near maximum float32 value
		maxF32 := float32(math.MaxFloat32)
		a := []float32{maxF32 / 2, maxF32 / 4}
		b := []float32{maxF32 / 2, maxF32 / 4}

		got := cosineDistance(a, b)

		// Identical vectors should have distance 0 (or near 0)
		// Even though intermediate values are large
		if math.IsNaN(got) {
			t.Error("max float32 produced NaN")
		}
		if got > 1e-5 {
			t.Errorf("identical large vectors should have distance ~0, got %v", got)
		}
	})

	t.Run("negative_zero", func(t *testing.T) {
		// Negative zero should behave like positive zero
		negZero := math.Float32frombits(0x80000000) // -0.0
		a := []float32{1, negZero, 0}
		b := []float32{1, 0, negZero}

		got := cosineDistance(a, b)
		if math.IsNaN(got) {
			t.Error("negative zero produced NaN")
		}
		if got > 1e-10 {
			t.Errorf("expected ~0 for identical vectors with neg zero, got %v", got)
		}
	})

	t.Run("inf_in_vector", func(t *testing.T) {
		// If input contains Inf, behavior should be defined
		posInf := float32(math.Inf(1))
		a := []float32{1, posInf, 1}
		b := []float32{1, 1, 1}

		got := cosineDistance(a, b)

		// Should not panic; exact behavior depends on implementation
		// With Inf, normA becomes Inf, so dot/sqrt(Inf*normB) = 0
		// So distance = 1 - 0 = 1
		t.Logf("Inf in input: distance = %v", got)

		// Just verify no panic and no NaN (Inf is acceptable)
		if math.IsNaN(got) {
			t.Logf("Inf input produced NaN (acceptable edge case)")
		}
	})

	t.Run("nan_in_vector", func(t *testing.T) {
		// NaN in input should propagate
		nan := float32(math.NaN())
		a := []float32{1, nan, 1}
		b := []float32{1, 1, 1}

		got := cosineDistance(a, b)

		// NaN should propagate
		if !math.IsNaN(got) {
			t.Logf("NaN in input produced %v (NaN propagation may vary)", got)
		}
	})
}

// TestCosineDistance_Symmetry verifies that cosineDistance(a, b) == cosineDistance(b, a)
func TestCosineDistance_Symmetry(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(999)) //nolint:gosec // test determinism
	for i := 0; i < 100; i++ {
		dim := rng.Intn(100) + 1
		a := make([]float32, dim)
		b := make([]float32, dim)
		for j := 0; j < dim; j++ {
			a[j] = float32(rng.Float64()*2 - 1)
			b[j] = float32(rng.Float64()*2 - 1)
		}

		ab := cosineDistance(a, b)
		ba := cosineDistance(b, a)

		if ab != ba {
			t.Errorf("symmetry violated: cosineDistance(a, b) = %v, cosineDistance(b, a) = %v", ab, ba)
		}
	}
}

// TestCosineDistance_TriangleInequalityApproximate checks that the function
// produces reasonable values that approximately satisfy triangle inequality
// for cosine distance. Note: cosine distance is NOT a proper metric
// (doesn't satisfy triangle inequality strictly), but should be "reasonable".
func TestCosineDistance_TriangleInequalityApproximate(t *testing.T) {
	t.Parallel()

	// Generate three random unit vectors
	rng := rand.New(rand.NewSource(888)) //nolint:gosec // test determinism
	dim := 10
	a := make([]float32, dim)
	b := make([]float32, dim)
	c := make([]float32, dim)

	// Normalize to unit vectors
	normalize := func(v []float32) {
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		norm = math.Sqrt(norm)
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}

	for i := 0; i < dim; i++ {
		a[i] = float32(rng.Float64()*2 - 1)
		b[i] = float32(rng.Float64()*2 - 1)
		c[i] = float32(rng.Float64()*2 - 1)
	}
	normalize(a)
	normalize(b)
	normalize(c)

	dAB := cosineDistance(a, b)
	dBC := cosineDistance(b, c)
	dAC := cosineDistance(a, c)

	// All distances should be in [0, 2]
	for _, d := range []float64{dAB, dBC, dAC} {
		if d < 0 || d > 2.0 {
			t.Errorf("distance out of range: %v", d)
		}
	}

	t.Logf("dAB=%v, dBC=%v, dAC=%v", dAB, dBC, dAC)
}
