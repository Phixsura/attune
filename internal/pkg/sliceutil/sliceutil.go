// SPDX-License-Identifier: Apache-2.0

// Package sliceutil provides generic slice utilities.
package sliceutil

// Map applies a function to each element of a slice and returns a new slice.
func Map[T, U any](s []T, f func(T) U) []U {
	if s == nil {
		return nil
	}
	result := make([]U, len(s))
	for i, v := range s {
		result[i] = f(v)
	}
	return result
}

// Filter returns a new slice containing only elements that satisfy the predicate.
func Filter[T any](s []T, f func(T) bool) []T {
	if s == nil {
		return nil
	}
	result := make([]T, 0, len(s)/2) // estimate half will pass
	for _, v := range s {
		if f(v) {
			result = append(result, v)
		}
	}
	return result
}

// Contains reports whether v is present in s.
func Contains[T comparable](s []T, v T) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Unique returns a new slice with duplicates removed, preserving order.
func Unique[T comparable](s []T) []T {
	if s == nil {
		return nil
	}
	seen := make(map[T]struct{}, len(s))
	result := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// Chunk splits a slice into chunks of the given size.
func Chunk[T any](s []T, size int) [][]T {
	if size <= 0 || len(s) == 0 {
		return nil
	}
	chunks := make([][]T, 0, (len(s)+size-1)/size)
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// First returns the first element of a slice, or zero value if empty.
func First[T any](s []T) T {
	if len(s) == 0 {
		var zero T
		return zero
	}
	return s[0]
}

// Last returns the last element of a slice, or zero value if empty.
func Last[T any](s []T) T {
	if len(s) == 0 {
		var zero T
		return zero
	}
	return s[len(s)-1]
}
