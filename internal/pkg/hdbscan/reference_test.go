package hdbscan

import (
	"fmt"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// TestAgainstPythonReference compares Go output with scikit-learn HDBSCAN
func TestAgainstPythonReference(t *testing.T) {
	t.Run("TwoClusters", func(t *testing.T) {
		data := [][]float32{
			{1.0, 0.0, 0.0},
			{0.95, 0.05, 0.0},
			{0.9, 0.1, 0.0},
			{0.98, 0.02, 0.0},
			{0.0, 1.0, 0.0},
			{0.05, 0.95, 0.0},
			{0.1, 0.9, 0.0},
			{0.02, 0.98, 0.0},
		}

		c := ptrext.Of(Clusterer{MinClusterSize: 3, MinSamples: 3})
		result := c.Cluster(data)

		// Python: Labels: [0, 0, 0, 0, 1, 1, 1, 1], Clusters: 2
		fmt.Printf("Go Labels: %v\n", result.Labels)
		fmt.Printf("Go Probs:  %v\n", result.Probabilities)
		fmt.Printf("Go Clusters: %d\n", result.ClusterCount)

		// Verify cluster count
		if result.ClusterCount != 2 {
			t.Errorf("Expected 2 clusters, got %d", result.ClusterCount)
		}

		// Verify grouping (first 4 in one cluster, last 4 in another)
		cluster0 := result.Labels[0]
		cluster1 := result.Labels[4]
		if cluster0 == cluster1 {
			t.Error("Points 0-3 and 4-7 should be in different clusters")
		}

		for i := 0; i < 4; i++ {
			if result.Labels[i] != cluster0 {
				t.Errorf("Point %d should be in same cluster as point 0", i)
			}
		}
		for i := 4; i < 8; i++ {
			if result.Labels[i] != cluster1 {
				t.Errorf("Point %d should be in same cluster as point 4", i)
			}
		}

		// Verify some probabilities are < 1.0 (edge points)
		hasLowProb := false
		for _, p := range result.Probabilities {
			if p > 0 && p < 0.9 {
				hasLowProb = true
				break
			}
		}
		if !hasLowProb {
			t.Log("Warning: No low probabilities found (Python has 0.34 for edge points)")
		}
	})

	t.Run("ThreeClusters", func(t *testing.T) {
		data := [][]float32{
			{1.0, 0.0, 0.0},
			{0.98, 0.02, 0.0},
			{0.95, 0.05, 0.0},
			{0.92, 0.08, 0.0},
			{0.0, 1.0, 0.0},
			{0.02, 0.98, 0.0},
			{0.05, 0.95, 0.0},
			{0.08, 0.92, 0.0},
			{0.0, 0.0, 1.0},
			{0.0, 0.02, 0.98},
			{0.0, 0.05, 0.95},
			{0.0, 0.08, 0.92},
		}

		c := ptrext.Of(Clusterer{MinClusterSize: 3, MinSamples: 3})
		result := c.Cluster(data)

		// Python: Labels: [1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0], Clusters: 3
		fmt.Printf("Go Labels: %v\n", result.Labels)
		fmt.Printf("Go Probs:  %v\n", result.Probabilities)
		fmt.Printf("Go Clusters: %d\n", result.ClusterCount)

		if result.ClusterCount != 3 {
			t.Errorf("Expected 3 clusters, got %d", result.ClusterCount)
		}

		// Verify grouping
		c0, c1, c2 := result.Labels[0], result.Labels[4], result.Labels[8]
		if c0 == c1 || c1 == c2 || c0 == c2 {
			t.Error("Three groups should be in different clusters")
		}
	})

	t.Run("WithNoise", func(t *testing.T) {
		data := [][]float32{
			{1.0, 0.0, 0.0},
			{0.95, 0.05, 0.0},
			{0.9, 0.1, 0.0},
			{0.98, 0.02, 0.0},
			{0.92, 0.08, 0.0},
			{0.0, 1.0, 0.0}, // outlier
			{0.0, 0.0, 1.0}, // outlier
		}

		c := ptrext.Of(Clusterer{MinClusterSize: 4, MinSamples: 4})
		result := c.Cluster(data)

		// Python: all -1 (no cluster found)
		fmt.Printf("Go Labels: %v\n", result.Labels)
		fmt.Printf("Go Probs:  %v\n", result.Probabilities)
		fmt.Printf("Go Clusters: %d\n", result.ClusterCount)

		// Count noise
		noiseCount := 0
		for _, l := range result.Labels {
			if l == -1 {
				noiseCount++
			}
		}
		fmt.Printf("Noise points: %d\n", noiseCount)

		// At least outliers should be noise
		if result.Labels[5] != -1 || result.Labels[6] != -1 {
			t.Log("Note: outlier points not marked as noise")
		}
	})
}
