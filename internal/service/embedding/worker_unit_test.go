// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	embeddingRepo "github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/service/llmrouter"
)

func TestNewWorker_OwnerUnique(t *testing.T) {
	t.Parallel()
	w1 := NewWorker(nil, nil, nil, nil)
	w2 := NewWorker(nil, nil, nil, nil)
	if w1.owner == w2.owner {
		t.Fatalf("two workers should have distinct owners, both = %q", w1.owner)
	}
}

func TestNewWorker_RecencyDays(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil)
	if w.recencyDays != 30 {
		t.Fatalf("recencyDays = %d, want 30", w.recencyDays)
	}
}

func TestConfigure_OnlyPositiveThresholdApplied(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil)
	original := w.threshold
	w.Configure(0, 0, 0, 0.95)
	if w.threshold != 0.95 {
		t.Fatalf("threshold = %f, want 0.95", w.threshold)
	}
	w.Configure(0, 0, 0, 0)
	if w.threshold != 0.95 {
		t.Fatalf("threshold should stay 0.95 after zero input, got %f", w.threshold)
	}
	_ = original
}

func TestConfigure_LargeValues(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil)
	w.Configure(1*time.Minute, 4096, 100, 0.99)
	if w.pollInterval != 1*time.Minute {
		t.Fatalf("pollInterval = %v, want 1m", w.pollInterval)
	}
	if w.dimensions != 4096 {
		t.Fatalf("dimensions = %d, want 4096", w.dimensions)
	}
	if w.maxAttempts != 100 {
		t.Fatalf("maxAttempts = %d, want 100", w.maxAttempts)
	}
	if w.threshold != 0.99 {
		t.Fatalf("threshold = %f, want 0.99", w.threshold)
	}
}

func TestConfigure_MinimalPoll(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil)
	w.Configure(1*time.Millisecond, 0, 0, 0)
	if w.pollInterval != 1*time.Millisecond {
		t.Fatalf("pollInterval = %v, want 1ms", w.pollInterval)
	}
}

func TestWriteAudit_NilRepo(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil)
	// writeAudit with nil auditRepo should not panic
	task := ptrext.Of(embeddingRepo.Task{TenantID: "t1", FeedbackID: 1})
	w.writeAudit(context.Background(), task, llmrouter.EmbeddingResponse{}, time.Now())
}

func TestGenerateLabel_NilRouter(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, nil, nil)
	w.router = nil
	_, err := w.generateLabel(context.Background(), "tenant-1", []string{"t1", "t2"})
	if err == nil {
		t.Fatal("nil router should return error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error should mention 'not configured', got %q", err.Error())
	}
}

func TestHeartbeatInterval(t *testing.T) {
	t.Parallel()
	// heartbeatInterval must be strictly less than staleDuration to give the
	// worker a chance to refresh its claim before another worker reclaims it.
	w := NewWorker(nil, nil, nil, nil)
	if heartbeatInterval >= w.staleDuration {
		t.Fatalf("heartbeatInterval=%v must be less than staleDuration=%v", heartbeatInterval, w.staleDuration)
	}
	if heartbeatInterval <= 0 {
		t.Fatal("heartbeatInterval must be positive")
	}
}

func TestDrainTimeout(t *testing.T) {
	t.Parallel()
	if drainTimeout != 30*time.Second {
		t.Fatalf("drainTimeout = %v, want 30s", drainTimeout)
	}
}
