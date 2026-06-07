// Package ptrext — small generic helpers for working with pointers.
//
// Go's lack of an "addressof literal" operator means turning a literal
// into *T is a two-line dance:
//
//	v := "name"
//	x.Field = &v
//
// ptrext.Of("name") collapses that into one expression. Companion
// helpers cover the common nil-safe read/copy/map cases that show up
// all over JSON-marshalling and proto-shaped code.
//
// Zero deps — stdlib only. Pure functions, no allocations beyond the
// pointers they're documented to return.
package ptrext

import "cmp"

// Of returns &v. Useful for taking the address of a literal:
//
//	x.Name = ptrext.Of("Alice")
//
// The returned pointer always references a fresh local; callers can
// mutate *p without affecting the original argument.
func Of[T any](v T) *T { return &v }

// OfNotZero returns &v when v is non-zero, otherwise nil.
// Handy when a proto optional field should be omitted on default.
func OfNotZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// OfPositive returns &v when v > 0, otherwise nil.
func OfPositive[T cmp.Ordered](v T) *T {
	var zero T
	if v <= zero {
		return nil
	}
	return &v
}

// Indirect dereferences p, returning the zero value of T when p is nil.
func Indirect[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// IndirectOr dereferences p, returning fallback when p is nil.
func IndirectOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// IsNil reports whether p == nil.
func IsNil[T any](p *T) bool { return p == nil }

// IsNotNil reports whether p != nil.
func IsNotNil[T any](p *T) bool { return p != nil }

// IsNilOrZero reports whether p is nil or *p equals T's zero value.
func IsNilOrZero[T comparable](p *T) bool {
	if p == nil {
		return true
	}
	var zero T
	return *p == zero
}

// HasZeroValue reports whether p is non-nil and *p equals T's zero value.
// (Distinct from IsNilOrZero: nil → false here.)
func HasZeroValue[T comparable](p *T) bool {
	if p == nil {
		return false
	}
	var zero T
	return *p == zero
}

// HasNonZeroValue reports whether p is non-nil and *p is non-zero.
func HasNonZeroValue[T comparable](p *T) bool {
	if p == nil {
		return false
	}
	var zero T
	return *p != zero
}

// Equal reports whether two pointers point at equal values.
// nil == nil, and nil != non-nil.
func Equal[T comparable](a, b *T) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// EqualTo reports whether *p equals v. nil → false.
func EqualTo[T comparable](p *T, v T) bool {
	if p == nil {
		return false
	}
	return *p == v
}

// Clone returns a fresh pointer to a shallow copy of *p.
// Returns nil when p is nil.
func Clone[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// CloneBy returns a fresh pointer whose value is clone(*p).
// Returns nil when p is nil. Use this when *p is itself a pointer
// and a deeper copy is required.
func CloneBy[T any](p *T, clone func(T) T) *T {
	if p == nil {
		return nil
	}
	v := clone(*p)
	return &v
}

// Map applies f to *p when p is non-nil, returning a pointer to the
// result. Returns nil when p is nil — f is never called in that case.
func Map[T, R any](p *T, f func(T) R) *R {
	if p == nil {
		return nil
	}
	r := f(*p)
	return &r
}
