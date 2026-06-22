// SPDX-License-Identifier: Apache-2.0

package enrich

import (
	"testing"
)

func TestAttrsSizeBytes(t *testing.T) {
	tests := []struct {
		name    string
		a       map[string]any
		wantMin int
		wantMax int
	}{
		{"nil map", nil, 0, 0},
		{"empty map", map[string]any{}, 2, 2}, // "{}"
		{"single key", map[string]any{"key": "value"}, 10, 20},
		{"nested map", map[string]any{
			"outer": map[string]any{"inner": "v"},
		}, 15, 30},
		{"array value", map[string]any{
			"tags": []string{"a", "b", "c"},
		}, 15, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := attrsSizeBytes(tt.a)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("attrsSizeBytes() = %v, want in range [%v, %v]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestAttrsSizeBytes_NonEmpty(t *testing.T) {
	a := map[string]any{"key": "value"}
	got := attrsSizeBytes(a)
	if got <= 0 {
		t.Errorf("attrsSizeBytes for non-empty map returned %v, want > 0", got)
	}
}

func TestNewEnricher(t *testing.T) {
	e := NewEnricher(nil, nil, "test-model")
	if e == nil {
		t.Fatal("NewEnricher returned nil")
	}
	if e.model != "test-model" {
		t.Errorf("model = %q, want test-model", e.model)
	}
}

func TestEnricher_SetOutbox(t *testing.T) {
	e := NewEnricher(nil, nil, "")
	if e.outbox != nil {
		t.Error("outbox should be nil initially")
	}
	if e.targets != nil {
		t.Error("targets should be nil initially")
	}
	// SetOutbox with nil repos is a no-op
	e.SetOutbox(nil, nil)
	if e.outbox != nil || e.targets != nil {
		t.Error("SetOutbox with nils should not set values")
	}
}

func TestEnricher_SetEmbeddingTask(t *testing.T) {
	e := NewEnricher(nil, nil, "")
	if e.embeddingTask != nil {
		t.Error("embeddingTask should be nil initially")
	}
	e.SetEmbeddingTask(nil)
	if e.embeddingTask != nil {
		t.Error("SetEmbeddingTask with nil should not set value")
	}
}

func TestEnricher_SetDraftTask(t *testing.T) {
	e := NewEnricher(nil, nil, "")
	if e.draftTask != nil {
		t.Error("draftTask should be nil initially")
	}
	e.SetDraftTask(nil)
	if e.draftTask != nil {
		t.Error("SetDraftTask with nil should not set value")
	}
}

func TestClassifyResult_Structure(t *testing.T) {
	// Just verify the public ClassifyResult type is usable.
	r := ClassifyResult{}
	_ = r.Enriched
	_ = r.DropDiagnostics
}
