// SPDX-License-Identifier: Apache-2.0

package canonicaljson

import (
	"encoding/json"
	"testing"
)

func TestMarshal_SortedKeys(t *testing.T) {
	input := map[string]any{
		"z": 1,
		"a": 2,
		"m": 3,
	}
	got, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":2,"m":3,"z":1}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMarshal_NestedSorting(t *testing.T) {
	input := map[string]any{
		"b": map[string]any{
			"y": 1,
			"x": 2,
		},
		"a": "first",
	}
	got, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":"first","b":{"x":2,"y":1}}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMarshal_CompactNoWhitespace(t *testing.T) {
	input := map[string]any{
		"a": []any{1, 2, 3},
	}
	got, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[1,2,3]}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMarshal_Numbers(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"zero", 0, "0"},
		{"integer", 42, "42"},
		{"negative integer", -7, "-7"},
		{"decimal", 3.14, "3.14"},
		{"no trailing zeros", 1.50, "1.5"},
		{"negative decimal", -0.5, "-0.5"},
		{"large integer", 1e6, "1000000"},
		{"ES6 fixed 1e20", 1e20, "100000000000000000000"},
		{"ES6 fixed 1.5e20", 1.5e20, "150000000000000000000"},
		{"ES6 exponential 1e21", 1e21, "1e+21"},
		{"ES6 no pad 1e-7", 1e-7, "1e-7"},
		{"ES6 no pad 1e-8", 1e-8, "1e-8"},
		{"ES6 fixed 1e-6", 1e-6, "0.000001"},
		{"ES6 boundary 1e-5", 1e-5, "0.00001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMarshal_StringEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", `"hello"`},
		{"quotes", `a"b`, `"a\"b"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"newline", "a\nb", `"a\nb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"control char", string([]byte{'a', 0x01, 'b'}), "\"a\\u0001b\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMarshal_NullAndBool(t *testing.T) {
	got, err := Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "null" {
		t.Errorf("nil: got %s, want null", got)
	}

	got, err = Marshal(true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "true" {
		t.Errorf("true: got %s, want true", got)
	}

	got, err = Marshal(false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "false" {
		t.Errorf("false: got %s, want false", got)
	}
}

func TestMarshalEntry_RawJSON(t *testing.T) {
	raw := json.RawMessage(`{"z":1,"a":2}`)
	got, err := MarshalEntry(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":2,"z":1}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMarshalEntry_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := MarshalEntry(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMarshal_CustomStruct(t *testing.T) {
	t.Parallel()
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	got, err := Marshal(point{X: 1, Y: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"x":1,"y":2}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMarshal_StringBackspace(t *testing.T) {
	t.Parallel()
	got, err := Marshal("a\bb")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"a\bb"` {
		t.Errorf("got %s, want %q", got, `a\bb`)
	}
}

func TestMarshal_StringFormFeed(t *testing.T) {
	t.Parallel()
	got, err := Marshal("a\fb")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"a\fb"` {
		t.Errorf("got %s, want %q", got, `a\fb`)
	}
}

func TestMarshal_StringCarriageReturn(t *testing.T) {
	t.Parallel()
	got, err := Marshal("a\rb")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"a\rb"` {
		t.Errorf("got %s, want %q", got, `a\rb`)
	}
}

func TestMarshal_EmptyArray(t *testing.T) {
	t.Parallel()
	got, err := Marshal([]any{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "[]" {
		t.Errorf("got %s, want []", got)
	}
}

func TestMarshal_EmptyObject(t *testing.T) {
	t.Parallel()
	got, err := Marshal(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}" {
		t.Errorf("got %s, want {}", got)
	}
}

func TestMarshal_NestedArrays(t *testing.T) {
	t.Parallel()
	got, err := Marshal([]any{[]any{1.0, 2.0}, []any{3.0}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "[[1,2],[3]]" {
		t.Errorf("got %s, want [[1,2],[3]]", got)
	}
}

func TestMarshal_JSONNumber(t *testing.T) {
	t.Parallel()
	got, err := Marshal(json.Number("3.14"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "3.14" {
		t.Errorf("got %s, want 3.14", got)
	}
}

func TestMarshal_JSONNumberInvalid(t *testing.T) {
	t.Parallel()
	got, err := Marshal(json.Number("not-a-number"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "not-a-number" {
		t.Errorf("got %s, want not-a-number", got)
	}
}

func TestMarshal_UnsupportedChannelType(t *testing.T) {
	t.Parallel()
	_, err := Marshal(make(chan int))
	if err == nil {
		t.Fatal("channel should fail")
	}
}

func TestMarshal_MixedArray(t *testing.T) {
	t.Parallel()
	got, err := Marshal([]any{"hello", float64(42), true, nil})
	if err != nil {
		t.Fatal(err)
	}
	want := `["hello",42,true,null]`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMarshal_NegativeExponential(t *testing.T) {
	t.Parallel()
	got, err := Marshal(-1.5e-8)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "-1.5e-8" {
		t.Errorf("got %s, want -1.5e-8", got)
	}
}

func TestMarshal_Deterministic(t *testing.T) {
	input := map[string]any{
		"id":     float64(42),
		"action": "api_key.create",
		"actor":  map[string]any{"type": "admin", "id": "u1"},
		"meta":   nil,
	}
	first, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		got, err := Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(first) {
			t.Fatalf("non-deterministic at iteration %d: %s vs %s", i, got, first)
		}
	}
}
