package feedback

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalAttrs_NilBecomesEmptyObject(t *testing.T) {
	got, err := marshalAttrs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}" {
		t.Errorf("nil → {}: got %q", string(got))
	}
}

func TestMarshalAttrs_RoundTrip(t *testing.T) {
	in := map[string]any{"type": "bug", "labels": []string{"a", "b"}}
	out, err := marshalAttrs(in)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back["type"] != "bug" {
		t.Errorf("type lost: %v", back["type"])
	}
	labels := back["labels"].([]any)
	if len(labels) != 2 {
		t.Errorf("labels count: %v", labels)
	}
}

func TestContainmentClause_Single(t *testing.T) {
	got, err := containmentClause(AttrFilter{Dim: "severity", Value: "critical", Multi: false})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(got, &back)
	if back["severity"] != "critical" {
		t.Errorf("single shape: %s", string(got))
	}
}

func TestContainmentClause_Multi(t *testing.T) {
	got, err := containmentClause(AttrFilter{Dim: "labels", Value: "payment", Multi: true})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(got, &back)
	arr := back["labels"].([]any)
	if len(arr) != 1 || arr[0] != "payment" {
		t.Errorf("multi shape: %s", string(got))
	}
}

func TestContainmentClause_UnicodeEscaped(t *testing.T) {
	// Dim names are validated lowercase-Latin, but Taxonomy Values can be
	// any Unicode. JSONB containment payloads must escape correctly.
	got, err := containmentClause(AttrFilter{Dim: "type", Value: "缺陷", Multi: false})
	if err != nil {
		t.Fatal(err)
	}
	// encoding/json doesn't escape non-ASCII by default — the raw runes
	// pass through, which Postgres @> accepts as UTF-8.
	if !strings.Contains(string(got), "缺陷") {
		t.Errorf("unicode value should survive: %s", string(got))
	}
}

func TestContainmentClause_NoQuoteInjection(t *testing.T) {
	// JSON encoding escapes embedded quotes — proves we are NOT
	// concatenating strings (which would risk SQL injection).
	got, err := containmentClause(AttrFilter{Dim: "type", Value: `evil"value`, Multi: false})
	if err != nil {
		t.Fatal(err)
	}
	// Must contain escaped quote, not raw.
	if !strings.Contains(string(got), `\"`) {
		t.Errorf("embedded quote should be escaped: %s", string(got))
	}
}
