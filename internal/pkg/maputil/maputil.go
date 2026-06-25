// SPDX-License-Identifier: Apache-2.0

// Package maputil provides generic map utilities.
package maputil

// Keys returns all keys from a map.
func Keys[K comparable, V any](m map[K]V) []K {
	if m == nil {
		return nil
	}
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Values returns all values from a map.
func Values[K comparable, V any](m map[K]V) []V {
	if m == nil {
		return nil
	}
	vals := make([]V, 0, len(m))
	for _, v := range m {
		vals = append(vals, v)
	}
	return vals
}

// Merge merges multiple maps into one. Later maps override earlier ones.
func Merge[K comparable, V any](maps ...map[K]V) map[K]V {
	result := make(map[K]V)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// FilterKeys returns a new map containing only keys that satisfy the predicate.
func FilterKeys[K comparable, V any](m map[K]V, f func(K) bool) map[K]V {
	if m == nil {
		return nil
	}
	result := make(map[K]V)
	for k, v := range m {
		if f(k) {
			result[k] = v
		}
	}
	return result
}

// FilterValues returns a new map containing only entries whose values satisfy the predicate.
func FilterValues[K comparable, V any](m map[K]V, f func(V) bool) map[K]V {
	if m == nil {
		return nil
	}
	result := make(map[K]V)
	for k, v := range m {
		if f(v) {
			result[k] = v
		}
	}
	return result
}

// GetOr returns the value for key if present, otherwise returns defaultVal.
func GetOr[K comparable, V any](m map[K]V, key K, defaultVal V) V {
	if v, ok := m[key]; ok {
		return v
	}
	return defaultVal
}
