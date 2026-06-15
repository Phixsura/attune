// SPDX-License-Identifier: Apache-2.0

package hdbscan

import (
	"math"
)

// PCA performs Principal Component Analysis for dimensionality reduction.
// This is a simplified implementation using power iteration for the top-k
// principal components, suitable for reducing high-dimensional embeddings
// before HDBSCAN clustering.
type PCA struct {
	// Components is the number of principal components to keep.
	Components int

	// MaxIter is the maximum iterations for power iteration (default: 100).
	MaxIter int
}

const convergenceThreshold = 0.9999

// centerData subtracts mean from each dimension and returns centered float64 data.
func centerData(data [][]float32) [][]float64 {
	n, d := len(data), len(data[0])
	mean := make([]float64, d)
	for i := 0; i < n; i++ {
		for j := 0; j < d; j++ {
			mean[j] += float64(data[i][j])
		}
	}
	for j := 0; j < d; j++ {
		mean[j] /= float64(n)
	}
	centered := make([][]float64, n)
	for i := 0; i < n; i++ {
		centered[i] = make([]float64, d)
		for j := 0; j < d; j++ {
			centered[i][j] = float64(data[i][j]) - mean[j]
		}
	}
	return centered
}

// powerIterationStep performs one step of power iteration: v' = X^T (X v).
func powerIterationStep(centered [][]float64, v []float64) []float64 {
	n, d := len(centered), len(v)
	u := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := 0; j < d; j++ {
			u[i] += centered[i][j] * v[j]
		}
	}
	newV := make([]float64, d)
	for j := 0; j < d; j++ {
		for i := 0; i < n; i++ {
			newV[j] += centered[i][j] * u[i]
		}
	}
	return newV
}

// orthogonalize removes components in the direction of previous vectors.
func orthogonalize(v []float64, previous [][]float64) {
	for _, prev := range previous {
		dot := dotProduct(v, prev)
		for j := range v {
			v[j] -= dot * prev[j]
		}
	}
}

// projectData projects centered data onto principal components.
func projectData(centered [][]float64, components [][]float64) [][]float32 {
	n, k := len(centered), len(components)
	result := make([][]float32, n)
	for i := 0; i < n; i++ {
		result[i] = make([]float32, k)
		for c := 0; c < k; c++ {
			var proj float64
			for j := range components[c] {
				proj += centered[i][j] * components[c][j]
			}
			result[i][c] = float32(proj)
		}
	}
	return result
}

// Reduce projects data onto the top-k principal components.
// Input: n points x d dimensions. Output: n points x k dimensions.
func (p *PCA) Reduce(data [][]float32) [][]float32 {
	if len(data) == 0 || p.Components <= 0 || p.Components >= len(data[0]) {
		return data
	}

	maxIter := p.MaxIter
	if maxIter == 0 {
		maxIter = 100
	}

	centered := centerData(data)
	d := len(data[0])
	components := make([][]float64, p.Components)

	for k := 0; k < p.Components; k++ {
		v := initVector(d)
		for iter := 0; iter < maxIter; iter++ {
			newV := powerIterationStep(centered, v)
			orthogonalize(newV, components[:k])
			normalize(newV)
			if dotProduct(v, newV) > convergenceThreshold {
				v = newV
				break
			}
			v = newV
		}
		components[k] = v
	}

	return projectData(centered, components)
}

// initVector creates an initial vector for power iteration.
func initVector(d int) []float64 {
	v := make([]float64, d)
	for j := 0; j < d; j++ {
		v[j] = float64(j%7+1) / 10.0
	}
	normalize(v)
	return v
}

func dotProduct(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func normalize(v []float64) {
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm > 1e-10 {
		for i := range v {
			v[i] /= norm
		}
	}
}
