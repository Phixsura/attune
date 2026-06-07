package ptrext

import (
	"fmt"
	"strconv"
	"testing"
)

func TestOf(t *testing.T) {
	if got := *Of(543); got != 543 {
		t.Errorf("Of(543) = %d, want 543", got)
	}
	if got := *Of("Alice"); got != "Alice" {
		t.Errorf("Of(\"Alice\") = %q, want \"Alice\"", got)
	}
	if got := **Of(Of("Alice")); got != "Alice" {
		t.Errorf("**Of(Of(\"Alice\")) = %q, want \"Alice\"", got)
	}

	// Of always returns a fresh address — mutating *p must not touch the source.
	v := 1
	p := Of(v)
	if p == &v {
		t.Errorf("Of(v) returned the same address as &v; should be a copy")
	}
	*p = 2
	if v != 1 {
		t.Errorf("mutating *Of(v) affected v: got %d, want 1", v)
	}
	if *p != 2 {
		t.Errorf("*p = %d after assignment, want 2", *p)
	}
}

func TestOfNotZero(t *testing.T) {
	if got := *OfNotZero(543); got != 543 {
		t.Errorf("OfNotZero(543) = %d, want 543", got)
	}
	if got := *OfNotZero("Alice"); got != "Alice" {
		t.Errorf("OfNotZero(\"Alice\") = %q, want \"Alice\"", got)
	}

	if OfNotZero(0) != nil {
		t.Errorf("OfNotZero(0) = non-nil, want nil")
	}
	if OfNotZero("") != nil {
		t.Errorf("OfNotZero(\"\") = non-nil, want nil")
	}
}

func TestOfPositive(t *testing.T) {
	if got := *OfPositive(543); got != 543 {
		t.Errorf("OfPositive(543) = %d, want 543", got)
	}
	if got := *OfPositive(1.23); got != 1.23 {
		t.Errorf("OfPositive(1.23) = %v, want 1.23", got)
	}

	if OfPositive(0) != nil {
		t.Errorf("OfPositive(0) = non-nil, want nil")
	}
	if OfPositive(-1) != nil {
		t.Errorf("OfPositive(-1) = non-nil, want nil")
	}
	if OfPositive(-1.23) != nil {
		t.Errorf("OfPositive(-1.23) = non-nil, want nil")
	}
}

func TestIndirect(t *testing.T) {
	if got := Indirect(Of(543)); got != 543 {
		t.Errorf("Indirect(Of(543)) = %d, want 543", got)
	}
	if got := Indirect(Of("Alice")); got != "Alice" {
		t.Errorf("Indirect(Of(\"Alice\")) = %q, want \"Alice\"", got)
	}
	if got := Indirect[int](nil); got != 0 {
		t.Errorf("Indirect[int](nil) = %d, want 0", got)
	}
}

func TestIndirectOr(t *testing.T) {
	if got := IndirectOr(Of("Alice"), "Bob"); got != "Alice" {
		t.Errorf("IndirectOr(Of(\"Alice\"), \"Bob\") = %q, want \"Alice\"", got)
	}
	if got := IndirectOr(nil, "Bob"); got != "Bob" {
		t.Errorf("IndirectOr(nil, \"Bob\") = %q, want \"Bob\"", got)
	}
}

func TestIsNil(t *testing.T) {
	if IsNil(Of(1)) {
		t.Errorf("IsNil(Of(1)) = true, want false")
	}
	if !IsNil[int](nil) {
		t.Errorf("IsNil[int](nil) = false, want true")
	}
}

func TestIsNotNil(t *testing.T) {
	if !IsNotNil(Of(1)) {
		t.Errorf("IsNotNil(Of(1)) = false, want true")
	}
	if IsNotNil[int](nil) {
		t.Errorf("IsNotNil[int](nil) = true, want false")
	}
}

func TestIsNilOrZero(t *testing.T) {
	if IsNilOrZero(Of(1)) {
		t.Errorf("IsNilOrZero(Of(1)) = true, want false")
	}
	if IsNilOrZero(Of("Alice")) {
		t.Errorf("IsNilOrZero(Of(\"Alice\")) = true, want false")
	}
	if !IsNilOrZero(Of(0)) {
		t.Errorf("IsNilOrZero(Of(0)) = false, want true")
	}
	if !IsNilOrZero(Of("")) {
		t.Errorf("IsNilOrZero(Of(\"\")) = false, want true")
	}
	if !IsNilOrZero[int](nil) {
		t.Errorf("IsNilOrZero[int](nil) = false, want true")
	}
}

