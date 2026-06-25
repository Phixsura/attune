// SPDX-License-Identifier: Apache-2.0

package sliceutil

import (
	"testing"
)

func BenchmarkMap(b *testing.B) {
	input := make([]int, 1000)
	for i := range input {
		input[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Map(input, func(n int) int { return n * 2 })
	}
}

func BenchmarkFilter(b *testing.B) {
	input := make([]int, 1000)
	for i := range input {
		input[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Filter(input, func(n int) bool { return n%2 == 0 })
	}
}

func BenchmarkContains(b *testing.B) {
	input := make([]int, 1000)
	for i := range input {
		input[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Contains(input, 500)
	}
}

func BenchmarkUnique(b *testing.B) {
	input := make([]int, 1000)
	for i := range input {
		input[i] = i % 100 // 100 unique values
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Unique(input)
	}
}

func BenchmarkChunk(b *testing.B) {
	input := make([]int, 1000)
	for i := range input {
		input[i] = i
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Chunk(input, 100)
	}
}
