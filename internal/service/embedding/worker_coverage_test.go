// ptrext:file-allow test-fixtures
// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/service/llmrouter"
)

func TestNewWorker_OwnerUniqueness(t *testing.T) {
	t.Parallel()

	w1 := NewWorker(nil, nil, nil, nil)
	w2 := NewWorker(nil, nil, nil, nil)

	require.NotEmpty(t, w1.owner)
	require.NotEmpty(t, w2.owner)
	require.NotEqual(t, w1.owner, w2.owner, "each worker must have a unique owner")
}

func TestNewWorker_OwnerPrefix(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)

	require.True(t, strings.HasPrefix(w.owner, "embedding-"),
		"owner %q must start with \"embedding-\"", w.owner)

	// The suffix after the prefix should be a 36-char UUID.
	suffix := strings.TrimPrefix(w.owner, "embedding-")
	require.Len(t, suffix, 36, "UUID portion must be 36 characters")
}

func TestNewWorker_DrainNotNil(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)

	require.NotNil(t, w.drain, "drain must be initialised by NewWorker")
}

func TestWorker_Configure_ThresholdBoundary(t *testing.T) {
	t.Parallel()

	t.Run("zero keeps default", func(t *testing.T) {
		t.Parallel()

		w := NewWorker(nil, nil, nil, nil)
		defaultThreshold := w.threshold

		w.Configure(0, 0, 0, 0.0)

		require.Equal(t, defaultThreshold, w.threshold,
			"threshold=0.0 must not change the default")
	})

	t.Run("smallest positive updates", func(t *testing.T) {
		t.Parallel()

		w := NewWorker(nil, nil, nil, nil)
		w.Configure(0, 0, 0, 0.01)

		require.InDelta(t, 0.01, w.threshold, 1e-9,
			"threshold=0.01 must be accepted")
	})

	t.Run("maximum boundary updates", func(t *testing.T) {
		t.Parallel()

		w := NewWorker(nil, nil, nil, nil)
		w.Configure(0, 0, 0, 1.0)

		require.InDelta(t, 1.0, w.threshold, 1e-9,
			"threshold=1.0 must be accepted")
	})

	t.Run("negative keeps default", func(t *testing.T) {
		t.Parallel()

		w := NewWorker(nil, nil, nil, nil)
		defaultThreshold := w.threshold

		w.Configure(0, 0, 0, -0.5)

		require.Equal(t, defaultThreshold, w.threshold,
			"negative threshold must not change the default")
	})
}

func TestWorker_Configure_MultipleCalls(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)

	// First configure.
	w.Configure(10*time.Second, 512, 3, 0.90)
	require.Equal(t, 10*time.Second, w.pollInterval)
	require.Equal(t, 512, w.dimensions)
	require.Equal(t, 3, w.maxAttempts)
	require.InDelta(t, 0.90, w.threshold, 1e-9)

	// Second configure overrides the first.
	w.Configure(2*time.Second, 1024, 10, 0.70)
	require.Equal(t, 2*time.Second, w.pollInterval,
		"second Configure must override pollInterval")
	require.Equal(t, 1024, w.dimensions,
		"second Configure must override dimensions")
	require.Equal(t, 10, w.maxAttempts,
		"second Configure must override maxAttempts")
	require.InDelta(t, 0.70, w.threshold, 1e-9,
		"second Configure must override threshold")
}

func TestHeartbeatIntervalValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, 90*time.Second, heartbeatInterval,
		"heartbeatInterval must be 90s (staleDuration/3 to staleDuration/2 rule of thumb)")
}

func TestDrainTimeoutValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, 30*time.Second, drainTimeout,
		"drainTimeout must be 30s for graceful shutdown")
}

func TestWorker_WriteAudit_NilRepo(t *testing.T) {
	t.Parallel()

	w := Worker{auditRepo: nil}
	task := ptrext.Of(embedding.Task{
		ID:         42,
		FeedbackID: 100,
		TenantID:   "tenant-abc",
	})
	resp := llmrouter.EmbeddingResponse{}
	start := time.Now()

	// Must not panic when auditRepo is nil.
	require.NotPanics(t, func() {
		w.writeAudit(context.Background(), task, resp, start)
	})
}

func TestWorker_GenerateLabel_NilRouter(t *testing.T) {
	t.Parallel()

	w := Worker{router: nil}
	titles := []string{"Login broken", "Cannot sign in", "Auth fails"}

	label, err := w.generateLabel(context.Background(), "tenant-xyz", titles)

	require.Error(t, err)
	require.Contains(t, err.Error(), "llm router not configured")
	require.Empty(t, label, "label must be empty when router is nil")
}

func TestWorker_RecencyDaysDefault(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)

	require.Equal(t, 30, w.recencyDays, "default recencyDays must be 30")
}

func TestWorker_FieldsAfterConfigure(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)
	origStaleDuration := w.staleDuration
	origRecencyDays := w.recencyDays
	origOwner := w.owner

	// Configure only touches pollInterval, dimensions, maxAttempts, threshold.
	w.Configure(1*time.Second, 128, 2, 0.50)

	require.Equal(t, origStaleDuration, w.staleDuration,
		"Configure must not alter staleDuration")
	require.Equal(t, origRecencyDays, w.recencyDays,
		"Configure must not alter recencyDays")
	require.Equal(t, origOwner, w.owner,
		"Configure must not alter owner")
	require.NotNil(t, w.drain,
		"Configure must not nil out drain")

	// Verify the configured fields did change.
	require.Equal(t, 1*time.Second, w.pollInterval)
	require.Equal(t, 128, w.dimensions)
	require.Equal(t, 2, w.maxAttempts)
	require.InDelta(t, 0.50, w.threshold, 1e-9)
}

func TestNewWorker_AllDefaults(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)

	require.Equal(t, 5*time.Second, w.pollInterval, "default pollInterval")
	require.Equal(t, 5*time.Minute, w.staleDuration, "default staleDuration")
	require.Equal(t, 5, w.maxAttempts, "default maxAttempts")
	require.Equal(t, 256, w.dimensions, "default dimensions")
	require.InDelta(t, 0.85, w.threshold, 1e-9, "default threshold")
	require.Equal(t, 30, w.recencyDays, "default recencyDays")

	// Nil dependencies are passed through.
	require.Nil(t, w.taskRepo)
	require.Nil(t, w.router)
	require.Nil(t, w.llmClient)
	require.Nil(t, w.auditRepo)
}
