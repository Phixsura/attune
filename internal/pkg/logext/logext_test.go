// ptrext:file-allow test slog handlers use &buf for writer setup
package logext

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestAsLogParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "null"},
		{"string", "hello", `"hello"`},
		{"int", 42, "42"},
		{"bool", true, "true"},
		{"slice", []int{1, 2, 3}, "[1,2,3]"},
		{"map", map[string]int{"a": 1}, `{"a":1}`},
		{"struct", struct {
			Name string `json:"name"`
		}{"test"}, `{"name":"test"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AsLogParam(tt.v); got != tt.want {
				t.Errorf("AsLogParam(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestAsLogParamMarshalError(t *testing.T) {
	t.Parallel()
	ch := make(chan int)
	got := AsLogParam(ch)
	if !strings.HasPrefix(got, "<marshal-err:") {
		t.Errorf("AsLogParam(chan) = %q, want prefix <marshal-err:", got)
	}
}

// TestLogFunctions tests Infof, Warnf, Errorf sequentially because they call
// slog.SetDefault which modifies global state.
func TestLogFunctions(t *testing.T) {
	// NOT parallel — these tests modify slog.SetDefault global state

	t.Run("Infof", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		slog.SetDefault(logger)

		Infof(context.Background(), "test %s %d", "message", 42)

		output := buf.String()
		if !strings.Contains(output, "test message 42") {
			t.Errorf("Infof output = %q, want to contain 'test message 42'", output)
		}
		if !strings.Contains(output, "INFO") {
			t.Errorf("Infof output = %q, want to contain 'INFO'", output)
		}
	})

	t.Run("Warnf", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		slog.SetDefault(logger)

		Warnf(context.Background(), "warning %s", "test")

		output := buf.String()
		if !strings.Contains(output, "warning test") {
			t.Errorf("Warnf output = %q, want to contain 'warning test'", output)
		}
		if !strings.Contains(output, "WARN") {
			t.Errorf("Warnf output = %q, want to contain 'WARN'", output)
		}
	})

	t.Run("Errorf", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		slog.SetDefault(logger)

		Errorf(context.Background(), "error %d", 500)

		output := buf.String()
		if !strings.Contains(output, "error 500") {
			t.Errorf("Errorf output = %q, want to contain 'error 500'", output)
		}
		if !strings.Contains(output, "ERROR") {
			t.Errorf("Errorf output = %q, want to contain 'ERROR'", output)
		}
	})
}
