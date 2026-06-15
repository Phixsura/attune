package hdbscan

import (
	"fmt"
	"testing"
)

// TestSelectClustersEOM_ChildrenWin tests the EOM algorithm with a condensed tree where:
// - Root has stability 10
// - Child A has stability 4
// - Child B has stability 8
// - A+B = 12 > 10, so children should win
func TestSelectClustersEOM_ChildrenWin(t *testing.T) {
	// Let's simulate with n=10 points (so clusters start at index 10)
	// Cluster IDs:
	//   Root = 12
	//   Child A = 10
	//   Child B = 11
	n := 10

	// Build condensed tree edges
	// Root (12) has two cluster children: A (10) and B (11)
	condensed := []condensedEdge{
		// Root 12 spawns cluster A (10)
		{parent: 12, child: 10, lambda: 1.0, childSize: 5},
		// Root 12 spawns cluster B (11)
		{parent: 12, child: 11, lambda: 1.0, childSize: 5},
		// Points falling out of A
		{parent: 10, child: 0, lambda: 2.0, childSize: 1},
		{parent: 10, child: 1, lambda: 2.0, childSize: 1},
		{parent: 10, child: 2, lambda: 2.0, childSize: 1},
		{parent: 10, child: 3, lambda: 2.0, childSize: 1},
		{parent: 10, child: 4, lambda: 2.0, childSize: 1},
		// Points falling out of B
		{parent: 11, child: 5, lambda: 3.0, childSize: 1},
		{parent: 11, child: 6, lambda: 3.0, childSize: 1},
		{parent: 11, child: 7, lambda: 3.0, childSize: 1},
		{parent: 11, child: 8, lambda: 3.0, childSize: 1},
		{parent: 11, child: 9, lambda: 3.0, childSize: 1},
	}

	// Set up stability map matching the example
	// Root stability = 10
	// Child A stability = 4
	// Child B stability = 8
	stability := map[int]float64{
		12: 10.0, // Root
		10: 4.0,  // Child A
		11: 8.0,  // Child B
	}

	fmt.Println("=== Input ===")
	fmt.Printf("Condensed edges: %+v\n\n", condensed)
	fmt.Printf("Stability: %+v\n\n", stability)

	// Run EOM selection
	selected, clusterDeath := selectClustersEOM(condensed, stability, n)

	fmt.Println("=== Output ===")
	fmt.Printf("Selected clusters: %+v\n", selected)
	fmt.Printf("Cluster death lambdas: %+v\n\n", clusterDeath)

	// Verify children array is built correctly
	// The children map should show: 12 -> [10, 11]
	fmt.Println("=== Trace: Children Map ===")
	children := make(map[int][]int)
	for _, e := range condensed {
		if e.child >= n { // child is a cluster
			children[e.parent] = append(children[e.parent], e.child)
		}
	}
	fmt.Printf("children: %+v\n\n", children)

	// Expected: children[12] = [10, 11]
	if len(children[12]) != 2 {
		t.Errorf("Expected root to have 2 children, got %d", len(children[12]))
	}

	// Verify: children A+B (4+8=12) > root (10), so children should win
	// This means:
	// - Child A (10) should be selected
	// - Child B (11) should be selected
	// - Root (12) should NOT be selected
	fmt.Println("=== Verification ===")
	fmt.Printf("Root (12) selected? %v (expected: false)\n", selected[12])
	fmt.Printf("Child A (10) selected? %v (expected: true)\n", selected[10])
	fmt.Printf("Child B (11) selected? %v (expected: true)\n", selected[11])

	if selected[12] {
		t.Error("Root (12) should NOT be selected")
	}
	if !selected[10] {
		t.Error("Child A (10) SHOULD be selected")
	}
	if !selected[11] {
		t.Error("Child B (11) SHOULD be selected")
	}
}

