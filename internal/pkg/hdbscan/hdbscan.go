// SPDX-License-Identifier: Apache-2.0

// Package hdbscan implements the HDBSCAN clustering algorithm.
//
// HDBSCAN (Hierarchical Density-Based Spatial Clustering of Applications with Noise)
// automatically discovers clusters of varying densities without requiring the number
// of clusters to be specified upfront.
//
// Reference: Campello, R.J.G.B., Moulavi, D., Sander, J. (2013).
// "Density-Based Clustering Based on Hierarchical Density Estimates"
//
// This implementation follows scikit-learn's HDBSCAN with:
//   - Condensed tree with 4-case merging
//   - Per-point stability accumulation
//   - Proper probability calculation: min(λ_point, λ_death) / λ_death
package hdbscan

import (
	"math"
	"sort"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Clusterer performs HDBSCAN clustering.
type Clusterer struct {
	// MinClusterSize is the minimum number of points to form a cluster.
	MinClusterSize int

	// MinSamples controls the conservativeness of clustering.
	// Defaults to MinClusterSize if not set.
	MinSamples int

	// ReduceDims enables PCA dimensionality reduction before clustering.
	ReduceDims int
}

// Result contains the clustering output.
type Result struct {
	Labels        []int
	Probabilities []float64
	ClusterCount  int
}

// condensedEdge represents an edge in the condensed tree.
// Matches scikit-learn: (parent, child, lambda_val, child_size)
type condensedEdge struct {
	parent    int
	child     int
	lambda    float64
	childSize int
}

// Cluster runs HDBSCAN on the given data points.
func (c *Clusterer) Cluster(data [][]float32) Result {
	n := len(data)
	if n == 0 {
		return Result{}
	}
	if n == 1 {
		return Result{Labels: []int{0}, Probabilities: []float64{1.0}, ClusterCount: 1}
	}

	workData := data
	if c.ReduceDims > 0 && len(data[0]) > c.ReduceDims {
		pca := ptrext.Of(PCA{Components: c.ReduceDims, MaxIter: 100})
		workData = pca.Reduce(data)
	}

	minSamples := c.MinSamples
	if minSamples == 0 {
		minSamples = c.MinClusterSize
	}

	// Step 1-2: Core distances and MST
	coreDistances := computeCoreDistances(workData, minSamples)
	mst := buildMST(workData, coreDistances)

	// Step 3: Build single-linkage dendrogram
	dendro := buildDendrogram(mst, n)

	// Step 4: Build condensed tree with 4-case merging
	condensed, clusterBirth := buildCondensedTree(dendro, n, c.MinClusterSize)

	// Step 5: Compute stability and extract clusters
	stability := computeStability(condensed, clusterBirth, n)
	selected, clusterDeath := selectClustersEOM(condensed, stability, n)

	// Step 6: Assign labels and compute probabilities
	labels, probs := assignLabelsAndProbs(condensed, selected, clusterBirth, clusterDeath, n)

	clusterSet := make(map[int]bool)
	for _, l := range labels {
		if l >= 0 {
			clusterSet[l] = true
		}
	}

	return Result{
		Labels:        labels,
		Probabilities: probs,
		ClusterCount:  len(clusterSet),
	}
}

type edge struct {
	i, j   int
	weight float64
}

// dendroNode represents a node in the single-linkage dendrogram
type dendroNode struct {
	left, right int
	distance    float64
	size        int
}

func computeCoreDistances(data [][]float32, k int) []float64 {
	n := len(data)
	coreDistances := make([]float64, n)
	if n <= 1 {
		return coreDistances
	}

	// Reuse a single buffer across iterations to avoid n allocations.
	buf := make([]float64, 0, n-1)

	for i := 0; i < n; i++ {
		buf = buf[:0] // reset length, keep capacity
		for j := 0; j < n; j++ {
			if i != j {
				buf = append(buf, cosineDistance(data[i], data[j]))
			}
		}
		if len(buf) == 0 {
			continue
		}
		sort.Float64s(buf)
		idx := k - 1
		if idx >= len(buf) {
			idx = len(buf) - 1
		}
		if idx < 0 {
			idx = 0
		}
		coreDistances[i] = buf[idx]
	}
	return coreDistances
}

func mutualReachabilityDistance(i, j int, data [][]float32, coreDistances []float64) float64 {
	dist := cosineDistance(data[i], data[j])
	return math.Max(math.Max(coreDistances[i], coreDistances[j]), dist)
}

func buildMST(data [][]float32, coreDistances []float64) []edge {
	n := len(data)
	if n <= 1 {
		return nil
	}

	inMST := make([]bool, n)
	minEdge := make([]float64, n)
	parent := make([]int, n)

	for i := range minEdge {
		minEdge[i] = math.Inf(1)
		parent[i] = -1
	}
	minEdge[0] = 0

	edges := make([]edge, 0, n-1)

	for count := 0; count < n; count++ {
		u := -1
		minVal := math.Inf(1)
		for v := 0; v < n; v++ {
			if !inMST[v] && minEdge[v] < minVal {
				minVal = minEdge[v]
				u = v
			}
		}
		if u == -1 {
			break
		}
		inMST[u] = true
		if parent[u] != -1 {
			edges = append(edges, edge{i: parent[u], j: u, weight: minEdge[u]})
		}
		for v := 0; v < n; v++ {
			if !inMST[v] {
				dist := mutualReachabilityDistance(u, v, data, coreDistances)
				if dist < minEdge[v] {
					minEdge[v] = dist
					parent[v] = u
				}
			}
		}
	}
	return edges
}

// buildDendrogram builds single-linkage hierarchy from MST
func buildDendrogram(mst []edge, n int) []dendroNode {
	if len(mst) == 0 {
		return nil
	}

	sortedMST := make([]edge, len(mst))
	copy(sortedMST, mst)
	sort.Slice(sortedMST, func(i, j int) bool {
		return sortedMST[i].weight < sortedMST[j].weight
	})

	uf := newUnionFind(2 * n)
	size := make([]int, 2*n)
	for i := 0; i < n; i++ {
		size[i] = 1
	}

	dendro := make([]dendroNode, 0, n-1)
	nextCluster := n

	for _, e := range sortedMST {
		rootI := uf.find(e.i)
		rootJ := uf.find(e.j)
		if rootI == rootJ {
			continue
		}

		sizeI := size[rootI]
		sizeJ := size[rootJ]

		dendro = append(dendro, dendroNode{
			left:     rootI,
			right:    rootJ,
			distance: e.weight,
			size:     sizeI + sizeJ,
		})

		uf.union(rootI, rootJ)
		newRoot := uf.find(rootI)
		size[newRoot] = sizeI + sizeJ
		size[nextCluster] = sizeI + sizeJ

		// Map the merged cluster to nextCluster
		uf.parent[newRoot] = nextCluster
		uf.parent[nextCluster] = nextCluster

		nextCluster++
	}

	return dendro
}

// buildCondensedTree implements scikit-learn's 4-case condensation
func buildCondensedTree(dendro []dendroNode, n, minClusterSize int) ([]condensedEdge, map[int]float64) {
	if len(dendro) == 0 {
		return nil, nil
	}

	// Build lookup: for each node, find its dendrogram entry
	nodeToEntry := make(map[int]int) // node -> dendro index
	for i, d := range dendro {
		clusterID := n + i
		nodeToEntry[d.left] = i
		nodeToEntry[d.right] = i
		_ = clusterID
	}

	// Compute subtree sizes
	subtreeSize := make(map[int]int)
	for i := 0; i < n; i++ {
		subtreeSize[i] = 1
	}
	for i, d := range dendro {
		clusterID := n + i
		subtreeSize[clusterID] = d.size
	}

	// Track which cluster each node currently belongs to (for relabeling)
	relabel := make(map[int]int)
	rootCluster := n + len(dendro) - 1
	relabel[rootCluster] = rootCluster

	// Track birth lambda for each cluster
	clusterBirth := make(map[int]float64)
	clusterBirth[rootCluster] = 0 // Root is born at λ=0

	var condensed []condensedEdge
	ignore := make(map[int]bool)

	// Process dendrogram in reverse order (from root to leaves)
	for i := len(dendro) - 1; i >= 0; i-- {
		clusterID := n + i
		if ignore[clusterID] {
			continue
		}

		d := dendro[i]
		lambda := toLambda(d.distance)

		leftSize := subtreeSize[d.left]
		rightSize := subtreeSize[d.right]

		parentCluster := relabel[clusterID]
		if parentCluster == 0 && clusterID != rootCluster {
			parentCluster = rootCluster
		}

		// Record birth for this cluster if not set
		if _, ok := clusterBirth[parentCluster]; !ok {
			clusterBirth[parentCluster] = lambda
		}

		// 4-case logic from scikit-learn
		if leftSize >= minClusterSize && rightSize >= minClusterSize {
			// Case 1: Both children large enough -> both become new clusters
			relabel[d.left] = d.left
			relabel[d.right] = d.right
			clusterBirth[d.left] = lambda
			clusterBirth[d.right] = lambda

			condensed = append(condensed, condensedEdge{
				parent: parentCluster, child: d.left, lambda: lambda, childSize: leftSize,
			})
			condensed = append(condensed, condensedEdge{
				parent: parentCluster, child: d.right, lambda: lambda, childSize: rightSize,
			})

		} else if leftSize < minClusterSize && rightSize < minClusterSize {
			// Case 2: Both too small -> all points fall out to parent
			collectLeaves(&condensed, d.left, parentCluster, lambda, n, dendro, ignore)  // ptrext:allow out-param
			collectLeaves(&condensed, d.right, parentCluster, lambda, n, dendro, ignore) // ptrext:allow out-param
			ignore[d.left] = true
			ignore[d.right] = true

		} else if leftSize < minClusterSize {
			// Case 3: Left too small -> merge right with parent, left points fall out
			relabel[d.right] = parentCluster
			collectLeaves(&condensed, d.left, parentCluster, lambda, n, dendro, ignore) // ptrext:allow out-param
			ignore[d.left] = true

		} else {
			// Case 4: Right too small -> merge left with parent, right points fall out
			relabel[d.left] = parentCluster
			collectLeaves(&condensed, d.right, parentCluster, lambda, n, dendro, ignore) // ptrext:allow out-param
			ignore[d.right] = true
		}
	}

	return condensed, clusterBirth
}

// collectLeaves adds edges for all leaf points in a subtree
// Each point gets the lambda from when it actually merged in the dendrogram
func collectLeaves(condensed *[]condensedEdge, node, parent int, lambda float64, n int, dendro []dendroNode, ignore map[int]bool) {
	if node < n {
		// It's a point - use the provided lambda (from its merge level)
		*condensed = append(*condensed, condensedEdge{ // ptrext:allow slice-accumulator
			parent: parent, child: node, lambda: lambda, childSize: 1,
		})
		return
	}

	// It's a cluster - recurse to its children with their own lambda values
	idx := node - n
	if idx < 0 || idx >= len(dendro) {
		return
	}
	d := dendro[idx]
	// Each child gets the lambda from when this cluster was formed
	childLambda := toLambda(d.distance)
	collectLeaves(condensed, d.left, parent, childLambda, n, dendro, ignore)
	collectLeaves(condensed, d.right, parent, childLambda, n, dendro, ignore)
	ignore[node] = true
}

func toLambda(distance float64) float64 {
	if distance <= 0 {
		return math.Inf(1)
	}
	return 1.0 / distance
}

// computeStability: S(C) = Σ (λ_child - λ_birth(C)) × child_size
func computeStability(condensed []condensedEdge, clusterBirth map[int]float64, n int) map[int]float64 {
	stability := make(map[int]float64)

	for _, e := range condensed {
		if e.parent < n {
			continue
		}
		birth := clusterBirth[e.parent]
		if math.IsInf(e.lambda, 1) || math.IsInf(birth, 1) {
			continue
		}
		delta := e.lambda - birth
		if delta > 0 {
			stability[e.parent] += delta * float64(e.childSize)
		}
	}

	return stability
}

// selectClustersEOM implements Excess of Mass cluster selection
// allowSingleCluster=false by default (matches scikit-learn)
func selectClustersEOM(condensed []condensedEdge, stability map[int]float64, n int) (map[int]bool, map[int]float64) {
	// Find cluster children relationships
	children := make(map[int][]int)
	for _, e := range condensed {
		if e.child >= n { // child is a cluster
			children[e.parent] = append(children[e.parent], e.child)
		}
	}

	// Find all clusters
	allClusters := make(map[int]bool)
	for c := range stability {
		allClusters[c] = true
	}

	// Find root cluster (the one that's not a child of any other)
	childSet := make(map[int]bool)
	for _, e := range condensed {
		if e.child >= n {
			childSet[e.child] = true
		}
	}
	var root int
	for c := range allClusters {
		if !childSet[c] {
			root = c
			break
		}
	}

	// Process in bottom-up order (leaves first) using topological sort
	// Build in-degree map (count of cluster children)
	inDegree := make(map[int]int)
	for c := range allClusters {
		inDegree[c] = len(children[c])
	}

	// Start with leaf clusters (no cluster children)
	var queue []int
	for c := range allClusters {
		if inDegree[c] == 0 {
			queue = append(queue, c)
		}
	}

	// Topological sort: process leaves first, then parents
	clusters := make([]int, 0, len(allClusters))
	parent := make(map[int]int) // child -> parent
	for p, ch := range children {
		for _, c := range ch {
			parent[c] = p
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		clusters = append(clusters, node)

		// Decrement parent's in-degree
		if p, ok := parent[node]; ok {
			inDegree[p]--
			if inDegree[p] == 0 {
				queue = append(queue, p)
			}
		}
	}

	isCluster := make(map[int]bool)
	for c := range allClusters {
		isCluster[c] = true
	}

	// EOM selection - process all clusters including root
	for _, node := range clusters {
		childStab := 0.0
		for _, child := range children[node] {
			childStab += stability[child]
		}

		if childStab > stability[node] {
			isCluster[node] = false
			stability[node] = childStab
		} else {
			// Node wins - mark all descendants as non-clusters
			var markDescendants func(int)
			markDescendants = func(nd int) {
				for _, ch := range children[nd] {
					isCluster[ch] = false
					markDescendants(ch)
				}
			}
			markDescendants(node)
		}
	}

	// Handle allow_single_cluster=false (scikit-learn default)
	// Count how many non-root clusters are selected
	nonRootSelected := 0
	for c, selected := range isCluster {
		if selected && c != root {
			nonRootSelected++
		}
	}

	// If only root is selected, all points become noise
	if nonRootSelected == 0 {
		isCluster[root] = false
	} else {
		// Root is never selected when we have child clusters
		isCluster[root] = false
	}

	// Compute death lambda for each cluster (max lambda of children falling out)
	clusterDeath := make(map[int]float64)
	for _, e := range condensed {
		if e.parent >= n {
			if e.lambda > clusterDeath[e.parent] {
				clusterDeath[e.parent] = e.lambda
			}
		}
	}

	return isCluster, clusterDeath
}

// assignLabelsAndProbs assigns labels and computes per-point probabilities
// Probability formula: (λ_point - λ_birth) / (λ_death - λ_birth)
func assignLabelsAndProbs(condensed []condensedEdge, selected map[int]bool, clusterBirth, clusterDeath map[int]float64, n int) ([]int, []float64) {
	labels := make([]int, n)
	probs := make([]float64, n)
	for i := range labels {
		labels[i] = -1
	}

	// Map selected clusters to sequential labels
	clusterLabels := make(map[int]int)
	labelID := 0
	for c := range selected {
		if selected[c] {
			clusterLabels[c] = labelID
			labelID++
		}
	}

	// For each point, find which selected cluster it belongs to
	// and record its lambda value
	pointCluster := make(map[int]int)
	pointLambda := make(map[int]float64)

	for _, e := range condensed {
		if e.child >= n {
			continue // Skip cluster edges
		}

		// Walk up to find selected cluster
		cluster := e.parent
		lambda := e.lambda

		// If this cluster is not selected, find its selected ancestor
		for cluster >= n && !selected[cluster] {
			found := false
			for _, ce := range condensed {
				if ce.child == cluster {
					cluster = ce.parent
					found = true
					break
				}
			}
			if !found {
				cluster = -1
				break
			}
		}

		if cluster >= n && selected[cluster] {
			pointCluster[e.child] = cluster
			pointLambda[e.child] = lambda
		}
	}

	// Assign labels and compute probabilities
	for point, cluster := range pointCluster {
		if label, ok := clusterLabels[cluster]; ok {
			labels[point] = label

			// Probability = (λ_point - λ_birth) / (λ_death - λ_birth)
			birth := clusterBirth[cluster]
			death := clusterDeath[cluster]
			lambda := pointLambda[point]

			denominator := death - birth
			if denominator <= 0 || math.IsInf(death, 1) || math.IsInf(birth, 1) {
				probs[point] = 1.0
			} else if math.IsInf(lambda, 1) {
				probs[point] = 1.0
			} else {
				numerator := lambda - birth
				if numerator < 0 {
					numerator = 0
				}
				probs[point] = math.Min(numerator/denominator, 1.0)
			}
		}
	}

	return labels, probs
}

type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	parent := make([]int, n)
	rank := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	return ptrext.Of(unionFind{parent: parent, rank: rank})
}

func (uf *unionFind) find(x int) int {
	if x >= len(uf.parent) {
		return x
	}
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *unionFind) union(x, y int) {
	px, py := uf.find(x), uf.find(y)
	if px == py {
		return
	}
	if uf.rank[px] < uf.rank[py] {
		px, py = py, px
	}
	uf.parent[py] = px
	if uf.rank[px] == uf.rank[py] {
		uf.rank[px]++
	}
}

func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1.0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - dot/(math.Sqrt(normA)*math.Sqrt(normB))
}