func TestEqual(t *testing.T) {
	p := Of(1)
	if !Equal(p, p) {
		t.Errorf("Equal(p, p) = false, want true")
	}
	if !Equal(Of(1), Of(1)) {
		t.Errorf("Equal(Of(1), Of(1)) = false, want true")
	}
	if Equal(Of(1), Of(2)) {
		t.Errorf("Equal(Of(1), Of(2)) = true, want false")
	}
	if Equal(Of(1), nil) {
		t.Errorf("Equal(Of(1), nil) = true, want false")
	}
	if Equal(nil, Of(1)) {
		t.Errorf("Equal(nil, Of(1)) = true, want false")
	}
	if !Equal[string](nil, nil) {
		t.Errorf("Equal[string](nil, nil) = false, want true")
	}
}

func TestEqualTo(t *testing.T) {
	if !EqualTo(Of(1), 1) {
		t.Errorf("EqualTo(Of(1), 1) = false, want true")
	}
	if EqualTo(Of(2), 1) {
		t.Errorf("EqualTo(Of(2), 1) = true, want false")
	}
	if EqualTo[int](nil, 0) {
		t.Errorf("EqualTo[int](nil, 0) = true, want false")
	}
}

func TestClone(t *testing.T) {
	if Clone((*int)(nil)) != nil {
		t.Errorf("Clone(nil) = non-nil, want nil")
	}

	v := 1
	c := Clone(&v)
	if c == &v {
		t.Errorf("Clone(&v) returned the same address; should be a copy")
	}
	if *c != v {
		t.Errorf("Clone(&v) = %d, want %d", *c, v)
	}
}

func TestCloneBy(t *testing.T) {
	if CloneBy((**int)(nil), Clone[int]) != nil {
		t.Errorf("CloneBy(nil, _) = non-nil, want nil")
	}

	// Outer pointer is fresh, inner pointer is fresh too (deep clone).
	src := Of(1)
	dst := CloneBy(&src, Clone[int])
	if dst == &src {
		t.Errorf("CloneBy returned the same outer address")
	}
	if *dst == src {
		t.Errorf("CloneBy did not clone the inner pointer")
	}
	if **dst != *src {
		t.Errorf("CloneBy mismatched inner value: %d vs %d", **dst, *src)
	}
}

func TestMap(t *testing.T) {
	i := 1
	got := Map(&i, strconv.Itoa)
	if got == nil || *got != "1" {
		t.Errorf("Map(&1, Itoa) = %v, want pointer to \"1\"", got)
	}
	if Map[int, string](nil, strconv.Itoa) != nil {
		t.Errorf("Map(nil, _) = non-nil, want nil")
	}

	// nil pointer must not invoke f.
	called := false
	_ = Map[int, string](nil, func(int) string {
		called = true
		return ""
	})
	if called {
		t.Errorf("Map invoked f on nil input")
	}
}

func TestHasZeroValue(t *testing.T) {
	zeroInt := 0
	zeroString := ""
	zeroBool := false
	if !HasZeroValue(&zeroInt) || !HasZeroValue(&zeroString) || !HasZeroValue(&zeroBool) {
		t.Errorf("HasZeroValue on zero-valued pointer returned false")
	}
	if !HasZeroValue(Of(0)) || !HasZeroValue(Of("")) {
		t.Errorf("HasZeroValue(Of(zero)) returned false")
	}

	nonZeroInt := 1
	if HasZeroValue(&nonZeroInt) || HasZeroValue(Of("hello")) {
		t.Errorf("HasZeroValue on non-zero pointer returned true")
	}

	if HasZeroValue[int](nil) {
		t.Errorf("HasZeroValue(nil) = true, want false")
	}
}

func TestHasNonZeroValue(t *testing.T) {
	nonZeroInt := 1
	if !HasNonZeroValue(&nonZeroInt) || !HasNonZeroValue(Of("hello")) {
		t.Errorf("HasNonZeroValue on non-zero pointer returned false")
	}

	zeroInt := 0
	if HasNonZeroValue(&zeroInt) || HasNonZeroValue(Of(0)) || HasNonZeroValue(Of("")) {
		t.Errorf("HasNonZeroValue on zero-valued pointer returned true")
	}

	if HasNonZeroValue[int](nil) {
		t.Errorf("HasNonZeroValue(nil) = true, want false")
	}
}

func Example() {
	a := Of(1)
	fmt.Println(Indirect(a))

	b := OfNotZero(1)
	fmt.Println(IsNotNil(b))
	fmt.Println(IndirectOr(b, 2))
	fmt.Println(Indirect(Map(b, strconv.Itoa)))

	c := OfNotZero(0)
	fmt.Println(c)
	fmt.Println(IsNil(c))
	fmt.Println(IndirectOr(c, 2))

	// Output:
	// 1
	// true
	// 1
	// 1
	// <nil>
	// true
	// 2
}

// Big is held by reference in BenchmarkIndirect to confirm Indirect's
// nil branch doesn't allocate a zero-valued large struct on the heap.
type Big struct {
	Foo [200]string
	Bar int
}

func BenchmarkIndirect(b *testing.B) {
	var p *Big
	var v Big
	for i := 0; i < b.N; i++ {
		v = Indirect(p)
	}
	_ = v
}