// TestSelectClustersEOM_ParentWins tests when parent stability beats children
func TestSelectClustersEOM_ParentWins(t *testing.T) {
	// Root has stability 20
	// Child A has stability 4
	// Child B has stability 8
	// A+B = 12 < 20, so parent should win
	n := 10

	condensed := []condensedEdge{
		{parent: 12, child: 10, lambda: 1.0, childSize: 5},
		{parent: 12, child: 11, lambda: 1.0, childSize: 5},
		// Points falling out
		{parent: 10, child: 0, lambda: 2.0, childSize: 1},
		{parent: 10, child: 1, lambda: 2.0, childSize: 1},
		{parent: 10, child: 2, lambda: 2.0, childSize: 1},
		{parent: 10, child: 3, lambda: 2.0, childSize: 1},
		{parent: 10, child: 4, lambda: 2.0, childSize: 1},
		{parent: 11, child: 5, lambda: 3.0, childSize: 1},
		{parent: 11, child: 6, lambda: 3.0, childSize: 1},
		{parent: 11, child: 7, lambda: 3.0, childSize: 1},
		{parent: 11, child: 8, lambda: 3.0, childSize: 1},
		{parent: 11, child: 9, lambda: 3.0, childSize: 1},
	}

	stability := map[int]float64{
		12: 20.0, // Root - higher than children sum
		10: 4.0,  // Child A
		11: 8.0,  // Child B
	}

	fmt.Println("\n=== Parent Wins Test ===")
	fmt.Printf("Stability: %+v\n", stability)

	selected, _ := selectClustersEOM(condensed, stability, n)

	fmt.Printf("Selected clusters: %+v\n", selected)

	// With allow_single_cluster=false (default), if only root wins,
	// ALL clusters become noise
	// Expected: none selected because root is the only winner but allow_single_cluster=false
	if selected[12] {
		t.Error("Root should not be selected due to allow_single_cluster=false")
	}
	if selected[10] {
		t.Error("Child A should not be selected - parent won, then became noise")
	}
	if selected[11] {
		t.Error("Child B should not be selected - parent won, then became noise")
	}

	// Count selected
	count := 0
	for _, sel := range selected {
		if sel {
			count++
		}
	}
	fmt.Printf("Total selected: %d (expected: 0 due to allow_single_cluster=false)\n", count)
	if count != 0 {
		t.Errorf("Expected 0 clusters selected (all noise), got %d", count)
	}
}

// TestSelectClustersEOM_BottomUpTraversal verifies bottom-up traversal order
func TestSelectClustersEOM_BottomUpTraversal(t *testing.T) {
	// 3-level tree:
	//       Root (14)
	//      /        \
	//   Node (12)   Node (13)
	//   /    \        /    \
	//  10    11      (points)
	n := 10

	condensed := []condensedEdge{
		// Root 14 spawns clusters 12 and 13
		{parent: 14, child: 12, lambda: 0.5, childSize: 6},
		{parent: 14, child: 13, lambda: 0.5, childSize: 4},
		// Node 12 spawns clusters 10 and 11
		{parent: 12, child: 10, lambda: 1.0, childSize: 3},
		{parent: 12, child: 11, lambda: 1.0, childSize: 3},
		// Points falling out
		{parent: 10, child: 0, lambda: 2.0, childSize: 1},
		{parent: 10, child: 1, lambda: 2.0, childSize: 1},
		{parent: 10, child: 2, lambda: 2.0, childSize: 1},
		{parent: 11, child: 3, lambda: 2.0, childSize: 1},
		{parent: 11, child: 4, lambda: 2.0, childSize: 1},
		{parent: 11, child: 5, lambda: 2.0, childSize: 1},
		{parent: 13, child: 6, lambda: 2.0, childSize: 1},
		{parent: 13, child: 7, lambda: 2.0, childSize: 1},
		{parent: 13, child: 8, lambda: 2.0, childSize: 1},
		{parent: 13, child: 9, lambda: 2.0, childSize: 1},
	}

	// Stabilities designed so:
	// - 10 and 11 together (3+3=6) beat 12 (5)
	// - 13 (7) beats its children (none)
	// - Root 14 (2) loses to children
	stability := map[int]float64{
		14: 2.0, // Root
		12: 5.0, // Mid-level
		13: 7.0, // Mid-level (leaf cluster)
		10: 3.0, // Leaf
		11: 3.0, // Leaf
	}

	fmt.Println("\n=== 3-Level Tree Test ===")
	fmt.Printf("Stability: %+v\n", stability)

	selected, _ := selectClustersEOM(condensed, stability, n)

	fmt.Printf("Selected clusters: %+v\n", selected)

	// Expected:
	// - 10 and 11 selected (beat their parent 12)
	// - 13 selected (it's a leaf cluster)
	// - 12 NOT selected (children won)
	// - 14 NOT selected (children won, and even if only root, allow_single_cluster=false)
	fmt.Printf("Cluster 10 selected? %v (expected: true)\n", selected[10])
	fmt.Printf("Cluster 11 selected? %v (expected: true)\n", selected[11])
	fmt.Printf("Cluster 12 selected? %v (expected: false - children won)\n", selected[12])
	fmt.Printf("Cluster 13 selected? %v (expected: true)\n", selected[13])
	fmt.Printf("Cluster 14 selected? %v (expected: false - is root)\n", selected[14])

	if !selected[10] {
		t.Error("Cluster 10 should be selected")
	}
	if !selected[11] {
		t.Error("Cluster 11 should be selected")
	}
	if selected[12] {
		t.Error("Cluster 12 should NOT be selected (children won)")
	}
	if !selected[13] {
		t.Error("Cluster 13 should be selected")
	}
	if selected[14] {
		t.Error("Root 14 should NOT be selected")
	}
}
