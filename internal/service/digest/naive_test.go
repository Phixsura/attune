// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/repo/feedback"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty string", "", 10, ""},
		{"shorter than n", "hello", 10, "hello"},
		{"exactly n", "hello", 5, "hello"},
		{"longer than n", "hello world", 5, "hello"},
		{"zero n", "hello", 0, ""},
		{"CJK string", "你好世界", 2, "你好"},
		{"mixed", "hello你好", 6, "hello你"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateRunes(tt.s, tt.n); got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestStripCodeFence(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"plain json", `{"a":1}`, `{"a":1}`},
		{"with json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"with plain fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"whitespace around", "  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
		{"no trailing fence", "```json\n{\"a\":1}", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripCodeFence(tt.s); got != tt.want {
				t.Errorf("stripCodeFence() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNaiveSystem(t *testing.T) {
	t.Run("default system prompt", func(t *testing.T) {
		got := naiveSystem("")
		if !strings.Contains(got, "3 themes") {
			t.Errorf("naiveSystem('') should mention 3 themes, got %q", got)
		}
	})

	t.Run("custom prompt override", func(t *testing.T) {
		got := naiveSystem("Custom instruction")
		if got != "Custom instruction" {
			t.Errorf("naiveSystem('Custom instruction') = %q, want 'Custom instruction'", got)
		}
	})

	t.Run("whitespace only uses default", func(t *testing.T) {
		got := naiveSystem("   ")
		if !strings.Contains(got, "3 themes") {
			t.Errorf("naiveSystem with whitespace should use default, got %q", got)
		}
	})
}

func TestNaivePrompt(t *testing.T) {
	rows := []feedback.DigestFeedbackRow{
		{ID: 1, Title: "Login failed", Rationale: "User cannot access"},
		{ID: 2, Title: "Crash on load"},
	}
	got := naivePrompt(rows)

	if !strings.Contains(got, "1: Login failed") {
		t.Errorf("prompt should contain id and title")
	}
	if !strings.Contains(got, "User cannot access") {
		t.Errorf("prompt should contain rationale")
	}
	if !strings.Contains(got, "2: Crash on load") {
		t.Errorf("prompt should contain second row")
	}
}

func TestNaiveSchema(t *testing.T) {
	schema := naiveSchema()
	if schema == nil {
		t.Fatal("naiveSchema returned nil")
	}
	if schema.Name != "digest_themes_v1" {
		t.Errorf("schema name = %q, want digest_themes_v1", schema.Name)
	}
	if schema.Schema == nil {
		t.Error("schema.Schema is nil")
	}
}

func TestBuildNaiveTheme(t *testing.T) {
	valid := map[int64]string{
		1: "First",
		2: "Second",
		3: "Third",
	}

	t.Run("valid ids", func(t *testing.T) {
		theme := buildNaiveTheme("Test Theme", []float64{1, 2, 3}, valid)
		if theme.Title != "Test Theme" {
			t.Errorf("Title = %q, want 'Test Theme'", theme.Title)
		}
		if theme.Count != 3 {
			t.Errorf("Count = %d, want 3", theme.Count)
		}
	})

	t.Run("dedupe repeated ids", func(t *testing.T) {
		theme := buildNaiveTheme("Test", []float64{1, 1, 2, 2}, valid)
		if theme.Count != 2 {
			t.Errorf("Count = %d, want 2 (deduped)", theme.Count)
		}
	})

	t.Run("skip invalid ids", func(t *testing.T) {
		theme := buildNaiveTheme("Test", []float64{1, 99, 100}, valid)
		if theme.Count != 1 {
			t.Errorf("Count = %d, want 1 (invalid skipped)", theme.Count)
		}
	})

	t.Run("limits examples", func(t *testing.T) {
		manyValid := make(map[int64]string)
		var manyIDs []float64
		for i := int64(1); i <= 20; i++ {
			manyValid[i] = "title"
			manyIDs = append(manyIDs, float64(i))
		}
		theme := buildNaiveTheme("Test", manyIDs, manyValid)
		if len(theme.ExampleIDs) > examplesPerTheme {
			t.Errorf("ExampleIDs = %d, want <= %d", len(theme.ExampleIDs), examplesPerTheme)
		}
	})

	t.Run("empty title trimmed", func(t *testing.T) {
		theme := buildNaiveTheme("  Spaces  ", []float64{1}, valid)
		if theme.Title != "Spaces" {
			t.Errorf("Title = %q, want 'Spaces' (trimmed)", theme.Title)
		}
	})
}

func TestNewNaiveNamer(t *testing.T) {
	namer := newNaiveNamer(nil)
	if namer == nil {
		t.Fatal("newNaiveNamer returned nil")
	}
}
