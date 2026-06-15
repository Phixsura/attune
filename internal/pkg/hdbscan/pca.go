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

// Reduce projects data onto the top-k principal components.
// Input: n points x d dimensions. Output: n points x k dimensions.
func (p *PCA) Reduce(data [][]float32) [][]float32 {
	if len(data) == 0 || p.Components <= 0 {
		return data
	}

	n := len(data)
	d := len(data[0])

	// If target dims >= current dims, no reduction needed
	if p.Components >= d {
		return data
	}

	maxIter := p.MaxIter
	if maxIter == 0 {
		maxIter = 100
	}

	// Step 1: Center the data (subtract mean)
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

	// Step 2: Compute top-k principal components using power iteration
	// For small k, this is faster than full SVD
	components := make([][]float64, p.Components)

	const convergenceThreshold = 0.9999 // early stop when dot product exceeds this

	for k := 0; k < p.Components; k++ {
		// Initialize random-ish vector
		v := make([]float64, d)
		for j := 0; j < d; j++ {
			v[j] = float64(j%7+1) / 10.0
		}
		normalize(v)

		// Power iteration: v = (X^T X) v, repeated
		for iter := 0; iter < maxIter; iter++ {
			// u = X * v (n-dim)
			u := make([]float64, n)
			for i := 0; i < n; i++ {
				for j := 0; j < d; j++ {
					u[i] += centered[i][j] * v[j]
				}
			}

			// v = X^T * u (d-dim)
			newV := make([]float64, d)
			for j := 0; j < d; j++ {
				for i := 0; i < n; i++ {
					newV[j] += centered[i][j] * u[i]
				}
			}

			// Orthogonalize against previous components
			for prev := 0; prev < k; prev++ {
				dot := dotProduct(newV, components[prev])
				for j := 0; j < d; j++ {
					newV[j] -= dot * components[prev][j]
				}
			}

			normalize(newV)

			// Early convergence check: if direction barely changed, stop
			if dotProduct(v, newV) > convergenceThreshold {
				v = newV
				break
			}
			v = newV
		}

		components[k] = v
	}

	// Step 3: Project data onto principal components
	result := make([][]float32, n)
	for i := 0; i < n; i++ {
		result[i] = make([]float32, p.Components)
		for k := 0; k < p.Components; k++ {
			var proj float64
			for j := 0; j < d; j++ {
				proj += centered[i][j] * components[k][j]
			}
			result[i][k] = float32(proj)
		}
	}

	return result
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
