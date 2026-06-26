// ptrext:file-allow test-fixtures
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	enrichruntime "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
)

func TestParseGlobalArgs_ConfigFlag(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{
			name: "no args",
			args: nil,
			want: []string{},
		},
		{
			name: "config flag consumed",
			args: []string{"--config", "/tmp/test.yaml", "server"},
			want: []string{"server"},
		},
		{
			name: "config equals consumed",
			args: []string{"--config=/tmp/test.yaml", "server"},
			want: []string{"server"},
		},
		{
			name:    "config without value errors",
			args:    []string{"--config"},
			wantErr: true,
		},
		{
			name: "non-config args passed through",
			args: []string{"server", "--port", "8080"},
			want: []string{"server", "--port", "8080"},
		},
		{
			name: "multiple non-config args",
			args: []string{"keys", "issue", "--tenant", "demo"},
			want: []string{"keys", "issue", "--tenant", "demo"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGlobalArgs(tc.args)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseSince(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, got time.Time)
	}{
		{
			name:  "empty defaults to 30 days ago",
			input: "",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				diff := time.Since(got)
				require.InDelta(t, 30*24, diff.Hours(), 1)
			},
		},
		{
			name:  "date only",
			input: "2026-05-01",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				require.Equal(t, 2026, got.Year())
				require.Equal(t, time.May, got.Month())
				require.Equal(t, 1, got.Day())
			},
		},
		{
			name:  "RFC3339",
			input: "2026-05-01T12:30:00Z",
			check: func(t *testing.T, got time.Time) {
				t.Helper()
				require.Equal(t, 2026, got.Year())
				require.Equal(t, 12, got.Hour())
			},
		},
		{
			name:    "invalid format errors",
			input:   "not-a-date",
			wantErr: true,
		},
		{
			name:    "partial date errors",
			input:   "2026-05",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSince(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestSafePercent(t *testing.T) {
	tests := []struct {
		name  string
		n     int64
		total int64
		want  float64
	}{
		{"zero total", 5, 0, 0.0},
		{"50 percent", 50, 100, 50.0},
		{"100 percent", 100, 100, 100.0},
		{"zero numerator", 0, 100, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safePercent(tc.n, tc.total)
			require.NotNil(t, got)
			require.InDelta(t, tc.want, *got, 0.001) // ptrext:allow test-deref
		})
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]string{"c": "C", "a": "A", "b": "B"})
	require.Equal(t, []string{"a", "b", "c"}, got)
}

func TestSortedKeysEmpty(t *testing.T) {
	got := sortedKeys(map[string]string{})
	require.Empty(t, got)
}

func TestRunTenant_NoArgs(t *testing.T) {
	err := runTenant(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage")
}

func TestRunTenant_UnknownSubcommand(t *testing.T) {
	err := runTenant([]string{"delete"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestRunOutbox_NoArgs(t *testing.T) {
	err := runOutbox(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage")
}

func TestRunOutbox_UnknownSubcommand(t *testing.T) {
	err := runOutbox([]string{"delete"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestRunEmbed_NoArgs(t *testing.T) {
	err := runEmbed(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage")
}

func TestRunEmbed_UnknownSubcommand(t *testing.T) {
	err := runEmbed([]string{"destroy"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestEnrichmentRuntimeBootstrapSpec(t *testing.T) {
	cfg := &config.Config{
		EnricherQueueLen:    100,
		EnricherWorkers:     4,
		EnricherBatch:       10,
		EnricherBatchWindow: 5 * time.Second,
		EnricherInterval:    30 * time.Second,
		EnricherLLMMaxQPS:   5.0,
		EnricherLLMBurst:    2,
	}

	got := enrichmentRuntimeBootstrapSpec(cfg)

	require.Equal(t, int32(100), got.QueueLen)
	require.Equal(t, int32(4), got.Workers)
	require.Equal(t, int32(10), got.BatchSize)
	require.Equal(t, 5*time.Second, got.BatchWindow)
	require.Equal(t, 30*time.Second, got.SweepInterval)
	require.True(t, got.LLMRateLimitEnabled)
	require.InDelta(t, 5.0, got.LLMMaxQPS, 0.001)
	require.Equal(t, int32(2), got.LLMBurst)
}

func TestEnrichmentRuntimeBootstrapSpec_RateLimitDisabled(t *testing.T) {
	cfg := &config.Config{
		EnricherLLMMaxQPS: 0,
	}

	got := enrichmentRuntimeBootstrapSpec(cfg)
	require.False(t, got.LLMRateLimitEnabled)
}

func TestEnrichmentRuntimeBootstrapVersion(t *testing.T) {
	cfg := &config.Config{
		EnricherQueueLen:    100,
		EnricherWorkers:     4,
		EnricherBatch:       10,
		EnricherBatchWindow: 5 * time.Second,
		EnricherInterval:    30 * time.Second,
		EnricherLLMMaxQPS:   5.0,
		EnricherLLMBurst:    2,
	}

	got := enrichmentRuntimeBootstrapVersion(cfg)
	require.Contains(t, got, "enricher:")
	require.Contains(t, got, "100")
	require.Contains(t, got, "4")
}

// Verify the enrichmentRuntime Spec type matches what we construct.
// This is a compile-time conformance check.
var _ enrichruntime.Spec = enrichmentRuntimeBootstrapSpec(&config.Config{})

func TestWriteReport_Stdout(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	err := writeReport("# Test Report\n\nContent here.", "")
	require.NoError(t, err)
}

func TestWriteReport_ToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	err := writeReport("# Test Report\n\nContent here.", path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# Test Report\n\nContent here.", string(data))
}

func TestWriteReport_BadPath(t *testing.T) {
	err := writeReport("content", "/nonexistent/dir/file.md")
	require.Error(t, err)
}

func TestOpenOutput_Stdout(t *testing.T) {
	w, cleanup, err := openOutput("")
	require.NoError(t, err)
	require.NotNil(t, w)
	cleanup()
}

func TestOpenOutput_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	w, cleanup, err := openOutput(path)
	require.NoError(t, err)
	require.NotNil(t, w)

	_, writeErr := w.Write([]byte("test"))
	require.NoError(t, writeErr)
	cleanup()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "test", string(data))
}

func TestOpenOutput_BadPath(t *testing.T) {
	_, _, err := openOutput("/nonexistent/dir/file.txt")
	require.Error(t, err)
}

func TestPrintUsage_NoPanic(t *testing.T) {
	// No t.Parallel(): writes to os.Stderr.
	printUsage()
}

func TestBuildRateLimiter(t *testing.T) {
	// No t.Parallel(): logext writes to global slog handler.
	cfg := &config.Config{
		RateLimitPerMinute: 60,
		RateLimitBurst:     10,
		RateLimitDisabled:  false,
	}
	limiter := buildRateLimiter(cfg)
	require.NotNil(t, limiter)
}

func TestBuildRateLimiter_Disabled(t *testing.T) {
	// No t.Parallel(): logext writes to global slog handler.
	cfg := &config.Config{
		RateLimitDisabled: true,
	}
	limiter := buildRateLimiter(cfg)
	require.NotNil(t, limiter)
}

func TestBuildPerKeyRateLimiter(t *testing.T) {
	cfg := &config.Config{
		RateLimitDisabled: false,
	}
	limiter := buildPerKeyRateLimiter(cfg)
	require.NotNil(t, limiter)
}

func TestLogDatabasePingRetry_NoPanic(t *testing.T) {
	// No t.Parallel(): logext writes to global slog handler.
	ctx := context.Background()
	logDatabasePingRetry(ctx, 1, 30*time.Second, 2*time.Second, errors.New("connection refused"))
	logDatabasePingRetry(ctx, 2, 30*time.Second, 2*time.Second, errors.New("connection refused"))
	logDatabasePingRetry(ctx, 5, 30*time.Second, 2*time.Second, errors.New("connection refused"))
	logDatabasePingRetry(ctx, 10, 30*time.Second, 2*time.Second, errors.New("connection refused"))
}

func TestRunLLM_NoArgs(t *testing.T) {
	err := runLLM(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage")
}

func TestRunLLM_UnknownResource(t *testing.T) {
	err := runLLM([]string{"unknown", "list"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestRunLLMChannels_UnknownVerb(t *testing.T) {
	err := runLLMChannels([]string{"unknown"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

func TestOptionalStringFlag(t *testing.T) {
	var f optionalStringFlag
	require.Equal(t, "", f.String())

	require.NoError(t, f.Set("hello"))
	require.Equal(t, "hello", f.String())
	require.True(t, f.set)

	p := f.ptr()
	require.NotNil(t, p)
	require.Equal(t, "hello", *p) // ptrext:allow test-deref
}

func TestOptionalStringFlag_NilPtr(t *testing.T) {
	var f optionalStringFlag
	p := f.ptr()
	require.Nil(t, p)
}

func TestOptionalStringFlag_NilReceiver(t *testing.T) {
	var f *optionalStringFlag
	p := f.ptr()
	require.Nil(t, p)
}
